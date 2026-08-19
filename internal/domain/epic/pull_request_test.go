package epic

import "testing"

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
