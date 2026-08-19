package epic

import (
	"reflect"
	"testing"
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
