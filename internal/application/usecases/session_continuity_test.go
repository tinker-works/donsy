package usecases

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

type refinerRoundHarness struct {
	runtime  *fakeAgentRuntime
	registry *fakeAgentRegistry
	useCase  RunEpicAgentUseCase
	command  RunEpicAgentCommand
}

// resumingRefinerRound is an epic whose last refiner round succeeded. It proves
// the switch prevents continuation even when the prior session still exists.
func resumingRefinerRound(t *testing.T, now time.Time, recreated bool) refinerRoundHarness {
	t.Helper()
	detail := testEpic(epic.EpicStateConcept)
	workspace := &fakeWorkspace{detail: detail, agentSettings: testAgentSettings()}
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{{
		ID: "run-1", ProjectID: 1, SandboxID: "sandbox-refiner", Role: agent.AgentRoleRefiner,
		Subject:     agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: detail.ID},
		Engine:      agent.AgentEngineOpenCode,
		Agent:       "refiner",
		SessionMode: agent.SessionModeFresh,
		Status:      agent.AgentRunStatusSucceeded,
		Round:       1,
		CreatedAt:   now.Add(-time.Hour),
	}}}
	runtime := &fakeAgentRuntime{output: "Refiner completed the tree."}
	return refinerRoundHarness{
		runtime:  runtime,
		registry: registry,
		useCase: RunEpicAgentUseCase{
			factory: &fakeFactory{workspace: workspace}, registry: registry,
			sandboxes: &fakeSandboxManager{recreated: recreated},
			runtime:   runtime, builder: fakeCommandBuilder{},
			creds: &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
			issueTreeStore: fakeIssueTreeStore{},
			clock:          fixedClock{now: now},
		},
		command: RunEpicAgentCommand{
			Project: domain.Project{ID: 1, Name: "one"},
			EpicID:  detail.ID,
			Spec:    testEpicSandboxSpec(t, 1, detail.ID, agent.AgentRoleRefiner),
		},
	}
}

// latestRun is the round Handle just created, which is the last one recorded.
func latestRun(t *testing.T, registry *fakeAgentRegistry) agent.AgentRun {
	t.Helper()
	if len(registry.runs) == 0 {
		t.Fatal("expected a round to have been recorded")
	}
	return registry.runs[len(registry.runs)-1]
}

func TestAgentRound_ShouldStartFreshWhenTheSandboxWasReusedAndContinuationIsDisabled(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	harness := resumingRefinerRound(t, now, false)

	// Act
	if err := harness.useCase.Handle(context.Background(), harness.command); err != nil {
		t.Fatal(err)
	}

	// Assert
	run := latestRun(t, harness.registry)
	if run.SessionMode != agent.SessionModeFresh {
		t.Fatalf("expected the round to start fresh, got %q", run.SessionMode)
	}
	if strings.Contains(strings.Join(harness.runtime.argv, " "), "--continue") {
		t.Fatalf("expected no --continue on the command, got %#v", harness.runtime.argv)
	}
}

