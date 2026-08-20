package opencode

import (
	"encoding/json"
	"fmt"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const guestCredentialsPath = "/run/go-merge/credentials"

// Credentials stages the OpenCode credential a run needs into a per-sandbox read-only
// mount. The host auth file holds every provider ever logged into; only the entry
// for the provider the run's model names is copied, so code executing in the guest
// — the agent, or anything a build's dependency tree runs — can reach one key at
// most, never the whole store.
type Credentials struct {
	root     string
	authPath string
}

func NewCredentials(root string) (Credentials, error) {
	// A missing home directory has to fail here, where the cause is still
	// visible. Swallowed into an empty string it resurfaces later as a
	// baffling "auth.json not found" against a relative path.
	home, err := os.UserHomeDir()
	if err != nil {
		return Credentials{}, fmt.Errorf("resolve OpenCode auth location: %w", err)
	}
	return Credentials{
		root:     filepath.Join(root, "credentials"),
		authPath: filepath.Join(home, ".local", "share", "opencode", "auth.json"),
	}, nil
}

// Discard removes a sandbox's staged credentials. The directory is named after the sandbox,
// so a reclaimed sandbox otherwise leaves a real provider credential on disk for an
// instance that no longer exists — one more copy of a secret than anything needs,
// and the only one nobody is watching.
//
// A directory that is already gone is not an error: reclaim is retried, and the
// second attempt must not report a failure for work the first one finished.
func (c Credentials) Discard(sandboxName string) error {
	if sandboxName == "" {
		return fmt.Errorf("sandbox name is required to discard its credentials")
	}
	if err := validateSandboxName(sandboxName); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(c.root, sandboxName))
}

func (c Credentials) OpenCodeMount(sandboxName, model string) (agent_runtime.SandboxMount, error) {
	if sandboxName == "" {
		return agent_runtime.SandboxMount{}, fmt.Errorf("sandbox name is required to stage credentials")
	}
	if err := validateSandboxName(sandboxName); err != nil {
		return agent_runtime.SandboxMount{}, err
	}
	contents, err := os.ReadFile(c.authPath)
	if err != nil {
		return agent_runtime.SandboxMount{}, fmt.Errorf("read OpenCode auth file: %w", err)
	}
	scoped, err := scopeToProvider(contents, model)
	if err != nil {
		return agent_runtime.SandboxMount{}, err
	}
	directory := filepath.Join(c.root, sandboxName)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return agent_runtime.SandboxMount{}, err
	}
	path := filepath.Join(directory, "auth.json")
	if err := os.WriteFile(path, scoped, 0o600); err != nil {
		return agent_runtime.SandboxMount{}, err
	}
	return agent_runtime.SandboxMount{
		HostLocation: directory, GuestLocation: guestCredentialsPath,
	}, nil
}

func validateSandboxName(sandboxName string) error {
	if strings.ContainsAny(sandboxName, `/\`) || strings.Contains(sandboxName, "..") {
		return fmt.Errorf("sandbox name %q is not a bare directory name", sandboxName)
	}
	return nil
}

// scopeToProvider reduces the host auth store to the one entry the run's model
// needs. The auth file is a flat {providerID: entry} object and the model is
// "provider/model", so the provider ID is everything before the first slash.
// Entries are kept as raw JSON: OpenCode stores api, oauth and wellknown shapes
// here, and the selected one must pass through with every field intact rather
// than being re-modelled into whatever subset this adapter knows about.
func scopeToProvider(contents []byte, model string) ([]byte, error) {
	provider, _, found := strings.Cut(model, "/")
	if !found || provider == "" {
		return nil, fmt.Errorf(
			"agent model %q does not name a provider: expected \"provider/model\"", model,
		)
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(contents, &entries); err != nil {
		return nil, fmt.Errorf("parse OpenCode auth file: %w", err)
	}
	entry, ok := entries[provider]
	if !ok {
		// Provider IDs are safe to name in an error; entry values never are.
		return nil, fmt.Errorf(
			"no OpenCode credentials for provider %q (configured: %s): "+
				"run \"opencode auth login\" on the host",
			provider, strings.Join(slices.Sorted(maps.Keys(entries)), ", "),
		)
	}
	return json.Marshal(map[string]json.RawMessage{provider: entry})
}

var _ agent_runtime.AgentCredentials = Credentials{}
