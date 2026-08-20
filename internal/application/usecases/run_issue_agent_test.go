package usecases

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

func issueAgentSettings() agent.AgentSettings {
	return agent.AgentSettings{Roles: map[agent.AgentRole]agent.AgentProfile{
		agent.AgentRoleCoding:     {Agent: "coder", Variant: "high"},
		agent.AgentRolePRReviewer: {Agent: "reviewer", Variant: "high"},
		agent.AgentRoleMerge:      {Agent: "merger", Variant: "high"},
	}}
}

func epicInFlight(record epicpkg.PullRequest) epicpkg.Epic {
	// The issue's phase has to match the counters on its record, the way the
	// loop leaves it: code nobody has judged is in Review, anything else is
	// waiting on a coding round.
	phase := epicpkg.IssueStateCoding
	switch {
	case record.HasFlag(epicpkg.FlagStale):
		phase = epicpkg.IssueStateStale
	case record.Reviews < record.Rounds:
		phase = epicpkg.IssueStateReview
	}
	current := epicForRole(epicpkg.EpicStateReady, phase)
	current.Repositories = []string{"acme/widgets", "acme/gadgets"}
	current.PullRequests = []epicpkg.PullRequest{record}
	return current
}

type issueLoopHarness struct {
	workspace   *fakeWorkspace
	code        *fakeCodeWorkspace
	repos       *fakeRepositoryWorkspace
	runtime     *fakeAgentRuntime
	sandboxes   *fakeSandboxManager
	useCase     *RunIssueAgentUseCase
	invocations *[]application.AgentInvocation
}

func newIssueLoopHarness(t *testing.T, record epicpkg.PullRequest, output string) issueLoopHarness {
	t.Helper()
	workspace := &fakeWorkspace{
		detail:        epicInFlight(record),
		agentSettings: issueAgentSettings(),
		repositories:  []string{"acme/widgets", "acme/gadgets"},
	}
	code := newFakeCodeWorkspace()
	repos := &fakeRepositoryWorkspace{}
	runtime := &fakeAgentRuntime{output: output}
	sandboxes := &fakeSandboxManager{}
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	invocations := []application.AgentInvocation{}
	harness := issueLoopHarness{
		workspace:   workspace,
		code:        code,
		repos:       repos,
		runtime:     runtime,
		sandboxes:   sandboxes,
		invocations: &invocations,
	}
	harness.useCase = &RunIssueAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{},
		sandboxes: sandboxes, runtime: runtime,
		builder: fakeCommandBuilder{invocations: harness.invocations},
		creds:   &fakeAgentCredentials{}, code: code, repos: repos, issueTreeStore: fakeIssueTreeStore{},
		clock: fixedClock{now: now},
	}
	return harness
}

func runIssueRound(t *testing.T, harness issueLoopHarness, role agent.AgentRole) error {
	t.Helper()
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	return harness.useCase.Handle(context.Background(), RunIssueAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  "epic-1",
		IssueID: "child-1",
		Spec:    IssueSandboxSpec(1, "child-1", role, now),
	})
}

func TestRunIssueAgentUseCase_ShouldOverrideRolesFromRepositorySettings(t *testing.T) {
	// Arrange: the repository pins coding to a different agent than agent.yaml.
	harness := newIssueLoopHarness(t, openRecord(), "Implemented the endpoint.")
	harness.workspace.repositorySettings = map[string]agent.RepositorySettings{
		"acme/widgets": {Roles: map[agent.AgentRole]agent.AgentProfile{
			agent.AgentRoleCoding: {Agent: "repo-coder", Variant: "high"},
		}},
	}

	// Act
	if err := runIssueRound(t, harness, agent.AgentRoleCoding); err != nil {
		t.Fatal(err)
	}

	// Assert: begin() records profile.Agent on the run itself.
	registry := harness.useCase.registry.(*fakeAgentRegistry)
	if len(registry.runs) == 0 || registry.runs[len(registry.runs)-1].Agent != "repo-coder" {
		t.Fatalf("expected the repository's agent override to be used, got %#v", registry.runs)
	}
}

