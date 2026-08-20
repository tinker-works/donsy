package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testModel = "anthropic/claude-sonnet"

func writeAuthFile(t *testing.T, root, contents string) string {
	t.Helper()
	authPath := filepath.Join(root, "host", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return authPath
}

func TestCredentials_OpenCodeMount_ShouldRejectMissingAuthFile(t *testing.T) {
	// Arrange
	root := t.TempDir()
	credentials := Credentials{
		root: filepath.Join(root, "guest"), authPath: filepath.Join(root, "host", "auth.json"),
	}

	// Act
	_, err := credentials.OpenCodeMount("sandbox-1", testModel)

	// Assert
	if err == nil {
		t.Fatal("expected a missing auth file to be rejected")
	}
}

func TestCredentials_OpenCodeMount_ShouldScopeMountPerSandbox(t *testing.T) {
	// Arrange
	root := t.TempDir()
	authPath := writeAuthFile(t, root, `{"anthropic":{"type":"api","key":"secret"}}`)
	credentials := Credentials{root: filepath.Join(root, "guest"), authPath: authPath}

	// Act
	first, err := credentials.OpenCodeMount("sandbox-1", testModel)
	if err != nil {
		t.Fatal(err)
	}
	second, err := credentials.OpenCodeMount("sandbox-2", testModel)
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if first.HostLocation == second.HostLocation {
		t.Fatalf(
			"expected distinct sandboxes to get distinct credential mounts, both got %q",
			first.HostLocation,
		)
	}
}

func TestCredentials_OpenCodeMount_ShouldCopyOnlyTheModelsProvider(t *testing.T) {
	// The host auth file holds every provider ever logged into; the guest must
	// only ever see the one its run needs. An OAuth entry also has to arrive
	// with every field intact — OpenCode reads it back with a strict schema.
	// Arrange
	root := t.TempDir()
	authPath := writeAuthFile(t, root, `{
		"anthropic": {"type": "oauth", "refresh": "r-1", "access": "a-1", "expires": 1765000000000},
		"openai": {"type": "api", "key": "sk-other"}
	}`)
	credentials := Credentials{root: filepath.Join(root, "guest"), authPath: authPath}

	// Act
	mount, err := credentials.OpenCodeMount("sandbox-1", testModel)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if mount.GuestLocation != guestCredentialsPath || mount.Writable {
		t.Fatalf("unexpected mount: %#v", mount)
	}
	contents, err := os.ReadFile(filepath.Join(mount.HostLocation, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"anthropic":{"type":"oauth","refresh":"r-1","access":"a-1","expires":1765000000000}}`
	if string(contents) != want {
		t.Fatalf("unexpected credential contents: %q", contents)
	}
}

func TestCredentials_OpenCodeMount_ShouldRejectUnknownProviderWithoutLeakingKeys(t *testing.T) {
	// Arrange
	root := t.TempDir()
	authPath := writeAuthFile(t, root, `{"openai":{"type":"api","key":"sk-secret"}}`)
	credentials := Credentials{root: filepath.Join(root, "guest"), authPath: authPath}

	// Act
	_, err := credentials.OpenCodeMount("sandbox-1", testModel)

	// Assert
	if err == nil {
		t.Fatal("expected a provider without credentials to be rejected")
	}
	if !strings.Contains(err.Error(), `"anthropic"`) || !strings.Contains(err.Error(), "openai") {
		t.Fatalf("error should name the missing and configured providers: %v", err)
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("error leaked a credential value: %v", err)
	}
}

func TestCredentials_OpenCodeMount_ShouldRejectModelWithoutProvider(t *testing.T) {
	// Arrange
	root := t.TempDir()
	authPath := writeAuthFile(t, root, `{"anthropic":{"type":"api","key":"secret"}}`)
	credentials := Credentials{root: filepath.Join(root, "guest"), authPath: authPath}

	// Act
	_, err := credentials.OpenCodeMount("sandbox-1", "claude-sonnet")

	// Assert
	if err == nil {
		t.Fatal("expected a model without a provider prefix to be rejected")
	}
}

func TestNewCredentials_ShouldStageBelowTheGivenRootAndReadTheHostsAuthFile(t *testing.T) {
	// Arrange: a missing home directory has to fail where the cause is still
	// visible, so the location is resolved once here rather than swallowed.
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()

	// Act
	credentials, err := NewCredentials(root)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if credentials.root != filepath.Join(root, "credentials") {
		t.Fatalf("expected the staging directory under the root, got %q", credentials.root)
	}
	want := filepath.Join(home, ".local", "share", "opencode", "auth.json")
	if credentials.authPath != want {
		t.Fatalf("expected the host auth file at %q, got %q", want, credentials.authPath)
	}
}

func TestCredentials_OpenCodeMount_ShouldReportADirectoryItCannotCreate(t *testing.T) {
	// Arrange: a staging root whose parent is a file cannot hold a mount.
	root := t.TempDir()
	authPath := writeAuthFile(t, root,
		`{"anthropic":{"type":"api","key":"secret"}}`)
	blocked := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	credentials := Credentials{root: blocked, authPath: authPath}

	// Act
	_, err := credentials.OpenCodeMount("gm-sandbox", testModel)

	// Assert
	if err == nil {
		t.Fatal("expected the unusable staging directory to be reported")
	}
}
