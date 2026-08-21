package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

type CreateEpicCommand struct {
	Project      domain.Project
	Title        string
	Assignee     string
	Body         string
	Repositories []string
	// BranchPrefix is optional. Setting it at creation is the only way an epic
	// the agent loop drives unattended gets one: the loop takes an approved plan
	// straight from Proposed to Ready without stopping for the dialog that
	// otherwise asks.
	BranchPrefix string
}

type CreateEpicUseCase struct {
	factory application.WorkspaceFactory
}

func (u *CreateEpicUseCase) Handle(command CreateEpicCommand) (epicpkg.Epic, error) {
	aggregate, err := epicpkg.CreateEpic(command.Title, command.Assignee, command.Body)
	if err != nil {
		return epicpkg.Epic{}, err
	}
	if err := aggregate.SetBranchPrefix(command.BranchPrefix); err != nil {
		return epicpkg.Epic{}, err
	}
	workspace := u.factory.Open(command.Project.Name)
	repositories, err := resolveEpicRepositories(workspace, command.Repositories)
	if err != nil {
		return epicpkg.Epic{}, err
	}
	aggregate.Repositories = repositories
	if err := workspace.CreateEpic(aggregate); err != nil {
		return epicpkg.Epic{}, err
	}
	return aggregate, nil
}
