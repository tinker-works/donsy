package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

type GetAgentSettingsQuery struct {
	Project domain.Project
}

type GetAgentSettingsUseCase struct {
	factory application.WorkspaceFactory
}

func (u *GetAgentSettingsUseCase) Handle(query GetAgentSettingsQuery) (agent.AgentSettings, error) {
	return u.factory.Open(query.Project.Name).AgentSettings()
}
