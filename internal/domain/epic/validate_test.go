package epic

import (
	"strings"
	"testing"
	"time"
)

// valid is the smallest aggregate that passes Validate, so each case below can
// break exactly one rule.
func valid() Epic {
	created := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	return Epic{
		ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		State: EpicStateConcept, Repositories: []string{"api"},
		Issues: []Issue{
			{ID: "root", Title: "Checkout rewrite", State: IssueStateOpen, CreatedAt: created},
			{ID: "cart", ParentID: "root", Title: "Split cart", Repository: "api",
				State: IssueStateOpen, CreatedAt: created},
		},
	}
}

// sibling is a second issue under the root, which is what a dependency between
// two of them needs to exist at all.
func sibling(id string) Issue {
	return Issue{
		ID: id, ParentID: "root", Title: "Take payment", Repository: "api",
		State: IssueStateOpen, CreatedAt: time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC),
	}
}

func TestEpic_Validate_ShouldAcceptABlockedBySibling(t *testing.T) {
	// Arrange: nesting cannot order two issues under one parent, so a reference
	// between them is the whole point of BlockedBy.
	epic := valid()
	epic.Issues = append(epic.Issues, sibling("pay"))
	epic.Issues[1].BlockedBy = []string{"pay"}

	// Act & Assert
	if err := epic.Validate(); err != nil {
		t.Fatalf("expected a sibling dependency to validate: %v", err)
	}
}

func TestEpic_Validate_ShouldAcceptTheSmallestCompleteAggregate(t *testing.T) {
	// Arrange
	epic := valid()

	// Act & Assert
	if err := epic.Validate(); err != nil {
		t.Fatalf("expected the baseline aggregate to validate: %v", err)
	}
}

func TestEpic_Validate_ShouldRejectMalformedPersistedData(t *testing.T) {
	// Arrange: every case is a store somebody hand-edited or an older writer
	// left behind, and each must be refused rather than loaded.
	cases := map[string]func(*Epic){
		"no id":                    func(epic *Epic) { epic.ID = "" },
		"blank title":              func(epic *Epic) { epic.Title = "   " },
		"blank assignee":           func(epic *Epic) { epic.Assignee = "  " },
		"blank repository":         func(epic *Epic) { epic.Repositories = []string{"api", " "} },
		"duplicate repository":     func(epic *Epic) { epic.Repositories = []string{"api", "api"} },
		"unslugged branch prefix":  func(epic *Epic) { epic.BranchPrefix = "Jira 123/../etc" },
		"unknown state":            func(epic *Epic) { epic.State = EpicState("nonsense") },
		"negative drafting passes": func(epic *Epic) { epic.DraftingPasses = -1 },
		"no issues":                func(epic *Epic) { epic.Issues = nil },
		"invalid issue":            func(epic *Epic) { epic.Issues[1].Title = "" },
		"invalid issue comment":    func(epic *Epic) { epic.Issues[1].Comments = []Comment{{ID: "c1"}} },
		"root with a repository":   func(epic *Epic) { epic.Issues[0].Repository = "api" },
		"child without repository": func(epic *Epic) { epic.Issues[1].Repository = "" },
		"child outside the scope":  func(epic *Epic) { epic.Issues[1].Repository = "unscoped" },
		"duplicate issue":          func(epic *Epic) { epic.Issues = append(epic.Issues, epic.Issues[1]) },
		"two roots":                func(epic *Epic) { epic.Issues[1].ParentID = "" },
		"missing parent":           func(epic *Epic) { epic.Issues[1].ParentID = "ghost" },
		"invalid pull request":     func(epic *Epic) { epic.PullRequests = []PullRequest{{ID: "pr-1"}} },
		"blocked by a missing issue": func(epic *Epic) {
			epic.Issues[1].BlockedBy = []string{"ghost"}
		},
		"blocked by itself": func(epic *Epic) {
			epic.Issues[1].BlockedBy = []string{"cart"}
		},
		// An ancestor already waits on this issue through the tree, so the two
		// statements together are a deadlock.
		"blocked by an ancestor": func(epic *Epic) {
			epic.Issues[1].BlockedBy = []string{"root"}
		},
		"the same blocker twice": func(epic *Epic) {
			epic.Issues = append(epic.Issues, sibling("pay"))
			epic.Issues[1].BlockedBy = []string{"pay", "pay"}
		},
		"a cycle between siblings": func(epic *Epic) {
			epic.Issues = append(epic.Issues, sibling("pay"))
			epic.Issues[1].BlockedBy = []string{"pay"}
			epic.Issues[2].BlockedBy = []string{"cart"}
		},
		"pull request with no issue": func(epic *Epic) {
			epic.PullRequests = []PullRequest{
				{ID: "pr-1", IssueID: "ghost", Title: "Card form", Status: PullRequestOpen},
			}
		},
	}

	// Act & Assert
	for name, break_ := range cases {
		epic := valid()
		break_(&epic)
		if err := epic.Validate(); err == nil {
			t.Fatalf("expected %q to be refused", name)
		}
	}
}

