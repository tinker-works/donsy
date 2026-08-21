package usecases

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

// fakeSandboxManager is reached by every dispatched round at once, so it is guarded
// the way the runtime client it stands in for is.
type fakeSandboxManager struct {
	mu      sync.Mutex
	ensured []agent_runtime.SandboxSpec
	started []string
	stopped []string
	deleted []string
	// forceStopped records StopNow calls separately from Stop, so a test can tell a
	// graceful stop from a power cut rather than only that stopping happened.
	forceStopped []string

	ensureErr error
	startErr  error
	stopErr   error

	// deleteErr, when set, is returned by Delete. If deleteErrFor is non-empty, the
	// error only applies to that sandbox name, letting tests fail one sandbox's reclaim while
	// leaving others healthy.
	deleteErr    error
	deleteErrFor string

	// recreated makes Ensure report that it had to build the instance, which is
	// what tells a round its OpenCode session is gone. False is "reused".
	recreated bool

	// admitted defaults to true (via zero-value handling in Reserve) so existing
	// tests that don't care about admission control are unaffected.
	admitted     *bool
	admitErr     error
	checkedSpecs []agent_runtime.SandboxSpec
	// released counts the reservations handed back, so a test can tell a round
	// that let go of its share from one that leaked it.
	released int
}

func (m *fakeSandboxManager) Ensure(
	_ context.Context, spec agent_runtime.SandboxSpec,
) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensured = append(m.ensured, spec)
	if m.ensureErr != nil {
		return false, m.ensureErr
	}
	// Reusing the instance is the default so that every test that does not care
	// about session continuity keeps the behaviour it was written against.
	return m.recreated, nil
}

func (m *fakeSandboxManager) Start(_ context.Context, ref agent.SandboxRef) error {
	name := ref.Name
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = append(m.started, name)
	if m.startErr != nil {
		return m.startErr
	}
	return nil
}

func (m *fakeSandboxManager) Stop(_ context.Context, ref agent.SandboxRef) error {
	name := ref.Name
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = append(m.stopped, name)
	if m.stopErr != nil {
		return m.stopErr
	}
	return nil
}

func (m *fakeSandboxManager) StopNow(_ context.Context, ref agent.SandboxRef) error {
	name := ref.Name
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forceStopped = append(m.forceStopped, name)
	if m.stopErr != nil {
		return m.stopErr
	}
	return nil
}

func (m *fakeSandboxManager) Delete(_ context.Context, ref agent.SandboxRef) error {
	name := ref.Name
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted = append(m.deleted, name)
	if m.deleteErr != nil && (m.deleteErrFor == "" || m.deleteErrFor == name) {
		return m.deleteErr
	}
	return nil
}

func (m *fakeSandboxManager) Reserve(
	_ context.Context, spec agent_runtime.SandboxSpec,
) (func(), bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkedSpecs = append(m.checkedSpecs, spec)
	if m.admitErr != nil {
		return nil, false, m.admitErr
	}
	if m.admitted != nil && !*m.admitted {
		return nil, false, nil
	}
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.released++
	}, true, nil
}

type fakeAgentRuntime struct {
	mu     sync.Mutex
	output string
	argv   []string
	runID  string
	// run, when set, replaces the canned output so a test can observe the
	// round's context — cancellation is the only way to see it.
	run func(ctx context.Context, runID string) (string, error)
}

func (r *fakeAgentRuntime) Run(
	ctx context.Context, _ agent.SandboxRef, runID string, argv []string,
) (string, error) {
	r.mu.Lock()
	r.argv, r.runID = argv, runID
	run, output := r.run, r.output
	r.mu.Unlock()
	if run != nil {
		return run(ctx, runID)
	}
	return output, nil
}

type fakeCommandBuilder struct {
	invocations *[]application.AgentInvocation
}

func (builder fakeCommandBuilder) Command(
	invocation application.AgentInvocation,
) ([]string, error) {
	if builder.invocations != nil {
		*builder.invocations = append(*builder.invocations, invocation)
	}
	argv := []string{"opencode", "run", invocation.Prompt}
	if invocation.Run.SessionMode == agent.SessionModeContinue {
		argv = append(argv, "--continue")
	}
	return argv, nil
}

func (fakeCommandBuilder) ExtractAnswer(output string) string { return strings.TrimSpace(output) }

// ParseTranscript keeps the fake honest about the shape without reimplementing
// an engine's format: one text entry per non-blank line.
func (fakeCommandBuilder) ParseTranscript(output string) []agent.TranscriptEntry {
	var entries []agent.TranscriptEntry
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		entries = append(entries, agent.TranscriptEntry{
			Kind: agent.TranscriptText, Text: strings.TrimSpace(line),
		})
	}
	return entries
}

