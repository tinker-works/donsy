package agent

import "fmt"

// RepositorySettings overrides AgentSettings for one repository. Any field
// left at its zero value falls back to the project-wide AgentSettings via
// AgentSettings.Override — this type never resolves a default on its own.
type RepositorySettings struct {
	SetupScript string
	Roles       map[AgentRole]AgentProfile
}

func (s RepositorySettings) Validate() error {
	for role, profile := range s.Roles {
		if !IsAgentRole(role) {
			return fmt.Errorf("repository settings have invalid role %q", role)
		}
		if profile.Agent == "" {
			return fmt.Errorf("repository settings require an agent for role %q", role)
		}
		if profile.MaxRounds < 0 {
			return fmt.Errorf("repository settings require a non-negative max rounds for role %q", role)
		}
	}
	return nil
}
