package usecases

import (
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

type ListAgentRunsQuery struct {
	ProjectID uint
}

type ListAgentRunsUseCase struct {
	registry agent_runtime.AgentRegistry
}

// Handle reads every run in a project, newest first, so a caller can show
// what is running without knowing which subjects exist.
func (u *ListAgentRunsUseCase) Handle(query ListAgentRunsQuery) ([]agent.AgentRun, error) {
	return u.registry.ListProjectAgentRuns(query.ProjectID)
}

type GetAgentRunQuery struct {
	RunID string
}

type GetAgentRunUseCase struct {
	registry agent_runtime.AgentRegistry
}

func (u *GetAgentRunUseCase) Handle(query GetAgentRunQuery) (agent.AgentRun, error) {
	return u.registry.GetAgentRun(query.RunID)
}

type ListSandboxesQuery struct {
	ProjectID uint
}

type ListSandboxesUseCase struct {
	registry agent_runtime.AgentRegistry
}

func (u *ListSandboxesUseCase) Handle(query ListSandboxesQuery) ([]agent.Sandbox, error) {
	return u.registry.ListSandboxes(query.ProjectID)
}
