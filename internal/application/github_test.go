package application

import (
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
)

func TestGitHubRepository_Domain_ShouldMapEveryField(t *testing.T) {
	// Arrange
	source := GitHubRepository{
		Name:         "api",
		FullName:     "acme/api",
		HTTPURL:      "https://github.com/acme/api",
		SSHURL:       "git@github.com:acme/api.git",
		Organisation: "acme",
	}

	// Act
	got := source.Domain()

	// Assert
	want := domain.Repository{
		Name:         "api",
		FullName:     "acme/api",
		HTTPURL:      "https://github.com/acme/api",
		SSHURL:       "git@github.com:acme/api.git",
		Organisation: "acme",
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
