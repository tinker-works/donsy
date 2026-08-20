package usecases

import (
	"testing"

	"github.com/tinker-works/donsy/internal/domain/agent"
)

func TestListAgentRunsUseCase_Handle_ShouldReturnOnlyTheProjectsRuns(t *testing.T) {
	// Arrange
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{
		{ID: "run-1", ProjectID: 1, Status: agent.AgentRunStatusRunning},
		{ID: "run-2", ProjectID: 2, Status: agent.AgentRunStatusRunning},
		{ID: "run-3", ProjectID: 1, Status: agent.AgentRunStatusSucceeded},
	}}
	useCase := &ListAgentRunsUseCase{registry: registry}

	// Act
	runs, err := useCase.Handle(ListAgentRunsQuery{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].ID != "run-1" || runs[1].ID != "run-3" {
		t.Fatalf("expected project 1's runs, got %+v", runs)
	}
}

func TestGetAgentRunUseCase_Handle_ShouldReturnTheRequestedRun(t *testing.T) {
	// Arrange
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{
		{ID: "run-1", ProjectID: 1},
		{ID: "run-2", ProjectID: 1},
	}}
	useCase := &GetAgentRunUseCase{registry: registry}

	// Act
	run, err := useCase.Handle(GetAgentRunQuery{RunID: "run-2"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "run-2" {
		t.Fatalf("expected run-2, got %q", run.ID)
	}
}

func TestGetAgentRunUseCase_Handle_ShouldFailForAnUnknownRun(t *testing.T) {
	// Arrange
	useCase := &GetAgentRunUseCase{registry: &fakeAgentRegistry{}}

	// Act
	_, err := useCase.Handle(GetAgentRunQuery{RunID: "missing"})

	// Assert
	if err == nil {
		t.Fatal("expected an error for an unknown run")
	}
}

func TestListSandboxesUseCase_Handle_ShouldReturnTheRegistrysSandboxes(t *testing.T) {
	// Arrange
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{
		{ID: "sandbox-1", ProjectID: 1, Status: agent.SandboxStatusRunning},
	}}
	useCase := &ListSandboxesUseCase{registry: registry}

	// Act
	sandboxes, err := useCase.Handle(ListSandboxesQuery{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes) != 1 || sandboxes[0].ID != "sandbox-1" {
		t.Fatalf("expected sandbox-1, got %+v", sandboxes)
	}
}
