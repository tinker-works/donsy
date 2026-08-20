package usecases

import (
	"testing"

	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

func TestTerminalSubjects_ShouldIncludeMergedAndClosedIssues(t *testing.T) {
	// Arrange
	epics := []epicpkg.Epic{{
		ID: "epic-1", State: epicpkg.EpicStateReady,
		Issues: []epicpkg.Issue{
			{ID: "merged", State: epicpkg.IssueStateMerged},
			{ID: "closed", State: epicpkg.IssueStateClosed},
			{ID: "coding", State: epicpkg.IssueStateCoding},
		},
	}}

	// Act
	terminal := TerminalSubjects(epics)

	// Assert
	assertTerminal(t, terminal, agent.AgentSubjectIssue, "merged", true)
	assertTerminal(t, terminal, agent.AgentSubjectIssue, "closed", true)
	assertTerminal(t, terminal, agent.AgentSubjectIssue, "coding", false)
	assertTerminal(t, terminal, agent.AgentSubjectEpic, "epic-1", false)
}

func TestTerminalSubjects_ShouldIncludeEveryIssueOfAFinishedEpic(t *testing.T) {
	// Closing or completing an epic ends the work under it too, whatever state the
	// individual issues were left in — nothing will run against them again.
	// Arrange
	epics := []epicpkg.Epic{
		{
			ID: "done-epic", State: epicpkg.EpicStateDone,
			Issues: []epicpkg.Issue{{ID: "issue-1", State: epicpkg.IssueStateCoding}},
		},
		{
			ID: "closed-epic", State: epicpkg.EpicStateClosed,
			Issues: []epicpkg.Issue{{ID: "issue-2", State: epicpkg.IssueStateReview}},
		},
	}

	// Act
	terminal := TerminalSubjects(epics)

	// Assert
	assertTerminal(t, terminal, agent.AgentSubjectEpic, "done-epic", true)
	assertTerminal(t, terminal, agent.AgentSubjectEpic, "closed-epic", true)
	assertTerminal(t, terminal, agent.AgentSubjectIssue, "issue-1", true)
	assertTerminal(t, terminal, agent.AgentSubjectIssue, "issue-2", true)
}

func TestTerminalSubjects_ShouldSpareFailedEpicsAndApprovedPullRequests(t *testing.T) {
	// A Failed epic transitions back to Concept, and an approval is invalidated by
	// the next push. Both come back to the same sandbox, and whoever restarts them is
	// exactly who benefits from its OpenCode session still being there. Neither is
	// finished, so both are left to the idle clock instead.
	// Arrange
	epics := []epicpkg.Epic{{
		ID: "epic-1", State: epicpkg.EpicStateFailed,
		Issues: []epicpkg.Issue{{ID: "approved", State: epicpkg.IssueStatePR}},
	}}

	// Act
	terminal := TerminalSubjects(epics)

	// Assert
	if len(terminal) != 0 {
		t.Fatalf("expected nothing to be terminal, got %#v", terminal)
	}
}

func TestTerminalSubjects_ShouldBeEmptyWithoutEpics(t *testing.T) {
	// An empty or failed read must reclaim nothing rather than everything: this set
	// is what authorises deleting a sandbox, so its failure mode has to be inaction.
	// Act
	terminal := TerminalSubjects(nil)

	// Assert
	if len(terminal) != 0 {
		t.Fatalf("expected no subjects, got %#v", terminal)
	}
}

func assertTerminal(
	t *testing.T,
	terminal map[agent.AgentSubject]struct{},
	kind agent.AgentSubjectKind,
	id string,
	want bool,
) {
	t.Helper()
	_, got := terminal[agent.AgentSubject{Kind: kind, ID: id}]
	if got != want {
		t.Fatalf("subject %s %q: terminal = %t, want %t", kind, id, got, want)
	}
}
