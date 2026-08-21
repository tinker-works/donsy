package epic

import (
	"errors"
	"fmt"
	"github.com/tinker-works/donsy/internal/domain"
	"sort"
	"strings"
)

var ErrIssueNotFound = errors.New("issue not found")

type Epic struct {
	ID           string
	Title        string
	Assignee     string
	Repositories []string
	Body         string
	State        EpicState
	// BranchPrefix tags every branch this epic cuts with the tracker item that
	// asked for the work, so a branch can be traced back to it. It is optional,
	// and stored already slugged so what is read is exactly what lands in the
	// branch name.
	BranchPrefix string
	Issues       []Issue
	PullRequests []PullRequest
	// DraftingPasses counts completed refine-review cycles. A reviewer and a
	// refiner can disagree forever, so past a limit the epic is proposed
	// regardless of the verdict.
	DraftingPasses int
}

// MaxDraftingPasses is how many refine-review cycles an epic gets before it is
// proposed whatever the reviewer thinks.
const MaxDraftingPasses = 3

func CreateEpic(title, assignee, body string) (Epic, error) {
	title = strings.TrimSpace(title)
	assignee = strings.TrimSpace(assignee)
	if title == "" {
		return Epic{}, fmt.Errorf("epic title is required")
	}
	if assignee == "" {
		return Epic{}, fmt.Errorf("epic assignee is required")
	}

	var root, err = CreateIssue(title, body)
	if err != nil {
		return Epic{}, err
	}

	epic := Epic{
		ID:       domain.MintULID(),
		Title:    title,
		Assignee: assignee,
		Body:     body,
		State:    EpicStateConcept,
		Issues:   []Issue{root},
	}
	return epic, epic.Validate()
}

// Close abandons the epic and everything under it. Closing the tree is part of
// closing the epic rather than a separate step, because an epic nobody will
// deliver must not leave pull requests open against branches still on the
// remote. Deleting those branches is the use case's job — the aggregate has no
// checkout to act on.
func (e *Epic) Close() error {
	// An epic that was never drafted has no root issue, and closing it is still
	// legal — there is simply no tree to walk.
	if root, err := e.RootIssue(); err == nil {
		if err := e.CloseIssue(root.ID); err != nil {
			return err
		}
	}
	return e.Apply(EpicEventClose)
}

// SetTitle renames the epic. The root issue is the epic restated, so the two
// carry one title and a refine round writes both from the same file. A closed
// epic keeps the name its history was recorded under.
func (e *Epic) SetTitle(title string) error {
	if e.State == EpicStateClosed {
		return fmt.Errorf("cannot set title of closed epic")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("epic title is required")
	}
	e.Title = title
	return nil
}

// SetBody replaces the epic's description, the brief a refine round revises.
func (e *Epic) SetBody(body string) error {
	if e.State == EpicStateClosed {
		return fmt.Errorf("cannot set body of closed epic")
	}
	e.Body = body
	return nil
}

// AbandonedBranches lists the branch behind every pull request the aggregate
// has closed without merging, so a caller holding the checkout can delete them.
// Merged branches are left out: their commits landed, and removing them is
// repository hygiene rather than part of abandoning work.
func (e *Epic) AbandonedBranches() []Branch {
	branches := make([]Branch, 0, len(e.PullRequests))
	for _, pullRequest := range e.PullRequests {
		if pullRequest.Status != PullRequestClosed {
			continue
		}
		branches = append(branches, Branch{
			PullRequestID: pullRequest.ID,
			IssueID:       pullRequest.IssueID,
			Repository:    pullRequest.Repository,
			Name:          pullRequest.Head,
		})
	}
	return branches
}

// Branch names one pull request's head, with enough context to find the
// checkout it lives in.
type Branch struct {
	PullRequestID string
	IssueID       string
	Repository    string
	Name          string
}

// BranchNamespace prefixes every branch this loop cuts, so a repository's own
// branches are never mistaken for an agent owns.
const BranchNamespace = "gm/"

