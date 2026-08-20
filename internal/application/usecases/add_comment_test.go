package usecases

import (
	"github.com/tinker-works/donsy/internal/domain/epic"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
)

func TestAddCommentUseCase_ShouldAddPullRequestComment(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateConcept,
		Issues: []epic.Issue{{ID: "root", Title: "Root", State: epic.IssueStateOpen}},
		PullRequests: []epic.PullRequest{{
			ID: "pr", IssueID: "root", Title: "PR", Status: epic.PullRequestOpen,
		}},
	}}
	useCase := &AddCommentUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(AddCommentCommand{
		Project:  domain.Project{Name: "one"},
		EpicID:   "aggregate",
		TargetID: "pr",
		Target:   PullRequestCommentTarget,
		Author:   "author",
		Body:     "Review this",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.detail.PullRequests[0].Comments) != 1 ||
		workspace.detail.PullRequests[0].Comments[0].Body != "Review this" {
		t.Fatalf("unexpected pull request comments: %#v", workspace.detail.PullRequests[0].Comments)
	}
}

func TestAddCommentUseCase_ShouldAddIssueComment(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateConcept,
		Issues: []epic.Issue{{ID: "root", Title: "Root", State: epic.IssueStateOpen}},
	}}
	useCase := &AddCommentUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(AddCommentCommand{
		Project:  domain.Project{Name: "one"},
		EpicID:   "aggregate",
		TargetID: "root",
		Target:   IssueCommentTarget,
		Author:   "author",
		Body:     "Needs detail",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.detail.Issues[0].Comments) != 1 ||
		workspace.detail.Issues[0].Comments[0].Body != "Needs detail" {
		t.Fatalf("unexpected issue comments: %#v", workspace.detail.Issues[0].Comments)
	}
}

func TestAddCommentUseCase_ShouldRejectUnsupportedTarget(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateConcept,
		Issues: []epic.Issue{{ID: "root", Title: "Root", State: epic.IssueStateOpen}},
	}}
	useCase := &AddCommentUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(AddCommentCommand{
		Project:  domain.Project{Name: "one"},
		EpicID:   "aggregate",
		TargetID: "root",
		Target:   "unknown",
		Author:   "author",
		Body:     "Comment",
	})

	// Assert
	if err == nil {
		t.Fatal("expected unsupported comment target error")
	}
}
