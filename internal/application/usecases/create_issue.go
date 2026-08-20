package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

type CreateIssueCommand struct {
	Project    domain.Project
	EpicID     string
	ParentID   string
	Title      string
	Body       string
	Repository string
}

type CreateIssueUseCase struct {
	factory application.WorkspaceFactory
}

func (u *CreateIssueUseCase) Handle(command CreateIssueCommand) (epic.Issue, error) {
	var created epic.Issue
	err := updateEpic(u.factory, command.Project, command.EpicID,
		func(detail *epic.Epic) error {
			issue, err := epic.CreateRepositoryIssue(command.Title, command.Body, command.Repository)
			if err != nil {
				return err
			}
			parentID := command.ParentID
			if parentID == "" {
				root, err := detail.RootIssue()
				if err != nil {
					return err
				}
				parentID = root.ID
			}
			if err := detail.AddIssue(parentID, issue); err != nil {
				return err
			}
			issue.ParentID = parentID
			created = issue
			return nil
		})
	if err != nil {
		return epic.Issue{}, err
	}
	return created, nil
}
