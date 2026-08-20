package projectstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

func TestOpenStore_ShouldReadPopulatedLegacyProjectStoreBeforeWriting(t *testing.T) {
	// Arrange
	store, err := OpenStore(copyFixture(t, "store.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})

	// Act
	project, projectErr := store.ReadProject()
	settings, settingsErr := store.AgentSettings()
	override, overrideErr := store.RepositorySettings("acme/api")
	projectScript, projectScriptErr := store.ReadFile("agents/scripts/project.sh")
	apiScript, apiScriptErr := store.ReadFile("agents/scripts/api.sh")
	aggregate, epicErr := store.ReadEpic("legacy-epic")
	aggregates, epicsErr := store.ListEpics()

	// Assert
	for _, err := range []error{
		projectErr, settingsErr, overrideErr, projectScriptErr, apiScriptErr, epicErr, epicsErr,
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if project.Name != "Legacy project" || len(project.Repositories) != 2 ||
		project.Repositories[0] != "acme/api" || project.Repositories[1] != "acme/web" {
		t.Fatalf("unexpected project: %#v", project)
	}
	if settings.SetupScript != "agents/scripts/project.sh" ||
		settings.Roles[agent.AgentRoleCoding].Agent != "legacy-coder" {
		t.Fatalf("unexpected settings: %#v", settings)
	}
	if override.SetupScript != "agents/scripts/api.sh" ||
		override.Roles[agent.AgentRolePRReviewer].Variant != "fast" {
		t.Fatalf("unexpected override: %#v", override)
	}
	if projectScript != "#!/bin/sh\necho project\n" || apiScript != "#!/bin/sh\necho api\n" {
		t.Fatalf("unexpected scripts: %q %q", projectScript, apiScript)
	}
	if len(aggregate.Issues) != 2 || len(aggregate.PullRequests) != 1 ||
		aggregate.Issues[1].Comments[0].Body != "issue comment" ||
		aggregate.PullRequests[0].Comments[0].Body != "pull request comment" {
		t.Fatalf("unexpected aggregate: %#v", aggregate)
	}
	if len(aggregates) != 1 || aggregates[0].ID != "legacy-epic" {
		t.Fatalf("unexpected aggregates: %#v", aggregates)
	}
}

func TestStore_ShouldPersistSettingsAndEpicAcrossReopen(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "store.sqlite")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := agent.AgentSettings{
		SetupScript: "agents/scripts/project.sh",
		Roles: map[agent.AgentRole]agent.AgentProfile{
			agent.AgentRoleCoding: {Agent: "coder", Variant: "high", MaxRounds: 3},
		},
	}
	aggregate := testEpic("epic-1")

	// Act
	if err := store.WriteProject(Project{Name: "Project", Repositories: []string{"acme/web", "acme/api"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteAgentSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteFile("agents/scripts/project.sh", "#!/bin/sh\n"); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteEpic(aggregate); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	gotProject, err := store.ReadProject()
	if err != nil {
		t.Fatal(err)
	}
	gotSettings, err := store.AgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	gotEpic, err := store.ReadEpic(aggregate.ID)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if gotProject.Name != "Project" || len(gotProject.Repositories) != 2 {
		t.Fatalf("unexpected project: %#v", gotProject)
	}
	if gotSettings.Roles[agent.AgentRoleCoding].Agent != "coder" {
		t.Fatalf("unexpected settings: %#v", gotSettings)
	}
	if gotEpic.ID != aggregate.ID || len(gotEpic.Issues) != 2 {
		t.Fatalf("unexpected epic: %#v", gotEpic)
	}
}

func TestStore_ShouldRejectInvalidWritesWithoutChangingData(t *testing.T) {
	// Arrange
	store, err := OpenStore(filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := store.WriteProject(Project{Name: "Project"}); err != nil {
		t.Fatal(err)
	}

	// Act
	projectErr := store.WriteProject(Project{Name: "Project", Repositories: []string{"not-a-repository"}})
	pathErr := store.WriteFile("../outside", "unsafe")
	_, missingErr := store.ReadFile("agents/scripts/missing.sh")

	// Assert
	if projectErr == nil || pathErr == nil || !errors.Is(missingErr, os.ErrNotExist) {
		t.Fatalf("unexpected validation results: %v %v %v", projectErr, pathErr, missingErr)
	}
	project, err := store.ReadProject()
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != "Project" || len(project.Repositories) != 0 {
		t.Fatalf("invalid write changed project: %#v", project)
	}
}

func testEpic(id string) epic.Epic {
	created := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	return epic.Epic{
		ID: id, Title: "Epic", Assignee: "octocat", Repositories: []string{"acme/api"},
		Body: "body", State: epic.EpicStateReady, BranchPrefix: "legacy-123",
		Issues: []epic.Issue{
			{ID: id + "-root", Title: "Root", State: epic.IssueStateOpen, CreatedAt: created, Body: "root"},
			{ID: id + "-issue", Title: "Issue", ParentID: id + "-root", Repository: "acme/api",
				State: epic.IssueStatePR, CreatedAt: created.Add(time.Minute), Body: "issue"},
		},
		PullRequests: []epic.PullRequest{{
			ID: id + "-pr", IssueID: id + "-issue", Title: "PR", Status: epic.PullRequestOpen,
			Repository: "acme/api", Number: 1, URL: "https://example.test/pr/1", Head: "head", Base: "main",
			ReviewedHead: "head", ReviewedBase: "main", Rounds: 1, Reviews: 1, CodingRounds: 1,
			Approved: true, CreatedAt: created.Add(2 * time.Minute),
		}},
	}
}