func TestRunIssueAgentUseCase_ShouldReadSetupScriptFromRepositoryOverride(t *testing.T) {
	// Arrange: agent.yaml has no script; the repository names one.
	harness := newIssueLoopHarness(t, openRecord(), "Implemented the endpoint.")
	harness.workspace.repositorySettings = map[string]agent.RepositorySettings{
		"acme/widgets": {SetupScript: "agents/scripts/widgets.sh"},
	}
	harness.workspace.files = map[string]string{
		"agents/scripts/widgets.sh": "#!/bin/sh\napt-get install -y sqlite3\n",
	}

	// Act
	if err := runIssueRound(t, harness, agent.AgentRoleCoding); err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(harness.sandboxes.ensured) != 1 ||
		harness.sandboxes.ensured[0].SetupScript != "#!/bin/sh\napt-get install -y sqlite3\n" {
		t.Fatalf("expected the repository's setup script to be used, got %#v", harness.sandboxes.ensured)
	}
}

func TestRunIssueAgentUseCase_ShouldFailWhenSetupScriptCannotBeRead(t *testing.T) {
	// Arrange: the repository names a script the store does not have.
	harness := newIssueLoopHarness(t, openRecord(), "Implemented the endpoint.")
	harness.workspace.repositorySettings = map[string]agent.RepositorySettings{
		"acme/widgets": {SetupScript: "agents/scripts/missing.sh"},
	}

	// Act
	err := runIssueRound(t, harness, agent.AgentRoleCoding)

	// Assert
	if err == nil {
		t.Fatal("expected the missing setup script to fail the round")
	}
}

func TestRunIssueAgentUseCase_CodingRound_ShouldCommitGateAndPublish(t *testing.T) {
	// Arrange
	harness := newIssueLoopHarness(t, openRecord(), "Implemented the endpoint.")

	// Act
	if err := runIssueRound(t, harness, agent.AgentRoleCoding); err != nil {
		t.Fatal(err)
	}

	// Assert: the host committed, then published.
	if len(harness.code.committed) != 1 ||
		!strings.HasPrefix(harness.code.committed[0], "ai: round 1 on issue child-1") {
		t.Fatalf("unexpected commits: %v", harness.code.committed)
	}
	if len(harness.code.pushed) != 1 || harness.code.pushed[0] != "go-merge/child-1" {
		t.Fatalf("unexpected pushes: %v", harness.code.pushed)
	}
	record := harness.workspace.detail.PullRequests[0]
	if record.Rounds != 1 || record.Reviews != 0 || record.Approved {
		t.Fatalf("unexpected counters: %+v", record)
	}
	if len(record.Comments) != 1 || record.Comments[0].Author != "coding" {
		t.Fatalf("expected the agent's report to be recorded, got %+v", record.Comments)
	}
}

func TestRunIssueAgentUseCase_CodingRound_ShouldNotPublishWhenTheGateRefuses(t *testing.T) {
	// Arrange: the agent edited a workflow, which the gate rejects branch-wide.
	harness := newIssueLoopHarness(t, openRecord(), "Edited CI.")
	harness.code.commits = []agent_runtime.CommitInfo{
		{Hash: "aaa", Paths: []string{".github/workflows/ci.yml"}},
	}

	// Act
	err := runIssueRound(t, harness, agent.AgentRoleCoding)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "protected path") {
		t.Fatalf("expected the gate to refuse, got %v", err)
	}
	if len(harness.code.pushed) != 0 {
		t.Fatalf("expected nothing published, got %v", harness.code.pushed)
	}
	if harness.workspace.detail.PullRequests[0].Rounds != 0 {
		t.Fatal("expected a refused round not to count")
	}
}

