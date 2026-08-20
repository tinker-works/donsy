package usecases

import (
	"context"
	"errors"
	"fmt"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

// openPullRequestIDs snapshots which records are open before a close, so the
// caller can tell the pull requests it just closed from ones that were already
// closed and whose branches are long gone.
func openPullRequestIDs(detail epicpkg.Epic) map[string]struct{} {
	open := map[string]struct{}{}
	for _, pullRequest := range detail.PullRequests {
		if pullRequest.Status == epicpkg.PullRequestOpen {
			open[pullRequest.ID] = struct{}{}
		}
	}
	return open
}

// newlyAbandoned is the branches behind the pull requests this change closed.
func newlyAbandoned(detail epicpkg.Epic, wasOpen map[string]struct{}) []epicpkg.Branch {
	branches := make([]epicpkg.Branch, 0, len(wasOpen))
	for _, branch := range detail.AbandonedBranches() {
		if _, closedByThisChange := wasOpen[branch.PullRequestID]; closedByThisChange {
			branches = append(branches, branch)
		}
	}
	return branches
}

// deleteAbandonedBranches removes the branches behind pull requests that were
// closed without merging.
//
// It runs after the store is written, never before: a branch that cannot be
// deleted must not roll back a close the user asked for. The failure is
// reported instead, so a branch left on the remote is visible rather than
// silent. A nil CodeWorkspace means the loop is running drafting-only and never
// cut a branch to begin with.
func deleteAbandonedBranches(
	ctx context.Context, code agent_runtime.CodeWorkspace, epicID string, branches []epicpkg.Branch,
) error {
	if code == nil {
		return nil
	}
	var failures []error
	for _, branch := range branches {
		if err := code.DeleteBranch(ctx, agent_runtime.CodeCheckout{
			EpicID: epicID, IssueID: branch.IssueID, Repository: branch.Repository,
		}, branch.Name); err != nil {
			failures = append(failures, fmt.Errorf("delete branch %s: %w", branch.Name, err))
		}
	}
	return errors.Join(failures...)
}
