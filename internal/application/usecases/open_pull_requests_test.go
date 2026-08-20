package usecases

import (
	"context"
	"fmt"
	"testing"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

type fakeCodeWorkspace struct {
	defaultBranch string
	defaultErr    error
	checkoutErr   error
	pushErr       error
	pushed        []string
	commits       []agent_runtime.CommitInfo
	descends      bool
	head          string
	resolved      map[string]string
	committed     []string
	deleteErr     error
	deleted       []string
	mergeErr      error
	merged        []string
	branchState   agent_runtime.BranchState
	inspectErr    error
	inspected     [][]string
	purgedEpics   []string
	purgeErr      error
}

func newFakeCodeWorkspace() *fakeCodeWorkspace {
	return &fakeCodeWorkspace{defaultBranch: "main", descends: true, head: "head1234"}
}

func (w *fakeCodeWorkspace) PurgeEpic(epicID string) error {
	w.purgedEpics = append(w.purgedEpics, epicID)
	return w.purgeErr
}

func (w *fakeCodeWorkspace) DefaultBranch(context.Context, string) (string, error) {
	return w.defaultBranch, w.defaultErr
}

func (w *fakeCodeWorkspace) Checkout(
	context.Context, agent_runtime.CodeCheckout, string, string,
) (string, error) {
	return "/checkouts/repo", w.checkoutErr
}

func (w *fakeCodeWorkspace) Resolve(_ agent_runtime.CodeCheckout, ref string) (string, error) {
	if hash, ok := w.resolved[ref]; ok {
		return hash, nil
	}
	return "base1234", nil
}

func (w *fakeCodeWorkspace) CommitAll(
	_ agent_runtime.CodeCheckout, message string,
) (string, error) {
	w.committed = append(w.committed, message)
	return w.head, nil
}

func (w *fakeCodeWorkspace) CommitsSince(
	_ agent_runtime.CodeCheckout, _ string,
) ([]agent_runtime.CommitInfo, error) {
	return w.commits, nil
}

func (w *fakeCodeWorkspace) DescendsFrom(_ agent_runtime.CodeCheckout, _ string) (bool, error) {
	return w.descends, nil
}

func (w *fakeCodeWorkspace) Push(
	_ context.Context, _ agent_runtime.CodeCheckout, branch string,
) error {
	if w.pushErr != nil {
		return w.pushErr
	}
	w.pushed = append(w.pushed, branch)
	return nil
}

func (w *fakeCodeWorkspace) InspectBranches(
	_ context.Context, _, _, _ string, heads []string,
) (map[string]agent_runtime.BranchState, error) {
	if w.inspectErr != nil {
		return nil, w.inspectErr
	}
	// Recording each call as a batch is what lets a test assert that one fetch
	// covered every branch in a repository.
	w.inspected = append(w.inspected, heads)
	states := map[string]agent_runtime.BranchState{}
	for _, head := range heads {
		states[head] = w.branchState
	}
	return states, nil
}

func (w *fakeCodeWorkspace) Merge(
	_ context.Context, _ agent_runtime.CodeCheckout, head, base string,
) error {
	if w.mergeErr != nil {
		return w.mergeErr
	}
	w.merged = append(w.merged, head+" -> "+base)
	return nil
}

func (w *fakeCodeWorkspace) DeleteBranch(
	_ context.Context, _ agent_runtime.CodeCheckout, branch string,
) error {
	if w.deleteErr != nil {
		return w.deleteErr
	}
	w.deleted = append(w.deleted, branch)
	return nil
}

func readyEpic() epicpkg.Epic {
	return epicpkg.Epic{
		ID: "epic-1", Title: "Improve workflow", Assignee: "owner",
		State: epicpkg.EpicStateReady, Repositories: []string{"acme/widgets"},
		Issues: []epicpkg.Issue{
			{ID: "root", Title: "Improve workflow", State: epicpkg.IssueStateOpen},
			{
				ID: "child-1", Title: "Add widget", ParentID: "root",
				Repository: "acme/widgets", State: epicpkg.IssueStateOpen,
			},
		},
	}
}

func TestOpenPullRequestsUseCase_ShouldCutABranchPerIssue(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: readyEpic()}
	code := newFakeCodeWorkspace()
	useCase := &OpenPullRequestsUseCase{
		factory: &fakeFactory{workspace: workspace}, code: code,
	}

	// Act
	opened, err := useCase.Handle(context.Background(), OpenPullRequestsCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	})

	// Assert: the root issue has no repository, so only the child opens.
	if err != nil {
		t.Fatal(err)
	}
	if opened != 1 {
		t.Fatalf("expected one pull request, got %d", opened)
	}
	// The epic names no prefix, so the branch is the namespace, the issue title
	// and the issue ID.
	branch := "gm/add-widget-child-1"
	if len(code.pushed) != 1 || code.pushed[0] != branch {
		t.Fatalf("unexpected pushes: %v", code.pushed)
	}
	if len(workspace.detail.PullRequests) != 1 {
		t.Fatalf("expected one record, got %+v", workspace.detail.PullRequests)
	}
	pullRequest := workspace.detail.PullRequests[0]
	if pullRequest.Head != branch || pullRequest.Base != "main" {
		t.Fatalf("unexpected branches: %+v", pullRequest)
	}
	// Cutting a branch means the issue is owed a coding round, not that it is
	// ready to merge.
	if workspace.detail.Issues[1].State != epicpkg.IssueStateCoding {
		t.Fatalf("expected the issue to move to Coding, got %q", workspace.detail.Issues[1].State)
	}
}

