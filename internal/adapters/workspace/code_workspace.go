package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/repositorypath"
)

// CodeWorkspace maintains one writable clone per issue. A coding round is the
// only thing that writes to it, and it is separate from AgentWorkspace's
// read-only epic clones so that two issues in the same repository can run at
// once without sharing a working tree.
type CodeWorkspace struct {
	root string
	// remote maps owner/name to a clone URL. It is a field so that tests can
	// point at a local bare repository instead of reaching GitHub.
	remote func(repository string) string
}

func NewCodeWorkspace(root string) CodeWorkspace {
	return CodeWorkspace{root: root, remote: githubRemote}
}

func githubRemote(repository string) string {
	return "git@github.com:" + repository + ".git"
}

func (w CodeWorkspace) remoteURL(repository string) string {
	if w.remote == nil {
		return githubRemote(repository)
	}
	return w.remote(repository)
}

func (w CodeWorkspace) path(checkout agent_runtime.CodeCheckout) (string, error) {
	if !repositoryNameValid(checkout.Repository) {
		return "", fmt.Errorf("repository must use owner/name form, got %q", checkout.Repository)
	}
	for _, segment := range []string{checkout.EpicID, checkout.IssueID} {
		if !pathSegmentValid(segment) {
			return "", fmt.Errorf("invalid checkout segment %q", segment)
		}
	}
	return filepath.Join(
		w.root, checkout.EpicID, "issues", checkout.IssueID,
		repositorypath.Encode(checkout.Repository),
	), nil
}

// repositoryPath locates the epic-scoped clone InspectBranches uses. It is
// deliberately not tied to any issue — sharing an issue's own checkout is the
// bug this exists to avoid: a coding round's Checkout/CommitAll/Push and the
// sweep's clone/fetch touching the same directory with nothing serializing
// them corrupts the git object store. "repo" (singular) rather than
// AgentWorkspace's "repos" is deliberate, so the two are not mistaken for one
// another — they are different roots, different types, never shared.
func (w CodeWorkspace) repositoryPath(epicID, repository string) (string, error) {
	if !pathSegmentValid(epicID) {
		return "", fmt.Errorf("invalid epic ID %q", epicID)
	}
	if !repositoryNameValid(repository) {
		return "", fmt.Errorf("repository must use owner/name form, got %q", repository)
	}
	return filepath.Join(w.root, epicID, "repo", repositorypath.Encode(repository)), nil
}

// PurgeEpic removes every checkout cut under one epic. A coding round commits and
// pushes before it reports back, and closing an epic deletes the branches behind
// its abandoned work, so a finished epic's checkouts hold nothing the remote does
// not already have.
func (w CodeWorkspace) PurgeEpic(epicID string) error {
	if !pathSegmentValid(epicID) {
		return fmt.Errorf("invalid epic ID %q", epicID)
	}
	return os.RemoveAll(filepath.Join(w.root, epicID))
}

func (w CodeWorkspace) DefaultBranch(ctx context.Context, repository string) (string, error) {
	if !repositoryNameValid(repository) {
		return "", fmt.Errorf("repository must use owner/name form, got %q", repository)
	}
	auth, err := authForURL(w.remoteURL(repository))
	if err != nil {
		return "", err
	}
	remote := git.NewRemote(nil, &config.RemoteConfig{
		Name: "origin", URLs: []string{w.remoteURL(repository)},
	})
	references, err := remote.ListContext(ctx, &git.ListOptions{Auth: auth})
	if err != nil {
		return "", fmt.Errorf("list %s refs: %w", repository, err)
	}
	for _, reference := range references {
		if reference.Name() == plumbing.HEAD && reference.Target().IsBranch() {
			return reference.Target().Short(), nil
		}
	}
	return "", fmt.Errorf("%s does not report a default branch", repository)
}

func (w CodeWorkspace) open(checkout agent_runtime.CodeCheckout) (*git.Repository, string, error) {
	path, err := w.path(checkout)
	if err != nil {
		return nil, "", err
	}
	if err := validateCheckout(path); err != nil {
		return nil, "", fmt.Errorf("validate checkout for issue %s: %w", checkout.IssueID, err)
	}
	repository, err := git.PlainOpen(path)
	if err != nil {
		return nil, "", fmt.Errorf("open checkout for issue %s: %w", checkout.IssueID, err)
	}
	return repository, path, nil
}

