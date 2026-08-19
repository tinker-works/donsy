package domain

import (
	"strings"
	"testing"
)

func TestSplitRepositoryRef_ShouldReadBothRemoteShapesAndAPlainName(t *testing.T) {
	// Arrange: a tracker is recorded as whatever remote the user typed, and the
	// pool records owner/name.
	cases := []struct {
		ref   string
		owner string
		name  string
		ok    bool
	}{
		{"acme/api", "acme", "api", true},
		{"git@github.com:acme/api.git", "acme", "api", true},
		{"https://github.com/acme/api.git", "acme", "api", true},
		{"https://github.com/acme/api", "acme", "api", true},
		{"  acme/api  ", "acme", "api", true},
		{"api", "", "", false},
		{"", "", "", false},
		{"acme/group/api", "", "", false},
		{"/Users/luuk/repos/api", "", "", false},
		{"acme/", "", "", false},
	}

	for _, tc := range cases {
		// Act
		owner, name, ok := SplitRepositoryRef(tc.ref)

		// Assert
		if owner != tc.owner || name != tc.name || ok != tc.ok {
			t.Fatalf("%q: got (%q, %q, %v), want (%q, %q, %v)",
				tc.ref, owner, name, ok, tc.owner, tc.name, tc.ok)
		}
	}
}

func TestRepositoryOwner_ShouldPreferTheSyncedOrganisation(t *testing.T) {
	// Arrange: a synced repository already records the account it came from; one
	// added by hand carries it in the full name only.
	synced := Repository{Name: "api", FullName: "acme/api", Organisation: "acme"}
	byHand := Repository{Name: "api", FullName: "other/api"}

	// Act & Assert
	if got := synced.Owner(); got != "acme" {
		t.Fatalf("expected the organisation, got %q", got)
	}
	if got := byHand.Owner(); got != "other" {
		t.Fatalf("expected the owner read from the full name, got %q", got)
	}
}

func TestSameOwner_ShouldAllowOnlyTheTrackersOwner(t *testing.T) {
	// Arrange, Act & Assert
	if !SameOwner("acme", "acme/api") {
		t.Fatal("expected the tracker's own owner to pass")
	}
	if !SameOwner("acme", "ACME/api") {
		t.Fatal("expected owners to compare case-insensitively, as GitHub does")
	}
	if SameOwner("acme", "other/api") {
		t.Fatal("expected another owner to be refused")
	}
	if !SameOwner("acme", "api") {
		t.Fatal("expected a name without an owner to belong to the project")
	}
	if !SameOwner("", "other/api") {
		t.Fatal("expected nothing enforced when the tracker names no owner")
	}
}

func TestEnsureSameOwner_ShouldNameEveryForeignRepository(t *testing.T) {
	// Arrange: the settings screen replaces a project's linked set wholesale, so a
	// refusal naming only the first offender would be found one save at a time.
	scope := []string{"acme/api", "other/api", "acme/web", "third/tools"}

	// Act
	err := EnsureSameOwner("acme", scope)

	// Assert
	if err == nil {
		t.Fatal("expected a cross-owner scope to be refused")
	}
	for _, want := range []string{"other/api", "third/tools", "acme"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q named in %q", want, err)
		}
	}
	if strings.Contains(err.Error(), "acme/api") {
		t.Fatalf("expected only the foreign repositories named, got %q", err)
	}
	if err := EnsureSameOwner("acme", []string{"acme/api", "web"}); err != nil {
		t.Fatalf("expected an owned scope to pass, got %v", err)
	}
}
