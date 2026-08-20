package usecases

import (
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
)

type fakeRepositoryRegistry struct {
	repositories []domain.Repository
	// replaced keeps the last set stored per organisation, so a test can assert
	// which set a repository landed in rather than only that it was stored.
	replaced map[string][]domain.Repository
	saveErr  error
}

func (r *fakeRepositoryRegistry) ListRepositories() ([]domain.Repository, error) {
	return r.repositories, nil
}

// ReplaceRepositories mirrors the sqlite registry: the organisation being
// replaced is what every repository in the set is stored under, and storing is
// an upsert by full name, so a repository arriving from another set moves.
func (r *fakeRepositoryRegistry) ReplaceRepositories(
	organisation string, repositories []domain.Repository,
) error {
	stored := make([]domain.Repository, 0, len(repositories))
	for _, repository := range repositories {
		repository.Organisation = organisation
		stored = append(stored, repository)
	}
	if r.replaced == nil {
		r.replaced = map[string][]domain.Repository{}
	}
	r.replaced[organisation] = stored
	remaining := make([]domain.Repository, 0, len(r.repositories)+len(stored))
	for _, existing := range r.repositories {
		if existing.Organisation == organisation || containsRepository(stored, existing.FullName) {
			continue
		}
		remaining = append(remaining, existing)
	}
	remaining = append(remaining, stored...)
	r.repositories = remaining
	return nil
}

func (r *fakeRepositoryRegistry) SaveRepository(repository domain.Repository) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	for index, existing := range r.repositories {
		if existing.FullName == repository.FullName {
			r.repositories[index] = repository
			return nil
		}
	}
	r.repositories = append(r.repositories, repository)
	return nil
}

func TestAddRepositoryUseCase_ShouldRegisterOneRepositoryByName(t *testing.T) {
	// Arrange
	registry := &fakeRepositoryRegistry{}
	useCase := &AddRepositoryUseCase{registry: registry}

	// Act
	repository, err := useCase.Handle(AddRepositoryCommand{FullName: " acme/widgets "})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if repository.Name != "widgets" || repository.Organisation != "acme" {
		t.Fatalf("unexpected repository: %+v", repository)
	}
	if repository.SSHURL != "git@github.com:acme/widgets.git" {
		t.Fatalf("unexpected SSH URL: %q", repository.SSHURL)
	}
	if len(registry.repositories) != 1 {
		t.Fatalf("expected one repository stored, got %+v", registry.repositories)
	}
}

func TestAddRepositoryUseCase_ShouldNotDeleteTheOrganisationsOtherRepositories(t *testing.T) {
	// Arrange: the registry's only other write is a wholesale replace, which
	// is exactly what adding one repository must not do.
	registry := &fakeRepositoryRegistry{repositories: []domain.Repository{
		{FullName: "acme/gadgets", Name: "gadgets", Organisation: "acme"},
	}}
	useCase := &AddRepositoryUseCase{registry: registry}

	// Act
	if _, err := useCase.Handle(AddRepositoryCommand{FullName: "acme/widgets"}); err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(registry.repositories) != 2 {
		t.Fatalf("expected both repositories, got %+v", registry.repositories)
	}
}

func TestAddRepositoryUseCase_ShouldBeIdempotent(t *testing.T) {
	// Arrange: re-adding what discovery already found refreshes it.
	registry := &fakeRepositoryRegistry{}
	useCase := &AddRepositoryUseCase{registry: registry}

	// Act
	if _, err := useCase.Handle(AddRepositoryCommand{FullName: "acme/widgets"}); err != nil {
		t.Fatal(err)
	}
	if _, err := useCase.Handle(AddRepositoryCommand{FullName: "acme/widgets"}); err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(registry.repositories) != 1 {
		t.Fatalf("expected one repository, got %+v", registry.repositories)
	}
}

func TestAddRepositoryUseCase_ShouldRejectMalformedNames(t *testing.T) {
	// Arrange
	useCase := &AddRepositoryUseCase{registry: &fakeRepositoryRegistry{}}

	for _, name := range []string{"", "widgets", "acme/", "/widgets", "acme/widgets/extra", "  /  "} {
		t.Run(name, func(t *testing.T) {
			// Act
			_, err := useCase.Handle(AddRepositoryCommand{FullName: name})

			// Assert
			if err == nil {
				t.Fatalf("expected %q to be rejected", name)
			}
		})
	}
}
