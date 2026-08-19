package organisation

import "testing"

func TestNew(t *testing.T) {
	got, err := New("tinker-works", "Tinker Works")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got.Login != "tinker-works" || got.Name != "Tinker Works" {
		t.Fatalf("New() = %#v", got)
	}
}

func TestValidate(t *testing.T) {
	if err := (Organisation{}).Validate(); err == nil {
		t.Fatal("Validate() succeeded for an empty organisation")
	}
}
