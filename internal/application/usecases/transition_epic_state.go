package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

type TransitionEpicStateCommand struct {
	Project domain.Project
	EpicID  string
	State   epicpkg.EpicState
	// Force skips the state machine, for reviving an epic that the loop has
	// stranded in a state with no legal way out. Only a person picking the
	// state in the debug transition dialog sets it.
	Force bool
}

type TransitionEpicStateUseCase struct {
	factory application.WorkspaceFactory
}

func (u *TransitionEpicStateUseCase) Handle(command TransitionEpicStateCommand) error {
	return updateEpic(u.factory, command.Project, command.EpicID,
		func(detail *epicpkg.Epic) error {
			if command.Force {
				return detail.ForceState(command.State)
			}
			return detail.TransitionTo(command.State)
		})
}
