package filestore

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
)

// IssueTreeStore persists the editable issue tree mounted into an agent sandbox. It does no
// locking of its own: each tree path is private to
// one sandbox, and a sandbox never has two rounds in flight (the worker's in-flight set
// guarantees it), so the path itself is what serializes access.
type IssueTreeStore struct {
	Root string
}

func NewIssueTreeStore(root string) IssueTreeStore { return IssueTreeStore{Root: root} }

// treePath is where one sandbox's issue tree lives. Grouping the trees under the
// epic keeps PurgeFinishedWork's single RemoveAll of the epic directory
// reclaiming all of them.
func (s IssueTreeStore) treePath(sandboxName, epicID string) (string, error) {
	if err := validatePathComponent("sandbox name", sandboxName); err != nil {
		return "", err
	}
	if err := validatePathComponent("epic ID", epicID); err != nil {
		return "", err
	}
	return filepath.Join(s.Root, epicID, "trees", sandboxName), nil
}

func validatePathComponent(label, value string) error {
	if value == "" || value == "." || value == ".." ||
		filepath.VolumeName(value) != "" || strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("%s %q is not a bare path component", label, value)
	}
	return nil
}

var _ agent_runtime.IssueTreeStore = IssueTreeStore{}
