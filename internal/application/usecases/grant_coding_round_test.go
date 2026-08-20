package usecases

import (
	"context"
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

// spentPullRequest is an epic whose one pull request has used every coding round
// it was allowed, which is the state the loop parks and never dispatches again.
func spentPullRequest(t *testing.T) *fakeWorkspace {
	t.Helper()
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateReady,
		Issues: []epic.Issue{{ID: "root", Title: "Root", State: epic.IssueStateOpen}},
	}}
	pullRequest, err := epic.CreatePullRequest("root", "PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.detail.AddPullRequest("root", pullRequest); err != nil {
		t.Fatal(err)
	}
	if err := workspace.detail.UpdatePullRequest(
		pullRequest.ID,
		func(record *epic.PullRequest) error {
			// Each round is coded and then judged, so Reviews catches up to Rounds
			// and the pull request ends where the loop actually parks one: nothing
			// awaiting a verdict, no approval, and no coding round left.
			for range epic.MaxCodingRounds {
				if err := record.RecordCodingRound(); err != nil {
					return err
				}
				if err := record.RecordReview(false, "head", "base"); err != nil {
					return err
				}
			}
			return nil
		},
	); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func grant(t *testing.T, workspace *fakeWorkspace) error {
	t.Helper()
	useCase := &GrantCodingRoundUseCase{factory: &fakeFactory{workspace: workspace}}
	return useCase.Handle(context.Background(), GrantCodingRoundCommand{
		Project:       domain.Project{Name: "one"},
		EpicID:        "aggregate",
		PullRequestID: workspace.detail.PullRequests[0].ID,
	})
}

// Nothing else raises the ceiling, so this is the only thing that gets a parked
// pull request moving: IssueRole hands out no role at all while CanCode is false.
func TestGrantCodingRoundUseCase_ShouldGiveAParkedPullRequestAnotherRound(t *testing.T) {
	// Arrange
	workspace := spentPullRequest(t)
	if _, due := IssueRole(workspace.detail, workspace.detail.PullRequests[0]); due {
		t.Fatal("expected the fixture to be parked with no role due")
	}

	// Act
	err := grant(t, workspace)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	updated := workspace.detail.PullRequests[0]
	if updated.RoundsGranted != 1 {
		t.Fatalf("expected exactly one granted round, got %d", updated.RoundsGranted)
	}
	if updated.HasFlag(epic.FlagRoundLimit) {
		t.Fatalf("expected the round-limit flag cleared, got %v", updated.Flags)
	}
	// The role decision is the whole point: a grant nothing dispatches is a grant
	// that changed a counter and nothing else.
	role, due := IssueRole(workspace.detail, updated)
	if !due || role != agent.AgentRoleCoding {
		t.Fatalf("expected a coding round to become due, got (%q, %t)", role, due)
	}
}

// One round, not a reset: the limit exists to make a coder and a reviewer that
// disagree terminate, so a human stays in the loop for each further round.
func TestGrantCodingRoundUseCase_ShouldGrantOneRoundAtATime(t *testing.T) {
	// Arrange
	workspace := spentPullRequest(t)

	// Act
	if err := grant(t, workspace); err != nil {
		t.Fatal(err)
	}
	if err := workspace.detail.UpdatePullRequest(
		workspace.detail.PullRequests[0].ID,
		func(record *epic.PullRequest) error { return record.RecordCodingRound() },
	); err != nil {
		t.Fatal(err)
	}

	// Assert: spending the granted round parks it again rather than leaving slack.
	updated := workspace.detail.PullRequests[0]
	if updated.CanCode() {
		t.Fatalf("expected the granted round to be one-shot, got %+v", updated)
	}
	if !updated.HasFlag(epic.FlagRoundLimit) {
		t.Fatalf("expected the round-limit flag set again, got %v", updated.Flags)
	}
}

// The loop retries an ordinary failure on its own backoff. Granting there would
// spend a human's override on something already in hand and quietly raise the
// ceiling for the disagreement the limit exists to end.
func TestGrantCodingRoundUseCase_ShouldRefusePullRequestThatStillHasRounds(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner", State: epic.EpicStateReady,
		Issues: []epic.Issue{{ID: "root", Title: "Root", State: epic.IssueStateOpen}},
	}}
	pullRequest, err := epic.CreatePullRequest("root", "PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	if err := workspace.detail.AddPullRequest("root", pullRequest); err != nil {
		t.Fatal(err)
	}

	// Act
	err = grant(t, workspace)

	// Assert
	if err == nil {
		t.Fatal("expected a pull request with rounds left to be refused")
	}
	if !strings.Contains(err.Error(), "retries on its own") {
		t.Fatalf("expected the reason to name the loop's own retry, got %v", err)
	}
	if workspace.detail.PullRequests[0].RoundsGranted != 0 {
		t.Fatalf("expected nothing granted, got %+v", workspace.detail.PullRequests[0])
	}
}

func TestGrantCodingRoundUseCase_ShouldRefuseAClosedPullRequest(t *testing.T) {
	// Arrange
	workspace := spentPullRequest(t)
	if err := workspace.detail.UpdatePullRequest(
		workspace.detail.PullRequests[0].ID,
		func(record *epic.PullRequest) error {
			record.Status = epic.PullRequestClosed
			return nil
		},
	); err != nil {
		t.Fatal(err)
	}

	// Act
	err := grant(t, workspace)

	// Assert
	if err == nil {
		t.Fatal("expected a closed pull request to be refused")
	}
	if workspace.detail.PullRequests[0].RoundsGranted != 0 {
		t.Fatalf("expected nothing granted, got %+v", workspace.detail.PullRequests[0])
	}
}
