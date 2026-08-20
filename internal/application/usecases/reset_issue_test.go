package usecases

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

func TestResetIssueUseCase_Handle_ShouldDeleteRuntimeAndReopenIssue(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "epic-1", Title: "Epic", Assignee: "owner", State: epic.EpicStateReady,
		Issues: []epic.Issue{{
			ID: "issue-1", Title: "Issue", Repository: "acme/widgets",
			State: epic.IssueStateCoding, CreatedAt: time.Now().UTC(),
		}},
		PullRequests: []epic.PullRequest{{
			ID: "pr-1", IssueID: "issue-1", Title: "Issue", Status: epic.PullRequestOpen,
			Repository: "acme/widgets", Head: "gm/issue-1", Base: "main",
		}},
	}}
	subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "issue-sandbox", Role: agent.AgentRoleCoding,
		Subject: subject, Status: agent.SandboxStatusStopped,
	}}}
	code := newFakeCodeWorkspace()
	sandboxes := &fakeSandboxManager{}
	output := &fakeRunOutput{}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry, code: code,
		sandboxes: sandboxes, creds: &fakeAgentCredentials{}, output: output,
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(sandboxes.deleted) != 1 || sandboxes.deleted[0] != "issue-sandbox" {
		t.Fatalf("expected the issue sandbox to be deleted, got %#v", sandboxes.deleted)
	}
	if len(code.deleted) != 1 || code.deleted[0] != "gm/issue-1" {
		t.Fatalf("expected the branch to be deleted, got %#v", code.deleted)
	}
	if len(registry.sandboxes) != 0 || len(registry.runs) != 0 {
		t.Fatalf(
			"expected subject runtime records removed, got sandboxes=%#v runs=%#v",
			registry.sandboxes, registry.runs,
		)
	}
	if workspace.detail.PullRequests[0].Status != epic.PullRequestClosed {
		t.Fatalf("expected the old PR closed, got %q", workspace.detail.PullRequests[0].Status)
	}
	if workspace.detail.Issues[0].State != epic.IssueStateOpen {
		t.Fatalf("expected the issue reopened, got %q", workspace.detail.Issues[0].State)
	}
}

func TestResetIssueUseCase_Handle_ShouldRejectAnAlreadyClosedPullRequest(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{PullRequests: []epic.PullRequest{{
		ID: "pr-1", Status: epic.PullRequestClosed,
	}}}}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{},
		code: newFakeCodeWorkspace(), sandboxes: &fakeSandboxManager{},
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected a closed pull request to be rejected")
	}
}

func TestResetIssueUseCase_Handle_ShouldKeepThePullRequestOpenOnCleanupFailure(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "epic-1", State: epic.EpicStateReady,
		Issues: []epic.Issue{{ID: "issue-1", Title: "Issue", State: epic.IssueStateCoding}},
		PullRequests: []epic.PullRequest{{
			ID: "pr-1", IssueID: "issue-1", Title: "Issue", Status: epic.PullRequestOpen,
			Repository: "acme/widgets", Head: "gm/issue-1", Base: "main",
		}},
	}}
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "issue-sandbox", Role: agent.AgentRoleCoding,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
		Status:  agent.SandboxStatusStopped,
	}}}
	sandboxes := &fakeSandboxManager{deleteErr: errors.New("provider unavailable")}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry,
		code: newFakeCodeWorkspace(), sandboxes: sandboxes,
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected the sandbox cleanup failure")
	}
	if workspace.detail.PullRequests[0].Status != epic.PullRequestOpen {
		t.Fatalf("expected the PR to remain open, got %q", workspace.detail.PullRequests[0].Status)
	}
}

func TestResetIssueUseCase_Handle_ShouldRequireIdentifiers(t *testing.T) {
	// Arrange
	useCase := &ResetIssueUseCase{}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{})

	// Assert
	if err == nil {
		t.Fatal("expected missing identifiers to be rejected")
	}
}

func TestResetIssueUseCase_Handle_ShouldRequireTheAgentRuntime(t *testing.T) {
	// Arrange
	useCase := &ResetIssueUseCase{}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected missing runtime dependencies to be rejected")
	}
}

func TestResetIssueUseCase_Handle_ShouldReturnTheRuntimeListFailure(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{PullRequests: []epic.PullRequest{{
		ID: "pr-1", IssueID: "issue-1", Status: epic.PullRequestOpen,
	}}}}
	registry := &fakeAgentRegistry{listRunsErr: errors.New("database unavailable")}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry,
		code: newFakeCodeWorkspace(), sandboxes: &fakeSandboxManager{},
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected the run query failure")
	}
}

