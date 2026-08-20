package usecases

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
)

type fakeGitHubClient struct {
	organisations    []domain.Organisation
	organisationsErr error
	repositories     map[string][]application.GitHubRepository
	// login is what CurrentUser reports. Empty means "owner", the login most
	// tests do not care about.
	login            string
	userRepositories []application.GitHubRepository
	listedUser       int

	// repositoriesErr, when set, is returned by ListRepositories. If
	// repositoriesErrFor is non-empty, the error only applies to that
	// organisation, letting tests fail one while leaving others healthy.
	repositoriesErr    error
	repositoriesErrFor string
	listedFor          []string
}

func (c *fakeGitHubClient) CheckAuth(context.Context) error { return nil }

func (c *fakeGitHubClient) CurrentUser(context.Context) (string, error) {
	if c.login == "" {
		return "owner", nil
	}
	return c.login, nil
}

func (c *fakeGitHubClient) ListOrganisations(context.Context) ([]domain.Organisation, error) {
	return c.organisations, c.organisationsErr
}

func (c *fakeGitHubClient) ListRepositories(
	_ context.Context, organisation string,
) ([]application.GitHubRepository, error) {
	c.listedFor = append(c.listedFor, organisation)
	if c.repositoriesErr != nil &&
		(c.repositoriesErrFor == "" || c.repositoriesErrFor == organisation) {
		return nil, c.repositoriesErr
	}
	return c.repositories[organisation], nil
}

func (c *fakeGitHubClient) ListUserRepositories(
	context.Context,
) ([]application.GitHubRepository, error) {
	c.listedUser++
	return c.userRepositories, nil
}

type fakeOrganisationRegistry struct {
	organisations []domain.Organisation
	listErr       error
	createErr     error
	deleteErr     error
	deleted       []string
}

func (r *fakeOrganisationRegistry) ListOrganisations() ([]domain.Organisation, error) {
	return r.organisations, r.listErr
}

// CreateOrganisation upserts by name like the sqlite registry does, so
// re-discovering what is already stored stays idempotent in tests too.
func (r *fakeOrganisationRegistry) CreateOrganisation(organisation *domain.Organisation) error {
	if r.createErr != nil {
		return r.createErr
	}
	for _, existing := range r.organisations {
		if existing.Name == organisation.Name {
			return nil
		}
	}
	r.organisations = append(r.organisations, *organisation)
	return nil
}

func (r *fakeOrganisationRegistry) DeleteOrganisation(name string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	r.deleted = append(r.deleted, name)
	remaining := make([]domain.Organisation, 0, len(r.organisations))
	for _, organisation := range r.organisations {
		if organisation.Name != name {
			remaining = append(remaining, organisation)
		}
	}
	r.organisations = remaining
	return nil
}

func TestDiscoverOrganisationsUseCase_ShouldPersistDiscoveredOrganisations(t *testing.T) {
	// Arrange: one organisation is already known, so discovery must merge
	// rather than duplicate.
	registry := &fakeOrganisationRegistry{
		organisations: []domain.Organisation{{Name: "acme"}},
	}
	client := &fakeGitHubClient{
		organisations: []domain.Organisation{{Name: "acme"}, {Name: "globex"}},
	}
	useCase := &DiscoverOrganisationsUseCase{client: client, registry: registry}

	// Act
	organisations, err := useCase.Handle(context.Background())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(organisations) != 3 {
		t.Fatalf("expected both organisations and the account, got %+v", organisations)
	}
	if len(registry.organisations) != 3 {
		t.Fatalf("expected discovery to persist, got %+v", registry.organisations)
	}
}

func TestDiscoverOrganisationsUseCase_ShouldRegisterTheAuthenticatedAccount(t *testing.T) {
	// Arrange: GitHub never lists the account among its own organisations, so
	// discovery has to add it or the user's own repositories stay unreachable.
	registry := &fakeOrganisationRegistry{}
	client := &fakeGitHubClient{
		login:         "octocat",
		organisations: []domain.Organisation{{Name: "acme"}},
	}
	useCase := &DiscoverOrganisationsUseCase{client: client, registry: registry}

	// Act
	organisations, err := useCase.Handle(context.Background())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !containsOrganisation(organisations, "octocat") {
		t.Fatalf("expected the account registered, got %+v", organisations)
	}
}

