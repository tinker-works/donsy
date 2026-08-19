package agent

import "testing"

func TestAgentSettings_Profile_ShouldReturnConfiguredProfile(t *testing.T) {
	// Arrange
	settings := AgentSettings{Roles: map[AgentRole]AgentProfile{
		AgentRoleRefiner: {Agent: "refiner", Variant: "high", MaxRounds: 3},
	}}

	// Act
	profile, err := settings.Profile(AgentRoleRefiner)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if profile.Agent != "refiner" || profile.Variant != "high" || profile.MaxRounds != 3 {
		t.Fatalf("unexpected profile: %#v", profile)
	}
}

func TestAgentSettings_Profile_ShouldRejectMissingRole(t *testing.T) {
	// Arrange
	settings := AgentSettings{}

	// Act
	_, err := settings.Profile(AgentRoleRefiner)

	// Assert
	if err == nil {
		t.Fatal("expected missing role to be rejected")
	}
}

func TestAgentSettings_Profile_ShouldRejectProfileWithoutAgent(t *testing.T) {
	// Arrange
	settings := AgentSettings{Roles: map[AgentRole]AgentProfile{
		AgentRoleRefiner: {Variant: "high"},
	}}

	// Act
	_, err := settings.Profile(AgentRoleRefiner)

	// Assert
	if err == nil {
		t.Fatal("expected profile without an agent to be rejected")
	}
}

func TestAgentSettings_ConfiguredRoles_ShouldCountOnlyRolesWithAModel(t *testing.T) {
	// Arrange
	// The middle role carries a variant but no model, which Profile treats as
	// unset — the count has to agree with it.
	settings := AgentSettings{Roles: map[AgentRole]AgentProfile{
		AgentRoleRefiner:       {Agent: "github-copilot/claude-sonnet-5"},
		AgentRoleIssueReviewer: {Variant: "high"},
		AgentRoleCoding:        {Agent: "github-copilot/claude-opus-5"},
	}}

	// Act
	configured := settings.ConfiguredRoles()

	// Assert
	if configured != 2 {
		t.Fatalf("expected 2 configured roles, got %d", configured)
	}
}

func TestAgentSettings_ConfiguredRoles_ShouldIgnoreRolesOutsideTheKnownSet(t *testing.T) {
	// Arrange
	// A stale or hand-written agent.yaml can name a role this build does not
	// have. Counting it would report a store as more configured than it is.
	settings := AgentSettings{Roles: map[AgentRole]AgentProfile{
		AgentRoleRefiner: {Agent: "github-copilot/claude-sonnet-5"},
		"retired-role":   {Agent: "github-copilot/claude-opus-5"},
	}}

	// Act
	configured := settings.ConfiguredRoles()

	// Assert
	if configured != 1 {
		t.Fatalf("expected only the known role to count, got %d", configured)
	}
}

func TestAgentSettings_ConfiguredRoles_ShouldCountEveryRoleWhenAllAssigned(t *testing.T) {
	// Arrange
	settings := AgentSettings{Roles: map[AgentRole]AgentProfile{}}
	for _, role := range Roles() {
		settings.Roles[role] = AgentProfile{Agent: "github-copilot/claude-sonnet-5"}
	}

	// Act
	configured := settings.ConfiguredRoles()

	// Assert
	if configured != len(Roles()) {
		t.Fatalf("expected all %d roles configured, got %d", len(Roles()), configured)
	}
}

func TestRoles_ShouldListEveryRoleIsAgentRoleAccepts(t *testing.T) {
	// Arrange
	listed := Roles()

	// Act & Assert
	// Roles and IsAgentRole must describe the same set, or a setup gate built on
	// one disagrees with the validation built on the other.
	for _, role := range listed {
		if !IsAgentRole(role) {
			t.Fatalf("Roles lists %q but IsAgentRole rejects it", role)
		}
	}
	if len(listed) != 5 {
		t.Fatalf("expected 5 roles, got %d: %v", len(listed), listed)
	}
}

func TestRoles_ShouldNotLetCallersMutateTheSharedList(t *testing.T) {
	// Arrange
	first := Roles()

	// Act
	first[0] = "clobbered"

	// Assert
	if Roles()[0] != AgentRoleRefiner {
		t.Fatalf("mutating the returned slice changed the role list: %v", Roles())
	}
}

func TestAgentSettings_Validate_ShouldRejectUnknownRole(t *testing.T) {
	// Arrange
	settings := AgentSettings{Roles: map[AgentRole]AgentProfile{
		"unknown-role": {Agent: "agent"},
	}}

	// Act
	err := settings.Validate()

	// Assert
	if err == nil {
		t.Fatal("expected unknown role to be rejected")
	}
}

