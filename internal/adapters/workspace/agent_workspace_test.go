package workspace

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/storage/filesystem"
	cryptossh "golang.org/x/crypto/ssh"

	"github.com/go-git/go-billy/v5/osfs"
)

// localAgentWorkspace makes Ensure's hardwired github.com remote resolve to a
// local bare repository, so the clone-then-refresh cycle runs without a
// network. Ensure has no remote seam the way CodeWorkspace does, so the
// rerouting happens one layer down, in go-git's ssh transport.
func localAgentWorkspace(t *testing.T) (AgentWorkspace, string) {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "origin.git")
	if _, err := git.PlainInit(remote, true); err != nil {
		t.Fatal(err)
	}
	seedRemote(t, remote)
	fakeSSHIdentity(t)
	routeGitHubToLocal(t, "acme/widgets", remote)
	return NewAgentWorkspace(t.TempDir()), remote
}

// fakeSSHIdentity points HOME at an empty directory holding one throwaway
// key, so authForURL finds something parseable without touching (or depending
// on) the developer's real SSH setup.
func fakeSSHIdentity(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := cryptossh.MarshalPrivateKey(key, "go-merge test key")
	if err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(sshDir, "id_ed25519")
	if err := os.WriteFile(identity, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
}

// routeGitHubToLocal swaps go-git's ssh transport for an in-process server
// that answers the repository's github.com endpoint from a bare repository on
// disk. Local-path remotes use the file transport and are unaffected.
func routeGitHubToLocal(t *testing.T, repository, remote string) {
	t.Helper()
	endpoint, err := transport.NewEndpoint("git@github.com:" + repository + ".git")
	if err != nil {
		t.Fatal(err)
	}
	storage := filesystem.NewStorage(osfs.New(remote), cache.NewObjectLRUDefault())
	previous := client.Protocols["ssh"]
	client.InstallProtocol("ssh", server.NewClient(server.MapLoader{
		endpoint.String(): storage,
	}))
	t.Cleanup(func() { client.InstallProtocol("ssh", previous) })
}

func TestAgentWorkspace_Ensure_ShouldCloneTheRepository(t *testing.T) {
	// Arrange
	workspace, _ := localAgentWorkspace(t)

	// Act
	path, err := workspace.Ensure(context.Background(), "epic-1", "acme/widgets")

	// Assert: the clone lands at the epic's slot for the repository, with the
	// remote's content checked out.
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(workspace.root, "epic-1", "repos", "acme__widgets")
	if path != expected {
		t.Fatalf("expected clone at %s, got %s", expected, path)
	}
	if _, err := os.Stat(filepath.Join(path, "README.md")); err != nil {
		t.Fatalf("expected the seeded file in the clone: %v", err)
	}
}

func TestAgentWorkspace_Ensure_ShouldPullInsteadOfRecloning(t *testing.T) {
	// Arrange: a first Ensure has cloned, and the remote moves on afterwards.
	workspace, remote := localAgentWorkspace(t)
	path, err := workspace.Ensure(context.Background(), "epic-1", "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	// The marker lives inside .git because a reclone would rebuild that
	// directory, while a refresh leaves it alone — the worktree itself is fair
	// game for the pull to rewrite.
	marker := filepath.Join(path, ".git", "go-merge-marker")
	if err := os.WriteFile(marker, []byte("survives refresh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	advanceRemoteMaster(t, remote, "update.txt", "update\n", "base: later update")

	// Act
	refreshed, err := workspace.Ensure(context.Background(), "epic-1", "acme/widgets")

	// Assert: the new commit arrived in the same checkout, and a file only the
	// original clone could hold proves it was refreshed rather than recloned.
	if err != nil {
		t.Fatal(err)
	}
	if refreshed != path {
		t.Fatalf("expected the same checkout %s, got %s", path, refreshed)
	}
	if _, err := os.Stat(filepath.Join(path, "update.txt")); err != nil {
		t.Fatalf("expected the remote's new commit to arrive: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected the checkout to survive the refresh: %v", err)
	}
}

func TestAgentWorkspace_Ensure_ShouldResetChangesAndBuildOutputsBeforeRefreshing(t *testing.T) {
	// Arrange
	workspace, _ := localAgentWorkspace(t)
	path, err := workspace.Ensure(context.Background(), "epic-1", "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "build", "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "build", "cache", "output"), []byte("generated\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err = workspace.Ensure(context.Background(), "epic-1", "acme/widgets")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(path, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "seed\n" {
		t.Fatalf("expected tracked source reset to the remote copy, got %q", contents)
	}
	if _, err := os.Stat(filepath.Join(path, "build")); !os.IsNotExist(err) {
		t.Fatalf("expected generated output removed, got %v", err)
	}
}

func TestAgentWorkspace_Ensure_ShouldResetACommitMadeInTheLocalClone(t *testing.T) {
	// Arrange: a writable mount can leave a commit behind even though the agent
	// never published it. The remote then advances independently.
	workspace, remote := localAgentWorkspace(t)
	path, err := workspace.Ensure(context.Background(), "epic-1", "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	repository, err := git.PlainOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	commit(t, path, worktree, "unpublished.txt", "local\n", "agent: unpublished")
	advanceRemoteMaster(t, remote, "published.txt", "remote\n", "base: published")

	// Act
	_, err = workspace.Ensure(context.Background(), "epic-1", "acme/widgets")

	// Assert: refresh follows origin/master, discarding the local-only commit.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "unpublished.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected the unpublished commit to be discarded, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "published.txt")); err != nil {
		t.Fatalf("expected the remote commit to be checked out: %v", err)
	}
}

func TestAgentWorkspace_Ensure_ShouldIgnoreAMutatedCheckoutOrigin(t *testing.T) {
	// Arrange
	workspace, remote := localAgentWorkspace(t)
	path, err := workspace.Ensure(context.Background(), "epic-1", "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	evil := filepath.Join(t.TempDir(), "evil.git")
	if _, err := git.PlainInit(evil, true); err != nil {
		t.Fatal(err)
	}
	mutateOrigin(t, path, evil)
	advanceRemoteMaster(t, remote, "trusted.txt", "trusted\n", "base: trusted update")

	// Act
	_, err = workspace.Ensure(context.Background(), "epic-1", "acme/widgets")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "trusted.txt")); err != nil {
		t.Fatalf("expected the configured repository to be refreshed: %v", err)
	}
}

// Every round of an epic calls Ensure for each of its repositories, and rounds
// run concurrently. go-git has no equivalent of git's index.lock, so without
// serializing, two clones racing into one directory interleave their writes to
// the object store and neither ends up with a usable checkout.
func TestAgentWorkspace_Ensure_ShouldSurviveConcurrentCallers(t *testing.T) {
	// Arrange
	workspace, _ := localAgentWorkspace(t)

	// Act: the first caller through clones, the rest find it and pull.
	const callers = 8
	paths := make(chan string, callers)
	failures := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			path, err := workspace.Ensure(context.Background(), "epic-1", "acme/widgets")
			if err != nil {
				failures <- err
				return
			}
			paths <- path
		}()
	}
	group.Wait()
	close(paths)
	close(failures)

	// Assert
	for err := range failures {
		t.Fatalf("concurrent Ensure failed: %v", err)
	}
	expected := filepath.Join(workspace.root, "epic-1", "repos", "acme__widgets")
	for path := range paths {
		if path != expected {
			t.Fatalf("expected clone at %s, got %s", expected, path)
		}
	}
	// A torn object store shows up here: the checkout exists but cannot be read.
	repository, err := git.PlainOpen(expected)
	if err != nil {
		t.Fatalf("the clone is not a readable repository: %v", err)
	}
	if _, err := repository.Head(); err != nil {
		t.Fatalf("the clone has no usable head: %v", err)
	}
	if _, err := os.Stat(filepath.Join(expected, "README.md")); err != nil {
		t.Fatalf("expected the seeded file in the clone: %v", err)
	}
}

func TestAgentWorkspace_Ensure_ShouldRejectAnInvalidEpicID(t *testing.T) {
	// Arrange
	workspace := NewAgentWorkspace(t.TempDir())

	// Act & Assert: IDs that could escape the workspace root never reach git.
	for _, epicID := range []string{"", "epic/../escape", "a/b"} {
		if _, err := workspace.Ensure(context.Background(), epicID, "acme/widgets"); err == nil {
			t.Fatalf("expected epic ID %q to be rejected", epicID)
		}
	}
}

func TestAgentWorkspace_Ensure_ShouldRejectAnInvalidRepositoryName(t *testing.T) {
	// Arrange
	workspace := NewAgentWorkspace(t.TempDir())

	// Act & Assert
	invalid := []string{"", "widgets", "acme/", "/widgets", "acme/../widgets", "a/b/c"}
	for _, repository := range invalid {
		if _, err := workspace.Ensure(context.Background(), "epic-1", repository); err == nil {
			t.Fatalf("expected repository %q to be rejected", repository)
		}
	}
}

func TestDiffBranches_ShouldReportOnlyTheHeadBranchesChanges(t *testing.T) {
	// Arrange: base gains a commit after head branches off. A plain two-commit
	// diff would report that commit as a deletion by head; the merge base must
	// exclude it.
	path := t.TempDir()
	repository, worktree := initRepository(t, path)
	commit(t, path, worktree, "shared.txt", "shared\n", "base: shared file")
	branch(t, repository, worktree, "feature")
	commit(t, path, worktree, "feature.txt", "feature\n", "feature: add file")
	checkout(t, worktree, "master")
	commit(t, path, worktree, "later.txt", "later\n", "base: later file")

	// Act
	diff, err := diffBranches(path, "master", "feature")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "feature.txt") {
		t.Fatalf("expected the feature file in the diff, got:\n%s", diff)
	}
	if strings.Contains(diff, "later.txt") {
		t.Fatalf("base's later commit leaked into the diff:\n%s", diff)
	}
}