func TestResetIssueUseCase_Handle_ShouldCancelQueuedRunsBeforeDeletingRuntime(t *testing.T) {
	// Arrange
	now := time.Now().UTC()
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "epic-1", State: epic.EpicStateReady,
		Issues: []epic.Issue{{ID: "issue-1", Title: "Issue", State: epic.IssueStateCoding}},
		PullRequests: []epic.PullRequest{{
			ID: "pr-1", IssueID: "issue-1", Title: "Issue", Status: epic.PullRequestOpen,
			Repository: "acme/widgets", Head: "gm/issue-1", Base: "main",
		}},
	}}
	subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{{
		ID: "run-1", ProjectID: 1, SandboxID: "sandbox-1", Role: agent.AgentRoleCoding,
		Subject: subject, Engine: agent.AgentEngineOpenCode, Agent: "provider/model",
		SessionMode: agent.SessionModeFresh, Status: agent.AgentRunStatusQueued, Round: 1, CreatedAt: now,
	}}}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry,
		code: newFakeCodeWorkspace(), sandboxes: &fakeSandboxManager{}, output: &fakeRunOutput{},
		cancel: &CancelAgentRunUseCase{
			registry: registry, supervisor: NewRunSupervisor(), clock: fixedClock{now: now},
		},
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.runs) != 0 {
		t.Fatalf("expected cancelled run history deleted, got %#v", registry.runs)
	}
}

func TestResetIssueUseCase_Handle_ShouldKeepPullRequestOpenWhenBranchDeletionFails(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "epic-1", State: epic.EpicStateReady,
		Issues: []epic.Issue{{ID: "issue-1", Title: "Issue", State: epic.IssueStateCoding}},
		PullRequests: []epic.PullRequest{{
			ID: "pr-1", IssueID: "issue-1", Title: "Issue", Status: epic.PullRequestOpen,
			Repository: "acme/widgets", Head: "gm/issue-1", Base: "main",
		}},
	}}
	code := newFakeCodeWorkspace()
	code.deleteErr = errors.New("remote rejected deletion")
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{},
		code: code, sandboxes: &fakeSandboxManager{},
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected branch deletion failure")
	}
	if workspace.detail.PullRequests[0].Status != epic.PullRequestOpen {
		t.Fatalf("expected the PR to remain open, got %q", workspace.detail.PullRequests[0].Status)
	}
}

func TestResetIssueUseCase_Handle_ShouldReturnSubjectRuntimeDeletionFailure(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "epic-1", State: epic.EpicStateReady,
		Issues: []epic.Issue{{ID: "issue-1", Title: "Issue", State: epic.IssueStateCoding}},
		PullRequests: []epic.PullRequest{{
			ID: "pr-1", IssueID: "issue-1", Title: "Issue", Status: epic.PullRequestOpen,
			Repository: "acme/widgets", Head: "gm/issue-1", Base: "main",
		}},
	}}
	registry := &fakeAgentRegistry{deleteSubjectErr: errors.New("database unavailable")}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry,
		code: newFakeCodeWorkspace(), sandboxes: &fakeSandboxManager{},
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected subject runtime deletion failure")
	}
	if workspace.detail.PullRequests[0].Status != epic.PullRequestOpen {
		t.Fatalf("expected the PR to remain open, got %q", workspace.detail.PullRequests[0].Status)
	}
}

func TestResetIssueUseCase_Handle_ShouldReturnTheEpicReadFailure(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{readEpicErr: errors.New("store unavailable")}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{},
		code: newFakeCodeWorkspace(), sandboxes: &fakeSandboxManager{},
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected epic read failure")
	}
}

func TestResetIssueUseCase_Handle_ShouldRejectAnUnknownPullRequest(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{}}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{},
		code: newFakeCodeWorkspace(), sandboxes: &fakeSandboxManager{},
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected unknown pull request rejection")
	}
}

func TestResetIssueUseCase_Handle_ShouldRequireCancellationSupportForALiveRun(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{PullRequests: []epic.PullRequest{{
		ID: "pr-1", IssueID: "issue-1", Status: epic.PullRequestOpen,
	}}}}
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{{
		ID: "run-1", ProjectID: 1,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
		Status:  agent.AgentRunStatusRunning,
	}}}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry,
		code: newFakeCodeWorkspace(), sandboxes: &fakeSandboxManager{},
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected missing cancellation support to be rejected")
	}
}

func TestResetIssueUseCase_Handle_ShouldReturnTheSandboxListFailure(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{PullRequests: []epic.PullRequest{{
		ID: "pr-1", IssueID: "issue-1", Status: epic.PullRequestOpen,
	}}}}
	registry := &fakeAgentRegistry{listSandboxesErr: errors.New("database unavailable")}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry,
		code: newFakeCodeWorkspace(), sandboxes: &fakeSandboxManager{},
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected sandbox query failure")
	}
}

func TestResetIssueUseCase_Handle_ShouldKeepPROpenOnTranscriptCleanupFailure(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "epic-1", State: epic.EpicStateReady,
		Issues: []epic.Issue{{ID: "issue-1", Title: "Issue", State: epic.IssueStateCoding}},
		PullRequests: []epic.PullRequest{{
			ID: "pr-1", IssueID: "issue-1", Title: "Issue", Status: epic.PullRequestOpen,
			Repository: "acme/widgets", Head: "gm/issue-1", Base: "main",
		}},
	}}
	subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{{
		ID: "run-1", ProjectID: 1, Subject: subject,
	}}}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry,
		code: newFakeCodeWorkspace(), sandboxes: &fakeSandboxManager{},
		output: &fakeRunOutput{discardErr: errors.New("disk unavailable")},
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected transcript cleanup failure")
	}
	if workspace.detail.PullRequests[0].Status != epic.PullRequestOpen {
		t.Fatalf("expected the PR to remain open, got %q", workspace.detail.PullRequests[0].Status)
	}
}

