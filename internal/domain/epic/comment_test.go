package epic

import (
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain/id"
	"github.com/tinker-works/donsy/internal/domain/owner"
)

func TestCommentEdit(t *testing.T) {
	created := time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	comment, err := NewComment("alice", "first", created)
	if err != nil {
		t.Fatalf("NewComment() error = %v", err)
	}
	if err := comment.Edit("updated", created.Add(time.Minute)); err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	if comment.Body != "updated" {
		t.Fatalf("Edit() body = %q", comment.Body)
	}
}

func TestCommentEditRejectsUpdateBeforeCreation(t *testing.T) {
	created := time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	comment, err := NewComment("alice", "first", created)
	if err != nil {
		t.Fatalf("NewComment() error = %v", err)
	}
	want := comment

	if err := comment.Edit("updated", created.Add(-time.Minute)); err == nil {
		t.Fatal("Edit() accepted an update time before creation")
	}
	if comment != want {
		t.Fatalf("Edit() mutated the comment on error: got %#v, want %#v", comment, want)
	}
}

func TestCommentValidateRejectsInvalidOwner(t *testing.T) {
	comment := Comment{
		Author:    "",
		Owner:     owner.Owner{ID: id.New()},
		Body:      "review",
		CreatedAt: time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC),
	}

	if err := comment.Validate(); err == nil {
		t.Fatal("Validate() accepted an owner without a login")
	}
}
