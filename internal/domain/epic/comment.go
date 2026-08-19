package epic

import (
	"fmt"
	"github.com/tinker-works/donsy/internal/domain"
	"strings"
	"time"
)

type Comment struct {
	ID        string
	Author    string
	CreatedAt time.Time
	Body      string
}

func CreateComment(author, body string) (Comment, error) {
	comment := Comment{
		ID:        domain.MintULID(),
		Author:    strings.TrimSpace(author),
		CreatedAt: time.Now().UTC(),
	}
	if err := comment.Validate(); err != nil {
		return Comment{}, err
	}
	if err := comment.SetBody(body); err != nil {
		return Comment{}, err
	}
	return comment, nil
}

func (c *Comment) SetBody(body string) error {
	c.Body = body
	return nil
}

func (c Comment) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("comment id is required")
	}
	if strings.TrimSpace(c.Author) == "" {
		return fmt.Errorf("comment author is required")
	}
	if c.CreatedAt.IsZero() {
		return fmt.Errorf("comment creation time is required")
	}
	return nil
}
