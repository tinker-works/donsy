package epic

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tinker-works/donsy/internal/domain/id"
)

// Epic is the aggregate root for a collection of issues.
type Epic struct {
	ID          id.ID     `json:"id"`
	Prefix      string    `json:"prefix"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Status      Status    `json:"status"`
	Issues      []Issue   `json:"issues,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	ClosedAt    time.Time `json:"closed_at,omitempty"`
}

func New(title string, details ...string) (Epic, error) {
	value := Epic{ID: id.New(), Title: strings.TrimSpace(title), Status: StatusDraft, CreatedAt: time.Now()}
	if len(details) > 0 {
		value.Description = details[0]
	}
	if len(details) > 1 {
		value.Prefix = strings.TrimSpace(details[1])
	}
	if err := value.Validate(); err != nil {
		return Epic{}, err
	}
	return value, nil
}

func NewEpic(title string, details ...string) (Epic, error) { return New(title, details...) }

func (value Epic) Validate() error {
	if strings.TrimSpace(value.Title) == "" {
		return errors.New("epic title cannot be empty")
	}
	if value.Prefix != "" {
		if strings.ContainsAny(value.Prefix, " \t\r\n") {
			return errors.New("epic prefix cannot contain whitespace")
		}
	}
	if err := validateAggregateStatus(value.Status, validEpicStatus, "epic"); err != nil {
		return fmt.Errorf("epic: %w", err)
	}
	for index, issue := range value.Issues {
		if err := issue.Validate(); err != nil {
			return fmt.Errorf("issue %d: %w", index, err)
		}
	}
	return nil
}

func (value Epic) Valid() bool { return value.Validate() == nil }

func (value *Epic) SetPrefix(prefix string) error {
	if value == nil {
		return errors.New("epic is nil")
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || strings.ContainsAny(prefix, " \t\r\n") {
		return errors.New("epic prefix is invalid")
	}
	value.Prefix = prefix
	return nil
}

func (value *Epic) Transition(to Status) error {
	if value == nil {
		return errors.New("epic is nil")
	}
	if err := transition(value.Status, to, validEpicStatus, "epic", false); err != nil {
		return err
	}
	transitioned := *value
	transitioned.Status = to
	if to == StatusClosed || to == StatusDone {
		transitioned.ClosedAt = time.Now()
	}
	if err := transitioned.Validate(); err != nil {
		return err
	}
	*value = transitioned
	return nil
}

func (value *Epic) Close() error {
	if value == nil {
		return errors.New("epic is nil")
	}
	return value.Transition(StatusClosed)
}

func (value *Epic) AddIssue(issue Issue) error {
	if value == nil {
		return errors.New("epic is nil")
	}
	if err := issue.Validate(); err != nil {
		return err
	}
	if value.Status == StatusClosed || value.Status == StatusDone {
		return errors.New("cannot add an issue to a closed epic")
	}
	for _, existing := range value.Issues {
		if existing.ID != "" && existing.ID == issue.ID || existing.Number != 0 && existing.Number == issue.Number {
			return fmt.Errorf("issue %d already belongs to epic", issue.Number)
		}
	}
	issue.EpicID = value.ID
	value.Issues = append(value.Issues, issue)
	return nil
}

func (value *Epic) RemoveIssue(issueID id.ID) bool {
	if value == nil {
		return false
	}
	for index, issue := range value.Issues {
		if issue.ID == issueID {
			value.Issues = append(value.Issues[:index], value.Issues[index+1:]...)
			return true
		}
	}
	return false
}
