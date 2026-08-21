package colima

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"
)

type commandRunner interface {
	Run(context.Context, string, ...string) error
	Output(context.Context, string, ...string) ([]byte, error)
	OutputTo(context.Context, io.Writer, io.Writer, string, ...string) ([]byte, error)
}

type execRunner struct{}

// maxErrorOutput is how much of a failed command's stderr an error carries. The
// cause of a colima or docker failure is in its last lines, and the whole of a
// failed image build's output is far more than a run record should hold.
const maxErrorOutput = 2000

// killGrace bounds how long Wait may block after a cancellation was delivered.
const killGrace = 2 * time.Second

// Per-operation budgets. They are runaway guards, not estimates: what they
// bound is a colima or docker that never returns, which would otherwise hold a
// subject's round slot — and through it the host reservation Reserve handed out
// — for as long as the process lives.
const (
	// listTimeout bounds a read of local profile or container state.
	listTimeout = 30 * time.Second
	// profileStartTimeout covers downloading a disk image on the very first
	// start, booting the VM, and the preparation that follows it.
	profileStartTimeout  = 15 * time.Minute
	profileStopTimeout   = 3 * time.Minute
	profileDeleteTimeout = 5 * time.Minute
	// prepareTimeout bounds the firewall and daemon configuration applied
	// inside the VM on every start.
	prepareTimeout = 2 * time.Minute
	// imageBuildTimeout covers a cold build: the base image, the OpenCode
	// download, and a repository's own setup script, which may build a
	// toolchain from source.
	imageBuildTimeout = 60 * time.Minute
	createTimeout     = 2 * time.Minute
	startTimeout      = 2 * time.Minute
	stopTimeout       = 1 * time.Minute
	deleteTimeout     = 1 * time.Minute
	// readyTimeout bounds the wait for a started container's init to finish
	// staging credentials.
	readyTimeout = 60 * time.Second
	// hostProbeTimeout bounds the sysctl/procfs read behind the host budget.
	hostProbeTimeout = 5 * time.Second
)

// dockerEnvironment names the variables that redirect the docker CLI at a
// daemon of their own. They are stripped from every child, because this adapter
// picks its daemon per profile with an explicit --host and a value inherited
// from the user's shell would silently outrank it — sending an agent's
// containers to Docker Desktop, or to another project's profile.
var dockerEnvironment = []string{
	"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH",
	"DOCKER_CONFIG", "COMPOSE_FILE", "COMPOSE_PROJECT_NAME",
}

// scrubbedEnvironment is the process environment without the variables above.
func scrubbedEnvironment() []string {
	environment := os.Environ()
	kept := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if slices.Contains(dockerEnvironment, name) {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

// command builds a cancellable command whose cancellation actually lands.
//
// docker exec spawns the agent inside the container and colima spawns an ssh
// session. The default CommandContext behaviour kills only the CLI itself,
// leaving those children holding the write end of the stdout pipe — and Wait
// then blocks on an EOF that never arrives, which is what made quitting
// mid-round hang forever. A process group is what makes the signal reach the
// whole tree, and WaitDelay is the backstop for anything that still holds the
// pipe after being signalled.
func command(ctx context.Context, name string, args ...string) *exec.Cmd {
	prepared := exec.CommandContext(ctx, name, args...)
	prepared.Env = scrubbedEnvironment()
	prepared.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	prepared.Cancel = func() error {
		return syscall.Kill(-prepared.Process.Pid, syscall.SIGKILL)
	}
	prepared.WaitDelay = killGrace
	return prepared
}

// Run reports a failure with what the command said, not just its exit code. A
// bare "exit status 1" from colima names neither the profile nor the reason —
// and colima exits 1 for every failure it has — so its stderr is the only thing
// a failed round has to explain itself with.
func (execRunner) Run(ctx context.Context, name string, args ...string) error {
	var stderr bytes.Buffer
	prepared := command(ctx, name, args...)
	prepared.Stderr = &stderr
	return withCommandOutput(prepared.Run(), stderr.Bytes())
}

func (execRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := command(ctx, name, args...).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return output, withCommandOutput(err, exitErr.Stderr)
	}
	return output, err
}

func withCommandOutput(err error, stderr []byte) error {
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(stderr))
	if message == "" {
		return err
	}
	if len(message) > maxErrorOutput {
		message = "..." + message[len(message)-maxErrorOutput:]
	}
	return fmt.Errorf("%w: %s", err, message)
}

func (execRunner) OutputTo(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	name string,
	args ...string,
) ([]byte, error) {
	var output bytes.Buffer
	prepared := command(ctx, name, args...)
	prepared.Stdout = io.MultiWriter(&output, stdout)
	prepared.Stderr = stderr
	err := prepared.Run()
	return output.Bytes(), err
}
