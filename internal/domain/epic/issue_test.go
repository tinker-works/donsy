package epic

import "testing"

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
