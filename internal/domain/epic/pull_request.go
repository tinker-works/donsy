package epic

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tinker-works/donsy/internal/domain/id"
	"github.com/tinker-works/donsy/internal/domain/owner"
)

// PullRequest is a reviewable change associated with an issue.
type PullRequest struct {
	ID         id.ID         `json:"id"`
	Number     int           `json:"number,omitempty"`
	Title      string        `json:"title"`
	Body       string        `json:"body,omitempty"`
	URL        string        `json:"url,omitempty"`
	HeadBranch string        `json:"head_branch,omitempty"`
	BaseBranch string        `json:"base_branch,omitempty"`
	Status     Status        `json:"status"`
	Author     string        `json:"author,omitempty"`
	Comments   []Comment     `json:"comments,omitempty"`
	Reviewers  []owner.Owner `json:"reviewers,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	MergedAt   time.Time     `json:"merged_at,omitempty"`
	ClosedAt   time.Time     `json:"closed_at,omitempty"`
	Diff       string        `json:"diff,omitempty"`
}

func NewPullRequest(title string, details ...string) (PullRequest, error) {
	value := PullRequest{ID: id.New(), Title: strings.TrimSpace(title), Status: StatusOpen, CreatedAt: time.Now()}
	if len(details) > 0 {
		value.Body = details[0]
	}
	if len(details) > 1 {
		value.URL = details[1]
	}
	if len(details) > 2 {
		value.HeadBranch = details[2]
	}
	if len(details) > 3 {
		value.BaseBranch = details[3]
	}
	if err := value.Validate(); err != nil {
		return PullRequest{}, err
	}
	return value, nil
}

// NewPR is a short alias used by callers that use the GitHub abbreviation.
func NewPR(title string, details ...string) (PullRequest, error) {
	return NewPullRequest(title, details...)
}

func (value PullRequest) Validate() error {
	if strings.TrimSpace(value.Title) == "" {
		return errors.New("pull request title cannot be empty")
	}
	if err := validateAggregateStatus(value.Status, validPullRequestStatus, "pull request"); err != nil {
		return fmt.Errorf("pull request: %w", err)
	}
	if value.Number < 0 {
		return errors.New("pull request number cannot be negative")
	}
	for index, comment := range value.Comments {
		if err := comment.Validate(); err != nil {
			return fmt.Errorf("comment %d: %w", index, err)
		}
	}
	for index, reviewer := range value.Reviewers {
		if err := reviewer.Validate(); err != nil {
			return fmt.Errorf("reviewer %d: %w", index, err)
		}
	}
	return nil
}

func (value PullRequest) Valid() bool { return value.Validate() == nil }

func (value *PullRequest) Transition(to Status) error {
	if value == nil {
		return errors.New("pull request is nil")
	}
	if err := transition(value.Status, to, validPullRequestStatus, "pull request", value.Status == StatusMerged); err != nil {
		return err
	}
	transitioned := *value
	transitioned.Status = to
	if to == StatusMerged {
		transitioned.MergedAt = time.Now()
	}
	if to == StatusClosed {
		transitioned.ClosedAt = time.Now()
	}
	if err := transitioned.Validate(); err != nil {
		return err
	}
	*value = transitioned
	return nil
}

func (value *PullRequest) Merge() error { return value.Transition(StatusMerged) }

func (value *PullRequest) Close() error { return value.Transition(StatusClosed) }

// Reset returns an open request to its reviewable state. A merged request is
// intentionally terminal and cannot be reset.
func (value *PullRequest) Reset() error {
	if value == nil {
		return errors.New("pull request is nil")
	}
	if value.Status == StatusMerged {
		return errors.New("merged pull request cannot be reset")
	}
	reset := *value
	if reset.Status == StatusClosed {
		reset.ClosedAt = time.Time{}
	}
	reset.Status = StatusOpen
	if err := reset.Validate(); err != nil {
		return err
	}
	*value = reset
	return nil
}

func (value *PullRequest) AddComment(comment Comment) error {
	if value == nil {
		return errors.New("pull request is nil")
	}
	if err := comment.Validate(); err != nil {
		return err
	}
	updated := *value
	updated.Comments = append(append([]Comment(nil), value.Comments...), comment)
	if err := updated.Validate(); err != nil {
		return err
	}
	*value = updated
	return nil
}

// Grant records a reviewer approval. Repeated grants by the same reviewer
// are idempotent.
func (value *PullRequest) Grant(reviewer owner.Owner) error {
	if value == nil {
		return errors.New("pull request is nil")
	}
	if err := reviewer.Validate(); err != nil {
		return err
	}
	granted := *value
	granted.Reviewers = append([]owner.Owner(nil), value.Reviewers...)
	for _, existing := range granted.Reviewers {
		if existing.ID != "" && existing.ID == reviewer.ID || existing.Login == reviewer.Login {
			return nil
		}
	}
	granted.Reviewers = append(granted.Reviewers, reviewer)
	if granted.Status == StatusOpen {
		granted.Status = StatusApproved
	}
	if err := granted.Validate(); err != nil {
		return err
	}
	*value = granted
	return nil
}
