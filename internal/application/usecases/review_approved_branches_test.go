package usecases

import (
	"context"
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

func reviewApprovedCommand() ReviewApprovedBranchesCommand {
	return ReviewApprovedBranchesCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	}
}

func TestReviewApprovedBranches_ShouldMarkABranchBehindBaseAsStale(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: approvedEpic()}
	code := newFakeCodeWorkspace()
	code.branchState = agent_runtime.BranchState{Head: "head1234", Behind: true}
	useCase := &ReviewApprovedBranchesUseCase{
		factory: &fakeFactory{workspace: workspace}, code: code,
	}

	// Act
	if err := useCase.Handle(context.Background(), reviewApprovedCommand()); err != nil {
		t.Fatal(err)
	}

	// Assert
	updated := workspace.detail.PullRequests[0]
	if !updated.HasFlag(epicpkg.FlagStale) {
		t.Fatalf("expected the record to be flagged stale: %#v", updated.Flags)
	}
	if workspace.detail.Issues[1].State != epicpkg.IssueStateStale {
		t.Fatalf("expected the issue to go stale, got %q", workspace.detail.Issues[1].State)
	}
	role, ok := IssueRole(workspace.detail, updated)
	if !ok || role != agent.AgentRoleMerge {
		t.Fatalf("expected a merge round, got (%q, %t)", role, ok)
	}
	// The approval still describes this branch's own commits; the reviewer that
	// runs after the merge round is what judges the combination.
	if !updated.Approved {
		t.Fatal("expected the approval to survive going stale")
	}
}

// Somebody pushing to the branch by hand leaves the recorded verdict describing
// commits that are no longer on it.
func TestReviewApprovedBranches_ShouldSendAHandPushedBranchBackToReview(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: approvedEpic()}
	code := newFakeCodeWorkspace()
	code.branchState = agent_runtime.BranchState{Head: "pushed99", Behind: false}
	useCase := &ReviewApprovedBranchesUseCase{
		factory: &fakeFactory{workspace: workspace}, code: code,
	}

	// Act
	if err := useCase.Handle(context.Background(), reviewApprovedCommand()); err != nil {
		t.Fatal(err)
	}

	// Assert
	updated := workspace.detail.PullRequests[0]
	if updated.Approved || updated.ReviewedHead != "" {
		t.Fatalf("expected the approval to be dropped: %#v", updated)
	}
	if workspace.detail.Issues[1].State != epicpkg.IssueStateReview {
		t.Fatalf("expected the issue back in review, got %q", workspace.detail.Issues[1].State)
	}
	role, ok := IssueRole(workspace.detail, updated)
	if !ok || role != agent.AgentRolePRReviewer {
		t.Fatalf("expected a review round, got (%q, %t)", role, ok)
	}
	// A person fixing the branch themselves must not spend the agent's budget.
	if updated.Rounds != 2 || updated.CodingRounds != 0 {
		t.Fatalf("expected an uncharged round: rounds=%d coding=%d",
			updated.Rounds, updated.CodingRounds)
	}
	if len(updated.Comments) != 1 || !strings.Contains(updated.Comments[0].Body, "outside the loop") {
		t.Fatalf("expected the push to be explained on the record: %#v", updated.Comments)
	}
}

func TestReviewApprovedBranches_ShouldLeaveAnUpToDateBranchAlone(t *testing.T) {
	// Arrange: the sweep runs every tick, so the common case must be inert.
	workspace := &fakeWorkspace{detail: approvedEpic()}
	code := newFakeCodeWorkspace()
	code.branchState = agent_runtime.BranchState{Head: "head1234", Behind: false}
	useCase := &ReviewApprovedBranchesUseCase{
		factory: &fakeFactory{workspace: workspace}, code: code,
	}

	// Act
	if err := useCase.Handle(context.Background(), reviewApprovedCommand()); err != nil {
		t.Fatal(err)
	}

	// Assert
	if workspace.updatedEpicID != "" {
		t.Fatalf("expected no write, got update of %q", workspace.updatedEpicID)
	}
	if workspace.detail.Issues[1].State != epicpkg.IssueStatePR {
		t.Fatalf("expected the issue to stay in Pr, got %q", workspace.detail.Issues[1].State)
	}
}

// Only work the loop has finished with is worth a fetch. Anything earlier is
// still being written or judged, and the coding agent merges base in as it goes.
func TestReviewApprovedBranches_ShouldIgnoreWorkStillInTheLoop(t *testing.T) {
	// Arrange
	detail := approvedEpic()
	detail.Issues[1].State = epicpkg.IssueStateCoding
	detail.PullRequests[0].Approved = false
	code := newFakeCodeWorkspace()
	code.branchState = agent_runtime.BranchState{Head: "other999", Behind: true}
	useCase := &ReviewApprovedBranchesUseCase{
		factory: &fakeFactory{workspace: &fakeWorkspace{detail: detail}}, code: code,
	}

	// Act
	if err := useCase.Handle(context.Background(), reviewApprovedCommand()); err != nil {
		t.Fatal(err)
	}

	// Assert
	if detail.Issues[1].State != epicpkg.IssueStateCoding {
		t.Fatalf("expected the issue to be left alone, got %q", detail.Issues[1].State)
	}
}

// A fetch updates every remote ref, so a repository's branches cost one round
// trip between them rather than one each.
func TestReviewApprovedBranches_ShouldFetchOncePerRepository(t *testing.T) {
	// Arrange: three approved branches, two of them in the same repository.
	detail := approvedEpic()
	for _, extra := range []struct{ issue, repository, head string }{
		{"child-2", "acme/widgets", "go-merge/child-2"},
		{"child-3", "acme/gadgets", "go-merge/child-3"},
	} {
		detail.Issues = append(detail.Issues, epicpkg.Issue{
			ID: extra.issue, Title: extra.issue, ParentID: "root",
			Repository: extra.repository, State: epicpkg.IssueStatePR,
		})
		detail.PullRequests = append(detail.PullRequests, epicpkg.PullRequest{
			ID: "pr-" + extra.issue, IssueID: extra.issue, Title: extra.issue,
			Status: epicpkg.PullRequestOpen, Repository: extra.repository,
			Head: extra.head, Base: "main",
			Rounds: 1, Reviews: 1, Approved: true, ReviewedHead: "head1234",
		})
	}
	code := newFakeCodeWorkspace()
	code.branchState = agent_runtime.BranchState{Head: "head1234"}
	useCase := &ReviewApprovedBranchesUseCase{
		factory: &fakeFactory{workspace: &fakeWorkspace{detail: detail}}, code: code,
	}

	// Act
	if err := useCase.Handle(context.Background(), reviewApprovedCommand()); err != nil {
		t.Fatal(err)
	}

	// Assert: two repositories, so two fetches — not three.
	if len(code.inspected) != 2 {
		t.Fatalf("expected one fetch per repository, got %d: %v",
			len(code.inspected), code.inspected)
	}
	if len(code.inspected[0]) != 2 {
		t.Fatalf("expected both widgets branches in one call, got %v", code.inspected[0])
	}
}
