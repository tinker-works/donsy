package filestore

import (
	"github.com/tinker-works/donsy/internal/domain/epic"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssueTreeWriter_Write_ShouldRenderNestedTree(t *testing.T) {
	// Arrange
	detail := treeEpic(t)
	writer := NewIssueTreeStore(t.TempDir())

	// Act
	path, err := writer.Write("gm-sandbox", detail)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "acme__widgets", "child", "grandchild.md")); err != nil {
		t.Fatalf("nested issue file was not rendered: %v", err)
	}
}

func TestIssueTreeWriter_Write_ShouldNotRenderClosedIssues(t *testing.T) {
	// Arrange
	detail := treeEpic(t)
	if err := detail.CloseIssue("child"); err != nil {
		t.Fatal(err)
	}
	writer := NewIssueTreeStore(t.TempDir())

	// Act
	path, err := writer.Write("gm-sandbox", detail)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "acme__widgets", "child.md")); !os.IsNotExist(err) {
		t.Fatalf("closed child was rendered: %v", err)
	}
	grandchildPath := filepath.Join(path, "acme__widgets", "child", "grandchild.md")
	if _, err := os.Stat(grandchildPath); !os.IsNotExist(err) {
		t.Fatalf("closed descendant was rendered: %v", err)
	}
}

// The store keeps blockers as issue IDs, but an issue the refiner is about to
// create has no ID to name. Writing the reference as the file it lives in is
// what lets the refiner state a dependency in either direction.
func TestIssueTreeWriter_Write_ShouldRenderBlockersAsFilePaths(t *testing.T) {
	// Arrange
	detail := siblingEpic(t)
	detail.Issues[2].BlockedBy = []string{"first"}
	writer := NewIssueTreeStore(t.TempDir())

	// Act
	path, err := writer.Write("gm-sandbox", detail)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(path, "acme__widgets", "second.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "blocked_by:\n    - acme__widgets/first.md"; !strings.Contains(string(contents), want) {
		t.Fatalf("expected the blocker as a path, got:\n%s", contents)
	}
}

func TestIssueTreeWriter_Write_ShouldDropABlockerThatIsClosed(t *testing.T) {
	// Arrange: a closed issue is not in the tree and holds nothing up, so the
	// reference has nothing left to point at.
	detail := siblingEpic(t)
	detail.Issues[2].BlockedBy = []string{"first"}
	if err := detail.CloseIssue("first"); err != nil {
		t.Fatal(err)
	}
	writer := NewIssueTreeStore(t.TempDir())

	// Act
	path, err := writer.Write("gm-sandbox", detail)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(path, "acme__widgets", "second.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "blocked_by") {
		t.Fatalf("expected no blocker, got:\n%s", contents)
	}
}

// siblingEpic is two issues under the root, which nesting cannot order against
// each other — the case BlockedBy exists for.
func siblingEpic(t *testing.T) epic.Epic {
	t.Helper()
	detail := epic.Epic{
		ID: "epic", Title: "Epic", Assignee: "owner", State: epic.EpicStateConcept,
		Repositories: []string{"acme/widgets"},
		Issues: []epic.Issue{
			{ID: "root", Title: "Root", State: epic.IssueStateOpen, Body: "Root body"},
			{
				ID: "first", Title: "First", ParentID: "root", Repository: "acme/widgets",
				State: epic.IssueStateOpen, Body: plannedBody("First"),
			},
			{
				ID: "second", Title: "Second", ParentID: "root", Repository: "acme/widgets",
				State: epic.IssueStateOpen, Body: plannedBody("Second"),
			},
		},
	}
	if err := detail.Validate(); err != nil {
		t.Fatal(err)
	}
	return detail
}

// plannedBody renders the four sections the reader requires of every issue below
// the root, so a fixture exercises the tree rather than the section check.
func plannedBody(subject string) string {
	return "# Summary\n\n" + subject + " ships.\n\n" +
		"# Problem\n\n" + subject + " is missing.\n\n" +
		"# Context\n\nThe handler already owns this path.\n\n" +
		"# Proposal\n\nAdd it, covered by a test that fails without the change."
}

func treeEpic(t *testing.T) epic.Epic {
	t.Helper()
	detail := epic.Epic{
		ID: "epic", Title: "Epic", Assignee: "owner", State: epic.EpicStateConcept,
		Repositories: []string{"acme/widgets"},
		Issues: []epic.Issue{
			{ID: "root", Title: "Root", State: epic.IssueStateOpen, Body: "Root body"},
			{
				ID: "child", Title: "Child", ParentID: "root", Repository: "acme/widgets",
				State: epic.IssueStateOpen, Body: plannedBody("Child"),
			},
			{
				ID: "grandchild", Title: "Grandchild", ParentID: "child", Repository: "acme/widgets",
				State: epic.IssueStateOpen, Body: plannedBody("Grandchild"),
			},
		},
	}
	if err := detail.Validate(); err != nil {
		t.Fatal(err)
	}
	return detail
}

// A sandbox outlives the rounds that run on it, and docker resolves the issue-tree bind
// to an inode when the sandbox is created. Replacing the directory on a later round
// leaves the guest mounted onto a deleted inode: the tree reads as empty and every
// write fails with ENOENT, which looks like a permissions problem in the guest
// rather than a rewrite on the host.
func TestIssueTreeWriter_Write_ShouldReuseTheDirectoryItAlreadyMounted(t *testing.T) {
	// Arrange
	store := NewIssueTreeStore(t.TempDir())
	path, err := store.Write("gm-sandbox", treeEpic(t))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(path, "stale.md")
	if err := os.WriteFile(stale, []byte("left over"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act: the next round rewrites the same epic's tree.
	again, err := store.Write("gm-sandbox", treeEpic(t))
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if again != path {
		t.Fatalf("expected the same path, got %q and %q", path, again)
	}
	after, err := os.Stat(again)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("the mounted directory was replaced instead of emptied")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected the previous round's files to be cleared, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(again, "root.md")); err != nil {
		t.Fatalf("expected the tree to be rewritten: %v", err)
	}
}

// Rounds on two subjects of one epic run at the same time, and each keeps its
// tree mounted for the whole round. Sharing a directory would have the second
// round's clearDirectory emptying the tree the first round's sandbox is reading.
func TestIssueTreeWriter_Write_ShouldGiveEachSandboxItsOwnTree(t *testing.T) {
	// Arrange
	store := NewIssueTreeStore(t.TempDir())
	detail := treeEpic(t)

	// Act: two sandboxes of the same epic, the way a drafting round and a coding round
	// on one of its issues reach the store.
	first, err := store.Write("gm-1-epic-refiner", detail)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(first, "in-flight.md")
	if err := os.WriteFile(marker, []byte("mounted right now"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := store.Write("gm-1-issue-coding", detail)
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if first == second {
		t.Fatalf("both sandboxes were given the same tree at %q", first)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the second sandbox's write cleared the first sandbox's mounted tree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(second, "root.md")); err != nil {
		t.Fatalf("expected the second sandbox's tree to be written: %v", err)
	}
	// Both live under the epic, so PurgeFinishedWork's single RemoveAll of the
	// epic directory still reclaims every tree it accumulated.
	epicDir := filepath.Dir(filepath.Dir(first))
	if filepath.Dir(filepath.Dir(second)) != epicDir {
		t.Fatalf("trees are not both under the epic: %q and %q", first, second)
	}
	if filepath.Base(epicDir) != detail.ID {
		t.Fatalf("tree root is %q, want it under epic %q", epicDir, detail.ID)
	}
}
