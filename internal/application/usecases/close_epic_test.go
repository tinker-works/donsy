package usecases

import (
	"context"
	"github.com/tinker-works/donsy/internal/domain/epic"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
)

func TestCloseEpicUseCase_ShouldCloseEpic(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateConcept,
		Issues: []epic.Issue{{ID: "root", Title: "Root", State: epic.IssueStateOpen}},
	}}
	useCase := &CloseEpicUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(context.Background(), CloseEpicCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "aggregate",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.State != epic.EpicStateClosed {
		t.Fatalf("unexpected epic state: %s", workspace.detail.State)
	}
	if workspace.updatedEpicID != "aggregate" {
		t.Fatalf("unexpected update epic: %q", workspace.updatedEpicID)
	}
}

// Closing an epic abandons the whole tree. Leaving its issues open would keep
// pull requests alive against branches for work nobody intends to finish.
func TestCloseEpicUseCase_ShouldCloseTheTreeAndDeleteItsBranches(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateReady,
		Issues: []epic.Issue{
			{ID: "root", Title: "Root", State: epic.IssueStateOpen},
			{
				ID: "coding", Title: "Coding", ParentID: "root",
				Repository: "acme/widgets", State: epic.IssueStateCoding,
			},
			{
				ID: "done", Title: "Delivered", ParentID: "root",
				Repository: "acme/gadgets", State: epic.IssueStateMerged,
			},
		},
		PullRequests: []epic.PullRequest{
			{
				ID: "pr-open", IssueID: "coding", Title: "Coding", Status: epic.PullRequestOpen,
				Repository: "acme/widgets", Head: "go-merge/coding", Base: "main",
			},
			{
				ID: "pr-merged", IssueID: "done", Title: "Delivered",
				Status:     epic.PullRequestMerged,
				Repository: "acme/gadgets", Head: "go-merge/done", Base: "main",
			},
		},
	}}
	code := newFakeCodeWorkspace()
	useCase := &CloseEpicUseCase{factory: &fakeFactory{workspace: workspace}, code: code}

	// Act
	err := useCase.Handle(context.Background(), CloseEpicCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "aggregate",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.State != epic.EpicStateClosed {
		t.Fatalf("unexpected epic state: %s", workspace.detail.State)
	}
	if workspace.detail.Issues[0].State != epic.IssueStateClosed ||
		workspace.detail.Issues[1].State != epic.IssueStateClosed {
		t.Fatalf("expected the tree to close: %#v", workspace.detail.Issues)
	}
	// The merged issue and its branch are left as they are — that work landed.
	if workspace.detail.Issues[2].State != epic.IssueStateMerged {
		t.Fatalf("expected the merged issue to survive: %#v", workspace.detail.Issues[2])
	}
	if len(code.deleted) != 1 || code.deleted[0] != "go-merge/coding" {
		t.Fatalf("expected only the abandoned branch to be deleted, got %v", code.deleted)
	}
}
