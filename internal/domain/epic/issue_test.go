package epic

import (
	"reflect"
	"testing"
	"time"
)

func TestIssueTransitions(t *testing.T) {
	value, err := NewIssue("Write tests")
	if err != nil {
		t.Fatalf("NewIssue() error = %v", err)
	}
	if err := value.Transition(StatusInProgress); err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if err := value.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := value.Transition(StatusOpen); err == nil {
		t.Fatal("Transition() reopened a closed issue")
	}
}

func TestIssueTransitionRejectsInvalidRestoredPullRequest(t *testing.T) {
	value, err := NewIssue("Write tests")
	if err != nil {
		t.Fatalf("NewIssue() error = %v", err)
	}
	request, err := NewPullRequest("Review changes")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	if err := value.AddPullRequest(request); err != nil {
		t.Fatalf("AddPullRequest() error = %v", err)
	}
	value.PullRequests[0].Title = ""
	want := value

	if err := value.Transition(StatusInProgress); err == nil {
		t.Fatal("Transition() accepted an issue with an invalid restored pull request")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("Transition() mutated the issue on error: got %#v, want %#v", value, want)
	}
}

func TestIssueTransitionRejectsInvalidTimestamps(t *testing.T) {
	tests := []struct {
		name  string
		value func(t *testing.T) Issue
	}{
		{
			name: "future creation",
			value: func(t *testing.T) Issue {
				t.Helper()
				value, err := NewIssue("Write tests")
				if err != nil {
					t.Fatalf("NewIssue() error = %v", err)
				}
				value.CreatedAt = time.Now().Add(time.Hour)
				return value
			},
		},
		{
			name: "restored closed time",
			value: func(t *testing.T) Issue {
				t.Helper()
				value, err := NewIssue("Write tests")
				if err != nil {
					t.Fatalf("NewIssue() error = %v", err)
				}
				value.ClosedAt = value.CreatedAt.Add(-time.Second)
				return value
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := test.value(t)
			want := value

			if err := value.Close(); err == nil {
				t.Fatal("Close() accepted invalid lifecycle timestamps")
			}
			if !reflect.DeepEqual(value, want) {
				t.Fatalf("Close() mutated the issue on error: got %#v, want %#v", value, want)
			}
		})
	}
}

func TestIssueAddPullRequestRejectsInvalidRestoredPullRequest(t *testing.T) {
	value, err := NewIssue("Write tests")
	if err != nil {
		t.Fatalf("NewIssue() error = %v", err)
	}
	request, err := NewPullRequest("Review changes")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	if err := value.AddPullRequest(request); err != nil {
		t.Fatalf("AddPullRequest() error = %v", err)
	}
	value.PullRequests[0].Title = ""
	additional, err := NewPullRequest("Update review")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	want := value

	if err := value.AddPullRequest(additional); err == nil {
		t.Fatal("AddPullRequest() accepted an issue with an invalid restored pull request")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("AddPullRequest() mutated the issue on error: got %#v, want %#v", value, want)
	}
}

func TestIssueAddCommentRejectsInvalidRestoredComment(t *testing.T) {
	value, err := NewIssue("Write tests")
	if err != nil {
		t.Fatalf("NewIssue() error = %v", err)
	}
	comment, err := NewComment("alice", "review")
	if err != nil {
		t.Fatalf("NewComment() error = %v", err)
	}
	if err := value.AddComment(comment); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	value.Comments[0].Body = ""
	additional, err := NewComment("bob", "follow-up")
	if err != nil {
		t.Fatalf("NewComment() error = %v", err)
	}
	want := value

	if err := value.AddComment(additional); err == nil {
		t.Fatal("AddComment() accepted an issue with an invalid restored comment")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("AddComment() mutated the issue on error: got %#v, want %#v", value, want)
	}
}
