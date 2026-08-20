package usecases

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

func TestCloseIssueUseCase_ShouldCloseIssueAndRelatedOpenPullRequest(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateConcept,
		Issues: []epic.Issue{{ID: "root", Title: "Root", State: epic.IssueStatePR}},
		PullRequests: []epic.PullRequest{{
			ID: "pr", IssueID: "root", Title: "PR", Status: epic.PullRequestOpen,
		}},
	}}
	useCase := &CloseIssueUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(context.Background(), CloseIssueCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "aggregate",
		IssueID: "root",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.Issues[0].State != epic.IssueStateClosed ||
		workspace.detail.PullRequests[0].Status != epic.PullRequestClosed {
		t.Fatalf("unexpected closed aggregate: %#v", workspace.detail)
	}
	if workspace.updatedEpicID != "aggregate" {
		t.Fatalf("unexpected update epic: %q", workspace.updatedEpicID)
	}
}

func TestCloseIssueUseCase_ShouldDeleteTheBranchBehindTheClosedPullRequest(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateReady,
		Issues: []epic.Issue{
			{ID: "root", Title: "Root", State: epic.IssueStateOpen},
			{
				ID: "child", Title: "Child", ParentID: "root",
				Repository: "acme/widgets", State: epic.IssueStateCoding,
			},
		},
		PullRequests: []epic.PullRequest{{
			ID: "pr", IssueID: "child", Title: "PR", Status: epic.PullRequestOpen,
			Repository: "acme/widgets", Head: "go-merge/child", Base: "main",
		}},
	}}
	code := newFakeCodeWorkspace()
	useCase := &CloseIssueUseCase{factory: &fakeFactory{workspace: workspace}, code: code}

	// Act
	err := useCase.Handle(context.Background(), CloseIssueCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "aggregate",
		IssueID: "child",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(code.deleted) != 1 || code.deleted[0] != "go-merge/child" {
		t.Fatalf("expected the abandoned branch to be deleted, got %v", code.deleted)
	}
}

// A branch that cannot be deleted must not undo the close: the record is
// already written, and the person asked for the work to be abandoned.
func TestCloseIssueUseCase_ShouldReportButNotUndoAFailedBranchDelete(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateReady,
		Issues: []epic.Issue{
			{ID: "root", Title: "Root", State: epic.IssueStateOpen},
			{
				ID: "child", Title: "Child", ParentID: "root",
				Repository: "acme/widgets", State: epic.IssueStateCoding,
			},
		},
		PullRequests: []epic.PullRequest{{
			ID: "pr", IssueID: "child", Title: "PR", Status: epic.PullRequestOpen,
			Repository: "acme/widgets", Head: "go-merge/child", Base: "main",
		}},
	}}
	code := newFakeCodeWorkspace()
	code.deleteErr = errors.New("remote refused")
	useCase := &CloseIssueUseCase{factory: &fakeFactory{workspace: workspace}, code: code}

	// Act
	err := useCase.Handle(context.Background(), CloseIssueCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "aggregate",
		IssueID: "child",
	})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "go-merge/child") {
		t.Fatalf("expected the lingering branch to be reported, got %v", err)
	}
	if workspace.detail.Issues[1].State != epic.IssueStateClosed ||
		workspace.detail.PullRequests[0].Status != epic.PullRequestClosed {
		t.Fatalf("expected the close to stand: %#v", workspace.detail)
	}
}
