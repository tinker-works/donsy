package usecases

import (
	"context"
	"errors"
	"fmt"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
)

func TestEpicWorker_Tick_ShouldAdvanceConceptEpicWithoutUI(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	project := domain.Project{ID: 1, Name: "One"}
	workspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	registry := &fakeAgentRegistry{}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{project}},
		&ListEpicsUseCase{factory: &fakeFactory{workspace: workspace}},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
			clock:     fixedClock{now: now},
		},
		&RunEpicAgentUseCase{
			factory: &fakeFactory{workspace: workspace}, registry: registry,
			sandboxes: &fakeSandboxManager{},
			runtime:   &fakeAgentRuntime{output: "Refined epic."}, builder: fakeCommandBuilder{},
			creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
			issueTreeStore: fakeIssueTreeStore{}, clock: fixedClock{now: now},
		},
		fixedClock{now: now}, time.Minute, IssueLoop{}, nil,
	)

	// Act
	err := worker.TickAndWait(context.Background())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.State != epic.EpicStateReview || len(workspace.detail.Issues) != 2 {
		t.Fatalf("worker did not advance epic: %#v", workspace.detail)
	}
}

func TestEpicWorker_Tick_ShouldAdvanceEligibleWorkPastAnUnrelatedReconcileFailure(t *testing.T) {
	// Arrange: an abandoned issue sandbox fails inspection while the concept epic
	// remains safe to run because it has no unresolved sandbox.
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	project := domain.Project{ID: 1, Name: "One"}
	workspace := &fakeWorkspace{detail: testEpic(epic.EpicStateConcept), agentSettings: testAgentSettings()}
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "stale", ProjectID: project.ID, Name: "stale", Role: agent.AgentRoleCoding,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "stale"},
		Status:  agent.SandboxStatusStopped,
	}}}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{project}},
		&ListEpicsUseCase{factory: &fakeFactory{workspace: workspace}},
		&ReconcileSandboxesUseCase{
			registry: registry,
			inspector: fakeSelectiveSandboxInspector{
				failFor: "stale", err: fmt.Errorf("provider unavailable"),
			},
			clock: fixedClock{now: now},
		},
		&RunEpicAgentUseCase{
			factory: &fakeFactory{workspace: workspace}, registry: registry,
			sandboxes: &fakeSandboxManager{}, runtime: &fakeAgentRuntime{output: "Refined epic."},
			builder: fakeCommandBuilder{}, creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
			issueTreeStore: fakeIssueTreeStore{}, clock: fixedClock{now: now},
		},
		fixedClock{now: now}, time.Minute, IssueLoop{}, nil,
	)

	// Act
	err := worker.TickAndWait(context.Background())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.State != epic.EpicStateReview {
		t.Fatalf("expected eligible epic to advance, got %q", workspace.detail.State)
	}
}

func TestEpicWorker_Tick_ShouldOpenBranchesAndRunACodingRound(t *testing.T) {
	// Arrange: a Ready epic whose issue has no pull request yet. One tick
	// should cut the branch and immediately code against it.
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	project := domain.Project{ID: 1, Name: "One"}
	workspace := &fakeWorkspace{
		detail:        epicForRole(epic.EpicStateReady, epic.IssueStateOpen),
		agentSettings: issueAgentSettings(),
		repositories:  []string{"acme/widgets"},
	}
	factory := &fakeFactory{workspace: workspace}
	code := newFakeCodeWorkspace()
	registry := &fakeAgentRegistry{}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{project}},
		&ListEpicsUseCase{factory: factory},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
			clock:     fixedClock{now: now},
		},
		&RunEpicAgentUseCase{factory: factory, registry: registry, clock: fixedClock{now: now}},
		fixedClock{now: now}, time.Minute,
		IssueLoop{
			GetEpic:          &GetEpicUseCase{factory: factory},
			OpenPullRequests: &OpenPullRequestsUseCase{factory: factory, code: code},
			RunIssueAgent: &RunIssueAgentUseCase{
				factory: factory, registry: registry, sandboxes: &fakeSandboxManager{},
				runtime: &fakeAgentRuntime{output: "Implemented."}, builder: fakeCommandBuilder{},
				creds: &fakeAgentCredentials{}, code: code, repos: &fakeRepositoryWorkspace{},
				issueTreeStore: fakeIssueTreeStore{},
				clock:          fixedClock{now: now},
			},
			CompleteEpic:   &CompleteEpicUseCase{factory: factory},
			ReviewApproved: &ReviewApprovedBranchesUseCase{factory: factory, code: code},
		},
		nil,
	)

	// Act
	if err := worker.TickAndWait(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(workspace.detail.PullRequests) != 1 {
		t.Fatalf("expected a pull request to be opened, got %+v", workspace.detail.PullRequests)
	}
	record := workspace.detail.PullRequests[0]
	if record.Rounds != 1 {
		t.Fatalf("expected a coding round to have run, got %+v", record)
	}
	if len(code.pushed) != 2 {
		t.Fatalf("expected the branch cut and the round published, got %v", code.pushed)
	}
}

