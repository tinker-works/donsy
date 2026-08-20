package usecases

import (
	"strings"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

func TestConversationSection_ShouldSayWhenThereIsNoDiscussionYet(t *testing.T) {
	// Arrange: a first round has nothing to catch up on, and an empty heading
	// would read as a thread the agent should already know about.

	// Act
	section := conversationFor(
		epicpkg.PullRequest{ID: "pr-1"}, agent.AgentRoleCoding, agent.SessionModeFresh,
	)

	// Assert
	if !strings.Contains(section, "No discussion") {
		t.Fatalf("expected the empty-thread sentence, got %q", section)
	}
}

func TestConversationSection_ShouldRenderEveryTurnWithItsAuthor(t *testing.T) {
	// Arrange: an agent that cannot see the previous round's findings repeats work
	// that was answered.
	created := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	record := epicpkg.PullRequest{ID: "pr-1", Comments: []epicpkg.Comment{
		{ID: "c1", Author: "pr_reviewer", CreatedAt: created, Body: "  rename the field  "},
		{ID: "c2", Author: "coding", CreatedAt: created, Body: "renamed it"},
	}}

	// Act
	section := conversationFor(record, agent.AgentRoleCoding, agent.SessionModeFresh)

	// Assert
	if !strings.Contains(section, "## Discussion so far") {
		t.Fatalf("expected the section heading, got %q", section)
	}
	for _, comment := range record.Comments {
		if !strings.Contains(section, "### "+comment.Author) {
			t.Fatalf("expected %q attributed, got %q", comment.Author, section)
		}
	}
	if !strings.Contains(section, "rename the field\n") {
		t.Fatalf("expected the body trimmed, got %q", section)
	}
}

// A resumed session already holds everything up to and including the role's own
// last turn. Re-sending it invites the agent to answer points it answered.
func TestConversationFor_ShouldRenderOnlyWhatIsNewSinceTheRolesOwnTurn(t *testing.T) {
	// Arrange
	created := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	record := epicpkg.PullRequest{ID: "pr-1", Comments: []epicpkg.Comment{
		{ID: "c1", Author: "pr-reviewer", CreatedAt: created, Body: "rename the field"},
		{ID: "c2", Author: "coding", CreatedAt: created, Body: "renamed it"},
		{ID: "c3", Author: "pr-reviewer", CreatedAt: created, Body: "now the test is wrong"},
		{ID: "c4", Author: "luuk", CreatedAt: created, Body: "agreed, fix the test"},
	}}

	// Act
	section := conversationFor(record, agent.AgentRolePRReviewer, agent.SessionModeContinue)

	// Assert
	if !strings.Contains(section, "## Said since your last round") {
		t.Fatalf("expected the delta heading, got %q", section)
	}
	if !strings.Contains(section, "agreed, fix the test") {
		t.Fatalf("expected what came after the role's turn, got %q", section)
	}
	for _, stale := range []string{"rename the field", "renamed it", "now the test is wrong"} {
		if strings.Contains(section, stale) {
			t.Fatalf("expected %q left to the session, got %q", stale, section)
		}
	}
}

// The role's own last comment is the boundary, so a role whose turn is the final
// comment has nothing new to be told.
func TestConversationFor_ShouldSayWhenNothingHasBeenSaidSince(t *testing.T) {
	// Arrange
	record := epicpkg.PullRequest{ID: "pr-1", Comments: []epicpkg.Comment{
		{ID: "c1", Author: "coding", Body: "renamed it"},
	}}

	// Act
	section := conversationFor(record, agent.AgentRoleCoding, agent.SessionModeContinue)

	// Assert
	if !strings.Contains(section, "Nothing has been said") {
		t.Fatalf("expected the empty-delta sentence, got %q", section)
	}
}

// A role that has never spoken has nothing in session for the thread to be
// redundant with, so it gets all of it.
func TestConversationFor_ShouldRenderTheWholeThreadWhenTheRoleHasNotSpoken(t *testing.T) {
	// Arrange
	record := epicpkg.PullRequest{ID: "pr-1", Comments: []epicpkg.Comment{
		{ID: "c1", Author: "luuk", Body: "do the API first"},
		{ID: "c2", Author: "coding", Body: "done"},
	}}

	// Act
	section := conversationFor(record, agent.AgentRolePRReviewer, agent.SessionModeContinue)

	// Assert
	for _, want := range []string{"do the API first", "done"} {
		if !strings.Contains(section, want) {
			t.Fatalf("expected %q carried in, got %q", want, section)
		}
	}
}

// A fresh round has no session to lean on, so it still gets the whole thread
// however much of it the role itself wrote.
func TestConversationFor_ShouldKeepTheWholeThreadForAFreshRound(t *testing.T) {
	// Arrange
	record := epicpkg.PullRequest{ID: "pr-1", Comments: []epicpkg.Comment{
		{ID: "c1", Author: "pr-reviewer", Body: "rename the field"},
		{ID: "c2", Author: "coding", Body: "renamed it"},
	}}

	// Act
	section := conversationFor(record, agent.AgentRolePRReviewer, agent.SessionModeFresh)

	// Assert
	if !strings.Contains(section, "## Discussion so far") ||
		!strings.Contains(section, "rename the field") {
		t.Fatalf("expected the whole thread, got %q", section)
	}
}

func TestIssueContext_ShouldCarryOnlyTheNewCommentsWhenResuming(t *testing.T) {
	// Arrange
	current := epicpkg.Epic{
		ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		Repositories: []string{"acme/api"},
	}
	issue := epicpkg.Issue{ID: "cart", Title: "Split cart", Repository: "acme/api"}
	record := epicpkg.PullRequest{
		ID: "pr-1", IssueID: "cart", Title: "Split cart", Repository: "acme/api",
		Head: "gm/cart", Base: "main",
		Comments: []epicpkg.Comment{
			{ID: "c1", Author: "coding", Body: "first attempt"},
			{ID: "c2", Author: "pr-reviewer", Body: "the empty case is unhandled"},
		},
	}

	// Act
	context := issueContext(current, issue, record, "abc1234",
		agent.AgentRoleCoding, agent.SessionModeContinue)

	// Assert
	if !strings.Contains(context.Conversation, "the empty case is unhandled") {
		t.Fatalf("expected the new finding, got %q", context.Conversation)
	}
	if strings.Contains(context.Conversation, "first attempt") {
		t.Fatalf("expected the round's own report left to the session, got %q", context.Conversation)
	}
}

func TestIssueContext_ShouldNameTheGuestPathsTheAgentActuallySees(t *testing.T) {
	// Arrange: the mount layout is the adapter's, and the prompt has to describe
	// the guest's view of it rather than the host's.
	current := epicpkg.Epic{
		ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		Repositories: []string{"acme/api", "acme/web", "acme/infra"},
	}
	issue := epicpkg.Issue{
		ID: "cart", Title: "Split cart", Repository: "acme/api",
		State: epicpkg.IssueStateOpen,
	}
	record := epicpkg.PullRequest{
		ID: "pr-1", IssueID: "cart", Title: "Split cart", Repository: "acme/api",
		Head: "gm/cart", Base: "main", Status: epicpkg.PullRequestOpen,
	}

	// Act
	context := issueContext(current, issue, record, "abc1234",
		agent.AgentRoleCoding, agent.SessionModeFresh)

	// Assert
	if context.IssuePath != "/work/issues/acme__api/cart.md" {
		t.Fatalf("expected the guest issue path, got %q", context.IssuePath)
	}
	if context.RepoDir != "/work/repo" {
		t.Fatalf("expected the writable checkout at /work/repo, got %q", context.RepoDir)
	}
	if context.Branch != record.Head || context.BaseBranch != record.Base {
		t.Fatalf("expected both branches named, got %+v", context)
	}
	if context.BaseCommit != "abc1234" {
		t.Fatalf("expected the base commit, got %q", context.BaseCommit)
	}
}

func TestIssueContext_ShouldListEveryOtherRepositoryAsAReference(t *testing.T) {
	// Arrange: the issue's own repository is the writable checkout, so it must not
	// also appear as a read-only reference.
	current := epicpkg.Epic{
		ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		Repositories: []string{"acme/api", "acme/web"},
	}
	issue := epicpkg.Issue{ID: "cart", Title: "Split cart", Repository: "acme/api"}
	record := epicpkg.PullRequest{
		ID: "pr-1", IssueID: "cart", Title: "Split cart", Repository: "acme/api",
		Head: "gm/cart", Base: "main",
	}

	// Act
	context := issueContext(current, issue, record, "abc1234",
		agent.AgentRoleCoding, agent.SessionModeFresh)

	// Assert
	if len(context.Repositories) != 1 || context.Repositories[0] != "acme__web" {
		t.Fatalf("expected only the other repository, slugged, got %v", context.Repositories)
	}
}

func TestIssueContext_ShouldOfferNoReferencesForASingleRepositoryEpic(t *testing.T) {
	// Arrange
	current := epicpkg.Epic{
		ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		Repositories: []string{"acme/api"},
	}
	issue := epicpkg.Issue{ID: "cart", Title: "Split cart", Repository: "acme/api"}
	record := epicpkg.PullRequest{
		ID: "pr-1", IssueID: "cart", Title: "Split cart", Repository: "acme/api",
	}

	// Act
	context := issueContext(current, issue, record, "abc1234",
		agent.AgentRoleCoding, agent.SessionModeFresh)

	// Assert
	if len(context.Repositories) != 0 {
		t.Fatalf("expected no references, got %v", context.Repositories)
	}
}

func TestAddReport_ShouldStandInForARoundThatSaidNothing(t *testing.T) {
	// Arrange: a round with no report still leaves a comment, so the thread shows
	// that it ran at all.
	detail := epicpkg.Epic{
		ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		State: epicpkg.EpicStateReady,
		Issues: []epicpkg.Issue{
			{ID: "root", Title: "Checkout rewrite", State: epicpkg.IssueStateOpen},
			{ID: "cart", ParentID: "root", Title: "Split cart", Repository: "acme/api",
				State: epicpkg.IssueStateOpen},
		},
		PullRequests: []epicpkg.PullRequest{{
			ID: "pr-1", IssueID: "cart", Title: "Split cart",
			Status: epicpkg.PullRequestOpen,
		}},
	}
	record := detail.PullRequests[0]

	// Act
	if err := addReport(&detail, &record, "coding", "   ", "abc1234def"); err != nil {
		t.Fatal(err)
	}

	// Assert
	comments := detail.PullRequests[0].Comments
	if len(comments) != 1 {
		t.Fatalf("expected one comment, got %+v", comments)
	}
	if !strings.Contains(comments[0].Body, "no report") {
		t.Fatalf("expected the stand-in body, got %q", comments[0].Body)
	}
}

func TestAddReport_ShouldPostWhatTheRoundActuallySaid(t *testing.T) {
	// Arrange
	detail := epicpkg.Epic{
		ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		State: epicpkg.EpicStateReady,
		Issues: []epicpkg.Issue{
			{ID: "root", Title: "Checkout rewrite", State: epicpkg.IssueStateOpen},
			{ID: "cart", ParentID: "root", Title: "Split cart", Repository: "acme/api",
				State: epicpkg.IssueStateOpen},
		},
		PullRequests: []epicpkg.PullRequest{{
			ID: "pr-1", IssueID: "cart", Title: "Split cart",
			Status: epicpkg.PullRequestOpen,
		}},
	}
	record := detail.PullRequests[0]

	// Act
	if err := addReport(&detail, &record, "coding", "  split the cart step out  ",
		"abc1234def"); err != nil {
		t.Fatal(err)
	}

	// Assert
	if got := detail.PullRequests[0].Comments[0].Body; got != "split the cart step out" {
		t.Fatalf("expected the trimmed report, got %q", got)
	}
}

func TestAddReport_ShouldReportARecordItCannotAttachTo(t *testing.T) {
	// Arrange: a pull request rewritten out from under a finishing round.
	detail := epicpkg.Epic{
		ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		State:  epicpkg.EpicStateReady,
		Issues: []epicpkg.Issue{{ID: "root", Title: "Checkout rewrite"}},
	}
	record := epicpkg.PullRequest{ID: "ghost", IssueID: "cart", Title: "Gone"}

	// Act
	err := addReport(&detail, &record, "coding", "done", "abc1234def")

	// Assert
	if err == nil {
		t.Fatal("expected the missing record to be reported")
	}
}

func TestTerminalSubjects_ShouldOnlyIncludeWhatNothingWillMove(t *testing.T) {
	// Arrange: an epic waiting on its children and a pull request blocked by its
	// parent both have no role this tick, yet reclaiming their sandboxes would delete
	// and rebuild the same instance minutes later.
	epics := []epicpkg.Epic{
		{
			ID: "done", State: epicpkg.EpicStateDone,
			Issues: []epicpkg.Issue{{ID: "done-root", State: epicpkg.IssueStateOpen}},
		},
		{
			ID: "closed", State: epicpkg.EpicStateClosed,
			Issues: []epicpkg.Issue{{ID: "closed-root", State: epicpkg.IssueStateOpen}},
		},
		{
			ID: "failed", State: epicpkg.EpicStateFailed,
			Issues: []epicpkg.Issue{{ID: "failed-root", State: epicpkg.IssueStateOpen}},
		},
		{
			ID: "live", State: epicpkg.EpicStateReady,
			Issues: []epicpkg.Issue{
				{ID: "merged", State: epicpkg.IssueStateMerged},
				{ID: "closed-issue", State: epicpkg.IssueStateClosed},
				{ID: "awaiting-merge", State: epicpkg.IssueStatePR},
				{ID: "coding", State: epicpkg.IssueStateCoding},
			},
		},
	}

	// Act
	terminal := TerminalSubjects(epics)

	// Assert
	shouldBe := []agent.AgentSubject{
		{Kind: agent.AgentSubjectEpic, ID: "done"},
		{Kind: agent.AgentSubjectEpic, ID: "closed"},
		{Kind: agent.AgentSubjectIssue, ID: "done-root"},
		{Kind: agent.AgentSubjectIssue, ID: "closed-root"},
		{Kind: agent.AgentSubjectIssue, ID: "merged"},
		{Kind: agent.AgentSubjectIssue, ID: "closed-issue"},
	}
	for _, subject := range shouldBe {
		if _, ok := terminal[subject]; !ok {
			t.Fatalf("expected %+v to be terminal", subject)
		}
	}
	// Failed transitions back to Concept, and whoever restarts a failed epic
	// benefits from its refiner sandbox still being there. An approval is invalidated
	// by the next push, which sends the issue back to coding.
	shouldNotBe := []agent.AgentSubject{
		{Kind: agent.AgentSubjectEpic, ID: "failed"},
		{Kind: agent.AgentSubjectEpic, ID: "live"},
		{Kind: agent.AgentSubjectIssue, ID: "failed-root"},
		{Kind: agent.AgentSubjectIssue, ID: "awaiting-merge"},
		{Kind: agent.AgentSubjectIssue, ID: "coding"},
	}
	for _, subject := range shouldNotBe {
		if _, ok := terminal[subject]; ok {
			t.Fatalf("expected %+v not to be terminal", subject)
		}
	}
}
