package usecases

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain/agent"
)

// fakeProjectHost records what the sweep asked of a project's container host.
type fakeProjectHost struct {
	stopped []uint
	deleted []uint
	stopErr error
	reaped  []uint
	reapErr error
}

func (h *fakeProjectHost) ReapExpiredContainers(
	_ context.Context, projectID uint, _, _ time.Time,
) (bool, error) {
	h.reaped = append(h.reaped, projectID)
	return false, h.reapErr
}

func (h *fakeProjectHost) StopProfile(_ context.Context, projectID uint) (bool, error) {
	h.stopped = append(h.stopped, projectID)
	return h.stopErr == nil, h.stopErr
}

func (h *fakeProjectHost) DeleteProfile(_ context.Context, projectID uint) error {
	h.deleted = append(h.deleted, projectID)
	return nil
}

// hostSweep builds a reconciler whose sandboxes all report status, with the
// host idle window already elapsed unless a test says otherwise.
func hostSweep(
	now time.Time, status agent.SandboxStatus, host *fakeProjectHost,
) (*ReconcileSandboxesUseCase, *fakeAgentRegistry) {
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "gm-1-issue-coding", Role: agent.AgentRoleCoding,
		Subject:   agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
		Status:    status,
		UpdatedAt: now,
	}}}
	useCase := &ReconcileSandboxesUseCase{
		registry:         registry,
		inspector:        fakeSandboxInspector{status: status},
		sandboxes:        &fakeSandboxManager{},
		clock:            fixedClock{now: now},
		host:             host,
		hostIdleAfter:    5 * time.Minute,
		hostRunningAfter: time.Hour, hostStoppedAfter: 24 * time.Hour,
		// The first quiet tick only starts the clock, so pre-seed it as though
		// the project went quiet an hour ago.
		lastBusy: map[uint]time.Time{1: now.Add(-time.Hour)},
	}
	return useCase, registry
}

// Every sandbox of a project shares one machine, so the machine outlives them:
// a project whose containers are all stopped is still paying for a running VM.
func TestReconcileSandboxesUseCase_ShouldStopTheHostOfAQuietProject(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	host := &fakeProjectHost{}
	useCase, _ := hostSweep(now, agent.SandboxStatusStopped, host)

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(host.stopped) != 1 || host.stopped[0] != 1 {
		t.Fatalf("expected the project's host stopped, got %v", host.stopped)
	}
	if len(host.reaped) != 1 || host.reaped[0] != 1 {
		t.Fatalf("expected the project's expired containers reaped, got %v", host.reaped)
	}
}

// A running container is the clearest possible sign the machine is in use, and
// the records are not what decides — the provider is.
func TestReconcileSandboxesUseCase_ShouldKeepTheHostWhileASandboxIsRunning(t *testing.T) {
	// Arrange: running, with a live run so it is not force-stopped as unowned.
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	host := &fakeProjectHost{}
	useCase, registry := hostSweep(now, agent.SandboxStatusRunning, host)
	registry.runs = []agent.AgentRun{{
		ID: "run-1", ProjectID: 1, SandboxID: "sandbox-1", Role: agent.AgentRoleCoding,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
		Engine:  agent.AgentEngineOpenCode, SessionMode: agent.SessionModeFresh,
		Agent: "coding", Status: agent.AgentRunStatusRunning, Round: 1, CreatedAt: now,
	}}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(host.stopped) != 0 {
		t.Fatalf("expected the busy host left alone, got %v", host.stopped)
	}
}

