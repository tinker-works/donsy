package filestore

import (
	"fmt"
	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tinker-works/donsy/internal/repositorypath"
	"gopkg.in/yaml.v3"
)

// IssueTreeReader imports an agent sandbox's edited issue tree.
func (s IssueTreeStore) Read(path string, previous epicpkg.Epic) (epicpkg.Epic, error) {
	rootPath := filepath.Join(path, "root.md")
	root, err := readIssueFile(rootPath, epicpkg.Issue{})
	if err != nil {
		return epicpkg.Epic{}, err
	}
	oldByID := make(map[string]epicpkg.Issue, len(previous.Issues))
	for _, issue := range previous.Issues {
		oldByID[issue.ID] = issue
	}
	previousRoot, err := previous.RootIssue()
	if err != nil {
		return epicpkg.Epic{}, err
	}
	root.ID = previousRoot.ID
	root.ParentID = ""
	root.State = previousRoot.State
	root.CreatedAt = previousRoot.CreatedAt
	root.Comments = previousRoot.Comments
	// The epic restated waits on nothing; only the work below it can.
	root.BlockedBy = nil
	reader := &issueTreeReader{
		treeRoot: path,
		previous: previous,
		oldByID:  oldByID,
		issues:   []epicpkg.Issue{root},
		seen:     map[string]struct{}{root.ID: {}},
		idByPath: map[string]string{},
	}
	if err := reader.readRepositories(path, root.ID); err != nil {
		return epicpkg.Epic{}, err
	}
	if err := reader.resolveBlockedBy(); err != nil {
		return epicpkg.Epic{}, err
	}
	issues := reader.issues
	seen := reader.seen
	sort.Slice(issues, func(i, j int) bool { return issues[i].ID < issues[j].ID })
	updated := previous
	for _, old := range previous.Issues {
		if old.ParentID == "" || old.State == epicpkg.IssueStateClosed {
			continue
		}
		if _, exists := seen[old.ID]; exists {
			continue
		}
		if err := updated.CloseIssue(old.ID); err != nil {
			return epicpkg.Epic{}, fmt.Errorf("close omitted issue %q: %w", old.ID, err)
		}
	}
	stagedByID := make(map[string]epicpkg.Issue, len(issues))
	for _, issue := range issues {
		stagedByID[issue.ID] = issue
	}
	known := make(map[string]struct{}, len(updated.Issues))
	for index, issue := range updated.Issues {
		known[issue.ID] = struct{}{}
		if staged, exists := stagedByID[issue.ID]; exists {
			updated.Issues[index] = staged
		}
	}
	// An issue the refiner wrote for the first time is in no previous list, so
	// matching by ID can only ever update what already existed. Appending is what
	// lets the tree grow at all: without it a refiner that wrote a complete plan
	// reads back as one that wrote nothing, and the round is rejected for it.
	for _, issue := range issues {
		if _, exists := known[issue.ID]; exists {
			continue
		}
		updated.Issues = append(updated.Issues, issue)
	}
	updated.Sort()
	// root.md is the epic restated, and it is the only file the refiner has for
	// it: without adopting what it wrote there, a round that reshaped the plan
	// leaves the epic under whatever the request was first called.
	if err := updated.SetTitle(root.Title); err != nil {
		return epicpkg.Epic{}, err
	}
	if err := updated.SetBody(root.Body); err != nil {
		return epicpkg.Epic{}, err
	}
	if err := updated.Validate(); err != nil {
		return epicpkg.Epic{}, fmt.Errorf("validate refined tree: %w", err)
	}
	return updated, nil
}

// issueTreeReader carries what every level of the walk needs: where the tree
// starts, what the epic looked like before it, and what the walk has collected
// so far. Threading each of those through the recursion separately is what it
// replaces.
type issueTreeReader struct {
	treeRoot string
	previous epicpkg.Epic
	oldByID  map[string]epicpkg.Issue
	issues   []epicpkg.Issue
	seen     map[string]struct{}
	// idByPath resolves a blocked_by reference. The refiner names the file it
	// wrote, because an issue it created this round has no ID until the walk
	// below mints one.
	idByPath map[string]string
}

