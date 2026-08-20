package usecases

import (
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
)

func TestCreateEpicUseCase_ShouldCreateEpicThroughWorkspacePort(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{repositories: []string{"acme/widgets"}}
	useCase := &CreateEpicUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(CreateEpicCommand{
		Project:      domain.Project{Name: "one"},
		Title:        "Epic",
		Assignee:     "owner",
		Body:         "Details",
		Repositories: []string{"acme/widgets"},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.createdEpic == nil {
		t.Fatalf("unexpected create call: %#v", workspace)
	}
	if workspace.createdEpic.Title != "Epic" || workspace.createdEpic.Assignee != "owner" ||
		workspace.createdEpic.Body != "Details" ||
		len(workspace.createdEpic.Repositories) != 1 || len(workspace.createdEpic.Issues) != 1 {
		t.Fatalf("unexpected epic: %#v", workspace.createdEpic)
	}
}

func TestCreateEpicUseCase_ShouldScopeToEveryProjectRepositoryWhenNoneRequested(t *testing.T) {
	// Arrange: RunEpicAgentUseCase refuses an epic that names no repository
	// and nothing can add one later, so an unscoped request covers them all
	// rather than producing a permanently unrunnable epic.
	workspace := &fakeWorkspace{repositories: []string{"acme/widgets", "acme/gadgets"}}
	useCase := &CreateEpicUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(CreateEpicCommand{
		Project: domain.Project{Name: "one"},
		Title:   "Epic", Assignee: "owner",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.createdEpic == nil || len(workspace.createdEpic.Repositories) != 2 {
		t.Fatalf("expected the project's repositories, got %#v", workspace.createdEpic)
	}
}

func TestCreateEpicUseCase_ShouldRejectWhenTheProjectHasNoRepositories(t *testing.T) {
	// Arrange: nothing to fall back to, so the epic would still be unrunnable.
	workspace := &fakeWorkspace{}
	useCase := &CreateEpicUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(CreateEpicCommand{
		Project: domain.Project{Name: "one"},
		Title:   "Epic", Assignee: "owner",
	})

	// Assert
	if err == nil {
		t.Fatal("expected an epic with no possible scope to be rejected")
	}
	if workspace.createdEpic != nil {
		t.Fatalf("expected nothing to be written, got %#v", workspace.createdEpic)
	}
}

func TestCreateEpicUseCase_ShouldRejectARepositoryTheProjectDoesNotConfigure(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{repositories: []string{"acme/widgets"}}
	useCase := &CreateEpicUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(CreateEpicCommand{
		Project:      domain.Project{Name: "one"},
		Title:        "Epic",
		Assignee:     "owner",
		Repositories: []string{"acme/unknown"},
	})

	// Assert
	if err == nil {
		t.Fatal("expected an unconfigured repository to be rejected")
	}
}
