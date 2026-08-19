package epic

import "testing"

func TestEpicOwnsIssues(t *testing.T) {
	value, err := New("Migration")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	issue, err := NewIssue("Move the model")
	if err != nil {
		t.Fatalf("NewIssue() error = %v", err)
	}
	if err := value.AddIssue(issue); err != nil {
		t.Fatalf("AddIssue() error = %v", err)
	}
	if value.Issues[0].EpicID != value.ID {
		t.Fatalf("issue epic id = %q, want %q", value.Issues[0].EpicID, value.ID)
	}
	if err := value.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := value.AddIssue(issue); err == nil {
		t.Fatal("AddIssue() accepted an issue after close")
	}
}
