package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentWorkspace_Purge_ShouldRemoveEverythingAnEpicHolds(t *testing.T) {
	// Both roots live under the epic's directory: the clones this type maintains and
	// the issue tree filestore.IssueTreeStore writes, which share the same wired root.
	// Arrange
	root := t.TempDir()
	tree := filepath.Join(root, "epic-1", "epic", "acme__widgets")
	clone := filepath.Join(root, "epic-1", "repos", "acme__widgets")
	other := filepath.Join(root, "epic-2", "repos", "acme__widgets")
	for _, directory := range []string{tree, clone, other} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	if err := NewAgentWorkspace(root).Purge("epic-1"); err != nil {
		t.Fatal(err)
	}

	// Assert
	if _, err := os.Stat(filepath.Join(root, "epic-1")); !os.IsNotExist(err) {
		t.Fatal("expected the finished epic's directory to be removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("expected another epic to be untouched: %v", err)
	}
}

func TestCodeWorkspace_PurgeEpic_ShouldRemoveEveryCheckoutUnderIt(t *testing.T) {
	// Arrange
	root := t.TempDir()
	mine := filepath.Join(root, "epic-1", "issues", "issue-1", "acme__widgets")
	other := filepath.Join(root, "epic-2", "issues", "issue-2", "acme__widgets")
	for _, directory := range []string{mine, other} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	if err := NewCodeWorkspace(root).PurgeEpic("epic-1"); err != nil {
		t.Fatal(err)
	}

	// Assert
	if _, err := os.Stat(filepath.Join(root, "epic-1")); !os.IsNotExist(err) {
		t.Fatal("expected the finished epic's checkouts to be removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("expected another epic's checkouts to be untouched: %v", err)
	}
}

func TestPurge_ShouldRejectAnEpicIDThatEscapesTheRoot(t *testing.T) {
	// An ID is a ULID everywhere it is minted, so anything path-like is a caller bug
	// — and here it would delete a directory outside the root.
	// Arrange
	root := t.TempDir()

	// Act & Assert
	for _, id := range []string{"", "../elsewhere", "nested/id"} {
		if err := NewAgentWorkspace(root).Purge(id); err == nil {
			t.Fatalf("expected workspace purge of %q to be rejected", id)
		}
		if err := NewCodeWorkspace(root).PurgeEpic(id); err == nil {
			t.Fatalf("expected checkout purge of %q to be rejected", id)
		}
	}
}
