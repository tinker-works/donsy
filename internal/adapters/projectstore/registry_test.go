package projectstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

func TestOpen_ShouldReadPopulatedLegacyRegistryBeforeWriting(t *testing.T) {
	// Arrange
	path := copyFixture(t, "state.db")
	registry, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Error(err)
		}
	})

	// Act
	projects, projectErr := registry.List()
	organisations, organisationErr := registry.ListOrganisations()
	repositories, repositoryErr := registry.ListRepositories()
	sandboxes, sandboxErr := registry.ListSandboxes(7)
	runs, runErr := registry.ListProjectAgentRuns(7)

	// Assert
	for _, err := range []error{
		projectErr, organisationErr, repositoryErr, sandboxErr, runErr,
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(projects) != 1 || projects[0].ID != 7 || projects[0].Name != "Legacy project" ||
		!projects[0].LastOpenedAt.Equal(time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected projects: %#v", projects)
	}
	if len(organisations) != 1 || organisations[0].Name != "acme" {
		t.Fatalf("unexpected organisations: %#v", organisations)
	}
	if len(repositories) != 1 || repositories[0].FullName != "acme/api" {
		t.Fatalf("unexpected repositories: %#v", repositories)
	}
	if len(sandboxes) != 1 || sandboxes[0].Status != agent.SandboxStatusRunning ||
		sandboxes[0].Subject.ID != "issue-legacy" {
		t.Fatalf("unexpected sandboxes: %#v", sandboxes)
	}
	if len(runs) != 2 {
		t.Fatalf("unexpected runs: %#v", runs)
	}
	if runs[0].ID != "run-complete" || runs[0].Status != agent.AgentRunStatusSucceeded ||
		runs[0].SessionMode != agent.SessionModeContinue || runs[0].SandboxID != "sandbox-legacy" ||
		runs[0].Agent != "pr-reviewer" || runs[0].Variant != "fast" ||
		runs[0].Usage.TokensIn != 12 || runs[0].Usage.TokensOut != 34 ||
		runs[0].Usage.CostUSD != 0.42 || runs[0].StartedAt == nil || runs[0].FinishedAt == nil {
		t.Fatalf("unexpected complete run: %#v", runs[0])
	}
	if runs[1].ID != "run-legacy" || runs[1].Status != agent.AgentRunStatusFailed ||
		runs[1].Error != "legacy agent error" || runs[1].SandboxID != "" || runs[1].Agent != "" ||
		runs[1].Variant != "" || runs[1].Usage.Reported() || runs[1].CreatedAt.IsZero() ||
		runs[1].StartedAt == nil || runs[1].FinishedAt == nil {
		t.Fatalf("unexpected legacy run: %#v", runs[1])
	}
}

func TestRegistry_ShouldPersistRuntimeAndProjectData(t *testing.T) {
	// Arrange
	registry, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	project := domainProject("Project")
	project.ID = 7
	sandbox := agent.Sandbox{
		ID: "sandbox-1", ProjectID: 7, Name: "project-coding-issue-1",
		Role:    agent.AgentRoleCoding,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
		Status:  agent.SandboxStatusStopped, CreatedAt: now, UpdatedAt: now,
	}
	run := agent.AgentRun{
		ID: "run-1", ProjectID: 7, SandboxID: sandbox.ID, Role: agent.AgentRoleCoding,
		Subject: sandbox.Subject, Engine: agent.AgentEngineOpenCode, Agent: "coding",
		SessionMode: agent.SessionModeFresh, Status: agent.AgentRunStatusSucceeded,
		Round: 1, Error: "", Usage: agent.RunUsage{TokensIn: 3, TokensOut: 5, CostUSD: 0.25},
		CreatedAt: now, StartedAt: &now, FinishedAt: &now,
	}

	// Act
	if err := registry.Create(&project); err != nil {
		t.Fatal(err)
	}
	if err := registry.SaveSandbox(sandbox); err != nil {
		t.Fatal(err)
	}
	if err := registry.SaveAgentRun(run); err != nil {
		t.Fatal(err)
	}
	gotSandboxes, err := registry.ListSandboxes(7)
	if err != nil {
		t.Fatal(err)
	}
	gotRuns, err := registry.ListAgentRuns(project.ID, sandbox.Subject)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(gotSandboxes) != 1 || gotSandboxes[0].ID != sandbox.ID {
		t.Fatalf("unexpected sandboxes: %#v", gotSandboxes)
	}
	if len(gotRuns) != 1 || gotRuns[0].Usage != run.Usage {
		t.Fatalf("unexpected runs: %#v", gotRuns)
	}
}

func domainProject(name string) domain.Project {
	return domain.Project{Name: name}
}

func copyFixture(t *testing.T, name string) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("testdata", "legacy", name))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
