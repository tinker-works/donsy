package workspace

import (
	"context"
	"errors"
	"fmt"
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// AgentWorkspace maintains host-side clones that are mounted read-only into agent sandboxes.
type AgentWorkspace struct {
	root string
}

func NewAgentWorkspace(root string) AgentWorkspace {
	return AgentWorkspace{root: root}
}

// epicDirectoryLocks serializes work on one epic's directory here.
//
// Rounds run concurrently, and every round of an epic calls Ensure for each of
// its repositories. go-git has no equivalent of git's index.lock, so two clones
// or pulls of the same path corrupt the object store between them rather than
// one waiting for the other. Purge deletes the directory outright, and the TUI
// reaches Ensure through Diff on a goroutine of its own.
//
// Locks are kept per directory and never evicted, matching the handles Open
// keeps: an epic's directory is visited for as long as the epic exists, and a
// mutex is small next to the clone it guards.
var (
	epicDirectoryLock  sync.Mutex
	epicDirectoryLocks = map[string]*sync.Mutex{}
)

func lockEpicDirectory(path string) func() {
	epicDirectoryLock.Lock()
	lock, ok := epicDirectoryLocks[path]
	if !ok {
		lock = &sync.Mutex{}
		epicDirectoryLocks[path] = lock
	}
	epicDirectoryLock.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (w AgentWorkspace) Ensure(ctx context.Context, epicID, repository string) (string, error) {
	if !repositoryNameValid(repository) {
		return "", fmt.Errorf("repository must use owner/name form, got %q", repository)
	}
	if !pathSegmentValid(epicID) {
		return "", fmt.Errorf("invalid epic ID %q", epicID)
	}
	defer lockEpicDirectory(filepath.Join(w.root, epicID))()
	return w.ensureLocked(ctx, epicID, repository)
}

// ensureLocked does the work Ensure guards. It exists so Diff can hold the lock
// across both the clone and the read that follows it: sync.Mutex is not
// reentrant, so Diff cannot lock and then call Ensure.
func (w AgentWorkspace) ensureLocked(
	ctx context.Context, epicID, repository string,
) (string, error) {
	path := filepath.Join(w.root, epicID, "repos", strings.ReplaceAll(repository, "/", "__"))
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		auth, err := authForURL("git@github.com:" + repository + ".git")
		if err != nil {
			return "", err
		}
		if _, err := git.PlainCloneContext(ctx, path, false, &git.CloneOptions{
			URL:  "git@github.com:" + repository + ".git",
			Auth: auth,
		}); err != nil {
			return "", fmt.Errorf("clone repository %q: %w", repository, err)
		}
	} else if err != nil {
		return "", err
	} else {
		if err := validateCheckout(path); err != nil {
			return "", err
		}
		repositoryHandle, err := git.PlainOpen(path)
		if err != nil {
			return "", err
		}
		worktree, err := repositoryHandle.Worktree()
		if err != nil {
			return "", err
		}
		// Drop unpublished work before fetching. Besides restoring the checkout,
		// this keeps a local-only commit out of go-git's fetch negotiation: the
		// remote cannot advertise an object it has never received.
		if err := resetWorktree(path, repositoryHandle, worktree); err != nil {
			return "", fmt.Errorf("reset repository %q: %w", repository, err)
		}
		// The refresh needs the same identity the clone used. Without it go-git
		// falls back to the SSH agent, which fails outright on a host whose
		// agent holds no keys — so a repository that cloned fine would stop
		// updating, and only on the second visit. Fetch and reset again so the
		// checkout is restored to the refreshed remote-tracking ref.
		remoteURL := "git@github.com:" + repository + ".git"
		auth, err := authForURL(remoteURL)
		if err != nil {
			return "", err
		}
		fetchErr := trustedRemote(repositoryHandle, remoteURL).FetchContext(ctx, &git.FetchOptions{Auth: auth})
		if fetchErr != nil && !errors.Is(fetchErr, git.NoErrAlreadyUpToDate) {
			return "", fmt.Errorf("refresh repository %q: %w", repository, fetchErr)
		}
		if err := resetWorktree(path, repositoryHandle, worktree); err != nil {
			return "", fmt.Errorf("reset repository %q: %w", repository, err)
		}
	}
	return path, nil
}

// resetWorktree discards a previous round's source edits and build outputs before
// refreshing the disposable checkout. go-git's Clean keeps ignored files, so remove
// every worktree entry except .git before restoring the remote-tracking commit.
func resetWorktree(path string, repository *git.Repository, worktree *git.Worktree) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	head, err := repository.Head()
	if err != nil {
		return err
	}
	if !head.Name().IsBranch() {
		return fmt.Errorf("checkout is not on a local branch")
	}
	remoteHead, err := repository.Reference(
		plumbing.NewRemoteReferenceName("origin", head.Name().Short()), true,
	)
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", head.Name().Short(), err)
	}
	return worktree.Reset(&git.ResetOptions{Commit: remoteHead.Hash(), Mode: git.HardReset})
}

