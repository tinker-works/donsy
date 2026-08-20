package usecases

import (
	"context"
	"fmt"
	"strings"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
)

// registerAuthenticatedAccount registers the logged-in account as an
// organisation and reports its login. GitHub never lists an account among its
// own organisations, so without this the repositories the user owns personally
// are the one part of the pool discovery cannot reach.
func registerAuthenticatedAccount(
	ctx context.Context,
	client application.GitHubClient,
	registry application.OrganisationRegistry,
) (string, error) {
	login, err := client.CurrentUser(ctx)
	if err != nil {
		return "", err
	}
	if err := registry.CreateOrganisation(&domain.Organisation{Name: login}); err != nil {
		return "", err
	}
	return login, nil
}

type DiscoverOrganisationsUseCase struct {
	client   application.GitHubClient
	registry application.OrganisationRegistry
}

func (u *DiscoverOrganisationsUseCase) Handle(ctx context.Context) ([]domain.Organisation, error) {
	organisations, err := u.client.ListOrganisations(ctx)
	if err != nil {
		return nil, err
	}
	// Persisted after the listing so a discovery that could not reach GitHub
	// stores nothing at all.
	if _, err := registerAuthenticatedAccount(ctx, u.client, u.registry); err != nil {
		return nil, err
	}
	for _, organisation := range organisations {
		if err := u.registry.CreateOrganisation(&organisation); err != nil {
			return nil, err
		}
	}
	return u.registry.ListOrganisations()
}

type ListOrganisationsUseCase struct {
	registry application.OrganisationRegistry
}

func (u *ListOrganisationsUseCase) Handle() ([]domain.Organisation, error) {
	return u.registry.ListOrganisations()
}

type AddOrganisationUseCase struct {
	registry application.OrganisationRegistry
}

func (u *AddOrganisationUseCase) Handle(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("organisation name is required")
	}
	return u.registry.CreateOrganisation(&domain.Organisation{Name: name})
}

type RemoveOrganisationUseCase struct {
	registry application.OrganisationRegistry
}

func (u *RemoveOrganisationUseCase) Handle(name string) error {
	return u.registry.DeleteOrganisation(strings.TrimSpace(name))
}

type SyncRepositoriesUseCase struct {
	client        application.GitHubClient
	organisations application.OrganisationRegistry
	repositories  application.RepositoryRegistry
}

func (u *SyncRepositoriesUseCase) Handle(ctx context.Context) error {
	// Registering the account here as well as in discovery is what makes an
	// existing store pick it up: discovery only runs while nothing is registered
	// yet, so a store that already knows an organisation never runs it again.
	login, err := registerAuthenticatedAccount(ctx, u.client, u.organisations)
	if err != nil {
		return err
	}
	organisations, err := u.organisations.ListOrganisations()
	if err != nil {
		return err
	}
	for _, organisation := range organisations {
		githubRepositories, err := u.listFor(ctx, organisation.Name, login, organisations)
		if err != nil {
			return fmt.Errorf("list repositories for %s: %w", organisation.Name, err)
		}
		repositories := make([]domain.Repository, 0, len(githubRepositories))
		for _, repository := range githubRepositories {
			repositories = append(repositories, repository.Domain())
		}
		if err := u.repositories.ReplaceRepositories(organisation.Name, repositories); err != nil {
			return err
		}
	}
	return nil
}

func (u *SyncRepositoriesUseCase) listFor(
	ctx context.Context, organisation, login string, registered []domain.Organisation,
) ([]application.GitHubRepository, error) {
	if !strings.EqualFold(organisation, login) {
		return u.client.ListRepositories(ctx, organisation)
	}
	repositories, err := u.client.ListUserRepositories(ctx)
	if err != nil {
		return nil, err
	}
	// A repository owned by an organisation that is registered in its own right
	// belongs to that organisation's set. Keeping it in both would make which
	// one it ends up under depend on the order the sets happen to be synced in.
	kept := make([]application.GitHubRepository, 0, len(repositories))
	for _, repository := range repositories {
		if owned := repository.Organisation; !strings.EqualFold(owned, login) &&
			registeredContains(registered, owned) {
			continue
		}
		kept = append(kept, repository)
	}
	return kept, nil
}

func registeredContains(organisations []domain.Organisation, name string) bool {
	for _, organisation := range organisations {
		if strings.EqualFold(organisation.Name, name) {
			return true
		}
	}
	return false
}

type ListRepositoriesUseCase struct {
	registry application.RepositoryRegistry
}

func (u *ListRepositoriesUseCase) Handle() ([]domain.Repository, error) {
	return u.registry.ListRepositories()
}

type UpdateProjectRepositoriesUseCase struct {
	factory application.WorkspaceFactory
}

type ListProjectRepositoriesUseCase struct {
	factory application.WorkspaceFactory
}

func (u *ListProjectRepositoriesUseCase) Handle(project domain.Project) ([]string, error) {
	return u.factory.Open(project.Name).Repositories()
}

type UpdateProjectRepositoriesCommand struct {
	Project      domain.Project
	Repositories []string
}

func (u *UpdateProjectRepositoriesUseCase) Handle(command UpdateProjectRepositoriesCommand) error {
	if command.Project.Name == "" {
		return fmt.Errorf("project name is required")
	}
	// Linking is where a repository enters a project, and every later stage —
	// scoping an epic, cloning for a round — draws from what is linked here.
	return u.factory.Open(command.Project.Name).
		UpdateRepositories(command.Repositories)
}
