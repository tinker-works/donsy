package colima

import (
	"context"
	"io"
	"strings"
	"sync"
)

// recordedCommand is one subprocess the client would have run.
type recordedCommand struct {
	name string
	args []string
}

func (c recordedCommand) line() string {
	return strings.Join(append([]string{c.name}, c.args...), " ")
}

// fakeRunner answers per command shape rather than with one canned reply,
// because a single Ensure now issues six or seven different subprocesses —
// listing profiles, inspecting an image, building it, inspecting a container,
// creating a volume and creating the container — and a test that cares about
// one of them still has to let the others through.
//
// A response is keyed by a prefix of the command line, longest match first, so
// "docker --host … inspect" can be answered without also answering
// "docker --host … image inspect".
type fakeRunner struct {
	mu       sync.Mutex
	commands []recordedCommand
	// responses maps a command-line prefix to what it returns.
	responses map[string]response
	// fallback answers anything no prefix matches. The zero value is success
	// with no output, which is what most of a test wants from most commands.
	fallback response
}

type response struct {
	output string
	err    error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{responses: map[string]response{}}
}

// answer registers what a command whose line starts with prefix returns.
func (r *fakeRunner) answer(prefix string, out response) *fakeRunner {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.responses[prefix] = out
	return r
}

func (r *fakeRunner) record(name string, args []string) response {
	r.mu.Lock()
	defer r.mu.Unlock()
	command := recordedCommand{name: name, args: args}
	r.commands = append(r.commands, command)
	line := command.line()
	best, found := response{}, false
	longest := -1
	for prefix, out := range r.responses {
		if strings.HasPrefix(line, prefix) && len(prefix) > longest {
			best, found, longest = out, true, len(prefix)
		}
	}
	if !found {
		return r.fallback
	}
	return best
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	return r.record(name, args).err
}

func (r *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	out := r.record(name, args)
	return []byte(out.output), out.err
}

func (r *fakeRunner) OutputTo(
	_ context.Context, stdout, _ io.Writer, name string, args ...string,
) ([]byte, error) {
	out := r.record(name, args)
	if stdout != nil {
		_, _ = io.WriteString(stdout, out.output)
	}
	return []byte(out.output), out.err
}

// lines is every command the client ran, as whole command lines.
func (r *fakeRunner) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	lines := make([]string, 0, len(r.commands))
	for _, command := range r.commands {
		lines = append(lines, command.line())
	}
	return lines
}

// ran reports whether any command line contains every one of parts, so a test
// can pin the flags it cares about without spelling out the ones it does not.
//
// Matching is on word boundaries rather than raw substrings. Plain
// strings.Contains found "rm" inside "--format" and passed a test that meant to
// assert nothing was removed.
func (r *fakeRunner) ran(parts ...string) bool {
	return r.count(parts...) > 0
}

func (r *fakeRunner) count(parts ...string) int {
	matches := 0
	for _, line := range r.lines() {
		found := true
		for _, part := range parts {
			if !containsWord(line, part) {
				found = false
				break
			}
		}
		if found {
			matches++
		}
	}
	return matches
}

// containsWord matches part anywhere in line, but only where the characters on
// either side are not word characters — so "rm" matches "image rm x" and not
// "--format".
func containsWord(line, part string) bool {
	for at := 0; ; {
		index := strings.Index(line[at:], part)
		if index < 0 {
			return false
		}
		start := at + index
		end := start + len(part)
		if !wordChar(part[0]) || start == 0 || !wordChar(line[start-1]) {
			if !wordChar(part[len(part)-1]) || end == len(line) || !wordChar(line[end]) {
				return true
			}
		}
		at = start + 1
	}
}

func wordChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}