func containsOrganisation(organisations []domain.Organisation, name string) bool {
	for _, organisation := range organisations {
		if organisation.Name == name {
			return true
		}
	}
	return false
}

func TestDiscoverOrganisationsUseCase_ShouldPropagateAClientError(t *testing.T) {
	// Arrange
	client := &fakeGitHubClient{organisationsErr: fmt.Errorf("network down")}
	registry := &fakeOrganisationRegistry{}
	useCase := &DiscoverOrganisationsUseCase{client: client, registry: registry}

	// Act
	_, err := useCase.Handle(context.Background())

	// Assert
	if err == nil {
		t.Fatal("expected the client error to propagate")
	}
	if len(registry.organisations) != 0 {
		t.Fatalf("expected nothing persisted, got %+v", registry.organisations)
	}
}

func TestDiscoverOrganisationsUseCase_ShouldStopWhenPersistingFails(t *testing.T) {
	// Arrange
	client := &fakeGitHubClient{organisations: []domain.Organisation{{Name: "acme"}}}
	registry := &fakeOrganisationRegistry{createErr: fmt.Errorf("disk full")}
	useCase := &DiscoverOrganisationsUseCase{client: client, registry: registry}

	// Act
	_, err := useCase.Handle(context.Background())

	// Assert
	if err == nil {
		t.Fatal("expected the registry error to propagate")
	}
}

func TestListOrganisationsUseCase_ShouldReturnTheRegistrysOrganisations(t *testing.T) {
	// Arrange
	registry := &fakeOrganisationRegistry{
		organisations: []domain.Organisation{{Name: "acme"}},
	}
	useCase := &ListOrganisationsUseCase{registry: registry}

	// Act
	organisations, err := useCase.Handle()

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(organisations) != 1 || organisations[0].Name != "acme" {
		t.Fatalf("unexpected organisations: %+v", organisations)
	}
}

func TestAddOrganisationUseCase_ShouldTrimTheNameBeforeStoring(t *testing.T) {
	// Arrange
	registry := &fakeOrganisationRegistry{}
	useCase := &AddOrganisationUseCase{registry: registry}

	// Act
	err := useCase.Handle("  acme  ")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.organisations) != 1 || registry.organisations[0].Name != "acme" {
		t.Fatalf("unexpected organisations: %+v", registry.organisations)
	}
}

func TestAddOrganisationUseCase_ShouldRejectABlankName(t *testing.T) {
	// Arrange
	registry := &fakeOrganisationRegistry{}
	useCase := &AddOrganisationUseCase{registry: registry}

	for _, name := range []string{"", "   "} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			// Act
			err := useCase.Handle(name)

			// Assert
			if err == nil {
				t.Fatalf("expected %q to be rejected", name)
			}
		})
	}
	if len(registry.organisations) != 0 {
		t.Fatalf("expected nothing stored, got %+v", registry.organisations)
	}
}

func TestRemoveOrganisationUseCase_ShouldDeleteTheTrimmedName(t *testing.T) {
	// Arrange
	registry := &fakeOrganisationRegistry{
		organisations: []domain.Organisation{{Name: "acme"}},
	}
	useCase := &RemoveOrganisationUseCase{registry: registry}

	// Act
	err := useCase.Handle("  acme  ")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.deleted) != 1 || registry.deleted[0] != "acme" {
		t.Fatalf("unexpected deletions: %+v", registry.deleted)
	}
	if len(registry.organisations) != 0 {
		t.Fatalf("expected the organisation removed, got %+v", registry.organisations)
	}
}

func TestRemoveOrganisationUseCase_ShouldPropagateADeleteError(t *testing.T) {
	// Arrange
	registry := &fakeOrganisationRegistry{deleteErr: fmt.Errorf("locked")}
	useCase := &RemoveOrganisationUseCase{registry: registry}

	// Act
	err := useCase.Handle("acme")

	// Assert
	if err == nil {
		t.Fatal("expected the delete error to propagate")
	}
}