func (w CodeWorkspace) Checkout(
	ctx context.Context, checkout agent_runtime.CodeCheckout, branch, base string,
) (string, error) {
	path, err := w.path(checkout)
	if err != nil {
		return "", err
	}
	repository, err := w.ensureClone(ctx, path, checkout.Repository)
	if err != nil {
		return "", err
	}
	worktree, err := repository.Worktree()
	if err != nil {
		return "", err
	}
	reference := plumbing.NewBranchReferenceName(branch)
	// A checkout can contain a commit from a round whose push or gate failed.
	// Reset an existing branch to the published branch, or to base when no
	// published branch exists, so a retry never publishes failed work.
	start, resolveErr := resolveCommit(repository, "origin/"+branch)
	if resolveErr != nil {
		start, resolveErr = resolveCommit(repository, "origin/"+base)
	}
	if resolveErr != nil {
		start, resolveErr = resolveCommit(repository, base)
	}
	if resolveErr != nil {
		return "", resolveErr
	}
	if err := repository.Storer.SetReference(
		plumbing.NewHashReference(reference, start.Hash),
	); err != nil {
		return "", err
	}
	// Force, because a round that was killed never reached the commit that
	// publishes its work: the stall guard, the runaway guard, a cancel, or the
	// process itself dying all leave the tree dirty with something no reviewer
	// will ever see. go-git refuses to check out over that (ErrUnstagedChanges),
	// and this runs before the round books its run record — so without the reset
	// one killed round wedges the issue permanently, and does it invisibly,
	// because there is no run for the failure to be recorded on.
	//
	// go-git's force is a harder reset than git's own: it also removes untracked
	// and ignored files, leaving the tree at exactly the branch. That is wanted
	// here — an untracked leftover would otherwise be folded into the next
	// round's commit by CommitAll's AddWithOptions{All} and published as if that
	// round had written it — but it does mean a build cache kept inside the
	// repository is paid for again every round.
	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: reference, Force: true,
	}); err != nil {
		return "", fmt.Errorf("check out %s: %w", branch, err)
	}
	return path, nil
}

func (w CodeWorkspace) ensureClone(
	ctx context.Context, path, repository string,
) (*git.Repository, error) {
	if _, err := os.Stat(path); err == nil {
		if err := validateCheckout(path); err != nil {
			return nil, err
		}
		existing, openErr := git.PlainOpen(path)
		if openErr != nil {
			// A failed or interrupted clone can leave an empty checkout directory.
			// It cannot serve any later round, so remove it and rebuild from origin.
			if !errors.Is(openErr, git.ErrRepositoryNotExists) {
				return nil, openErr
			}
			if err := os.RemoveAll(path); err != nil {
				return nil, err
			}
		} else {
			if err := w.fetch(ctx, existing, repository); err != nil {
				return nil, err
			}
			return existing, nil
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	auth, err := authForURL(w.remoteURL(repository))
	if err != nil {
		return nil, err
	}
	cloned, err := git.PlainCloneContext(ctx, path, false, &git.CloneOptions{
		URL: w.remoteURL(repository), Auth: auth,
	})
	if err != nil {
		return nil, fmt.Errorf("clone repository %q: %w", repository, err)
	}
	return cloned, nil
}

func (w CodeWorkspace) fetch(ctx context.Context, repository *git.Repository, name string) error {
	remoteURL := w.remoteURL(name)
	auth, err := authForURL(remoteURL)
	if err != nil {
		return err
	}
	err = trustedRemote(repository, remoteURL).FetchContext(ctx, &git.FetchOptions{Auth: auth})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("fetch repository %q: %w", name, err)
	}
	return nil
}

func (w CodeWorkspace) Resolve(
	checkout agent_runtime.CodeCheckout, ref string,
) (string, error) {
	repository, _, err := w.open(checkout)
	if err != nil {
		return "", err
	}
	commit, err := resolveCommit(repository, ref)
	if err != nil {
		return "", err
	}
	return commit.Hash.String(), nil
}

// CommitAll commits the whole working tree. AllowEmptyCommits is deliberate:
// a round that produced no change still leaves a commit, so every round has
// something a reviewer can point at rather than silently vanishing.
func (w CodeWorkspace) CommitAll(
	checkout agent_runtime.CodeCheckout, message string,
) (string, error) {
	repository, _, err := w.open(checkout)
	if err != nil {
		return "", err
	}
	worktree, err := repository.Worktree()
	if err != nil {
		return "", err
	}
	if err := worktree.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return "", fmt.Errorf("stage agent changes: %w", err)
	}
	hash, err := worktree.Commit(message, &git.CommitOptions{
		AllowEmptyCommits: true,
		Author: &object.Signature{
			Name: "go-merge", Email: "go-merge@local", When: time.Now().UTC(),
		},
	})
	if err != nil {
		return "", fmt.Errorf("commit agent changes: %w", err)
	}
	return hash.String(), nil
}

