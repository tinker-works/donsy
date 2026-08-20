package application

import (
	"context"

	"github.com/tinker-works/donsy/internal/domain"
)

type GitHubRepository struct {
	Name         string
	FullName     string
	HTTPURL      string
	SSHURL       string
	Organisation string
}

func (r GitHubRepository) Domain() domain.Repository {
	return domain.Repository{
		Name: r.Name, FullName: r.FullName, HTTPURL: r.HTTPURL,
		SSHURL: r.SSHURL, Organisation: r.Organisation,
	}
}

type GitHubClient interface {
	CheckAuth(context.Context) error
	CurrentUser(context.Context) (string, error)
	ListOrganisations(context.Context) ([]domain.Organisation, error)
	ListRepositories(context.Context, string) ([]GitHubRepository, error)
	// ListUserRepositories lists what the authenticated account itself reaches.
	// It is a separate call because the endpoint behind ListRepositories only
	// answers for organisations, and a user account is not one.
	ListUserRepositories(context.Context) ([]GitHubRepository, error)
}
