package usecases

import (
	"context"
	"errors"
	"fmt"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"testing"
	"time"
)

type fakeSandboxInspector struct {
	status agent.SandboxStatus
	err    error
}

func (i fakeSandboxInspector) Inspect(
	context.Context, agent.SandboxRef,
) (agent.SandboxStatus, error) {
	return i.status, i.err
}

// fakeSelectiveSandboxInspector fails only for one named sandbox, leaving the rest healthy.
type fakeSelectiveSandboxInspector struct {
	failFor   string
	err       error
	otherwise agent.SandboxStatus
}

func (i fakeSelectiveSandboxInspector) Inspect(
	_ context.Context, ref agent.SandboxRef,
) (agent.SandboxStatus, error) {
	name := ref.Name
	if name == i.failFor {
		return "", i.err
	}
	return i.otherwise, nil
}

type recordingSandboxInspector struct {
	inspected []agent.SandboxRef
}

func (i *recordingSandboxInspector) Inspect(
	_ context.Context, ref agent.SandboxRef,
) (agent.SandboxStatus, error) {
	i.inspected = append(i.inspected, ref)
	return agent.SandboxStatusAbsent, nil
}

func TestReconcileSandboxesUseCase_ShouldOnlyInspectThePendingRole(t *testing.T) {
	// Arrange: a stale sandbox is stored before the subject the worker can run.
	stale := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "stale"}
	pending := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "pending"}
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{
		{ID: "stale", ProjectID: 1, Name: "stale", Role: agent.AgentRoleCoding, Subject: stale, Status: agent.SandboxStatusAbsent},
		{ID: "old-role", ProjectID: 1, Name: "old-role", Role: agent.AgentRoleCoding, Subject: pending, Status: agent.SandboxStatusAbsent},
		{ID: "pending", ProjectID: 1, Name: "pending", Role: agent.AgentRolePRReviewer, Subject: pending, Status: agent.SandboxStatusAbsent},
	}}
	inspector := &recordingSandboxInspector{}
	useCase := ReconcileSandboxesUseCase{registry: registry, inspector: inspector, clock: fixedClock{}}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{
		ProjectID: 1, Priority: map[agent.AgentSubject]agent.AgentRole{
			pending: agent.AgentRolePRReviewer,
		},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(inspector.inspected) != 1 || inspector.inspected[0].Name != "pending" {
		t.Fatalf("expected only the pending role, got %#v", inspector.inspected)
	}
}

func TestReconcileSandboxesUseCase_ShouldUpdateStateObservedAfterDowntime(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "project-coding-issue-1", Role: agent.AgentRoleCoding,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
		Status:  agent.SandboxStatusRunning,
	}}}
	useCase := ReconcileSandboxesUseCase{
		registry:  registry,
		inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
		clock:     fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.savedSandboxes) != 1 ||
		registry.savedSandboxes[0].Status != agent.SandboxStatusStopped ||
		!registry.savedSandboxes[0].UpdatedAt.Equal(now) {
		t.Fatalf("unexpected reconciled sandboxes: %#v", registry.savedSandboxes)
	}
}

func TestReconcileSandboxesUseCase_ShouldContinuePastOneSandboxesInspectFailure(t *testing.T) {
	// One sandbox the provider can no longer see (deleted out-of-band, provider crash,
	// etc.) must not stop the rest of the project's sandboxes from being reconciled.
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{
		{ID: "sandbox-1", ProjectID: 1, Name: "broken-sandbox", Status: agent.SandboxStatusRunning},
		{ID: "sandbox-2", ProjectID: 1, Name: "healthy-sandbox", Status: agent.SandboxStatusRunning},
	}}
	useCase := ReconcileSandboxesUseCase{
		registry: registry,
		inspector: fakeSelectiveSandboxInspector{
			failFor: "broken-sandbox", err: fmt.Errorf("provider offline"),
			otherwise: agent.SandboxStatusStopped,
		},
		clock: fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err == nil {
		t.Fatal("expected the broken sandbox's inspect error to be reported")
	}
	if len(registry.savedSandboxes) != 1 || registry.savedSandboxes[0].Name != "healthy-sandbox" ||
		registry.savedSandboxes[0].Status != agent.SandboxStatusStopped {
		t.Fatalf("expected the healthy sandbox to still be reconciled: %#v", registry.savedSandboxes)
	}
}

