package owner

import "testing"

func TestNew(t *testing.T) {
	got, err := New("alice", "Alice")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got.Login != "alice" || got.Name != "Alice" {
		t.Fatalf("New() = %#v", got)
	}
}
