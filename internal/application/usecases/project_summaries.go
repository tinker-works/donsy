package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

// ProjectSummary is one project plus the counts a picker shows beside it.
// Err records a per-project read failure: one unreadable project must not
// blank the whole list, so the row is still returned with zero counts.
type ProjectSummary struct {
	Project domain.Project
	Epics   int
	Running int
	Err     error
}

type ListProjectSummariesUseCase struct {
	registry      application.ProjectRegistry
	agentRegistry agent_runtime.AgentRegistry
	factory       application.WorkspaceFactory
}

func (u *ListProjectSummariesUseCase) Handle() ([]ProjectSummary, error) {
	projects, err := u.registry.List()
	if err != nil {
		return nil, err
	}
	summaries := make([]ProjectSummary, 0, len(projects))
	for _, project := range projects {
		summaries = append(summaries, u.summarize(project))
	}
	return summaries, nil
}

func (u *ListProjectSummariesUseCase) summarize(project domain.Project) ProjectSummary {
	summary := ProjectSummary{Project: project}
	epics, err := u.factory.Open(project.Name).ListEpics()
	if err != nil {
		summary.Err = err
		return summary
	}
	summary.Epics = len(epics)
	if u.agentRegistry == nil {
		return summary
	}
	runs, err := u.agentRegistry.ListProjectAgentRuns(project.ID)
	if err != nil {
		summary.Err = err
		return summary
	}
	for _, run := range runs {
		if isLiveRun(run.Status) {
			summary.Running++
		}
	}
	return summary
}

func isLiveRun(status agent.AgentRunStatus) bool {
	return status == agent.AgentRunStatusQueued || status == agent.AgentRunStatusAdmitted ||
		status == agent.AgentRunStatusRunning
}
