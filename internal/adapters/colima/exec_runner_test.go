package colima

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as the helper binary these tests run as a subprocess, which
// is how the real execRunner can be exercised without depending on colima or
// docker being installed.
func TestMain(m *testing.M) {
	switch os.Getenv("GO_MERGE_HELPER") {
	case "":
		os.Exit(m.Run())
	case "env":
		_, _ = fmt.Println(strings.Join(os.Environ(), "\n"))
	case "output":
		_, _ = fmt.Fprint(os.Stdout, "to stdout")
		_, _ = fmt.Fprint(os.Stderr, "to stderr")
	case "fail":
		_, _ = fmt.Fprint(os.Stderr, "the reason it failed")
		os.Exit(3)
	case "sleep":
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

// helper re-runs this test binary as a subprocess. The mode is passed through
// the environment rather than the argv, because what several of these tests are
// about is which environment the child is given.
func helper(t *testing.T, mode string) (string, []string) {
	t.Helper()
	t.Setenv("GO_MERGE_HELPER", mode)
	return os.Args[0], []string{"-test.run", "TestMain"}
}

// A DOCKER_HOST inherited from the user's shell would outrank the --host this
// adapter passes on every command, silently sending an agent's containers to
// Docker Desktop or to another project's machine.
func TestExecRunner_ShouldNotPassDockerRedirectionsToChildren(t *testing.T) {
	// Arrange
	t.Setenv("DOCKER_HOST", "unix:///somewhere/else.sock")
	name, args := helper(t, "env")

	// Act
	output, err := execRunner{}.Output(context.Background(), name, args...)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "DOCKER_HOST=") {
		t.Fatalf("expected DOCKER_HOST to be scrubbed from the child:\n%s", output)
	}
	if !strings.Contains(string(output), "GO_MERGE_HELPER=env") {
		t.Fatalf("expected the child to keep the rest of the environment:\n%s", output)
	}
}

// A bare "exit status 3" names neither the profile nor the reason, and colima
// exits 1 for every failure it has — so its stderr is the only thing a failed
// round has to explain itself with.
func TestExecRunner_ShouldCarryTheFailuresOwnWords(t *testing.T) {
	// Arrange
	name, args := helper(t, "fail")

	// Act
	err := execRunner{}.Run(context.Background(), name, args...)

	// Assert
	if err == nil {
		t.Fatal("expected the failure to be reported")
	}
	if !strings.Contains(err.Error(), "the reason it failed") {
		t.Fatalf("expected the child's stderr in %q", err)
	}
}

func TestExecRunner_Output_ShouldCarryTheFailuresOwnWords(t *testing.T) {
	// Arrange
	name, args := helper(t, "fail")

	// Act
	_, err := execRunner{}.Output(context.Background(), name, args...)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "the reason it failed") {
		t.Fatalf("expected the child's stderr in %v", err)
	}
}

func TestExecRunner_OutputTo_ShouldTeeStdoutAndReturnIt(t *testing.T) {
	// Arrange: the transcript is written as the round produces it, and the same
	// bytes are returned for the answer to be parsed out of.
	name, args := helper(t, "output")
	var stdout, stderr bytes.Buffer

	// Act
	output, err := execRunner{}.OutputTo(context.Background(), &stdout, &stderr, name, args...)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "to stdout") ||
		!strings.Contains(stdout.String(), "to stdout") {
		t.Fatalf("expected stdout both returned and teed, got %q and %q", output, stdout.String())
	}
	if !strings.Contains(stderr.String(), "to stderr") {
		t.Fatalf("expected stderr teed, got %q", stderr.String())
	}
}

// docker exec spawns the agent inside the container. Killing only the CLI
// leaves that child holding the write end of the stdout pipe, and Wait then
// blocks on an EOF that never arrives — which is what made quitting mid-round
// hang forever.
func TestExecRunner_ShouldReturnWhenCancelledMidCommand(t *testing.T) {
	// Arrange
	name, args := helper(t, "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Act
	done := make(chan error, 1)
	go func() { done <- execRunner{}.Run(ctx, name, args...) }()

	// Assert
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("expected the cancelled command to return")
	}
}

func TestWithCommandOutput_ShouldTrimAVeryLongFailure(t *testing.T) {
	// Arrange: a failed build's whole output is far more than a run record
	// should hold, and its cause is in the last lines.
	stderr := bytes.Repeat([]byte("x"), maxErrorOutput*2)

	// Act
	err := withCommandOutput(fmt.Errorf("exit status 1"), stderr)

	// Assert
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(err.Error()) > maxErrorOutput+100 {
		t.Fatalf("expected the output trimmed, got %d characters", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "...") {
		t.Fatal("expected the trim to be visible")
	}
}

func TestWithCommandOutput_ShouldPassThroughSuccessAndSilence(t *testing.T) {
	// Arrange, Act, Assert
	if err := withCommandOutput(nil, []byte("noise")); err != nil {
		t.Fatalf("expected success to stay success, got %v", err)
	}
	original := fmt.Errorf("exit status 1")
	if err := withCommandOutput(original, []byte("   ")); err != original {
		t.Fatalf("expected a silent failure to pass through unchanged, got %v", err)
	}
}

func TestClient_OpenRunLogs_ShouldReportADirectoryItCannotCreate(t *testing.T) {
	// Arrange: a file where the log directory should be.
	root := t.TempDir()
	blocked := filepath.Join(root, "logs")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	client := newClient(newFakeRunner(), blocked)

	// Act
	_, _, err := client.openRunLogs("run-1")

	// Assert
	if err == nil {
		t.Fatal("expected the unusable log directory to be reported")
	}
}

func TestCheckTooling_ShouldNameWhatIsMissing(t *testing.T) {
	// Arrange: an empty PATH, so neither binary resolves. Left to the first
	// round this would be recorded against a subject and read as the agent
	// being broken rather than the machine being unequipped.
	t.Setenv("PATH", t.TempDir())

	// Act
	err := CheckTooling()

	// Assert
	if err == nil {
		t.Fatal("expected the missing tooling to be reported")
	}
	for _, tool := range requiredTools {
		if !strings.Contains(err.Error(), tool) {
			t.Fatalf("expected %q named in %q", tool, err)
		}
	}
}

func TestCheckTooling_ShouldAcceptAHostThatHasBoth(t *testing.T) {
	// Arrange: stand-ins on a PATH of our own, so the test does not depend on
	// colima being installed where it runs.
	directory := t.TempDir()
	for _, tool := range requiredTools {
		path := filepath.Join(directory, tool)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory)

	// Act, Assert
	if err := CheckTooling(); err != nil {
		t.Fatal(err)
	}
}

func TestJoinTools_ShouldReadAsASentence(t *testing.T) {
	// Arrange, Act, Assert
	for _, testCase := range []struct {
		tools []string
		want  string
	}{
		{[]string{"colima"}, "colima"},
		{[]string{"colima", "docker"}, "colima and docker"},
		{[]string{"a", "b", "c"}, "a, b and c"},
	} {
		if got := joinTools(testCase.tools); got != testCase.want {
			t.Fatalf("joinTools(%v) = %q, want %q", testCase.tools, got, testCase.want)
		}
	}
}