func TestOpenPullRequestsUseCase_ShouldNameBranchesAfterTheEpicPrefix(t *testing.T) {
	// Arrange: the prefix is what ties a branch back to the tracker item that
	// asked for the work.
	detail := readyEpic()
	if err := detail.SetBranchPrefix("JIRA-123"); err != nil {
		t.Fatal(err)
	}
	workspace := &fakeWorkspace{detail: detail}
	code := newFakeCodeWorkspace()
	useCase := &OpenPullRequestsUseCase{
		factory: &fakeFactory{workspace: workspace}, code: code,
	}

	// Act
	if _, err := useCase.Handle(context.Background(), OpenPullRequestsCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	}); err != nil {
		t.Fatal(err)
	}

	// Assert
	want := "gm/jira-123-add-widget-child-1"
	if len(code.pushed) != 1 || code.pushed[0] != want {
		t.Fatalf("unexpected pushes: %v, want %q", code.pushed, want)
	}
	if head := workspace.detail.PullRequests[0].Head; head != want {
		t.Fatalf("recorded head %q, want %q", head, want)
	}
}

func TestOpenPullRequestsUseCase_ShouldSkipAnEpicThatIsNotReady(t *testing.T) {
	// Arrange
	detail := readyEpic()
	detail.State = epicpkg.EpicStateReview
	code := newFakeCodeWorkspace()
	useCase := &OpenPullRequestsUseCase{
		factory: &fakeFactory{workspace: &fakeWorkspace{detail: detail}}, code: code,
	}

	// Act
	opened, err := useCase.Handle(context.Background(), OpenPullRequestsCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	})

	// Assert
	if err != nil || opened != 0 {
		t.Fatalf("expected nothing to open, got %d (%v)", opened, err)
	}
	if len(code.pushed) != 0 {
		t.Fatalf("expected no branches, got %v", code.pushed)
	}
}

func TestOpenPullRequestsUseCase_ShouldBeIdempotent(t *testing.T) {
	// Arrange: the sweep calls this every tick, so a second pass must not cut
	// a second branch for the same issue.
	detail := readyEpic()
	detail.Issues[1].State = epicpkg.IssueStatePR
	detail.PullRequests = []epicpkg.PullRequest{{
		ID: "pr-1", IssueID: "child-1", Title: "Add widget",
		Status: epicpkg.PullRequestOpen, Repository: "acme/widgets",
		Head: "gm/add-widget-child-1", Base: "main",
	}}
	code := newFakeCodeWorkspace()
	useCase := &OpenPullRequestsUseCase{
		factory: &fakeFactory{workspace: &fakeWorkspace{detail: detail}}, code: code,
	}

	// Act
	opened, err := useCase.Handle(context.Background(), OpenPullRequestsCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	})

	// Assert
	if err != nil || opened != 0 {
		t.Fatalf("expected nothing to open, got %d (%v)", opened, err)
	}
}

