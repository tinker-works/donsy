package prompts

import (
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

// resumableEpicRoles and resumableIssueRoles are the roles that get a second
// round on the same subject, split by which loop drives them.
var (
	resumableEpicRoles  = []agent.AgentRole{agent.AgentRoleRefiner, agent.AgentRoleIssueReviewer}
	resumableIssueRoles = []agent.AgentRole{agent.AgentRoleCoding, agent.AgentRolePRReviewer}
)

func continueEpic() epic.Epic {
	return reviewedEpic(epic.Comment{
		ID: "c1", Author: "issue-reviewer", Body: "issue-3 duplicates issue-4.",
	})
}

func TestEpic_ShouldRenderTheContinuePromptForEveryResumableRole(t *testing.T) {
	// Arrange
	detail := continueEpic()

	// Act & Assert
	for _, role := range resumableEpicRoles {
		t.Run(string(role), func(t *testing.T) {
			rendered, err := Epic(detail, role, agent.SessionModeContinue)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rendered, "<no value>") {
				t.Fatalf("expected every placeholder filled, got:\n%s", rendered)
			}
			if !strings.Contains(rendered, detail.Title) {
				t.Fatalf("expected the epic's title, got:\n%s", rendered)
			}
		})
	}
}

func TestIssue_ShouldRenderTheContinuePromptForEveryResumableRole(t *testing.T) {
	// Act & Assert
	for _, role := range resumableIssueRoles {
		t.Run(string(role), func(t *testing.T) {
			rendered, err := Issue(testIssueContext(), role, agent.SessionModeContinue)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rendered, "<no value>") {
				t.Fatalf("expected every placeholder filled, got:\n%s", rendered)
			}
			if !strings.Contains(rendered, "Add the widget endpoint") {
				t.Fatalf("expected the issue named, got:\n%s", rendered)
			}
		})
	}
}

// The whole point of a continue prompt is that it does not re-send what the
// resumed session already holds. A continue prompt that grows to the size of
// the fresh one has quietly stopped being one.
func TestPrompts_ShouldMakeTheContinuePromptShorterThanTheFreshOne(t *testing.T) {
	// Arrange
	detail := continueEpic()

	// Act & Assert
	for _, role := range resumableEpicRoles {
		t.Run(string(role), func(t *testing.T) {
			fresh, err := Epic(detail, role, agent.SessionModeFresh)
			if err != nil {
				t.Fatal(err)
			}
			resumed, err := Epic(detail, role, agent.SessionModeContinue)
			if err != nil {
				t.Fatal(err)
			}
			if len(resumed) >= len(fresh) {
				t.Fatalf("continue prompt is %d bytes against a fresh %d", len(resumed), len(fresh))
			}
		})
	}
	for _, role := range resumableIssueRoles {
		t.Run(string(role), func(t *testing.T) {
			fresh, err := Issue(testIssueContext(), role, agent.SessionModeFresh)
			if err != nil {
				t.Fatal(err)
			}
			resumed, err := Issue(testIssueContext(), role, agent.SessionModeContinue)
			if err != nil {
				t.Fatal(err)
			}
			if len(resumed) >= len(fresh) {
				t.Fatalf("continue prompt is %d bytes against a fresh %d", len(resumed), len(fresh))
			}
		})
	}
}

// A continue prompt leans on the session for context, so the values that are
// re-resolved every round are exactly the ones it may not let the agent
// remember: a reviewer diffing a remembered base reviews the wrong range.
func TestIssue_ShouldRestateWhatMovesBetweenRoundsWhenResuming(t *testing.T) {
	// Arrange
	context := testIssueContext()

	// Act & Assert
	for _, role := range resumableIssueRoles {
		t.Run(string(role), func(t *testing.T) {
			rendered, err := Issue(context, role, agent.SessionModeContinue)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				context.BaseCommit, context.RepoDir, context.IssuePath,
				"/work/repos/" + context.Repositories[0],
			} {
				if !strings.Contains(rendered, want) {
					t.Fatalf("continue prompt is missing %q:\n%s", want, rendered)
				}
			}
		})
	}
}

// opencode.ReviewApproved matches the verdict line literally, so a continue
// prompt that assumes the session remembers its spelling is one that eventually
// approves nothing.
func TestPrompts_ShouldKeepTheVerdictContractInEveryReviewPrompt(t *testing.T) {
	// Arrange & Act & Assert
	for _, mode := range []agent.SessionMode{
		agent.SessionModeFresh, agent.SessionModeContinue,
	} {
		epicReview, err := Epic(continueEpic(), agent.AgentRoleIssueReviewer, mode)
		if err != nil {
			t.Fatal(err)
		}
		pullRequestReview, err := Issue(testIssueContext(), agent.AgentRolePRReviewer, mode)
		if err != nil {
			t.Fatal(err)
		}
		for _, rendered := range []string{epicReview, pullRequestReview} {
			if !strings.Contains(rendered, "VERDICT: approve") ||
				!strings.Contains(rendered, "VERDICT: request-changes") {
				t.Fatalf("%s: verdict contract missing:\n%s", mode, rendered)
			}
		}
	}
}