// readRepositories reads the top level of the tree, where a directory name is a
// repository rather than an issue. Everything below it is nested issues, which
// inherit the repository their branch is rooted in.
func (r *issueTreeReader) readRepositories(root, parentID string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repository, err := repositorypath.Decode(entry.Name())
		if err != nil {
			return fmt.Errorf("issue tree has invalid repository directory %q: %w",
				entry.Name(), err)
		}
		if !contains(r.previous.Repositories, repository) {
			return fmt.Errorf("issue tree names unscoped repository %q", repository)
		}
		if err := r.readDirectory(
			filepath.Join(root, entry.Name()), parentID, repository,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *issueTreeReader) readDirectory(directory, parentID, repository string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" ||
			strings.HasSuffix(entry.Name(), "-comments.md") {
			continue
		}
		file := filepath.Join(directory, entry.Name())
		issue, err := readPlannedIssue(file)
		if err != nil {
			return err
		}
		issue.ParentID = parentID
		issue.Repository = repository
		if old, exists := r.oldByID[issue.ID]; exists {
			issue.CreatedAt = old.CreatedAt
			issue.Comments = old.Comments
			issue.State = old.State
		} else {
			issue.ID = domain.MintULID()
			issue.CreatedAt = r.previous.Issues[0].CreatedAt
			issue.State = epicpkg.IssueStateOpen
		}
		if _, exists := r.seen[issue.ID]; exists {
			return fmt.Errorf("issue tree repeats ID %q", issue.ID)
		}
		r.seen[issue.ID] = struct{}{}
		if reference, err := filepath.Rel(r.treeRoot, file); err == nil {
			r.idByPath[filepath.ToSlash(reference)] = issue.ID
		}
		r.issues = append(r.issues, issue)
		// An issue's children live in a folder named after its file without the
		// ".md", and they are issue files themselves — not another level of
		// repository folders. A leaf has no such folder, which is not an error.
		if err := r.readDirectory(
			filepath.Join(directory, strings.TrimSuffix(entry.Name(), ".md")),
			issue.ID, repository,
		); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// resolveBlockedBy turns the paths the refiner wrote into the IDs the store
// keeps. A reference nothing answers is refused rather than dropped: the plan
// said this work waits, and losing that silently is how it gets started early.
func (r *issueTreeReader) resolveBlockedBy() error {
	for index := range r.issues {
		issue := &r.issues[index]
		if len(issue.BlockedBy) == 0 {
			continue
		}
		resolved := make([]string, 0, len(issue.BlockedBy))
		for _, reference := range issue.BlockedBy {
			id, found := r.idByPath[normalizeReference(reference)]
			if !found {
				return fmt.Errorf(
					"issue %q is blocked by %q, which is not a file in the tree",
					issue.Title, reference,
				)
			}
			resolved = append(resolved, id)
		}
		issue.BlockedBy = resolved
	}
	return nil
}

// normalizeReference accepts a blocked_by entry however the refiner spelled it:
// the guest's absolute path, or one relative to the tree it is writing in.
func normalizeReference(reference string) string {
	reference = filepath.ToSlash(strings.TrimSpace(reference))
	reference = strings.TrimPrefix(reference, "/work/issues/")
	reference = strings.TrimPrefix(reference, "./")
	return strings.TrimPrefix(reference, "/")
}

type issueDocument struct {
	Title     string   `yaml:"title"`
	ID        string   `yaml:"id,omitempty"`
	BlockedBy []string `yaml:"blocked_by,omitempty"`
}

// plannedSections are the headings every planned issue carries, in this order.
// They are what makes an issue readable on its own: a coding round is handed one
// issue file and nothing else, so an issue that omits its problem or its
// completion criteria is one the round has to guess at.
//
// root.md is exempt. It is the epic restated, written by the host from the
// epic's own body rather than authored as a unit of work.
var plannedSections = []string{"# Summary", "# Problem", "# Context", "# Proposal"}

func readPlannedIssue(path string) (epicpkg.Issue, error) {
	issue, err := readIssueFile(path, epicpkg.Issue{})
	if err != nil {
		return epicpkg.Issue{}, err
	}
	if err := validateSections(path, issue.Body); err != nil {
		return epicpkg.Issue{}, err
	}
	return issue, nil
}

// validateSections walks the body once, advancing only when it meets the next
// heading it is waiting for. A section that is present but out of order never
// satisfies the wait, so misordered and missing report the same way.
func validateSections(path, body string) error {
	wanted := 0
	for _, line := range strings.Split(body, "\n") {
		if wanted == len(plannedSections) {
			break
		}
		if strings.TrimSpace(line) == plannedSections[wanted] {
			wanted++
		}
	}
	if wanted == len(plannedSections) {
		return nil
	}
	return fmt.Errorf(
		"%s is missing the %q section: an issue body must carry %s, in that order",
		path, plannedSections[wanted], strings.Join(plannedSections, ", "),
	)
}

func readIssueFile(path string, issue epicpkg.Issue) (epicpkg.Issue, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return epicpkg.Issue{}, err
	}
	parts := strings.SplitN(string(contents), "---\n", 3)
	if len(parts) != 3 || parts[0] != "" {
		return epicpkg.Issue{}, fmt.Errorf("%s must start with YAML front matter", path)
	}
	var document issueDocument
	if err := yaml.Unmarshal([]byte(parts[1]), &document); err != nil {
		return epicpkg.Issue{}, err
	}
	issue.ID = document.ID
	issue.Title = strings.TrimSpace(document.Title)
	// Still the paths the refiner wrote at this point; resolveBlockedBy turns
	// them into IDs once the whole tree is known.
	issue.BlockedBy = document.BlockedBy
	issue.Body = strings.TrimSpace(parts[2])
	if issue.Title == "" || issue.Body == "" {
		return epicpkg.Issue{}, fmt.Errorf("%s requires title and body", path)
	}
	return issue, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
