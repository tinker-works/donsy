package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
)

type OpenProjectCommand struct {
	Project domain.Project
}

// OpenProjectUseCase stamps a project as the most recently opened one. The
// project list is ordered by that stamp, so whatever was opened last is what
// the next start resumes on.
type OpenProjectUseCase struct {
	registry application.ProjectRegistry
}

func (u *OpenProjectUseCase) Handle(command OpenProjectCommand) error {
	return u.registry.Touch(command.Project.ID)
}