func (fakeCommandBuilder) ReviewApproved(answer string) bool {
	return strings.Contains(answer, "VERDICT: approve")
}

// ParseUsage reports a fixed figure so tests can assert the round persisted
// what the builder summed.
func (fakeCommandBuilder) ParseUsage(string) agent.RunUsage {
	return agent.RunUsage{TokensIn: 11, TokensOut: 7, CostUSD: 0.03}
}

// emptyIssueTreeStore stands in for a refiner that returned without writing any issue
// files. The host wrote root.md itself, so the tree still reads back as a valid
// epic — one with nothing planned in it.
type emptyIssueTreeStore struct{}

func (emptyIssueTreeStore) Write(string, epic.Epic) (string, error) { return "/tmp/epic", nil }

func (emptyIssueTreeStore) Read(_ string, detail epic.Epic) (epic.Epic, error) {
	return detail, nil
}

func TestRunEpicAgentUseCase_ShouldRejectARefineThatPlannedNoIssues(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	registry := &fakeAgentRegistry{}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry,
		sandboxes: &fakeSandboxManager{}, runtime: &fakeAgentRuntime{output: "Nothing to do."},
		builder: fakeCommandBuilder{}, creds: &fakeAgentCredentials{},
		repos: &fakeRepositoryWorkspace{}, issueTreeStore: emptyIssueTreeStore{},
		clock: fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert: the round fails rather than passing an empty plan downstream, and
	// the epic stays in Refine so the next tick can try again.
	if err == nil || !strings.Contains(err.Error(), "no issues") {
		t.Fatalf("expected the empty refinement to be rejected, got %v", err)
	}
	if workspace.detail.State != epic.EpicStateRefine {
		t.Fatalf("expected the epic to stay in Refine, got %q", workspace.detail.State)
	}
	last := registry.runs[len(registry.runs)-1]
	if last.Status != agent.AgentRunStatusFailed {
		t.Fatalf("expected the run to be recorded failed, got %q", last.Status)
	}
}

// renamingIssueTreeStore stands in for a refiner that rewrote root.md's front matter:
// root.md holds the epic's own title and brief, so the tree read back carries a
// new name as well as new issues. Keeping the aggregate and its root issue in
// step is the store's job; what matters here is that the round persists more
// than the issue list.
type renamingIssueTreeStore struct{ fakeIssueTreeStore }

func (s renamingIssueTreeStore) Read(path string, detail epic.Epic) (epic.Epic, error) {
	refined, err := s.fakeIssueTreeStore.Read(path, detail)
	if err != nil {
		return epic.Epic{}, err
	}
	if err := refined.SetTitle("Extract the cart total"); err != nil {
		return epic.Epic{}, err
	}
	if err := refined.SetBody("The refined brief."); err != nil {
		return epic.Epic{}, err
	}
	return refined, nil
}

func TestRunEpicAgentUseCase_ShouldPersistTheTitleAndBodyARefinerChanged(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{},
		sandboxes: &fakeSandboxManager{}, runtime: &fakeAgentRuntime{output: "Renamed the epic."},
		builder: fakeCommandBuilder{}, creds: &fakeAgentCredentials{},
		repos: &fakeRepositoryWorkspace{}, issueTreeStore: renamingIssueTreeStore{},
		clock: fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.Title != "Extract the cart total" ||
		workspace.detail.Body != "The refined brief." {
		t.Fatalf("refined title and brief were not persisted: %#v", workspace.detail)
	}
	if workspace.detail.State != epic.EpicStateReview {
		t.Fatalf("expected the renamed epic to go to review, got %q", workspace.detail.State)
	}
}

