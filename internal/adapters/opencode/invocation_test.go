package opencode

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"slices"
	"strings"
	"testing"
)

func TestCommand_ShouldContinueEveryRoleThatKeepsItsSession(t *testing.T) {
	// Arrange
	tests := []struct {
		role        agent.AgentRole
		hasContinue bool
	}{
		{role: agent.AgentRoleRefiner, hasContinue: true},
		{role: agent.AgentRoleCoding, hasContinue: true},
		{role: agent.AgentRoleIssueReviewer, hasContinue: true},
		{role: agent.AgentRolePRReviewer, hasContinue: true},
		{role: agent.AgentRoleMerge, hasContinue: false},
	}

	for _, test := range tests {
		t.Run(string(test.role), func(t *testing.T) {
			run := agent.AgentRun{
				ID: "run-1", SandboxID: "sandbox-1", Role: test.role,
				Subject:     agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
				Engine:      agent.AgentEngineOpenCode,
				Agent:       "agent-" + string(test.role),
				SessionMode: agent.SessionModeFor(agent.AgentEngineOpenCode, test.role),
				Status:      agent.AgentRunStatusQueued, Round: 1,
			}

			// Act
			argv, err := Builder{}.Command(application.AgentInvocation{
				Run: run, Prompt: "implement the issue",
			})

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(argv, "--continue") != test.hasContinue {
				t.Fatalf("unexpected command: %#v", argv)
			}
			// The configured value is a "provider/model" identifier, so it has to
			// reach --model. On --agent, OpenCode only warns and falls back to
			// its default agent, which is why this is asserted rather than left
			// to a failing run to reveal.
			if !slices.Contains(argv, "--model") || !slices.Contains(argv, run.Agent) {
				t.Fatalf("command did not select configured model: %#v", argv)
			}
			if slices.Contains(argv, "--agent") {
				t.Fatalf("model must not be passed as an agent: %#v", argv)
			}
			// The image pins OpenCode's version; the run must not undo that.
			if argv[0] != "env" || !slices.Contains(argv, "OPENCODE_DISABLE_AUTOUPDATE=1") {
				t.Fatalf("command does not disable self-update: %#v", argv)
			}
		})
	}
}