// Purge removes the whole directory an epic holds here. That is both the clones
// this type maintains and the issue tree filestore.IssueTreeStore writes, because the two
// are wired to the same root and share the epic's directory.
//
// Ensure re-clones on demand, so the cost of purging too eagerly is a re-download
// rather than lost work — which is what makes viewing a finished epic's diff still
// correct after this, only slow.
func (w AgentWorkspace) Purge(epicID string) error {
	if !pathSegmentValid(epicID) {
		return fmt.Errorf("invalid epic ID %q", epicID)
	}
	path := filepath.Join(w.root, epicID)
	defer lockEpicDirectory(path)()
	return os.RemoveAll(path)
}

// Diff reports what head changes relative to base, computed from the local
// clone. It diffs from the merge base rather than straight from base's tip,
// so commits that landed on base after head branched are not reported as
// head's own changes — the same view a pull request shows.
func (w AgentWorkspace) Diff(
	ctx context.Context, epicID, repository, base, head string,
) (string, error) {
	if !repositoryNameValid(repository) {
		return "", fmt.Errorf("repository must use owner/name form, got %q", repository)
	}
	if !pathSegmentValid(epicID) {
		return "", fmt.Errorf("invalid epic ID %q", epicID)
	}
	// Held across the read as well as the clone: this runs on the TUI's
	// goroutine, and a round's Ensure landing between the two would have the
	// diff reading a repository mid-pull.
	defer lockEpicDirectory(filepath.Join(w.root, epicID))()
	path, err := w.ensureLocked(ctx, epicID, repository)
	if err != nil {
		return "", err
	}
	return diffBranches(path, base, head)
}

func diffBranches(path, base, head string) (string, error) {
	if err := validateCheckout(path); err != nil {
		return "", err
	}
	repositoryHandle, err := git.PlainOpen(path)
	if err != nil {
		return "", err
	}
	baseCommit, err := resolveCommit(repositoryHandle, base)
	if err != nil {
		return "", err
	}
	headCommit, err := resolveCommit(repositoryHandle, head)
	if err != nil {
		return "", err
	}
	mergeBases, err := baseCommit.MergeBase(headCommit)
	if err != nil {
		return "", fmt.Errorf("find merge base of %q and %q: %w", base, head, err)
	}
	from := baseCommit
	if len(mergeBases) > 0 {
		from = mergeBases[0]
	}
	patch, err := from.Patch(headCommit)
	if err != nil {
		return "", fmt.Errorf("diff %q against %q: %w", head, base, err)
	}
	return patch.String(), nil
}

// resolveCommit accepts a plain branch name. Ensure clones without checking
// out every branch, so a local name usually only exists under origin/.
func resolveCommit(
	repositoryHandle *git.Repository, reference string,
) (*object.Commit, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, fmt.Errorf("branch name is required")
	}
	candidates := []string{reference, "origin/" + reference}
	for _, candidate := range candidates {
		hash, err := repositoryHandle.ResolveRevision(plumbing.Revision(candidate))
		if err != nil {
			continue
		}
		return repositoryHandle.CommitObject(*hash)
	}
	return nil, fmt.Errorf("branch %q not found in the local clone", reference)
}

func repositoryNameValid(repository string) bool {
	parts := strings.Split(repository, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" &&
		!strings.Contains(repository, "..") && !strings.Contains(repository, "\\")
}

// pathSegmentValid rejects an ID that could escape the directory it names a
// segment of. IDs are ULIDs everywhere they are minted, so anything else is a
// caller error, not a case to accommodate.
func pathSegmentValid(id string) bool {
	return id != "" && id != "." && !strings.Contains(id, "/") &&
		!strings.Contains(id, "..")
}

var (
	_ agent_runtime.RepositoryWorkspace = AgentWorkspace{}
	_ application.RepositoryDiffer      = AgentWorkspace{}
)