func TestReconcileSandboxesUseCase_ShouldReclaimSandboxIdlePastThreshold(t *testing.T) {
	// A sandbox nothing has used in over an hour should be deleted so it stops holding
	// host disk and memory. EpicSandboxSpec's stable identity means the worker recreates
	// it automatically the next time its role is needed.
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "idle-sandbox",
		Status: agent.SandboxStatusStopped, UpdatedAt: now.Add(-2 * time.Hour),
	}}}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.deleted) != 1 || sandboxes.deleted[0] != "idle-sandbox" {
		t.Fatalf("expected the idle sandbox to be deleted: %#v", sandboxes.deleted)
	}
	if len(registry.savedSandboxes) != 1 ||
		registry.savedSandboxes[0].Status != agent.SandboxStatusAbsent {
		t.Fatalf("expected the sandbox to be recorded absent: %#v", registry.savedSandboxes)
	}
}

// fakeHostDisk reports a fixed amount of free space, or an error.
type fakeHostDisk struct {
	free int64
	err  error
}

func (d fakeHostDisk) FreeBytes() (int64, error) { return d.free, d.err }

func TestReconcileSandboxesUseCase_ShouldReclaimSoonerUnderDiskPressure(t *testing.T) {
	// The clock alone cannot tell a quiet host from one about to run out. Every sandbox
	// shares its project's host disk, so abandoned work fills it while
	// every sandbox is still well inside a day — and what fails first is not a round
	// but the image build every new sandbox waits on.
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	idle := agent.Sandbox{
		ID: "sandbox-1", ProjectID: 1, Name: "idle-sandbox",
		Status: agent.SandboxStatusStopped, UpdatedAt: now.Add(-2 * time.Hour),
	}
	tests := []struct {
		name    string
		disk    agent_runtime.HostDisk
		deleted bool
	}{
		// Two hours idle: past the pressure window, far short of the clock.
		{name: "under pressure", disk: fakeHostDisk{free: 1 << 30}, deleted: true},
		{name: "space to spare", disk: fakeHostDisk{free: 500 << 30}, deleted: false},
		// Free space is an optimisation on top of the clock. Failing the
		// reconciliation that repairs sandbox records after a crash because a statfs
		// failed would cost more than reclaiming late.
		{name: "unreadable disk", disk: fakeHostDisk{err: errors.New("nope")}, deleted: false},
		{name: "no disk configured", disk: nil, deleted: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{idle}}
			sandboxes := &fakeSandboxManager{}
			useCase := ReconcileSandboxesUseCase{
				registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
				sandboxes: sandboxes, clock: fixedClock{now: now},
				idleAfter: 24 * time.Hour,
				disk:      test.disk, pressureBelow: 25 << 30, idleUnderPressure: time.Hour,
			}

			// Act
			err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			if deleted := len(sandboxes.deleted) == 1; deleted != test.deleted {
				t.Fatalf("deleted=%t, want %t: %#v", deleted, test.deleted, sandboxes.deleted)
			}
		})
	}
}

func TestReconcileSandboxesUseCase_ShouldNotPostponeReclaimUnderPressure(t *testing.T) {
	// Pressure brings reclaim forward; it must never push it out. A host
	// configured to reclaim faster than the pressure window keeps its own pace.
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "idle-sandbox",
		Status: agent.SandboxStatusStopped, UpdatedAt: now.Add(-10 * time.Minute),
	}}}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
		sandboxes: sandboxes, clock: fixedClock{now: now},
		idleAfter: 5 * time.Minute,
		disk:      fakeHostDisk{free: 1 << 30}, pressureBelow: 25 << 30,
		idleUnderPressure: time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.deleted) != 1 {
		t.Fatalf("expected the shorter idle window to still apply: %#v", sandboxes.deleted)
	}
}

func TestReconcileSandboxesUseCase_ShouldReclaimBrokenSandbox(t *testing.T) {
	// A broken instance never comes back on its own — Ensure only knows how to
	// start Stopped and reuse Running, so a Broken sandbox would fail with a name
	// conflict on every future round, blocking its subject forever. Deleting it
	// lets Ensure rebuild from scratch next time the role is needed.
	// Arrange: the record still says Running; the provider is the authority.
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "wedged-sandbox",
		Status: agent.SandboxStatusRunning, UpdatedAt: now.Add(-time.Minute),
	}}}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusBroken},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.deleted) != 1 || sandboxes.deleted[0] != "wedged-sandbox" {
		t.Fatalf("expected the broken sandbox to be deleted: %#v", sandboxes.deleted)
	}
	if len(registry.savedSandboxes) != 1 ||
		registry.savedSandboxes[0].Status != agent.SandboxStatusAbsent {
		t.Fatalf("expected the sandbox to be recorded absent: %#v", registry.savedSandboxes)
	}
}

