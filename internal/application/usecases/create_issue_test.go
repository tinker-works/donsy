package usecases

import (
	"github.com/tinker-works/donsy/internal/domain/epic"
	"reflect"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
)

func TestCreateIssueUseCase_ShouldAddIssueThroughWorkspacePort(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateConcept,
		Issues: []epic.Issue{{ID: "root", Title: "Root", State: epic.IssueStateOpen}},
	}}
	useCase := &CreateIssueUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	created, err := useCase.Handle(CreateIssueCommand{
		Project:    domain.Project{Name: "one"},
		EpicID:     "aggregate",
		Title:      "Child",
		Repository: "acme/widgets",
		Body:       "Details",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.detail.Issues) != 2 {
		t.Fatalf("expected two issues, got %d", len(workspace.detail.Issues))
	}
	issue := workspace.detail.Issues[1]
	if !reflect.DeepEqual(created, issue) {
		t.Fatalf("returned issue = %#v, stored issue = %#v", created, issue)
	}
	if issue.Title != "Child" || issue.Body != "Details" || issue.Repository != "acme/widgets" ||
		issue.ParentID != "root" ||
		issue.CreatedAt.IsZero() {
		t.Fatalf("unexpected issue: %#v", issue)
	}
	if workspace.updatedEpicID != "aggregate" {
		t.Fatalf("unexpected update call: %#v", workspace)
	}
}
