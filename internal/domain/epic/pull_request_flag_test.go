package epic

import "testing"

func TestPullRequestFlags_ShouldAddRemoveAndReport(t *testing.T) {
	// Arrange
	pullRequest := PullRequest{ID: "pr-1"}

	// Act
	if err := pullRequest.AddFlag(FlagBlocked); err != nil {
		t.Fatal(err)
	}
	if err := pullRequest.AddFlag(FlagBlocked); err != nil {
		t.Fatal(err)
	}

	// Assert: adding twice is not an error and does not duplicate.
	if !pullRequest.HasFlag(FlagBlocked) || len(pullRequest.Flags) != 1 {
		t.Fatalf("unexpected flags: %v", pullRequest.Flags)
	}

	pullRequest.RemoveFlag(FlagBlocked)
	if pullRequest.HasFlag(FlagBlocked) || len(pullRequest.Flags) != 0 {
		t.Fatalf("expected the flag to be gone, got %v", pullRequest.Flags)
	}
}

func TestPullRequest_AddFlag_ShouldRejectAnUnknownFlag(t *testing.T) {
	// Arrange
	pullRequest := PullRequest{ID: "pr-1"}

	// Act
	err := pullRequest.AddFlag(PullRequestFlag("nonsense"))

	// Assert
	if err == nil {
		t.Fatal("expected an unknown pull request flag to be rejected")
	}
}

func TestPullRequest_SetFlag_ShouldMatchThePresentArgument(t *testing.T) {
	// Arrange: the sweep recomputes derived flags rather than branching.
	pullRequest := PullRequest{ID: "pr-1"}

	// Act
	if err := pullRequest.SetFlag(FlagBlocked, true); err != nil {
		t.Fatal(err)
	}
	blockedAfterSet := pullRequest.HasFlag(FlagBlocked)
	if err := pullRequest.SetFlag(FlagBlocked, false); err != nil {
		t.Fatal(err)
	}

	// Assert
	if !blockedAfterSet || pullRequest.HasFlag(FlagBlocked) {
		t.Fatalf("expected the flag to follow present, got %v", pullRequest.Flags)
	}
}

func TestPullRequest_ReviewIsStale_ShouldTrackTheReviewedCommits(t *testing.T) {
	// Arrange
	pullRequest := PullRequest{ID: "pr-1", ReviewedHead: "aaa", ReviewedBase: "bbb"}

	// Act & Assert
	if pullRequest.ReviewIsStale("aaa", "bbb") {
		t.Fatal("expected the recorded verdict to cover its own commits")
	}
	if !pullRequest.ReviewIsStale("ccc", "bbb") {
		t.Fatal("expected a moved head to invalidate the verdict")
	}
	if !(PullRequest{ID: "pr-2"}).ReviewIsStale("aaa", "bbb") {
		t.Fatal("expected an unreviewed pull request to be stale")
	}
}

func TestPullRequest_Validate_ShouldRejectAnInvalidFlag(t *testing.T) {
	// Arrange: flags arrive from YAML, so validation is the only guard.
	pullRequest := PullRequest{
		ID: "pr-1", IssueID: "issue-1", Title: "Add widget", Status: PullRequestOpen,
		Flags: []PullRequestFlag{"nonsense"},
	}

	// Act
	err := pullRequest.Validate()

	// Assert
	if err == nil {
		t.Fatal("expected an invalid flag to be rejected")
	}
}