func TestReconcileSandboxesUseCase_ShouldNotReclaimRecentlyStoppedSandbox(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "fresh-sandbox",
		Status: agent.SandboxStatusStopped, UpdatedAt: now.Add(-5 * time.Minute),
	}}}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.deleted) != 0 {
		t.Fatalf("did not expect a recently stopped sandbox to be reclaimed: %#v", sandboxes.deleted)
	}
}

func TestReconcileSandboxesUseCase_ShouldNotReclaimRunningSandbox(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "busy-sandbox",
		Status: agent.SandboxStatusRunning, UpdatedAt: now.Add(-3 * time.Hour),
	}}}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusRunning},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.deleted) != 0 {
		t.Fatalf("did not expect a running sandbox to be reclaimed regardless of age: %#v",
			sandboxes.deleted)
	}
	// Nothing owns it, so it is stopped rather than left burning host resources —
	// but the reclaim clock only starts from that stop.
	if len(sandboxes.forceStopped) != 1 || sandboxes.forceStopped[0] != "busy-sandbox" {
		t.Fatalf("expected the unowned running sandbox to be force-stopped, got %#v",
			sandboxes.forceStopped)
	}
	if len(registry.savedSandboxes) != 1 ||
		registry.savedSandboxes[0].Status != agent.SandboxStatusStopped ||
		!registry.savedSandboxes[0].UpdatedAt.Equal(now) {
		t.Fatalf("expected the record to be stopped and restamped, got %#v", registry.savedSandboxes)
	}
}

func TestReconcileSandboxesUseCase_ShouldDisableReclaimWhenIdleAfterIsZero(t *testing.T) {
	// idleAfter's zero value must mean "disabled", matching the same convention as
	// AgentProfile.MaxRounds, so existing callers that don't set it are unaffected.
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "idle-sandbox",
		Status: agent.SandboxStatusStopped, UpdatedAt: now.Add(-100 * time.Hour),
	}}}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
		sandboxes: sandboxes, clock: fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.deleted) != 0 {
		t.Fatalf("did not expect reclaim with idleAfter disabled: %#v", sandboxes.deleted)
	}
}

func TestReconcileSandboxesUseCase_ShouldContinuePastReclaimFailure(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	idleSince := now.Add(-2 * time.Hour)
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{
		{
			ID: "sandbox-1", ProjectID: 1, Name: "broken-idle-sandbox",
			Status: agent.SandboxStatusStopped, UpdatedAt: idleSince,
		},
		{
			ID: "sandbox-2", ProjectID: 1, Name: "healthy-idle-sandbox",
			Status: agent.SandboxStatusStopped, UpdatedAt: idleSince,
		},
	}}
	sandboxes := &fakeSandboxManager{
		deleteErrFor: "broken-idle-sandbox", deleteErr: fmt.Errorf("provider offline"),
	}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err == nil {
		t.Fatal("expected the broken reclaim's error to be reported")
	}
	if len(registry.savedSandboxes) != 1 ||
		registry.savedSandboxes[0].Name != "healthy-idle-sandbox" ||
		registry.savedSandboxes[0].Status != agent.SandboxStatusAbsent {
		t.Fatalf("expected the healthy sandbox to still be reclaimed: %#v", registry.savedSandboxes)
	}
}

func TestReconcileSandboxesUseCase_ShouldReclaimSandboxOfTerminalSubject(t *testing.T) {
	// A merged issue's sandbox holds 20GB of host disk for work nobody will touch again.
	// Its age is irrelevant: what makes it reclaimable is that the subject is done.
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	merged := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "merged-sandbox", Role: agent.AgentRoleCoding,
		Subject: merged, Status: agent.SandboxStatusStopped, UpdatedAt: now,
	}}}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: 24 * time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{
		ProjectID: 1,
		Terminal:  map[agent.AgentSubject]struct{}{merged: {}},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.deleted) != 1 || sandboxes.deleted[0] != "merged-sandbox" {
		t.Fatalf("expected the finished subject's sandbox to be deleted, got %#v", sandboxes.deleted)
	}
	if len(registry.savedSandboxes) != 1 ||
		registry.savedSandboxes[0].Status != agent.SandboxStatusAbsent {
		t.Fatalf("expected the record to be absent, got %#v", registry.savedSandboxes)
	}
}

