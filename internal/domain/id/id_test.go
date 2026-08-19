package id

import (
	"encoding/json"
	"testing"
)

func TestParse(t *testing.T) {
	got, err := Parse("project-1")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.String() != "project-1" {
		t.Fatalf("Parse() = %q", got)
	}

	for _, value := range []string{"", " ", "project/name", "project\\name"} {
		if _, err := Parse(value); err == nil {
			t.Errorf("Parse(%q) succeeded", value)
		}
	}
}

func TestNew(t *testing.T) {
	first, second := New(), New()
	if first.IsZero() || second.IsZero() || first == second {
		t.Fatalf("New() = %q, %q", first, second)
	}
}

func TestJSON(t *testing.T) {
	original := MustParse("issue-1")
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var decoded ID
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded != original {
		t.Fatalf("Unmarshal() = %q, want %q", decoded, original)
	}
}