func TestDiffBranches_ShouldRejectAnUnknownBranch(t *testing.T) {
	// Arrange
	path := t.TempDir()
	_, worktree := initRepository(t, path)
	commit(t, path, worktree, "shared.txt", "shared\n", "base: shared file")

	// Act
	_, err := diffBranches(path, "master", "missing")

	// Assert
	if err == nil {
		t.Fatal("expected an unknown branch to be rejected")
	}
}

func initRepository(t *testing.T, path string) (*git.Repository, *git.Worktree) {
	t.Helper()
	repository, err := git.PlainInit(path, false)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	return repository, worktree
}

func commit(t *testing.T, path string, worktree *git.Worktree, name, body, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(path, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add(name); err != nil {
		t.Fatal(err)
	}
	_, err := worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name: "Test", Email: "test@example.com",
			When: time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func branch(t *testing.T, repository *git.Repository, worktree *git.Worktree, name string) {
	t.Helper()
	head, err := repository.Head()
	if err != nil {
		t.Fatal(err)
	}
	reference := plumbing.NewBranchReferenceName(name)
	if err := repository.Storer.SetReference(
		plumbing.NewHashReference(reference, head.Hash()),
	); err != nil {
		t.Fatal(err)
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Branch: reference}); err != nil {
		t.Fatal(err)
	}
}

func checkout(t *testing.T, worktree *git.Worktree, name string) {
	t.Helper()
	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(name),
	}); err != nil {
		t.Fatal(err)
	}
}
