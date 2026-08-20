package usecases

import (
	"strings"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

func TestIssueSandboxSpec_ShouldBeStablePerIssueAndRole(t *testing.T) {
	// Arrange: coding runs in continue mode, so the same issue must land on
	// the same sandbox every round or the session is lost.
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)

	// Act
	first := IssueSandboxSpec(1, "issue-1", agent.AgentRoleCoding, now)
	second := IssueSandboxSpec(1, "issue-1", agent.AgentRoleCoding, later)
	otherRole := IssueSandboxSpec(1, "issue-1", agent.AgentRolePRReviewer, now)
	otherIssue := IssueSandboxSpec(1, "issue-2", agent.AgentRoleCoding, now)

	// Assert
	if first.Sandbox.Name != second.Sandbox.Name {
		t.Fatalf("expected a stable name, got %q and %q", first.Sandbox.Name, second.Sandbox.Name)
	}
	if first.Sandbox.Name == otherRole.Sandbox.Name || first.Sandbox.Name == otherIssue.Sandbox.Name {
		t.Fatal("expected role and issue to change the sandbox identity")
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestIssueSandboxSpec_ShouldUseAnIssueScopedSubject(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)

	// Act
	spec := IssueSandboxSpec(1, "issue-1", agent.AgentRoleCoding, now)

	// Assert: the run registry keys liveness off this, so an issue round must
	// not claim the epic's subject.
	want := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	if spec.Sandbox.Subject != want {
		t.Fatalf("unexpected subject: %+v", spec.Sandbox.Subject)
	}
}

func TestSandboxSpecs_ShouldGrantDockerOnlyToVerificationRoles(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)

	// Act & Assert
	for _, test := range []struct {
		name string
		spec agent_runtime.SandboxSpec
		want bool
	}{
		{"epic refiner", EpicSandboxSpec(1, "epic-1", agent.AgentRoleRefiner, now), true},
		{"epic reviewer", EpicSandboxSpec(1, "epic-1", agent.AgentRoleIssueReviewer, now), true},
		{"issue coding", IssueSandboxSpec(1, "issue-1", agent.AgentRoleCoding, now), true},
		{"pull request reviewer", IssueSandboxSpec(1, "issue-1", agent.AgentRolePRReviewer, now), true},
		{"merge", IssueSandboxSpec(1, "issue-1", agent.AgentRoleMerge, now), false},
	} {
		if test.spec.InstallDocker != test.want {
			t.Fatalf("%s: InstallDocker = %v, want %v", test.name, test.spec.InstallDocker, test.want)
		}
	}
}

func TestIssueSandboxSpec_ShouldNotCollideWithAnEpicSandbox(t *testing.T) {
	// Arrange: an epic and an issue can share an ID space, and both specs
	// hash (projectID, subjectID, role) — only the role keeps them apart, so
	// check the one case where roles could overlap.
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)

	// Act
	epicSpec := EpicSandboxSpec(1, "shared-id", agent.AgentRoleRefiner, now)
	issueSpec := IssueSandboxSpec(1, "shared-id", agent.AgentRoleCoding, now)

	// Assert
	if epicSpec.Sandbox.Name == issueSpec.Sandbox.Name {
		t.Fatal("expected distinct sandbox names")
	}
}

// A name is also the container's hostname, which is capped at 63 octets, and a
// full subject ULID plus a role as long as "issue-reviewer" goes over on its
// own. The failure that shape produces is the reason to test it: the long role
// could never get a sandbox while "refiner", seven characters shorter, fit and
// looked fine.
func TestSandboxSpec_ShouldKeepNamesInsideTheNameLimit(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	roles := []agent.AgentRole{
		agent.AgentRoleRefiner, agent.AgentRoleIssueReviewer,
		agent.AgentRoleCoding, agent.AgentRolePRReviewer,
	}
	// Epic and issue subjects are distinct ULIDs, so uniqueness only has to hold
	// per subject across roles — which is what truncating the subject could break.
	for kind, spec := range map[string]func(agent.AgentRole) string{
		"epic": func(role agent.AgentRole) string {
			return EpicSandboxSpec(1, "01KZX8RF797REMYFTZFB5BC03P", role, now).Sandbox.Name
		},
		"issue": func(role agent.AgentRole) string {
			return IssueSandboxSpec(1, "01KZXEYMHB2W82Q722EVWEJ82S", role, now).Sandbox.Name
		},
	} {
		seen := map[string]agent.AgentRole{}
		for _, role := range roles {
			name := spec(role)
			if len(name) > agent_runtime.MaxSandboxNameLength {
				t.Fatalf("%s %s: name %q is %d characters, over the %d limit",
					kind, role, name, len(name), agent_runtime.MaxSandboxNameLength)
			}
			// The role has to stay legible: `docker ps` is how a stuck sandbox is
			// found, and a name of only hashes makes that guesswork.
			if !strings.Contains(name, sandboxNameSlug(string(role))) {
				t.Fatalf("expected %q to name its role %q", name, role)
			}
			if other, clash := seen[name]; clash {
				t.Fatalf("%s: name %q collides between %q and %q", kind, name, other, role)
			}
			seen[name] = role
		}
	}
}