func TestResetIssueUseCase_cancelAndDrain_ShouldReturnACancellationLookupFailure(t *testing.T) {
	// Arrange
	registry := &fakeAgentRegistry{}
	useCase := &ResetIssueUseCase{
		registry: registry,
		cancel: &CancelAgentRunUseCase{
			registry: registry, supervisor: NewRunSupervisor(), clock: fixedClock{},
		},
	}
	subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	runs := []agent.AgentRun{{
		ID: "missing", ProjectID: 1, Subject: subject, Status: agent.AgentRunStatusRunning,
	}}

	// Act
	err := useCase.cancelAndDrain(context.Background(), 1, subject, runs)

	// Assert
	if err == nil {
		t.Fatal("expected cancellation lookup failure")
	}
}

func TestResetIssueUseCase_cancelAndDrain_ShouldRespectContextWhileARunStops(t *testing.T) {
	// Arrange
	now := time.Now().UTC()
	subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"}
	run := agent.AgentRun{
		ID: "run-1", ProjectID: 1, SandboxID: "sandbox-1", Role: agent.AgentRoleCoding,
		Subject: subject, Engine: agent.AgentEngineOpenCode, Agent: "provider/model",
		SessionMode: agent.SessionModeFresh, Status: agent.AgentRunStatusRunning,
		Round: 1, CreatedAt: now,
	}
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{run}}
	supervisor := NewRunSupervisor()
	_, release := supervisor.Begin(context.Background(), run.ID)
	defer release()
	useCase := &ResetIssueUseCase{
		registry: registry,
		cancel: &CancelAgentRunUseCase{
			registry: registry, supervisor: supervisor, clock: fixedClock{now: now},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Act
	err := useCase.cancelAndDrain(ctx, 1, subject, []agent.AgentRun{run})

	// Assert
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestResettablePullRequest_ShouldSkipOtherPullRequests(t *testing.T) {
	// Arrange
	current := epic.Epic{PullRequests: []epic.PullRequest{
		{ID: "other", Status: epic.PullRequestClosed},
		{ID: "wanted", Status: epic.PullRequestOpen},
	}}

	// Act
	pullRequest, err := resettablePullRequest(current, "wanted")

	// Assert
	if err != nil || pullRequest.ID != "wanted" {
		t.Fatalf("expected wanted PR, got %#v, %v", pullRequest, err)
	}
}

func TestResetIssueUseCase_Handle_ShouldKeepPROpenOnCredentialCleanupFailure(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{detail: epic.Epic{
		ID: "epic-1", State: epic.EpicStateReady,
		Issues: []epic.Issue{{ID: "issue-1", Title: "Issue", State: epic.IssueStateCoding}},
		PullRequests: []epic.PullRequest{{
			ID: "pr-1", IssueID: "issue-1", Title: "Issue", Status: epic.PullRequestOpen,
			Repository: "acme/widgets", Head: "gm/issue-1", Base: "main",
		}},
	}}
	registry := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "issue-sandbox", Role: agent.AgentRoleCoding,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
		Status:  agent.SandboxStatusStopped,
	}}}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry,
		code: newFakeCodeWorkspace(), sandboxes: &fakeSandboxManager{},
		creds: &fakeAgentCredentials{discardErr: errors.New("credential cleanup failed")},
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected credential cleanup failure")
	}
	if workspace.detail.PullRequests[0].Status != epic.PullRequestOpen {
		t.Fatalf("expected the PR to remain open, got %q", workspace.detail.PullRequests[0].Status)
	}
}

func TestResetIssueUseCase_Handle_ShouldReturnTheTrackerWriteFailureAfterCleanup(t *testing.T) {
	// Arrange
	workspace := &fakeWorkspace{
		detail: epic.Epic{
			ID: "epic-1", State: epic.EpicStateReady,
			Issues: []epic.Issue{{ID: "issue-1", Title: "Issue", State: epic.IssueStateCoding}},
			PullRequests: []epic.PullRequest{{
				ID: "pr-1", IssueID: "issue-1", Title: "Issue", Status: epic.PullRequestOpen,
				Repository: "acme/widgets", Head: "gm/issue-1", Base: "main",
			}},
		},
		updateErr: errors.New("tracker unavailable"),
	}
	useCase := &ResetIssueUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: &fakeAgentRegistry{},
		code: newFakeCodeWorkspace(), sandboxes: &fakeSandboxManager{},
	}

	// Act
	err := useCase.Handle(context.Background(), ResetIssueCommand{
		Project: domain.Project{ID: 1}, EpicID: "epic-1", PullRequestID: "pr-1",
	})

	// Assert
	if err == nil {
		t.Fatal("expected tracker write failure")
	}
}