func TestEpicWorker_Tick_ShouldCompleteAnEpicOnceEverythingMerged(t *testing.T) {
	// Arrange: Ready is otherwise a dead end — nothing else moves an epic on.
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	project := domain.Project{ID: 1, Name: "One"}
	detail := epicForRole(epic.EpicStateReady, epic.IssueStateMerged)
	workspace := &fakeWorkspace{detail: detail, agentSettings: issueAgentSettings()}
	factory := &fakeFactory{workspace: workspace}
	registry := &fakeAgentRegistry{}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{project}},
		&ListEpicsUseCase{factory: factory},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
			clock:     fixedClock{now: now},
		},
		&RunEpicAgentUseCase{factory: factory, registry: registry, clock: fixedClock{now: now}},
		fixedClock{now: now}, time.Minute,
		IssueLoop{
			GetEpic:          &GetEpicUseCase{factory: factory},
			OpenPullRequests: &OpenPullRequestsUseCase{factory: factory, code: newFakeCodeWorkspace()},
			RunIssueAgent:    &RunIssueAgentUseCase{factory: factory, registry: registry},
			CompleteEpic:     &CompleteEpicUseCase{factory: factory},
			ReviewApproved: &ReviewApprovedBranchesUseCase{
				factory: factory, code: newFakeCodeWorkspace(),
			},
		},
		nil,
	)

	// Act
	if err := worker.TickAndWait(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Assert
	if workspace.detail.State != epic.EpicStateDone {
		t.Fatalf("expected the epic to be done, got %q", workspace.detail.State)
	}
}

func TestEpicWorker_Tick_ShouldDriveDraftingOnlyWithoutAnIssueLoop(t *testing.T) {
	// Arrange: the worker predates the coding roles and must still run
	// without them rather than panicking on a nil use case.
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	project := domain.Project{ID: 1, Name: "One"}
	workspace := &fakeWorkspace{
		detail:        epicForRole(epic.EpicStateReady, epic.IssueStateOpen),
		agentSettings: issueAgentSettings(),
	}
	factory := &fakeFactory{workspace: workspace}
	registry := &fakeAgentRegistry{}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{project}},
		&ListEpicsUseCase{factory: factory},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
			clock:     fixedClock{now: now},
		},
		&RunEpicAgentUseCase{factory: factory, registry: registry, clock: fixedClock{now: now}},
		fixedClock{now: now}, time.Minute, IssueLoop{}, nil,
	)

	// Act
	err := worker.TickAndWait(context.Background())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.detail.PullRequests) != 0 {
		t.Fatalf("expected no execution work, got %+v", workspace.detail.PullRequests)
	}
}

func TestEpicWorker_Tick_ShouldKeepSweepingAfterAProjectFails(t *testing.T) {
	// Arrange: the first project's epic has no repository scope, which
	// RunEpicAgentUseCase refuses. The second project must still advance.
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	broken := testEpic(epic.EpicStateConcept)
	broken.Repositories = nil
	brokenWorkspace := &fakeWorkspace{detail: broken, agentSettings: testAgentSettings()}
	healthyWorkspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	factory := &fakeFactory{byPath: map[string]*fakeWorkspace{
		"Broken":  brokenWorkspace,
		"Healthy": healthyWorkspace,
	}}
	registry := &fakeAgentRegistry{}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{
			{ID: 1, Name: "Broken"},
			{ID: 2, Name: "Healthy"},
		}},
		&ListEpicsUseCase{factory: factory},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
			clock:     fixedClock{now: now},
		},
		&RunEpicAgentUseCase{
			factory: factory, registry: registry, sandboxes: &fakeSandboxManager{},
			runtime: &fakeAgentRuntime{output: "Refined epic."}, builder: fakeCommandBuilder{},
			creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
			issueTreeStore: fakeIssueTreeStore{}, clock: fixedClock{now: now},
		},
		fixedClock{now: now}, time.Minute, IssueLoop{}, nil,
	)

	// Act
	err := worker.TickAndWait(context.Background())

	// Assert: the broken epic is reported, but the healthy one still advanced.
	if err == nil {
		t.Fatal("expected the broken project to be reported")
	}
	if healthyWorkspace.detail.State != epic.EpicStateReview {
		t.Fatalf("a failing project stopped the sweep: %#v", healthyWorkspace.detail)
	}
}

