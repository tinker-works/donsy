package usecases

import (
	"fmt"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
)

type AddRepositoryCommand struct {
	// FullName is owner/name. Everything else is derived from it, because a
	// caller typing a repository knows that and nothing more.
	FullName string
}

// AddRepositoryUseCase registers one repository by name, without a GitHub
// round trip. Organisation discovery finds repositories in bulk; this is for
// the one that discovery cannot see — a repository outside the organisations
// the user listed, or one added before the next sync.
type AddRepositoryUseCase struct {
	registry application.RepositoryRegistry
}

func (u *AddRepositoryUseCase) Handle(command AddRepositoryCommand) (domain.Repository, error) {
	owner, name, ok := domain.SplitRepositoryRef(command.FullName)
	if !ok {
		return domain.Repository{}, fmt.Errorf(
			"repository must use owner/name form, got %q", command.FullName,
		)
	}
	repository := domain.Repository{
		Name:         name,
		FullName:     owner + "/" + name,
		Organisation: owner,
		HTTPURL:      "https://github.com/" + owner + "/" + name,
		SSHURL:       "git@github.com:" + owner + "/" + name + ".git",
	}
	if err := u.registry.SaveRepository(repository); err != nil {
		return domain.Repository{}, err
	}
	return repository, nil
}
