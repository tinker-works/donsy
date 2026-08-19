package setup

import "testing"

func TestNew(t *testing.T) {
	got, err := New("default", "A default setup", "Run the checks", "main", "main")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got.Name.String() != "default" || got.BaseBranch != "main" {
		t.Fatalf("New() = %#v", got)
	}
}
