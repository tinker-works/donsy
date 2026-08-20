package usecases

import (
	"context"
	"github.com/tinker-works/donsy/internal/domain/epic"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
)

func TestTransitionPullRequestUseCase_ShouldCloseRequestAndReopenIssue(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateConcept,
		Issues: []epic.Issue{{ID: "root", Title: "Root", State: epic.IssueStateOpen}},
	}}
	pullRequest, err := epic.CreatePullRequest("root", "PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.detail.AddPullRequest("root", pullRequest); err != nil {
		t.Fatal(err)
	}
	// Closing is only offered once the issue is under review, so put it in Pr first.
	for _, state := range []epic.IssueState{epic.IssueStateReview, epic.IssueStatePR} {
		if err := workspace.detail.TransitionIssue("root", state); err != nil {
			t.Fatal(err)
		}
	}
	useCase := &TransitionPullRequestUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err = useCase.Handle(context.Background(), TransitionPullRequestCommand{
		Project:       domain.Project{Name: "one"},
		EpicID:        "aggregate",
		PullRequestID: pullRequest.ID,
		Status:        epic.PullRequestClosed,
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.PullRequests[0].Status != epic.PullRequestClosed ||
		workspace.detail.Issues[0].State != epic.IssueStateOpen {
		t.Fatalf("unexpected transition: %#v", workspace.detail)
	}
	if workspace.updatedEpicID != "aggregate" {
		t.Fatalf("unexpected update epic: %q", workspace.updatedEpicID)
	}
}

// Recording a merge here would write down commits that never landed; merging
// must go through MergePullRequestUseCase, which publishes the branch first.
func TestTransitionPullRequestUseCase_ShouldRejectRecordingAMerge(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateConcept,
		Issues: []epic.Issue{{ID: "root", Title: "Root", State: epic.IssueStateOpen}},
	}}
	pullRequest, err := epic.CreatePullRequest("root", "PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.detail.AddPullRequest("root", pullRequest); err != nil {
		t.Fatal(err)
	}
	useCase := &TransitionPullRequestUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err = useCase.Handle(context.Background(), TransitionPullRequestCommand{
		Project:       domain.Project{Name: "one"},
		EpicID:        "aggregate",
		PullRequestID: pullRequest.ID,
		Status:        epic.PullRequestMerged,
	})

	// Assert
	if err == nil {
		t.Fatal("expected recording a merge to be rejected")
	}
	if workspace.detail.PullRequests[0].Status != epic.PullRequestOpen {
		t.Fatalf("pull request changed on a rejected command: %#v", workspace.detail.PullRequests[0])
	}
}
