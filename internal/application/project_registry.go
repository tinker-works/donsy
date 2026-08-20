package application

import "github.com/tinker-works/donsy/internal/domain"

type ProjectRegistry interface {
	List() ([]domain.Project, error)
	Create(*domain.Project) error
	Touch(uint) error
	// Delete drops the local registration only. The remote and the cloned
	// working copy are left alone, so forgetting a project is reversible by
	// re-adding it from its URL.
	Delete(uint) error
	Close() error
}

type OrganisationRegistry interface {
	ListOrganisations() ([]domain.Organisation, error)
	CreateOrganisation(*domain.Organisation) error
	DeleteOrganisation(name string) error
}

type RepositoryRegistry interface {
	ListRepositories() ([]domain.Repository, error)
	// ReplaceRepositories rewrites one organisation's whole set, which is what
	// a sync from GitHub does.
	ReplaceRepositories(organisation string, repositories []domain.Repository) error
	// SaveRepository adds or updates a single repository. Discovery replaces
	// wholesale; naming one by hand must not delete the rest.
	SaveRepository(repository domain.Repository) error
}
