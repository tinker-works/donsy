package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

type GetEpicQuery struct {
	Project domain.Project
	EpicID  string
}

type GetEpicUseCase struct {
	factory application.WorkspaceFactory
}

func (u *GetEpicUseCase) Handle(query GetEpicQuery) (epic.Epic, error) {
	return u.factory.Open(query.Project.Name).ReadEpic(query.EpicID)
}