func TestRunEpicAgentUseCase_ShouldRefineThenReviewWithSeparateSandboxes(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: testAgentSettings(),
	}
	registry := &fakeAgentRegistry{}
	sandboxes := &fakeSandboxManager{}
	runtime := &fakeAgentRuntime{output: "Refiner completed the tree."}
	useCase := RunEpicAgentUseCase{
		factory:        &fakeFactory{workspace: workspace},
		registry:       registry,
		sandboxes:      sandboxes,
		runtime:        runtime,
		builder:        fakeCommandBuilder{},
		creds:          &fakeAgentCredentials{},
		repos:          &fakeRepositoryWorkspace{},
		issueTreeStore: fakeIssueTreeStore{},
		clock:          fixedClock{now: now},
	}
	refinerSandbox := testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner)

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    refinerSandbox,
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.State != epic.EpicStateReview ||
		len(workspace.detail.Issues) != 2 || workspace.detail.Issues[0].Body != "Refined issue tree." {
		t.Fatalf("unexpected refined epic: %#v", workspace.detail)
	}
	// The first round has no prior conversation of its own to resume. Asking to
	// continue here would attach the round to whatever session the sandbox last held.
	if strings.Contains(strings.Join(runtime.argv, " "), "--continue") {
		t.Fatalf("first refiner round continued a session that cannot exist: %#v", runtime.argv)
	}
	metadataFound := false
	for _, mount := range sandboxes.ensured[0].Mounts {
		if mount.GuestLocation != "/work/repos/acme__widgets/.git" {
			continue
		}
		metadataFound = true
		if mount.HostLocation != filepath.Join("/tmp/repositories/epic-1/acme/widgets", ".git") || mount.Writable {
			t.Fatalf("expected the epic Git metadata mounted read-only, got %+v", mount)
		}
	}
	if !metadataFound {
		t.Fatal("expected the epic repository Git metadata to be mounted read-only")
	}

	runtime.output = "Independent review.\nVERDICT: approve"
	reviewerSandbox := testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleIssueReviewer)
	err = useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    reviewerSandbox,
	})
	if err != nil {
		t.Fatal(err)
	}
	// An approved plan stops at Proposed and waits. Ready is what cuts and
	// pushes branches, so the loop hands back rather than committing to it.
	if workspace.detail.State != epic.EpicStateProposed {
		t.Fatalf("expected proposed epic, got %q", workspace.detail.State)
	}
	if strings.Contains(strings.Join(runtime.argv, " "), "--continue") {
		t.Fatalf("reviewer unexpectedly continued a session: %#v", runtime.argv)
	}
	if len(sandboxes.ensured) != 2 || len(registry.savedSandboxes) != 6 {
		t.Fatalf("unexpected sandbox lifecycle: %#v %#v", sandboxes, registry.savedSandboxes)
	}
}

func TestRunEpicAgentUseCase_ShouldReadSetupScriptFromAgentSettings(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	settings := testAgentSettings()
	settings.SetupScript = "agents/scripts/project.sh"
	workspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateConcept),
		agentSettings: settings,
		files: map[string]string{
			"agents/scripts/project.sh": "#!/bin/sh\napt-get install -y sqlite3\n",
		},
	}
	sandboxes := &fakeSandboxManager{}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{},
		sandboxes: sandboxes, runtime: &fakeAgentRuntime{output: "Refiner completed the tree."},
		builder: fakeCommandBuilder{}, creds: &fakeAgentCredentials{},
		repos: &fakeRepositoryWorkspace{}, issueTreeStore: fakeIssueTreeStore{},
		clock: fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.ensured) != 1 ||
		sandboxes.ensured[0].SetupScript != "#!/bin/sh\napt-get install -y sqlite3\n" {
		t.Fatalf("expected the setup script to be read from agent.yaml, got %#v", sandboxes.ensured)
	}
}

func TestRunEpicAgentUseCase_ShouldFailWhenSetupScriptCannotBeRead(t *testing.T) {
	// Arrange: agent.yaml names a script the store does not have.
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	settings := testAgentSettings()
	settings.SetupScript = "agents/scripts/missing.sh"
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateConcept), agentSettings: settings,
	}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{},
		sandboxes: &fakeSandboxManager{}, runtime: &fakeAgentRuntime{output: "Nothing to do."},
		builder: fakeCommandBuilder{}, creds: &fakeAgentCredentials{},
		repos: &fakeRepositoryWorkspace{}, issueTreeStore: emptyIssueTreeStore{},
		clock: fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err == nil {
		t.Fatal("expected the missing setup script to fail the round")
	}
}

func TestRunEpicAgentUseCase_ShouldRecordSandboxBrokenWhenStopFails(t *testing.T) {
	// A failed stop leaves the real sandbox in an unknown state. Recording Stopped
	// anyway would let the next round Start it as if nothing happened; Broken
	// keeps it out of idle reclaim until reconciliation re-inspects it.
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateConcept), agentSettings: testAgentSettings(),
	}
	registry := &fakeAgentRegistry{}
	sandboxes := &fakeSandboxManager{stopErr: fmt.Errorf("docker: stop timed out")}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry, sandboxes: sandboxes,
		runtime: &fakeAgentRuntime{output: "Refiner completed the tree."},
		builder: fakeCommandBuilder{}, creds: &fakeAgentCredentials{},
		repos: &fakeRepositoryWorkspace{}, issueTreeStore: fakeIssueTreeStore{},
		clock: fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err == nil {
		t.Fatal("expected the stop failure to surface")
	}
	if len(registry.savedSandboxes) == 0 {
		t.Fatal("expected the sandbox to have been saved")
	}
	last := registry.savedSandboxes[len(registry.savedSandboxes)-1]
	if last.Status != agent.SandboxStatusBroken {
		t.Fatalf("expected the sandbox recorded broken after a failed stop, got %q", last.Status)
	}
}

