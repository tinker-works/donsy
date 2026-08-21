package agentlog

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLog(t *testing.T, dir, runID, body string) {
	t.Helper()
	path := filepath.Join(dir, runID+Extension)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReader_Tail_ShouldReadWholeLinesFromAnOffset(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeLog(t, dir, "run-1", "first\nsecond\n")
	reader := NewReader(dir)

	// Act
	lines, next, err := reader.Tail("run-1", 0)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "first" || lines[1] != "second" {
		t.Fatalf("unexpected lines: %q", lines)
	}
	if next != int64(len("first\nsecond\n")) {
		t.Fatalf("unexpected offset: %d", next)
	}

	// Act: a second poll with nothing new.
	lines, again, err := reader.Tail("run-1", next)

	// Assert
	if err != nil || len(lines) != 0 || again != next {
		t.Fatalf("expected an empty poll at the same offset, got %q %d (%v)", lines, again, err)
	}
}

func TestReader_Tail_ShouldHoldBackAPartialLine(t *testing.T) {
	// Arrange: the round is still writing, and half a JSON object decodes to
	// nothing useful — so it must not be consumed and skipped.
	dir := t.TempDir()
	writeLog(t, dir, "run-1", "complete\n{\"type\":\"te")
	reader := NewReader(dir)

	// Act
	lines, next, err := reader.Tail("run-1", 0)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "complete" {
		t.Fatalf("unexpected lines: %q", lines)
	}
	if next != int64(len("complete\n")) {
		t.Fatalf("expected to resume at the partial line, got %d", next)
	}

	// Act: the round finishes the line.
	writeLog(t, dir, "run-1", "complete\n{\"type\":\"text\"}\n")
	lines, _, err = reader.Tail("run-1", next)

	// Assert
	if err != nil || len(lines) != 1 || lines[0] != `{"type":"text"}` {
		t.Fatalf("expected the completed line, got %q (%v)", lines, err)
	}
}

func TestReader_Tail_ShouldTreatAMissingLogAsEmpty(t *testing.T) {
	// Arrange: the file appears when the round starts, not when the run is
	// recorded, so a read in between is ordinary.
	reader := NewReader(t.TempDir())

	// Act
	lines, next, err := reader.Tail("run-1", 0)

	// Assert
	if err != nil || len(lines) != 0 || next != 0 {
		t.Fatalf("expected an empty read, got %q %d (%v)", lines, next, err)
	}
}

func TestReader_Tail_ShouldRestartWhenTheLogShrank(t *testing.T) {
	// Arrange: a re-run truncates the file, so an old offset points past the
	// end. Seeking there would read nothing forever.
	dir := t.TempDir()
	writeLog(t, dir, "run-1", "short\n")
	reader := NewReader(dir)

	// Act
	lines, next, err := reader.Tail("run-1", 5000)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "short" || next != int64(len("short\n")) {
		t.Fatalf("expected a restart from the beginning, got %q %d", lines, next)
	}
}

func TestReader_Tail_ShouldRejectAnEmptyRunID(t *testing.T) {
	// Arrange
	reader := NewReader(t.TempDir())

	// Act
	_, _, err := reader.Tail("  ", 0)

	// Assert
	if err == nil {
		t.Fatal("expected an empty run ID to be rejected")
	}
}

func TestReader_Tail_ShouldRejectARunIDThatIsNotABareIdentifier(t *testing.T) {
	// Arrange: the run ID reaches this from a caller and becomes a filename.
	// Rejecting beats sanitising — stripping separators would make
	// "../run-1" and "run-1" name the same file.
	dir := t.TempDir()
	writeLog(t, dir, "run-1", "inside\n")
	reader := NewReader(dir)

	for _, runID := range []string{"../run-1", "run 1", "run/1", "run.1"} {
		t.Run(runID, func(t *testing.T) {
			// Act
			lines, _, err := reader.Tail(runID, 0)

			// Assert
			if err == nil {
				t.Fatalf("expected %q to be rejected, read %q", runID, lines)
			}
		})
	}
}

func TestReader_Size_ShouldReportTheTranscriptsLength(t *testing.T) {
	// Arrange: sampling growth is what drives the activity sparklines, so a stat
	// answers rather than a read.
	dir := t.TempDir()
	writeLog(t, dir, "run-1", "first\nsecond\n")
	reader := NewReader(dir)

	// Act
	size, err := reader.Size("run-1")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len("first\nsecond\n")) {
		t.Fatalf("expected the byte length, got %d", size)
	}
}

func TestReader_Size_ShouldTreatAMissingLogAsZeroBytes(t *testing.T) {
	// Arrange: the file exists because a round wrote it, not because a record
	// says it should — the same reason Tail reads a missing one as empty.
	reader := NewReader(t.TempDir())

	// Act
	size, err := reader.Size("run-1")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if size != 0 {
		t.Fatalf("expected zero bytes, got %d", size)
	}
}

func TestReader_Size_ShouldRejectAnIDThatIsNotBare(t *testing.T) {
	// Arrange: sanitising would let two different IDs name one file.
	reader := NewReader(t.TempDir())

	// Act & Assert
	if _, err := reader.Size(""); err == nil {
		t.Fatal("expected an empty run ID to be refused")
	}
	if _, err := reader.Size("../escape"); err == nil {
		t.Fatal("expected a path-bearing run ID to be refused")
	}
}