func TestSyncRepositoriesUseCase_ShouldReplaceEveryOrganisationsRepositories(t *testing.T) {
	// Arrange: acme already holds a repository GitHub no longer reports, which
	// a sync must drop — replace is the whole point over append.
	organisations := &fakeOrganisationRegistry{
		organisations: []domain.Organisation{{Name: "acme"}, {Name: "globex"}},
	}
	repositories := &fakeRepositoryRegistry{repositories: []domain.Repository{
		{FullName: "acme/stale", Name: "stale", Organisation: "acme"},
	}}
	client := &fakeGitHubClient{repositories: map[string][]application.GitHubRepository{
		"acme":   {{Name: "widgets", FullName: "acme/widgets", Organisation: "acme"}},
		"globex": {{Name: "gadgets", FullName: "globex/gadgets", Organisation: "globex"}},
	}}
	useCase := &SyncRepositoriesUseCase{
		client: client, organisations: organisations, repositories: repositories,
	}

	// Act
	err := useCase.Handle(context.Background())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories.repositories) != 2 {
		t.Fatalf("expected the synced set only, got %+v", repositories.repositories)
	}
	for _, repository := range repositories.repositories {
		if repository.FullName == "acme/stale" {
			t.Fatalf("expected the stale repository replaced, got %+v", repositories.repositories)
		}
	}
}

func TestSyncRepositoriesUseCase_ShouldStopAtTheFirstFailingOrganisation(t *testing.T) {
	// Arrange
	organisations := &fakeOrganisationRegistry{
		organisations: []domain.Organisation{{Name: "acme"}, {Name: "globex"}},
	}
	repositories := &fakeRepositoryRegistry{}
	client := &fakeGitHubClient{
		repositoriesErr:    fmt.Errorf("rate limited"),
		repositoriesErrFor: "acme",
	}
	useCase := &SyncRepositoriesUseCase{
		client: client, organisations: organisations, repositories: repositories,
	}

	// Act
	err := useCase.Handle(context.Background())

	// Assert
	if err == nil || !strings.Contains(err.Error(), "acme") {
		t.Fatalf("expected an error naming the failing organisation, got %v", err)
	}
	if len(client.listedFor) != 1 {
		t.Fatalf("expected the sync to stop at the first failure, listed %v", client.listedFor)
	}
	if len(repositories.repositories) != 0 {
		t.Fatalf("expected nothing replaced, got %+v", repositories.repositories)
	}
}

func TestSyncRepositoriesUseCase_ShouldReadTheAccountsOwnRepositories(t *testing.T) {
	// Arrange: the account is not registered yet, which is the state every store
	// that was set up before personal repositories existed is in.
	organisations := &fakeOrganisationRegistry{
		organisations: []domain.Organisation{{Name: "acme"}},
	}
	repositories := &fakeRepositoryRegistry{}
	client := &fakeGitHubClient{
		login: "octocat",
		repositories: map[string][]application.GitHubRepository{
			"acme": {{Name: "widgets", FullName: "acme/widgets", Organisation: "acme"}},
		},
		userRepositories: []application.GitHubRepository{
			{Name: "dotfiles", FullName: "octocat/dotfiles", Organisation: "octocat"},
		},
	}
	useCase := &SyncRepositoriesUseCase{
		client: client, organisations: organisations, repositories: repositories,
	}

	// Act
	err := useCase.Handle(context.Background())

	// Assert: the account was registered and its repositories came from the
	// user endpoint, not from an organisation listing that would 404.
	if err != nil {
		t.Fatal(err)
	}
	if !containsOrganisation(organisations.organisations, "octocat") {
		t.Fatalf("expected the account registered, got %+v", organisations.organisations)
	}
	if client.listedUser != 1 {
		t.Fatalf("expected one user listing, got %d", client.listedUser)
	}
	for _, listed := range client.listedFor {
		if listed == "octocat" {
			t.Fatal("expected the account not to be listed as an organisation")
		}
	}
	if len(repositories.repositories) != 2 ||
		!containsRepository(repositories.repositories, "octocat/dotfiles") {
		t.Fatalf("expected the personal repository stored, got %+v", repositories.repositories)
	}
}