// The run history says there is a conversation to resume; the sandbox manager says it
// had to build the instance that held it. Without this the round would pass
// --continue to an engine with nothing to continue and render the prompt that
// only states what is new, leaving the agent a task without its context.
func TestAgentRound_ShouldStartFreshWhenTheSandboxHadToBeRecreated(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	harness := resumingRefinerRound(t, now, true)

	// Act
	if err := harness.useCase.Handle(context.Background(), harness.command); err != nil {
		t.Fatal(err)
	}

	// Assert
	run := latestRun(t, harness.registry)
	if run.SessionMode != agent.SessionModeFresh {
		t.Fatalf("expected a rebuilt sandbox to force a fresh round, got %q", run.SessionMode)
	}
	if slices.Contains(harness.runtime.argv, "--continue") {
		t.Fatalf("expected no --continue on the command, got %#v", harness.runtime.argv)
	}
	// The prompt has to agree with the command: the full template, not the one
	// that assumes a session.
	prompt := strings.Join(harness.runtime.argv, " ")
	if !strings.Contains(prompt, "## Where you are running") {
		t.Fatalf("expected the fresh prompt with the full environment, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "Continuing: refiner") {
		t.Fatalf("expected the fresh prompt rather than the continue one, got:\n%s", prompt)
	}
}

// The guard is one-directional: a round that was never going to resume anything
// must not be rewritten by it, and it must not cost an extra registry write.
func TestAgentRound_ShouldLeaveAFreshRoundAloneWhenTheSandboxWasRecreated(t *testing.T) {
	// Arrange: no prior run, so SessionModeAfter already says fresh.
	now := time.Date(2026, time.August, 12, 14, 0, 0, 0, time.UTC)
	detail := testEpic(epic.EpicStateConcept)
	workspace := &fakeWorkspace{detail: detail, agentSettings: testAgentSettings()}
	registry := &fakeAgentRegistry{}
	useCase := RunEpicAgentUseCase{
		factory: &fakeFactory{workspace: workspace}, registry: registry,
		sandboxes: &fakeSandboxManager{recreated: true},
		runtime:   &fakeAgentRuntime{output: "Refiner completed the tree."},
		builder:   fakeCommandBuilder{},
		creds:     &fakeAgentCredentials{}, repos: &fakeRepositoryWorkspace{},
		issueTreeStore: fakeIssueTreeStore{},
		clock:          fixedClock{now: now},
	}

	// Act
	err := useCase.Handle(context.Background(), RunEpicAgentCommand{
		Project: domain.Project{ID: 1, Name: "one"},
		EpicID:  detail.ID,
		Spec:    testEpicSandboxSpec(t, 1, detail.ID, agent.AgentRoleRefiner),
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if run := latestRun(t, registry); run.SessionMode != agent.SessionModeFresh {
		t.Fatalf("expected the round to stay fresh, got %q", run.SessionMode)
	}
}

func TestRunIssueAgentUseCase_ShouldSendAFreshPromptToASecondReviewRoundWhenDisabled(t *testing.T) {
	// Arrange: one review already succeeded on this issue, and the coding round
	// after it left a report the second review has not seen.
	now := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	record := openRecord()
	record.Rounds = 2
	record.Reviews = 1
	record.Comments = []epic.Comment{
		{ID: "c1", Author: "pr-reviewer", CreatedAt: now, Body: "The empty case is unhandled."},
		{ID: "c2", Author: "coding", CreatedAt: now, Body: "Handled the empty case."},
	}
	harness := newIssueLoopHarness(t, record, "Better.\n\nVERDICT: approve")
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{{
		ID: "review-1", ProjectID: 1, SandboxID: "sandbox-reviewer", Role: agent.AgentRolePRReviewer,
		Subject:     agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "child-1"},
		Engine:      agent.AgentEngineOpenCode,
		Agent:       "reviewer",
		SessionMode: agent.SessionModeFresh,
		Status:      agent.AgentRunStatusSucceeded,
		Round:       1,
		CreatedAt:   now.Add(-time.Hour),
	}}}
	harness.useCase.registry = registry

	// Act
	if err := runIssueRound(t, harness, agent.AgentRolePRReviewer); err != nil {
		t.Fatal(err)
	}

	// Assert
	if run := latestRun(t, registry); run.SessionMode != agent.SessionModeFresh {
		t.Fatalf("expected the second review to start fresh, got %q", run.SessionMode)
	}
	prompt := strings.Join(harness.runtime.argv, " ")
	if strings.Contains(prompt, "Continuing: pull request reviewer") {
		t.Fatalf("expected the fresh prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "## Where you are running") {
		t.Fatalf("expected the full environment section, got:\n%s", prompt)
	}
	// The session cannot know these, so the prompt still has to carry them.
	if !strings.Contains(prompt, "VERDICT: approve") {
		t.Fatalf("expected the verdict contract restated, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Handled the empty case.") {
		t.Fatalf("expected the coding round's report, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "The empty case is unhandled.") {
		t.Fatalf("expected the reviewer's prior finding in a fresh prompt, got:\n%s", prompt)
	}
}
