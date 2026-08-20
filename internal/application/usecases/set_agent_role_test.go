package usecases

import (
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

func TestSetAgentRoleUseCase_Handle_ShouldAssignTheRolesProfile(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{agentSettings: testAgentSettings()}
	useCase := &SetAgentRoleUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(SetAgentRoleCommand{
		Project: domain.Project{Name: "one"},
		Role:    agent.AgentRoleCoding, Agent: "coder", Variant: "high",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	profile, err := workspace.agentSettings.Profile(agent.AgentRoleCoding)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Agent != "coder" || profile.Variant != "high" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
}

func TestSetAgentRoleUseCase_Handle_ShouldCreateSettingsWhenNoneExist(t *testing.T) {
	// Arrange: a project that has never configured a role has a nil map.
	workspace := &fakeWorkspace{}
	useCase := &SetAgentRoleUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(SetAgentRoleCommand{
		Project: domain.Project{Name: "one"},
		Role:    agent.AgentRoleRefiner, Agent: "refiner",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.agentSettings.Profile(agent.AgentRoleRefiner); err != nil {
		t.Fatal(err)
	}
}

func TestSetAgentRoleUseCase_Handle_ShouldRejectAnEmptyAgent(t *testing.T) {
	// Arrange: an empty agent is what "role has no profile" looks like, and
	// EpicWorker silently skips such a role rather than reporting it.
	workspace := &fakeWorkspace{agentSettings: testAgentSettings()}
	useCase := &SetAgentRoleUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(SetAgentRoleCommand{
		Project: domain.Project{Name: "one"},
		Role:    agent.AgentRoleCoding, Agent: "  ",
	})

	// Assert
	if err == nil {
		t.Fatal("expected an empty agent to be rejected")
	}
}

// MaxRounds is configured in the settings file, not by this command, so
// reassigning a role's agent must leave the existing round limit intact —
// otherwise RunEpicAgentUseCase's round cap silently becomes unlimited.
func TestSetAgentRoleUseCase_Handle_ShouldPreserveTheRolesRoundLimit(t *testing.T) {
	// Arrange
	settings := testAgentSettings()
	settings.Roles[agent.AgentRoleRefiner] = agent.AgentProfile{
		Agent: "old", Variant: "old-variant", MaxRounds: 3,
	}
	workspace := &fakeWorkspace{agentSettings: settings}
	useCase := &SetAgentRoleUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	err := useCase.Handle(SetAgentRoleCommand{
		Project: domain.Project{Name: "one"},
		Role:    agent.AgentRoleRefiner,
		Agent:   "new",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	profile := workspace.agentSettings.Roles[agent.AgentRoleRefiner]
	if profile.Agent != "new" || profile.Variant != "" {
		t.Fatalf("expected the agent and variant to be replaced, got %#v", profile)
	}
	if profile.MaxRounds != 3 {
		t.Fatalf("expected the round limit to survive, got %d", profile.MaxRounds)
	}
}

func TestGetAgentSettingsUseCase_Handle_ShouldReturnTheProjectsSettings(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{agentSettings: testAgentSettings()}
	useCase := &GetAgentSettingsUseCase{factory: &fakeFactory{workspace: workspace}}

	// Act
	settings, err := useCase.Handle(GetAgentSettingsQuery{
		Project: domain.Project{Name: "one"},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Profile(agent.AgentRoleRefiner); err != nil {
		t.Fatal(err)
	}
}
