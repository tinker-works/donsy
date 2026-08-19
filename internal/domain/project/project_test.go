package project

import (
	"reflect"
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

func TestAddRepositoryRejectsInvalidRestoredRepository(t *testing.T) {
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
	value.Repositories[0].URL = ""
	additional, err := repository.New("upstream", "https://github.com/tinker-works/upstream")
	if err != nil {
		t.Fatalf("repository.New() error = %v", err)
	}
	want := value

	if err := value.AddRepository(additional); err == nil {
		t.Fatal("AddRepository() accepted a project with an invalid restored repository")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("AddRepository() mutated the project on error: got %#v, want %#v", value, want)
	}
}
