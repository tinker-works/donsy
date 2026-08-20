package usecases

import (
	"context"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

type fakeDiffer struct {
	diff       string
	err        error
	epicID     string
	repository string
	base       string
	head       string
}

func (d *fakeDiffer) Diff(
	_ context.Context, epicID, repository, base, head string,
) (string, error) {
	d.epicID, d.repository, d.base, d.head = epicID, repository, base, head
	return d.diff, d.err
}

func epicWithPullRequest(pullRequest epic.PullRequest) epic.Epic {
	return epic.Epic{
		ID: "epic-1", Title: "Improve workflow",
		PullRequests: []epic.PullRequest{pullRequest},
	}
}

func TestGetPullRequestDiffUseCase_Handle_ShouldDiffTheRecordedBranches(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epicWithPullRequest(epic.PullRequest{
		ID: "pr-1", IssueID: "issue-1", Title: "Add widget",
		Repository: "acme/widgets", Head: "feature/widget", Base: "main",
		Status: epic.PullRequestOpen,
	})}
	differ := &fakeDiffer{diff: "diff --git a/main.go b/main.go\n"}
	useCase := &GetPullRequestDiffUseCase{
		factory: &fakeFactory{workspace: workspace}, differ: differ,
	}

	// Act
	diff, err := useCase.Handle(context.Background(), GetPullRequestDiffQuery{
		Project:       domain.Project{Name: "one"},
		EpicID:        "epic-1",
		PullRequestID: "pr-1",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if diff != differ.diff {
		t.Fatalf("unexpected diff: %q", diff)
	}
	if differ.repository != "acme/widgets" || differ.base != "main" ||
		differ.head != "feature/widget" || differ.epicID != "epic-1" {
		t.Fatalf("unexpected diff request: %+v", differ)
	}
}

func TestGetPullRequestDiffUseCase_Handle_ShouldFailWhenBranchesAreMissing(t *testing.T) {
	// Arrange: CreatePullRequestUseCase accepts an empty head and base, so a
	// recorded pull request may have nothing to diff.
	workspace := &fakeWorkspace{detail: epicWithPullRequest(epic.PullRequest{
		ID: "pr-1", IssueID: "issue-1", Title: "Add widget",
		Repository: "acme/widgets", Status: epic.PullRequestOpen,
	})}
	useCase := &GetPullRequestDiffUseCase{
		factory: &fakeFactory{workspace: workspace}, differ: &fakeDiffer{},
	}

	// Act
	_, err := useCase.Handle(context.Background(), GetPullRequestDiffQuery{
		Project:       domain.Project{Name: "one"},
		EpicID:        "epic-1",
		PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected a pull request with no branches to be rejected")
	}
}

func TestGetPullRequestDiffUseCase_Handle_ShouldFailForAnUnknownPullRequest(t *testing.T) {
	// Arrange
	useCase := &GetPullRequestDiffUseCase{
		factory: &fakeFactory{workspace: &fakeWorkspace{detail: epic.Epic{ID: "epic-1"}}},
		differ:  &fakeDiffer{},
	}

	// Act
	_, err := useCase.Handle(context.Background(), GetPullRequestDiffQuery{
		Project:       domain.Project{Name: "one"},
		EpicID:        "epic-1",
		PullRequestID: "missing",
	})

	// Assert
	if err == nil {
		t.Fatal("expected an unknown pull request to be rejected")
	}
}
