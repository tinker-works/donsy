package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
)

type ListProjectsQuery struct{}

type ListProjectsUseCase struct {
	registry application.ProjectRegistry
}

func (u *ListProjectsUseCase) Handle(ListProjectsQuery) ([]domain.Project, error) {
	return u.registry.List()
}
