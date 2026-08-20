package usecases

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

// These cover the failure halves of the use cases that own the git side: a
// record that has already been acted on, a branch that cannot be published, and
// a checkout the adapter could not prepare.

func TestGetPullRequestDiffUseCase_ShouldReportARecordItCannotDiff(t *testing.T) {
	// Arrange: a record with no branches has nothing to diff, and a record that
	// does not exist has nothing at all.
	query := func(id string) GetPullRequestDiffQuery {
		return GetPullRequestDiffQuery{
			Project:       domain.Project{Name: "one"},
			EpicID:        "epic-1",
			PullRequestID: id,
		}
	}
	branchless := &fakeWorkspace{detail: epicWithPullRequest(epicpkg.PullRequest{
		ID: "pr-1", IssueID: "issue-1", Title: "Add widget",
		Repository: "acme/widgets", Status: epicpkg.PullRequestOpen,
	})}
	useCase := &GetPullRequestDiffUseCase{
		factory: &fakeFactory{workspace: branchless}, differ: &fakeDiffer{},
	}

	// Act & Assert
	if _, err := useCase.Handle(context.Background(), query("pr-1")); err == nil {
		t.Fatal("expected a record with no branches to be reported")
	}
	if _, err := useCase.Handle(context.Background(), query("ghost")); err == nil {
		t.Fatal("expected an unknown record to be reported")
	}
}

func TestGetPullRequestDiffUseCase_ShouldSurfaceAReadFailure(t *testing.T) {
	// Arrange
	useCase := &GetPullRequestDiffUseCase{
		factory: broken(), differ: &fakeDiffer{},
	}

	// Act & Assert
	_, err := useCase.Handle(context.Background(), GetPullRequestDiffQuery{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1", PullRequestID: "pr-1",
	})
	if err == nil {
		t.Fatal("expected the read failure surfaced")
	}
}

func TestGetPullRequestDiffUseCase_ShouldSurfaceAFailedDiff(t *testing.T) {
	// Arrange: computing the diff Ensure-s the clone, which reaches the network.
	workspace := &fakeWorkspace{detail: epicWithPullRequest(epicpkg.PullRequest{
		ID: "pr-1", IssueID: "issue-1", Title: "Add widget",
		Repository: "acme/widgets", Head: "gm/widget", Base: "main",
		Status: epicpkg.PullRequestOpen,
	})}
	useCase := &GetPullRequestDiffUseCase{
		factory: &fakeFactory{workspace: workspace},
		differ:  &fakeDiffer{err: errStore},
	}

	// Act & Assert
	_, err := useCase.Handle(context.Background(), GetPullRequestDiffQuery{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1", PullRequestID: "pr-1",
	})
	if err == nil {
		t.Fatal("expected the failed diff surfaced")
	}
}

func TestMergePullRequestUseCase_ShouldRefuseARecordThatIsAlreadyTerminal(t *testing.T) {
	// Arrange: merging one that is already closed or merged would push commits a
	// second time on the strength of a record nobody re-read.
	for _, status := range []epicpkg.PullRequestStatus{
		epicpkg.PullRequestMerged, epicpkg.PullRequestClosed,
	} {
		detail := approvedEpic()
		detail.PullRequests[0].Status = status
		useCase := &MergePullRequestUseCase{
			factory: &fakeFactory{workspace: &fakeWorkspace{detail: detail}},
			code:    newFakeCodeWorkspace(),
		}

		// Act
		err := useCase.Handle(context.Background(), mergeCommand())

		// Assert
		if err == nil {
			t.Fatalf("expected a %s record to be refused", status)
		}
		if !strings.Contains(err.Error(), string(status)) {
			t.Fatalf("expected the status named, got %v", err)
		}
	}
}

func TestMergePullRequestUseCase_ShouldReportARecordThatIsNotThere(t *testing.T) {
	// Arrange
	useCase := &MergePullRequestUseCase{
		factory: &fakeFactory{workspace: &fakeWorkspace{detail: approvedEpic()}},
		code:    newFakeCodeWorkspace(),
	}
	command := mergeCommand()
	command.PullRequestID = "ghost"

	// Act & Assert
	if err := useCase.Handle(context.Background(), command); err == nil {
		t.Fatal("expected an unknown record to be reported")
	}
}

func TestMergePullRequestUseCase_ShouldSurfaceAReadFailure(t *testing.T) {
	// Arrange
	useCase := &MergePullRequestUseCase{factory: broken(), code: newFakeCodeWorkspace()}

	// Act & Assert
	if err := useCase.Handle(context.Background(), mergeCommand()); err == nil {
		t.Fatal("expected the read failure surfaced")
	}
}

func TestMergePullRequestUseCase_ShouldSurfaceAMergeThatFailedForAnyOtherReason(t *testing.T) {
	// Arrange: a conflict sends the branch back for another round, but anything
	// else is a failure the caller has to see.
	workspace := &fakeWorkspace{detail: approvedEpic()}
	code := newFakeCodeWorkspace()
	code.mergeErr = errStore
	useCase := &MergePullRequestUseCase{
		factory: &fakeFactory{workspace: workspace}, code: code,
	}

	// Act
	err := useCase.Handle(context.Background(), mergeCommand())

	// Assert
	if err == nil {
		t.Fatal("expected the failure surfaced")
	}
	if errors.Is(err, agent_runtime.ErrMergeConflict) {
		t.Fatal("expected it not to be reported as a conflict")
	}
	stored, _ := workspace.ReadEpic("epic-1")
	if stored.PullRequests[0].HasFlag(epicpkg.FlagStale) {
		t.Fatal("expected no stale flag for a failure that is not a conflict")
	}
}

