package usecases

import (
	"crypto/sha256"
	"fmt"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"strings"
	"time"
)

// A sandbox runs one OpenCode CLI process in an Ubuntu container, so it costs
// what it uses rather than reserving a guest. Every one is admitted against its
// project's own host budget, and headroom nothing uses is headroom the next
// round cannot have.
//
// EpicSandboxSpec returns the stable sandbox identity for one epic role. Stable identities keep
// OpenCode's database on the refiner sandbox, which makes later refinement runs resumable.
func EpicSandboxSpec(
	projectID uint,
	epicID string,
	role agent.AgentRole,
	now time.Time,
) agent_runtime.SandboxSpec {
	name := epicSandboxName(projectID, epicID, role)
	return agent_runtime.SandboxSpec{
		Sandbox: agent.Sandbox{
			ID:        "sandbox-" + name,
			ProjectID: projectID,
			Name:      name,
			Role:      role,
			Subject:   agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: epicID},
			Status:    agent.SandboxStatusCreating,
			CreatedAt: now,
			UpdatedAt: now,
		},
		InstallDocker: requiresDocker(role),
	}
}

// IssueSandboxSpec returns the stable sandbox identity for one issue role. Coding and
// pull request review both keep their OpenCode session across rounds
// (SessionModeFor puts them in continue mode), so the identity has to be stable
// per issue and role, not per round.
func IssueSandboxSpec(
	projectID uint,
	issueID string,
	role agent.AgentRole,
	now time.Time,
) agent_runtime.SandboxSpec {
	name := subjectSandboxName(projectID, issueID, role)
	return agent_runtime.SandboxSpec{
		Sandbox: agent.Sandbox{
			ID:        "sandbox-" + name,
			ProjectID: projectID,
			Name:      name,
			Role:      role,
			Subject:   agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: issueID},
			Status:    agent.SandboxStatusCreating,
			CreatedAt: now,
			UpdatedAt: now,
		},
		InstallDocker: requiresDocker(role),
	}
}

// requiresDocker grants daemon access only to roles that verify repository work.
// Merge updates branches through the host and runs no repository tests.
func requiresDocker(role agent.AgentRole) bool {
	switch role {
	case agent.AgentRoleRefiner, agent.AgentRoleIssueReviewer,
		agent.AgentRoleCoding, agent.AgentRolePRReviewer:
		return true
	default:
		return false
	}
}

func epicSandboxName(projectID uint, epicID string, role agent.AgentRole) string {
	return subjectSandboxName(projectID, epicID, role)
}

// sandboxLayoutVersion changes every sandbox's name, which retires the existing
// ones through reconciliation and builds their replacements from scratch.
//
// It is the blunt instrument, for a change of runtime or of identity. The
// everyday case it used to cover — a sandbox built against mounts or sizes that
// have since moved — is handled by the runtime instead, which fingerprints the
// spec and recreates the container when it no longer matches. Bump this only
// when the name itself has to change.
const sandboxLayoutVersion = 5

// subjectSandboxName keeps the name within the port's MaxSandboxNameLength. The digest
// is what makes the name unique, so only the readable part is shortened.
func subjectSandboxName(projectID uint, subjectID string, role agent.AgentRole) string {
	identity := fmt.Sprintf("%d-%s-%s-v%d", projectID, subjectID, role, sandboxLayoutVersion)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))[:8]
	head := fmt.Sprintf("gm-%d-", projectID)
	tail := "-" + sandboxNameSlug(string(role)) + "-" + digest
	subject := sandboxNameSlug(subjectID)
	if budget := agent_runtime.MaxSandboxNameLength - len(head) - len(tail); budget < len(subject) {
		subject = subject[:max(0, budget)]
	}
	return strings.Trim(head+subject+tail, "-")
}

func sandboxNameSlug(value string) string {
	return strings.Trim(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, strings.ToLower(value)), "-")
}
