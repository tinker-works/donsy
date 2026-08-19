package epic

import (
	"testing"
	"time"
)

func TestCreateEpicRejectsMissingRequiredFields(t *testing.T) {
	// Arrange
	tests := []struct {
		name     string
		title    string
		assignee string
	}{
		{name: "title", title: "", assignee: "owner"},
		{name: "assignee", title: "Epic", assignee: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			if _, err := CreateEpic(tt.title, tt.assignee, ""); err == nil {
				// Assert
				t.Fatal("expected missing field to fail")
			}
		})
	}
}

func TestEpicAddIssueSetsParent(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	issue, err := CreateRepositoryIssue("Child", "body", "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}

	// Act
	if err := epic.AddIssue(epic.Issues[0].ID, issue); err != nil {
		t.Fatal(err)
	}
	// Assert
	if got := epic.Issues[1].ParentID; got != epic.Issues[0].ID {
		t.Fatalf("expected parent %q, got %q", epic.Issues[0].ID, got)
	}
}

func TestEpicAddIssueRejectsMissingParent(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	issue, err := CreateRepositoryIssue("Child", "body", "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}

	// Act
	if err := epic.AddIssue("missing", issue); err == nil {
		// Assert
		t.Fatal("expected missing parent to fail")
	}
}

func TestEpicCloseIssueTraversesDescendants(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	child, err := CreateRepositoryIssue("Child", "child body", "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	if err := epic.AddIssue(epic.Issues[0].ID, child); err != nil {
		t.Fatal(err)
	}
	grandchild, err := CreateRepositoryIssue("Grandchild", "grandchild body", "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	if err := epic.AddIssue(child.ID, grandchild); err != nil {
		t.Fatal(err)
	}

	// Act
	if err := epic.CloseIssue(epic.Issues[0].ID); err != nil {
		t.Fatal(err)
	}
	// Assert
	for _, issue := range epic.Issues {
		if issue.State != IssueStateClosed {
			t.Fatalf("issue %q was not closed: %s", issue.ID, issue.State)
		}
	}
}

func TestEpicAddPullRequest_ShouldMoveIssueToCoding(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	pullRequest, err := CreatePullRequest("wrong", "PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}

	// Act
	if err := epic.AddPullRequest(epic.Issues[0].ID, pullRequest); err != nil {
		t.Fatal(err)
	}

	// Assert
	if epic.Issues[0].State != IssueStateCoding {
		t.Fatalf("expected issue state %q, got %q", IssueStateCoding, epic.Issues[0].State)
	}
	if epic.PullRequests[0].IssueID != epic.Issues[0].ID {
		t.Fatalf("pull request was attached to %q", epic.PullRequests[0].IssueID)
	}
}

func TestEpicTransitionPullRequest_ShouldMergeIssueWhenPRMerges(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	pullRequest, err := CreatePullRequest(epic.Issues[0].ID, "PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	if err := epic.AddPullRequest(epic.Issues[0].ID, pullRequest); err != nil {
		t.Fatal(err)
	}
	// Only an approved issue can be merged, so walk it through the loop's
	// phases first rather than merging one nobody reviewed.
	approve(t, &epic, epic.Issues[0].ID)

	// Act
	if err := epic.TransitionPullRequest(pullRequest.ID, PullRequestMerged); err != nil {
		t.Fatal(err)
	}

	// Assert
	if epic.PullRequests[0].Status != PullRequestMerged || epic.Issues[0].State != IssueStateMerged {
		t.Fatalf("unexpected merged state: %#v", epic)
	}
}

// approve walks an issue from Coding to Pr, the way a coding round followed by
// an approving review does.
func approve(t *testing.T, target *Epic, issueID string) {
	t.Helper()
	for _, state := range []IssueState{IssueStateReview, IssueStatePR} {
		if err := target.TransitionIssue(issueID, state); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEpicTransitionPullRequest_ShouldReopenIssueWhenPRCloses(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	pullRequest, err := CreatePullRequest(epic.Issues[0].ID, "PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	if err := epic.AddPullRequest(epic.Issues[0].ID, pullRequest); err != nil {
		t.Fatal(err)
	}

	// Act
	if err := epic.TransitionPullRequest(pullRequest.ID, PullRequestClosed); err != nil {
		t.Fatal(err)
	}

	// Assert
	// The work still has to happen, and only an Open issue gets a fresh branch.
	if epic.PullRequests[0].Status != PullRequestClosed ||
		epic.Issues[0].State != IssueStateOpen {
		t.Fatalf("unexpected closed state: %#v", epic)
	}
}

func TestEpicCloseIssue_ShouldCloseRelatedPullRequests(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	pullRequest, err := CreatePullRequest(epic.Issues[0].ID, "PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	if err := epic.AddPullRequest(epic.Issues[0].ID, pullRequest); err != nil {
		t.Fatal(err)
	}

	// Act
	if err := epic.CloseIssue(epic.Issues[0].ID); err != nil {
		t.Fatal(err)
	}

	// Assert
	if epic.Issues[0].State != IssueStateClosed || epic.PullRequests[0].Status != PullRequestClosed {
		t.Fatalf("unexpected closed aggregate: %#v", epic)
	}
}

func TestEpicIssueOperationsRejectMissingIssue(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	for _, operation := range []struct {
		name string
		call func() error
	}{
		{name: "close", call: func() error { return epic.CloseIssue("missing") }},
		{name: "comment", call: func() error {
			comment := Comment{ID: "comment", Author: "owner", CreatedAt: time.Now().UTC()}
			return epic.AddIssueComment("missing", comment)
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			// Act
			if err := operation.call(); err == nil {
				// Assert
				t.Fatal("expected missing issue to fail")
			}
		})
	}
}

func TestEpicFindIssue(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	issueID := epic.Issues[0].ID

	// Act
	issue, err := epic.FindIssue(issueID)
	if err != nil {
		t.Fatal(err)
	}
	// Assert
	if issue.ID != issueID {
		t.Fatalf("issue lookup failed: %#v", issue)
	}
	if _, err := epic.FindIssue("missing"); err == nil {
		t.Fatal("expected missing issue lookup to fail")
	}
}

func TestEpicAddPullRequest(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	pullRequest, err := CreatePullRequest("wrong", "PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}

	// Act
	if err := epic.AddPullRequest(epic.Issues[0].ID, pullRequest); err != nil {
		t.Fatal(err)
	}
	// Assert
	if len(epic.PullRequests) != 1 || epic.PullRequests[0].IssueID != epic.Issues[0].ID {
		t.Fatalf("unexpected pull request: %#v", epic.PullRequests)
	}
	if err := epic.AddPullRequest("missing", pullRequest); err == nil {
		t.Fatal("expected missing issue to fail")
	}
}

func TestEpicTransitionPullRequestRejectsNonOutcomes(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	pullRequest, err := CreatePullRequest(epic.Issues[0].ID, "PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	if err := epic.AddPullRequest(epic.Issues[0].ID, pullRequest); err != nil {
		t.Fatal(err)
	}
	issueState := epic.Issues[0].State

	// Act
	err = epic.TransitionPullRequest(pullRequest.ID, PullRequestOpen)

	// Assert
	if err == nil {
		t.Fatal("expected transition to the current status to fail")
	}
	if epic.Issues[0].State != issueState {
		t.Fatalf("issue state changed on a rejected transition: %q", epic.Issues[0].State)
	}
	if epic.PullRequests[0].Status != PullRequestOpen {
		t.Fatalf("pull request status changed on a rejected transition: %q", epic.PullRequests[0].Status)
	}
}

func TestEpicAddCommentsToOwningEntities(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	pullRequest, err := CreatePullRequest(epic.Issues[0].ID, "PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	epic.PullRequests = append(epic.PullRequests, pullRequest)
	issueComment, err := CreateComment("owner", "issue comment")
	if err != nil {
		t.Fatal(err)
	}
	prComment, err := CreateComment("owner", "pr comment")
	if err != nil {
		t.Fatal(err)
	}

	// Act
	if err := epic.AddIssueComment(epic.Issues[0].ID, issueComment); err != nil {
		t.Fatal(err)
	}
	if err := epic.AddPullRequestComment(pullRequest.ID, prComment); err != nil {
		t.Fatal(err)
	}
	// Assert
	if len(epic.Issues[0].Comments) != 1 || len(epic.PullRequests[0].Comments) != 1 {
		t.Fatalf("comments were not attached to owners: %#v", epic)
	}
	if err := epic.AddPullRequestComment("missing", prComment); err == nil {
		t.Fatal("expected missing pull request to fail")
	}
}

func TestEpicRootIssue(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)

	// Act
	root, err := epic.RootIssue()
	if err != nil {
		t.Fatal(err)
	}
	// Assert
	if root.ParentID != "" {
		t.Fatalf("unexpected root parent: %q", root.ParentID)
	}

	missing := Epic{Issues: []Issue{{ID: "child", ParentID: "root"}}}
	if _, err := missing.RootIssue(); err == nil {
		t.Fatal("expected missing root to fail")
	}
}

func TestEpicValidateAcceptsValidAggregateAndRejectsInvalidPullRequest(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)

	// Act
	if err := epic.Validate(); err != nil {
		t.Fatal(err)
	}
	epic.PullRequests = append(epic.PullRequests, PullRequest{
		ID: "pr", IssueID: "missing", Title: "PR", Status: PullRequestOpen,
	})

	// Assert
	if err := epic.Validate(); err == nil {
		t.Fatal("expected pull request with missing issue to fail")
	}
}

func TestEpicSortOrdersIssuesAndPullRequests(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	first := epic.Issues[0]
	first.CreatedAt = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	second, err := CreateIssue("Second", "body")
	if err != nil {
		t.Fatal(err)
	}
	second.CreatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	epic.Issues = []Issue{first, second}
	firstPR, err := CreatePullRequest(first.ID, "First PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	firstPR.CreatedAt = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	secondPR, err := CreatePullRequest(first.ID, "Second PR", "repo", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	secondPR.CreatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	epic.PullRequests = []PullRequest{firstPR, secondPR}

	// Act
	epic.Sort()

	// Assert
	if epic.Issues[0].ID != second.ID || epic.PullRequests[0].ID != secondPR.ID {
		t.Fatalf("aggregate was not sorted: %#v", epic)
	}
}

func TestEpic_AbandonedBranches_ShouldListOnlyClosedPullRequestHeads(t *testing.T) {
	// Arrange: one pull request per outcome. Only the closed one was abandoned —
	// the open one is still being worked and the merged one's commits landed.
	epic := newTestEpic(t)
	issueID := epic.Issues[0].ID
	epic.PullRequests = []PullRequest{
		{ID: "pr-open", IssueID: issueID, Title: "Open", Status: PullRequestOpen,
			Repository: "acme/widgets", Head: "feature/open"},
		{ID: "pr-closed", IssueID: issueID, Title: "Closed", Status: PullRequestClosed,
			Repository: "acme/widgets", Head: "feature/closed"},
		{ID: "pr-merged", IssueID: issueID, Title: "Merged", Status: PullRequestMerged,
			Repository: "acme/widgets", Head: "feature/merged"},
	}

	// Act
	branches := epic.AbandonedBranches()

	// Assert
	want := Branch{
		PullRequestID: "pr-closed", IssueID: issueID,
		Repository: "acme/widgets", Name: "feature/closed",
	}
	if len(branches) != 1 || branches[0] != want {
		t.Fatalf("unexpected branches: %#v", branches)
	}
}

func TestEpic_AbandonedBranches_ShouldListBranchesClosedByCloseIssue(t *testing.T) {
	// Arrange: closing an issue is what closes its open pull requests, so the
	// branches CloseIssue abandons must show up for the caller to delete.
	epic := newTestEpic(t)
	pullRequest, err := CreatePullRequest(epic.Issues[0].ID, "PR", "acme/widgets", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	if err := epic.AddPullRequest(epic.Issues[0].ID, pullRequest); err != nil {
		t.Fatal(err)
	}
	if err := epic.CloseIssue(epic.Issues[0].ID); err != nil {
		t.Fatal(err)
	}

	// Act
	branches := epic.AbandonedBranches()

	// Assert
	if len(branches) != 1 || branches[0].Name != "head" ||
		branches[0].PullRequestID != pullRequest.ID {
		t.Fatalf("unexpected branches: %#v", branches)
	}
}

func TestEpic_SetTitleAndSetBody_ShouldUpdateAndTrimTitle(t *testing.T) {
	// Arrange: a refine round renames the epic to what it actually planned.
	epic := newTestEpic(t)

	// Act
	if err := epic.SetTitle("  Extract the cart total  "); err != nil {
		t.Fatal(err)
	}
	if err := epic.SetBody("The refined brief."); err != nil {
		t.Fatal(err)
	}

	// Assert
	if epic.Title != "Extract the cart total" || epic.Body != "The refined brief." {
		t.Fatalf("unexpected title and body: %q, %q", epic.Title, epic.Body)
	}
}

func TestEpic_SetTitle_ShouldRejectEmptyTitle(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)

	// Act
	err := epic.SetTitle("   ")

	// Assert
	if err == nil {
		t.Fatal("expected a blank title to be rejected")
	}
	if epic.Title != "Epic" {
		t.Fatalf("rejected title changed the epic: %q", epic.Title)
	}
}

func TestEpic_SetTitleAndSetBody_ShouldRejectClosedEpic(t *testing.T) {
	// Arrange: a closed epic keeps the name and brief its history was recorded
	// under.
	epic := newTestEpic(t)
	if err := epic.ForceState(EpicStateClosed); err != nil {
		t.Fatal(err)
	}

	// Act
	titleErr := epic.SetTitle("Renamed")
	bodyErr := epic.SetBody("rewritten")

	// Assert
	if titleErr == nil || bodyErr == nil {
		t.Fatalf("expected closed epic edits to be rejected: %v, %v", titleErr, bodyErr)
	}
	if epic.Title != "Epic" || epic.Body != "body" {
		t.Fatalf("rejected edit changed the epic: %q, %q", epic.Title, epic.Body)
	}
}

func newTestEpic(t *testing.T) Epic {
	t.Helper()
	epic, err := CreateEpic("Epic", "owner", "body")
	if err != nil {
		t.Fatal(err)
	}
	return epic
}
