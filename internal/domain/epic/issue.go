package epic

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tinker-works/donsy/internal/domain/id"
)

// Issue is a tracked unit of work belonging to an epic.
type Issue struct {
	ID           id.ID         `json:"id"`
	Number       int           `json:"number,omitempty"`
	Title        string        `json:"title"`
	Body         string        `json:"body,omitempty"`
	Status       Status        `json:"status"`
	EpicID       id.ID         `json:"epic_id"`
	PullRequests []PullRequest `json:"pull_requests,omitempty"`
	Comments     []Comment     `json:"comments,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	ClosedAt     time.Time     `json:"closed_at,omitempty"`
}

func NewIssue(title string, details ...string) (Issue, error) {
	value := Issue{ID: id.New(), Title: strings.TrimSpace(title), Status: StatusOpen, CreatedAt: time.Now()}
	if len(details) > 0 {
		value.Body = details[0]
	}
	if err := value.Validate(); err != nil {
		return Issue{}, err
	}
	return value, nil
}

func (value Issue) Validate() error {
	if strings.TrimSpace(value.Title) == "" {
		return errors.New("issue title cannot be empty")
	}
	if err := validateStatus(value.Status); err != nil {
		return fmt.Errorf("issue: %w", err)
	}
	if value.Number < 0 {
		return errors.New("issue number cannot be negative")
	}
	for index, request := range value.PullRequests {
		if err := request.Validate(); err != nil {
			return fmt.Errorf("pull request %d: %w", index, err)
		}
	}
	for index, comment := range value.Comments {
		if err := comment.Validate(); err != nil {
			return fmt.Errorf("comment %d: %w", index, err)
		}
	}
	return nil
}

func (value Issue) Valid() bool { return value.Validate() == nil }

func (value *Issue) Transition(to Status) error {
	if value == nil {
		return errors.New("issue is nil")
	}
	if err := transition(value.Status, to, false); err != nil {
		return err
	}
	value.Status = to
	if to == StatusClosed || to == StatusDone {
		value.ClosedAt = time.Now()
	}
	return nil
}

func (value *Issue) Close() error {
	if value == nil {
		return errors.New("issue is nil")
	}
	return value.Transition(StatusClosed)
}

func (value *Issue) AddPullRequest(request PullRequest) error {
	if value == nil {
		return errors.New("issue is nil")
	}
	if err := request.Validate(); err != nil {
		return err
	}
	for _, existing := range value.PullRequests {
		if existing.ID != "" && existing.ID == request.ID || existing.Number != 0 && existing.Number == request.Number {
			return fmt.Errorf("pull request %d already belongs to issue", request.Number)
		}
	}
	value.PullRequests = append(value.PullRequests, request)
	return nil
}

func (value *Issue) AddComment(comment Comment) error {
	if value == nil {
		return errors.New("issue is nil")
	}
	if err := comment.Validate(); err != nil {
		return err
	}
	value.Comments = append(value.Comments, comment)
	return nil
}