func TestRunEpicAgentUseCase_ShouldLoopBackAfterReviewRequestsChanges(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail:        testEpic(epic.EpicStateReview),
		agentSettings: testAgentSettings(),
	}
	registry := &fakeAgentRegistry{}
	useCase := RunEpicAgentUseCase{
		factory:        &fakeFactory{workspace: workspace},
		registry:       registry,
		sandboxes:      &fakeSandboxManager{},
		runtime:        &fakeAgentRuntime{output: "Missing tests.\nVERDICT: request-changes"},
		builder:        fakeCommandBuilder{},
		creds:          &fakeAgentCredentials{},
		repos:          &fakeRepositoryWorkspace{},
		issueTreeStore: fakeIssueTreeStore{},
		clock:          fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleIssueReviewer),
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.State != epic.EpicStateChangesRequested {
		t.Fatalf("expected changes requested, got %q", workspace.detail.State)
	}
	if role, ok := epicRole(workspace.detail.State); !ok || role != agent.AgentRoleRefiner {
		t.Fatalf("changes-requested did not schedule the refiner: %q %t", role, ok)
	}
}

func TestRunEpicAgentUseCase_ShouldFailRunWhenSandboxProvisioningFailsBeforeRunning(t *testing.T) {
	// A sandbox Ensure failure happens while the run is still Queued, before it is ever
	// admitted or started. Regression test for a gap where failRun could not apply
	// AgentRunEventFail from Queued, leaving the run "live" forever and blocking the
	// epic on every future tick.
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateConcept), agentSettings: testAgentSettings(),
	}
	registry := &fakeAgentRegistry{}
	sandboxes := &fakeSandboxManager{ensureErr: fmt.Errorf("docker: image build failed")}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry, sandboxes: sandboxes,
		runtime: &fakeAgentRuntime{}, builder: fakeCommandBuilder{},
		creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
		issueTreeStore: fakeIssueTreeStore{},
		clock:          fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err == nil {
		t.Fatal("expected the provisioning failure to surface")
	}
	// Recorded terminally so it is not left live, but as the host's failure
	// rather than the agent's: the image never built, so the refiner never had a
	// turn to spend.
	if len(registry.runs) != 1 || registry.runs[0].Status != agent.AgentRunStatusHostFailed {
		t.Fatalf("expected the run recorded host-failed, not left live: %#v", registry.runs)
	}
	if registry.runs[0].CountsTowardRoundLimit() {
		t.Fatal("a sandbox that never built must not spend one of the role's attempts")
	}
}

func TestRunEpicAgentUseCase_ShouldRecoverEpicStuckInRefineAfterProvisioningFailure(t *testing.T) {
	// The refiner's Concept -> Refine transition commits before its sandbox/run exist. If
	// provisioning then fails, the epic must still be recognized as refiner work on
	// the next attempt instead of stalling in a state epicRole no longer maps to a role.
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateConcept), agentSettings: testAgentSettings(),
	}
	registry := &fakeAgentRegistry{}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry,
		sandboxes: &fakeSandboxManager{ensureErr: fmt.Errorf("docker: image build failed")},
		runtime:   &fakeAgentRuntime{}, builder: fakeCommandBuilder{},
		creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
		issueTreeStore: fakeIssueTreeStore{},
		clock:          fixedClock{now: now},
	}
	command := RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	}
	if err := useCase.Handle(context.Background(), command); err == nil {
		t.Fatal("expected the first attempt to fail")
	}
	if workspace.detail.State != epic.EpicStateRefine {
		t.Fatalf("expected the epic to have committed to refine, got %q", workspace.detail.State)
	}
	if role, ok := epicRole(workspace.detail.State); !ok || role != agent.AgentRoleRefiner {
		t.Fatalf("epicRole no longer recognizes the stranded epic: %q %t", role, ok)
	}

	// Act: a later tick retries with working infrastructure.
	useCase.sandboxes = &fakeSandboxManager{}
	useCase.runtime = &fakeAgentRuntime{output: "Refiner completed the tree."}
	err := useCase.Handle(context.Background(), command)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.State != epic.EpicStateReview {
		t.Fatalf("expected the epic to recover and reach review, got %q", workspace.detail.State)
	}
}