func TestRunIssueAgentUseCase_CodingRound_ShouldNotPublishRewrittenHistory(t *testing.T) {
	// Arrange
	harness := newIssueLoopHarness(t, openRecord(), "Rebased everything.")
	harness.code.descends = false
	harness.code.commits = []agent_runtime.CommitInfo{{Hash: "aaa", Paths: []string{"main.go"}}}

	// Act
	err := runIssueRound(t, harness, agent.AgentRoleCoding)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "history was rewritten") {
		t.Fatalf("expected the gate to refuse, got %v", err)
	}
	if len(harness.code.pushed) != 0 {
		t.Fatalf("expected nothing published, got %v", harness.code.pushed)
	}
}

func TestRunIssueAgentUseCase_ReviewRound_ShouldRecordAnApproval(t *testing.T) {
	// Arrange
	record := openRecord()
	record.Rounds = 1
	harness := newIssueLoopHarness(t, record, "Looks right.\n\nVERDICT: approve")
	harness.code.resolved = map[string]string{
		"go-merge/child-1": "head1234", "main": "base1234",
	}

	// Act
	if err := runIssueRound(t, harness, agent.AgentRolePRReviewer); err != nil {
		t.Fatal(err)
	}

	// Assert: the verdict is pinned to the commits it was about, and nothing
	// merged — that is a human's decision.
	updated := harness.workspace.detail.PullRequests[0]
	if !updated.Approved || updated.Reviews != 1 {
		t.Fatalf("unexpected counters: %+v", updated)
	}
	if updated.ReviewedHead != "head1234" || updated.ReviewedBase != "base1234" {
		t.Fatalf("unexpected reviewed commits: %+v", updated)
	}
	if updated.Status != epicpkg.PullRequestOpen {
		t.Fatalf("expected the pull request to stay open, got %q", updated.Status)
	}
}

func TestRunIssueAgentUseCase_ReviewRound_ShouldSendChangesBackToCoding(t *testing.T) {
	// Arrange
	record := openRecord()
	record.Rounds = 1
	findings := "The empty case is unhandled.\n\nVERDICT: request-changes"
	harness := newIssueLoopHarness(t, record, findings)

	// Act
	if err := runIssueRound(t, harness, agent.AgentRolePRReviewer); err != nil {
		t.Fatal(err)
	}

	// Assert
	updated := harness.workspace.detail.PullRequests[0]
	if updated.Approved || updated.Reviews != 1 {
		t.Fatalf("unexpected counters: %+v", updated)
	}
	role, ok := IssueRole(harness.workspace.detail, updated)
	if !ok || role != agent.AgentRoleCoding {
		t.Fatalf("expected the next round to be coding, got (%q, %t)", role, ok)
	}
}

func TestRunIssueAgentUseCase_ReviewRound_ShouldFailWithoutAMarkedAnswer(t *testing.T) {
	// Arrange: an unreadable review must not silently pass as an approval.
	record := openRecord()
	record.Rounds = 1
	harness := newIssueLoopHarness(t, record, "")

	// Act
	err := runIssueRound(t, harness, agent.AgentRolePRReviewer)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "marked answer") {
		t.Fatalf("expected an unmarked answer to fail the round, got %v", err)
	}
	if harness.workspace.detail.PullRequests[0].Approved {
		t.Fatal("expected no approval from an unreadable review")
	}
	// The failure must land on the thread: a round that dies only in the run
	// registry is invisible on the board.
	comments := harness.workspace.detail.PullRequests[0].Comments
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "failed") {
		t.Fatalf("expected the failure on the pull request thread, got %+v", comments)
	}
}

