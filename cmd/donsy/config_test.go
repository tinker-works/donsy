package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/tinker-works/donsy/internal/adapters/instancelock"
	"github.com/tinker-works/donsy/internal/adapters/projectstore"
	"golang.org/x/sys/unix"
)

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "localhost", input: "http://LOCALHOST:08337/", want: "http://localhost:8337"},
		{name: "IPv4", input: "http://127.0.0.1:0", want: "http://127.0.0.1:0"},
		{name: "IPv6", input: "http://[::1]:8337", want: "http://[::1]:8337"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			endpoint, err := parseEndpoint(test.input)
			if err != nil {
				t.Fatalf("parseEndpoint() error = %v", err)
			}
			if got := endpoint.String(); got != test.want {
				t.Fatalf("parseEndpoint() = %q, want %q", got, test.want)
			}
		})
	}

	for _, input := range []string{
		"https://localhost:8337", "http://example.com:8337", "http://127.0.0.1",
		"http://127.0.0.1:bad", "http://127.0.0.1:65536", "http://user@localhost:8337",
		"http://localhost:8337/path", "http://localhost:8337?query", "http://localhost:8337?", "http://localhost:8337#fragment", "http://localhost:8337#",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := parseEndpoint(input); err == nil {
				t.Fatalf("parseEndpoint(%q) succeeded", input)
			}
		})
	}
}

