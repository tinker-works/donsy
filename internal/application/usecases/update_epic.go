package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

// updateEpic opens the project's workspace and applies one change to one epic.
// It is the shared write path for every use case that edits the store.
func updateEpic(
	factory application.WorkspaceFactory,
	project domain.Project,
	epicID string,
	change func(*epic.Epic) error,
) error {
	return factory.Open(project.Name).UpdateEpic(epicID, change)
}