// The sweep also has to survive a project failing before its epics are ever
// listed. Reconcile runs first and skips to the next project on error, which is
// a different path out of the loop than an epic that fails to advance.
func TestEpicWorker_Tick_ShouldKeepSweepingAfterAProjectsReconcileFails(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	broken := domain.Project{ID: 1, Name: "Broken"}
	healthy := domain.Project{ID: 2, Name: "Healthy"}
	brokenWorkspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	healthyWorkspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	factory := &fakeFactory{byPath: map[string]*fakeWorkspace{
		broken.Name:  brokenWorkspace,
		healthy.Name: healthyWorkspace,
	}}
	registry := &fakeAgentRegistry{
		listSandboxesErr: fmt.Errorf("provider offline"), listSandboxesErrFor: broken.ID,
	}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{broken, healthy}},
		&ListEpicsUseCase{factory: factory},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
			clock:     fixedClock{now: now},
		},
		&RunEpicAgentUseCase{
			factory: factory, registry: registry, sandboxes: &fakeSandboxManager{},
			runtime: &fakeAgentRuntime{output: "Refined epic."}, builder: fakeCommandBuilder{},
			creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
			issueTreeStore: fakeIssueTreeStore{}, clock: fixedClock{now: now},
		},
		fixedClock{now: now}, time.Minute, IssueLoop{}, nil,
	)

	// Act
	err := worker.TickAndWait(context.Background())

	// Assert: the failure names the project it came from, and the healthy
	// project's epic still advanced past it.
	if err == nil || !strings.Contains(err.Error(), broken.Name) {
		t.Fatalf("expected the broken project to be named in the error, got %v", err)
	}
	if healthyWorkspace.detail.State != epic.EpicStateReview {
		t.Fatalf("a failing reconcile stopped the sweep: %#v", healthyWorkspace.detail)
	}
}

// hangingSandboxInspector never answers, like a provider whose backend wedged. It
// honours ctx, which is exactly what the worker's per-project reconcile
// deadline cancels.
type hangingSandboxInspector struct{}

func (hangingSandboxInspector) Inspect(
	ctx context.Context, _ agent.SandboxRef,
) (agent.SandboxStatus, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func TestEpicWorker_Tick_ShouldBoundAHangingReconcileAndKeepSweeping(t *testing.T) {
	// Arrange: the broken project owns a sandbox whose inspect never returns. Without
	// the per-project deadline this tick would never finish, and neither would
	// any project scheduled after the broken one.
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	broken := domain.Project{ID: 1, Name: "Broken"}
	healthy := domain.Project{ID: 2, Name: "Healthy"}
	brokenWorkspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	healthyWorkspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	factory := &fakeFactory{byPath: map[string]*fakeWorkspace{
		broken.Name:  brokenWorkspace,
		healthy.Name: healthyWorkspace,
	}}
	registry := &fakeAgentRegistry{
		sandboxes: []agent.Sandbox{{
			ID: "sandbox-1", ProjectID: broken.ID, Name: "gm-wedged", Role: agent.AgentRoleRefiner,
			Subject: agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: brokenWorkspace.detail.ID},
			Status:  agent.SandboxStatusRunning,
		}},
		sandboxesFor: broken.ID,
	}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{broken, healthy}},
		&ListEpicsUseCase{factory: factory},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: hangingSandboxInspector{},
			clock:     fixedClock{now: now},
		},
		&RunEpicAgentUseCase{
			factory: factory, registry: registry, sandboxes: &fakeSandboxManager{},
			runtime: &fakeAgentRuntime{output: "Refined epic."}, builder: fakeCommandBuilder{},
			creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
			issueTreeStore: fakeIssueTreeStore{}, clock: fixedClock{now: now},
		},
		fixedClock{now: now}, time.Minute, IssueLoop{}, nil,
	)
	worker.reconcileBudget = 20 * time.Millisecond

	// Act
	err := worker.TickAndWait(context.Background())

	// Assert: the deadline stopped the hung reconcile, the failure names the
	// project it came from, and the healthy project's epic still advanced.
	if err == nil || !strings.Contains(err.Error(), broken.Name) {
		t.Fatalf("expected the broken project to be named in the error, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the reconcile deadline to stop the hang, got %v", err)
	}
	if healthyWorkspace.detail.State != epic.EpicStateReview {
		t.Fatalf("a hanging reconcile stopped the sweep: %#v", healthyWorkspace.detail)
	}
}

