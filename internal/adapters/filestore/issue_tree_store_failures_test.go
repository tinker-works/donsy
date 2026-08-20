package filestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain/epic"
)

func TestIssueTreeStore_Write_ShouldReportAnEpicWithNoRootIssue(t *testing.T) {
	// Arrange: an epic nobody has drafted has no tree to mount.
	store := NewIssueTreeStore(t.TempDir())

	// Act
	_, err := store.Write("gm-sandbox", epic.Epic{ID: "epic", Title: "Epic", Assignee: "owner"})

	// Assert
	if err == nil {
		t.Fatal("expected the missing root to be reported")
	}
}

func TestIssueTreeStore_Write_ShouldReportADirectoryItCannotCreate(t *testing.T) {
	// Arrange: a root whose parent is a file cannot hold a tree.
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewIssueTreeStore(blocked)

	// Act
	_, err := store.Write("gm-sandbox", treeEpic(t))

	// Assert
	if err == nil {
		t.Fatal("expected the unusable tree path to be reported")
	}
}

func TestIssueTreeStore_Write_ShouldRejectPathLikeTreeComponents(t *testing.T) {
	// Arrange: neither input may turn the tree path into a path outside Root.
	root := filepath.Join(t.TempDir(), "store")
	escapeBase := filepath.Base(root)
	tests := []struct {
		name        string
		sandboxName string
		epicID      string
		escapePath  string
	}{
		{
			name:        "sandbox name",
			sandboxName: filepath.Join("..", "..", "..", escapeBase+"-sandbox-escape"),
			epicID:      "epic",
			escapePath:  filepath.Join(filepath.Dir(root), escapeBase+"-sandbox-escape"),
		},
		{
			name:        "epic ID",
			sandboxName: "gm-sandbox",
			epicID:      filepath.Join("..", escapeBase+"-epic-escape"),
			escapePath: filepath.Join(
				filepath.Dir(root), escapeBase+"-epic-escape", "trees", "gm-sandbox",
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.MkdirAll(test.escapePath, 0o755); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(test.escapePath, "sentinel.md")
			if err := os.WriteFile(sentinel, []byte("keep me"), 0o600); err != nil {
				t.Fatal(err)
			}

			// Act
			_, err := NewIssueTreeStore(root).Write(test.sandboxName, epic.Epic{
				ID: test.epicID, Issues: []epic.Issue{{ID: "root"}},
			})

			// Assert
			if err == nil {
				t.Fatal("expected a path-like tree component to be rejected")
			}
			if contents, err := os.ReadFile(sentinel); err != nil {
				t.Fatalf("expected the escaped tree to remain untouched: %v", err)
			} else if string(contents) != "keep me" {
				t.Fatalf("escaped tree was modified: %q", contents)
			}
		})
	}
}

