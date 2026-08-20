package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

type SetBranchPrefixCommand struct {
	Project domain.Project
	EpicID  string
	// Prefix is free text. The aggregate slugs it, so callers pass whatever the
	// user typed rather than pre-formatting a branch segment here.
	Prefix string
}

// SetBranchPrefixUseCase names the tracker item an epic's branches belong to.
// It is only useful before the epic's branches are cut; the aggregate refuses
// the write afterwards.
type SetBranchPrefixUseCase struct {
	factory application.WorkspaceFactory
}

func (u *SetBranchPrefixUseCase) Handle(command SetBranchPrefixCommand) error {
	return updateEpic(u.factory, command.Project, command.EpicID,
		func(detail *epicpkg.Epic) error {
			return detail.SetBranchPrefix(command.Prefix)
		})
}
