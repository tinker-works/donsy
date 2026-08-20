package usecases

import (
	"github.com/tinker-works/donsy/internal/domain/epic"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
)

func TestCreatePullRequestUseCase_ShouldRecordPullRequest(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateConcept,
		Issues: []epic.Issue{{ID: "root", Title: "Root", State: epic.IssueStateOpen}},
	}}
	useCase := &CreatePullRequestUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(CreatePullRequestCommand{
		Project:    domain.Project{Name: "one"},
		EpicID:     "aggregate",
		IssueID:    "root",
		Title:      "PR",
		Repository: "repo",
		Head:       "head",
		Base:       "base",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.detail.PullRequests) != 1 {
		t.Fatalf("expected one pull request, got %d", len(workspace.detail.PullRequests))
	}
	pullRequest := workspace.detail.PullRequests[0]
	if pullRequest.Title != "PR" || pullRequest.IssueID != "root" ||
		pullRequest.Repository != "repo" ||
		pullRequest.Head != "head" || pullRequest.Base != "base" {
		t.Fatalf("unexpected pull request: %#v", pullRequest)
	}
	if workspace.updatedEpicID != "aggregate" {
		t.Fatalf("unexpected update epic: %q", workspace.updatedEpicID)
	}
}