func TestRunEpicAgentUseCase_ShouldReapOrphanedLiveRunAndStartFreshRound(t *testing.T) {
	// A run left Running by a process that quit or crashed mid-run must not block the
	// epic forever: Handle always resolves the run it creates before returning, so a
	// live run found at entry can only be a leftover from an earlier process.
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateConcept), agentSettings: testAgentSettings(),
	}
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{{
		ID: "run-orphaned", ProjectID: 1, SandboxID: "sandbox-old", Role: agent.AgentRoleRefiner,
		Subject:     agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: workspace.detail.ID},
		Engine:      agent.AgentEngineOpenCode,
		Agent:       "refiner",
		SessionMode: agent.SessionModeContinue,
		Status:      agent.AgentRunStatusRunning,
		Round:       1,
		CreatedAt:   now.Add(-time.Hour),
	}}}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry, sandboxes: &fakeSandboxManager{},
		runtime: &fakeAgentRuntime{output: "Refiner completed the tree."}, builder: fakeCommandBuilder{},
		creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
		issueTreeStore: fakeIssueTreeStore{},
		clock:          fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.runs) != 2 {
		t.Fatalf("expected the orphaned run plus a fresh round, got %#v", registry.runs)
	}
	if registry.runs[0].Status != agent.AgentRunStatusStalled || registry.runs[0].Error == "" {
		t.Fatalf("expected the orphaned run to be stalled with an explanation: %#v", registry.runs[0])
	}
	if registry.runs[1].Round != 2 || registry.runs[1].Status != agent.AgentRunStatusSucceeded {
		t.Fatalf("expected a fresh, succeeding round: %#v", registry.runs[1])
	}
	if workspace.detail.State != epic.EpicStateReview {
		t.Fatalf("expected the epic to advance despite the orphaned run: %#v", workspace.detail)
	}
}

func TestRunEpicAgentUseCase_ShouldFailEpicWhenRoundLimitReached(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	subject := agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: "epic-1"}
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateReview),
		agentSettings: agent.AgentSettings{Roles: map[agent.AgentRole]agent.AgentProfile{
			agent.AgentRoleIssueReviewer: {Agent: "reviewer", Variant: "high", MaxRounds: 2},
		}},
	}
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{
		{ID: "run-1", Round: 1, Status: agent.AgentRunStatusFailed, Subject: subject},
		{ID: "run-2", Round: 2, Status: agent.AgentRunStatusFailed, Subject: subject},
	}}
	sandboxes := &fakeSandboxManager{}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry, sandboxes: sandboxes,
		runtime: &fakeAgentRuntime{}, builder: fakeCommandBuilder{},
		creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
		issueTreeStore: fakeIssueTreeStore{},
		clock:          fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleIssueReviewer),
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.State != epic.EpicStateFailed {
		t.Fatalf(
			"expected the epic to fail once its round limit is exhausted, got %q",
			workspace.detail.State,
		)
	}
	if len(sandboxes.ensured) != 0 {
		t.Fatalf("expected no sandbox once the round limit is reached: %#v",
			sandboxes.ensured)
	}
	if len(registry.runs) != 2 {
		t.Fatalf("expected no new run to be recorded, got %#v", registry.runs)
	}
}

func TestRunEpicAgentUseCase_ShouldSkipRoundWhenHostHasNoCapacity(t *testing.T) {
	// A "no" from the runner queue's admission check is backpressure, not a failure:
	// the epic must stay exactly where it was so a later tick can retry once another
	// Sandbox frees up.
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateConcept), agentSettings: testAgentSettings(),
	}
	registry := &fakeAgentRegistry{}
	full := false
	sandboxes := &fakeSandboxManager{admitted: &full}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry, sandboxes: sandboxes,
		runtime: &fakeAgentRuntime{}, builder: fakeCommandBuilder{},
		creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
		issueTreeStore: fakeIssueTreeStore{},
		clock:          fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.checkedSpecs) != 1 {
		t.Fatalf("expected capacity to be checked once, got %d", len(sandboxes.checkedSpecs))
	}
	if len(sandboxes.ensured) != 0 || len(registry.runs) != 0 {
		t.Fatalf(
			"expected no sandbox or run to be started while over capacity: %#v %#v",
			sandboxes.ensured, registry.runs,
		)
	}
	if workspace.detail.State != epic.EpicStateRefine {
		t.Fatalf(
			"expected the epic to have only committed its refine transition, got %q",
			workspace.detail.State,
		)
	}
	if role, ok := epicRole(workspace.detail.State); !ok || role != agent.AgentRoleRefiner {
		t.Fatalf("expected the epic to remain schedulable for a later tick: %q %t", role, ok)
	}
}