// Builder.Command appends the marker instruction to every prompt in both modes.
// A prompt that also states it gives the agent two answers to the same question.
func TestPrompts_ShouldNotRestateTheAnswerMarkersWhenResuming(t *testing.T) {
	// Act & Assert
	for _, role := range resumableEpicRoles {
		rendered, err := Epic(continueEpic(), role, agent.SessionModeContinue)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rendered, "===GO-MERGE-BEGIN===") {
			t.Fatalf("%s restates the answer markers:\n%s", role, rendered)
		}
	}
	for _, role := range resumableIssueRoles {
		rendered, err := Issue(testIssueContext(), role, agent.SessionModeContinue)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rendered, "===GO-MERGE-BEGIN===") {
			t.Fatalf("%s restates the answer markers:\n%s", role, rendered)
		}
	}
}

// The short environment section drops the description of the machine, which the
// session already has, but keeps the rules that fail a round quietly.
func TestPrompts_ShouldAppendTheShortEnvironmentWhenResuming(t *testing.T) {
	// Act
	rendered, err := Issue(testIssueContext(), agent.AgentRoleCoding, agent.SessionModeContinue)
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if !strings.HasSuffix(strings.TrimSpace(rendered), strings.TrimSpace(environmentContinue)) {
		t.Fatalf("expected the short environment section appended, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "## Where you are running") {
		t.Fatalf("expected the full environment section left out, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, ".github/") {
		t.Fatalf("expected the .github rule kept, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "GO_MERGE_DOCKER_BIND_SOURCE") ||
		!strings.Contains(rendered, "remains `/work/repo` for editing") {
		t.Fatalf("expected the Docker bind source guidance kept, got:\n%s", rendered)
	}
}

// The zero SessionMode is not SessionModeFresh. Falling through to the fresh
// template for an unmapped pair is the kind of wrong that reads as working, so
// the lookup is total and every unmapped pair is an error.
func TestPrompts_ShouldRejectAModeTheRoleCannotRun(t *testing.T) {
	// Arrange
	tests := []struct {
		name string
		role agent.AgentRole
		mode agent.SessionMode
	}{
		{"merge has no session to resume", agent.AgentRoleMerge, agent.SessionModeContinue},
		{"the zero mode is not fresh", agent.AgentRoleCoding, agent.SessionMode("")},
		{"an unknown mode", agent.AgentRolePRReviewer, agent.SessionMode("nonsense")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Act & Assert
			if _, err := Issue(testIssueContext(), test.role, test.mode); err == nil {
				t.Fatalf("expected %q in %q mode to be rejected", test.role, test.mode)
			}
		})
	}

	// The epic loop refuses the same way.
	if _, err := Epic(continueEpic(), agent.AgentRoleRefiner, agent.SessionMode("")); err == nil {
		t.Fatal("expected the zero mode to be rejected")
	}
}

// The refiner's new task is the review that came back. It reaches the guest as
// root-comments.md either way, but a round that has to go looking for its own
// feedback is a round that ignores it.
func TestEpic_ShouldPutTheNewCritiqueInFrontOfAResumingRefiner(t *testing.T) {
	// Arrange
	const finding = "issue-3 and issue-4 both add the balance endpoint."
	detail := reviewedEpic(
		epic.Comment{ID: "old", Author: "issue-reviewer", Body: "An earlier round's finding."},
		epic.Comment{ID: "new", Author: "issue-reviewer", Body: finding},
	)

	// Act
	rendered, err := Epic(detail, agent.AgentRoleRefiner, agent.SessionModeContinue)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, finding) {
		t.Fatalf("expected the newest finding, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "An earlier round's finding.") {
		t.Fatalf("expected only the newest review, got:\n%s", rendered)
	}
}

// A resumed reviewer's files have been replaced underneath it, which is the one
// thing its session cannot tell it.
func TestPrompts_ShouldTellAResumingReviewerItsFilesChanged(t *testing.T) {
	// Act
	epicReview, err := Epic(continueEpic(), agent.AgentRoleIssueReviewer, agent.SessionModeContinue)
	if err != nil {
		t.Fatal(err)
	}
	pullRequestReview, err := Issue(
		testIssueContext(), agent.AgentRolePRReviewer, agent.SessionModeContinue,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if !strings.Contains(epicReview, "rewritten on disk") {
		t.Fatalf("expected the tree said to have changed, got:\n%s", epicReview)
	}
	if !strings.Contains(pullRequestReview, "replaced with the branch's new head") {
		t.Fatalf("expected the checkout said to have changed, got:\n%s", pullRequestReview)
	}
}
