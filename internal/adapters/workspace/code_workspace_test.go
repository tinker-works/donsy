package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
)

// localCodeWorkspace points the adapter at a bare repository on disk so the
// whole cut-commit-gate-push flow runs without touching GitHub.
func localCodeWorkspace(t *testing.T) (CodeWorkspace, string, agent_runtime.CodeCheckout) {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "origin.git")
	if _, err := git.PlainInit(remote, true); err != nil {
		t.Fatal(err)
	}
	seedRemote(t, remote)
	workspace := CodeWorkspace{
		root:   t.TempDir(),
		remote: func(string) string { return remote },
	}
	return workspace, remote, agent_runtime.CodeCheckout{
		EpicID: "epic-1", IssueID: "issue-1", Repository: "acme/widgets",
	}
}

func TestCodeWorkspace_Checkout_ShouldRecloneAnInvalidCheckoutDirectory(t *testing.T) {
	// Arrange
	workspace, _, checkout := localCodeWorkspace(t)
	path, err := workspace.path(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err = workspace.Checkout(context.Background(), checkout, "gm/issue-1", "master")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git.PlainOpen(path); err != nil {
		t.Fatalf("expected the invalid directory to be recloned, got %v", err)
	}
}

// seedRemote gives the bare repository one commit on master, because a clone
// of an empty repository has no HEAD to branch from.
func seedRemote(t *testing.T, remote string) {
	t.Helper()
	seed := t.TempDir()
	repository, worktree := initRepository(t, seed)
	commit(t, seed, worktree, "README.md", "seed\n", "seed: initial commit")
	if _, err := repository.CreateRemote(&config.RemoteConfig{
		Name: "origin", URLs: []string{remote},
	}); err != nil {
		t.Fatal(err)
	}
	branch := plumbing.NewBranchReferenceName("master")
	if err := repository.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec(branch.String() + ":" + branch.String())},
	}); err != nil {
		t.Fatal(err)
	}
	bare, err := git.PlainOpen(remote)
	if err != nil {
		t.Fatal(err)
	}
	if err := bare.Storer.SetReference(
		plumbing.NewSymbolicReference(plumbing.HEAD, branch),
	); err != nil {
		t.Fatal(err)
	}
}

// advanceRemoteMaster lands a commit on the remote's master through a separate
// clone, the way another writer would while a round is in flight.
func advanceRemoteMaster(t *testing.T, remote, name, body, message string) string {
	t.Helper()
	path := t.TempDir()
	repository, err := git.PlainClone(path, false, &git.CloneOptions{URL: remote})
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	commit(t, path, worktree, name, body, message)
	master := plumbing.NewBranchReferenceName("master")
	if err := repository.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec(master.String() + ":" + master.String())},
	}); err != nil {
		t.Fatal(err)
	}
	head, err := repository.Head()
	if err != nil {
		t.Fatal(err)
	}
	return head.Hash().String()
}

func advanceRemoteBranch(t *testing.T, remote, branch, name, body, message string) string {
	t.Helper()
	path := t.TempDir()
	repository, err := git.PlainClone(path, false, &git.CloneOptions{URL: remote})
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	remoteReference, err := repository.Reference(
		plumbing.NewRemoteReferenceName("origin", branch), true,
	)
	if err != nil {
		t.Fatal(err)
	}
	localReference := plumbing.NewBranchReferenceName(branch)
	if err := repository.Storer.SetReference(
		plumbing.NewHashReference(localReference, remoteReference.Hash()),
	); err != nil {
		t.Fatal(err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: localReference,
	}); err != nil {
		t.Fatal(err)
	}
	commit(t, path, worktree, name, body, message)
	ref := plumbing.NewBranchReferenceName(branch)
	if err := repository.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec(ref.String() + ":" + ref.String())},
	}); err != nil {
		t.Fatal(err)
	}
	head, err := repository.Head()
	if err != nil {
		t.Fatal(err)
	}
	return head.Hash().String()
}

