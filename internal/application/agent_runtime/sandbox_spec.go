package agent_runtime

import (
	"fmt"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

// MaxSandboxNameLength is the longest name a SandboxManager must accept.
//
// The container runtime allows far more, but the name is also the container's
// hostname, which is capped at 63 octets. Enforcing it here rather than leaving
// it to the provider is what turns it into a validation error instead of a
// round that fails minutes in, on one role and not the others — a full subject
// ULID plus "issue-reviewer" goes over where the shorter roles fit.
const MaxSandboxNameLength = 63

// SandboxSpec is the application contract for one agent sandbox. How the definition is
// rendered and stored is the adapter's concern; the spec carries only what
// every provider needs to know.
type SandboxSpec struct {
	Sandbox       agent.Sandbox
	Mounts        []SandboxMount
	InstallDocker bool
	// SetupScript is the raw content (not a path) of a repository's setup
	// script, which the runtime runs as the last step of the image build.
	// Empty means no repository-specific customization.
	SetupScript string
}

func (s SandboxSpec) Validate() error {
	if err := s.Sandbox.Validate(); err != nil {
		return err
	}
	if len(s.Sandbox.Name) > MaxSandboxNameLength {
		return fmt.Errorf("sandbox name %q exceeds %d characters", s.Sandbox.Name, MaxSandboxNameLength)
	}
	for _, mount := range s.Mounts {
		if err := mount.Validate(); err != nil {
			return err
		}
	}
	return nil
}
