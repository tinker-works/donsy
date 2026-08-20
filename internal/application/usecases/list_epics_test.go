package usecases

import (
	"github.com/tinker-works/donsy/internal/domain/epic"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
)

func TestListEpicsUseCase_ShouldListWorkspaceEpics(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{ID: "epic-1", Title: "Epic"}}
	factory := &fakeFactory{workspace: workspace}
	useCase := &ListEpicsUseCase{factory: factory}

	// Act
	got, err := useCase.Handle(ListEpicsQuery{
		Project: domain.Project{Name: "one"},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "epic-1" {
		t.Fatalf("unexpected epics: %#v", got)
	}
	if factory.openPath != "one" {
		t.Fatalf("unexpected open argument: %#v", factory)
	}
}