func TestMergePullRequestUseCase_ShouldSendABehindBranchBackForAMergeRound(t *testing.T) {
	// Arrange: a conflict is a normal outcome — the pull request goes back so the
	// conflict is resolved by whoever wrote the code.
	workspace := &fakeWorkspace{detail: approvedEpic()}
	code := newFakeCodeWorkspace()
	code.mergeErr = agent_runtime.ErrMergeConflict
	useCase := &MergePullRequestUseCase{
		factory: &fakeFactory{workspace: workspace}, code: code,
	}

	// Act
	err := useCase.Handle(context.Background(), mergeCommand())

	// Assert
	if !errors.Is(err, agent_runtime.ErrMergeConflict) {
		t.Fatalf("expected the conflict reported as such, got %v", err)
	}
	stored, _ := workspace.ReadEpic("epic-1")
	record := stored.PullRequests[0]
	if !record.HasFlag(epicpkg.FlagStale) {
		t.Fatalf("expected the record flagged stale, got %+v", record.Flags)
	}
	if !record.Approved {
		t.Fatal("expected the approval kept: it still describes this branch's own commits")
	}
	if len(record.Comments) == 0 ||
		!strings.Contains(record.Comments[0].Body, "has to be merged into this branch") {
		t.Fatalf("expected the reason in the thread, got %+v", record.Comments)
	}
	for _, issue := range stored.Issues {
		if issue.ID == "child-1" && issue.State != epicpkg.IssueStateStale {
			t.Fatalf("expected the issue sent back, got %q", issue.State)
		}
	}
}

func TestOpenPullRequestsUseCase_ShouldSurfaceAReadFailure(t *testing.T) {
	// Arrange
	useCase := &OpenPullRequestsUseCase{factory: broken(), code: newFakeCodeWorkspace()}

	// Act & Assert
	_, err := useCase.Handle(context.Background(), OpenPullRequestsCommand{
		Project: project(), EpicID: "aggregate",
	})
	if err == nil {
		t.Fatal("expected the read failure surfaced")
	}
}

func TestOpenPullRequestsUseCase_ShouldSurfaceAFailedDefaultBranchLookup(t *testing.T) {
	// Arrange: every issue branch is cut from the repository's own default branch,
	// so a lookup that fails leaves nothing to cut from.
	code := newFakeCodeWorkspace()
	code.defaultErr = errStore
	detail := approvedEpic()
	detail.PullRequests = nil
	detail.Issues[1].State = epicpkg.IssueStateOpen
	detail.Repositories = []string{"acme/widgets"}
	useCase := &OpenPullRequestsUseCase{
		factory: &fakeFactory{workspace: &fakeWorkspace{detail: detail}}, code: code,
	}

	// Act
	_, err := useCase.Handle(context.Background(), OpenPullRequestsCommand{
		Project: project(), EpicID: "epic-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected the failed lookup surfaced")
	}
}

func TestReviewApprovedBranchesUseCase_ShouldSurfaceAReadFailure(t *testing.T) {
	// Arrange
	useCase := &ReviewApprovedBranchesUseCase{
		factory: broken(), code: newFakeCodeWorkspace(),
	}

	// Act & Assert
	err := useCase.Handle(context.Background(), ReviewApprovedBranchesCommand{
		Project: project(), EpicID: "aggregate",
	})
	if err == nil {
		t.Fatal("expected the read failure surfaced")
	}
}

func TestReviewApprovedBranchesUseCase_ShouldDoNothingWithNothingAwaitingMerge(t *testing.T) {
	// Arrange: the sweep costs a fetch per repository, so an epic with no approved
	// branch must not pay for one.
	code := newFakeCodeWorkspace()
	detail := approvedEpic()
	detail.PullRequests[0].Approved = false
	useCase := &ReviewApprovedBranchesUseCase{
		factory: &fakeFactory{workspace: &fakeWorkspace{detail: detail}}, code: code,
	}

	// Act
	err := useCase.Handle(context.Background(), ReviewApprovedBranchesCommand{
		Project: project(), EpicID: "epic-1",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(code.inspected) != 0 {
		t.Fatalf("expected no fetch, got %v", code.inspected)
	}
}

func TestReviewApprovedBranchesUseCase_ShouldSurfaceAFailedInspection(t *testing.T) {
	// Arrange
	code := newFakeCodeWorkspace()
	code.inspectErr = errStore
	useCase := &ReviewApprovedBranchesUseCase{
		factory: &fakeFactory{workspace: &fakeWorkspace{detail: approvedEpic()}},
		code:    code,
	}

	// Act & Assert
	err := useCase.Handle(context.Background(), ReviewApprovedBranchesCommand{
		Project: project(), EpicID: "epic-1",
	})
	if err == nil {
		t.Fatal("expected the failed inspection surfaced")
	}
}

func TestCompleteEpicUseCase_ShouldSurfaceAFailedWrite(t *testing.T) {
	// Arrange
	useCase := &CompleteEpicUseCase{factory: broken()}

	// Act & Assert
	if _, err := useCase.Handle(
		CompleteEpicCommand{Project: project(), EpicID: "aggregate"},
	); err == nil {
		t.Fatal("expected the write failure surfaced")
	}
}

func TestTransitionPullRequestUseCase_ShouldSurfaceAFailedWrite(t *testing.T) {
	// Arrange
	useCase := &TransitionPullRequestUseCase{
		factory: broken(), code: newFakeCodeWorkspace(),
	}

	// Act & Assert
	err := useCase.Handle(context.Background(), TransitionPullRequestCommand{
		Project: project(), EpicID: "aggregate", PullRequestID: "pr-1",
		Status: epicpkg.PullRequestClosed,
	})
	if err == nil {
		t.Fatal("expected the write failure surfaced")
	}
}