func TestRunEpicAgentUseCase_ShouldSurfaceCapacityCheckFailure(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateConcept), agentSettings: testAgentSettings(),
	}
	sandboxes := &fakeSandboxManager{admitErr: fmt.Errorf("colima unavailable")}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{}, sandboxes: sandboxes,
		runtime: &fakeAgentRuntime{}, builder: fakeCommandBuilder{},
		creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
		issueTreeStore: fakeIssueTreeStore{},
		clock:          fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err == nil {
		t.Fatal("expected the capacity check failure to surface")
	}
}

func TestRunEpicAgentUseCase_ShouldProceedWhenWithinRoundLimit(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateConcept),
		agentSettings: agent.AgentSettings{Roles: map[agent.AgentRole]agent.AgentProfile{
			agent.AgentRoleRefiner: {Agent: "refiner", Variant: "high", MaxRounds: 3},
		}},
	}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{},
		sandboxes: &fakeSandboxManager{},
		runtime:   &fakeAgentRuntime{output: "Refiner completed the tree."},
		builder:   fakeCommandBuilder{},
		creds:     &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
		issueTreeStore: fakeIssueTreeStore{},
		clock:          fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if workspace.detail.State != epic.EpicStateReview {
		t.Fatalf(
			"expected the epic to advance normally within its round limit, got %q",
			workspace.detail.State,
		)
	}
}

func TestRunEpicAgentUseCase_ShouldStopAtProposedWhateverTheVerdict(t *testing.T) {
	// Arrange: Ready is what cuts and pushes branches, and the branch prefix
	// they are named after is answered on the way into it, so the loop hands
	// back at Proposed however the review round ended.
	tests := []struct {
		name    string
		verdict string
		passes  int
	}{
		{name: "approved", verdict: "Looks good.\nVERDICT: approve"},
		{
			name:    "out of drafting passes",
			verdict: "Still not right.\nVERDICT: request-changes",
			passes:  epic.MaxDraftingPasses - 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := testEpic(epic.EpicStateReview)
			detail.DraftingPasses = tt.passes
			workspace := &fakeWorkspace{detail: detail, agentSettings: testAgentSettings()}
			useCase := RunEpicAgentUseCase{
				factory:        &fakeFactory{workspace: workspace},
				registry:       &fakeAgentRegistry{},
				sandboxes:      &fakeSandboxManager{},
				runtime:        &fakeAgentRuntime{output: tt.verdict},
				builder:        fakeCommandBuilder{},
				creds:          &fakeAgentCredentials{},
				repos:          &fakeRepositoryWorkspace{},
				issueTreeStore: fakeIssueTreeStore{},
				clock: fixedClock{
					now: time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC),
				},
			}
			sandbox := testEpicSandboxSpec(t, 1, detail.ID, agent.AgentRoleIssueReviewer)

			// Act
			err := useCase.Handle(context.Background(), RunEpicAgentCommand{
				Project: domain.Project{ID: 1, Name: "one"},
				EpicID:  detail.ID,
				Spec:    sandbox,
			})

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			if workspace.detail.State != epic.EpicStateProposed {
				t.Fatalf("expected the loop to hand back at Proposed, got %q",
					workspace.detail.State)
			}
		})
	}
}

func testEpic(state epic.EpicState) epic.Epic {
	return epic.Epic{
		ID: "epic-1", Title: "Improve workflow", Assignee: "owner", Body: "Initial idea.", State: state,
		Repositories: []string{"acme/widgets"},
		Issues: []epic.Issue{{
			ID: "root-1", Title: "Improve workflow", State: epic.IssueStateOpen,
		}},
	}
}

func testAgentSettings() agent.AgentSettings {
	return agent.AgentSettings{Roles: map[agent.AgentRole]agent.AgentProfile{
		agent.AgentRoleRefiner:       {Agent: "refiner", Variant: "high"},
		agent.AgentRoleIssueReviewer: {Agent: "reviewer", Variant: "high"},
	}}
}

func testEpicSandboxSpec(
	t *testing.T,
	projectID uint,
	epicID string,
	role agent.AgentRole,
) agent_runtime.SandboxSpec {
	t.Helper()
	now := time.Now().UTC()
	return agent_runtime.SandboxSpec{
		Sandbox: agent.Sandbox{
			ID: "sandbox-" + string(role), ProjectID: projectID, Name: "project-" + string(role), Role: role,
			Subject: agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: epicID},
			Status:  agent.SandboxStatusCreating, CreatedAt: now, UpdatedAt: now,
		},
	}
}

