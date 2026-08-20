package usecases

import (
	"github.com/tinker-works/donsy/internal/domain/epic"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
)

func TestGetEpicUseCase_ShouldReadEpicFromWorkspace(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{ID: "epic-1", Title: "Epic"}}
	factory := &fakeFactory{workspace: workspace}
	useCase := &GetEpicUseCase{factory: factory}

	// Act
	got, err := useCase.Handle(GetEpicQuery{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "epic-1" || got.Title != "Epic" {
		t.Fatalf("unexpected epic: %#v", got)
	}
}
