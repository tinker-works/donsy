package usecases

import (
	"context"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

func TestCancelAgentRunUseCase_ShouldCancelAQueuedRunDirectly(t *testing.T) {
	// Arrange: nothing is executing it, so no goroutine will notice a
	// context — this use case has to record the outcome itself.
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{{
		ID: "run-1", ProjectID: 1, SandboxID: "sandbox-1", Agent: "opencode", Round: 1,
		Role: agent.AgentRoleRefiner, Engine: agent.AgentEngineOpenCode,
		SessionMode: agent.SessionModeFresh, Status: agent.AgentRunStatusQueued,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: "epic-1"},
	}}}
	useCase := &CancelAgentRunUseCase{
		registry: registry, supervisor: NewRunSupervisor(), clock: fixedClock{now: now},
	}

	// Act
	cancelled, err := useCase.Handle(CancelAgentRunCommand{RunID: "run-1"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !cancelled || registry.runs[0].Status != agent.AgentRunStatusCancelled {
		t.Fatalf("expected the run to be cancelled, got %+v", registry.runs[0])
	}
}

func TestCancelAgentRunUseCase_ShouldReportNothingForAFinishedRun(t *testing.T) {
	// Arrange: a cancel that arrives after the round finished is a lost race,
	// not an error.
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{{
		ID: "run-1", ProjectID: 1, Status: agent.AgentRunStatusSucceeded,
	}}}
	useCase := &CancelAgentRunUseCase{
		registry: registry, supervisor: NewRunSupervisor(), clock: fixedClock{now: now},
	}

	// Act
	cancelled, err := useCase.Handle(CancelAgentRunCommand{RunID: "run-1"})

	// Assert
	if err != nil || cancelled {
		t.Fatalf("expected nothing to be cancelled, got %t (%v)", cancelled, err)
	}
	if registry.runs[0].Status != agent.AgentRunStatusSucceeded {
		t.Fatalf("expected the run untouched, got %+v", registry.runs[0])
	}
}

func TestCancelAgentRunUseCase_ShouldStopALiveRoundAndRecordItCancelled(t *testing.T) {
	// Arrange: a round blocked in the runtime. Cancelling has to reach it
	// through the supervisor and land as Cancelled, not Failed — a failed
	// round reads as something going wrong, which is not what happened.
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{}
	supervisor := NewRunSupervisor()
	started := make(chan string, 1)
	runtime := &fakeAgentRuntime{run: func(ctx context.Context, runID string) (string, error) {
		started <- runID
		<-ctx.Done()
		return "", ctx.Err()
	}}
	workspace := &fakeWorkspace{
		detail:        testEpic(epicpkg.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	useCase := &RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry,
		sandboxes: &fakeSandboxManager{}, runtime: runtime, builder: fakeCommandBuilder{},
		creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
		issueTreeStore: fakeIssueTreeStore{}, clock: fixedClock{now: now}, supervisor: supervisor,
	}
	cancelUseCase := &CancelAgentRunUseCase{
		registry: registry, supervisor: supervisor, clock: fixedClock{now: now},
	}

	// Act
	done := make(chan error, 1)
	go func() {
		done <- useCase.Handle(context.Background(), RunEpicAgentCommand{
			Project: domain.Project{ID: 1, Name: "one"},
			EpicID:  "epic-1",
			Spec:    EpicSandboxSpec(1, "epic-1", agent.AgentRoleRefiner, now),
		})
	}()
	runID := <-started
	cancelled, err := cancelUseCase.Handle(CancelAgentRunCommand{RunID: runID})
	roundErr := <-done

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !cancelled {
		t.Fatal("expected the live round to be cancelled")
	}
	if roundErr == nil {
		t.Fatal("expected the round to report that it stopped")
	}
	var recorded agent.AgentRun
	for _, run := range registry.runs {
		if run.ID == runID {
			recorded = run
		}
	}
	if recorded.Status != agent.AgentRunStatusCancelled {
		t.Fatalf("expected a cancelled run, got %q (%s)", recorded.Status, recorded.Error)
	}
}

func TestRunSupervisor_ShouldForgetARunAfterItIsRecorded(t *testing.T) {
	// Arrange: the cancelled marker must not outlive the run, or the next
	// round on the same ID would be misreported.
	supervisor := NewRunSupervisor()
	_, release := supervisor.Begin(context.Background(), "run-1")
	supervisor.Cancel("run-1")
	release()

	// Act
	before := supervisor.WasCancelled("run-1")
	supervisor.Forget("run-1")

	// Assert
	if !before || supervisor.WasCancelled("run-1") {
		t.Fatal("expected the cancellation to be remembered once, then forgotten")
	}
}

func TestRunSupervisor_ShouldReportNoLiveRoundAfterRelease(t *testing.T) {
	// Arrange
	supervisor := NewRunSupervisor()
	_, release := supervisor.Begin(context.Background(), "run-1")
	release()

	// Act & Assert
	if supervisor.Cancel("run-1") {
		t.Fatal("expected no live round to cancel once released")
	}
}