func TestEpic_Validate_ShouldLetAnUnscopedEpicsIssuesNameAnyRepository(t *testing.T) {
	// Arrange: an epic without a declared scope leaves the choice to its issues.
	epic := valid()
	epic.Repositories = nil

	// Act & Assert
	if err := epic.Validate(); err != nil {
		t.Fatalf("expected an unscoped epic to accept any repository: %v", err)
	}
}

func TestEpic_Validate_ShouldAcceptDraftingPassesPastTheLimit(t *testing.T) {
	// Arrange: a failed epic restarted from Concept keeps its count, so passes
	// can legitimately exceed the limit across attempts.
	epic := valid()
	epic.DraftingPasses = MaxDraftingPasses + 2

	// Act & Assert
	if err := epic.Validate(); err != nil {
		t.Fatalf("expected passes past the limit to be accepted: %v", err)
	}
}

func TestEpic_RootIssue_ShouldReportAnEpicWithNoTree(t *testing.T) {
	// Arrange: an epic nobody has drafted has no root issue.
	epic := Epic{ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk"}

	// Act
	_, err := epic.RootIssue()

	// Assert
	if err == nil {
		t.Fatal("expected no root issue in an undrafted epic")
	}
}

func TestEpic_Close_ShouldSucceedOnAnUndraftedEpic(t *testing.T) {
	// Arrange: closing is still legal — there is simply no tree to walk.
	epic := Epic{ID: "checkout", Title: "Checkout rewrite", Assignee: "luuk",
		State: EpicStateConcept}

	// Act
	err := epic.Close()

	// Assert
	if err != nil {
		t.Fatalf("expected an undrafted epic to close: %v", err)
	}
	if epic.State != EpicStateClosed {
		t.Fatalf("expected the epic closed, got %q", epic.State)
	}
}

func TestEpic_CloseIssue_ShouldLeaveDeliveredWorkAlone(t *testing.T) {
	// Arrange: a merged child's work landed, and abandoning the tree around it
	// must not pretend otherwise.
	epic := valid()
	epic.Issues = append(epic.Issues, Issue{
		ID: "card", ParentID: "cart", Title: "Card form", Repository: "api",
		State: IssueStateMerged,
	})

	// Act
	err := epic.CloseIssue("cart")

	// Assert
	if err != nil {
		t.Fatalf("expected closing a tree with a merged child to succeed: %v", err)
	}
	states := map[string]IssueState{}
	for _, issue := range epic.Issues {
		states[issue.ID] = issue.State
	}
	if states["cart"] != IssueStateClosed {
		t.Fatalf("expected cart closed, got %q", states["cart"])
	}
	if states["card"] != IssueStateMerged {
		t.Fatalf("expected the merged child untouched, got %q", states["card"])
	}
}

func TestEpic_CloseIssue_ShouldReportAnUnknownIssue(t *testing.T) {
	// Arrange
	epic := valid()

	// Act
	err := epic.CloseIssue("ghost")

	// Assert
	if err == nil {
		t.Fatal("expected an unknown issue to be reported")
	}
}

func TestEpic_TransitionPullRequest_ShouldReportAnOrphanedRecord(t *testing.T) {
	// Arrange: a pull request whose issue was rewritten out from under it.
	epic := valid()
	epic.PullRequests = []PullRequest{{
		ID: "pr-1", IssueID: "ghost", Title: "Split cart", Status: PullRequestOpen,
	}}

	// Act
	err := epic.TransitionPullRequest("pr-1", PullRequestMerged)

	// Assert
	if err == nil {
		t.Fatal("expected an orphaned pull request to be reported")
	}
}

func TestEpic_UpdatePullRequest_ShouldValidateWhatTheChangeWrote(t *testing.T) {
	// Arrange: an in-place edit that breaks an invariant must not be persisted
	// silently.
	epic := valid()
	epic.PullRequests = []PullRequest{{
		ID: "pr-1", IssueID: "cart", Title: "Split cart", Status: PullRequestOpen,
	}}

	// Act
	err := epic.UpdatePullRequest("pr-1", func(pullRequest *PullRequest) error {
		pullRequest.Reviews = 99
		return nil
	})

	// Assert
	if err == nil {
		t.Fatal("expected the broken invariant to be refused")
	}
}

func TestCreateEpic_ShouldSurfaceAFailedRootIssue(t *testing.T) {
	// Arrange: the root issue carries the epic's own title, so a title the issue
	// refuses has to fail the whole creation.

	// Act
	_, err := CreateEpic(strings.Repeat(" ", 4), "luuk", "")

	// Assert
	if err == nil {
		t.Fatal("expected a blank title to be refused")
	}
}
