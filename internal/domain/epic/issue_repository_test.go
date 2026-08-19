package epic

import "testing"

func TestEpic_Validate_ShouldRequireRepositoryForChildrenOnly(t *testing.T) {
	// Arrange
	epic, err := CreateEpic("Epic", "owner", "Description")
	if err != nil {
		t.Fatal(err)
	}

	// Act
	child, err := CreateRepositoryIssue("Child", "Description", "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	err = epic.AddIssue(epic.Issues[0].ID, child)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if err := epic.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestEpic_Validate_ShouldRejectRepositoryOnRoot(t *testing.T) {
	// Arrange
	epic, err := CreateEpic("Epic", "owner", "Description")
	if err != nil {
		t.Fatal(err)
	}
	epic.Issues[0].Repository = "acme/widgets"

	// Act
	err = epic.Validate()

	// Assert
	if err == nil {
		t.Fatal("expected root repository to be rejected")
	}
}

func TestEpic_Validate_ShouldRejectIssueOutsideDeclaredScope(t *testing.T) {
	// Arrange
	epic, err := CreateEpic("Epic", "owner", "Description")
	if err != nil {
		t.Fatal(err)
	}
	epic.Repositories = []string{"acme/widgets"}
	child, err := CreateRepositoryIssue("Child", "Description", "other/repo")
	if err != nil {
		t.Fatal(err)
	}
	if err := epic.AddIssue(epic.Issues[0].ID, child); err != nil {
		t.Fatal(err)
	}

	// Act
	err = epic.Validate()

	// Assert
	if err == nil {
		t.Fatal("expected an issue outside the declared scope to be rejected")
	}
}

func TestEpic_AddIssue_ShouldRejectMissingRepository(t *testing.T) {
	// Arrange
	epic, err := CreateEpic("Epic", "owner", "Description")
	if err != nil {
		t.Fatal(err)
	}
	child, err := CreateIssue("Child", "Description")
	if err != nil {
		t.Fatal(err)
	}

	// Act
	err = epic.AddIssue(epic.Issues[0].ID, child)

	// Assert
	if err == nil {
		t.Fatal("expected child without repository to be rejected")
	}
}
