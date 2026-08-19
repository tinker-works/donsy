package epic

import "testing"

func TestIssue_SetTitle_ShouldUpdateTitle(t *testing.T) {
	// Arrange
	issue, err := CreateIssue("Original", "body")
	if err != nil {
		t.Fatal(err)
	}

	// Act
	if err := issue.SetTitle("Updated"); err != nil {
		t.Fatal(err)
	}

	// Assert
	if issue.Title != "Updated" {
		t.Fatalf("unexpected title: %q", issue.Title)
	}
}

func TestIssue_SetBody_ShouldUpdateBody(t *testing.T) {
	// Arrange
	issue, err := CreateIssue("Title", "original")
	if err != nil {
		t.Fatal(err)
	}

	// Act
	if err := issue.SetBody("updated"); err != nil {
		t.Fatal(err)
	}

	// Assert
	if issue.Body != "updated" {
		t.Fatalf("unexpected body: %q", issue.Body)
	}
}

func TestIssue_SetTitleAndSetBody_ShouldRejectClosedIssue(t *testing.T) {
	// Arrange: a closed issue is abandoned work, so nothing may rewrite what it
	// said when it was abandoned.
	issue, err := CreateIssue("Title", "body")
	if err != nil {
		t.Fatal(err)
	}
	if err := issue.TransitionTo(IssueStateClosed); err != nil {
		t.Fatal(err)
	}

	// Act
	titleErr := issue.SetTitle("Updated")
	bodyErr := issue.SetBody("updated")

	// Assert
	if titleErr == nil || bodyErr == nil {
		t.Fatalf("expected closed issue edits to be rejected: %v, %v", titleErr, bodyErr)
	}
	if issue.Title != "Title" || issue.Body != "body" {
		t.Fatalf("rejected edit changed the issue: %#v", issue)
	}
}
