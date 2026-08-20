package usecases

import (
	"fmt"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

func TestCompleteEpicUseCase_ShouldDeclineWhileTheEpicIsNotReady(t *testing.T) {
	// Arrange: delivered issues alone are not enough — a Review epic's merges
	// are drafting artifacts, not the finish line.
	workspace := &fakeWorkspace{
		detail: epicForRole(epicpkg.EpicStateReview, epicpkg.IssueStateMerged),
	}
	useCase := &CompleteEpicUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	completed, err := useCase.Handle(CompleteEpicCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if completed {
		t.Fatal("expected an epic outside Ready to be left alone")
	}
	if workspace.updatedEpicID != "" {
		t.Fatalf("expected no write, got update of %q", workspace.updatedEpicID)
	}
}

func TestCompleteEpicUseCase_ShouldDeclineWhileIssuesAreStillInFlight(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{
		detail: epicForRole(epicpkg.EpicStateReady, epicpkg.IssueStateOpen),
	}
	useCase := &CompleteEpicUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	completed, err := useCase.Handle(CompleteEpicCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if completed {
		t.Fatal("expected an undelivered epic to stay Ready")
	}
	if workspace.detail.State != epicpkg.EpicStateReady {
		t.Fatalf("expected the epic untouched, got %q", workspace.detail.State)
	}
}

func TestCompleteEpicUseCase_ShouldCompleteADeliveredEpic(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{
		detail: epicForRole(epicpkg.EpicStateReady, epicpkg.IssueStateMerged),
	}
	useCase := &CompleteEpicUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	completed, err := useCase.Handle(CompleteEpicCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !completed {
		t.Fatal("expected a delivered Ready epic to complete")
	}
	if workspace.detail.State != epicpkg.EpicStateDone {
		t.Fatalf("expected Done, got %q", workspace.detail.State)
	}
	if workspace.updatedEpicID != "epic-1" {
		t.Fatalf("unexpected write: %q", workspace.updatedEpicID)
	}
}

func TestCompleteEpicUseCase_ShouldPropagateAReadError(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{readEpicErr: fmt.Errorf("epic gone")}
	useCase := &CompleteEpicUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	completed, err := useCase.Handle(CompleteEpicCommand{
		Project: domain.Project{Name: "one"},
		EpicID:  "epic-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected the read error to propagate")
	}
	if completed {
		t.Fatal("expected no completion on a failed read")
	}
}
