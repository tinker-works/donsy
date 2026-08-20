package usecases

import (
	"github.com/tinker-works/donsy/internal/domain/epic"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
)

func TestTransitionEpicStateUseCase_ShouldTransitionEpic(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateConcept,
		Issues: []epic.Issue{{ID: "root", Title: "Root", State: epic.IssueStateOpen}},
	}}
	useCase := &TransitionEpicStateUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(TransitionEpicStateCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "aggregate",
		State:   epic.EpicStateRefine,
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.State != epic.EpicStateRefine {
		t.Fatalf("unexpected epic state: %s", workspace.detail.State)
	}
}
