package application

import (
	"context"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

// RepositoryDiffer computes a branch diff from the host-side clone a project
// repository already has. Ensure-ing that clone up to date still reaches the
// network (a fetch/pull), so the caller's context is threaded through to it.
type RepositoryDiffer interface {
	Diff(ctx context.Context, epicID, repository, base, head string) (string, error)
}

type Workspace interface {
	ListEpics() ([]epic.Epic, error)
	ReadEpic(epicID string) (epic.Epic, error)
	AgentSettings() (agent.AgentSettings, error)
	UpdateAgentSettings(change func(*agent.AgentSettings) error) error
	UpdateRepositorySettings(
		repository string, change func(*agent.RepositorySettings) error,
	) error
	// RepositorySettings reads one repository's override of AgentSettings. A
	// repository with no override file returns the zero value, which
	// AgentSettings.Override treats as "defer to the project-wide settings."
	RepositorySettings(repository string) (agent.RepositorySettings, error)
	// ReadFile reads an arbitrary file from within the tracker store by a path
	// relative to its root, such as a setup_script named by AgentSettings or
	// RepositorySettings.
	ReadFile(path string) (string, error)
	WriteFile(path, contents string) error
	Repositories() ([]string, error)
	UpdateRepositories([]string) error
	CreateEpic(detail epic.Epic) error
	UpdateEpic(epicID string, change func(*epic.Epic) error) error
}