func TestSyncRepositoriesUseCase_ShouldLeaveARegisteredOrganisationsRepositoryToIt(t *testing.T) {
	// Arrange: acme/widgets reaches the account as a collaboration and acme as
	// its own organisation. Only acme's set may hold it, or which one it lands
	// under would depend on the order the sets are synced in.
	organisations := &fakeOrganisationRegistry{
		organisations: []domain.Organisation{{Name: "octocat"}, {Name: "acme"}},
	}
	repositories := &fakeRepositoryRegistry{}
	client := &fakeGitHubClient{
		login: "octocat",
		repositories: map[string][]application.GitHubRepository{
			"acme": {{Name: "widgets", FullName: "acme/widgets", Organisation: "acme"}},
		},
		userRepositories: []application.GitHubRepository{
			{Name: "dotfiles", FullName: "octocat/dotfiles", Organisation: "octocat"},
			{Name: "widgets", FullName: "acme/widgets", Organisation: "acme"},
			{Name: "notes", FullName: "someone/notes", Organisation: "someone"},
		},
	}
	useCase := &SyncRepositoriesUseCase{
		client: client, organisations: organisations, repositories: repositories,
	}

	// Act
	err := useCase.Handle(context.Background())

	// Assert: the collaboration with an unregistered owner stays in the
	// account's set, the one acme owns does not.
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories.replaced["octocat"]) != 2 {
		t.Fatalf("unexpected personal set: %+v", repositories.replaced["octocat"])
	}
	if !containsRepository(repositories.replaced["octocat"], "someone/notes") {
		t.Fatalf("expected the collaboration kept, got %+v", repositories.replaced["octocat"])
	}
	if containsRepository(repositories.replaced["octocat"], "acme/widgets") {
		t.Fatalf("expected acme's repository left to acme, got %+v", repositories.replaced["octocat"])
	}
}

func containsRepository(repositories []domain.Repository, fullName string) bool {
	for _, repository := range repositories {
		if repository.FullName == fullName {
			return true
		}
	}
	return false
}

func TestListRepositoriesUseCase_ShouldReturnTheRegistrysRepositories(t *testing.T) {
	// Arrange
	registry := &fakeRepositoryRegistry{repositories: []domain.Repository{
		{FullName: "acme/widgets", Name: "widgets", Organisation: "acme"},
	}}
	useCase := &ListRepositoriesUseCase{registry: registry}

	// Act
	repositories, err := useCase.Handle()

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 1 || repositories[0].FullName != "acme/widgets" {
		t.Fatalf("unexpected repositories: %+v", repositories)
	}
}

func TestListProjectRepositoriesUseCase_ShouldReadTheProjectWorkspace(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{repositories: []string{"acme/widgets"}}
	factory := &fakeFactory{workspace: workspace}
	useCase := &ListProjectRepositoriesUseCase{factory: factory}

	// Act
	repositories, err := useCase.Handle(domain.Project{Name: "one"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 1 || repositories[0] != "acme/widgets" {
		t.Fatalf("unexpected repositories: %+v", repositories)
	}
	if factory.openPath != "one" {
		t.Fatalf("expected the project's workspace opened, got %q", factory.openPath)
	}
}

func TestUpdateProjectRepositoriesUseCase_ShouldWriteThroughTheWorkspace(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{repositories: []string{"acme/stale"}}
	useCase := &UpdateProjectRepositoriesUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(UpdateProjectRepositoriesCommand{
		Project:      domain.Project{Name: "one"},
		Repositories: []string{"acme/widgets", "acme/gadgets"},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.repositories) != 2 || workspace.repositories[0] != "acme/widgets" {
		t.Fatalf("unexpected repositories: %+v", workspace.repositories)
	}
}

func TestUpdateProjectRepositoriesUseCase_ShouldRejectAMissingProjectName(t *testing.T) {
	// Arrange: opening an empty path would silently write somewhere real.
	workspace := &fakeWorkspace{}
	useCase := &UpdateProjectRepositoriesUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(UpdateProjectRepositoriesCommand{
		Repositories: []string{"acme/widgets"},
	})

	// Assert
	if err == nil {
		t.Fatal("expected a project without a local path to be rejected")
	}
	if len(workspace.repositories) != 0 {
		t.Fatalf("expected nothing written, got %+v", workspace.repositories)
	}
}
