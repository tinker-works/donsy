package usecases

import (
	"context"
	"fmt"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

type TransitionPullRequestCommand struct {
	Project       domain.Project
	EpicID        string
	PullRequestID string
	Status        epic.PullRequestStatus
}

// TransitionPullRequestUseCase records a human's verdict on one pull request.
// Closing it also deletes the branch behind it, the same way closing the issue
// or the epic does — abandoning work should not depend on which of the three
// the person happened to reach for.
type TransitionPullRequestUseCase struct {
	factory application.WorkspaceFactory
	// code is nil when the loop runs drafting-only, where no branch was ever
	// cut and there is nothing to delete.
	code agent_runtime.CodeWorkspace
}

func (u *TransitionPullRequestUseCase) Handle(
	ctx context.Context, command TransitionPullRequestCommand,
) error {
	// Merging changes the repository, not just the record, so it must go
	// through MergePullRequestUseCase. Recording it here would write down a
	// merge whose commits never landed.
	if command.Status != epic.PullRequestClosed {
		return fmt.Errorf(
			"cannot record pull request status %q here; merging goes through the merge use case",
			command.Status,
		)
	}
	var abandoned []epic.Branch
	if err := updateEpic(u.factory, command.Project,
		command.EpicID,
		func(detail *epic.Epic) error {
			wasOpen := openPullRequestIDs(*detail)
			if err := detail.TransitionPullRequest(command.PullRequestID, command.Status); err != nil {
				return err
			}
			abandoned = newlyAbandoned(*detail, wasOpen)
			return nil
		},
	); err != nil {
		return err
	}
	return deleteAbandonedBranches(ctx, u.code, command.EpicID, abandoned)
}
