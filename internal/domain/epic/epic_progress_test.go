package epic

import "testing"

func treeWithChildren(childStates ...IssueState) Epic {
	aggregate := Epic{
		ID: "epic-1", Title: "Improve workflow", State: EpicStateReady,
		Issues: []Issue{{ID: "root", Title: "Improve workflow", State: IssueStateOpen}},
	}
	for index, state := range childStates {
		aggregate.Issues = append(aggregate.Issues, Issue{
			ID: string(rune('a' + index)), Title: "Child", ParentID: "root",
			Repository: "acme/widgets", State: state,
		})
	}
	return aggregate
}

func TestEpic_Blocked_ShouldWaitOnUnmergedChildren(t *testing.T) {
	// Arrange
	aggregate := treeWithChildren(IssueStateMerged, IssueStatePR)

	// Act & Assert
	if !aggregate.Blocked("root") {
		t.Fatal("expected a parent with an unmerged child to be blocked")
	}
	if aggregate.Blocked("a") {
		t.Fatal("expected a leaf issue never to be blocked")
	}
}

func TestEpic_Blocked_ShouldClearOnceChildrenAreTerminal(t *testing.T) {
	// Arrange: closed counts as settled — nothing will ever merge it.
	aggregate := treeWithChildren(IssueStateMerged, IssueStateClosed)

	// Act & Assert
	if aggregate.Blocked("root") {
		t.Fatal("expected merged and closed children to clear the block")
	}
}

func TestEpic_Blocked_ShouldWaitOnAnIssueBlockedByNames(t *testing.T) {
	// Arrange: siblings are unordered by nesting, so a dependency between two of
	// them is the case BlockedBy exists for.
	waiting := treeWithChildren(IssueStatePR, IssueStateOpen)
	waiting.Issues[2].BlockedBy = []string{"a"}
	settled := treeWithChildren(IssueStateMerged, IssueStateOpen)
	settled.Issues[2].BlockedBy = []string{"a"}

	// Act & Assert
	if !waiting.Blocked("b") {
		t.Fatal("expected an issue waiting on an unmerged sibling to be blocked")
	}
	if settled.Blocked("b") {
		t.Fatal("expected a merged blocker to clear the block")
	}
}

func TestEpic_Blocked_ShouldNotHoldAnIssueItCannotResolve(t *testing.T) {
	// Arrange: Validate rejects a dangling reference, but the gate is consulted
	// on every read and must not stall work behind something that cannot land.
	aggregate := treeWithChildren(IssueStateOpen)
	aggregate.Issues[1].BlockedBy = []string{"gone"}

	// Act & Assert
	if aggregate.Blocked("missing") {
		t.Fatal("expected an unknown issue not to be reported as blocked")
	}
	if aggregate.Blocked("a") {
		t.Fatal("expected a missing blocker not to hold an issue up")
	}
}

func TestEpic_Delivered_ShouldRequireEveryNonRootIssueSettled(t *testing.T) {
	// Arrange
	inFlight := treeWithChildren(IssueStateMerged, IssueStatePR)
	settled := treeWithChildren(IssueStateMerged, IssueStateClosed)

	// Act & Assert
	if inFlight.Delivered() {
		t.Fatal("expected an epic with an open pull request not to be delivered")
	}
	if !settled.Delivered() {
		t.Fatal("expected an epic with everything settled to be delivered")
	}
}

func TestEpic_Delivered_ShouldRejectAnUndraftedEpic(t *testing.T) {
	// Arrange: a root with no children was never drafted, so there is
	// nothing to have delivered.
	aggregate := treeWithChildren()

	// Act & Assert
	if aggregate.Delivered() {
		t.Fatal("expected an epic with no non-root issues not to be delivered")
	}
}

func TestEpic_OpenPullRequestFor_ShouldIgnoreSettledRecords(t *testing.T) {
	// Arrange
	aggregate := treeWithChildren(IssueStatePR)
	aggregate.PullRequests = []PullRequest{
		{ID: "pr-old", IssueID: "a", Title: "First try", Status: PullRequestClosed},
		{ID: "pr-new", IssueID: "a", Title: "Add widget", Status: PullRequestOpen},
	}

	// Act
	pullRequest, ok := aggregate.OpenPullRequestFor("a")

	// Assert
	if !ok || pullRequest.ID != "pr-new" {
		t.Fatalf("expected the open record, got %+v (found=%t)", pullRequest, ok)
	}
}

func TestEpic_UpdatePullRequest_ShouldValidateTheResult(t *testing.T) {
	// Arrange
	aggregate := treeWithChildren(IssueStatePR)
	aggregate.PullRequests = []PullRequest{
		{ID: "pr-1", IssueID: "a", Title: "Add widget", Status: PullRequestOpen},
	}

	// Act: a change that leaves the record invalid must not be kept silently.
	err := aggregate.UpdatePullRequest("pr-1", func(pullRequest *PullRequest) error {
		pullRequest.Rounds = -1
		return nil
	})

	// Assert
	if err == nil {
		t.Fatal("expected an invalid update to be rejected")
	}
}
