package colima

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain/agent"
)

func startRef() agent.SandboxRef {
	return agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"}
}

// docker start returns once the process is spawned, not once its init has
// staged the round's credentials. Without the wait the first agent command
// races that and comes back as an authentication error that names no file.
func TestClient_Start_ShouldWaitForTheContainerToBeReady(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" inspect --format {{.State.Status}}",
		response{output: "exited"})

	// Act
	err := client.Start(context.Background(), startRef())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !runner.ran("start", "gm-7-issue-coding") {
		t.Fatalf("expected the container started:\n%s", strings.Join(runner.lines(), "\n"))
	}
	if !runner.ran("exec", "gm-7-issue-coding", "test", "-f", readyMarker) {
		t.Fatalf("expected the readiness probe:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

// A container can already be running if go-merge quit after starting it but
// before recording that. Restarting it would disturb a round still finishing.
func TestClient_Start_ShouldNotRestartOneAlreadyRunning(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" inspect --format {{.State.Status}}",
		response{output: "running"})

	// Act
	if err := client.Start(context.Background(), startRef()); err != nil {
		t.Fatal(err)
	}

	// Assert
	if runner.ran("docker", "start") {
		t.Fatalf("expected no restart:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_Start_ShouldGiveUpOnAContainerThatNeverBecomesReady(t *testing.T) {
	// Arrange: an init that never writes its marker would otherwise hold the
	// round for as long as the process lives.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	client.limit = 10 * time.Millisecond
	runner.answer("docker --host "+dockerHost("gm-7")+" inspect --format {{.State.Status}}",
		response{output: "exited"})
	runner.answer("docker --host "+dockerHost("gm-7")+" exec",
		response{err: fmt.Errorf("no such file")})

	// Act
	err := client.Start(context.Background(), startRef())

	// Assert
	if err == nil {
		t.Fatal("expected the readiness wait to give up")
	}
	if !strings.Contains(err.Error(), "gm-7-issue-coding") {
		t.Fatalf("expected the sandbox named in %q", err)
	}
}

func TestClient_Start_ShouldStopWaitingWhenTheRoundIsCancelled(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" inspect --format {{.State.Status}}",
		response{output: "exited"})
	runner.answer("docker --host "+dockerHost("gm-7")+" exec",
		response{err: fmt.Errorf("no such file")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Act
	err := client.Start(ctx, startRef())

	// Assert
	if err == nil {
		t.Fatal("expected the cancellation to end the wait")
	}
}

func TestClient_Start_ShouldStartTheProfileFirst(t *testing.T) {
	// Arrange: a round dispatched after the sweep stopped the host has to bring
	// it back, and Start is the first thing that touches it.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("colima list --json", response{output: ""})

	// Act
	_ = client.Start(context.Background(), startRef())

	// Assert
	if !runner.ran("colima start", "--profile", "gm-7") {
		t.Fatalf("expected the host started:\n%s", strings.Join(runner.lines(), "\n"))
	}
}