func TestCommand_ShouldRunFromTheGuestMountRoot(t *testing.T) {
	// Anything outside the agent's working directory needs per-call approval that
	// a non-interactive run cannot give, so the working directory has to be the
	// root everything is mounted below rather than the guest home.
	// Arrange
	run := agent.AgentRun{
		ID: "run-1", SandboxID: "sandbox-1", Role: agent.AgentRoleIssueReviewer,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: "epic-1"},
		Engine:  agent.AgentEngineOpenCode, Agent: "reviewer",
		SessionMode: agent.SessionModeFresh, Status: agent.AgentRunStatusQueued, Round: 1,
	}

	// Act
	argv, err := Builder{}.Command(application.AgentInvocation{
		Run: run, Prompt: "review the epic",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	index := slices.Index(argv, "--dir")
	if index == -1 || index+1 >= len(argv) {
		t.Fatalf("command does not set a working directory: %#v", argv)
	}
	if argv[index+1] != agent_runtime.GuestMountRoot {
		t.Fatalf("expected %q as the working directory: %#v", agent_runtime.GuestMountRoot, argv)
	}
}

func TestCommand_ShouldExportTheDockerBindSourceWithoutChangingTheWorkingDirectory(t *testing.T) {
	// Arrange
	run := agent.AgentRun{
		ID: "run-1", SandboxID: "sandbox-1", Role: agent.AgentRoleCoding,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
		Engine:  agent.AgentEngineOpenCode, Agent: "coder",
		SessionMode: agent.SessionModeFresh, Status: agent.AgentRunStatusQueued, Round: 1,
	}
	const source = "/host/workdir/code/epic-1/issues/issue-1/acme__widgets"

	// Act
	argv, err := Builder{}.Command(application.AgentInvocation{
		Run: run, Prompt: "implement the issue",
		Environment: map[string]string{
			"GO_MERGE_DOCKER_BIND_SOURCE": source,
		},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(argv, "GO_MERGE_DOCKER_BIND_SOURCE="+source) {
		t.Fatalf("command did not export the checkout path: %#v", argv)
	}
	if !slices.Contains(argv, "OPENCODE_DISABLE_AUTOUPDATE=1") {
		t.Fatalf("command stopped disabling self-update: %#v", argv)
	}
	index := slices.Index(argv, "--dir")
	if index == -1 || index+1 >= len(argv) || argv[index+1] != agent_runtime.GuestMountRoot {
		t.Fatalf("command changed its working directory: %#v", argv)
	}
}

func TestExtractAnswer_ShouldReadTextEventsOnly(t *testing.T) {
	// Arrange
	output := strings.Join([]string{
		`{"type":"tool_use","part":{"text":"VERDICT: approve"}}`,
		`{"type":"text","part":{"text":"===GO-MERGE-BEGIN===\nAnswer\n===GO-MERGE-END==="}}`,
	}, "\n")

	// Act
	answer := Builder{}.ExtractAnswer(output)

	// Assert
	if answer != "Answer" {
		t.Fatalf("expected text answer, got %q", answer)
	}
}

func TestCommand_ShouldRejectInvalidRun(t *testing.T) {
	// Arrange
	run := agent.AgentRun{} // missing ID, SandboxID, agent, round, role, engine

	// Act
	_, err := Builder{}.Command(application.AgentInvocation{Run: run, Prompt: "do the thing"})

	// Assert
	if err == nil {
		t.Fatal("expected an invalid run to be rejected before building a command")
	}
}

func TestCommand_ShouldRejectEmptyPrompt(t *testing.T) {
	// Arrange
	run := agent.AgentRun{
		ID: "run-1", SandboxID: "sandbox-1", Role: agent.AgentRoleIssueReviewer,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: "epic-1"},
		Engine:  agent.AgentEngineOpenCode, Agent: "reviewer",
		SessionMode: agent.SessionModeFresh, Status: agent.AgentRunStatusQueued, Round: 1,
	}

	// Act
	_, err := Builder{}.Command(application.AgentInvocation{Run: run})

	// Assert
	if err == nil {
		t.Fatal("expected an empty prompt to be rejected")
	}
}

func TestCommand_ShouldOmitVariantWhenUnset(t *testing.T) {
	// Arrange
	run := agent.AgentRun{
		ID: "run-1", SandboxID: "sandbox-1", Role: agent.AgentRoleIssueReviewer,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: "epic-1"},
		Engine:  agent.AgentEngineOpenCode, Agent: "reviewer",
		SessionMode: agent.SessionModeFresh, Status: agent.AgentRunStatusQueued, Round: 1,
	}

	// Act
	argv, err := Builder{}.Command(application.AgentInvocation{Run: run, Prompt: "review the epic"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(argv, "--variant") {
		t.Fatalf("expected no --variant flag without one configured: %#v", argv)
	}
}

func TestExtractAnswer_ShouldReturnEmptyWithoutMarkers(t *testing.T) {
	// Arrange
	tests := []struct {
		name   string
		output string
	}{
		{name: "no markers", output: `{"type":"text","part":{"text":"just some text"}}`},
		{
			name:   "missing end marker",
			output: `{"type":"text","part":{"text":"===GO-MERGE-BEGIN===\nunterminated"}}`,
		},
		{name: "empty output", output: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Act
			answer := Builder{}.ExtractAnswer(test.output)

			// Assert
			if answer != "" {
				t.Fatalf("expected no answer, got %q", answer)
			}
		})
	}
}

func TestExtractAnswer_ShouldFallBackToRawOutputWithoutTextEvents(t *testing.T) {
	// Non-JSON or non-text-event output (e.g. a crash before any structured output)
	// still has to be scanned for the answer markers, not silently dropped.
	// Arrange
	output := "not json at all\n===GO-MERGE-BEGIN===\nraw answer\n===GO-MERGE-END==="

	// Act
	answer := Builder{}.ExtractAnswer(output)

	// Assert
	if answer != "raw answer" {
		t.Fatalf("expected the raw answer, got %q", answer)
	}
}

func TestExtractAnswer_ShouldUseTheLastMarkedAnswer(t *testing.T) {
	// If the agent's output contains multiple marked blocks (e.g. an example inside
	// its reasoning followed by its real answer), the final one is authoritative.
	// Arrange
	output := strings.Join([]string{
		`{"type":"text","part":{"text":"===GO-MERGE-BEGIN===\nfirst\n===GO-MERGE-END==="}}`,
		`{"type":"text","part":{"text":"===GO-MERGE-BEGIN===\nsecond\n===GO-MERGE-END==="}}`,
	}, "\n")

	// Act
	answer := Builder{}.ExtractAnswer(output)

	// Assert
	if answer != "second" {
		t.Fatalf("expected the last marked answer, got %q", answer)
	}
}

func TestReviewApproved_ShouldRequireVerdictOnItsOwnLine(t *testing.T) {
	// Arrange
	tests := []struct {
		name   string
		answer string
		want   bool
	}{
		{name: "exact match", answer: "VERDICT: approve", want: true},
		{name: "case insensitive", answer: "verdict: APPROVE", want: true},
		{name: "trailing content on the line", answer: "notes\nVERDICT: approve please", want: false},
		{name: "request changes", answer: "notes\nVERDICT: request-changes", want: false},
		{name: "no verdict", answer: "looks good to me", want: false},
		{name: "last line wins", answer: "VERDICT: request-changes\nVERDICT: approve", want: true},
		{name: "final request changes wins", answer: "VERDICT: approve\nVERDICT: request-changes", want: false},
		{name: "surrounding whitespace", answer: "  VERDICT: approve  ", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Act
			got := Builder{}.ReviewApproved(test.answer)

			// Assert
			if got != test.want {
				t.Fatalf("ReviewApproved(%q) = %t, want %t", test.answer, got, test.want)
			}
		})
	}
}
