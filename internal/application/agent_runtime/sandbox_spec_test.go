package agent_runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain/agent"
)

// spec is the smallest sandbox contract that validates, so each case below can break
// exactly one rule.
func spec() SandboxSpec {
	moment := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	return SandboxSpec{
		Sandbox: agent.Sandbox{
			ID: "sandbox-1", ProjectID: 1, Name: "acme-coding-cart", Role: agent.AgentRoleCoding,
			Subject:   agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "cart"},
			Status:    agent.SandboxStatusStopped,
			CreatedAt: moment, UpdatedAt: moment,
		},
		Mounts: []SandboxMount{
			{HostLocation: "/host/code", GuestLocation: "/work", Writable: true},
			{HostLocation: "/host/creds", GuestLocation: "/run/go-merge/credentials"},
		},
	}
}

func TestSandboxSpec_Validate_ShouldAcceptACompleteSpec(t *testing.T) {
	// Arrange & Act & Assert
	if err := spec().Validate(); err != nil {
		t.Fatalf("expected the baseline spec to validate: %v", err)
	}
}

func TestSandboxSpec_Validate_ShouldRefuseAnInvalidSandbox(t *testing.T) {
	// Arrange
	s := spec()
	s.Sandbox.Name = ""

	// Act & Assert
	if err := s.Validate(); err == nil {
		t.Fatal("expected an invalid sandbox to be refused")
	}
}

func TestSandboxSpec_Validate_ShouldRefuseANameTheStrictestProviderWouldReject(t *testing.T) {
	// Arrange: the name becomes the container's hostname, which the runtime caps,
	// so the bound is enforced here rather than surfacing as a provider error
	// minutes into a round.
	s := spec()
	s.Sandbox.Name = strings.Repeat("a", MaxSandboxNameLength+1)

	// Act
	err := s.Validate()

	// Assert
	if err == nil {
		t.Fatalf("expected a name of %d characters to be refused", len(s.Sandbox.Name))
	}
	s.Sandbox.Name = strings.Repeat("a", MaxSandboxNameLength)
	if err := s.Validate(); err != nil {
		t.Fatalf("expected a name at the limit to be accepted: %v", err)
	}
}

func TestSandboxSpec_Validate_ShouldRefuseAnInvalidMount(t *testing.T) {
	// Arrange
	s := spec()
	s.Mounts = []SandboxMount{{HostLocation: "relative", GuestLocation: "/work"}}

	// Act & Assert
	if err := s.Validate(); err == nil {
		t.Fatal("expected a relative mount to be refused")
	}
}

func TestSandboxSpec_Validate_ShouldAcceptMultipleWritableMounts(t *testing.T) {
	// Arrange: an epic can verify several scoped repositories in one round.
	s := spec()
	s.Mounts = append(s.Mounts, SandboxMount{
		HostLocation: "/host/other", GuestLocation: "/other", Writable: true,
	})

	// Act
	err := s.Validate()

	// Assert
	if err != nil {
		t.Fatalf("expected writable repository mounts to validate: %v", err)
	}
}

func TestSandboxSpec_Validate_ShouldAcceptASpecWithNoMounts(t *testing.T) {
	// Arrange: a drafting round has no code to mount.
	s := spec()
	s.Mounts = nil

	// Act & Assert
	if err := s.Validate(); err != nil {
		t.Fatalf("expected a mountless spec to validate: %v", err)
	}
}

func TestSandboxMount_Validate_ShouldRequireBothSidesAbsolute(t *testing.T) {
	// Arrange: a relative path resolves against whatever directory the provider
	// happens to run in.
	cases := map[string]SandboxMount{
		"relative host":  {HostLocation: "code", GuestLocation: "/work"},
		"relative guest": {HostLocation: "/host/code", GuestLocation: "work"},
	}

	// Act & Assert
	for name, mount := range cases {
		if err := mount.Validate(); err == nil {
			t.Fatalf("expected %q to be refused", name)
		}
	}
	absolute := SandboxMount{HostLocation: "/host/code", GuestLocation: "/work"}
	if err := absolute.Validate(); err != nil {
		t.Fatalf("expected an absolute mount to validate: %v", err)
	}
}

func TestErrMergeConflict_ShouldBeANormalOutcomeCallersCanRecognise(t *testing.T) {
	// Arrange: the caller sends the pull request back for another coding round
	// rather than treating this as a failure, so it has to be matchable.

	// Act & Assert
	if ErrMergeConflict == nil || ErrMergeConflict.Error() == "" {
		t.Fatal("expected a named merge-conflict error")
	}
}
