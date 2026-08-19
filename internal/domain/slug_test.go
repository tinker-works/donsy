package domain

import "testing"

func TestSlug_ShouldReduceTextToSlugAlphabet(t *testing.T) {
	// Arrange
	cases := map[string]string{
		"Fix that issue":           "fix-that-issue",
		"JIRA-123":                 "jira-123",
		"  leading and trailing  ": "leading-and-trailing",
		"lots---of___separators":   "lots-of-separators",
		"Refactor: the /parser/!":  "refactor-the-parser",
		"already-a-slug":           "already-a-slug",
		"!!!":                      "",
		"":                         "",
		"日本語":                      "",
		"v2.1 release":             "v2-1-release",
	}

	for input, want := range cases {
		// Act
		got := Slug(input)

		// Assert
		if got != want {
			t.Errorf("Slug(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSlug_ShouldBeIdempotent(t *testing.T) {
	// Arrange: callers store the slugged value and re-validate it later, so
	// slugging a slug must be a no-op or that check would reject its own output.
	inputs := []string{"Fix that issue", "JIRA-123", "!!!", "trailing---"}

	for _, input := range inputs {
		// Act
		once := Slug(input)
		twice := Slug(once)

		// Assert
		if once != twice {
			t.Errorf("Slug(%q) is not idempotent: %q then %q", input, once, twice)
		}
	}
}

func TestSlugMax_ShouldCutOnWordBoundary(t *testing.T) {
	// Arrange
	cases := []struct {
		text string
		max  int
		want string
	}{
		{"one two three four", 11, "one-two"},
		{"one two three four", 7, "one-two"},
		{"one two three four", 8, "one-two"},
		{"short", 40, "short"},
		{"exactfit", 8, "exactfit"},
		{"supercalifragilistic", 5, "super"},
		{"one two", 0, "one-two"},
	}

	for _, c := range cases {
		// Act
		got := SlugMax(c.text, c.max)

		// Assert
		if got != c.want {
			t.Errorf("SlugMax(%q, %d) = %q, want %q", c.text, c.max, got, c.want)
		}
		if c.max > 0 && len(got) > c.max {
			t.Errorf("SlugMax(%q, %d) = %q exceeds the cap", c.text, c.max, got)
		}
	}
}