func TestIssueTreeStore_Write_ShouldRejectAPathLikeIssueIDBeforeClearingTheTree(t *testing.T) {
	// Arrange: issue IDs are persisted data, but writeBranch uses them as file
	// names. Validation must happen before the existing mounted tree is cleared.
	root := filepath.Join(t.TempDir(), "store")
	path := filepath.Join(root, "epic", "trees", "gm-sandbox")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(path, "stale.md")
	if err := os.WriteFile(stale, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join("..", "..", "..", "..", "..", "victim")
	detail := treeEpic(t)
	detail.Issues[1].ID = escape
	escaped := filepath.Join(filepath.Dir(root), "victim.md")
	if err := os.WriteFile(escaped, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := NewIssueTreeStore(root).Write("gm-sandbox", detail)

	// Assert
	if err == nil {
		t.Fatal("expected the path-like issue ID to be rejected")
	}
	if contents, readErr := os.ReadFile(stale); readErr != nil || string(contents) != "keep me" {
		t.Fatalf("expected the existing tree to remain untouched: %v, %q", readErr, contents)
	}
	if contents, readErr := os.ReadFile(escaped); readErr != nil || string(contents) != "keep me" {
		t.Fatalf("expected the escaped file to remain untouched: %v, %q", readErr, contents)
	}
}

func TestIssueTreeStore_Write_ShouldRenderTheCommentThreadsBesideTheIssues(t *testing.T) {
	// Arrange: the agent reads the discussion as part of the tree.
	created := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	detail := treeEpic(t)
	detail.Issues[0].Comments = []epic.Comment{{
		ID: "c1", Author: "luuk", CreatedAt: created, Body: "the brief",
	}}
	detail.Issues[1].Comments = []epic.Comment{{
		ID: "c2", Author: "pr_reviewer", CreatedAt: created, Body: "rename the field",
	}}
	store := NewIssueTreeStore(t.TempDir())

	// Act
	path, err := store.Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	root, err := os.ReadFile(filepath.Join(path, "root-comments.md"))
	if err != nil {
		t.Fatalf("expected the root's thread rendered: %v", err)
	}
	if !strings.Contains(string(root), "## luuk") ||
		!strings.Contains(string(root), "the brief") {
		t.Fatalf("expected the author and body, got %q", root)
	}
	child, err := os.ReadFile(
		filepath.Join(path, "acme__widgets", "child-comments.md"))
	if err != nil {
		t.Fatalf("expected the child's thread rendered: %v", err)
	}
	if !strings.Contains(string(child), "rename the field") {
		t.Fatalf("expected the child's comment, got %q", child)
	}
}

func TestIssueTreeStore_Write_ShouldWriteNoThreadFileForAnIssueWithNoComments(t *testing.T) {
	// Arrange: an empty file would read as a thread the agent should reply in.
	store := NewIssueTreeStore(t.TempDir())

	// Act
	path, err := store.Write("gm-sandbox", treeEpic(t))
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if _, err := os.Stat(filepath.Join(path, "root-comments.md")); !os.IsNotExist(err) {
		t.Fatalf("expected no thread file, got err=%v", err)
	}
}

func TestIssueTreeStore_Read_ShouldReportATreeThatIsNotThere(t *testing.T) {
	// Arrange: a round whose sandbox never wrote a tree has nothing to import.
	store := NewIssueTreeStore(t.TempDir())

	// Act
	_, err := store.Read(filepath.Join(t.TempDir(), "missing"), treeEpic(t))

	// Assert
	if err == nil {
		t.Fatal("expected the missing tree to be reported")
	}
}

func TestIssueTreeStore_Read_ShouldReportAPreviousEpicWithNoRoot(t *testing.T) {
	// Arrange: the import re-anchors the tree on the epic's own root, so an epic
	// without one cannot take an edited tree back.
	store := NewIssueTreeStore(t.TempDir())
	path, err := store.Write("gm-sandbox", treeEpic(t))
	if err != nil {
		t.Fatal(err)
	}

	// Act
	_, err = store.Read(path, epic.Epic{ID: "epic", Title: "Epic", Assignee: "owner"})

	// Assert
	if err == nil {
		t.Fatal("expected the missing root to be reported")
	}
}

func TestIssueTreeStore_Read_ShouldReportAMalformedIssueFile(t *testing.T) {
	// Arrange: the agent writes these files, so a broken one is a round's output
	// to reject rather than a store to trust.
	store := NewIssueTreeStore(t.TempDir())
	detail := treeEpic(t)
	path, err := store.Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "root.md"),
		[]byte("---\ntitle: : :\n---\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err = store.Read(path, detail)

	// Assert
	if err == nil {
		t.Fatal("expected the malformed issue file to be refused")
	}
}

func TestIssueTreeStore_Read_ShouldRoundTripAnUneditedTree(t *testing.T) {
	// Arrange: a round that changed nothing must import back as what went in.
	store := NewIssueTreeStore(t.TempDir())
	detail := treeEpic(t)
	path, err := store.Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}

	// Act
	got, err := store.Read(path, detail)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Issues) != len(detail.Issues) {
		t.Fatalf("expected %d issues back, got %d", len(detail.Issues), len(got.Issues))
	}
	titles := map[string]string{}
	for _, issue := range got.Issues {
		titles[issue.ID] = issue.Title
	}
	for _, issue := range detail.Issues {
		if titles[issue.ID] != issue.Title {
			t.Fatalf("expected %q back for %q, got %q", issue.Title, issue.ID, titles[issue.ID])
		}
	}
}

func TestIssueTreeStore_Read_ShouldKeepAClosedIssueTheTreeNeverShowed(t *testing.T) {
	// Arrange: closed issues are left out of the tree, so an import must not read
	// their absence as a deletion.
	store := NewIssueTreeStore(t.TempDir())
	detail := treeEpic(t)
	if err := detail.CloseIssue("grandchild"); err != nil {
		t.Fatal(err)
	}
	path, err := store.Write("gm-sandbox", detail)
	if err != nil {
		t.Fatal(err)
	}

	// Act
	got, err := store.Read(path, detail)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range got.Issues {
		if issue.ID == "grandchild" {
			if issue.State != epic.IssueStateClosed {
				t.Fatalf("expected the closed issue kept closed, got %q", issue.State)
			}
			return
		}
	}
	t.Fatalf("expected the closed issue kept, got %+v", got.Issues)
}

func TestIssueTreeStore_Write_ShouldClearAStaleIssueOutOfTheTree(t *testing.T) {
	// Arrange: the directory is emptied in place rather than replaced, so a second
	// round must not still see the first round's files.
	store := NewIssueTreeStore(t.TempDir())
	first := treeEpic(t)
	path, err := store.Write("gm-sandbox", first)
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(path, "acme__widgets", "child", "grandchild.md")
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("expected the first round's file: %v", err)
	}

	// Act
	trimmed := treeEpic(t)
	trimmed.Issues = trimmed.Issues[:2]
	if _, err := store.Write("gm-sandbox", trimmed); err != nil {
		t.Fatal(err)
	}

	// Assert
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected the stale file gone, got err=%v", err)
	}
}
