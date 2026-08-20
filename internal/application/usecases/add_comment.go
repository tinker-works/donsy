package usecases

import (
	"fmt"
	"github.com/tinker-works/donsy/internal/domain/epic"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
)

type CommentTarget string

const (
	IssueCommentTarget       CommentTarget = "issue"
	PullRequestCommentTarget CommentTarget = "pull_request"
)

type AddCommentCommand struct {
	Project  domain.Project
	EpicID   string
	TargetID string
	Target   CommentTarget
	Author   string
	Body     string
}

type AddCommentUseCase struct {
	factory application.WorkspaceFactory
}

func (u *AddCommentUseCase) Handle(command AddCommentCommand) error {
	return updateEpic(u.factory, command.Project, command.EpicID,
		func(detail *epic.Epic) error {
			comment, err := epic.CreateComment(command.Author, command.Body)
			if err != nil {
				return err
			}
			switch command.Target {
			case IssueCommentTarget:
				return detail.AddIssueComment(command.TargetID, comment)
			case PullRequestCommentTarget:
				return detail.AddPullRequestComment(command.TargetID, comment)
			default:
				return fmt.Errorf("unsupported comment target %q", command.Target)
			}
		})
}
