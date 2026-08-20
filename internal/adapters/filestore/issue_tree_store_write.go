package filestore

import (
	"fmt"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func (s IssueTreeStore) Write(sandboxName string, epic epicpkg.Epic) (string, error) {
	path, err := s.treePath(sandboxName, epic.ID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	// Empty the directory without replacing it. A sandbox outlives the rounds that run
	// on it, and the bind resolved this path to an inode when the container was created:
	// deleting the directory leaves the guest with a mount onto something that no
	// longer exists, where the tree reads as empty and every write fails with
	// ENOENT rather than a permission error that would point back here.
	//
	// Clearing in place is also why the path is per sandbox rather than per epic: two
	// rounds in one epic run concurrently, and a shared directory would have one
	// of them emptying the tree the other's running sandbox has mounted.
	if err := clearDirectory(path); err != nil {
		return "", err
	}
	root, err := epic.RootIssue()
	if err != nil {
		return "", err
	}
	if err := writeIssueFile(path, "root.md", root, nil); err != nil {
		return "", err
	}
	if err := writeIssueComments(path, "root-comments.md", root.Comments); err != nil {
		return "", err
	}
	children := childrenByParent(epic.Issues)
	// The whole map is needed before the first file is written: an issue can wait
	// on one that is written later, and the reference is a path to its file.
	paths := treePaths(root, children)
	for _, issue := range children[root.ID] {
		if issue.State == epicpkg.IssueStateClosed {
			continue
		}
		if err := writeBranch(path, "", issue, children, paths); err != nil {
			return "", err
		}
	}
	return path, nil
}

// treePaths locates every issue the tree will hold, keyed by ID and relative to
// the tree root. A closed issue is left out for the same reason it is not
// written: the refiner is planning what is left to do.
func treePaths(root epicpkg.Issue, children map[string][]epicpkg.Issue) map[string]string {
	paths := map[string]string{}
	var walk func(parentDir string, issue epicpkg.Issue)
	walk = func(parentDir string, issue epicpkg.Issue) {
		if issue.State == epicpkg.IssueStateClosed {
			return
		}
		folder := parentDir
		if folder == "" {
			folder = strings.ReplaceAll(issue.Repository, "/", "__")
		}
		if folder == "" {
			return
		}
		paths[issue.ID] = folder + "/" + issue.ID + ".md"
		for _, child := range children[issue.ID] {
			walk(folder+"/"+issue.ID, child)
		}
	}
	for _, issue := range children[root.ID] {
		walk("", issue)
	}
	return paths
}

// blockedByPaths renders an issue's blockers as the files the refiner sees. A
// blocker missing from the tree was closed, and a closed blocker holds nothing
// up, so dropping it is what the reference meant.
func blockedByPaths(issue epicpkg.Issue, paths map[string]string) []string {
	rendered := make([]string, 0, len(issue.BlockedBy))
	for _, blockerID := range issue.BlockedBy {
		if path, exists := paths[blockerID]; exists {
			rendered = append(rendered, path)
		}
	}
	return rendered
}

func clearDirectory(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func writeBranch(
	root, parentDir string,
	issue epicpkg.Issue,
	children map[string][]epicpkg.Issue,
	paths map[string]string,
) error {
	if issue.State == epicpkg.IssueStateClosed {
		return nil
	}
	folder := parentDir
	if folder == "" {
		folder = strings.ReplaceAll(issue.Repository, "/", "__")
	}
	if folder == "" {
		return fmt.Errorf("issue %q has no repository", issue.ID)
	}
	if err := writeIssueFile(
		filepath.Join(root, folder), issue.ID+".md", issue, blockedByPaths(issue, paths),
	); err != nil {
		return err
	}
	commentsPath := filepath.Join(root, folder)
	if err := writeIssueComments(commentsPath, issue.ID+"-comments.md", issue.Comments); err != nil {
		return err
	}
	childDir := filepath.Join(folder, issue.ID)
	for _, child := range children[issue.ID] {
		if child.State == epicpkg.IssueStateClosed {
			continue
		}
		if child.Repository != issue.Repository {
			return fmt.Errorf("issue %q has child %q in another repository", issue.ID, child.ID)
		}
		if err := writeBranch(root, childDir, child, children, paths); err != nil {
			return err
		}
	}
	return nil
}

func childrenByParent(issues []epicpkg.Issue) map[string][]epicpkg.Issue {
	children := make(map[string][]epicpkg.Issue)
	for _, issue := range issues {
		if issue.ParentID != "" {
			children[issue.ParentID] = append(children[issue.ParentID], issue)
		}
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool {
			return children[parent][i].ID < children[parent][j].ID
		})
	}
	return children
}

func writeIssueFile(folder, name string, issue epicpkg.Issue, blockedBy []string) error {
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	frontMatter, err := yaml.Marshal(issueDocument{
		Title: issue.Title, ID: issue.ID, BlockedBy: blockedBy,
	})
	if err != nil {
		return err
	}
	contents := "---\n" + string(frontMatter) + "---\n\n" + strings.TrimSpace(issue.Body) + "\n"
	return os.WriteFile(filepath.Join(folder, name), []byte(contents), 0o644)
}

func writeIssueComments(folder, name string, comments []epicpkg.Comment) error {
	if len(comments) == 0 {
		return nil
	}
	var contents strings.Builder
	for _, comment := range comments {
		_, _ = fmt.Fprintf(&contents, "## %s\n\n%s\n\n", comment.Author, strings.TrimSpace(comment.Body))
	}
	return os.WriteFile(filepath.Join(folder, name), []byte(contents.String()), 0o644)
}
