package usecases

import (
	"context"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

type CloseIssueCommand struct {
	Project domain.Project
	EpicID  string
	IssueID string
}

// CloseIssueUseCase closes an issue and everything below it, along with the
// pull requests open against them, and deletes the branches those records
// pointed at.
type CloseIssueUseCase struct {
	factory application.WorkspaceFactory
	// code is nil when the loop runs drafting-only, where no branch was ever
	// cut and there is nothing to delete.
	code agent_runtime.CodeWorkspace
}

func (u *CloseIssueUseCase) Handle(ctx context.Context, command CloseIssueCommand) error {
	var abandoned []epic.Branch
	if err := updateEpic(u.factory, command.Project, command.EpicID,
		func(detail *epic.Epic) error {
			wasOpen := openPullRequestIDs(*detail)
			if err := detail.CloseIssue(command.IssueID); err != nil {
				return err
			}
			abandoned = newlyAbandoned(*detail, wasOpen)
			return nil
		}); err != nil {
		return err
	}
	return deleteAbandonedBranches(ctx, u.code, command.EpicID, abandoned)
}
