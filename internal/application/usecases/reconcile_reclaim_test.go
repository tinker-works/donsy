package usecases

import (
	"context"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain/agent"
)

// finishedSubject is the shape reconciliation reclaims immediately: a sandbox whose
// subject will never get another round.
func finishedSubject() agent.AgentSubject {
	return agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
}

func TestReconcileSandboxesUseCase_ShouldReclaimAFinishedSubjectsSandboxAtOnce(t *testing.T) {
	// Arrange: a sandbox whose subject is merged or closed goes now rather than waiting
	// out the idle clock.
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "project-coding-issue-1", Role: agent.AgentRoleCoding,
		Subject: finishedSubject(), Status: agent.SandboxStatusStopped,
		CreatedAt: now, UpdatedAt: now,
	}}}
	sandboxes := &fakeSandboxManager{}
	credentials := &fakeAgentCredentials{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
		sandboxes: sandboxes, creds: credentials, clock: fixedClock{now: now},
		idleAfter: 24 * time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{
		ProjectID: 1,
		Terminal:  map[agent.AgentSubject]struct{}{finishedSubject(): {}},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.deleted) != 1 || sandboxes.deleted[0] != "project-coding-issue-1" {
		t.Fatalf("expected the sandbox deleted, got %v", sandboxes.deleted)
	}
	// The credentials were staged under the sandbox's name, so they go with it: left
	// behind they are a real provider credential for an instance that is gone.
	if len(credentials.discarded) != 1 {
		t.Fatalf("expected the staged credentials discarded, got %v", credentials.discarded)
	}
	if len(registry.savedSandboxes) != 1 ||
		registry.savedSandboxes[0].Status != agent.SandboxStatusAbsent {
		t.Fatalf("expected the record brought in line, got %#v", registry.savedSandboxes)
	}
}

func TestReconcileSandboxesUseCase_ShouldOnlySquareTheRecordForAnAbsentSandbox(t *testing.T) {
	// Arrange: a provider that no longer has the instance leaves nothing to
	// delete, so the record is simply brought in line.
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "project-coding-issue-1", Role: agent.AgentRoleCoding,
		Subject: finishedSubject(), Status: agent.SandboxStatusStopped,
		CreatedAt: now, UpdatedAt: now,
	}}}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: 24 * time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{
		ProjectID: 1,
		Terminal:  map[agent.AgentSubject]struct{}{finishedSubject(): {}},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.deleted) != 0 {
		t.Fatalf("expected nothing deleted, got %v", sandboxes.deleted)
	}
	if len(registry.savedSandboxes) != 1 || !registry.savedSandboxes[0].UpdatedAt.Equal(now) {
		t.Fatalf("expected the record stamped, got %#v", registry.savedSandboxes)
	}
}

func TestReconcileSandboxesUseCase_ShouldCutThePowerOnARunawaySandbox(t *testing.T) {
	// Arrange: a sandbox the provider still reports running past the runtime cap is
	// force-stopped whatever its records claim.
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	started := now.Add(-4 * time.Hour)
	registry := &fakeAgentRegistry{
		sandboxes: []agent.Sandbox{{
			ID: "sandbox-1", ProjectID: 1, Name: "project-coding-issue-1",
			Role: agent.AgentRoleCoding, Subject: finishedSubject(),
			Status: agent.SandboxStatusRunning, CreatedAt: started, UpdatedAt: started,
		}},
		runs: []agent.AgentRun{{
			ID: "run-1", ProjectID: 1, SandboxID: "sandbox-1", Role: agent.AgentRoleCoding,
			Subject: finishedSubject(), Engine: agent.AgentEngineOpenCode,
			Agent: "opencode", SessionMode: agent.SessionModeFresh,
			Status: agent.AgentRunStatusRunning, Round: 1,
			CreatedAt: started, StartedAt: &started,
		}},
	}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusRunning},
		sandboxes: sandboxes, clock: fixedClock{now: now},
		idleAfter: 24 * time.Hour, maxRuntime: 3 * time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.forceStopped) != 1 {
		t.Fatalf("expected the power cut, got graceful=%v forced=%v",
			sandboxes.stopped, sandboxes.forceStopped)
	}
	// A run still claiming a sandbox that just had its power cut is not going to report
	// back, and leaving it live would shield the sandbox again next tick.
	run, err := registry.GetAgentRun("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != agent.AgentRunStatusStalled {
		t.Fatalf("expected the run stalled, got %q", run.Status)
	}
}

func TestReconcileSandboxesUseCase_ShouldLeaveARunningSandboxWithALiveRoundAlone(t *testing.T) {
	// Arrange: the live-run check is what keeps reconciliation from being the only
	// thing standing between a real round and a stop.
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Minute)
	registry := &fakeAgentRegistry{
		sandboxes: []agent.Sandbox{{
			ID: "sandbox-1", ProjectID: 1, Name: "project-coding-issue-1",
			Role: agent.AgentRoleCoding, Subject: finishedSubject(),
			Status: agent.SandboxStatusRunning, CreatedAt: started, UpdatedAt: started,
		}},
		runs: []agent.AgentRun{{
			ID: "run-1", ProjectID: 1, SandboxID: "sandbox-1", Role: agent.AgentRoleCoding,
			Subject: finishedSubject(), Engine: agent.AgentEngineOpenCode,
			Agent: "opencode", SessionMode: agent.SessionModeFresh,
			Status: agent.AgentRunStatusRunning, Round: 1,
			CreatedAt: started, StartedAt: &started,
		}},
	}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusRunning},
		sandboxes: sandboxes, clock: fixedClock{now: now},
		idleAfter: 24 * time.Hour, maxRuntime: 3 * time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{
		ProjectID: 1,
		InFlight:  map[agent.AgentSubject]struct{}{finishedSubject(): {}},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.forceStopped) != 0 || len(sandboxes.deleted) != 0 {
		t.Fatalf("expected the live round's sandbox untouched, got forced=%v deleted=%v",
			sandboxes.forceStopped, sandboxes.deleted)
	}
}

func TestReconcileSandboxesUseCase_ShouldSurfaceARegistryFailure(t *testing.T) {
	// Arrange
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	useCase := ReconcileSandboxesUseCase{
		registry:  &fakeAgentRegistry{listSandboxesErr: errStore},
		inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
		sandboxes: &fakeSandboxManager{}, clock: fixedClock{now: now},
	}

	// Act & Assert
	if err := useCase.Handle(
		context.Background(), ReconcileSandboxesCommand{ProjectID: 1},
	); err == nil {
		t.Fatal("expected the registry failure surfaced")
	}
}
