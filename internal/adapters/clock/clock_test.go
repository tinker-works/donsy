package clock

import (
	"testing"
	"time"
)

func TestReal_Now_ShouldReturnUTC(t *testing.T) {
	// Everything persisted compares timestamps across machines, so the clock
	// adapter is where the timezone is pinned.
	// Act
	first := Real{}.Now()
	second := Real{}.Now()

	// Assert
	if first.Location() != time.UTC {
		t.Fatalf("expected UTC, got %v", first.Location())
	}
	if second.Before(first) {
		t.Fatalf("clock went backwards: %v then %v", first, second)
	}
}