func TestAgentSettings_Validate_ShouldRequireAnAgentForEveryRole(t *testing.T) {
	// Arrange
	settings := AgentSettings{Roles: map[AgentRole]AgentProfile{
		AgentRoleRefiner: {Variant: "high"},
	}}

	// Act
	err := settings.Validate()

	// Assert
	if err == nil {
		t.Fatal("expected a role without an agent to be rejected")
	}
}

func TestAgentSettings_Validate_ShouldRejectNegativeMaxRounds(t *testing.T) {
	// Arrange
	settings := AgentSettings{Roles: map[AgentRole]AgentProfile{
		AgentRoleRefiner: {Agent: "refiner", MaxRounds: -1},
	}}

	// Act
	err := settings.Validate()

	// Assert
	if err == nil {
		t.Fatal("expected negative max rounds to be rejected")
	}
}

func TestAgentSettings_Validate_ShouldAllowZeroMaxRoundsAsUnlimited(t *testing.T) {
	// Arrange
	settings := AgentSettings{Roles: map[AgentRole]AgentProfile{
		AgentRoleRefiner: {Agent: "refiner", MaxRounds: 0},
	}}

	// Act
	err := settings.Validate()

	// Assert
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentSettings_Override_ShouldFallBackToProjectSetupScriptWhenRepoUnset(t *testing.T) {
	// Arrange
	settings := AgentSettings{SetupScript: "agents/scripts/project.sh"}

	// Act
	merged := settings.Override(RepositorySettings{})

	// Assert
	if merged.SetupScript != "agents/scripts/project.sh" {
		t.Fatalf("expected project setup script to survive, got %q", merged.SetupScript)
	}
}

func TestAgentSettings_Override_ShouldPreferRepoSetupScriptWhenSet(t *testing.T) {
	// Arrange
	settings := AgentSettings{SetupScript: "agents/scripts/project.sh"}

	// Act
	merged := settings.Override(RepositorySettings{SetupScript: "agents/scripts/repo.sh"})

	// Assert
	if merged.SetupScript != "agents/scripts/repo.sh" {
		t.Fatalf("expected repo setup script to win, got %q", merged.SetupScript)
	}
}

func TestAgentSettings_Override_ShouldMergeRolesWithRepoWinningPerRole(t *testing.T) {
	// Arrange
	settings := AgentSettings{Roles: map[AgentRole]AgentProfile{
		AgentRoleRefiner: {Agent: "project-refiner"},
		AgentRoleCoding:  {Agent: "project-coding"},
	}}
	repo := RepositorySettings{Roles: map[AgentRole]AgentProfile{
		AgentRoleCoding: {Agent: "repo-coding"},
	}}

	// Act
	merged := settings.Override(repo)

	// Assert
	if merged.Roles[AgentRoleRefiner].Agent != "project-refiner" {
		t.Fatalf("expected untouched role to survive, got %#v", merged.Roles[AgentRoleRefiner])
	}
	if merged.Roles[AgentRoleCoding].Agent != "repo-coding" {
		t.Fatalf("expected repo role to win, got %#v", merged.Roles[AgentRoleCoding])
	}
}

func TestRepositorySettings_Validate_ShouldRejectUnknownRole(t *testing.T) {
	// Arrange
	settings := RepositorySettings{Roles: map[AgentRole]AgentProfile{
		"unknown-role": {Agent: "agent"},
	}}

	// Act
	err := settings.Validate()

	// Assert
	if err == nil {
		t.Fatal("expected unknown role to be rejected")
	}
}

func TestRepositorySettings_Validate_ShouldRejectNegativeMaxRounds(t *testing.T) {
	// Arrange
	settings := RepositorySettings{Roles: map[AgentRole]AgentProfile{
		AgentRoleCoding: {Agent: "coder", MaxRounds: -1},
	}}

	// Act
	err := settings.Validate()

	// Assert
	if err == nil {
		t.Fatal("expected a negative max rounds value to be rejected")
	}
}

func TestRepositorySettings_Validate_ShouldAcceptAConfiguredRole(t *testing.T) {
	// Arrange
	settings := RepositorySettings{Roles: map[AgentRole]AgentProfile{
		AgentRoleCoding: {Agent: "coder", MaxRounds: 2},
	}}

	// Act
	err := settings.Validate()

	// Assert
	if err != nil {
		t.Fatal(err)
	}
}