func TestRenameNoReplaceKeepsExistingDestination(t *testing.T) {
	directory := t.TempDir()
	from := filepath.Join(directory, "from")
	to := filepath.Join(directory, "to")
	if err := os.Mkdir(from, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(to, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(to, "keep"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := renameNoReplace(from, to)
	if !errors.Is(err, unix.EEXIST) {
		t.Fatalf("renameNoReplace() error = %v, want EEXIST", err)
	}
	for _, path := range []string{from, filepath.Join(to, "keep")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%q was changed: %v", path, err)
		}
	}
}

func TestPrepareRootMigratesLegacyRootAndRetainsLock(t *testing.T) {
	configDir := t.TempDir()
	legacy := filepath.Join(configDir, legacyRootName)
	if err := os.MkdirAll(filepath.Join(legacy, "stores", "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"state.db", "state.db-wal", "state.db-shm", "stores/project/store.sqlite-wal"} {
		if err := os.WriteFile(filepath.Join(legacy, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	root, lock, err := prepareRoot(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if root != filepath.Join(configDir, rootName) {
		t.Fatalf("root = %q", root)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy root still exists: %v", err)
	}
	for _, name := range []string{"state.db", "state.db-wal", "state.db-shm", "stores/project/store.sqlite-wal"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("migrated %q: %v", name, err)
		}
	}
	if _, err := instancelock.Acquire(filepath.Join(root, "go-merge.lock")); err == nil {
		t.Fatal("second lock acquisition succeeded while migrated handle is held")
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := instancelock.Acquire(filepath.Join(root, "go-merge.lock"))
	if err != nil {
		t.Fatalf("lock acquisition after release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareRootMigratesReadableLegacyDataAndConnectionFiles(t *testing.T) {
	configDir := t.TempDir()
	legacy := filepath.Join(configDir, legacyRootName)
	if err := os.MkdirAll(filepath.Join(legacy, "stores", "projects"), 0o700); err != nil {
		t.Fatal(err)
	}
	copyLegacyFixture(t, "state.db", filepath.Join(legacy, "state.db"))
	copyLegacyFixture(t, "store.sqlite", filepath.Join(legacy, "stores", "projects", "legacy.sqlite"))
	for _, name := range []string{"state.db-wal", "state.db-shm", "stores/projects/legacy.sqlite-wal", "stores/projects/legacy.sqlite-shm"} {
		if err := os.WriteFile(filepath.Join(legacy, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacy, tokenFileName), []byte("legacy-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, endpointFileName), []byte("http://localhost:9123"), 0o600); err != nil {
		t.Fatal(err)
	}

	root, lock, err := prepareRoot(configDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	for _, name := range []string{"state.db-wal", "state.db-shm", "stores/projects/legacy.sqlite-wal", "stores/projects/legacy.sqlite-shm"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("migrated sidecar %q: %v", name, err)
		}
	}

	registry, err := projectstore.Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	projects, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	sandboxes, err := registry.ListSandboxes(7)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := registry.ListProjectAgentRuns(7)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Name != "Legacy project" || len(sandboxes) != 1 || len(runs) != 2 {
		t.Fatalf("migrated state is unreadable: projects=%#v sandboxes=%#v runs=%#v", projects, sandboxes, runs)
	}
	store, err := projectstore.OpenStore(filepath.Join(root, "stores", "projects", "legacy.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.ReadProject()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if project.Name != "Legacy project" {
		t.Fatalf("migrated project store = %#v", project)
	}
	if token, err := configuredToken(root, "", false); err != nil || token != "legacy-token" {
		t.Fatalf("configuredToken() = %q, %v", token, err)
	}
	if endpoint, err := configuredEndpoint(root, "", false); err != nil || endpoint.String() != "http://localhost:9123" {
		t.Fatalf("configuredEndpoint() = %q, %v", endpoint.String(), err)
	}
}

func TestPrepareRootCreatesNewRootAndCanRestart(t *testing.T) {
	configDir := t.TempDir()
	root, lock, err := prepareRoot(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if root != filepath.Join(configDir, rootName) {
		t.Fatalf("root = %q", root)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("root mode = %o, want 700", info.Mode().Perm())
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	secondRoot, secondLock, err := prepareRoot(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if secondRoot != root {
		t.Fatalf("second root = %q, want %q", secondRoot, root)
	}
	if err := secondLock.Release(); err != nil {
		t.Fatal(err)
	}
}

func copyLegacyFixture(t *testing.T, name, destination string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "internal", "adapters", "projectstore", "testdata", "legacy", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareRootRefusesIncompleteMigration(t *testing.T) {
	for _, roots := range [][]string{{legacyRootName, rootName}, {stagingRootName}} {
		t.Run(roots[0], func(t *testing.T) {
			root := t.TempDir()
			for _, name := range roots {
				if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := prepareRoot(root); err == nil {
				t.Fatal("prepareRoot() succeeded")
			}
		})
	}
}

func TestConfiguredTokenRegeneratesOnlyEmptyDaemonToken(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, tokenFileName)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	token, err := configuredToken(root, "", false)
	if err != nil || token == "" {
		t.Fatalf("configuredToken() = %q, %v", token, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token mode = %o, want 600", info.Mode().Perm())
	}
	if got, err := configuredToken(root, "override", true); err != nil || got != "override" {
		t.Fatalf("configuredToken(flag) = %q, %v", got, err)
	}
	if _, err := configuredToken(root, "", true); err == nil {
		t.Fatal("configuredToken(empty flag) succeeded")
	}
}

func TestConfiguredEndpointUsesFlagAndPersistsEphemeralListener(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, endpointFileName), []byte("http://localhost:8337"), 0o600); err != nil {
		t.Fatal(err)
	}
	selected, err := configuredEndpoint(root, "http://127.0.0.1:0", true)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", selected.listenAddress())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	selected, err = selected.withListener(listener)
	if err != nil {
		t.Fatal(err)
	}
	if selected.port == 0 {
		t.Fatal("listener did not assign an ephemeral port")
	}
	if err := persistEndpoint(root, selected); err != nil {
		t.Fatal(err)
	}
	persisted, err := configuredEndpoint(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.String() != selected.String() {
		t.Fatalf("persisted endpoint = %q, want %q", persisted, selected)
	}
	info, err := os.Stat(filepath.Join(root, endpointFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("endpoint mode = %o, want 600", info.Mode().Perm())
	}
}
