package domain

import "strings"

// Slug reduces text to the lowercase [a-z0-9-] alphabet, collapsing every run of
// other characters into a single hyphen. That alphabet is a legal Git ref and a
// legal path segment by construction, so callers building either from free text
// need no further validation. Non-ASCII letters are not transliterated; they
// count as separators like any other unmappable character.
func Slug(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	separated := false
	for _, r := range text {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if separated && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			separated = false
			builder.WriteRune(r)
		default:
			// Deferring the hyphen until the next kept character means a trailing
			// run never reaches the result, so no trim pass is needed.
			separated = true
		}
	}
	return builder.String()
}

// SlugMax is Slug capped at max characters, cut back to the last hyphen so a
// truncated slug never ends mid-word. A slug whose first word already exceeds
// max is cut hard, since there is no boundary to fall back to.
func SlugMax(text string, max int) string {
	slug := Slug(text)
	// Slug's alphabet is ASCII, so byte length is character length here.
	if max <= 0 || len(slug) <= max {
		return slug
	}
	cut := slug[:max]
	// A cut landing on the separator itself already ends on a whole word, so
	// falling back to the previous hyphen here would drop a word for nothing.
	if slug[max] == '-' {
		return cut
	}
	if boundary := strings.LastIndexByte(cut, '-'); boundary > 0 {
		return cut[:boundary]
	}
	return cut
}
