package epic

import (
	"reflect"
	"testing"
	"time"
)

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

func TestEpicTransitionRejectsInvalidRestoredIssue(t *testing.T) {
	value, err := NewEpic("Migration")
	if err != nil {
		t.Fatalf("NewEpic() error = %v", err)
	}
	issue, err := NewIssue("Move the model")
	if err != nil {
		t.Fatalf("NewIssue() error = %v", err)
	}
	if err := value.AddIssue(issue); err != nil {
		t.Fatalf("AddIssue() error = %v", err)
	}
	value.Issues[0].Title = ""
	want := value

	if err := value.Transition(StatusInProgress); err == nil {
		t.Fatal("Transition() accepted an epic with an invalid restored issue")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("Transition() mutated the epic on error: got %#v, want %#v", value, want)
	}
}

func TestEpicTransitionRejectsInvalidTimestamps(t *testing.T) {
	tests := []struct {
		name  string
		value func(t *testing.T) Epic
	}{
		{
			name: "future creation",
			value: func(t *testing.T) Epic {
				t.Helper()
				value, err := NewEpic("Migration")
				if err != nil {
					t.Fatalf("NewEpic() error = %v", err)
				}
				value.CreatedAt = time.Now().Add(time.Hour)
				return value
			},
		},
		{
			name: "restored closed time",
			value: func(t *testing.T) Epic {
				t.Helper()
				value, err := NewEpic("Migration")
				if err != nil {
					t.Fatalf("NewEpic() error = %v", err)
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
				t.Fatalf("Close() mutated the epic on error: got %#v, want %#v", value, want)
			}
		})
	}
}

func TestEpicAddIssueRejectsInvalidRestoredIssue(t *testing.T) {
	value, err := NewEpic("Migration")
	if err != nil {
		t.Fatalf("NewEpic() error = %v", err)
	}
	issue, err := NewIssue("Move the model")
	if err != nil {
		t.Fatalf("NewIssue() error = %v", err)
	}
	if err := value.AddIssue(issue); err != nil {
		t.Fatalf("AddIssue() error = %v", err)
	}
	value.Issues[0].Title = ""
	additional, err := NewIssue("Validate the model")
	if err != nil {
		t.Fatalf("NewIssue() error = %v", err)
	}
	want := value

	if err := value.AddIssue(additional); err == nil {
		t.Fatal("AddIssue() accepted an epic with an invalid restored issue")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("AddIssue() mutated the epic on error: got %#v, want %#v", value, want)
	}
}

func TestEpicSetPrefixRejectsInvalidRestoredIssue(t *testing.T) {
	value, err := NewEpic("Migration")
	if err != nil {
		t.Fatalf("NewEpic() error = %v", err)
	}
	issue, err := NewIssue("Move the model")
	if err != nil {
		t.Fatalf("NewIssue() error = %v", err)
	}
	if err := value.AddIssue(issue); err != nil {
		t.Fatalf("AddIssue() error = %v", err)
	}
	value.Issues[0].Title = ""
	want := value

	if err := value.SetPrefix("MIG"); err == nil {
		t.Fatal("SetPrefix() accepted an epic with an invalid restored issue")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("SetPrefix() mutated the epic on error: got %#v, want %#v", value, want)
	}
}

func TestEpicRemoveIssueRejectsInvalidRestoredIssue(t *testing.T) {
	value, err := NewEpic("Migration")
	if err != nil {
		t.Fatalf("NewEpic() error = %v", err)
	}
	first, err := NewIssue("Move the model")
	if err != nil {
		t.Fatalf("NewIssue() error = %v", err)
	}
	second, err := NewIssue("Validate the model")
	if err != nil {
		t.Fatalf("NewIssue() error = %v", err)
	}
	if err := value.AddIssue(first); err != nil {
		t.Fatalf("AddIssue() error = %v", err)
	}
	if err := value.AddIssue(second); err != nil {
		t.Fatalf("AddIssue() error = %v", err)
	}
	value.Issues[1].Title = ""
	want := value

	removed, err := value.RemoveIssue(first.ID)
	if err == nil || removed {
		t.Fatal("RemoveIssue() accepted an epic with an invalid restored issue")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("RemoveIssue() mutated the epic on error: got %#v, want %#v", value, want)
	}
}