func TestEpicWorker_Tick_ShouldBackOffAfterAFailedRound(t *testing.T) {
	// A subject whose round failed is not retried on the very next tick: ticking
	// every few seconds would retry a systemic failure a dozen times a minute
	// for as long as the process lives.
	// Arrange
	start := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	clockNow := start
	clock := movingClock{now: &clockNow}
	project := domain.Project{ID: 1, Name: "One"}
	workspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	factory := &fakeFactory{workspace: workspace}
	registry := &fakeAgentRegistry{}
	manager := &fakeSandboxManager{ensureErr: fmt.Errorf("disk full")}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{project}},
		&ListEpicsUseCase{factory: factory},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
			clock:     clock,
		},
		&RunEpicAgentUseCase{
			factory: factory, registry: registry, sandboxes: manager,
			runtime: &fakeAgentRuntime{output: "Refined epic."}, builder: fakeCommandBuilder{},
			creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
			issueTreeStore: fakeIssueTreeStore{}, clock: clock,
		},
		clock, time.Minute, IssueLoop{}, nil,
	)

	// Act: the first tick fails the round.
	if err := worker.TickAndWait(context.Background()); err == nil {
		t.Fatal("expected the failing round to be reported")
	}
	attempts := len(manager.ensured)

	// Act: a tick inside the hold must not retry the subject.
	clockNow = start.Add(time.Second)
	_ = worker.TickAndWait(context.Background())
	held := len(manager.ensured)

	// Act: a tick past the hold must.
	clockNow = start.Add(retryBase + time.Second)
	_ = worker.TickAndWait(context.Background())

	// Assert
	if held != attempts {
		t.Fatalf("expected the failed subject to be held back, got %d attempts", held)
	}
	if len(manager.ensured) != attempts+1 {
		t.Fatalf("expected a retry once the hold elapsed, got %d attempts", len(manager.ensured))
	}
}

func TestEpicWorker_Tick_ShouldClearTheHoldAfterASuccess(t *testing.T) {
	// Arrange: same shape as the backoff test, but the failure is transient.
	start := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	clockNow := start
	clock := movingClock{now: &clockNow}
	project := domain.Project{ID: 1, Name: "One"}
	workspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	factory := &fakeFactory{workspace: workspace}
	registry := &fakeAgentRegistry{}
	manager := &fakeSandboxManager{ensureErr: fmt.Errorf("disk full")}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{project}},
		&ListEpicsUseCase{factory: factory},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
			clock:     clock,
		},
		&RunEpicAgentUseCase{
			factory: factory, registry: registry, sandboxes: manager,
			runtime: &fakeAgentRuntime{output: "Refined epic."}, builder: fakeCommandBuilder{},
			creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
			issueTreeStore: fakeIssueTreeStore{}, clock: clock,
		},
		clock, time.Minute, IssueLoop{}, nil,
	)
	if err := worker.TickAndWait(context.Background()); err == nil {
		t.Fatal("expected the failing round to be reported")
	}

	// Act: the failure passes, and the retry after the hold succeeds.
	manager.ensureErr = nil
	clockNow = start.Add(retryBase + time.Second)
	if err := worker.TickAndWait(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Assert: success cleared the ledger, so the next failure starts at the
	// base hold instead of a doubled one.
	if len(worker.retries) != 0 {
		t.Fatalf("expected the hold to be cleared, got %#v", worker.retries)
	}
	if workspace.detail.State != epic.EpicStateReview {
		t.Fatalf("expected the retried round to advance the epic: %#v", workspace.detail)
	}
}

