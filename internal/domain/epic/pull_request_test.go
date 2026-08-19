package epic

import (
	"reflect"
	"testing"
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