func TestRunIssueAgentUseCase_FailedRun_ShouldKeepTheAgentsOutputInTheRecord(t *testing.T) {
	// Arrange: "exit status 1" on its own diagnoses nothing; the output the
	// agent died with is the only evidence and must survive the failure.
	harness := newIssueLoopHarness(t, openRecord(), "")
	harness.runtime.run = func(context.Context, string) (string, error) {
		return "panic: config missing", errors.New("exit status 1")
	}
	registry := &fakeAgentRegistry{}
	harness.useCase.registry = registry

	// Act
	err := runIssueRound(t, harness, agent.AgentRoleCoding)

	// Assert
	if err == nil {
		t.Fatal("expected the round to fail")
	}
	if len(registry.runs) != 1 {
		t.Fatalf("expected one recorded run, got %#v", registry.runs)
	}
	saved := registry.runs[0]
	if !strings.Contains(saved.Error, "exit status 1") ||
		!strings.Contains(saved.Error, "panic: config missing") {
		t.Fatalf("expected both the cause and the raw output in the record, got %q", saved.Error)
	}
}

func TestRunIssueAgentUseCase_ShouldMountTheRepositoryWritableAndReferencesReadOnly(t *testing.T) {
	// Arrange: SandboxSpec.Validate permits one writable mount, and it has to be
	// the checkout — that, not the issue tree, is the deliverable.
	harness := newIssueLoopHarness(t, openRecord(), "Done.")

	// Act
	if err := runIssueRound(t, harness, agent.AgentRoleCoding); err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(harness.sandboxes.ensured) != 1 {
		t.Fatalf("expected one sandbox, got %d", len(harness.sandboxes.ensured))
	}
	spec := harness.sandboxes.ensured[0]
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	var repo, tree, reference agent_runtime.SandboxMount
	var dockerSource agent_runtime.SandboxMount
	for _, mount := range spec.Mounts {
		switch mount.GuestLocation {
		case "/work/repo":
			repo = mount
		case "/work/issues":
			tree = mount
		case "/work/repos/acme__gadgets":
			reference = mount
		case "/checkouts/repo":
			dockerSource = mount
		}
	}
	if !repo.Writable {
		t.Fatal("expected the repository checkout to be writable")
	}
	if tree.Writable {
		t.Fatal("expected the issue tree to be read-only")
	}
	if reference.HostLocation != "/tmp/repositories/epic-1/acme/gadgets" || reference.Writable {
		t.Fatalf("expected the sibling repository mounted read-only, got %+v", reference)
	}
	if len(harness.repos.ensured) != 1 || harness.repos.ensured[0] != "acme/gadgets" {
		t.Fatalf("expected the sibling repository prepared once, got %v", harness.repos.ensured)
	}
	if dockerSource.HostLocation != "/checkouts/repo" || dockerSource.Writable {
		t.Fatalf("expected the host-identity checkout mount to be read-only, got %+v", dockerSource)
	}
	if len(*harness.invocations) != 1 ||
		(*harness.invocations)[0].Environment["GO_MERGE_DOCKER_BIND_SOURCE"] != "/checkouts/repo" {
		t.Fatalf("expected the host checkout path in the invocation, got %+v", *harness.invocations)
	}
}

func TestRunIssueAgentUseCase_ShouldReapOrphanedLiveRunAndStartFreshRound(t *testing.T) {
	// A run left Running by a process that quit or crashed mid-run must not block
	// the issue forever: Handle always resolves the run it creates before
	// returning, so a live run found at entry can only be such a leftover.
	// Arrange
	harness := newIssueLoopHarness(t, openRecord(), "Done.")
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{{
		ID: "run-orphaned", ProjectID: 1, SandboxID: "sandbox-old", Role: agent.AgentRoleCoding,
		Subject:     agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "child-1"},
		Engine:      agent.AgentEngineOpenCode,
		Agent:       "coder",
		SessionMode: agent.SessionModeContinue,
		Status:      agent.AgentRunStatusRunning,
		Round:       1,
		CreatedAt:   time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC),
	}}}
	harness.useCase.registry = registry

	// Act
	if err := runIssueRound(t, harness, agent.AgentRoleCoding); err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(registry.runs) != 2 {
		t.Fatalf("expected the orphaned run plus a fresh round, got %#v", registry.runs)
	}
	if registry.runs[0].Status != agent.AgentRunStatusStalled || registry.runs[0].Error == "" {
		t.Fatalf("expected the orphaned run to be stalled with an explanation: %#v", registry.runs[0])
	}
	if registry.runs[1].Round != 2 || registry.runs[1].Status != agent.AgentRunStatusSucceeded {
		t.Fatalf("expected a fresh, succeeding round: %#v", registry.runs[1])
	}
}

