package prompts

import (
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/domain/agent"
)

func testIssueContext() IssueContext {
	return IssueContext{
		IssuePath:    "/work/issues/acme__widgets/issue-1.md",
		IssueTitle:   "Add the widget endpoint",
		RepoDir:      "/work/repo",
		Branch:       "go-merge/issue-1",
		BaseBranch:   "main",
		BaseCommit:   "abc1234",
		Conversation: "No discussion yet.",
		Repositories: []string{"acme__gadgets"},
	}
}

func TestIssue_ShouldRenderCodingPrompt(t *testing.T) {
	// Act
	prompt, err := Issue(testIssueContext(), agent.AgentRoleCoding, agent.SessionModeFresh)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Add the widget endpoint", "/work/repo", "go-merge/issue-1", "abc1234",
		"No discussion yet.", "/work/repos/acme__gadgets",
		"GO_MERGE_DOCKER_BIND_SOURCE", "editing directory",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt is missing %q:\n%s", want, prompt)
		}
	}
}

func TestIssue_ShouldAppendTheEnvironmentSection(t *testing.T) {
	// Act
	prompt, err := Issue(testIssueContext(), agent.AgentRolePRReviewer, agent.SessionModeFresh)

	// Assert: every role gets the same description of the machine.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "## Where you are running") {
		t.Fatalf("environment section missing:\n%s", prompt)
	}
}

func TestIssue_ShouldKeepTheVerdictContractForTheReviewer(t *testing.T) {
	// Arrange: opencode.ReviewApproved parses exactly this line, so the
	// prompt and the parser have to agree on its spelling.
	prompt, err := Issue(testIssueContext(), agent.AgentRolePRReviewer, agent.SessionModeFresh)
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if !strings.Contains(prompt, "VERDICT: approve") ||
		!strings.Contains(prompt, "VERDICT: request-changes") {
		t.Fatalf("verdict contract missing:\n%s", prompt)
	}
}

func TestIssue_ShouldNotRestateTheAnswerMarkers(t *testing.T) {
	// Arrange: opencode.Command appends the marker instruction to every
	// prompt. A prompt that also states it gives the agent two conflicting
	// instructions about where its answer goes.
	for _, role := range []agent.AgentRole{agent.AgentRoleCoding, agent.AgentRolePRReviewer} {
		prompt, err := Issue(testIssueContext(), role, agent.SessionModeFresh)
		if err != nil {
			t.Fatal(err)
		}

		// Assert
		if strings.Contains(prompt, "===GO-MERGE-BEGIN===") {
			t.Fatalf("%s prompt restates the answer markers:\n%s", role, prompt)
		}
	}
}

func TestIssue_ShouldRejectAnEpicRole(t *testing.T) {
	// Act
	_, err := Issue(testIssueContext(), agent.AgentRoleRefiner, agent.SessionModeFresh)

	// Assert
	if err == nil {
		t.Fatal("expected an epic-scoped role to be rejected")
	}
}

func TestIssue_ShouldOmitReferenceReposWhenThereAreNone(t *testing.T) {
	// Arrange
	context := testIssueContext()
	context.Repositories = nil

	// Act
	prompt, err := Issue(context, agent.AgentRoleCoding, agent.SessionModeFresh)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "Read-only checkouts you may consult") {
		t.Fatalf("expected no reference-repo section:\n%s", prompt)
	}
}

func TestIssue_ShouldRenderEveryRoleTheIssueLoopDrives(t *testing.T) {
	// Arrange: a role the loop dispatches but this cannot render is a round that
	// fails at the last moment.
	context := IssueContext{
		IssuePath: "/work/tree/root.md", IssueTitle: "Split cart",
		RepoDir: "acme__api", Branch: "gm/cart", BaseBranch: "main",
		BaseCommit: "abc123", Conversation: "No prior discussion.",
	}

	// Act & Assert
	for _, role := range []agent.AgentRole{
		agent.AgentRoleCoding, agent.AgentRolePRReviewer, agent.AgentRoleMerge,
	} {
		rendered, err := Issue(context, role, agent.SessionModeFresh)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if strings.Contains(rendered, "<no value>") {
			t.Fatalf("%s: expected every placeholder filled, got:\n%s", role, rendered)
		}
		if !strings.Contains(rendered, context.IssueTitle) {
			t.Fatalf("%s: expected the issue named, got:\n%s", role, rendered)
		}
	}
}

func TestIssue_ShouldCarryThePriorDiscussionIntoEveryRole(t *testing.T) {
	// Arrange: the conversation is pre-rendered, so the template only places it.
	context := IssueContext{
		IssuePath: "/work/tree/root.md", IssueTitle: "Split cart",
		RepoDir: "acme__api", Branch: "gm/cart", BaseBranch: "main",
		BaseCommit:   "abc123",
		Conversation: "## pr_reviewer\n\nrename the field",
	}

	// Act & Assert
	for _, role := range []agent.AgentRole{
		agent.AgentRoleCoding, agent.AgentRolePRReviewer, agent.AgentRoleMerge,
	} {
		rendered, err := Issue(context, role, agent.SessionModeFresh)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if !strings.Contains(rendered, "rename the field") {
			t.Fatalf("%s: expected the discussion carried in, got:\n%s", role, rendered)
		}
	}
}

func TestIssue_ShouldNameTheReadOnlyReferenceCheckouts(t *testing.T) {
	// Arrange: the other repositories are mounted read-only, and the agent has to
	// know they are there to read across them.
	context := IssueContext{
		IssuePath: "/work/tree/root.md", IssueTitle: "Split cart",
		RepoDir: "acme__api", Branch: "gm/cart", BaseBranch: "main",
		BaseCommit: "abc123", Conversation: "No prior discussion.",
		Repositories: []string{"acme__web", "acme__infra"},
	}

	// Act
	rendered, err := Issue(context, agent.AgentRoleCoding, agent.SessionModeFresh)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	for _, repository := range context.Repositories {
		if !strings.Contains(rendered, repository) {
			t.Fatalf("expected %q named, got:\n%s", repository, rendered)
		}
	}
}
