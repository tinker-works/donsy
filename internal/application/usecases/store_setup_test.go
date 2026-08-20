package usecases

import (
	"fmt"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

func configuredProject() domain.Project {
	return domain.Project{
		Name: "one",
	}
}

func TestStoreSetupUseCase_ShouldReportAFreshStoreAsIncomplete(t *testing.T) {
	// Arrange: what workspace.Clone leaves behind — a valid store with no roles
	// and no linked repositories — and no organisation registered.
	organisations := &fakeOrganisationRegistry{}
	factory := &fakeFactory{workspace: &fakeWorkspace{}}
	useCase := &StoreSetupUseCase{organisations: organisations, factory: factory}

	// Act
	state, err := useCase.Handle(configuredProject())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if state.Complete() {
		t.Fatalf("expected a fresh store to be incomplete, got %+v", state)
	}
	if !state.NeedsOrganisation() || !state.NeedsRepository() || !state.NeedsRoles() {
		t.Fatalf("expected every step outstanding, got %+v", state)
	}
	if state.RolesTotal != len(agent.Roles()) {
		t.Fatalf("expected %d roles counted, got %d", len(agent.Roles()), state.RolesTotal)
	}
}

func TestStoreSetupUseCase_ShouldReportAConfiguredStoreAsComplete(t *testing.T) {
	// Arrange
	organisations := &fakeOrganisationRegistry{
		organisations: []domain.Organisation{{Name: "acme"}},
	}
	settings := agent.AgentSettings{Roles: map[agent.AgentRole]agent.AgentProfile{}}
	for _, role := range agent.Roles() {
		settings.Roles[role] = agent.AgentProfile{Agent: "github-copilot/claude-sonnet-5"}
	}
	factory := &fakeFactory{workspace: &fakeWorkspace{
		repositories:  []string{"acme/widgets"},
		agentSettings: settings,
	}}
	useCase := &StoreSetupUseCase{organisations: organisations, factory: factory}

	// Act
	state, err := useCase.Handle(configuredProject())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !state.Complete() {
		t.Fatalf("expected a configured store to be complete, got %+v", state)
	}
}

func TestStoreSetupUseCase_ShouldReportPartiallyAssignedRolesAsOutstanding(t *testing.T) {
	// Arrange: only the first role is assigned, which is enough to start refining
	// and not enough to finish anything.
	organisations := &fakeOrganisationRegistry{
		organisations: []domain.Organisation{{Name: "acme"}},
	}
	factory := &fakeFactory{workspace: &fakeWorkspace{
		repositories: []string{"acme/widgets"},
		agentSettings: agent.AgentSettings{Roles: map[agent.AgentRole]agent.AgentProfile{
			agent.AgentRoleRefiner: {Agent: "github-copilot/claude-sonnet-5"},
		}},
	}}
	useCase := &StoreSetupUseCase{organisations: organisations, factory: factory}

	// Act
	state, err := useCase.Handle(configuredProject())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !state.NeedsRoles() || state.Complete() {
		t.Fatalf("expected 1 of %d roles to be outstanding, got %+v", len(agent.Roles()), state)
	}
}

func TestStoreSetupUseCase_ShouldOpenTheProjectsOwnWorkspace(t *testing.T) {
	// Arrange
	factory := &fakeFactory{workspace: &fakeWorkspace{}}
	useCase := &StoreSetupUseCase{organisations: &fakeOrganisationRegistry{}, factory: factory}

	// Act
	if _, err := useCase.Handle(configuredProject()); err != nil {
		t.Fatal(err)
	}

	// Assert
	if factory.openPath != "one" {
		t.Fatalf("expected the project's workspace opened, got %q", factory.openPath)
	}
}

func TestStoreSetupUseCase_ShouldRejectAMissingProjectName(t *testing.T) {
	// Arrange: a blank path would read whatever the factory resolves it to.
	useCase := &StoreSetupUseCase{
		organisations: &fakeOrganisationRegistry{},
		factory:       &fakeFactory{workspace: &fakeWorkspace{}},
	}

	// Act
	_, err := useCase.Handle(domain.Project{})

	// Assert
	if err == nil {
		t.Fatal("expected a project without a local path to be rejected")
	}
}

func TestStoreSetupUseCase_ShouldPropagateARegistryError(t *testing.T) {
	// Arrange
	organisations := &fakeOrganisationRegistry{listErr: fmt.Errorf("database locked")}
	useCase := &StoreSetupUseCase{
		organisations: organisations,
		factory:       &fakeFactory{workspace: &fakeWorkspace{}},
	}

	// Act
	_, err := useCase.Handle(configuredProject())

	// Assert
	if err == nil {
		t.Fatal("expected the registry error to propagate")
	}
}

func TestInitialiseStoreUseCase_ShouldAssignTheModelToEveryRole(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{}
	useCase := &InitialiseStoreUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(InitialiseStoreCommand{
		Project: configuredProject(),
		Model:   "github-copilot/claude-sonnet-5",
		Variant: "Default",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.agentSettings.Roles) != len(agent.Roles()) {
		t.Fatalf("expected every role assigned, got %+v", workspace.agentSettings.Roles)
	}
	for _, role := range agent.Roles() {
		profile, err := workspace.agentSettings.Profile(role)
		if err != nil {
			t.Fatalf("role %q: %v", role, err)
		}
		if profile.Agent != "github-copilot/claude-sonnet-5" || profile.Variant != "Default" {
			t.Fatalf("role %q got %#v", role, profile)
		}
	}
}

func TestInitialiseStoreUseCase_ShouldPreserveAnExistingMaxRounds(t *testing.T) {
	// Arrange: max_rounds is configured in agent.yaml by hand, so assigning a
	// model must not silently reset a role's round limit to unlimited.
	workspace := &fakeWorkspace{agentSettings: agent.AgentSettings{
		Roles: map[agent.AgentRole]agent.AgentProfile{
			agent.AgentRoleCoding: {Agent: "old/model", MaxRounds: 7},
		},
	}}
	useCase := &InitialiseStoreUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(InitialiseStoreCommand{
		Project: configuredProject(),
		Model:   "github-copilot/claude-sonnet-5",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	profile := workspace.agentSettings.Roles[agent.AgentRoleCoding]
	if profile.MaxRounds != 7 {
		t.Fatalf("expected max rounds preserved, got %#v", profile)
	}
	if profile.Agent != "github-copilot/claude-sonnet-5" {
		t.Fatalf("expected the model replaced, got %#v", profile)
	}
}

func TestInitialiseStoreUseCase_ShouldLinkTheSelectedRepositories(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{}
	useCase := &InitialiseStoreUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(InitialiseStoreCommand{
		Project:      configuredProject(),
		Model:        "github-copilot/claude-sonnet-5",
		Repositories: []string{"acme/widgets", "acme/gadgets"},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.repositories) != 2 || workspace.repositories[0] != "acme/widgets" {
		t.Fatalf("unexpected repositories: %+v", workspace.repositories)
	}
}

func TestInitialiseStoreUseCase_ShouldLeaveLinkedRepositoriesAloneWhenNoneSelected(t *testing.T) {
	// Arrange: an empty selection is "nothing chosen", not "clear the set" — the
	// setup screen can submit before the discovered pool has arrived.
	workspace := &fakeWorkspace{repositories: []string{"acme/widgets"}}
	useCase := &InitialiseStoreUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(InitialiseStoreCommand{
		Project: configuredProject(),
		Model:   "github-copilot/claude-sonnet-5",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.repositories) != 1 || workspace.repositories[0] != "acme/widgets" {
		t.Fatalf("expected the linked set untouched, got %+v", workspace.repositories)
	}
}

func TestInitialiseStoreUseCase_ShouldRejectABlankModel(t *testing.T) {
	// Arrange: a role with no model is exactly the state setup exists to fix, so
	// writing one back would defeat the gate.
	workspace := &fakeWorkspace{}
	useCase := &InitialiseStoreUseCase{factory: &fakeFactory{workspace: workspace}}

	for _, model := range []string{"", "   "} {
		t.Run(fmt.Sprintf("%q", model), func(t *testing.T) {
			// Act
			err := useCase.Handle(InitialiseStoreCommand{
				Project: configuredProject(), Model: model,
			})

			// Assert
			if err == nil {
				t.Fatalf("expected model %q to be rejected", model)
			}
		})
	}
	if len(workspace.agentSettings.Roles) != 0 {
		t.Fatalf("expected nothing written, got %+v", workspace.agentSettings.Roles)
	}
}

func TestInitialiseStoreUseCase_ShouldRejectAMissingProjectName(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{}
	useCase := &InitialiseStoreUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(InitialiseStoreCommand{
		Model: "github-copilot/claude-sonnet-5",
	})

	// Assert
	if err == nil {
		t.Fatal("expected a project without a local path to be rejected")
	}
	if len(workspace.agentSettings.Roles) != 0 {
		t.Fatalf("expected nothing written, got %+v", workspace.agentSettings.Roles)
	}
}

func TestInitialiseStoreUseCase_ShouldNotLinkRepositoriesWhenTheRoleWriteFails(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{updateErr: fmt.Errorf("push rejected")}
	useCase := &InitialiseStoreUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(InitialiseStoreCommand{
		Project:      configuredProject(),
		Model:        "github-copilot/claude-sonnet-5",
		Repositories: []string{"acme/widgets"},
	})

	// Assert
	if err == nil {
		t.Fatal("expected the role write failure to propagate")
	}
	if len(workspace.repositories) != 0 {
		t.Fatalf("expected the second write skipped, got %+v", workspace.repositories)
	}
}
