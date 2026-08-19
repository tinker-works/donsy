package project

import (
	"testing"

	"github.com/tinker-works/donsy/internal/domain/repository"
)

func TestAddRepository(t *testing.T) {
	value, err := New("tracker")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	item, err := repository.New("origin", "https://github.com/tinker-works/tracker")
	if err != nil {
		t.Fatalf("repository.New() error = %v", err)
	}
	if err := value.AddRepository(item); err != nil {
		t.Fatalf("AddRepository() error = %v", err)
	}
	if err := value.AddRepository(item); err == nil {
		t.Fatal("AddRepository() accepted a duplicate")
	}
	if !value.RemoveRepository(item.Name) || value.RemoveRepository(item.Name) {
		t.Fatal("RemoveRepository() returned an unexpected result")
	}
}
