package usecases

import (
	"context"
	"errors"
	"fmt"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

type MergePullRequestCommand struct {
	Project       domain.Project
	EpicID        string
	PullRequestID string
}

// MergePullRequestUseCase lands one pull request: it publishes the branch onto
// its base, records the merge, and deletes the branch.
//
// It is separate from TransitionPullRequestUseCase because merging is the one
// pull request outcome that changes the repository rather than just the record.
// A merge that is only written down leaves the commits sitting on a branch
// nobody will look at again, which is worse than not merging at all.
type MergePullRequestUseCase struct {
	factory application.WorkspaceFactory
	code    agent_runtime.CodeWorkspace
}

func (u *MergePullRequestUseCase) Handle(
	ctx context.Context, command MergePullRequestCommand,
) error {
	if u.code == nil {
		return fmt.Errorf("no code workspace is configured to merge branches in")
	}
	workspace := u.factory.Open(command.Project.Name)
	current, err := workspace.ReadEpic(command.EpicID)
	if err != nil {
		return err
	}
	record, err := openPullRequest(current, command.PullRequestID)
	if err != nil {
		return err
	}
	checkout := agent_runtime.CodeCheckout{
		EpicID: current.ID, IssueID: record.IssueID, Repository: record.Repository,
	}

	// The branch is published before the record moves. A merge recorded against
	// commits that never landed is the one outcome there is no way back from.
	if err := u.code.Merge(ctx, checkout, record.Head, record.Base); err != nil {
		if !errors.Is(err, agent_runtime.ErrMergeConflict) {
			return err
		}
		if recordErr := u.markStale(workspace, current, record, err); recordErr != nil {
			return recordErr
		}
		// The conflict is returned even though it was handled, so the caller
		// can say the branch went stale rather than claiming a merge.
		return err
	}
	if err := workspace.UpdateEpic(
		current.ID,
		func(target *epicpkg.Epic) error {
			return target.TransitionPullRequest(record.ID, epicpkg.PullRequestMerged)
		},
	); err != nil {
		return err
	}
	// The commits are on base now, so the branch is finished. A delete that
	// fails is reported rather than retried: the merge itself already landed.
	return u.code.DeleteBranch(ctx, checkout, record.Head)
}

// markStale hands the branch to the merge role. The approval is kept: it still
// describes this branch's own commits, and the reviewer that runs after the
// merge round is what judges the combination.
func (u *MergePullRequestUseCase) markStale(
	workspace application.Workspace,
	current epicpkg.Epic,
	record epicpkg.PullRequest,
	conflict error,
) error {
	return workspace.UpdateEpic(
		current.ID,
		func(target *epicpkg.Epic) error {
			if err := target.UpdatePullRequest(
				record.ID, func(pullRequest *epicpkg.PullRequest) error {
					return pullRequest.SetFlag(epicpkg.FlagStale, true)
				},
			); err != nil {
				return err
			}
			// The merge agent reads the thread, so the reason it was called has
			// to be in it rather than only in a toast that is already gone.
			comment, err := epicpkg.CreateComment("merge", fmt.Sprintf(
				"Could not publish `%s` onto `%s`: %v\n\n`%s` has to be merged into this "+
					"branch before it can land.",
				record.Head, record.Base, conflict, record.Base,
			))
			if err != nil {
				return err
			}
			if err := target.AddPullRequestComment(record.ID, comment); err != nil {
				return err
			}
			return target.TransitionIssue(record.IssueID, epicpkg.IssueStateStale)
		},
	)
}

// openPullRequest finds a record that can still be merged. Merging one that is
// already closed or merged would push commits a second time on the strength of
// a record nobody re-read.
func openPullRequest(
	current epicpkg.Epic, pullRequestID string,
) (epicpkg.PullRequest, error) {
	for _, pullRequest := range current.PullRequests {
		if pullRequest.ID != pullRequestID {
			continue
		}
		if pullRequest.Status != epicpkg.PullRequestOpen {
			return epicpkg.PullRequest{}, fmt.Errorf(
				"pull request %s is %s", pullRequestID, pullRequest.Status,
			)
		}
		return pullRequest, nil
	}
	return epicpkg.PullRequest{}, fmt.Errorf("pull request not found")
}
