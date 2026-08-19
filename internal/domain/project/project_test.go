package project

import (
	"reflect"
	"testing"

	"github.com/tinker-works/donsy/internal/domain/id"
	"github.com/tinker-works/donsy/internal/domain/organisation"
	"github.com/tinker-works/donsy/internal/domain/owner"
	"github.com/tinker-works/donsy/internal/domain/repository"
	"github.com/tinker-works/donsy/internal/domain/setup"
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
	removed, err := value.RemoveRepository(item.Name)
	if err != nil || !removed {
		t.Fatalf("RemoveRepository() = %t, %v, want true, nil", removed, err)
	}
	removed, err = value.RemoveRepository(item.Name)
	if err != nil || removed {
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

func TestRemoveRepositoryRejectsInvalidRestoredRepository(t *testing.T) {
	value, err := New("tracker")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	first, err := repository.New("origin", "https://github.com/tinker-works/tracker")
	if err != nil {
		t.Fatalf("repository.New() error = %v", err)
	}
	second, err := repository.New("upstream", "https://github.com/tinker-works/upstream")
	if err != nil {
		t.Fatalf("repository.New() error = %v", err)
	}
	if err := value.AddRepository(first); err != nil {
		t.Fatalf("AddRepository() error = %v", err)
	}
	if err := value.AddRepository(second); err != nil {
		t.Fatalf("AddRepository() error = %v", err)
	}
	value.Repositories[1].URL = ""
	want := value

	removed, err := value.RemoveRepository(first.Name)
	if err == nil || removed {
		t.Fatal("RemoveRepository() accepted a project with an invalid restored repository")
	}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("RemoveRepository() mutated the project on error: got %#v, want %#v", value, want)
	}
}

func TestValidateRejectsInvalidOptionalValues(t *testing.T) {
	base, err := New("tracker")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for name, mutate := range map[string]func(*Project){
		"owner":        func(value *Project) { value.Owner = owner.Owner{Name: "orphan"} },
		"organisation": func(value *Project) { value.Organisation = organisation.Organisation{Name: "orphan"} },
		"setup":        func(value *Project) { value.Setup = setup.Setup{ID: id.New()} },
	} {
		t.Run(name, func(t *testing.T) {
			value := base
			mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatal("Validate() accepted an invalid optional value")
			}
		})
	}
}
