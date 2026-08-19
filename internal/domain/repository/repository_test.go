package repository

import "testing"

func TestNew(t *testing.T) {
	got, err := New("tracker", "https://github.com/tinker-works/tracker", "tinker-works/tracker", "main")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got.Name.String() != "tracker" || got.DefaultBranch != "main" {
		t.Fatalf("New() = %#v", got)
	}
}

func TestValidateRejectsInvalidRemote(t *testing.T) {
	value, err := New("tracker", "not a URL")
	if err == nil || value.Valid() {
		t.Fatalf("New() = %#v, error = %v", value, err)
	}
}
