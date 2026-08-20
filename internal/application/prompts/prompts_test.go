package prompts

import (
	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
	"strings"
	"testing"
)

func TestEpic_ShouldRenderRoleTemplate(t *testing.T) {
	// Arrange
	epic := epic.Epic{Title: "Add sandbox loop", Body: "Run agents in containers."}

	// Act
	prompt, err := Epic(epic, agent.AgentRoleRefiner, agent.SessionModeFresh)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Add sandbox loop") ||
		!strings.Contains(prompt, "Run agents in containers.") {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestEpic_ShouldRenderEveryRoleTheEpicLoopDrives(t *testing.T) {
	// Arrange: a role the loop dispatches but this cannot render is a round that
	// fails at the last moment.
	detail := epic.Epic{
		ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		Body: "Split the single checkout page.", Repositories: []string{"acme/api"},
	}

	// Act & Assert
	for _, role := range []agent.AgentRole{
		agent.AgentRoleRefiner, agent.AgentRoleIssueReviewer,
	} {
		rendered, err := Epic(detail, role, agent.SessionModeFresh)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if !strings.Contains(rendered, detail.Title) {
			t.Fatalf("%s: expected the epic's title, got:\n%s", role, rendered)
		}
		if !strings.Contains(rendered, detail.Body) {
			t.Fatalf("%s: expected the epic's body, got:\n%s", role, rendered)
		}
		if strings.Contains(rendered, "<no value>") {
			t.Fatalf("%s: expected every placeholder filled, got:\n%s", role, rendered)
		}
	}
}

func TestEpic_ShouldRejectAnIssueScopedRole(t *testing.T) {
	// Arrange: the epic loop drafts; coding and reviewing a pull request belong to
	// the issue loop.
	detail := epic.Epic{ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk"}

	// Act & Assert
	for _, role := range []agent.AgentRole{
		agent.AgentRoleCoding, agent.AgentRolePRReviewer, agent.AgentRoleMerge, "nonsense",
	} {
		if _, err := Epic(detail, role, agent.SessionModeFresh); err == nil {
			t.Fatalf("expected role %q to be rejected", role)
		}
	}
}

// The reviewer's critique reaches the guest as root-comments.md, but a refiner
// that has to go looking for its own feedback is a refiner that ignores it. The
// prompt says it turns criticism into a better plan, so the criticism has to be
// in the prompt.
func TestEpic_ShouldPutTheLastReviewInFrontOfTheRefiner(t *testing.T) {
	// Arrange
	const finding = "issue-3 and issue-4 both add the balance endpoint."
	detail := reviewedEpic(
		epic.Comment{ID: "old", Author: "issue-reviewer", Body: "An earlier round's finding."},
		epic.Comment{ID: "new", Author: "issue-reviewer", Body: finding},
	)

	// Act
	rendered, err := Epic(detail, agent.AgentRoleRefiner, agent.SessionModeFresh)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "## The last review of this plan") {
		t.Fatalf("expected the critique section, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, finding) {
		t.Fatalf("expected the newest finding, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "An earlier round's finding.") {
		t.Fatalf("expected only the newest review, got:\n%s", rendered)
	}
}

func TestEpic_ShouldSayWhenNothingHasBeenReviewedYet(t *testing.T) {
	// Arrange: the first pass, and a human comment that is not a review.
	detail := reviewedEpic(epic.Comment{ID: "c1", Author: "luuk", Body: "Do the API first."})

	// Act
	rendered, err := Epic(detail, agent.AgentRoleRefiner, agent.SessionModeFresh)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "## No review yet") {
		t.Fatalf("expected the no-critique section, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "## The last review of this plan") {
		t.Fatalf("expected no critique section, got:\n%s", rendered)
	}
}

// Both epic roles run in the same guest as the issue roles, and neither prompt
// describes the mount on its own.
func TestEpic_ShouldAppendTheEnvironmentSection(t *testing.T) {
	// Arrange
	detail := epic.Epic{
		ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		Body: "Split the single checkout page.", Repositories: []string{"acme/api"},
	}

	// Act & Assert
	for _, role := range []agent.AgentRole{
		agent.AgentRoleRefiner, agent.AgentRoleIssueReviewer,
	} {
		rendered, err := Epic(detail, role, agent.SessionModeFresh)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if !strings.HasSuffix(strings.TrimSpace(rendered), strings.TrimSpace(environment)) {
			t.Fatalf("%s: expected the environment section appended, got:\n%s", role, rendered)
		}
	}
}

func reviewedEpic(comments ...epic.Comment) epic.Epic {
	return epic.Epic{
		ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		Body: "Split the single checkout page.", Repositories: []string{"acme/api"},
		Issues: []epic.Issue{{
			ID: "root", Title: "Checkout rewrite", State: epic.IssueStateOpen,
			Body: "Split the single checkout page.", Comments: comments,
		}},
	}
}

func TestEpic_ShouldListTheRepositoriesItIsScopedTo(t *testing.T) {
	// Arrange: the scope is what stops a refiner proposing work in a repository
	// the epic may not touch.
	detail := epic.Epic{
		ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		Repositories: []string{"acme/api", "acme/web"},
	}

	// Act
	rendered, err := Epic(detail, agent.AgentRoleRefiner, agent.SessionModeFresh)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	for _, repository := range detail.Repositories {
		if !strings.Contains(rendered, repository) {
			t.Fatalf("expected %q named, got:\n%s", repository, rendered)
		}
	}
}