// movingClock lets a test step past approvedBranchSweepInterval.
type movingClock struct{ now *time.Time }

func (c movingClock) Now() time.Time { return *c.now }

// Ticking is cheap and frequent; fetching the approved branches is neither, so
// it keeps its own slower beat inside the worker.
func TestEpicWorker_Tick_ShouldFetchApprovedBranchesOnlyOnItsOwnInterval(t *testing.T) {
	// Arrange
	start := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	clockNow := start
	clock := movingClock{now: &clockNow}
	project := domain.Project{ID: 1, Name: "One"}
	workspace := &fakeWorkspace{
		detail:        approvedEpic(),
		agentSettings: issueAgentSettings(),
		repositories:  []string{"acme/widgets"},
	}
	factory := &fakeFactory{workspace: workspace}
	code := newFakeCodeWorkspace()
	code.branchState = agent_runtime.BranchState{Head: "head1234"}
	registry := &fakeAgentRegistry{}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{project}},
		&ListEpicsUseCase{factory: factory},
		&ReconcileSandboxesUseCase{
			registry: registry, inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
			clock: clock,
		},
		&RunEpicAgentUseCase{factory: factory, registry: registry, clock: clock},
		clock, 5*time.Second,
		IssueLoop{
			GetEpic:          &GetEpicUseCase{factory: factory},
			OpenPullRequests: &OpenPullRequestsUseCase{factory: factory, code: code},
			RunIssueAgent:    &RunIssueAgentUseCase{factory: factory, registry: registry},
			CompleteEpic:     &CompleteEpicUseCase{factory: factory},
			ReviewApproved:   &ReviewApprovedBranchesUseCase{factory: factory, code: code},
		},
		nil,
	)

	// Act: the first tick sweeps, several quick ones after it must not.
	for range 4 {
		if err := worker.TickAndWait(context.Background()); err != nil {
			t.Fatal(err)
		}
		clockNow = clockNow.Add(5 * time.Second)
	}

	// Assert
	if len(code.inspected) != 1 {
		t.Fatalf("expected one fetch across four quick ticks, got %d", len(code.inspected))
	}

	// Act: once the interval has passed it sweeps again.
	clockNow = start.Add(approvedBranchSweepInterval)
	if err := worker.TickAndWait(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(code.inspected) != 2 {
		t.Fatalf("expected a second fetch after the interval, got %d", len(code.inspected))
	}
}

func TestEpicWorker_Tick_ShouldReclaimTheSandboxesOfFinishedWork(t *testing.T) {
	// The epics are read before reconciliation so it knows which subjects are done.
	// A merged issue's sandbox holds 20GB of host disk, and waiting out the idle clock for
	// work that will never run again is what made that pile up.
	// Arrange
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	project := domain.Project{ID: 1, Name: "One"}
	finished := testEpic(epic.EpicStateClosed)
	workspace := &fakeWorkspace{detail: finished, agentSettings: testAgentSettings()}
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "closed-epic-sandbox", Role: agent.AgentRoleRefiner,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: finished.ID},
		Status:  agent.SandboxStatusStopped, UpdatedAt: now,
	}}}
	sandboxes := &fakeSandboxManager{}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{project}},
		&ListEpicsUseCase{factory: &fakeFactory{workspace: workspace}},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: fakeSandboxInspector{status: agent.SandboxStatusStopped},
			sandboxes: sandboxes,
			clock:     fixedClock{now: now},
			idleAfter: 24 * time.Hour,
		},
		&RunEpicAgentUseCase{
			factory: &fakeFactory{workspace: workspace}, registry: registry, sandboxes: sandboxes,
			runtime: &fakeAgentRuntime{}, builder: fakeCommandBuilder{},
			creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
			issueTreeStore: fakeIssueTreeStore{}, clock: fixedClock{now: now},
		},
		fixedClock{now: now}, time.Minute, IssueLoop{}, nil,
	)

	// Act
	err := worker.TickAndWait(context.Background())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.deleted) != 1 || sandboxes.deleted[0] != "closed-epic-sandbox" {
		t.Fatalf("expected the closed epic's sandbox to be reclaimed, got %#v", sandboxes.deleted)
	}
}