// branchTitleLimit caps the title slug in a branch name. Long enough to
// recognise the issue at a glance, short enough that the ID stays visible in
// a terminal listing branches.
const branchTitleLimit = 40

// SetBranchPrefix records the tracker tag branches are named after. It slugs
// the input rather than rejecting free text, so a user can type "JIRA-123" and
// get what a branch can actually be called.
//
// Once branches exist the prefix is fixed: they are already on the remote under
// their old names, and renaming only the epic's copy would leave the two
// disagreeing about what a pull request's head is called.
func (e *Epic) SetBranchPrefix(prefix string) error {
	if len(e.PullRequests) > 0 {
		return fmt.Errorf("cannot change branch prefix after branches are cut")
	}
	e.BranchPrefix = domain.Slug(prefix)
	return nil
}

// BranchName is the branch one issue's work happens on: the namespace, the
// epic's optional prefix, a slug of the issue title, and the issue ID. Empty
// segments drop out, so an epic with no prefix yields gm/<title>-<id> and an
// issue whose title slugs to nothing yields gm/<id>.
//
// The ID goes last and stays as stored, so the name survives a retitled issue
// and its tail can be grepped against the store as-is.
func (e *Epic) BranchName(issue Issue) string {
	segments := make([]string, 0, 3)
	for _, segment := range []string{
		e.BranchPrefix,
		domain.SlugMax(issue.Title, branchTitleLimit),
		issue.ID,
	} {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return BranchNamespace + strings.Join(segments, "-")
}

func (e *Epic) AddIssue(parentID string, issue Issue) error {
	if strings.TrimSpace(issue.Repository) == "" {
		return fmt.Errorf("non-root issue requires a repository")
	}
	for _, i := range e.Issues {
		if i.ID == parentID {
			err := issue.setParent(parentID)
			if err != nil {
				return err
			}
			e.Issues = append(e.Issues, issue)
			return nil
		}
	}
	return fmt.Errorf("parent issue not found")
}

// mutate applies change to a scratch copy of the aggregate's issues and pull
// requests and commits it only on full success, so a change that fails partway
// through a traversal cannot leave the tree half-mutated.
func (e *Epic) mutate(change func(updated *Epic) error) error {
	updated := *e
	updated.Issues = append([]Issue(nil), e.Issues...)
	updated.PullRequests = append([]PullRequest(nil), e.PullRequests...)
	if err := change(&updated); err != nil {
		return err
	}
	e.Issues = updated.Issues
	e.PullRequests = updated.PullRequests
	return nil
}

func (e *Epic) CloseIssue(issueID string) error {
	// Keep issue closure and its open pull requests in the same aggregate update.
	return e.mutate(func(updated *Epic) error {
		return updated.traverseIssuesDown(issueID, func(issue Issue) error {
			for i := range updated.Issues {
				if updated.Issues[i].ID != issue.ID {
					continue
				}
				// A merged issue is left alone. Its work landed, and abandoning the
				// tree around it must not pretend otherwise — without this, closing
				// any issue with a delivered child fails outright.
				if updated.Issues[i].State == IssueStateMerged {
					return nil
				}
				if err := updated.Issues[i].TransitionTo(IssueStateClosed); err != nil {
					return err
				}
				for j := range updated.PullRequests {
					if updated.PullRequests[j].IssueID == issue.ID &&
						updated.PullRequests[j].Status == PullRequestOpen {
						if err := updated.PullRequests[j].TransitionTo(PullRequestClosed); err != nil {
							return err
						}
					}
				}
				return nil
			}
			return fmt.Errorf("issue not found")
		})
	})
}

// traverseIssuesDown traverses over all issues, starting from the issue which matches issueID
func (e *Epic) traverseIssuesDown(issueID string, visit func(issue Issue) error) error {
	var traverse func(issueID string) error
	traverse = func(issueID string) error {
		for _, issue := range e.Issues {
			if issue.ID == issueID {
				if err := visit(issue); err != nil {
					return err
				}
				for _, child := range e.Issues {
					if child.ParentID == issueID {
						if err := traverse(child.ID); err != nil {
							return err
						}
					}
				}
				return nil
			}
		}
		return fmt.Errorf("issue not found")
	}

	return traverse(issueID)
}

// FindIssue finds an issue by its ID and returns it, or an error if not found
func (e *Epic) FindIssue(issueID string) (Issue, error) {
	for _, issue := range e.Issues {
		if issue.ID == issueID {
			return issue, nil
		}
	}

	return Issue{}, ErrIssueNotFound
}

func (e *Epic) AddPullRequest(issueID string, pullRequest PullRequest) error {
	if err := pullRequest.Validate(); err != nil {
		return err
	}

	for i := range e.Issues {
		if e.Issues[i].ID != issueID {
			continue
		}
		// Cutting a branch means the issue is waiting on a coding round, not
		// that anything has been written or judged yet. Only an approving
		// review moves it on to IssueStatePR.
		if err := e.Issues[i].TransitionTo(IssueStateCoding); err != nil {
			return err
		}
		pullRequest.IssueID = issueID
		e.PullRequests = append(e.PullRequests, pullRequest)
		return nil
	}
	return fmt.Errorf("issue not found")
}

// TransitionIssue moves one issue to next. It is how the round use cases keep
// an issue's phase in step with the counters they write on its pull request,
// inside the same aggregate update.
func (e *Epic) TransitionIssue(issueID string, next IssueState) error {
	for i := range e.Issues {
		if e.Issues[i].ID != issueID {
			continue
		}
		return e.Issues[i].TransitionTo(next)
	}
	return fmt.Errorf("issue not found")
}

func (e *Epic) TransitionPullRequest(pullRequestID string, status PullRequestStatus) error {
	// Only the two outcomes translate into an issue transition. Anything else
	// — including a no-op back to the current status — must fail loudly here:
	// TransitionTo treats same-status as success, and that silence would fall
	// through to marking the issue Merged for a merge that never happened.
	var event PullRequestEvent
	switch status {
	case PullRequestMerged:
		event = PullRequestEventMerge
	case PullRequestClosed:
		event = PullRequestEventClose
	default:
		return fmt.Errorf("cannot transition pull request to %q", status)
	}
	return e.mutate(func(updated *Epic) error {
		for i := range updated.PullRequests {
			if updated.PullRequests[i].ID != pullRequestID {
				continue
			}
			if err := updated.PullRequests[i].Apply(event); err != nil {
				return err
			}
			// Merging is what delivers an issue. Closing a pull request without
			// merging rewinds its issue to Open instead, because the work still
			// has to happen and only an Open issue gets a fresh branch cut.
			next := IssueStateMerged
			if status == PullRequestClosed {
				next = IssueStateOpen
			}
			for j := range updated.Issues {
				if updated.Issues[j].ID != updated.PullRequests[i].IssueID {
					continue
				}
				return updated.Issues[j].TransitionTo(next)
			}
			return fmt.Errorf("issue not found")
		}
		return fmt.Errorf("pull request not found")
	})
}

func (e *Epic) AddIssueComment(issueID string, comment Comment) error {
	for i := range e.Issues {
		if e.Issues[i].ID == issueID {
			return e.Issues[i].AddComment(comment)
		}
	}
	return fmt.Errorf("issue not found")
}

func (e *Epic) AddPullRequestComment(pullRequestID string, comment Comment) error {
	for i := range e.PullRequests {
		if e.PullRequests[i].ID == pullRequestID {
			return e.PullRequests[i].AddComment(comment)
		}
	}
	return fmt.Errorf("pull request not found")
}

func (e *Epic) RootIssue() (Issue, error) {
	for _, issue := range e.Issues {
		if issue.ParentID == "" {
			return issue, nil
		}
	}
	return Issue{}, fmt.Errorf("root issue not found")
}

func (e *Epic) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("aggregate id is required")
	}
	if strings.TrimSpace(e.Title) == "" {
		return fmt.Errorf("epic title is required")
	}
	if strings.TrimSpace(e.Assignee) == "" {
		return fmt.Errorf("epic assignee is required")
	}
	seenRepositories := make(map[string]struct{}, len(e.Repositories))
	for _, repository := range e.Repositories {
		repository = strings.TrimSpace(repository)
		if repository == "" {
			return fmt.Errorf("epic repository is required")
		}
		if _, exists := seenRepositories[repository]; exists {
			return fmt.Errorf("duplicate epic repository %s", repository)
		}
		seenRepositories[repository] = struct{}{}
	}
	// The prefix goes into a Git ref verbatim, so a hand-edited store must not
	// be able to smuggle anything the slug alphabet would have removed.
	if e.BranchPrefix != domain.Slug(e.BranchPrefix) {
		return fmt.Errorf("epic %s has invalid branch prefix %q", e.ID, e.BranchPrefix)
	}
	if !isEpicState(e.State) {
		return fmt.Errorf("epic %s has invalid state %q", e.ID, e.State)
	}
	// No upper bound: a failed epic restarted from Concept keeps its count, so
	// passes can legitimately exceed MaxDraftingPasses across attempts.
	if e.DraftingPasses < 0 {
		return fmt.Errorf("epic %s has negative drafting passes", e.ID)
	}
	if len(e.Issues) == 0 {
		return fmt.Errorf("aggregate requires a root issue")
	}
	issues := make(map[string]Issue, len(e.Issues))
	rootCount := 0
	for _, issue := range e.Issues {
		if err := issue.Validate(); err != nil {
			return err
		}
		for _, comment := range issue.Comments {
			if err := comment.Validate(); err != nil {
				return err
			}
		}
		if issue.ParentID == "" {
			rootCount++
			if strings.TrimSpace(issue.Repository) != "" {
				return fmt.Errorf("root issue cannot have a repository")
			}
		} else if strings.TrimSpace(issue.Repository) == "" {
			return fmt.Errorf("non-root issue %s requires a repository", issue.ID)
		} else if _, scoped := seenRepositories[strings.TrimSpace(issue.Repository)]; !scoped &&
			// An epic without a declared scope leaves repository choice to its
			// issues; only a declared scope constrains them.
			len(e.Repositories) > 0 {
			return fmt.Errorf(
				"issue %s references repository %s outside the epic's scope",
				issue.ID, issue.Repository,
			)
		}
		if _, exists := issues[issue.ID]; exists {
			return fmt.Errorf("duplicate issue %s", issue.ID)
		}
		issues[issue.ID] = issue
	}
	if rootCount != 1 {
		return fmt.Errorf("aggregate must have one root issue")
	}
	for _, issue := range e.Issues {
		if issue.ParentID != "" {
			if _, ok := issues[issue.ParentID]; !ok {
				return fmt.Errorf("issue %s names missing parent %s", issue.ID, issue.ParentID)
			}
		}
		seen := map[string]bool{}
		for current := issue; current.ParentID != ""; current = issues[current.ParentID] {
			if seen[current.ID] {
				return fmt.Errorf("issue hierarchy contains a cycle at %s", current.ID)
			}
			seen[current.ID] = true
		}
	}
	if err := validateBlockedBy(e.Issues, issues); err != nil {
		return err
	}
	for _, pr := range e.PullRequests {
		if err := pr.Validate(); err != nil {
			return err
		}
		if _, ok := issues[pr.IssueID]; !ok {
			return fmt.Errorf("pull request %s names missing issue %s", pr.ID, pr.IssueID)
		}
	}
	return nil
}

func (e *Epic) Sort() {
	sort.Slice(e.Issues, func(i, j int) bool {
		return e.Issues[i].CreatedAt.Before(e.Issues[j].CreatedAt)
	})
	sort.Slice(e.PullRequests, func(i, j int) bool {
		return e.PullRequests[i].CreatedAt.Before(e.PullRequests[j].CreatedAt)
	})
}
