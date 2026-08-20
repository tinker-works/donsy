package usecases

import (
	"sync"

	"github.com/tinker-works/donsy/internal/application"
)

type fakeFactory struct {
	mu        sync.Mutex
	workspace *fakeWorkspace
	byPath    map[string]*fakeWorkspace
	openPath  string
}

func (f *fakeFactory) Open(path string) application.Workspace {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openPath = path
	if workspace, ok := f.byPath[path]; ok {
		return workspace
	}
	return f.workspace
}
