package usecases

import (
	"testing"

	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

func epicForRole(state epicpkg.EpicState, childState epicpkg.IssueState) epicpkg.Epic {
	return epicpkg.Epic{
		ID: "epic-1", Title: "Improve workflow", State: state,
		Issues: []epicpkg.Issue{
			{ID: "root", Title: "Improve workflow", State: epicpkg.IssueStateOpen},
			{
				ID: "child-1", Title: "Add widget", ParentID: "root",
				Repository: "acme/widgets", State: childState,
			},
		},
	}
}

func openRecord() epicpkg.PullRequest {
	return epicpkg.PullRequest{
		ID: "pr-1", IssueID: "child-1", Title: "Add widget",
		Status: epicpkg.PullRequestOpen, Repository: "acme/widgets",
		Head: "go-merge/child-1", Base: "main",
	}
}

func TestIssueRole_ShouldWalkTheCodeReviewCycle(t *testing.T) {
	tests := []struct {
		name     string
		rounds   int
		reviews  int
		approved bool
		granted  int
		want     agent.AgentRole
		wantOK   bool
	}{
		{
			name: "a fresh pull request is coded first",
			want: agent.AgentRoleCoding, wantOK: true,
		},
		{
			name:   "code nobody judged is reviewed",
			rounds: 1, reviews: 0,
			want: agent.AgentRolePRReviewer, wantOK: true,
		},
		{
			name:   "changes requested sends it back to coding",
			rounds: 1, reviews: 1, approved: false,
			want: agent.AgentRoleCoding, wantOK: true,
		},
		{
			name:   "an approval covering the latest round waits for a human",
			rounds: 1, reviews: 1, approved: true,
			wantOK: false,
		},
		{
			name:   "out of rounds, the loop stops",
			rounds: epicpkg.MaxCodingRounds, reviews: epicpkg.MaxCodingRounds, approved: false,
			wantOK: false,
		},
		{
			name:   "a granted round restarts it",
			rounds: epicpkg.MaxCodingRounds, reviews: epicpkg.MaxCodingRounds, approved: false, granted: 1,
			want: agent.AgentRoleCoding, wantOK: true,
		},
		{
			name:   "the last round is still reviewed after the limit",
			rounds: epicpkg.MaxCodingRounds, reviews: epicpkg.MaxCodingRounds - 1, approved: false,
			want: agent.AgentRolePRReviewer, wantOK: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			current := epicForRole(epicpkg.EpicStateReady, epicpkg.IssueStatePR)
			record := openRecord()
			// Every round in this table is a coding round, which is the only
			// kind the limit counts.
			record.Rounds, record.Reviews = test.rounds, test.reviews
			record.CodingRounds = test.rounds
			record.Approved, record.RoundsGranted = test.approved, test.granted

			// Act
			role, ok := IssueRole(current, record)

			// Assert
			if ok != test.wantOK || (test.wantOK && role != test.want) {
				t.Fatalf("expected (%q, %t), got (%q, %t)", test.want, test.wantOK, role, ok)
			}
		})
	}
}

func TestIssueRole_ShouldDeclineWhenTheEpicIsNotReady(t *testing.T) {
	// Arrange: drafting is still in progress, so no branch should be touched.
	current := epicForRole(epicpkg.EpicStateReview, epicpkg.IssueStatePR)

	// Act
	_, ok := IssueRole(current, openRecord())

	// Assert
	if ok {
		t.Fatal("expected no role while the epic is still drafting")
	}
}

func TestIssueRole_ShouldDeclineForASettledPullRequest(t *testing.T) {
	// Arrange
	current := epicForRole(epicpkg.EpicStateReady, epicpkg.IssueStateMerged)
	record := openRecord()
	record.Status = epicpkg.PullRequestMerged

	// Act
	_, ok := IssueRole(current, record)

	// Assert
	if ok {
		t.Fatal("expected no role for a merged pull request")
	}
}

func epicForChildRole(childState epicpkg.IssueState) epicpkg.Epic {
	current := epicForRole(epicpkg.EpicStateReady, epicpkg.IssueStatePR)
	current.Issues = append(current.Issues, epicpkg.Issue{
		ID: "grandchild", Title: "Sub work", ParentID: "child-1",
		Repository: "acme/widgets", State: childState,
	})
	return current
}

func TestIssueRole_ShouldDeclineWhileChildrenAreUnmerged(t *testing.T) {
	// Arrange: implementing a parent before its children land means
	// implementing work the children are about to deliver.
	current := epicForChildRole(epicpkg.IssueStateOpen)

	// Act
	_, ok := IssueRole(current, openRecord())

	// Assert
	if ok {
		t.Fatal("expected a blocked parent to be skipped")
	}
}

func TestIssueRole_ShouldResumeOnceChildrenMerge(t *testing.T) {
	// Arrange
	current := epicForChildRole(epicpkg.IssueStateMerged)

	// Act
	role, ok := IssueRole(current, openRecord())

	// Assert
	if !ok || role != agent.AgentRoleCoding {
		t.Fatalf("expected coding once children merged, got (%q, %t)", role, ok)
	}
}

func TestIssueRole_ShouldDeclineWhileABlockedByReferenceIsUnmerged(t *testing.T) {
	// Arrange: nesting cannot order two siblings, so the reference is the only
	// thing holding this one back.
	current := epicForRole(epicpkg.EpicStateReady, epicpkg.IssueStatePR)
	current.Issues = append(current.Issues, epicpkg.Issue{
		ID: "child-2", Title: "Other work", ParentID: "root",
		Repository: "acme/widgets", State: epicpkg.IssueStateOpen,
	})
	current.Issues[1].BlockedBy = []string{"child-2"}

	// Act
	_, ok := IssueRole(current, openRecord())

	// Assert
	if ok {
		t.Fatal("expected an issue waiting on a sibling to be skipped")
	}
}
