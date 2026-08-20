package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

type CreatePullRequestCommand struct {
	Project    domain.Project
	EpicID     string
	IssueID    string
	Title      string
	Repository string
	Head       string
	Base       string
}

type CreatePullRequestUseCase struct {
	factory application.WorkspaceFactory
}

func (u *CreatePullRequestUseCase) Handle(command CreatePullRequestCommand) error {
	return updateEpic(u.factory, command.Project, command.EpicID,
		func(detail *epic.Epic) error {
			pullRequest, err := epic.CreatePullRequest(
				command.IssueID,
				command.Title,
				command.Repository,
				command.Head,
				command.Base,
			)
			if err != nil {
				return err
			}
			return detail.AddPullRequest(command.IssueID, pullRequest)
		})
}