func TestReconcileSandboxesUseCase_ShouldStopAndReclaimTerminalRunner(t *testing.T) {
	// Reclaiming has to reach a sandbox the provider still reports running, even though a
	// stale run record claims it: a finished subject cannot be owed a round, and the
	// FSM only permits a delete once the record agrees the instance is stopped.
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	closed := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	registry := &fakeAgentRegistry{
		sandboxes: []agent.Sandbox{{
			ID: "sandbox-1", ProjectID: 1, Name: "closed-sandbox", Role: agent.AgentRoleCoding,
			Subject: closed, Status: agent.SandboxStatusRunning, UpdatedAt: now,
		}},
		runs: []agent.AgentRun{liveRun("run-1", "sandbox-1", closed)},
	}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusRunning},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: 24 * time.Hour,
		maxRuntime: 3 * time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{
		ProjectID: 1,
		Terminal:  map[agent.AgentSubject]struct{}{closed: {}},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.forceStopped) != 1 || sandboxes.forceStopped[0] != "closed-sandbox" {
		t.Fatalf("expected the sandbox to be force-stopped first, got %#v", sandboxes.forceStopped)
	}
	if len(sandboxes.deleted) != 1 || sandboxes.deleted[0] != "closed-sandbox" {
		t.Fatalf("expected the sandbox to be deleted, got %#v", sandboxes.deleted)
	}
	if status := registry.runs[0].Status; status != agent.AgentRunStatusStalled {
		t.Fatalf("expected the run claiming it to be stalled, got %q", status)
	}
}

func TestReconcileSandboxesUseCase_ShouldLeaveSandboxOfActiveSubjectAlone(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	active := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "active-sandbox", Role: agent.AgentRoleCoding,
		Subject: active, Status: agent.SandboxStatusStopped, UpdatedAt: now.Add(-2 * time.Hour),
	}}}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: 24 * time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{
		ProjectID: 1,
		Terminal: map[agent.AgentSubject]struct{}{
			{Kind: agent.AgentSubjectIssue, ID: "other-issue"}: {},
		},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.deleted) != 0 || len(sandboxes.forceStopped) != 0 {
		t.Fatalf("expected an unfinished subject's sandbox to be untouched before idleAfter: %#v %#v",
			sandboxes.deleted, sandboxes.forceStopped)
	}
}

func TestReconcileSandboxesUseCase_ShouldSpareTheSandboxOfASubjectInFlight(t *testing.T) {
	// The runs are read before the sandboxes are listed, so a round dispatched between
	// those two reads is missing from the snapshot while its sandbox is already
	// visible. Judged on the snapshot alone every reclaim below fires on it: it
	// looks unowned, its subject has been closed from the UI so it also looks
	// finished, and its record is old enough to look idle. The in-flight set is
	// what says otherwise.
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	busy := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "busy-sandbox", Role: agent.AgentRoleCoding,
		Subject: busy, Status: agent.SandboxStatusRunning, UpdatedAt: now.Add(-48 * time.Hour),
	}}}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusRunning},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: 24 * time.Hour,
		maxRuntime: 3 * time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{
		ProjectID: 1,
		Terminal:  map[agent.AgentSubject]struct{}{busy: {}},
		InFlight:  map[agent.AgentSubject]struct{}{busy: {}},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.forceStopped) != 0 || len(sandboxes.stopped) != 0 {
		t.Fatalf("stopped a sandbox with a round on it: %#v %#v",
			sandboxes.forceStopped, sandboxes.stopped)
	}
	if len(sandboxes.deleted) != 0 {
		t.Fatalf("deleted a sandbox with a round on it: %#v", sandboxes.deleted)
	}
}

func TestReconcileSandboxesUseCase_ShouldForceStopSandboxRunningPastMaxRuntime(t *testing.T) {
	// The one case that overrides the live-run guard: a run still claiming a sandbox hours
	// after it started is a round that will never report back, and nothing else can
	// see it — a wedged round blocks the tick that would otherwise reconcile it.
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	registry := &fakeAgentRegistry{
		sandboxes: []agent.Sandbox{{
			ID: "sandbox-1", ProjectID: 1, Name: "runaway-sandbox", Role: agent.AgentRoleCoding,
			Subject: subject, Status: agent.SandboxStatusRunning, UpdatedAt: now.Add(-4 * time.Hour),
		}},
		runs: []agent.AgentRun{liveRun("run-1", "sandbox-1", subject)},
	}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusRunning},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: 24 * time.Hour,
		maxRuntime: 3 * time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.forceStopped) != 1 || sandboxes.forceStopped[0] != "runaway-sandbox" {
		t.Fatalf("expected the runaway sandbox to be force-stopped, got %#v", sandboxes.forceStopped)
	}
	if status := registry.runs[0].Status; status != agent.AgentRunStatusStalled {
		t.Fatalf("expected the run that outlived the cap to be stalled, got %q", status)
	}
}

