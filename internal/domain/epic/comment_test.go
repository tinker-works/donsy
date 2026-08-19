package epic

import "testing"

func TestCommentSetBody(t *testing.T) {
	// Arrange
	comment := Comment{}

	// Act
	if err := comment.SetBody("Body"); err != nil {
		t.Fatal(err)
	}

	// Assert
	if comment.Body != "Body" {
		t.Fatalf("unexpected comment body: %q", comment.Body)
	}
}

func TestCreateCommentTrimsAuthor(t *testing.T) {
	// Arrange

	// Act
	comment, err := CreateComment(" author ", "Body")
	if err != nil {
		t.Fatal(err)
	}
	// Assert
	if comment.ID == "" || comment.Author != "author" ||
		comment.Body != "Body" || comment.CreatedAt.IsZero() {
		t.Fatalf("unexpected comment: %#v", comment)
	}
}