func TestEpicWorker_Tick_ShouldRecoverOnlyOnTheFirstTick(t *testing.T) {
	// Runs left live by a process that died are only recognisable as leftovers before
	// this process has started a round of its own. After that, a live run may be the
	// round the worker is in the middle of, and stalling it would be wrong.
	// Arrange
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	project := domain.Project{ID: 1, Name: "One"}
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateReady), agentSettings: testAgentSettings(),
	}
	subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{liveRun("run-1", "sandbox-1", subject)}}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{project}},
		&ListEpicsUseCase{factory: &fakeFactory{workspace: workspace}},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
			sandboxes: &fakeSandboxManager{},
			clock:     fixedClock{now: now},
			idleAfter: 24 * time.Hour,
		},
		&RunEpicAgentUseCase{
			factory: &fakeFactory{workspace: workspace}, registry: registry,
			sandboxes: &fakeSandboxManager{}, runtime: &fakeAgentRuntime{},
			builder: fakeCommandBuilder{}, creds: &fakeAgentCredentials{},
			repos: &fakeRepositoryWorkspace{}, issueTreeStore: fakeIssueTreeStore{},
			clock: fixedClock{now: now},
		},
		fixedClock{now: now}, time.Minute, IssueLoop{}, nil,
	)

	// Act
	if err := worker.TickAndWait(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Assert: the leftover was reaped.
	if status := registry.runs[0].Status; status != agent.AgentRunStatusStalled {
		t.Fatalf("expected the first tick to stall the leftover run, got %q", status)
	}

	// Act again with a fresh live run, standing in for one this process started.
	registry.runs = []agent.AgentRun{liveRun("run-2", "sandbox-1", subject)}
	if err := worker.TickAndWait(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Assert
	if status := registry.runs[0].Status; status != agent.AgentRunStatusRunning {
		t.Fatalf("expected later ticks to leave a live run alone, got %q", status)
	}
}

// draftingWorker builds a worker whose only work is one epic's drafting round,
// with runtime driving what that round does. It is the smallest arrangement
// that actually dispatches, which is what the scheduling tests below need.
func draftingWorker(
	t *testing.T, runtime *fakeAgentRuntime,
) (*EpicWorker, *fakeAgentRegistry) {
	t.Helper()
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	project := domain.Project{ID: 1, Name: "One"}
	workspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	registry := &fakeAgentRegistry{}
	worker := NewEpicWorker(
		&fakeRegistry{projects: []domain.Project{project}},
		&ListEpicsUseCase{factory: &fakeFactory{workspace: workspace}},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
			sandboxes: &fakeSandboxManager{},
			clock:     fixedClock{now: now},
		},
		&RunEpicAgentUseCase{
			factory: &fakeFactory{workspace: workspace}, registry: registry,
			sandboxes: &fakeSandboxManager{}, runtime: runtime, builder: fakeCommandBuilder{},
			creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
			issueTreeStore: fakeIssueTreeStore{}, clock: fixedClock{now: now},
		},
		fixedClock{now: now}, time.Minute, IssueLoop{}, nil,
	)
	return worker, registry
}

