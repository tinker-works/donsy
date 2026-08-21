package repositorypath

import "testing"

func TestRepositoryPath_ShouldRoundTripRepositoryNames(t *testing.T) {
	// Arrange
	tests := []string{
		"acme/widgets",
		"acme/foo__bar",
		"acme__foo/bar",
	}

	for _, repository := range tests {
		t.Run(repository, func(t *testing.T) {
			// Act
			encoded := Encode(repository)
			decoded, err := Decode(encoded)

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			if decoded != repository {
				t.Fatalf("Decode(Encode(%q)) = %q", repository, decoded)
			}
		})
	}
}

func TestRepositoryPath_ShouldKeepTheReadableEncodingForSimpleNames(t *testing.T) {
	// Arrange, Act, Assert
	if got := Encode("acme/widgets"); got != "acme__widgets" {
		t.Fatalf("Encode(acme/widgets) = %q", got)
	}
}

func TestRepositoryPath_ShouldRejectAnInvalidEncoding(t *testing.T) {
	// Arrange, Act, Assert
	for _, encoded := range []string{"acme_widgets", "acme_", "acme_x"} {
		if _, err := Decode(encoded); err == nil {
			t.Fatalf("expected %q to be rejected", encoded)
		}
	}
}