func (w CodeWorkspace) CommitsSince(
	checkout agent_runtime.CodeCheckout, base string,
) ([]agent_runtime.CommitInfo, error) {
	repository, _, err := w.open(checkout)
	if err != nil {
		return nil, err
	}
	baseCommit, err := resolveCommit(repository, base)
	if err != nil {
		return nil, err
	}
	head, err := repository.Head()
	if err != nil {
		return nil, err
	}
	current, err := repository.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}
	var commits []agent_runtime.CommitInfo
	for current.Hash != baseCommit.Hash {
		paths, err := changedPaths(current)
		if err != nil {
			return nil, err
		}
		commits = append([]agent_runtime.CommitInfo{
			{Hash: current.Hash.String(), Paths: paths},
		}, commits...)
		if current.NumParents() == 0 {
			break
		}
		parent, err := current.Parent(0)
		if err != nil {
			return nil, err
		}
		current = parent
	}
	return commits, nil
}

// changedPaths lists every path a commit touched, on both sides of a rename
// so that moving a file into a protected directory is visible to the gate.
func changedPaths(commit *object.Commit) ([]string, error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	var parentTree *object.Tree
	if commit.NumParents() > 0 {
		parent, err := commit.Parent(0)
		if err != nil {
			return nil, err
		}
		if parentTree, err = parent.Tree(); err != nil {
			return nil, err
		}
	}
	changes, err := object.DiffTree(parentTree, tree)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.From.Name != "" {
			paths = append(paths, change.From.Name)
		}
		if change.To.Name != "" && change.To.Name != change.From.Name {
			paths = append(paths, change.To.Name)
		}
	}
	return paths, nil
}

func (w CodeWorkspace) DescendsFrom(
	checkout agent_runtime.CodeCheckout, base string,
) (bool, error) {
	repository, _, err := w.open(checkout)
	if err != nil {
		return false, err
	}
	baseCommit, err := resolveCommit(repository, base)
	if err != nil {
		return false, err
	}
	head, err := repository.Head()
	if err != nil {
		return false, err
	}
	headCommit, err := repository.CommitObject(head.Hash())
	if err != nil {
		return false, err
	}
	// base must be an ancestor of head, not the other way round: the question
	// is whether head still builds on what the round started from.
	return baseCommit.IsAncestor(headCommit)
}

func (w CodeWorkspace) Push(
	ctx context.Context, checkout agent_runtime.CodeCheckout, branch string,
) error {
	repository, _, err := w.open(checkout)
	if err != nil {
		return err
	}
	remoteURL := w.remoteURL(checkout.Repository)
	auth, err := authForURL(remoteURL)
	if err != nil {
		return err
	}
	reference := plumbing.NewBranchReferenceName(branch)
	err = trustedRemote(repository, remoteURL).PushContext(ctx, &git.PushOptions{
		Auth:     auth,
		RefSpecs: []config.RefSpec{config.RefSpec(reference.String() + ":" + reference.String())},
		Atomic:   true,
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("push %s: %w", branch, err)
	}
	return nil
}

// DeleteBranch pushes an empty source to the branch's ref, which is how git
// asks a remote to drop it, then removes the working copy. The remote goes
// first: a local directory nobody can reach again is a smaller problem than a
// branch left on GitHub for work that was abandoned.
//
// It works through a detached remote rather than the checkout, so a branch can
// still be deleted after its working copy is gone.
func (w CodeWorkspace) DeleteBranch(
	ctx context.Context, checkout agent_runtime.CodeCheckout, branch string,
) error {
	path, err := w.path(checkout)
	if err != nil {
		return err
	}
	auth, err := authForURL(w.remoteURL(checkout.Repository))
	if err != nil {
		return err
	}
	// The detached remote needs a storer even though a delete uploads nothing:
	// go-git enumerates local refs while building the push request, and a nil
	// storer panics there.
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin", URLs: []string{w.remoteURL(checkout.Repository)},
	})
	reference := plumbing.NewBranchReferenceName(branch)
	err = remote.PushContext(ctx, &git.PushOptions{
		RemoteName: "origin",
		Auth:       auth,
		RefSpecs:   []config.RefSpec{config.RefSpec(":" + reference.String())},
		Atomic:     true,
	})
	// A branch that is already gone leaves nothing to update, which go-git
	// reports the same way it reports a no-op push — or, on some paths, as
	// its reference-not-found sentinel. Both mean the work is already done.
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) &&
		!errors.Is(err, plumbing.ErrReferenceNotFound) {
		return fmt.Errorf("delete %s: %w", branch, err)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove checkout for issue %s: %w", checkout.IssueID, err)
	}
	return nil
}

