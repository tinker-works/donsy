// Package agentlog reads the transcripts agent rounds leave on the host. It
// is the read side of the files internal/adapters/colima writes: both name a
// run's log after its ID, so nothing has to store a path.
package agentlog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
)

// Extension is the suffix the runtime gives a run's stdout transcript.
const Extension = ".stdout.jsonl"

// stderrExtension is the other half of what a round writes. Only Discard needs it:
// reading a transcript means reading the structured stdout, but removing one has to
// take everything the run left behind.
const stderrExtension = ".stderr.log"

type Reader struct {
	dir string
}

func NewReader(dir string) Reader {
	return Reader{dir: dir}
}

// Tail returns the lines written after byte offset from, and the offset to
// resume at. A run whose log does not exist yet reads as empty rather than as
// an error: the file appears when the round starts, not when it is recorded.
func (r Reader) Tail(runID string, from int64) ([]string, int64, error) {
	if err := validRunID(runID); err != nil {
		return nil, from, err
	}
	file, err := os.Open(filepath.Join(r.dir, runID+Extension))
	if errors.Is(err, os.ErrNotExist) {
		return nil, from, nil
	}
	if err != nil {
		return nil, from, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, from, err
	}
	// A shorter file than the caller's offset means the log was replaced —
	// a re-run of the same ID. Start over rather than seeking past the end.
	if from > info.Size() {
		from = 0
	}
	if _, err := file.Seek(from, io.SeekStart); err != nil {
		return nil, from, err
	}
	body, err := io.ReadAll(file)
	if err != nil {
		return nil, from, err
	}
	text := string(body)
	// Hold back a trailing partial line: the round is still writing, and half
	// a JSON object decodes to nothing useful.
	cut := strings.LastIndexByte(text, '\n')
	if cut < 0 {
		return nil, from, nil
	}
	next := from + int64(cut) + 1
	lines := strings.Split(strings.TrimSuffix(text[:cut+1], "\n"), "\n")
	return lines, next, nil
}

// Size reports the transcript's current length in bytes. A log that does not
// exist yet is zero bytes long, for the same reason Tail reads it as empty.
func (r Reader) Size(runID string) (int64, error) {
	if err := validRunID(runID); err != nil {
		return 0, err
	}
	info, err := os.Stat(filepath.Join(r.dir, runID+Extension))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// Discard removes a run's transcript, both streams of it. The run record survives
// — that is the history the Runs screen lists — while the raw output, which is all
// the bulk, goes once nobody can act on the round any more.
//
// A transcript that is already gone is not an error, for the same reason Tail
// treats a missing one as empty: the file exists because a round wrote it, not
// because a record says it should.
func (r Reader) Discard(runID string) error {
	if err := validRunID(runID); err != nil {
		return err
	}
	var errs []error
	for _, suffix := range []string{Extension, stderrExtension} {
		if err := os.Remove(filepath.Join(r.dir, runID+suffix)); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// validRunID rejects anything that is not a bare identifier rather than
// stripping it. Sanitising would let two different IDs name one file, and it
// would hide the caller bug that produced the odd ID in the first place.
func validRunID(runID string) error {
	if runID == "" {
		return fmt.Errorf("agent run ID is required")
	}
	for _, character := range runID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return fmt.Errorf("agent run ID %q is not a bare identifier", runID)
	}
	return nil
}

var _ agent_runtime.RunOutput = Reader{}
