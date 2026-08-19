package epic

import (
	"reflect"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain/id"
	"github.com/tinker-works/donsy/internal/domain/owner"
)

func TestPullRequestLifecycle(t *testing.T) {
	value, err := NewPullRequest("Improve validation")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	if err := value.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := value.Merge(); err == nil {
		t.Fatal("Merge() reopened a closed pull request")
	}
	if err := value.Reset(); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if value.Status != StatusOpen {
		t.Fatalf("Reset() status = %q", value.Status)
	}
}

func TestPullRequestTransitionRejectsInvalidRestoredComment(t *testing.T) {
	value, err := NewPullRequest("Improve validation")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	comment, err := NewComment("alice", "review")
	if err != nil {
		t.Fatalf("NewComment() error = %v", err)
	}
	if err := value.AddComment(comment); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	value.Comments[0].Body = ""
	want := value

	if err := value.Transition(StatusApproved); err == nil {
		t.Fatal("Transition() accepted a pull request with an invalid restored comment")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("Transition() mutated the pull request on error: got %#v, want %#v", value, want)
	}
}

func TestPullRequestTransitionRejectsInvalidTimestamps(t *testing.T) {
	tests := []struct {
		name  string
		to    Status
		value func(t *testing.T) PullRequest
	}{
		{
			name: "future creation when closing",
			to:   StatusClosed,
			value: func(t *testing.T) PullRequest {
				t.Helper()
				value, err := NewPullRequest("Improve validation")
				if err != nil {
					t.Fatalf("NewPullRequest() error = %v", err)
				}
				value.CreatedAt = time.Now().Add(time.Hour)
				return value
			},
		},
		{
			name: "restored merged time",
			to:   StatusMerged,
			value: func(t *testing.T) PullRequest {
				t.Helper()
				value, err := NewPullRequest("Improve validation")
				if err != nil {
					t.Fatalf("NewPullRequest() error = %v", err)
				}
				value.MergedAt = value.CreatedAt.Add(-time.Second)
				return value
			},
		},
		{
			name: "restored closed time",
			to:   StatusClosed,
			value: func(t *testing.T) PullRequest {
				t.Helper()
				value, err := NewPullRequest("Improve validation")
				if err != nil {
					t.Fatalf("NewPullRequest() error = %v", err)
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

			if err := value.Transition(test.to); err == nil {
				t.Fatal("Transition() accepted invalid lifecycle timestamps")
			}
			if !reflect.DeepEqual(value, want) {
				t.Fatalf("Transition() mutated the pull request on error: got %#v, want %#v", value, want)
			}
		})
	}
}

func TestPullRequestTransitionRejectsInvalidRestoredReviewer(t *testing.T) {
	value, err := NewPullRequest("Improve validation")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	value.Reviewers = []owner.Owner{{ID: id.New()}}
	want := value

	if err := value.Transition(StatusApproved); err == nil {
		t.Fatal("Transition() accepted a pull request with an invalid restored reviewer")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("Transition() mutated the pull request on error: got %#v, want %#v", value, want)
	}
}

func TestPullRequestAddCommentRejectsInvalidRestoredComment(t *testing.T) {
	value, err := NewPullRequest("Improve validation")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
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
		t.Fatal("AddComment() accepted a pull request with an invalid restored comment")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("AddComment() mutated the pull request on error: got %#v, want %#v", value, want)
	}
}

func TestPullRequestResetRejectsInvalidRestoredComment(t *testing.T) {
	value, err := NewPullRequest("Improve validation")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	comment, err := NewComment("alice", "review")
	if err != nil {
		t.Fatalf("NewComment() error = %v", err)
	}
	if err := value.AddComment(comment); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	value.Comments[0].Body = ""
	value.Status = StatusClosed
	want := value

	if err := value.Reset(); err == nil {
		t.Fatal("Reset() accepted a pull request with an invalid restored comment")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("Reset() mutated the pull request on error: got %#v, want %#v", value, want)
	}
}

func TestPullRequestResetRejectsInvalidRestoredReviewer(t *testing.T) {
	value, err := NewPullRequest("Improve validation")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	value.Reviewers = []owner.Owner{{ID: id.New()}}
	value.Status = StatusClosed
	want := value

	if err := value.Reset(); err == nil {
		t.Fatal("Reset() accepted a pull request with an invalid restored reviewer")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("Reset() mutated the pull request on error: got %#v, want %#v", value, want)
	}
}

func TestPullRequestResetRejectsInvalidRestoredStatus(t *testing.T) {
	value, err := NewPullRequest("Improve validation")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	value.Status = Status("invalid")
	want := value

	if err := value.Reset(); err == nil {
		t.Fatal("Reset() accepted a pull request with an invalid restored status")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("Reset() mutated the pull request on error: got %#v, want %#v", value, want)
	}
}

func TestPullRequestGrantRejectsInvalidRestoredComment(t *testing.T) {
	value, err := NewPullRequest("Improve validation")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	comment, err := NewComment("alice", "review")
	if err != nil {
		t.Fatalf("NewComment() error = %v", err)
	}
	if err := value.AddComment(comment); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	value.Comments[0].Body = ""
	reviewer, err := owner.New("bob")
	if err != nil {
		t.Fatalf("owner.New() error = %v", err)
	}
	want := value

	if err := value.Grant(reviewer); err == nil {
		t.Fatal("Grant() accepted a pull request with an invalid restored comment")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("Grant() mutated the pull request on error: got %#v, want %#v", value, want)
	}
}

func TestPullRequestGrantDuplicateRejectsInvalidRestoredComment(t *testing.T) {
	value, err := NewPullRequest("Improve validation")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	reviewer, err := owner.New("bob")
	if err != nil {
		t.Fatalf("owner.New() error = %v", err)
	}
	if err := value.Grant(reviewer); err != nil {
		t.Fatalf("Grant() error = %v", err)
	}
	comment, err := NewComment("alice", "review")
	if err != nil {
		t.Fatalf("NewComment() error = %v", err)
	}
	if err := value.AddComment(comment); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	value.Comments[0].Body = ""
	want := value

	if err := value.Grant(reviewer); err == nil {
		t.Fatal("Grant() accepted a pull request with an invalid restored comment")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("Grant() mutated the pull request on error: got %#v, want %#v", value, want)
	}
}

func TestPullRequestGrantRejectsInvalidRestoredReviewer(t *testing.T) {
	value, err := NewPullRequest("Improve validation")
	if err != nil {
		t.Fatalf("NewPullRequest() error = %v", err)
	}
	value.Reviewers = []owner.Owner{{ID: id.New()}}
	reviewer, err := owner.New("bob")
	if err != nil {
		t.Fatalf("owner.New() error = %v", err)
	}
	want := value

	if err := value.Grant(reviewer); err == nil {
		t.Fatal("Grant() accepted a pull request with an invalid restored reviewer")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("Grant() mutated the pull request on error: got %#v, want %#v", value, want)
	}
}
