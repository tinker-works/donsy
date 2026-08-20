package usecases

import (
	"context"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

type CloseEpicCommand struct {
	Project domain.Project
	EpicID  string
}

// CloseEpicUseCase abandons an epic and everything under it: the tree closes,
// every pull request still open against it closes, and the branches behind
// those records are deleted. Leaving them would strand branches on the remote
// for work nobody intends to finish.
type CloseEpicUseCase struct {
	factory application.WorkspaceFactory
	// code is nil when the loop runs drafting-only, where no branch was ever
	// cut and there is nothing to delete.
	code agent_runtime.CodeWorkspace
}

func (u *CloseEpicUseCase) Handle(ctx context.Context, command CloseEpicCommand) error {
	workspace := u.factory.Open(command.Project.Name)
	var abandoned []epicpkg.Branch
	if err := workspace.UpdateEpic(command.EpicID, func(detail *epicpkg.Epic) error {
		wasOpen := openPullRequestIDs(*detail)
		if err := detail.Close(); err != nil {
			return err
		}
		abandoned = newlyAbandoned(*detail, wasOpen)
		return nil
	}); err != nil {
		return err
	}
	return deleteAbandonedBranches(ctx, u.code, command.EpicID, abandoned)
}
