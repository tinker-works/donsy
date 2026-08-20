package usecases

import (
	"context"
	"fmt"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

type GetPullRequestDiffQuery struct {
	Project       domain.Project
	EpicID        string
	PullRequestID string
}

type GetPullRequestDiffUseCase struct {
	factory application.WorkspaceFactory
	differ  application.RepositoryDiffer
}

// Handle computes the pull request's diff from the local clone of its
// repository. The tracker stores no diff and GitHub is not consulted, so this
// reflects whatever the last fetch of that repository saw.
func (u *GetPullRequestDiffUseCase) Handle(
	ctx context.Context, query GetPullRequestDiffQuery,
) (string, error) {
	if u.differ == nil {
		return "", fmt.Errorf("no repository checkout is configured to diff against")
	}
	detail, err := u.factory.Open(query.Project.Name).
		ReadEpic(query.EpicID)
	if err != nil {
		return "", err
	}
	pullRequest, err := findPullRequest(detail, query.PullRequestID)
	if err != nil {
		return "", err
	}
	if pullRequest.Head == "" || pullRequest.Base == "" {
		return "", fmt.Errorf(
			"pull request %q records no head or base branch to diff", pullRequest.ID,
		)
	}
	return u.differ.Diff(
		ctx, query.EpicID, pullRequest.Repository, pullRequest.Base, pullRequest.Head,
	)
}

func findPullRequest(detail epic.Epic, pullRequestID string) (epic.PullRequest, error) {
	for _, pullRequest := range detail.PullRequests {
		if pullRequest.ID == pullRequestID {
			return pullRequest, nil
		}
	}
	return epic.PullRequest{}, fmt.Errorf("pull request %q not found", pullRequestID)
}
