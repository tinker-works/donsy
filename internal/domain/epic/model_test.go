package epic

import "testing"

func TestNewIssueCreatesOpenIssue(t *testing.T) {
	// Arrange

	// Act
	issue, err := CreateIssue(" Root issue ", "Details")
	if err != nil {
		t.Fatal(err)
	}
	// Assert
	if issue.ID == "" || issue.Title != "Root issue" ||
		issue.Body != "Details" || issue.State != IssueStateOpen {
		t.Fatalf("unexpected issue: %#v", issue)
	}
}

func TestEpicCreatesRootIssue(t *testing.T) {
	// Arrange

	// Act
	aggregate, err := CreateEpic(" Root issue ", " owner ", "Details")
	if err != nil {
		t.Fatal(err)
	}
	// Assert
	if aggregate.ID == "" || aggregate.Title != "Root issue" ||
		aggregate.Assignee != "owner" || aggregate.Body != "Details" ||
		aggregate.State != EpicStateConcept || len(aggregate.Issues) != 1 {
		t.Fatalf("unexpected aggregate: %#v", aggregate)
	}
	root := aggregate.Issues[0]
	if root.ID == "" || root.Title != "Root issue" || root.Body != "Details" ||
		root.State != IssueStateOpen || root.CreatedAt.IsZero() {
		t.Fatalf("unexpected root issue: %#v", root)
	}
}

func TestEpicRejectsParentCycle(t *testing.T) {
	// Arrange
	detail := Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: EpicStateConcept,
		Issues: []Issue{
			{ID: "root", Title: "Root", State: IssueStateOpen, ParentID: "child"},
			{ID: "child", Title: "Child", State: IssueStateOpen, ParentID: "root"},
		},
	}
	// Act
	if err := detail.Validate(); err == nil {
		// Assert
		t.Fatal("expected parent cycle to fail validation")
	}
}

func TestIssueTransitions(t *testing.T) {
	// Arrange
	issue, err := CreateIssue("Issue", "")
	if err != nil {
		t.Fatal(err)
	}
	// Act
	for _, state := range []IssueState{
		IssueStateCoding, IssueStateReview, IssueStatePR, IssueStateMerged,
	} {
		if err := issue.TransitionTo(state); err != nil {
			t.Fatal(err)
		}
	}
	// Assert
	if err := issue.TransitionTo(IssueStateClosed); err == nil {
		t.Fatal("expected merged issue to reject transition to closed")
	}
}

func TestIssueCannotReachPRWithoutAReview(t *testing.T) {
	// Arrange
	for _, initial := range []IssueState{IssueStateOpen, IssueStateCoding} {
		issue := Issue{State: initial}

		// Act
		err := issue.TransitionTo(IssueStatePR)

		// Assert
		if err == nil {
			t.Fatalf("expected %s to refuse a jump to Pr", initial)
		}
	}
}

func TestIssueStaleShouldReturnToReviewNotStraightToPR(t *testing.T) {
	// Arrange
	issue := Issue{State: IssueStateStale}

	// Act
	err := issue.TransitionTo(IssueStatePR)

	// Assert
	if err == nil {
		t.Fatal("expected a resolved merge to need review before Pr")
	}
	if err := issue.TransitionTo(IssueStateReview); err != nil {
		t.Fatalf("expected a resolved merge to go back to review: %v", err)
	}
}

func TestIssueOnlyApprovedWorkCanGoStale(t *testing.T) {
	// Arrange
	for _, initial := range []IssueState{
		IssueStateOpen, IssueStateCoding, IssueStateReview,
	} {
		issue := Issue{State: initial}

		// Act
		err := issue.TransitionTo(IssueStateStale)

		// Assert
		if err == nil {
			t.Fatalf("expected %s to refuse going stale", initial)
		}
	}
}

func TestIssueChangesRequestedShouldReturnToCoding(t *testing.T) {
	// Arrange
	issue := Issue{State: IssueStateReview}

	// Act
	if err := issue.TransitionTo(IssueStateCoding); err != nil {
		// Assert
		t.Fatalf("expected a review to send the issue back to coding: %v", err)
	}
}

func TestIssueCanCloseFromAnyLivePhase(t *testing.T) {
	// Arrange
	for _, initial := range []IssueState{
		IssueStateOpen, IssueStateCoding, IssueStateReview, IssueStatePR,
	} {
		issue := Issue{State: initial}

		// Act
		if err := issue.TransitionTo(IssueStateClosed); err != nil {
			// Assert
			t.Fatalf("expected %s to close: %v", initial, err)
		}
	}
}

func TestEpicTransitions(t *testing.T) {
	// Arrange
	epic, err := CreateEpic("Epic", "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	// Act
	for _, state := range []EpicState{
		EpicStateRefine,
		EpicStateReview,
		EpicStateProposed,
		EpicStateReady,
		EpicStateDone,
		EpicStateClosed,
	} {
		if err := epic.TransitionTo(state); err != nil {
			t.Fatal(err)
		}
	}
	// Assert
	if err := epic.TransitionTo(EpicStateConcept); err == nil {
		t.Fatal("expected closed epic to reject transition")
	}
}
