package opencode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentials_Discard_ShouldRemoveOnlyThatSandboxesCredentials(t *testing.T) {
	// A reclaimed sandbox otherwise leaves a real provider credential on disk for an
	// instance that no longer exists.
	// Arrange
	root := t.TempDir()
	credentials := Credentials{root: filepath.Join(root, "credentials")}
	for _, sandbox := range []string{"gm-1-gone", "gm-1-live"} {
		directory := filepath.Join(credentials.root, sandbox)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "auth.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	if err := credentials.Discard("gm-1-gone"); err != nil {
		t.Fatal(err)
	}

	// Assert
	if _, err := os.Stat(filepath.Join(credentials.root, "gm-1-gone")); !os.IsNotExist(err) {
		t.Fatal("expected the reclaimed sandbox's credentials to be removed")
	}
	if _, err := os.Stat(filepath.Join(credentials.root, "gm-1-live", "auth.json")); err != nil {
		t.Fatalf("expected another sandbox's credentials to survive: %v", err)
	}
}

func TestCredentials_Discard_ShouldRejectANameThatEscapesTheRoot(t *testing.T) {
	// Arrange
	root := t.TempDir()
	credentials := Credentials{root: filepath.Join(root, "credentials")}
	if err := os.MkdirAll(filepath.Join(credentials.root, "gm-1-live"), 0o700); err != nil {
		t.Fatal(err)
	}

	// Act & Assert
	for _, name := range []string{"", ".", "../elsewhere", "nested/name"} {
		if err := credentials.Discard(name); err == nil {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
	if _, err := os.Stat(filepath.Join(credentials.root, "gm-1-live")); err != nil {
		t.Fatalf("expected the credentials root to remain untouched: %v", err)
	}
}
