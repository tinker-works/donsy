package domain

import (
	"testing"

	"github.com/oklog/ulid/v2"
)

func TestMintULID_ShouldReturnDistinctParseableValues(t *testing.T) {
	// Arrange: callers persist these as identifiers, so they must be valid ULIDs
	// and must not collide across successive mints.

	// Act
	first := MintULID()
	second := MintULID()

	// Assert
	if _, err := ulid.Parse(first); err != nil {
		t.Fatalf("first value is not a ULID: %q: %v", first, err)
	}
	if _, err := ulid.Parse(second); err != nil {
		t.Fatalf("second value is not a ULID: %q: %v", second, err)
	}
	if first == second {
		t.Fatalf("repeated calls returned the same value %q", first)
	}
}
