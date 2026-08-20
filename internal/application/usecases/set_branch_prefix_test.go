package usecases

import (
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

func TestSetBranchPrefixUseCase_ShouldStoreSluggedPrefix(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: readyEpic()}
	useCase := &SetBranchPrefixUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(SetBranchPrefixCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
		Prefix:  "JIRA-123",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.BranchPrefix != "jira-123" {
		t.Fatalf("unexpected prefix: %q", workspace.detail.BranchPrefix)
	}
}

func TestSetBranchPrefixUseCase_ShouldRejectAnEpicWithBranchesAlreadyCut(t *testing.T) {
	// Arrange: those branches are on the remote under their old names already.
	detail := readyEpic()
	detail.PullRequests = []epicpkg.PullRequest{{
		ID: "pr-1", IssueID: "child-1", Title: "Add widget",
		Status: epicpkg.PullRequestOpen, Repository: "acme/widgets",
		Head: "gm/add-widget-child-1", Base: "main",
	}}
	workspace := &fakeWorkspace{detail: detail}
	useCase := &SetBranchPrefixUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(SetBranchPrefixCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
		Prefix:  "jira-456",
	})

	// Assert
	if err == nil {
		t.Fatal("expected setting a prefix after branches are cut to fail")
	}
	if workspace.detail.BranchPrefix != "" {
		t.Fatalf("refused change must not be stored, got %q", workspace.detail.BranchPrefix)
	}
}
