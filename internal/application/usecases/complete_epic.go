package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

type CompleteEpicCommand struct {
	Project domain.Project
	EpicID  string
}

// CompleteEpicUseCase closes the loop on a Ready epic whose every issue has
// been merged or closed. Without it, Ready is a dead end: nothing else moves
// an epic to Done, so a fully delivered epic would sit in the board's active
// lanes forever.
type CompleteEpicUseCase struct {
	factory application.WorkspaceFactory
}

// Handle reports whether it completed the epic. False is the ordinary answer
// while work is still in flight.
func (u *CompleteEpicUseCase) Handle(command CompleteEpicCommand) (bool, error) {
	workspace := u.factory.Open(command.Project.Name)
	current, err := workspace.ReadEpic(command.EpicID)
	if err != nil {
		return false, err
	}
	if current.State != epicpkg.EpicStateReady || !current.Delivered() {
		return false, nil
	}
	return true, workspace.UpdateEpic(
		current.ID,
		func(target *epicpkg.Epic) error {
			return target.Apply(epicpkg.EpicEventDone)
		},
	)
}