func TestReconcileSandboxesUseCase_ShouldNotStopRunningSandboxAnAgentRunClaims(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	registry := &fakeAgentRegistry{
		sandboxes: []agent.Sandbox{{
			ID: "sandbox-1", ProjectID: 1, Name: "working-sandbox", Role: agent.AgentRoleCoding,
			Subject: subject, Status: agent.SandboxStatusRunning, UpdatedAt: now.Add(-time.Minute),
		}},
		runs: []agent.AgentRun{liveRun("run-1", "sandbox-1", subject)},
	}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusRunning},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: 24 * time.Hour,
		maxRuntime: 3 * time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.forceStopped) != 0 {
		t.Fatalf("expected a claimed sandbox to keep running, got %#v", sandboxes.forceStopped)
	}
	if status := registry.runs[0].Status; status != agent.AgentRunStatusRunning {
		t.Fatalf("expected the live run to be left alone, got %q", status)
	}
}

func TestReconcileSandboxesUseCase_ShouldStallLiveRunsAndStopThemOnRecovery(t *testing.T) {
	// A run left Running by a crash would otherwise shield its leaked sandbox from the
	// unowned check forever, in the case where its subject never needs another round.
	// The first tick of a process has started no round, so nothing live can be real.
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	registry := &fakeAgentRegistry{
		sandboxes: []agent.Sandbox{{
			ID: "sandbox-1", ProjectID: 1, Name: "leaked-sandbox", Role: agent.AgentRoleCoding,
			Subject: subject, Status: agent.SandboxStatusRunning, UpdatedAt: now.Add(-time.Minute),
		}},
		runs: []agent.AgentRun{liveRun("run-1", "sandbox-1", subject)},
	}
	sandboxes := &fakeSandboxManager{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusRunning},
		sandboxes: sandboxes, clock: fixedClock{now: now}, idleAfter: 24 * time.Hour,
		maxRuntime: 3 * time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1, Recover: true})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if status := registry.runs[0].Status; status != agent.AgentRunStatusStalled {
		t.Fatalf("expected the orphaned run to be stalled, got %q", status)
	}
	// Same pass, not the next one: recovery is what makes the sandbox look unowned.
	if len(sandboxes.forceStopped) != 1 || sandboxes.forceStopped[0] != "leaked-sandbox" {
		t.Fatalf("expected the leaked sandbox to be force-stopped, got %#v", sandboxes.forceStopped)
	}
}

// liveRun builds a running AgentRun the fake registry's validation accepts.
func liveRun(runID, sandboxID string, subject agent.AgentSubject) agent.AgentRun {
	return agent.AgentRun{
		ID: runID, ProjectID: 1, SandboxID: sandboxID, Role: agent.AgentRoleCoding,
		Subject: subject, Engine: agent.AgentEngineOpenCode,
		Agent: "anthropic/claude-opus-5", SessionMode: agent.SessionModeFresh,
		Status: agent.AgentRunStatusRunning, Round: 1,
		CreatedAt: time.Date(2026, time.August, 12, 11, 0, 0, 0, time.UTC),
	}
}

func TestReconcileSandboxesUseCase_ShouldDiscardCredentialsWithTheSandbox(t *testing.T) {
	// The credentials are staged under the sandbox's name, so they go with it. Left behind
	// they are a real provider credential on disk for an instance that no longer
	// exists — the one copy nobody is watching.
	// Arrange
	now := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "idle-sandbox", Role: agent.AgentRoleCoding,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
		Status:  agent.SandboxStatusStopped, UpdatedAt: now.Add(-48 * time.Hour),
	}}}
	creds := &fakeAgentCredentials{}
	useCase := ReconcileSandboxesUseCase{
		registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
		sandboxes: &fakeSandboxManager{}, creds: creds, clock: fixedClock{now: now},
		idleAfter: 24 * time.Hour,
	}

	// Act
	err := useCase.Handle(context.Background(), ReconcileSandboxesCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(creds.discarded) != 1 || creds.discarded[0] != "idle-sandbox" {
		t.Fatalf("expected the reclaimed sandbox's credentials to be discarded, got %#v",
			creds.discarded)
	}
}
