package usecases

import (
	"fmt"
	"strings"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

type SetAgentRoleCommand struct {
	Project domain.Project
	Role    agent.AgentRole
	Agent   string
	Variant string
}

type SetAgentRoleUseCase struct {
	factory application.WorkspaceFactory
}

// Handle assigns one role's agent profile. A role with no profile is silently
// skipped by EpicWorker, so this is the only way to make a role runnable.
func (u *SetAgentRoleUseCase) Handle(command SetAgentRoleCommand) error {
	agentName := strings.TrimSpace(command.Agent)
	if agentName == "" {
		return fmt.Errorf("agent is required for role %q", command.Role)
	}
	return u.factory.Open(command.Project.Name).UpdateAgentSettings(
		func(settings *agent.AgentSettings) error {
			if settings.Roles == nil {
				settings.Roles = map[agent.AgentRole]agent.AgentProfile{}
			}
			// Only the fields this command carries are replaced: MaxRounds is
			// configured in the settings file itself, and assigning an agent
			// must not silently reset a role's round limit to unlimited.
			profile := settings.Roles[command.Role]
			profile.Agent = agentName
			profile.Variant = strings.TrimSpace(command.Variant)
			settings.Roles[command.Role] = profile
			return settings.Validate()
		},
	)
}
