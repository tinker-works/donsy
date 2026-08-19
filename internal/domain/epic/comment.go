package epic

import (
	"errors"
	"strings"
	"time"

	"github.com/tinker-works/donsy/internal/domain/id"
	"github.com/tinker-works/donsy/internal/domain/owner"
)

// Comment is an immutable authored comment until Edit is called by the
// aggregate owner.
type Comment struct {
	ID        id.ID       `json:"id"`
	Author    string      `json:"author"`
	Owner     owner.Owner `json:"owner,omitempty"`
	Body      string      `json:"body"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at,omitempty"`
}

func NewComment(author, body string, at ...time.Time) (Comment, error) {
	value := Comment{ID: id.New(), Author: strings.TrimSpace(author), Body: strings.TrimSpace(body)}
	if len(at) > 0 {
		value.CreatedAt = at[0]
	} else {
		value.CreatedAt = time.Now()
	}
	if err := value.Validate(); err != nil {
		return Comment{}, err
	}
	return value, nil
}

func (value Comment) Validate() error {
	if strings.TrimSpace(value.Author) == "" && value.Owner.Login == "" && value.Owner.ID.IsZero() {
		return errors.New("comment author cannot be empty")
	}
	if strings.TrimSpace(value.Body) == "" {
		return errors.New("comment body cannot be empty")
	}
	if value.UpdatedAt.Before(value.CreatedAt) && !value.UpdatedAt.IsZero() {
		return errors.New("comment updated before it was created")
	}
	return nil
}

func (value Comment) Valid() bool { return value.Validate() == nil }

func (value *Comment) Edit(body string, at ...time.Time) error {
	if value == nil {
		return errors.New("comment is nil")
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("comment body cannot be empty")
	}
	value.Body = body
	if len(at) > 0 {
		value.UpdatedAt = at[0]
	} else {
		value.UpdatedAt = time.Now()
	}
	return value.Validate()
}