// InFlight is the race-free guard: it lives on the scheduler's own goroutine,
// so a round dispatched this tick is visible here even though no record is.
func TestReconcileSandboxesUseCase_ShouldKeepTheHostWhileASubjectIsInFlight(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	host := &fakeProjectHost{}
	useCase, _ := hostSweep(now, agent.SandboxStatusStopped, host)
	subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{
		ProjectID: 1,
		InFlight:  map[agent.AgentSubject]struct{}{subject: {}},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(host.stopped) != 0 {
		t.Fatalf("expected the host of a project with a round in flight kept, got %v", host.stopped)
	}
}

// A sandbox this pass could not read may well be running, and stopping the
// machine under it would kill a round nobody observed.
func TestReconcileSandboxesUseCase_ShouldKeepTheHostWhenASandboxCouldNotBeInspected(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	host := &fakeProjectHost{}
	useCase, _ := hostSweep(now, agent.SandboxStatusStopped, host)
	useCase.inspector = fakeSandboxInspector{err: fmt.Errorf("provider offline")}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err == nil {
		t.Fatal("expected the inspect failure to be reported")
	}
	if len(host.stopped) != 0 {
		t.Fatalf("expected no stop on a pass that could not see everything, got %v", host.stopped)
	}
}

// On a five-second tick a project running rounds back to back would otherwise
// stop and lazily restart its machine in every gap between them.
func TestReconcileSandboxesUseCase_ShouldNotStopTheHostInsideTheIdleWindow(t *testing.T) {
	// Arrange: quiet, but only just.
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	host := &fakeProjectHost{}
	useCase, _ := hostSweep(now, agent.SandboxStatusStopped, host)
	useCase.lastBusy = map[uint]time.Time{1: now.Add(-time.Minute)}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(host.stopped) != 0 {
		t.Fatalf("expected the recently busy host kept warm, got %v", host.stopped)
	}
}

// The first sight of a project starts its clock rather than assuming it has
// been idle since the beginning of time.
func TestReconcileSandboxesUseCase_ShouldStartTheIdleClockOnFirstSight(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	host := &fakeProjectHost{}
	useCase, _ := hostSweep(now, agent.SandboxStatusStopped, host)
	useCase.lastBusy = nil

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(host.stopped) != 0 {
		t.Fatalf("expected the first quiet tick only to start the clock, got %v", host.stopped)
	}
	if _, seen := useCase.lastBusy[1]; !seen {
		t.Fatal("expected the clock to have been started")
	}
}

// Stopping an already-stopped machine is harmless but not free — it shells out
// — and a project nobody is working on would otherwise pay for that on every
// tick for as long as go-merge runs.
func TestReconcileSandboxesUseCase_ShouldAskToStopTheHostOncePerQuietPeriod(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	host := &fakeProjectHost{}
	useCase, _ := hostSweep(now, agent.SandboxStatusStopped, host)

	// Act: three quiet ticks in a row.
	for range 3 {
		if err := useCase.Handle(
			context.Background(), ReconcileSandboxesCommand{ProjectID: 1}); err != nil {
			t.Fatal(err)
		}
	}

	// Assert
	if len(host.stopped) != 1 {
		t.Fatalf("expected one stop across three quiet ticks, got %v", host.stopped)
	}
}

// A project that wakes up and goes quiet again gets its machine stopped again;
// the once-per-quiet-period guard is not once per process.
func TestReconcileSandboxesUseCase_ShouldStopTheHostAgainAfterTheProjectWakesUp(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	host := &fakeProjectHost{}
	useCase, _ := hostSweep(now, agent.SandboxStatusStopped, host)
	subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	quiet := ReconcileSandboxesCommand{ProjectID: 1}
	busy := ReconcileSandboxesCommand{
		ProjectID: 1, InFlight: map[agent.AgentSubject]struct{}{subject: {}},
	}

	// Act
	for _, command := range []ReconcileSandboxesCommand{quiet, busy, quiet} {
		if err := useCase.Handle(context.Background(), command); err != nil {
			t.Fatal(err)
		}
		// The round in the middle leaves the project quiet again well before
		// the window; wind the clock past it.
		useCase.lastBusy[1] = now.Add(-time.Hour)
	}

	// Assert
	if len(host.stopped) != 2 {
		t.Fatalf("expected the host stopped on both quiet ticks, got %v", host.stopped)
	}
}

// A host that will not stop must not stop the next project from being
// reconciled, tick after tick.
func TestReconcileSandboxesUseCase_ShouldReportButNotSwallowAHostStopFailure(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	host := &fakeProjectHost{stopErr: fmt.Errorf("colima is wedged")}
	useCase, _ := hostSweep(now, agent.SandboxStatusStopped, host)

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err == nil {
		t.Fatal("expected the stop failure to be reported")
	}
	if len(host.stopped) != 1 {
		t.Fatalf("expected the stop to have been attempted, got %v", host.stopped)
	}
}

// A runtime whose sandboxes have no shared machine leaves this nil, and the
// sweep must behave exactly as it did before the port existed.
func TestReconcileSandboxesUseCase_ShouldReconcileWithoutAHostAtAll(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	useCase, _ := hostSweep(now, agent.SandboxStatusStopped, nil)
	useCase.host = nil

	// Act, Assert
	if err := useCase.Handle(
		context.Background(), ReconcileSandboxesCommand{ProjectID: 1}); err != nil {
		t.Fatal(err)
	}
}
