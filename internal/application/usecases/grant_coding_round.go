package usecases

import (
	"context"
	"fmt"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

type GrantCodingRoundCommand struct {
	Project       domain.Project
	EpicID        string
	PullRequestID string
}

// GrantCodingRoundUseCase hands one pull request another coding round after the
// loop has spent its allowance. It is what the Retry key does, and the only
// thing that restarts a pull request sitting at its round limit — IssueRole
// gives out no role once CanCode is false, so nothing on the automatic path
// ever reaches it again.
//
// It deliberately refuses a pull request that still has rounds left. The loop
// retries a failed round on its own backoff, so granting there would spend a
// human's override on something already in hand and quietly raise the ceiling
// for the disagreement the limit exists to end.
type GrantCodingRoundUseCase struct {
	factory application.WorkspaceFactory
}

func (u *GrantCodingRoundUseCase) Handle(
	_ context.Context, command GrantCodingRoundCommand,
) error {
	return updateEpic(u.factory, command.Project,
		command.EpicID,
		func(detail *epic.Epic) error {
			return detail.UpdatePullRequest(
				command.PullRequestID,
				func(pullRequest *epic.PullRequest) error {
					if pullRequest.Status != epic.PullRequestOpen {
						return fmt.Errorf(
							"pull request %s is %s; only an open one can take another round",
							command.PullRequestID, pullRequest.Status,
						)
					}
					if pullRequest.CanCode() {
						return fmt.Errorf(
							"pull request %s still has coding rounds left; the loop retries on its own",
							command.PullRequestID,
						)
					}
					return pullRequest.GrantCodingRound()
				},
			)
		},
	)
}