// Merge publishes head onto base by fast-forwarding base to it.
//
// It is only ever a fast-forward because keeping up with base is the coding
// agent's job: issue_coding.md tells it to merge the base branch in and resolve
// the conflicts itself, on its own branch, where it has the issue's context to
// resolve them with. So by the time a round is approved, its branch already
// contains base and there is nothing left to combine here.
//
// That makes "not a fast-forward" mean one thing — base moved on after the
// agent last caught up — and it is reported as ErrMergeConflict so the pull
// request goes back for another coding round rather than being merged by a
// host that has no idea what the code means.
func (w CodeWorkspace) Merge(
	ctx context.Context, checkout agent_runtime.CodeCheckout, head, base string,
) error {
	path, err := w.path(checkout)
	if err != nil {
		return err
	}
	// ensureClone fetches, so base is compared against the remote's current
	// tip rather than whatever this checkout last saw.
	repository, err := w.ensureClone(ctx, path, checkout.Repository)
	if err != nil {
		return err
	}
	baseCommit, err := resolveCommit(repository, "origin/"+base)
	if err != nil {
		return err
	}
	headCommit, err := resolveCommit(repository, "origin/"+head)
	if err != nil {
		return err
	}
	fastForward, err := baseCommit.IsAncestor(headCommit)
	if err != nil {
		return err
	}
	if !fastForward {
		return fmt.Errorf(
			"%w: %s has moved on since %s last merged it",
			agent_runtime.ErrMergeConflict, base, head,
		)
	}
	if err := repository.Storer.SetReference(plumbing.NewHashReference(
		plumbing.NewBranchReferenceName(base), headCommit.Hash,
	)); err != nil {
		return err
	}
	// Push does not force, so a base that moved between the check above and
	// here is rejected rather than overwritten. That race is the same stale
	// branch by another name, so it goes back for another round too. But a
	// push also fails for reasons that have nothing to do with the branch —
	// auth, network — and reporting those as a conflict would send the round
	// back to coding over an outage. Re-fetching and re-checking ancestry is
	// what tells the two apart.
	if pushErr := w.Push(ctx, checkout, base); pushErr != nil {
		if err := w.fetch(ctx, repository, checkout.Repository); err != nil {
			return errors.Join(pushErr, err)
		}
		movedBase, err := resolveCommit(repository, "origin/"+base)
		if err != nil {
			return errors.Join(pushErr, err)
		}
		stillFastForward, err := movedBase.IsAncestor(headCommit)
		if err != nil {
			return errors.Join(pushErr, err)
		}
		if !stillFastForward {
			return fmt.Errorf("%w: %s", agent_runtime.ErrMergeConflict, pushErr)
		}
		return pushErr
	}
	return nil
}

// InspectBranches fetches once and compares every head against base in memory.
//
// One fetch answers all of them because it updates every refs/remotes/origin
// ref, not just one branch — so the cost of a sweep is one round trip per
// repository rather than one per pull request.
//
// It runs against its own epic-scoped clone (repositoryPath), not any issue's
// checkout: this is a repository-wide operation, and sharing a coding round's
// own working tree here is what previously corrupted the git object store —
// nothing serialized this fetch against that round's Checkout/CommitAll/Push
// touching the identical directory.
//
// Every ref is read as origin/ so the answer describes what everyone else can
// see, rather than whatever this clone happened to be left on.
func (w CodeWorkspace) InspectBranches(
	ctx context.Context, epicID, repository, base string, heads []string,
) (map[string]agent_runtime.BranchState, error) {
	path, err := w.repositoryPath(epicID, repository)
	if err != nil {
		return nil, err
	}
	repositoryHandle, err := w.ensureClone(ctx, path, repository)
	if err != nil {
		return nil, err
	}
	baseCommit, err := resolveCommit(repositoryHandle, "origin/"+base)
	if err != nil {
		return nil, err
	}
	states := make(map[string]agent_runtime.BranchState, len(heads))
	for _, head := range heads {
		headCommit, err := resolveCommit(repositoryHandle, "origin/"+head)
		if err != nil {
			// A branch that is not on the remote has nothing to report. It was
			// deleted or never pushed, and neither is this sweep's problem.
			continue
		}
		contains, err := baseCommit.IsAncestor(headCommit)
		if err != nil {
			return nil, err
		}
		states[head] = agent_runtime.BranchState{
			Head: headCommit.Hash.String(), Behind: !contains,
		}
	}
	return states, nil
}

var _ agent_runtime.CodeWorkspace = CodeWorkspace{}