func TestRunEpicAgentUseCase_ShouldFailARoundThatOverrunsItsDeadline(t *testing.T) {
	// A round that never returns wedges the worker inside its tick: no further ticks,
	// no reconciliation, and every other epic and issue stuck behind it while the sandbox
	// keeps running. Nobody asked for this stop, so it is a failure rather than a
	// cancellation — and the recorded cause has to name the timeout, because what the
	// runtime reports about being killed names neither it nor its length.
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateConcept), agentSettings: testAgentSettings(),
	}
	registry := &fakeAgentRegistry{}
	sandboxes := &fakeSandboxManager{}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry, sandboxes: sandboxes,
		runtime: &fakeAgentRuntime{run: func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}},
		builder: fakeCommandBuilder{}, creds: &fakeAgentCredentials{},
		repos: &fakeRepositoryWorkspace{}, issueTreeStore: fakeIssueTreeStore{},
		clock: fixedClock{now: now}, roundTimeout: 10 * time.Millisecond,
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err == nil {
		t.Fatal("expected the overrun round to fail")
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("expected the cause to name the timeout, got %v", err)
	}
	if len(registry.runs) != 1 {
		t.Fatalf("expected one run to be recorded, got %#v", registry.runs)
	}
	if status := registry.runs[0].Status; status != agent.AgentRunStatusHostFailed {
		t.Fatalf("expected the run host-failed rather than cancelled, got %q", status)
	}
	// A guard stopping the round is the host acting, not the agent returning a
	// verdict, so it must not spend one of the role.s attempts.
	if registry.runs[0].CountsTowardRoundLimit() {
		t.Fatal("a round the host stopped must not spend one of the role.s attempts")
	}
	// The process is healthy, so the sandbox is shut down properly rather than cut off.
	if len(sandboxes.stopped) != 1 || len(sandboxes.forceStopped) != 0 {
		t.Fatalf("expected a graceful stop, got stopped=%#v forced=%#v",
			sandboxes.stopped, sandboxes.forceStopped)
	}
}

func TestNextRound_ShouldSpendAnAttemptOnlyWhenTheAgentAnswered(t *testing.T) {
	// The shape that killed a real epic: three rounds of genuine work, then the
	// host stopped being able to run anything — a full disk failing every image
	// build. Seven non-events took the role to its limit of ten and the epic was
	// recorded Failed, blaming the refiner for a disk.
	//
	// A round is an attempt at the work, so it is spent when the agent answers —
	// including when the answer is wrong, which is what Failed records. A round
	// the host never ran is not an attempt at anything.
	// Arrange
	runs := []agent.AgentRun{
		{Round: 1, Status: agent.AgentRunStatusSucceeded},
		{Round: 2, Status: agent.AgentRunStatusSucceeded},
		{Round: 3, Status: agent.AgentRunStatusSucceeded},
		// An answer that was wrong: no marked answer, or a plan with no issues.
		{Round: 4, Status: agent.AgentRunStatusFailed},
	}

	// Act
	afterWork := nextRound(runs)

	// Assert
	if afterWork != 5 {
		t.Fatalf("four answered rounds should leave the next at 5, got %d", afterWork)
	}

	// Arrange: the host then fails repeatedly, the way a full disk does.
	for range 20 {
		runs = append(runs, agent.AgentRun{
			Round: afterWork, Status: agent.AgentRunStatusHostFailed,
		})
	}

	// Act
	afterHostFailures := nextRound(runs)

	// Assert
	if afterHostFailures != afterWork {
		t.Fatalf("host failures moved the round from %d to %d; they must not move it at all",
			afterWork, afterHostFailures)
	}
	if afterHostFailures > roundLimit(agent.AgentProfile{}) {
		t.Fatal("host failures alone pushed the role past its limit")
	}
}

func TestRunEpicAgentUseCase_ShouldFailARoundThatProducesNothing(t *testing.T) {
	// The runaway guard is hours long, because a coding round that runs a test
	// suite legitimately takes hours. That makes it the wrong instrument for a
	// round that is not slow but stopped — an agent waiting on an answer nobody
	// will give it writes nothing at all, and holds its sandbox, and with it a share
	// of the host budget that admits the next round, until the guard fires.
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateConcept), agentSettings: testAgentSettings(),
	}
	registry := &fakeAgentRegistry{}
	sandboxes := &fakeSandboxManager{}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry, sandboxes: sandboxes,
		runtime: &fakeAgentRuntime{run: func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}},
		builder: fakeCommandBuilder{}, creds: &fakeAgentCredentials{},
		repos: &fakeRepositoryWorkspace{}, issueTreeStore: fakeIssueTreeStore{},
		clock: fixedClock{now: now},
		// A transcript that never grows is the whole signal. The runaway guard is
		// left far out of reach so a pass here cannot be it firing instead.
		output: stuckRunOutput{}, roundTimeout: time.Minute,
		roundSilence: 20 * time.Millisecond, silenceSample: 5 * time.Millisecond,
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err == nil {
		t.Fatal("expected the silent round to fail")
	}
	// The two guards fail a round for different reasons, and a cause naming the
	// wrong one sends whoever reads it looking at the wrong thing.
	if !strings.Contains(err.Error(), "no output") {
		t.Fatalf("expected the cause to name the stall, got %v", err)
	}
	if strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("expected the stall reported rather than the runaway guard, got %v", err)
	}
	if len(registry.runs) != 1 {
		t.Fatalf("expected one run to be recorded, got %#v", registry.runs)
	}
	if status := registry.runs[0].Status; status != agent.AgentRunStatusHostFailed {
		t.Fatalf("expected the run host-failed rather than cancelled, got %q", status)
	}
	// A guard stopping the round is the host acting, not the agent returning a
	// verdict, so it must not spend one of the role.s attempts.
	if registry.runs[0].CountsTowardRoundLimit() {
		t.Fatal("a round the host stopped must not spend one of the role.s attempts")
	}
	// The process is healthy, so the sandbox is shut down properly rather than cut off.
	if len(sandboxes.stopped) != 1 || len(sandboxes.forceStopped) != 0 {
		t.Fatalf("expected a graceful stop, got stopped=%#v forced=%#v",
			sandboxes.stopped, sandboxes.forceStopped)
	}
}