func TestEpicWorker_Tick_ShouldNotDispatchASubjectThatIsAlreadyRunning(t *testing.T) {
	// Nothing else prevents this. Handle reads the subject's runs and reaps the
	// live ones as orphans rather than standing down, so a second dispatch would
	// stall the first round's own record and then race it for the same sandbox name,
	// the same checkout and the same registry row.
	// Arrange: a round that blocks until the test lets it finish.
	started := make(chan struct{})
	release := make(chan struct{})
	var rounds atomic.Int32
	runtime := &fakeAgentRuntime{run: func(ctx context.Context, _ string) (string, error) {
		if rounds.Add(1) == 1 {
			close(started)
		}
		<-release
		return "Refined epic.", nil
	}}
	worker, _ := draftingWorker(t, runtime)

	// Act: the first tick dispatches and returns without waiting; the next two
	// find the same epic still eligible, because nothing has written its new
	// state yet.
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-started
	for range 2 {
		if err := worker.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	// Assert
	if got := rounds.Load(); got != 1 {
		t.Fatalf("dispatched %d rounds for one subject, want 1", got)
	}
	close(release)
	// The first call drains the round that was in flight; the second is the one
	// that can dispatch the subject again.
	for range 2 {
		if err := worker.TickAndWait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := rounds.Load(); got != 2 {
		t.Fatalf("subject was not dispatched again after its round finished, ran %d", got)
	}
}

func TestEpicWorker_Tick_ShouldNotBlockOnARunningRound(t *testing.T) {
	// This is the point of dispatching. Inline, a round that takes an hour took
	// the whole worker with it: no reconciliation, no reclamation, and every
	// other project waiting behind it.
	// Arrange
	started := make(chan struct{})
	release := make(chan struct{})
	runtime := &fakeAgentRuntime{run: func(context.Context, string) (string, error) {
		close(started)
		<-release
		return "Refined epic.", nil
	}}
	worker, _ := draftingWorker(t, runtime)
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-started

	// Act: a tick while that round is still inside the agent.
	ticked := make(chan error, 1)
	go func() { ticked <- worker.Tick(context.Background()) }()

	// Assert
	select {
	case err := <-ticked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Tick blocked behind a round that had not finished")
	}
	close(release)
	if err := worker.TickAndWait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestEpicWorker_Run_ShouldWaitForRoundsBeforeReturning(t *testing.T) {
	// The caller closes the registry once Run returns. A round still persisting
	// its terminal status when that happens is left live for the next launch to
	// reap, which is the failure the wait exists to prevent.
	// Arrange
	started := make(chan struct{})
	finished := make(chan struct{})
	runtime := &fakeAgentRuntime{run: func(ctx context.Context, _ string) (string, error) {
		close(started)
		<-ctx.Done()
		// Stand in for the work a cancelled round still has to do: stop its sandbox
		// and record why it ended.
		time.Sleep(50 * time.Millisecond)
		close(finished)
		return "", ctx.Err()
	}}
	worker, _ := draftingWorker(t, runtime)
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		worker.Run(ctx, func(error) {})
	}()
	<-started

	// Act
	cancel()

	// Assert
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
	select {
	case <-finished:
	default:
		t.Fatal("Run returned while a round was still finishing")
	}
}

func TestEpicWorker_Tick_ShouldHoldRoundsToTheConcurrencyCeiling(t *testing.T) {
	// Reserve shells out to the provider on every call, so without a ceiling a
	// backlog of eligible subjects spawns a goroutine and a subprocess each just
	// to be turned away.
	// Arrange: more subjects than the ceiling, none of whose rounds finish.
	now := time.Date(2026, time.August, 12, 16, 0, 0, 0, time.UTC)
	var live atomic.Int32
	release := make(chan struct{})
	runtime := &fakeAgentRuntime{run: func(context.Context, string) (string, error) {
		live.Add(1)
		<-release
		return "Refined epic.", nil
	}}
	projects := make([]domain.Project, 0, maxConcurrentRounds+4)
	factory := &fakeFactory{byPath: map[string]*fakeWorkspace{}}
	registry := &fakeAgentRegistry{}
	for i := range maxConcurrentRounds + 4 {
		path := fmt.Sprintf("/projects/%d", i)
		projects = append(projects, domain.Project{
			ID: uint(i + 1), Name: path,
		})
		detail := testEpic(epic.EpicStateConcept)
		detail.ID = fmt.Sprintf("epic-%d", i)
		factory.byPath[path] = &fakeWorkspace{
			detail: detail, agentSettings: testAgentSettings(),
		}
	}
	worker := NewEpicWorker(
		&fakeRegistry{projects: projects},
		&ListEpicsUseCase{factory: factory},
		&ReconcileSandboxesUseCase{
			registry:  registry,
			inspector: fakeSandboxInspector{status: agent.SandboxStatusAbsent},
			sandboxes: &fakeSandboxManager{}, clock: fixedClock{now: now},
		},
		&RunEpicAgentUseCase{
			factory: factory, registry: registry, sandboxes: &fakeSandboxManager{},
			runtime: runtime, builder: fakeCommandBuilder{},
			creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
			issueTreeStore: fakeIssueTreeStore{}, clock: fixedClock{now: now},
		},
		fixedClock{now: now}, time.Minute, IssueLoop{}, nil,
	)

	// Act
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Assert: the subjects past the ceiling stay eligible for a later tick
	// rather than piling up as goroutines.
	if got := len(worker.inflight); got != maxConcurrentRounds {
		t.Fatalf("dispatched %d rounds, want the ceiling of %d", got, maxConcurrentRounds)
	}
	close(release)
	if err := worker.TickAndWait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
