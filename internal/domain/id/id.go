// Package id contains the identifier value used by the domain aggregates.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ID is the stable identifier of a persisted domain object.
//
// IDs are deliberately represented as strings. The storage adapters persist
// them as text and the domain must not depend on a particular database.
type ID string

// New returns a new random identifier. A caller that is restoring an existing
// identifier should use Parse instead.
func New() ID {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("generate domain id: %v", err))
	}
	return ID(hex.EncodeToString(raw[:]))
}

// Parse validates a persisted identifier.
func Parse(value string) (ID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("id cannot be empty")
	}
	if len(value) > 255 {
		return "", errors.New("id is too long")
	}
	for _, character := range value {
		if character < 0x21 || character == '/' || character == '\\' {
			return "", errors.New("id contains invalid characters")
		}
	}
	return ID(value), nil
}

// FromString is an alias for Parse for callers converting persisted fields.
func FromString(value string) (ID, error) { return Parse(value) }

// MustParse parses value and panics when it is not a valid identifier. It is
// intended for package-level constants and test fixtures.
func MustParse(value string) ID {
	parsed, err := Parse(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

// String returns the persisted representation.
func (value ID) String() string { return string(value) }

// IsZero reports whether no identifier has been assigned.
func (value ID) IsZero() bool { return value == "" }

// Valid reports whether value is a valid identifier.
func (value ID) Valid() bool {
	_, err := Parse(value.String())
	return err == nil
}

func (value ID) MarshalJSON() ([]byte, error) {
	if value.IsZero() {
		return []byte(`""`), nil
	}
	if !value.Valid() {
		return nil, errors.New("cannot marshal invalid id")
	}
	return json.Marshal(value.String())
}

func (value *ID) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode id: %w", err)
	}
	if raw == "" {
		*value = ""
		return nil
	}
	parsed, err := Parse(raw)
	if err != nil {
		return err
	}
	*value = parsed
	return nil
}