func TestRunEpicAgentUseCase_ShouldNotStopARoundThatIsStillWriting(t *testing.T) {
	// The guard must measure silence, not elapsed time: a round that keeps
	// writing is working, however long it takes.
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateConcept), agentSettings: testAgentSettings(),
	}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{},
		sandboxes: &fakeSandboxManager{},
		runtime: &fakeAgentRuntime{run: func(_ context.Context, _ string) (string, error) {
			// Long enough to outlast several silence windows had the transcript
			// not been growing underneath it.
			time.Sleep(100 * time.Millisecond)
			return "Refiner completed the tree.", nil
		}},
		builder: fakeCommandBuilder{}, creds: &fakeAgentCredentials{},
		repos: &fakeRepositoryWorkspace{}, issueTreeStore: fakeIssueTreeStore{},
		clock:  fixedClock{now: now},
		output: &growingRunOutput{}, roundTimeout: time.Minute,
		roundSilence: 20 * time.Millisecond, silenceSample: 5 * time.Millisecond,
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err != nil {
		t.Fatalf("expected a round that keeps writing to survive, got %v", err)
	}
}

// stuckRunOutput is a transcript that never grows, which is what a stalled
// round leaves behind.
type stuckRunOutput struct{}

func (stuckRunOutput) Tail(string, int64) ([]string, int64, error) { return nil, 0, nil }
func (stuckRunOutput) Size(string) (int64, error)                  { return 0, nil }
func (stuckRunOutput) Discard(string) error                        { return nil }

// growingRunOutput is a transcript that grows on every sample, which is what a
// working round leaves behind.
type growingRunOutput struct {
	size atomic.Int64
}

func (*growingRunOutput) Tail(string, int64) ([]string, int64, error) { return nil, 0, nil }
func (o *growingRunOutput) Size(string) (int64, error)                { return o.size.Add(1), nil }
func (*growingRunOutput) Discard(string) error                        { return nil }

func TestRunEpicAgentUseCase_ShouldForceStopTheSandboxWhenTheProcessIsShuttingDown(t *testing.T) {
	// A cancelled outer context means the worker is being torn down, which is the one
	// case where waiting for the guest to shut down cleanly is the problem: somebody
	// is sitting in front of a quit that appears to hang. The round's work is already
	// lost and the container is started again from its image, so its power is cut instead.
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	workspace := &fakeWorkspace{
		detail: testEpic(epic.EpicStateConcept), agentSettings: testAgentSettings(),
	}
	sandboxes := &fakeSandboxManager{}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{}, sandboxes: sandboxes,
		runtime: &fakeAgentRuntime{output: "Refiner completed the tree."},
		builder: fakeCommandBuilder{}, creds: &fakeAgentCredentials{},
		repos: &fakeRepositoryWorkspace{}, issueTreeStore: fakeIssueTreeStore{},
		clock: fixedClock{now: now},
	}
	spec := testEpicSandboxSpec(t, 1, workspace.detail.ID, agent.AgentRoleRefiner)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Act
	_ = useCase.Handle(ctx, RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  workspace.detail.ID,
		Spec:    spec,
	})

	// Assert
	if len(sandboxes.forceStopped) != 1 || sandboxes.forceStopped[0] != spec.Sandbox.Name {
		t.Fatalf("expected the sandbox to be force-stopped, got %#v", sandboxes.forceStopped)
	}
	if len(sandboxes.stopped) != 0 {
		t.Fatalf("expected no graceful stop to be attempted, got %#v", sandboxes.stopped)
	}
}
