package filestore

import (
	"github.com/tinker-works/donsy/internal/domain/epic"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssueTreeReader_Read_ShouldPreserveNestedTree(t *testing.T) {
	// Arrange
	detail := treeEpic(t)
	path, err := NewIssueTreeStore(t.TempDir()).Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}

	// Act
	reloaded, err := IssueTreeStore{}.Read(path, detail)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Issues) != 3 {
		t.Fatalf("expected root, child, and grandchild: %#v", reloaded.Issues)
	}
	if reloaded.Issues[2].ParentID != "child" || reloaded.Issues[2].Repository != "acme/widgets" {
		t.Fatalf("nested issue was not preserved: %#v", reloaded.Issues[2])
	}
	// A nested issue the reader never visits falls out of the staged tree and is
	// closed as omitted, which leaves its ParentID intact — so state is the only
	// assertion that tells "read back" from "silently withdrawn".
	if reloaded.Issues[2].State != epic.IssueStateOpen {
		t.Fatalf("nested issue was closed as omitted: %#v", reloaded.Issues[2])
	}
}

// Nesting is how the refiner says "this lands first": a prerequisite goes under
// the issue that integrates it, and Epic.Blocked holds the parent's pull request
// until the child merges. The refiner writes those files from scratch, with slug
// names and no id, so the nesting has to survive minting too.
func TestIssueTreeReader_Read_ShouldImportNestingTheRefinerCreated(t *testing.T) {
	// Arrange: an epic with nothing but its root, as it is on the first round.
	detail := rootOnlyEpic(t)
	path, err := NewIssueTreeStore(t.TempDir()).Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(path, "acme__widgets")
	if err := os.MkdirAll(filepath.Join(folder, "split-cart"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, title string) {
		contents := "---\ntitle: " + title + "\n---\n\n" + plannedBody(title) + "\n"
		if err := os.WriteFile(filepath.Join(folder, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("split-cart.md", "Split the cart total out of the checkout handler")
	write(filepath.Join("split-cart", "extract-total.md"), "Extract the total calculation")

	// Act
	reloaded, err := IssueTreeStore{}.Read(path, detail)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	byTitle := make(map[string]epic.Issue, len(reloaded.Issues))
	for _, issue := range reloaded.Issues {
		byTitle[issue.Title] = issue
	}
	parent, ok := byTitle["Split the cart total out of the checkout handler"]
	if !ok {
		t.Fatalf("the parent issue was not imported: %+v", reloaded.Issues)
	}
	child, ok := byTitle["Extract the total calculation"]
	if !ok {
		t.Fatalf("the nested issue was not imported: %+v", reloaded.Issues)
	}
	if child.ParentID != parent.ID {
		t.Fatalf("nested issue is not under its parent: %+v", child)
	}
	if child.Repository != "acme/widgets" || child.State != epic.IssueStateOpen {
		t.Fatalf("nested issue did not inherit its branch: %+v", child)
	}
	if !reloaded.Blocked(parent.ID) {
		t.Fatal("the parent should be blocked until its prerequisite lands")
	}
}

// A coding round is handed one issue file and nothing else, so an issue that
// omits a section is one the round has to guess at. Failing the refine round is
// what keeps that from reaching a coding agent.
func TestIssueTreeReader_Read_ShouldRejectAnIssueMissingASection(t *testing.T) {
	// Arrange
	detail := rootOnlyEpic(t)
	path, err := NewIssueTreeStore(t.TempDir()).Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(path, "acme__widgets")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Summary\n\nShips.\n\n# Problem\n\nMissing.\n\n# Proposal\n\nAdd it."
	contents := "---\ntitle: Split the cart total\n---\n\n" + body + "\n"
	issuePath := filepath.Join(folder, "split-cart.md")
	if err := os.WriteFile(issuePath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err = IssueTreeStore{}.Read(path, detail)

	// Assert
	if err == nil {
		t.Fatal("expected the round to be refused")
	}
	if !strings.Contains(err.Error(), "# Context") ||
		!strings.Contains(err.Error(), "split-cart.md") {
		t.Fatalf("error should name the file and the missing section: %v", err)
	}
}

// root.md is the epic restated, written by the host from the epic's own body
// rather than authored as a unit of work, so the section requirement that makes
// an issue implementable on its own does not apply to it.
func TestIssueTreeReader_Read_ShouldNotRequireSectionsOfTheRoot(t *testing.T) {
	// Arrange
	detail := treeEpic(t)
	path, err := NewIssueTreeStore(t.TempDir()).Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := "---\ntitle: Epic\nid: root\n---\n\nA brief with no sections at all.\n"
	if err := os.WriteFile(filepath.Join(path, "root.md"), []byte(rewritten), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	reloaded, err := IssueTreeStore{}.Read(path, detail)

	// Assert
	if err != nil {
		t.Fatalf("the root should not be held to the issue sections: %v", err)
	}
	if reloaded.Body != "A brief with no sections at all." {
		t.Fatalf("root body was not adopted: %q", reloaded.Body)
	}
}

// The refiner names a file because an issue it created this round has no ID
// until the host mints one, so a dependency between two brand-new issues can
// only be stated as a path. It has to survive minting as an ID.
func TestIssueTreeReader_Read_ShouldResolveBlockersTheRefinerWroteAsPaths(t *testing.T) {
	// Arrange
	detail := rootOnlyEpic(t)
	path, err := NewIssueTreeStore(t.TempDir()).Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(path, "acme__widgets")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, frontMatter, title string) {
		contents := "---\ntitle: " + title + "\n" + frontMatter + "---\n\n" +
			plannedBody(title) + "\n"
		if err := os.WriteFile(filepath.Join(folder, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("extract-total.md", "", "Extract the total calculation")
	// Spelled as the guest sees it, which is the path the refiner is looking at.
	write("split-cart.md", "blocked_by:\n  - /work/issues/acme__widgets/extract-total.md\n",
		"Split the cart total out of the checkout handler")

	// Act
	reloaded, err := IssueTreeStore{}.Read(path, detail)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	byTitle := make(map[string]epic.Issue, len(reloaded.Issues))
	for _, issue := range reloaded.Issues {
		byTitle[issue.Title] = issue
	}
	blocker := byTitle["Extract the total calculation"]
	waiting := byTitle["Split the cart total out of the checkout handler"]
	if len(waiting.BlockedBy) != 1 || waiting.BlockedBy[0] != blocker.ID {
		t.Fatalf("blocker was not resolved to its minted id: %#v", waiting)
	}
	if !reloaded.Blocked(waiting.ID) {
		t.Fatal("the waiting issue should be held until its blocker lands")
	}
}

func TestIssueTreeReader_Read_ShouldRejectABlockerThatNamesNoFile(t *testing.T) {
	// Arrange: dropping the reference would start work the plan said has to
	// wait, so a path nothing answers fails the round instead.
	detail := rootOnlyEpic(t)
	path, err := NewIssueTreeStore(t.TempDir()).Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(path, "acme__widgets")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "---\ntitle: Split the cart\nblocked_by:\n  - acme__widgets/ghost.md\n---\n\n" +
		plannedBody("Split the cart") + "\n"
	file := filepath.Join(folder, "split-cart.md")
	if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err = IssueTreeStore{}.Read(path, detail)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "not a file in the tree") {
		t.Fatalf("expected the dangling blocker to be reported, got %v", err)
	}
}

func rootOnlyEpic(t *testing.T) epic.Epic {
	t.Helper()
	detail := epic.Epic{
		ID: "epic", Title: "Epic", Assignee: "owner", State: epic.EpicStateRefine,
		Repositories: []string{"acme/widgets"},
		Issues: []epic.Issue{
			{ID: "root", Title: "Root", State: epic.IssueStateOpen, Body: "Root body"},
		},
	}
	if err := detail.Validate(); err != nil {
		t.Fatal(err)
	}
	return detail
}

// root.md is the epic restated, and the only file the refiner has for the epic
// itself: a round that reshaped the plan has to be able to say so in the epic's
// own title, not only in the issues below it.
func TestIssueTreeReader_Read_ShouldAdoptTheRootFilesTitleAndBody(t *testing.T) {
	// Arrange
	detail := treeEpic(t)
	path, err := NewIssueTreeStore(t.TempDir()).Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := "---\ntitle: Extract the cart total\nid: root\n---\n\nThe refined brief.\n"
	if err := os.WriteFile(filepath.Join(path, "root.md"), []byte(rewritten), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	reloaded, err := IssueTreeStore{}.Read(path, detail)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Title != "Extract the cart total" || reloaded.Body != "The refined brief." {
		t.Fatalf("epic did not adopt the root file: %q, %q", reloaded.Title, reloaded.Body)
	}
	root, err := reloaded.RootIssue()
	if err != nil {
		t.Fatal(err)
	}
	if root.Title != reloaded.Title || root.Body != reloaded.Body {
		t.Fatalf("root issue drifted from the epic: %#v", root)
	}
	if root.ID != "root" {
		t.Fatalf("expected the root issue's identity preserved: %#v", root)
	}
}

func TestIssueTreeReader_Read_ShouldCloseOmittedIssueAndDescendants(t *testing.T) {
	// Arrange
	detail := treeEpic(t)
	path, err := NewIssueTreeStore(t.TempDir()).Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(path, "acme__widgets", "child.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(path, "acme__widgets", "child")); err != nil {
		t.Fatal(err)
	}

	// Act
	reloaded, err := IssueTreeStore{}.Read(path, detail)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range reloaded.Issues {
		if issue.ID != "root" && issue.State != epic.IssueStateClosed {
			t.Fatalf("omitted issue was not closed: %#v", issue)
		}
	}
}

// The whole point of a refine round is that the tree grows: the epic arrives with
// only its root issue and the refiner writes the plan as new files. Matching the
// staged tree against the previous one by ID can only ever update what already
// existed, so without appending, a refiner that wrote a complete plan reads back
// as one that wrote nothing and RunEpicAgentUseCase rejects the round.
func TestIssueTreeReader_Read_ShouldImportIssuesTheRefinerCreated(t *testing.T) {
	// Arrange: an epic with nothing but its root, as it is on the first round.
	detail := rootOnlyEpic(t)
	path, err := NewIssueTreeStore(t.TempDir()).Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}
	// The refiner writes two issue files with no id, the way the prompt asks.
	folder := filepath.Join(path, "acme__widgets")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, title := range map[string]string{
		"split-cart.md": "Split the cart total out of the checkout handler",
		"card-form.md":  "Rebuild the card form",
	} {
		contents := "---\ntitle: " + title + "\n---\n\n" + plannedBody(title) + "\n"
		if err := os.WriteFile(filepath.Join(folder, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	reloaded, err := IssueTreeStore{}.Read(path, detail)
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	var planned []epic.Issue
	for _, issue := range reloaded.Issues {
		if issue.ParentID != "" {
			planned = append(planned, issue)
		}
	}
	if len(planned) != 2 {
		t.Fatalf("expected the two planned issues to be imported, got %+v", reloaded.Issues)
	}
	for _, issue := range planned {
		if issue.ID == "" {
			t.Fatalf("expected a minted ID for a created issue: %+v", issue)
		}
		if issue.ParentID != "root" || issue.Repository != "acme/widgets" {
			t.Fatalf("expected the issue scoped under the root: %+v", issue)
		}
		if issue.State != epic.IssueStateOpen {
			t.Fatalf("expected a created issue to be open, got %q", issue.State)
		}
	}
}