// nestedEpic puts a prerequisite under the root's own child, which is the
// shallowest tree the block applies to.
func nestedEpic(childState epicpkg.IssueState) epicpkg.Epic {
	detail := readyEpic()
	detail.Issues = append(detail.Issues, epicpkg.Issue{
		ID: "grandchild", Title: "Extract total", ParentID: "child-1",
		Repository: "acme/widgets", State: childState,
	})
	return detail
}

func TestOpenPullRequestsUseCase_ShouldNotCutABranchForABlockedIssue(t *testing.T) {
	// Arrange: a branch for the parent would be cut from a base that does not
	// carry the work it integrates yet.
	workspace := &fakeWorkspace{detail: nestedEpic(epicpkg.IssueStateOpen)}
	code := newFakeCodeWorkspace()
	useCase := &OpenPullRequestsUseCase{
		factory: &fakeFactory{workspace: workspace}, code: code,
	}

	// Act
	opened, err := useCase.Handle(context.Background(), OpenPullRequestsCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if opened != 1 {
		t.Fatalf("expected only the prerequisite to open, got %d", opened)
	}
	if len(code.pushed) != 1 || code.pushed[0] != "gm/extract-total-grandchild" {
		t.Fatalf("unexpected pushes: %v", code.pushed)
	}
}

func TestOpenPullRequestsUseCase_ShouldCutTheParentBranchOnceTheChildLands(t *testing.T) {
	// Arrange: the sweep runs every tick, so the tick after the child merged is
	// what opens the issue above it — from a base that now carries its work.
	detail := nestedEpic(epicpkg.IssueStateMerged)
	detail.PullRequests = []epicpkg.PullRequest{{
		ID: "pr-1", IssueID: "grandchild", Title: "Extract total",
		Status: epicpkg.PullRequestMerged, Repository: "acme/widgets",
		Head: "gm/extract-total-grandchild", Base: "main",
	}}
	workspace := &fakeWorkspace{detail: detail}
	code := newFakeCodeWorkspace()
	useCase := &OpenPullRequestsUseCase{
		factory: &fakeFactory{workspace: workspace}, code: code,
	}

	// Act
	opened, err := useCase.Handle(context.Background(), OpenPullRequestsCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if opened != 1 {
		t.Fatalf("expected the parent to open, got %d", opened)
	}
	want := "gm/add-widget-child-1"
	if len(code.pushed) != 1 || code.pushed[0] != want {
		t.Fatalf("unexpected pushes: %v, want %q", code.pushed, want)
	}
}

func TestOpenPullRequestsUseCase_ShouldRecomputeTheBlockedFlag(t *testing.T) {
	// Arrange: merging a blocker has to clear the flag on what was waiting for
	// it, and nothing else goes back to correct the record.
	detail := nestedEpic(epicpkg.IssueStateMerged)
	detail.Issues[1].State = epicpkg.IssueStateCoding
	detail.PullRequests = []epicpkg.PullRequest{
		{
			ID: "pr-1", IssueID: "child-1", Title: "Add widget",
			Status: epicpkg.PullRequestOpen, Repository: "acme/widgets",
			Head: "gm/add-widget-child-1", Base: "main",
			Flags: []epicpkg.PullRequestFlag{epicpkg.FlagBlocked},
		},
		{
			ID: "pr-2", IssueID: "grandchild", Title: "Extract total",
			Status: epicpkg.PullRequestMerged, Repository: "acme/widgets",
			Head: "gm/extract-total-grandchild", Base: "main",
		},
	}
	workspace := &fakeWorkspace{detail: detail}
	useCase := &OpenPullRequestsUseCase{
		factory: &fakeFactory{workspace: workspace}, code: newFakeCodeWorkspace(),
	}

	// Act
	if _, err := useCase.Handle(context.Background(), OpenPullRequestsCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	}); err != nil {
		t.Fatal(err)
	}

	// Assert
	var parent epicpkg.PullRequest
	for _, pullRequest := range workspace.detail.PullRequests {
		if pullRequest.IssueID == "child-1" {
			parent = pullRequest
		}
	}
	if parent.HasFlag(epicpkg.FlagBlocked) {
		t.Fatalf("expected the merged child to clear the flag, got %+v", parent.Flags)
	}
}

func TestOpenPullRequestsUseCase_ShouldReportAFailedResync(t *testing.T) {
	// Arrange: the flags are what the loop reads to know an issue is waiting, so
	// a sweep that could not correct them must not go on to cut branches as if
	// it had.
	detail := nestedEpic(epicpkg.IssueStateMerged)
	detail.Issues[1].State = epicpkg.IssueStateCoding
	detail.PullRequests = []epicpkg.PullRequest{{
		ID: "pr-1", IssueID: "child-1", Title: "Add widget",
		Status: epicpkg.PullRequestOpen, Repository: "acme/widgets",
		Head: "gm/add-widget-child-1", Base: "main",
		Flags: []epicpkg.PullRequestFlag{epicpkg.FlagBlocked},
	}}
	code := newFakeCodeWorkspace()
	useCase := &OpenPullRequestsUseCase{
		factory: &fakeFactory{workspace: &fakeWorkspace{
			detail: detail, updateErr: fmt.Errorf("remote rejected"),
		}},
		code: code,
	}

	// Act
	_, err := useCase.Handle(context.Background(), OpenPullRequestsCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected the failed resync to be reported")
	}
	if len(code.pushed) != 0 {
		t.Fatalf("expected no branches, got %v", code.pushed)
	}
}

func TestOpenPullRequestsUseCase_ShouldNotWriteWhenTheFlagsAlreadyAgree(t *testing.T) {
	// Arrange: the tracker checkout refuses an empty commit, so a sweep with
	// nothing to correct must not reach UpdateEpic at all.
	detail := readyEpic()
	detail.Issues[1].State = epicpkg.IssueStatePR
	detail.PullRequests = []epicpkg.PullRequest{{
		ID: "pr-1", IssueID: "child-1", Title: "Add widget",
		Status: epicpkg.PullRequestOpen, Repository: "acme/widgets",
		Head: "gm/add-widget-child-1", Base: "main",
	}}
	workspace := &fakeWorkspace{detail: detail}
	useCase := &OpenPullRequestsUseCase{
		factory: &fakeFactory{workspace: workspace}, code: newFakeCodeWorkspace(),
	}

	// Act
	if _, err := useCase.Handle(context.Background(), OpenPullRequestsCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	}); err != nil {
		t.Fatal(err)
	}

	// Assert
	if workspace.updatedEpicID != "" {
		t.Fatalf("expected no write, got update of %q", workspace.updatedEpicID)
	}
}

func TestOpenPullRequestsUseCase_ShouldNotRecordWhenThePushFails(t *testing.T) {
	// Arrange: a record pointing at a branch that was never published is a
	// pull request nobody can check out.
	workspace := &fakeWorkspace{detail: readyEpic()}
	code := newFakeCodeWorkspace()
	code.pushErr = fmt.Errorf("remote rejected")
	useCase := &OpenPullRequestsUseCase{
		factory: &fakeFactory{workspace: workspace}, code: code,
	}

	// Act
	_, err := useCase.Handle(context.Background(), OpenPullRequestsCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected the push failure to surface")
	}
	if len(workspace.detail.PullRequests) != 0 {
		t.Fatalf("expected no record, got %+v", workspace.detail.PullRequests)
	}
}
