package epic

import "testing"

func TestCreatePullRequestMintsID(t *testing.T) {
	// Arrange

	// Act
	pullRequest, err := CreatePullRequest("issue", " Title ", " repo ", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	// Assert
	if pullRequest.ID == "" || pullRequest.IssueID != "issue" || pullRequest.Title != "Title" {
		t.Fatalf("unexpected pull request: %#v", pullRequest)
	}
}

func TestPullRequest_Validate_ShouldRejectMoreCodingRoundsThanRounds(t *testing.T) {
	// Arrange
	pullRequest, err := CreatePullRequest("issue", "Title", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	pullRequest.Rounds = 2
	pullRequest.CodingRounds = 3

	// Act
	err = pullRequest.Validate()

	// Assert
	if err == nil {
		t.Fatal("expected more coding rounds than total rounds to be rejected")
	}
}

func TestPullRequestOwnsComments(t *testing.T) {
	// Arrange
	pullRequest, err := CreatePullRequest("issue", "Title", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	comment, err := CreateComment("author", "body")
	if err != nil {
		t.Fatal(err)
	}
	// Act
	if err := pullRequest.AddComment(comment); err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(pullRequest.Comments) != 1 || pullRequest.Comments[0].Body != "body" {
		t.Fatalf("unexpected comments: %#v", pullRequest.Comments)
	}
}

func TestPullRequest_TransitionTo_ShouldMergeOrCloseOpenRequest(t *testing.T) {
	// Arrange
	for _, status := range []PullRequestStatus{PullRequestMerged, PullRequestClosed} {
		pullRequest, err := CreatePullRequest("issue", "Title", "repo", "head", "base")
		if err != nil {
			t.Fatal(err)
		}

		// Act
		if err := pullRequest.TransitionTo(status); err != nil {
			t.Fatalf("expected transition to %q: %v", status, err)
		}

		// Assert
		if pullRequest.Status != status {
			t.Fatalf("expected status %q, got %q", status, pullRequest.Status)
		}
	}
}

// reviewedRecord is a pull request mid-loop: one judged coding round, with the
// verdict recorded against commits, the shape every Record* method starts from.
func reviewedRecord(t *testing.T, approved bool) PullRequest {
	t.Helper()
	pullRequest, err := CreatePullRequest("issue", "Title", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	pullRequest.Rounds, pullRequest.CodingRounds, pullRequest.Reviews = 1, 1, 1
	pullRequest.Approved = approved
	pullRequest.ReviewedHead, pullRequest.ReviewedBase = "abc123", "def456"
	return pullRequest
}

func TestPullRequest_RecordCodingRound_ShouldInvalidateThePriorVerdict(t *testing.T) {
	// Arrange
	pullRequest := reviewedRecord(t, true)
	if err := pullRequest.AddFlag(FlagFailed); err != nil {
		t.Fatal(err)
	}

	// Act
	if err := pullRequest.RecordCodingRound(); err != nil {
		t.Fatal(err)
	}

	// Assert: the new round awaits review and owes the reviewer a fresh verdict.
	if pullRequest.Rounds != 2 || pullRequest.CodingRounds != 2 {
		t.Fatalf("expected both counters bumped, got %#v", pullRequest)
	}
	if pullRequest.Approved || pullRequest.ReviewedHead != "" || pullRequest.ReviewedBase != "" {
		t.Fatalf("expected the verdict dropped, got %#v", pullRequest)
	}
	if pullRequest.HasFlag(FlagFailed) {
		t.Fatal("expected a published round to clear the failure flag")
	}
	if err := pullRequest.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPullRequest_RecordCodingRound_ShouldFlagTheLastRound(t *testing.T) {
	// Arrange: one round left before the limit.
	pullRequest := reviewedRecord(t, false)
	pullRequest.Rounds = MaxCodingRounds - 1
	pullRequest.CodingRounds = MaxCodingRounds - 1
	pullRequest.Reviews = pullRequest.Rounds

	// Act
	if err := pullRequest.RecordCodingRound(); err != nil {
		t.Fatal(err)
	}

	// Assert
	if !pullRequest.HasFlag(FlagRoundLimit) {
		t.Fatal("expected the round that spends the limit to raise the flag")
	}
}

func TestPullRequest_GrantCodingRound_ShouldLiftTheLimitByOne(t *testing.T) {
	// Arrange: every round spent and judged, which is where the loop parks one.
	pullRequest := reviewedRecord(t, false)
	pullRequest.Rounds = MaxCodingRounds
	pullRequest.CodingRounds = MaxCodingRounds
	pullRequest.Reviews = pullRequest.Rounds
	if err := pullRequest.AddFlag(FlagRoundLimit); err != nil {
		t.Fatal(err)
	}

	// Act
	if err := pullRequest.GrantCodingRound(); err != nil {
		t.Fatal(err)
	}

	// Assert
	if !pullRequest.CanCode() {
		t.Fatalf("expected a coding round available, got %+v", pullRequest)
	}
	if pullRequest.HasFlag(FlagRoundLimit) {
		t.Fatalf("expected the round-limit flag cleared, got %v", pullRequest.Flags)
	}
}

func TestPullRequest_GrantCodingRound_ShouldBeOneShot(t *testing.T) {
	// Arrange
	pullRequest := reviewedRecord(t, false)
	pullRequest.Rounds = MaxCodingRounds
	pullRequest.CodingRounds = MaxCodingRounds
	pullRequest.Reviews = pullRequest.Rounds
	if err := pullRequest.GrantCodingRound(); err != nil {
		t.Fatal(err)
	}

	// Act
	if err := pullRequest.RecordCodingRound(); err != nil {
		t.Fatal(err)
	}

	// Assert
	if pullRequest.CanCode() {
		t.Fatalf("expected the granted round to be spent, got %+v", pullRequest)
	}
	if !pullRequest.HasFlag(FlagRoundLimit) {
		t.Fatal("expected the round-limit flag raised again")
	}
}

func TestPullRequest_GrantCodingRound_ShouldNotCountARound(t *testing.T) {
	// Arrange
	pullRequest := reviewedRecord(t, false)
	pullRequest.Rounds = MaxCodingRounds
	pullRequest.CodingRounds = MaxCodingRounds
	pullRequest.Reviews = pullRequest.Rounds

	// Act
	if err := pullRequest.GrantCodingRound(); err != nil {
		t.Fatal(err)
	}

	// Assert
	if pullRequest.Rounds != MaxCodingRounds || pullRequest.CodingRounds != MaxCodingRounds {
		t.Fatalf("expected the counters untouched, got %+v", pullRequest)
	}
	if pullRequest.Reviews != MaxCodingRounds {
		t.Fatalf("expected reviews untouched, got %d", pullRequest.Reviews)
	}
	if err := pullRequest.Validate(); err != nil {
		t.Fatalf("expected the record to stay valid, got %v", err)
	}
}

func TestPullRequest_RecordMergeRound_ShouldNotSpendACodingRound(t *testing.T) {
	// Arrange: an approved branch that fell behind base.
	pullRequest := reviewedRecord(t, true)
	if err := pullRequest.AddFlag(FlagStale); err != nil {
		t.Fatal(err)
	}

	// Act
	pullRequest.RecordMergeRound()

	// Assert: the resolution is judged, but the issue keeps its attempts.
	if pullRequest.Rounds != 2 || pullRequest.CodingRounds != 1 {
		t.Fatalf("expected a round without a coding round, got %#v", pullRequest)
	}
	if pullRequest.Approved || pullRequest.HasFlag(FlagStale) {
		t.Fatalf("expected the verdict dropped and staleness cleared, got %#v", pullRequest)
	}
	if err := pullRequest.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPullRequest_RecordExternalPush_ShouldSendItBackForReview(t *testing.T) {
	// Arrange
	pullRequest := reviewedRecord(t, true)

	// Act
	pullRequest.RecordExternalPush()

	// Assert: Reviews now trails Rounds, and the old approval covers nothing.
	if pullRequest.Rounds != 2 || pullRequest.CodingRounds != 1 || pullRequest.Reviews != 1 {
		t.Fatalf("expected an unjudged round without a coding round, got %#v", pullRequest)
	}
	if pullRequest.Approved || pullRequest.ReviewedHead != "" {
		t.Fatalf("expected the verdict dropped, got %#v", pullRequest)
	}
}

func TestPullRequest_RecordReview_ShouldCatchReviewsUpToRounds(t *testing.T) {
	// Arrange: two rounds ran, none judged yet.
	pullRequest := reviewedRecord(t, false)
	pullRequest.Rounds, pullRequest.CodingRounds, pullRequest.Reviews = 2, 2, 0

	// Act
	if err := pullRequest.RecordReview(false, "new-head", "new-base"); err != nil {
		t.Fatal(err)
	}

	// Assert: request-changes is still a verdict, recorded against its commits.
	if pullRequest.Reviews != 2 || pullRequest.Approved {
		t.Fatalf("expected reviews caught up without approval, got %#v", pullRequest)
	}
	if pullRequest.ReviewedHead != "new-head" || pullRequest.ReviewedBase != "new-base" {
		t.Fatalf("expected the verdict pinned to its commits, got %#v", pullRequest)
	}
	if err := pullRequest.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPullRequest_RecordReview_ShouldOnlyFlagTheLimitOnRequestChanges(t *testing.T) {
	// Arrange: out of coding rounds either way; only the verdict differs.
	for _, approved := range []bool{true, false} {
		pullRequest := reviewedRecord(t, false)
		pullRequest.Rounds = MaxCodingRounds
		pullRequest.CodingRounds = MaxCodingRounds
		pullRequest.Reviews = MaxCodingRounds - 1

		// Act
		if err := pullRequest.RecordReview(approved, "head", "base"); err != nil {
			t.Fatal(err)
		}

		// Assert: an approval ends the loop, so it never needs the flag.
		if pullRequest.HasFlag(FlagRoundLimit) == approved {
			t.Fatalf("expected round-limit flag only without approval, got %#v", pullRequest)
		}
	}
}

func TestPullRequest_CanCode_ShouldRationOnlyCodingRounds(t *testing.T) {
	tests := []struct {
		name    string
		coding  int
		granted int
		want    bool
	}{
		{name: "fresh", coding: 0, want: true},
		{name: "at the limit", coding: MaxCodingRounds, want: false},
		{name: "a granted round restarts it", coding: MaxCodingRounds, granted: 1, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange: Rounds far past the limit — only coding rounds count.
			pullRequest := PullRequest{
				Rounds:        test.coding + 10,
				CodingRounds:  test.coding,
				RoundsGranted: test.granted,
			}

			// Act & Assert
			if pullRequest.CanCode() != test.want {
				t.Fatalf("expected CanCode %t for %#v", test.want, pullRequest)
			}
		})
	}
}

func TestPullRequest_TransitionTo_ShouldRejectTerminalTransitions(t *testing.T) {
	// Arrange
	for _, initial := range []PullRequestStatus{PullRequestMerged, PullRequestClosed} {
		pullRequest := PullRequest{Status: initial}

		// Act
		err := pullRequest.TransitionTo(PullRequestOpen)

		// Assert
		if err == nil {
			t.Fatalf("expected %q to reject reopening", initial)
		}
	}
}
