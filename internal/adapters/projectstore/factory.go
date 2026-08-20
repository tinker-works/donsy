package projectstore

import (
	"path/filepath"

	"github.com/tinker-works/donsy/internal/application"
)

var _ application.WorkspaceFactory = (*Factory)(nil)

// Factory opens per-project SQLite tracker stores below StoreRoot.
type Factory struct {
	StoreRoot string
}

func NewFactory(storeRoot string) *Factory {
	return &Factory{StoreRoot: storeRoot}
}

func (f *Factory) Open(name string) application.Workspace {
	return OpenRepository(name, f.storePath(name))
}

func (f *Factory) storePath(name string) string {
	return filepath.Join(f.StoreRoot, name, "store.sqlite")
}
