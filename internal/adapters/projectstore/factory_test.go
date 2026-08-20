package projectstore

import (
	"path/filepath"
	"testing"
)

func TestFactory_Open_ShouldUseTheProjectNameForLocalStorage(t *testing.T) {
	// Arrange
	factory := NewFactory(filepath.Join(t.TempDir(), "stores", "projects"))

	// Act
	first := factory.Open("project")
	second := factory.Open("project")

	// Assert
	if first != second {
		t.Fatal("expected a project name to resolve to one shared workspace")
	}
	repository, ok := first.(*Repository)
	if !ok {
		t.Fatalf("expected project-store repository, got %T", first)
	}
	if repository.StorePath != filepath.Join(factory.StoreRoot, "project", "store.sqlite") {
		t.Fatalf("unexpected store path: %q", repository.StorePath)
	}
	if _, err := repository.openStore(); err != nil {
		t.Fatal(err)
	}
}

func TestRepository_ShouldPreserveExistingProjectLinks(t *testing.T) {
	// Arrange
	path := copyFixture(t, "store.sqlite")
	repository := OpenRepository("different name", path)

	// Act
	workspace, err := repository.Repositories()

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace) != 2 || workspace[0] != "acme/api" || workspace[1] != "acme/web" {
		t.Fatalf("unexpected repository links: %#v", workspace)
	}
}
