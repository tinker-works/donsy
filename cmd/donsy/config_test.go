package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tinker-works/donsy/internal/adapters/instancelock"
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
		"http://localhost:8337/path", "http://localhost:8337?query", "http://localhost:8337#fragment",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := parseEndpoint(input); err == nil {
				t.Fatalf("parseEndpoint(%q) succeeded", input)
			}
		})
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
