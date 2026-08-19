package epic

import (
	"strings"
	"testing"
)

func TestEpic_SetBranchPrefix_ShouldSlugInput(t *testing.T) {
	// Arrange: users type tracker keys as they appear in the tracker, not as a
	// branch could be named.
	epic := newTestEpic(t)

	// Act
	if err := epic.SetBranchPrefix("  JIRA-123 "); err != nil {
		t.Fatal(err)
	}

	// Assert
	if epic.BranchPrefix != "jira-123" {
		t.Fatalf("unexpected prefix: %q", epic.BranchPrefix)
	}
	if err := epic.Validate(); err != nil {
		t.Fatalf("slugged prefix must validate: %v", err)
	}
}

func TestEpic_SetBranchPrefix_ShouldClearOnEmptyInput(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	if err := epic.SetBranchPrefix("jira-123"); err != nil {
		t.Fatal(err)
	}

	// Act
	if err := epic.SetBranchPrefix(""); err != nil {
		t.Fatal(err)
	}

	// Assert
	if epic.BranchPrefix != "" {
		t.Fatalf("expected prefix to be cleared, got %q", epic.BranchPrefix)
	}
}

func TestEpic_SetBranchPrefix_ShouldRejectChangeAfterBranchesAreCut(t *testing.T) {
	// Arrange: the branches are already pushed under their old names, so the
	// epic's copy of the prefix must not drift away from them.
	epic := newTestEpic(t)
	pullRequest, err := CreatePullRequest(epic.Issues[0].ID, "PR", "acme/widgets", "head", "base")
	if err != nil {
		t.Fatal(err)
	}
	epic.PullRequests = []PullRequest{pullRequest}

	// Act
	err = epic.SetBranchPrefix("jira-123")

	// Assert
	if err == nil {
		t.Fatal("expected a prefix change after branches are cut to fail")
	}
	if epic.BranchPrefix != "" {
		t.Fatalf("refused change must not be applied, got %q", epic.BranchPrefix)
	}
}

func TestEpic_Validate_ShouldRejectNonSluggedBranchPrefix(t *testing.T) {
	// Arrange: the prefix reaches a Git ref verbatim, so a hand-edited store
	// must not be able to smuggle path separators into it.
	epic := newTestEpic(t)
	epic.BranchPrefix = "feature/../etc"

	// Act
	err := epic.Validate()

	// Assert
	if err == nil {
		t.Fatal("expected an unslugged branch prefix to fail validation")
	}
}

func TestEpic_BranchName_ShouldJoinPrefixTitleAndID(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	if err := epic.SetBranchPrefix("JIRA-123"); err != nil {
		t.Fatal(err)
	}
	issue := Issue{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Title: "Fix that issue"}

	// Act
	name := epic.BranchName(issue)

	// Assert
	if want := "gm/jira-123-fix-that-issue-01ARZ3NDEKTSV4RRFFQ69G5FAV"; name != want {
		t.Fatalf("BranchName = %q, want %q", name, want)
	}
}

func TestEpic_BranchName_ShouldDropEmptySegments(t *testing.T) {
	// Arrange
	epic := newTestEpic(t)
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	tests := []struct {
		name   string
		prefix string
		title  string
		want   string
	}{
		{name: "no prefix", prefix: "", title: "Fix that issue",
			want: "gm/fix-that-issue-" + id},
		{name: "untitleable issue", prefix: "jira-123", title: "!!!",
			want: "gm/jira-123-" + id},
		{name: "neither", prefix: "", title: "!!!", want: "gm/" + id},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			epic.BranchPrefix = tt.prefix

			// Act
			name := epic.BranchName(Issue{ID: id, Title: tt.title})

			// Assert
			if name != tt.want {
				t.Fatalf("BranchName = %q, want %q", name, tt.want)
			}
		})
	}
}

func TestEpic_BranchName_ShouldCapLongTitles(t *testing.T) {
	// Arrange: a title has no length limit, but a branch name has to stay
	// readable in a terminal listing branches.
	epic := newTestEpic(t)
	issue := Issue{
		ID:    "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Title: strings.Repeat("very long title ", 10),
	}

	// Act
	name := epic.BranchName(issue)

	// Assert
	slug := strings.TrimSuffix(strings.TrimPrefix(name, BranchNamespace), "-"+issue.ID)
	if len(slug) > branchTitleLimit {
		t.Fatalf("title slug %q exceeds the cap of %d", slug, branchTitleLimit)
	}
	if !strings.HasSuffix(name, issue.ID) {
		t.Fatalf("issue id must survive the cap: %q", name)
	}
}
