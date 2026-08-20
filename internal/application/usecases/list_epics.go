package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

type ListEpicsQuery struct {
	Project domain.Project
}

type ListEpicsUseCase struct {
	factory application.WorkspaceFactory
}

func (u *ListEpicsUseCase) Handle(query ListEpicsQuery) ([]epic.Epic, error) {
	return u.factory.Open(query.Project.Name).ListEpics()
}
