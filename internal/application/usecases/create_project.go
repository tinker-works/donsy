package usecases

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
)

var projectNamePattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

type CreateProjectCommand struct {
	Name string
}

type CreateProjectUseCase struct {
	registry application.ProjectRegistry
	factory  application.WorkspaceFactory
	clock    application.Clock
}

func (u *CreateProjectUseCase) Handle(command CreateProjectCommand) (domain.Project, error) {
	name := strings.TrimSpace(command.Name)
	if name == "" {
		return domain.Project{}, fmt.Errorf("project name is required")
	}
	if !projectNamePattern.MatchString(name) {
		return domain.Project{}, fmt.Errorf("project name must contain only letters, numbers, and dashes")
	}
	project := domain.Project{Name: name, LastOpenedAt: u.clock.Now()}
	if err := u.registry.Create(&project); err != nil {
		return domain.Project{}, err
	}
	return project, nil
}
