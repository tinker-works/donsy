package epic

import (
	"fmt"
	"github.com/tinker-works/donsy/internal/domain"
	"strings"
	"time"
)

type Issue struct {
	ID         string
	Title      string
	ParentID   string
	Repository string
	State      IssueState
	CreatedAt  time.Time
	Body       string
	Comments   []Comment
	// BlockedBy names issues this one waits on beyond its own children. Nesting
	// already orders a subtree; this is for a dependency it cannot express, such
	// as one on a sibling or on work in a different branch of the tree. It never
	// names an ancestor — an ancestor already waits on this issue.
	BlockedBy []string
}

// settled reports whether an issue reached a state nothing will move it out
// of, which is what clears it as a blocker.
func (i Issue) settled() bool {
	return i.State == IssueStateMerged || i.State == IssueStateClosed
}

func CreateIssue(title, body string) (Issue, error) {
	return createIssue(title, body, "")
}

// CreateRepositoryIssue creates a non-root issue owned by one repository.
func CreateRepositoryIssue(title, body, repository string) (Issue, error) {
	return createIssue(title, body, repository)
}

func createIssue(title, body, repository string) (Issue, error) {
	var issue Issue
	title = strings.TrimSpace(title)
	if title == "" {
		return Issue{}, fmt.Errorf("issue title is required")
	}

	issue = Issue{
		ID:         domain.MintULID(),
		Title:      title,
		Repository: strings.TrimSpace(repository),
		State:      IssueStateOpen,
		CreatedAt:  time.Now(),
		Body:       body,
	}
	return issue, issue.Validate()
}

func (i *Issue) setParent(parentID string) error {
	i.ParentID = parentID
	return nil
}

func (i *Issue) SetTitle(title string) error {
	if i.State == IssueStateClosed {
		return fmt.Errorf("cannot set title of closed issue")
	}
	i.Title = title
	return nil
}

func (i *Issue) SetBody(body string) error {
	if i.State == IssueStateClosed {
		return fmt.Errorf("cannot set body of closed issue")
	}
	i.Body = body
	return nil
}

func (i *Issue) AddComment(comment Comment) error {
	if err := comment.Validate(); err != nil {
		return err
	}
	i.Comments = append(i.Comments, comment)
	return nil
}

func (i Issue) Validate() error {
	if i.ID == "" || strings.TrimSpace(i.Title) == "" {
		return fmt.Errorf("issue id and title are required")
	}
	if !isIssueState(i.State) {
		return fmt.Errorf("issue %s has invalid state %q", i.ID, i.State)
	}
	return nil
}
