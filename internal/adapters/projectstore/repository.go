package projectstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

// Repository owns one project's local tracker store. It does not represent a
// Git checkout; Git workspaces belong to the runtime adapters.
type Repository struct {
	Name, StorePath string
	lock            sync.Mutex
	store           *Store
}

var _ application.Workspace = (*Repository)(nil)

// OpenRepository returns the shared handle for a project's local store.
//
// The daemon, worker, and background refresh can reach the same project from
// different goroutines. One shared lock serializes local SQLite access.
func OpenRepository(name, storePath string) *Repository {
	key := name + "\x00" + storePath
	openLock.Lock()
	defer openLock.Unlock()
	if existing, ok := opened[key]; ok {
		return existing
	}
	repository := &Repository{Name: name, StorePath: storePath}
	opened[key] = repository
	return repository
}

var (
	openLock sync.Mutex
	opened   = map[string]*Repository{}
)

func (r *Repository) openStore() (*Store, error) {
	if r.store != nil {
		return r.store, nil
	}
	if err := os.MkdirAll(filepath.Dir(r.StorePath), 0o700); err != nil {
		return nil, fmt.Errorf("create local store directory: %w", err)
	}
	store, err := OpenStore(r.StorePath)
	if err != nil {
		return nil, err
	}
	if err := r.ensureProject(store); err != nil {
		_ = store.Close()
		return nil, err
	}
	r.store = store
	return store, nil
}

// ensureProject only creates metadata for a genuinely empty SQLite store. An
// existing store's metadata includes repository links and must survive reopening.
func (r *Repository) ensureProject(store *Store) error {
	_, err := store.ReadProject()
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return store.WriteProject(Project{Name: r.Name})
}

func (r *Repository) ListEpics() ([]epic.Epic, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	store, err := r.openStore()
	if err != nil {
		return nil, err
	}
	return store.ListEpics()
}

func (r *Repository) ReadEpic(epicID string) (epic.Epic, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	store, err := r.openStore()
	if err != nil {
		return epic.Epic{}, err
	}
	return store.ReadEpic(epicID)
}

// AgentSettings and the readers below share the mutation lock so callers see a
// completed local SQLite transaction rather than another goroutine's write.
func (r *Repository) AgentSettings() (agent.AgentSettings, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	store, err := r.openStore()
	if err != nil {
		return agent.AgentSettings{}, err
	}
	return store.AgentSettings()
}

func (r *Repository) UpdateAgentSettings(change func(*agent.AgentSettings) error) error {
	return r.mutate(func(store *Store) error {
		settings, err := store.AgentSettings()
		if err != nil {
			return err
		}
		if err := change(&settings); err != nil {
			return err
		}
		return store.WriteAgentSettings(settings)
	})
}

func (r *Repository) UpdateRepositorySettings(
	repository string, change func(*agent.RepositorySettings) error,
) error {
	return r.mutate(func(store *Store) error {
		settings, err := store.RepositorySettings(repository)
		if err != nil {
			return err
		}
		if err := change(&settings); err != nil {
			return err
		}
		return store.WriteRepositorySettings(repository, settings)
	})
}

func (r *Repository) RepositorySettings(repository string) (agent.RepositorySettings, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	store, err := r.openStore()
	if err != nil {
		return agent.RepositorySettings{}, err
	}
	return store.RepositorySettings(repository)
}

func (r *Repository) ReadFile(path string) (string, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	store, err := r.openStore()
	if err != nil {
		return "", err
	}
	return store.ReadFile(path)
}

func (r *Repository) WriteFile(path, contents string) error {
	return r.mutate(func(store *Store) error {
		return store.WriteFile(path, contents)
	})
}

func (r *Repository) Repositories() ([]string, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	store, err := r.openStore()
	if err != nil {
		return nil, err
	}
	return store.Repositories()
}

func (r *Repository) UpdateRepositories(repositories []string) error {
	return r.mutate(func(store *Store) error {
		return store.ReplaceRepositories(repositories)
	})
}

func (r *Repository) CreateEpic(detail epic.Epic) error {
	return r.mutate(func(store *Store) error {
		return store.WriteEpic(detail)
	})
}

func (r *Repository) UpdateEpic(id string, change func(*epic.Epic) error) error {
	return r.mutate(func(store *Store) error {
		detail, err := store.ReadEpic(id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("epic %s not found", id)
			}
			return fmt.Errorf("epic %s not found: %w", id, err)
		}
		if err := change(&detail); err != nil {
			return err
		}
		return store.WriteEpic(detail)
	})
}

func (r *Repository) mutate(mutate func(*Store) error) error {
	r.lock.Lock()
	defer r.lock.Unlock()
	store, err := r.openStore()
	if err != nil {
		return err
	}
	return mutate(store)
}
