// Package slug contains the validated, URL-safe names used by the domain.
package slug

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Slug is a lower-case, URL-safe domain name.
type Slug string

// Parse validates a persisted slug.
func Parse(value string) (Slug, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("slug cannot be empty")
	}
	if len(value) > 100 {
		return "", errors.New("slug is too long")
	}
	if value[0] == '-' || value[len(value)-1] == '-' {
		return "", errors.New("slug cannot start or end with a hyphen")
	}
	previousHyphen := false
	for _, character := range value {
		switch {
		case character == '-':
			if previousHyphen {
				return "", errors.New("slug cannot contain consecutive hyphens")
			}
			previousHyphen = true
		case character > unicode.MaxASCII || (!unicode.IsLower(character) && !unicode.IsDigit(character)):
			return "", errors.New("slug must contain only lower-case letters, digits, and hyphens")
		default:
			previousHyphen = false
		}
	}
	return Slug(value), nil
}

// New is the constructor for a slug. Slugs are not silently normalized;
// callers should make normalization a deliberate presentation concern.
func New(value string) (Slug, error) { return Parse(value) }

// MustParse parses value and panics when it is invalid.
func MustParse(value string) Slug {
	parsed, err := Parse(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func (value Slug) String() string { return string(value) }

func (value Slug) IsZero() bool { return value == "" }

func (value Slug) Valid() bool {
	_, err := Parse(value.String())
	return err == nil
}

func (value Slug) MarshalJSON() ([]byte, error) {
	if !value.Valid() {
		return nil, fmt.Errorf("cannot marshal invalid slug %q", value)
	}
	return json.Marshal(value.String())
}

func (value *Slug) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode slug: %w", err)
	}
	parsed, err := Parse(raw)
	if err != nil {
		return err
	}
	*value = parsed
	return nil
}