func TestRunIssueAgentUseCase_ShouldWaitWhenHostLacksCapacity(t *testing.T) {
	// Arrange: a full host is backpressure, not failure — the round waits for a
	// later tick without touching git or minting a run.
	harness := newIssueLoopHarness(t, openRecord(), "Done.")
	no := false
	harness.sandboxes.admitted = &no
	registry := &fakeAgentRegistry{}
	harness.useCase.registry = registry

	// Act
	if err := runIssueRound(t, harness, agent.AgentRoleCoding); err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(registry.runs) != 0 || len(harness.sandboxes.ensured) != 0 {
		t.Fatalf("expected no round to start, got runs %#v ensured %#v",
			registry.runs, harness.sandboxes.ensured)
	}
	if len(harness.code.committed) != 0 {
		t.Fatalf("expected no git work on a full host, got %v", harness.code.committed)
	}
}

func TestRunIssueAgentUseCase_ShouldRejectAMismatchedSandbox(t *testing.T) {
	// Arrange: an epic-scoped sandbox would claim the wrong subject in the run
	// registry, defeating the liveness guard above.
	harness := newIssueLoopHarness(t, openRecord(), "Done.")
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)

	// Act
	err := harness.useCase.Handle(context.Background(), RunIssueAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  "epic-1",
		IssueID: "child-1",
		Spec:    EpicSandboxSpec(1, "epic-1", agent.AgentRoleCoding, now),
	})

	// Assert
	if err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("expected a mismatched sandbox to be rejected, got %v", err)
	}
}

// A merge round republishes the branch and hands it to a reviewer. It must not
// spend a coding round: falling behind base is not a failed attempt at the
// issue, and charging for it would ration the wrong thing.
func TestRunIssueAgentUseCase_MergeRound_ShouldRepublishAndReturnToReview(t *testing.T) {
	// Arrange
	record := openRecord()
	record.Rounds, record.Reviews, record.CodingRounds = 1, 1, 1
	record.Approved = true
	if err := record.SetFlag(epicpkg.FlagStale, true); err != nil {
		t.Fatal(err)
	}
	harness := newIssueLoopHarness(t, record, "Resolved the conflict in cart.go.")

	// Act
	if err := runIssueRound(t, harness, agent.AgentRoleMerge); err != nil {
		t.Fatal(err)
	}

	// Assert
	if len(harness.code.pushed) != 1 || harness.code.pushed[0] != "go-merge/child-1" {
		t.Fatalf("unexpected pushes: %v", harness.code.pushed)
	}
	updated := harness.workspace.detail.PullRequests[0]
	if updated.Rounds != 2 || updated.CodingRounds != 1 {
		t.Fatalf("expected an uncharged round: rounds=%d coding=%d",
			updated.Rounds, updated.CodingRounds)
	}
	if updated.HasFlag(epicpkg.FlagStale) || updated.Approved {
		t.Fatalf("expected the record to be cleared for review: %#v", updated)
	}
	if harness.workspace.detail.Issues[1].State != epicpkg.IssueStateReview {
		t.Fatalf("expected the issue back in review, got %q",
			harness.workspace.detail.Issues[1].State)
	}
	role, ok := IssueRole(harness.workspace.detail, updated)
	if !ok || role != agent.AgentRolePRReviewer {
		t.Fatalf("expected a review round next, got (%q, %t)", role, ok)
	}
}
