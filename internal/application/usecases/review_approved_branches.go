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

type ReviewApprovedBranchesCommand struct {
	Project domain.Project
	EpicID  string
}

// ReviewApprovedBranchesUseCase re-checks the branches that are approved and
// waiting for a human to merge them. Nothing else looks at those: the loop has
// finished with them, so without this sweep an approval quietly rots while the
// world moves on around it.
//
// Two things invalidate one. Base can move past the branch, which the merge
// role fixes. Or somebody can push to the branch by hand, which means the
// recorded verdict describes commits that are no longer on it. Both send the
// pull request back into the loop rather than letting it be merged on the
// strength of a review of something else.
type ReviewApprovedBranchesUseCase struct {
	factory application.WorkspaceFactory
	code    agent_runtime.CodeWorkspace
}

func (u *ReviewApprovedBranchesUseCase) Handle(
	ctx context.Context, command ReviewApprovedBranchesCommand,
) error {
	if u.code == nil {
		return nil
	}
	workspace := u.factory.Open(command.Project.Name)
	current, err := workspace.ReadEpic(command.EpicID)
	if err != nil {
		return err
	}
	// Branches are inspected a repository at a time. One fetch updates every
	// remote ref, so a repository with ten approved branches costs one round
	// trip rather than ten.
	var failures []error
	for _, group := range u.groupByRemote(current) {
		states, err := u.code.InspectBranches(
			ctx, current.ID, group.repository, group.base, group.heads(),
		)
		if err != nil {
			failures = append(failures, fmt.Errorf("inspect %q: %w", group.repository, err))
			continue
		}
		for _, pullRequest := range group.records {
			state, ok := states[pullRequest.Head]
			if !ok {
				continue
			}
			if err := u.apply(workspace, current, pullRequest, state); err != nil {
				failures = append(failures, fmt.Errorf("issue %q: %w", pullRequest.IssueID, err))
			}
		}
	}
	return errors.Join(failures...)
}

// remoteGroup is the set of approved branches that share one fetch: the same
// repository compared against the same base.
type remoteGroup struct {
	repository string
	base       string
	records    []epicpkg.PullRequest
}

func (g remoteGroup) heads() []string {
	heads := make([]string, 0, len(g.records))
	for _, record := range g.records {
		heads = append(heads, record.Head)
	}
	return heads
}

// groupByRemote keeps the epic's own order so the sweep reports failures in a
// stable sequence rather than whatever a map hands back.
func (u *ReviewApprovedBranchesUseCase) groupByRemote(current epicpkg.Epic) []remoteGroup {
	var groups []remoteGroup
	index := map[string]int{}
	for _, pullRequest := range current.PullRequests {
		if !u.awaitingMerge(current, pullRequest) {
			continue
		}
		key := pullRequest.Repository + "\x00" + pullRequest.Base
		if at, ok := index[key]; ok {
			groups[at].records = append(groups[at].records, pullRequest)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, remoteGroup{
			repository: pullRequest.Repository,
			base:       pullRequest.Base,
			records:    []epicpkg.PullRequest{pullRequest},
		})
	}
	return groups
}

// awaitingMerge is the narrow set this sweep costs a fetch for: work the loop
// has already signed off and parked in Pr. Anything earlier is still being
// written or judged, and the coding agent merges base in as it goes.
func (u *ReviewApprovedBranchesUseCase) awaitingMerge(
	current epicpkg.Epic, pullRequest epicpkg.PullRequest,
) bool {
	if pullRequest.Status != epicpkg.PullRequestOpen || !pullRequest.Approved {
		return false
	}
	issue, err := current.FindIssue(pullRequest.IssueID)
	return err == nil && issue.State == epicpkg.IssueStatePR
}

func (u *ReviewApprovedBranchesUseCase) apply(
	workspace application.Workspace,
	current epicpkg.Epic,
	pullRequest epicpkg.PullRequest,
	state agent_runtime.BranchState,
) error {
	// Behind is checked first. A branch that is both behind and pushed to still
	// has to be caught up before anything can judge it in its final shape.
	if state.Behind {
		return u.markStale(workspace, current, pullRequest)
	}
	if pullRequest.ReviewedHead == state.Head {
		return nil
	}
	return u.sendBackToReview(workspace, current, pullRequest, state.Head)
}

func (u *ReviewApprovedBranchesUseCase) markStale(
	workspace application.Workspace, current epicpkg.Epic, pullRequest epicpkg.PullRequest,
) error {
	return workspace.UpdateEpic(
		current.ID,
		func(target *epicpkg.Epic) error {
			if err := target.UpdatePullRequest(
				pullRequest.ID, func(record *epicpkg.PullRequest) error {
					// The approval is kept. It still describes this branch's own
					// commits, and the merge role is about to add base to them
					// — the reviewer that runs afterwards is what re-judges the
					// combination.
					return record.SetFlag(epicpkg.FlagStale, true)
				},
			); err != nil {
				return err
			}
			return target.TransitionIssue(pullRequest.IssueID, epicpkg.IssueStateStale)
		},
	)
}

// sendBackToReview counts a hand-pushed commit as a round nobody judged and
// puts a reviewer in front of it.
func (u *ReviewApprovedBranchesUseCase) sendBackToReview(
	workspace application.Workspace,
	current epicpkg.Epic,
	pullRequest epicpkg.PullRequest,
	head string,
) error {
	return workspace.UpdateEpic(
		current.ID,
		func(target *epicpkg.Epic) error {
			if err := target.UpdatePullRequest(
				pullRequest.ID, func(record *epicpkg.PullRequest) error {
					record.RecordExternalPush()
					return nil
				},
			); err != nil {
				return err
			}
			comment, err := epicpkg.CreateComment("merge", fmt.Sprintf(
				"`%s` was pushed to outside the loop and is now at %s, "+
					"which the approval did not cover. Sending it back for review.",
				pullRequest.Head, short(head),
			))
			if err != nil {
				return err
			}
			if err := target.AddPullRequestComment(pullRequest.ID, comment); err != nil {
				return err
			}
			return target.TransitionIssue(pullRequest.IssueID, epicpkg.IssueStateReview)
		},
	)
}