// remoteBranchHash reads a branch tip straight from the bare remote, so
// assertions describe what everyone else can see rather than a local ref.
func remoteBranchHash(t *testing.T, remote, branch string) (string, error) {
	t.Helper()
	bare, err := git.PlainOpen(remote)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := bare.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		return "", err
	}
	return reference.Hash().String(), nil
}

func TestCodeWorkspace_DefaultBranch_ShouldReadTheRemoteHead(t *testing.T) {
	// Arrange
	workspace, _, _ := localCodeWorkspace(t)

	// Act
	branch, err := workspace.DefaultBranch(context.Background(), "acme/widgets")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if branch != "master" {
		t.Fatalf("expected master, got %q", branch)
	}
}

func TestCodeWorkspace_ShouldCutCommitAndPublishARound(t *testing.T) {
	// Arrange
	workspace, remote, checkout := localCodeWorkspace(t)

	// Act: cut the branch, let the "agent" write a file, then commit and push.
	path, err := workspace.Checkout(context.Background(), checkout, "go-merge/issue-1", "master")
	if err != nil {
		t.Fatal(err)
	}
	base, err := workspace.Resolve(checkout, "master")
	if err != nil {
		t.Fatal(err)
	}
	widget := filepath.Join(path, "widget.go")
	if err := os.WriteFile(widget, []byte("package widget\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.CommitAll(checkout, "ai: round 1 on issue issue-1"); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Push(context.Background(), checkout, "go-merge/issue-1"); err != nil {
		t.Fatal(err)
	}

	// Assert: the branch exists on the remote with the agent's file.
	bare, err := git.PlainOpen(remote)
	if err != nil {
		t.Fatal(err)
	}
	published := plumbing.NewBranchReferenceName("go-merge/issue-1")
	if _, err := bare.Reference(published, true); err != nil {
		t.Fatalf("branch was not published: %v", err)
	}
	commits, err := workspace.CommitsSince(checkout, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || len(commits[0].Paths) != 1 || commits[0].Paths[0] != "widget.go" {
		t.Fatalf("unexpected commits: %+v", commits)
	}
}

func TestCodeWorkspace_CommitAll_ShouldCommitEvenWhenNothingChanged(t *testing.T) {
	// Arrange: a round that produced nothing still has to leave something a
	// reviewer can point at.
	workspace, _, checkout := localCodeWorkspace(t)
	if _, err := workspace.Checkout(
		context.Background(), checkout, "go-merge/issue-1", "master",
	); err != nil {
		t.Fatal(err)
	}
	base, err := workspace.Resolve(checkout, "master")
	if err != nil {
		t.Fatal(err)
	}

	// Act
	head, err := workspace.CommitAll(checkout, "ai: round 1 on issue issue-1")
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if head == base {
		t.Fatal("expected an empty commit to advance head")
	}
	commits, err := workspace.CommitsSince(checkout, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || len(commits[0].Paths) != 0 {
		t.Fatalf("expected one commit touching nothing, got %+v", commits)
	}
}

func TestCodeWorkspace_CommitsSince_ShouldReportProtectedPathsToTheGate(t *testing.T) {
	// Arrange: this is the wiring that makes the push gate able to see a
	// workflow edit at all.
	workspace, _, checkout := localCodeWorkspace(t)
	path, err := workspace.Checkout(context.Background(), checkout, "go-merge/issue-1", "master")
	if err != nil {
		t.Fatal(err)
	}
	base, err := workspace.Resolve(checkout, "master")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	workflow := filepath.Join(path, ".github", "workflows", "ci.yml")
	if err := os.WriteFile(workflow, []byte("on: push\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.CommitAll(checkout, "ai: round 1"); err != nil {
		t.Fatal(err)
	}

	// Act
	commits, err := workspace.CommitsSince(checkout, base)
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(commits) != 1 || !strings.HasPrefix(commits[0].Paths[0], ".github/") {
		t.Fatalf("expected the workflow path to be reported, got %+v", commits)
	}
}

func TestCodeWorkspace_DescendsFrom_ShouldDetectRewrittenHistory(t *testing.T) {
	// Arrange
	workspace, _, checkout := localCodeWorkspace(t)
	path, err := workspace.Checkout(context.Background(), checkout, "go-merge/issue-1", "master")
	if err != nil {
		t.Fatal(err)
	}
	base, err := workspace.Resolve(checkout, "master")
	if err != nil {
		t.Fatal(err)
	}
	widget := filepath.Join(path, "widget.go")
	if err := os.WriteFile(widget, []byte("package widget\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.CommitAll(checkout, "ai: round 1"); err != nil {
		t.Fatal(err)
	}

	// Act
	ok, err := workspace.DescendsFrom(checkout, base)
	if err != nil {
		t.Fatal(err)
	}

	// Assert: an ordinary round still builds on its base.
	if !ok {
		t.Fatal("expected the round's head to descend from its base")
	}

	// Act: build a commit on a sibling branch, which the issue branch has
	// never seen. Asking whether head descends from it is the same question
	// the gate asks after a history rewrite moved the base out of ancestry.
	repository, err := git.PlainOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	sibling := plumbing.NewBranchReferenceName("sibling")
	baseHash := plumbing.NewHash(base)
	if err := repository.Storer.SetReference(
		plumbing.NewHashReference(sibling, baseHash),
	); err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Branch: sibling}); err != nil {
		t.Fatal(err)
	}
	commit(t, path, worktree, "sibling.txt", "sibling\n", "sibling commit")
	siblingHead, err := repository.Head()
	if err != nil {
		t.Fatal(err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("go-merge/issue-1"),
	}); err != nil {
		t.Fatal(err)
	}

	// Assert
	diverged, err := workspace.DescendsFrom(checkout, siblingHead.Hash().String())
	if err != nil {
		t.Fatal(err)
	}
	if diverged {
		t.Fatal("expected a base outside head's ancestry to be reported as diverged")
	}
}

func TestCodeWorkspace_Merge_ShouldFastForwardBaseToHead(t *testing.T) {
	// Arrange: a published round whose branch still contains base's tip.
	workspace, remote, checkout := localCodeWorkspace(t)
	path, err := workspace.Checkout(context.Background(), checkout, "go-merge/issue-1", "master")
	if err != nil {
		t.Fatal(err)
	}
	widget := filepath.Join(path, "widget.go")
	if err := os.WriteFile(widget, []byte("package widget\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	head, err := workspace.CommitAll(checkout, "ai: round 1 on issue issue-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.Push(context.Background(), checkout, "go-merge/issue-1"); err != nil {
		t.Fatal(err)
	}

	// Act
	err = workspace.Merge(context.Background(), checkout, "go-merge/issue-1", "master")

	// Assert: the remote's base ref moved to the round's head commit.
	if err != nil {
		t.Fatal(err)
	}
	master, err := remoteBranchHash(t, remote, "master")
	if err != nil {
		t.Fatal(err)
	}
	if master != head {
		t.Fatalf("expected remote master at %s, got %s", head, master)
	}
}

func TestCodeWorkspace_Merge_ShouldUseTheRemoteHeadTip(t *testing.T) {
	// Arrange: the published head advances after this checkout last fetched it.
	workspace, remote, checkout := localCodeWorkspace(t)
	path, err := workspace.Checkout(context.Background(), checkout, "go-merge/issue-1", "master")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "widget.go"), []byte("package widget\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.CommitAll(checkout, "ai: round 1 on issue issue-1"); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Push(context.Background(), checkout, "go-merge/issue-1"); err != nil {
		t.Fatal(err)
	}
	advancedHead := advanceRemoteBranch(
		t, remote, "go-merge/issue-1", "later.go", "package later\n", "agent: later round",
	)

	// Act
	err = workspace.Merge(context.Background(), checkout, "go-merge/issue-1", "master")

	// Assert: Merge fast-forwards base to the current remote head, not the stale
	// local branch ref left by the earlier Push.
	if err != nil {
		t.Fatal(err)
	}
	master, err := remoteBranchHash(t, remote, "master")
	if err != nil {
		t.Fatal(err)
	}
	if master != advancedHead {
		t.Fatalf("expected remote master at the advanced head %s, got %s", advancedHead, master)
	}
}

func TestCodeWorkspace_Merge_ShouldReportAMovedBaseAsAConflict(t *testing.T) {
	// Arrange: base gains a commit after the round's branch was cut, so the
	// branch no longer contains base and cannot be fast-forwarded onto it.
	workspace, remote, checkout := localCodeWorkspace(t)
	path, err := workspace.Checkout(context.Background(), checkout, "go-merge/issue-1", "master")
	if err != nil {
		t.Fatal(err)
	}
	widget := filepath.Join(path, "widget.go")
	if err := os.WriteFile(widget, []byte("package widget\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.CommitAll(checkout, "ai: round 1 on issue issue-1"); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Push(context.Background(), checkout, "go-merge/issue-1"); err != nil {
		t.Fatal(err)
	}
	hotfix := advanceRemoteMaster(t, remote, "hotfix.txt", "hotfix\n", "base: hotfix")

	// Act
	err = workspace.Merge(context.Background(), checkout, "go-merge/issue-1", "master")

	// Assert: the stale branch goes back for another round, and base is left
	// exactly where the other writer put it.
	if !errors.Is(err, agent_runtime.ErrMergeConflict) {
		t.Fatalf("expected ErrMergeConflict, got %v", err)
	}
	master, err := remoteBranchHash(t, remote, "master")
	if err != nil {
		t.Fatal(err)
	}
	if master != hotfix {
		t.Fatalf("expected remote master untouched at %s, got %s", hotfix, master)
	}
}

func TestCodeWorkspace_DeleteBranch_ShouldRemoveRemoteBranchAndLocalCheckout(t *testing.T) {
	// Arrange: a published branch with a local working copy.
	workspace, remote, checkout := localCodeWorkspace(t)
	path, err := workspace.Checkout(context.Background(), checkout, "go-merge/issue-1", "master")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.CommitAll(checkout, "ai: round 1 on issue issue-1"); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Push(context.Background(), checkout, "go-merge/issue-1"); err != nil {
		t.Fatal(err)
	}

	// Act
	err = workspace.DeleteBranch(context.Background(), checkout, "go-merge/issue-1")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	_, err = remoteBranchHash(t, remote, "go-merge/issue-1")
	if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		t.Fatalf("expected the remote branch to be gone, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected the local checkout to be removed, got %v", err)
	}
}

func TestCodeWorkspace_DeleteBranch_ShouldSucceedWhenTheBranchIsAlreadyGone(t *testing.T) {
	// Arrange: the branch was already deleted, as happens when a close is
	// retried after a partial failure.
	workspace, _, checkout := localCodeWorkspace(t)
	if _, err := workspace.Checkout(
		context.Background(), checkout, "go-merge/issue-1", "master",
	); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Push(context.Background(), checkout, "go-merge/issue-1"); err != nil {
		t.Fatal(err)
	}
	if err := workspace.DeleteBranch(context.Background(), checkout, "go-merge/issue-1"); err != nil {
		t.Fatal(err)
	}

	// Act
	err := workspace.DeleteBranch(context.Background(), checkout, "go-merge/issue-1")

	// Assert
	if err != nil {
		t.Fatalf("expected deleting a missing branch to succeed, got %v", err)
	}
}

func TestCodeWorkspace_InspectBranches_ShouldReportWhereEachHeadStands(t *testing.T) {
	// Arrange: one branch cut before base moved, one sitting exactly on base's
	// new tip, and one that exists nowhere on the remote.
	workspace, remote, checkout := localCodeWorkspace(t)
	path, err := workspace.Checkout(context.Background(), checkout, "go-merge/stale", "master")
	if err != nil {
		t.Fatal(err)
	}
	widget := filepath.Join(path, "widget.go")
	if err := os.WriteFile(widget, []byte("package widget\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.CommitAll(checkout, "ai: round 1 on issue issue-1"); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Push(context.Background(), checkout, "go-merge/stale"); err != nil {
		t.Fatal(err)
	}
	staleHead, err := workspace.Resolve(checkout, "go-merge/stale")
	if err != nil {
		t.Fatal(err)
	}
	masterHead := advanceRemoteMaster(t, remote, "hotfix.txt", "hotfix\n", "base: hotfix")
	bare, err := git.PlainOpen(remote)
	if err != nil {
		t.Fatal(err)
	}
	if err := bare.Storer.SetReference(plumbing.NewHashReference(
		plumbing.NewBranchReferenceName("go-merge/fresh"), plumbing.NewHash(masterHead),
	)); err != nil {
		t.Fatal(err)
	}

	// Act
	states, err := workspace.InspectBranches(
		context.Background(), checkout.EpicID, checkout.Repository, "master",
		[]string{"go-merge/stale", "go-merge/fresh", "go-merge/ghost"},
	)

	// Assert: the stale head is behind, the fresh one still fast-forwards, and
	// the branch missing from the remote is omitted rather than invented.
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("expected two branch states, got %+v", states)
	}
	stale := states["go-merge/stale"]
	if stale.Head != staleHead || !stale.Behind {
		t.Fatalf("expected stale head %s to be behind, got %+v", staleHead, stale)
	}
	fresh := states["go-merge/fresh"]
	if fresh.Head != masterHead || fresh.Behind {
		t.Fatalf("expected fresh head %s to fast-forward, got %+v", masterHead, fresh)
	}
}

// TestCodeWorkspace_InspectBranches_ShouldNotTouchAnIssuesOwnCheckout is the
// regression guard for the bug InspectBranches used to have: it took a
// CodeCheckout and reused whichever issue's directory it was handed, racing
// that issue's own coding round (Checkout/CommitAll/Push) on the identical
// path. It now runs against its own epic-scoped clone instead.
func TestCodeWorkspace_InspectBranches_ShouldNotTouchAnIssuesOwnCheckout(t *testing.T) {
	// Arrange: an issue mid-coding-round, checked out on its own branch.
	workspace, _, checkout := localCodeWorkspace(t)
	issuePath, err := workspace.Checkout(context.Background(), checkout, "go-merge/issue-1", "master")
	if err != nil {
		t.Fatal(err)
	}
	issueHeadBefore, err := workspace.Resolve(checkout, "go-merge/issue-1")
	if err != nil {
		t.Fatal(err)
	}

	// Act
	if _, err := workspace.InspectBranches(
		context.Background(), checkout.EpicID, checkout.Repository, "master",
		[]string{"go-merge/issue-1"},
	); err != nil {
		t.Fatal(err)
	}

	// Assert: the issue's own checkout is exactly where it was, on the same
	// commit — InspectBranches did not clone or fetch into it.
	issueHeadAfter, err := workspace.Resolve(checkout, "go-merge/issue-1")
	if err != nil {
		t.Fatal(err)
	}
	if issueHeadAfter != issueHeadBefore {
		t.Fatalf("issue checkout moved: was %s, now %s", issueHeadBefore, issueHeadAfter)
	}
	repositoryPath, err := workspace.repositoryPath(checkout.EpicID, checkout.Repository)
	if err != nil {
		t.Fatal(err)
	}
	if repositoryPath == issuePath {
		t.Fatalf(
			"expected InspectBranches's clone to live apart from the issue checkout, "+
				"both resolved to %q",
			issuePath,
		)
	}
	if _, err := os.Stat(repositoryPath); err != nil {
		t.Fatalf("expected InspectBranches to have cloned its own directory: %v", err)
	}
}
