package usecases

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

func approvedEpic() epicpkg.Epic {
	return epicpkg.Epic{
		ID: "epic-1", Title: "Improve workflow", Assignee: "owner",
		State: epicpkg.EpicStateReady,
		Issues: []epicpkg.Issue{
			{ID: "root", Title: "Improve workflow", State: epicpkg.IssueStateOpen},
			{
				ID: "child-1", Title: "Add widget", ParentID: "root",
				Repository: "acme/widgets", State: epicpkg.IssueStatePR,
			},
		},
		PullRequests: []epicpkg.PullRequest{{
			ID: "pr-1", IssueID: "child-1", Title: "Add widget",
			Status: epicpkg.PullRequestOpen, Repository: "acme/widgets",
			Head: "go-merge/child-1", Base: "main",
			Rounds: 1, Reviews: 1, Approved: true,
			ReviewedHead: "head1234", ReviewedBase: "base1234",
		}},
	}
}

func mergeCommand() MergePullRequestCommand {
	return MergePullRequestCommand{
		Project:       domain.Project{Name: "one"},
		EpicID:        "epic-1",
		PullRequestID: "pr-1",
	}
}

func TestMergePullRequestUseCase_ShouldPublishRecordAndDeleteTheBranch(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: approvedEpic()}
	code := newFakeCodeWorkspace()
	useCase := &MergePullRequestUseCase{factory: &fakeFactory{workspace: workspace}, code: code}

	// Act
	err := useCase.Handle(context.Background(), mergeCommand())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(code.merged) != 1 || code.merged[0] != "go-merge/child-1 -> main" {
		t.Fatalf("expected the branch to be published onto base, got %v", code.merged)
	}
	if workspace.detail.PullRequests[0].Status != epicpkg.PullRequestMerged ||
		workspace.detail.Issues[1].State != epicpkg.IssueStateMerged {
		t.Fatalf("unexpected merged aggregate: %#v", workspace.detail)
	}
	// The commits are on base now, so nothing should still point at the branch.
	if len(code.deleted) != 1 || code.deleted[0] != "go-merge/child-1" {
		t.Fatalf("expected the merged branch to be deleted, got %v", code.deleted)
	}
}

// A branch base has moved past goes to the merge role, not back to coding:
// the work is written and approved, and only the merge needs doing.
func TestMergePullRequestUseCase_ShouldMarkAStaleBranchForTheMergeRole(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: approvedEpic()}
	code := newFakeCodeWorkspace()
	code.mergeErr = fmt.Errorf("%w: main has moved on", agent_runtime.ErrMergeConflict)
	useCase := &MergePullRequestUseCase{factory: &fakeFactory{workspace: workspace}, code: code}

	// Act
	err := useCase.Handle(context.Background(), mergeCommand())

	// Assert: the conflict is reported so the caller does not claim a merge.
	if !errors.Is(err, agent_runtime.ErrMergeConflict) {
		t.Fatalf("expected the conflict to be reported, got %v", err)
	}
	updated := workspace.detail.PullRequests[0]
	if updated.Status != epicpkg.PullRequestOpen {
		t.Fatalf("expected the pull request to stay open, got %q", updated.Status)
	}
	// The approval still describes this branch's own commits — the reviewer
	// that runs after the merge round is what judges the combination.
	if !updated.Approved || !updated.HasFlag(epicpkg.FlagStale) {
		t.Fatalf("expected an approved but stale record: %#v", updated)
	}
	if workspace.detail.Issues[1].State != epicpkg.IssueStateStale {
		t.Fatalf("expected the issue to go stale, got %q", workspace.detail.Issues[1].State)
	}
	role, ok := IssueRole(workspace.detail, updated)
	if !ok || role != agent.AgentRoleMerge {
		t.Fatalf("expected a merge round to be scheduled, got (%q, %t)", role, ok)
	}
	// The next coder reads the thread, so the reason has to be in it.
	if len(updated.Comments) != 1 || !strings.Contains(updated.Comments[0].Body, "main") {
		t.Fatalf("expected the conflict to be explained on the record: %#v", updated.Comments)
	}
	if len(code.deleted) != 0 {
		t.Fatalf("expected the branch to survive a conflict, got %v", code.deleted)
	}
}

func TestMergePullRequestUseCase_ShouldRefuseARecordThatIsNotOpen(t *testing.T) {
	// Arrange: merging twice would push the same commits on a stale read.
	detail := approvedEpic()
	detail.PullRequests[0].Status = epicpkg.PullRequestMerged
	workspace := &fakeWorkspace{detail: detail}
	code := newFakeCodeWorkspace()
	useCase := &MergePullRequestUseCase{factory: &fakeFactory{workspace: workspace}, code: code}

	// Act
	err := useCase.Handle(context.Background(), mergeCommand())

	// Assert
	if err == nil || !strings.Contains(err.Error(), "merged") {
		t.Fatalf("expected a closed record to be refused, got %v", err)
	}
	if len(code.merged) != 0 {
		t.Fatalf("expected nothing to be published, got %v", code.merged)
	}
}
