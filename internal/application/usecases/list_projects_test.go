package usecases

import (
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
)

func TestListProjectsUseCase_ShouldReturnRegisteredProjects(t *testing.T) {
	// Arrange
	projects := []domain.Project{{ID: 1, Name: "Project-1"}}
	useCase := &ListProjectsUseCase{registry: &fakeRegistry{projects: projects}}

	// Act
	got, err := useCase.Handle(ListProjectsQuery{})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Project-1" {
		t.Fatalf("unexpected projects: %#v", got)
	}
}
