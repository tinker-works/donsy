package agent

import "fmt"

// AgentSettings are shared project policy, committed beside the project store.
type AgentSettings struct {
	// SetupScript is a path, relative to the tracker store root, to a shell
	// script that customizes the image issue sandboxes are built from — beyond
	// what the base image and Docker choice already provide. Empty means no
	// customization.
	SetupScript string
	Roles       map[AgentRole]AgentProfile
}

// Override layers a repository's settings on top of the project-wide ones:
// any key the repository specifies wins, and anything it leaves unset falls
// through to this AgentSettings' value.
func (s AgentSettings) Override(repo RepositorySettings) AgentSettings {
	merged := AgentSettings{SetupScript: s.SetupScript}
	if repo.SetupScript != "" {
		merged.SetupScript = repo.SetupScript
	}
	merged.Roles = make(map[AgentRole]AgentProfile, len(s.Roles)+len(repo.Roles))
	for role, profile := range s.Roles {
		merged.Roles[role] = profile
	}
	for role, profile := range repo.Roles {
		merged.Roles[role] = profile
	}
	return merged
}

type AgentProfile struct {
	Agent   string
	Variant string
	// MaxRounds caps how many rounds a role may run for one subject before the
	// worker gives up instead of retrying forever. Zero means unlimited, which
	// keeps existing agent.yaml files without this field behaving as before.
	MaxRounds int
}

// ConfiguredRoles counts the roles that are actually runnable. It applies the
// same rule as Profile — an empty Agent is unset, not assigned — so a caller
// deciding whether a store is configured cannot disagree with the caller that
// resolves a role at run time.
func (s AgentSettings) ConfiguredRoles() int {
	configured := 0
	for _, role := range Roles() {
		if _, err := s.Profile(role); err == nil {
			configured++
		}
	}
	return configured
}

func (s AgentSettings) Profile(role AgentRole) (AgentProfile, error) {
	profile, ok := s.Roles[role]
	if !ok || profile.Agent == "" {
		return AgentProfile{}, fmt.Errorf("agent settings do not configure role %q", role)
	}
	return profile, nil
}

func (s AgentSettings) Validate() error {
	for role, profile := range s.Roles {
		if !IsAgentRole(role) {
			return fmt.Errorf("agent settings have invalid role %q", role)
		}
		if profile.Agent == "" {
			return fmt.Errorf("agent settings require an agent for role %q", role)
		}
		if profile.MaxRounds < 0 {
			return fmt.Errorf("agent settings require a non-negative max rounds for role %q", role)
		}
	}
	return nil
}
