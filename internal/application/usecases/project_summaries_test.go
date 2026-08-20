package usecases

import (
	"fmt"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

func TestListProjectSummariesUseCase_Handle_ShouldCountEpicsAndLiveRuns(t *testing.T) {
	// Arrange: only queued, admitted, and running count as live.
	registry := &fakeRegistry{projects: []domain.Project{
		{ID: 1, Name: "One"},
	}}
	agentRegistry := &fakeAgentRegistry{runs: []agent.AgentRun{
		{ID: "run-1", ProjectID: 1, Status: agent.AgentRunStatusRunning},
		{ID: "run-2", ProjectID: 1, Status: agent.AgentRunStatusQueued},
		{ID: "run-3", ProjectID: 1, Status: agent.AgentRunStatusSucceeded},
		{ID: "run-4", ProjectID: 2, Status: agent.AgentRunStatusRunning},
	}}
	useCase := &ListProjectSummariesUseCase{
		registry:      registry,
		agentRegistry: agentRegistry,
		factory:       &fakeFactory{workspace: &fakeWorkspace{detail: testEpic(epic.EpicStateReady)}},
	}

	// Act
	summaries, err := useCase.Handle()

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected one summary, got %+v", summaries)
	}
	if summaries[0].Epics != 1 || summaries[0].Running != 2 {
		t.Fatalf("unexpected counts: %+v", summaries[0])
	}
}

func TestListProjectSummariesUseCase_Handle_ShouldKeepListingAfterOneProjectFails(t *testing.T) {
	// Arrange: an unreadable project must not blank the whole picker.
	registry := &fakeRegistry{projects: []domain.Project{
		{ID: 1, Name: "Broken"},
		{ID: 2, Name: "Healthy"},
	}}
	useCase := &ListProjectSummariesUseCase{
		registry:      registry,
		agentRegistry: &fakeAgentRegistry{},
		factory: &fakeFactory{byPath: map[string]*fakeWorkspace{
			"Broken":  {listEpicsErr: fmt.Errorf("store is unreadable")},
			"Healthy": {detail: testEpic(epic.EpicStateReady)},
		}},
	}

	// Act
	summaries, err := useCase.Handle()

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected both projects, got %+v", summaries)
	}
	if summaries[0].Err == nil || summaries[0].Epics != 0 {
		t.Fatalf("expected the broken project to report its error: %+v", summaries[0])
	}
	if summaries[1].Err != nil || summaries[1].Epics != 1 {
		t.Fatalf("expected the healthy project to be counted: %+v", summaries[1])
	}
}
