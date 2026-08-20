package usecases

import (
	"context"
	"fmt"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

type OpenPullRequestsCommand struct {
	Project domain.Project
	EpicID  string
}

// OpenPullRequestsUseCase is the handoff from drafting to implementation: once
// an epic is Ready, an issue that is not waiting on anything gets a branch and a
// pull request record. It runs on every worker tick rather than once, so an
// issue is cut only after what it waits on merged — from a default branch that
// already contains that work.
type OpenPullRequestsUseCase struct {
	factory application.WorkspaceFactory
	code    agent_runtime.CodeWorkspace
}

// Handle opens what is not open yet and returns how many records it cut.
// Zero is not an error — it means there was nothing left to do.
func (u *OpenPullRequestsUseCase) Handle(
	ctx context.Context, command OpenPullRequestsCommand,
) (int, error) {
	if u.code == nil {
		return 0, fmt.Errorf("no code workspace is configured to cut branches in")
	}
	workspace := u.factory.Open(command.Project.Name)
	current, err := workspace.ReadEpic(command.EpicID)
	if err != nil {
		return 0, err
	}
	if current.State != epicpkg.EpicStateReady {
		return 0, nil
	}
	if err := u.resyncBlocked(workspace, current); err != nil {
		return 0, err
	}
	opened := 0
	for _, issue := range current.Issues {
		if issue.ParentID == "" || issue.State != epicpkg.IssueStateOpen {
			continue
		}
		// Cutting a branch for an issue that is still waiting would base it on
		// work that does not exist yet. A later tick opens it instead.
		if current.Blocked(issue.ID) {
			continue
		}
		if _, exists := current.OpenPullRequestFor(issue.ID); exists {
			continue
		}
		if err := u.open(ctx, workspace, current, issue); err != nil {
			return opened, err
		}
		opened++
	}
	return opened, nil
}

// resyncBlocked brings the recorded flag back in line with the tree. Nothing
// else clears it: a record cut while its issue was still gated stays flagged
// once the parent merges, and records written before the tree was ordered
// top-down carry the flag on the wrong issues entirely.
//
// A sweep that changes nothing must not write. The tracker checkout rejects an
// empty commit, and Mutate reads that as a failed push and retries it.
func (u *OpenPullRequestsUseCase) resyncBlocked(
	workspace application.Workspace, current epicpkg.Epic,
) error {
	stale := map[string]bool{}
	for _, pullRequest := range current.PullRequests {
		if pullRequest.Status != epicpkg.PullRequestOpen {
			continue
		}
		blocked := current.Blocked(pullRequest.IssueID)
		if pullRequest.HasFlag(epicpkg.FlagBlocked) != blocked {
			stale[pullRequest.ID] = blocked
		}
	}
	if len(stale) == 0 {
		return nil
	}
	return workspace.UpdateEpic(
		current.ID,
		func(target *epicpkg.Epic) error {
			for id, blocked := range stale {
				if err := target.UpdatePullRequest(
					id, func(record *epicpkg.PullRequest) error {
						return record.SetFlag(epicpkg.FlagBlocked, blocked)
					},
				); err != nil {
					return err
				}
			}
			return nil
		},
	)
}

func (u *OpenPullRequestsUseCase) open(
	ctx context.Context, workspace application.Workspace, current epicpkg.Epic, issue epicpkg.Issue,
) error {
	base, err := u.code.DefaultBranch(ctx, issue.Repository)
	if err != nil {
		return fmt.Errorf("resolve default branch for %s: %w", issue.Repository, err)
	}
	branch := current.BranchName(issue)
	checkout := agent_runtime.CodeCheckout{
		EpicID: current.ID, IssueID: issue.ID, Repository: issue.Repository,
	}
	if _, err := u.code.Checkout(ctx, checkout, branch, base); err != nil {
		return err
	}
	// Publishing the branch before recording it means a record always points
	// at a branch that exists. The reverse would leave a pull request nobody
	// can check out if the push failed.
	if err := u.code.Push(ctx, checkout, branch); err != nil {
		return err
	}
	return workspace.UpdateEpic(
		current.ID,
		func(target *epicpkg.Epic) error {
			pullRequest, err := epicpkg.CreatePullRequest(
				issue.ID, issue.Title, issue.Repository, branch, base,
			)
			if err != nil {
				return err
			}
			return target.AddPullRequest(issue.ID, pullRequest)
		},
	)
}
