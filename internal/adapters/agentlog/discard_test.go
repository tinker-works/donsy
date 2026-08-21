package agentlog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReader_Discard_ShouldRemoveBothStreams(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	for _, name := range []string{"run-1.stdout.jsonl", "run-1.stderr.log", "run-2.stdout.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Act
	if err := NewReader(dir).Discard("run-1"); err != nil {
		t.Fatal(err)
	}

	// Assert
	for _, gone := range []string{"run-1.stdout.jsonl", "run-1.stderr.log"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Fatalf("expected %q to be removed", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "run-2.stdout.jsonl")); err != nil {
		t.Fatalf("expected another run's transcript to be untouched: %v", err)
	}
}

func TestReader_Discard_ShouldAcceptATranscriptThatIsAlreadyGone(t *testing.T) {
	// Reclaim is retried, so the second attempt must not report a failure for work
	// the first one finished.
	// Act
	err := NewReader(t.TempDir()).Discard("never-ran")

	// Assert
	if err != nil {
		t.Fatalf("expected a missing transcript to be fine, got %v", err)
	}
}

func TestReader_Discard_ShouldRejectAnIDThatIsNotBare(t *testing.T) {
	// Sanitising would let one ID reach another run's files.
	// Act
	err := NewReader(t.TempDir()).Discard("../../etc/passwd")

	// Assert
	if err == nil {
		t.Fatal("expected a path-like run ID to be rejected")
	}
}
