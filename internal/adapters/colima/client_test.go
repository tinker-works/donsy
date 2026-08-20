package colima

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

// runningProfile is what `colima list --json` prints for a profile that is up.
// It is NDJSON — one object per line, not an array.
func runningProfile(name string) string {
	return fmt.Sprintf(
		`{"name":"%s","status":"Running","arch":"aarch64","cpus":%d,"memory":%d}`+"\n",
		name, profileCPUs, int64(profileMemoryGiB)<<30)
}

// fingerprintQuery is the inspect that reads back a container's spec label.
func fingerprintQuery(profile string) string {
	return "docker --host " + dockerHost(profile) +
		` inspect --format {{index .Config.Labels "go-merge.spec"}}`
}

// noSuchObject is what docker says about a container that was never created.
func noSuchObject() response {
	return response{err: fmt.Errorf("Error: No such object: gm-7-issue-coding")}
}

// absentContainer arranges for the status inspect alone to report the container
// missing. It is deliberately narrow: an answer keyed on "docker" would also
// answer the image inspect, the volume inspect and the create, and Ensure would
// fail on the first of them instead of exercising the path under test.
func absentContainer(runner *fakeRunner, profile string) {
	runner.answer(
		"docker --host "+dockerHost(profile)+" inspect --format {{.State.Status}}",
		noSuchObject())
}

// freshSandbox is a subject that has never had a round: no container, and no
// session volume either, which is what makes Ensure report a new session.
func freshSandbox(runner *fakeRunner, profile string) {
	absentContainer(runner, profile)
	runner.answer("docker --host "+dockerHost(profile)+" volume inspect", noSuchObject())
}

func testSpec(t *testing.T) agent_runtime.SandboxSpec {
	t.Helper()
	return agent_runtime.SandboxSpec{
		Sandbox: agent.Sandbox{
			ID: "sandbox-1", ProjectID: 7, Name: "gm-7-issue-coding",
			Role:    agent.AgentRoleCoding,
			Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
			Status:  agent.SandboxStatusCreating,
		},
	}
}

// clientWith builds a client whose state and build directories are temporary,
// against a runner that reports the project's profile already running — the
// ordinary case, where a test is not about the profile itself.
func clientWith(t *testing.T, runner *fakeRunner) *Client {
	t.Helper()
	root := t.TempDir()
	runner.answer("colima list --json", response{output: runningProfile("gm-7")})
	return newClient(runner,
		filepath.Join(root, "log"), root,
		filepath.Join(root, "containers"), filepath.Join(root, "build"))
}

func TestClient_Ensure_ShouldCreateTheContainerAgainstItsProjectsProfile(t *testing.T) {
	// Arrange: no container yet, so inspecting one fails the way docker does.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	freshSandbox(runner, "gm-7")

	// Act
	created, err := client.Ensure(context.Background(), testSpec(t))

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected a new session to be reported for a sandbox that did not exist")
	}
	if !runner.ran("docker", "--host", "/gm-7/docker.sock", "create", "--name", "gm-7-issue-coding") {
		t.Fatalf("expected the container created against project 7's profile:\n%s",
			strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_EnsureSessionVolume_ShouldPropagateANonNotFoundInspectError(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" volume inspect",
		response{err: fmt.Errorf("permission denied")})

	// Act
	_, err := client.ensureSessionVolume(context.Background(), "gm-7", "gm-7-issue-coding")

	// Assert
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected the inspect error to be propagated, got %v", err)
	}
	if runner.ran("volume", "create") {
		t.Fatalf("expected no volume creation after an inspect failure: %v", runner.lines())
	}
}

func TestClient_EnsureSessionVolume_ShouldNotTreatAnUnrelatedNotFoundPhraseAsAbsence(t *testing.T) {
	// Arrange: only Docker's own error response may authorize volume creation.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" volume inspect",
		response{err: fmt.Errorf("permission denied: no such volume: session")})

	// Act
	_, err := client.ensureSessionVolume(context.Background(), "gm-7", "session")

	// Assert
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected the inspect error to be propagated, got %v", err)
	}
	if runner.ran("volume", "create") {
		t.Fatalf("expected no volume creation after an inspect failure: %v", runner.lines())
	}
}

func TestCreateArgs_ShouldLimitMemoryWithoutLimitingCPU(t *testing.T) {
	// Arrange
	args := createArgs(testSpec(t), "image")

	// Act, Assert
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--memory "+containerMemoryLimit) {
		t.Fatalf("expected a %s memory limit, got %v", containerMemoryLimit, args)
	}
	if strings.Contains(joined, "--cpus") {
		t.Fatalf("expected no CPU limit, found %v", args)
	}
}

func TestCreateArgs_ShouldAllowRootlessDockerToCreateUserNamespaces(t *testing.T) {
	// Arrange
	spec := testSpec(t)
	spec.InstallDocker = true

	// Act
	args := createArgs(spec, "image")

	// Assert: the nested daemon is rootless, but RootlessKit still needs its
	// user-namespace unshare allowed by the outer Docker seccomp profile.
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--security-opt seccomp=unconfined") {
		t.Fatalf("expected the nested Docker security option, got %v", args)
	}
}

// Everything the user types `docker` for goes to whatever context is active,
// and colima's default is to make its own the active one. A regression here
// silently points the user's docker at an agent's machine, which is the most
// visible way this adapter could misbehave.
func TestClient_Ensure_ShouldNeverActivateTheProfilesDockerContext(t *testing.T) {
	// Arrange: the profile is not running, so starting it is part of Ensure.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	freshSandbox(runner, "gm-7")
	runner.answer("colima list --json", response{output: ""})

	// Act
	if _, err := client.Ensure(context.Background(), testSpec(t)); err != nil {
		t.Fatal(err)
	}

	// Assert
	if !runner.ran("colima start", "--activate=false") {
		t.Fatalf("expected the profile started without activating its context:\n%s",
			strings.Join(runner.lines(), "\n"))
	}
	if runner.ran("docker context") {
		t.Fatal("expected the user's docker context never to be touched")
	}
}

func TestClient_Ensure_ShouldMountOnlyTheGoMergeRoot(t *testing.T) {
	// Arrange: colima's own default mounts the whole home directory writable,
	// and an explicit mount replaces it rather than adding to it.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	freshSandbox(runner, "gm-7")
	runner.answer("colima list --json", response{output: ""})

	// Act
	if _, err := client.Ensure(context.Background(), testSpec(t)); err != nil {
		t.Fatal(err)
	}

	// Assert
	if !runner.ran("colima start", "--mount "+client.hostRoot+":w") {
		t.Fatalf("expected exactly the go-merge root mounted:\n%s",
			strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_Ensure_ShouldReuseAContainerWhoseSpecStillMatches(t *testing.T) {
	// Arrange: the container exists and carries the fingerprint the current
	// spec produces, so nothing about it needs to change.
	spec := testSpec(t)
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" inspect --format {{.State.Status}}",
		response{output: "exited"})
	runner.answer(fingerprintQuery("gm-7"),
		response{output: specFingerprint(spec, imageTag(spec))})

	// Act
	created, err := client.Ensure(context.Background(), spec)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected a reused sandbox to report no new session")
	}
	if runner.ran("create", "--name") {
		t.Fatalf("expected no container to be created:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

// The mounts a round is given are baked into the container at creation and
// cannot be changed afterwards. Adding a repository to an epic changes them,
// and without this the refiner would go on running with the old set forever.
func TestClient_Ensure_ShouldRecreateTheContainerWhenTheSpecMoved(t *testing.T) {
	// Arrange: an existing container carrying some other fingerprint.
	spec := testSpec(t)
	spec.Mounts = []agent_runtime.SandboxMount{
		{HostLocation: "/host/repo", GuestLocation: "/work/repo", Writable: true},
	}
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" inspect --format {{.State.Status}}",
		response{output: "exited"})
	runner.answer(fingerprintQuery("gm-7"),
		response{output: "a-fingerprint-from-before"})

	// Act
	created, err := client.Ensure(context.Background(), spec)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !runner.ran("rm", "--force", "gm-7-issue-coding") {
		t.Fatalf("expected the stale container removed:\n%s", strings.Join(runner.lines(), "\n"))
	}
	if !runner.ran("create", "--name", "gm-7-issue-coding") {
		t.Fatalf("expected the container recreated:\n%s", strings.Join(runner.lines(), "\n"))
	}
	// The conversation is not the container's, and a changed mount is no reason
	// to end it.
	if created {
		t.Fatal("expected the surviving session volume to report no new session")
	}
	if !runner.ran("--volume", "/host/repo:/work/repo") ||
		runner.ran("/host/repo:/work/repo:ro") {
		t.Fatalf("expected the writable mount bound writable:\n%s",
			strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_Ensure_ShouldBindReadOnlyMountsReadOnly(t *testing.T) {
	// Arrange
	spec := testSpec(t)
	spec.Mounts = []agent_runtime.SandboxMount{
		{HostLocation: "/host/tree", GuestLocation: "/work/issues"},
	}
	runner := newFakeRunner()
	client := clientWith(t, runner)
	freshSandbox(runner, "gm-7")

	// Act
	if _, err := client.Ensure(context.Background(), spec); err != nil {
		t.Fatal(err)
	}

	// Assert
	if !runner.ran("--volume", "/host/tree:/work/issues:ro") {
		t.Fatalf("expected the mount bound read-only:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_Ensure_ShouldBindAHostIdentityMountReadOnly(t *testing.T) {
	// Arrange: Docker in the profile resolves bind sources against the VM's
	// absolute filesystem, so the checkout is also mounted at that identity.
	spec := testSpec(t)
	spec.Mounts = []agent_runtime.SandboxMount{
		{HostLocation: "/host/repo", GuestLocation: "/host/repo"},
	}
	runner := newFakeRunner()
	client := clientWith(t, runner)
	freshSandbox(runner, "gm-7")

	// Act
	if _, err := client.Ensure(context.Background(), spec); err != nil {
		t.Fatal(err)
	}

	// Assert
	if !runner.ran("--volume", "/host/repo:/host/repo:ro") {
		t.Fatalf("expected the host-identity mount bound read-only:\n%s",
			strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_Ensure_ShouldRejectAnArbitraryOutOfRootGuestMount(t *testing.T) {
	// Arrange
	spec := testSpec(t)
	spec.Mounts = []agent_runtime.SandboxMount{
		{HostLocation: "/host/repo", GuestLocation: "/other/repo"},
	}
	runner := newFakeRunner()
	client := clientWith(t, runner)

	// Act
	_, err := client.Ensure(context.Background(), spec)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "outside guest work roots") {
		t.Fatalf("expected the out-of-root mount to be rejected, got %v", err)
	}
	if len(runner.lines()) != 0 {
		t.Fatalf("expected no provider command for an invalid mount:\n%s",
			strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_Ensure_ShouldConfigureNestedDockerOnlyWhenAsked(t *testing.T) {
	// Arrange
	for _, wanted := range []bool{false, true} {
		spec := testSpec(t)
		spec.InstallDocker = wanted
		runner := newFakeRunner()
		client := clientWith(t, runner)
		freshSandbox(runner, "gm-7")

		// Act
		if _, err := client.Ensure(context.Background(), spec); err != nil {
			t.Fatal(err)
		}

		// Assert
		if got := runner.ran("GO_MERGE_INSTALL_DOCKER=1"); got != wanted {
			t.Fatalf("InstallDocker=%v configured nested Docker=%v:\n%s",
				wanted, got, strings.Join(runner.lines(), "\n"))
		}
		if runner.ran("/var/run/docker.sock:/var/run/docker.sock") || runner.ran("--group-add") {
			t.Fatalf("expected no profile Docker socket exposure:\n%s",
				strings.Join(runner.lines(), "\n"))
		}
	}
}

func TestClient_Ensure_ShouldRejectAnInvalidSpec(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	spec := testSpec(t)
	spec.Sandbox.Name = ""

	// Act
	_, err := client.Ensure(context.Background(), spec)

	// Assert
	if err == nil {
		t.Fatal("expected an invalid spec to be refused")
	}
	if len(runner.lines()) != 0 {
		t.Fatalf("expected nothing to run for an invalid spec:\n%s",
			strings.Join(runner.lines(), "\n"))
	}
}

// This is the test that protects the sweep from itself. Inspect runs for every
// sandbox record on every tick; if it started the profile to answer, stopping
// an idle project's machine would be undone before the next tick.
func TestClient_Inspect_ShouldNotStartAStoppedProfile(t *testing.T) {
	// Arrange: a recorded container whose profile is not running.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("colima list --json", response{output: ""})
	if err := client.writeRecord("gm-7-issue-coding", record{Profile: "gm-7"}); err != nil {
		t.Fatal(err)
	}

	// Act
	status, err := client.Inspect(context.Background(),
		agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if status != agent.SandboxStatusStopped {
		t.Fatalf("expected a container in a stopped machine to be Stopped, got %q", status)
	}
	if runner.ran("colima start") {
		t.Fatalf("expected no profile start:\n%s", strings.Join(runner.lines(), "\n"))
	}
	if runner.ran("docker") {
		t.Fatalf("expected no daemon call against a stopped machine:\n%s",
			strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_Inspect_ShouldReportAbsentWithoutARecord(t *testing.T) {
	// Arrange: nothing was ever created under this name. Answering Stopped
	// instead would send reclaim to docker rm and start the machine to do it.
	runner := newFakeRunner()
	client := clientWith(t, runner)

	// Act
	status, err := client.Inspect(context.Background(),
		agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if status != agent.SandboxStatusAbsent {
		t.Fatalf("expected Absent, got %q", status)
	}
	if len(runner.lines()) != 0 {
		t.Fatalf("expected nothing to run:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_Inspect_ShouldMapDockerState(t *testing.T) {
	// Arrange
	for state, want := range map[string]agent.SandboxStatus{
		"running":    agent.SandboxStatusRunning,
		"created":    agent.SandboxStatusStopped,
		"exited":     agent.SandboxStatusStopped,
		"paused":     agent.SandboxStatusStopped,
		"restarting": agent.SandboxStatusStarting,
		"dead":       agent.SandboxStatusBroken,
		"removing":   agent.SandboxStatusBroken,
		// Reconciliation deletes a Broken sandbox, so a state this code does
		// not know must be read as Broken rather than silently as healthy.
		"hibernating": agent.SandboxStatusBroken,
	} {
		runner := newFakeRunner()
		client := clientWith(t, runner)
		runner.answer("colima list --json", response{output: runningProfile("gm-7")})
		runner.answer("docker --host", response{output: state})
		if err := client.writeRecord("gm-7-issue-coding", record{Profile: "gm-7"}); err != nil {
			t.Fatal(err)
		}

		// Act
		status, err := client.Inspect(context.Background(),
			agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"})

		// Assert
		if err != nil {
			t.Fatal(err)
		}
		if status != want {
			t.Fatalf("docker state %q mapped to %q, want %q", state, status, want)
		}
	}
}

func TestClient_Delete_ShouldOnlyDropTheRecordWhileTheProfileIsDown(t *testing.T) {
	// Arrange: reclaiming an idle sandbox must not boot the machine it is in.
	// The container becomes garbage inside a VM nobody is using, and the next
	// start collects it.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("colima list --json", response{output: ""})
	if err := client.writeRecord("gm-7-issue-coding", record{Profile: "gm-7"}); err != nil {
		t.Fatal(err)
	}

	// Act
	err := client.Delete(context.Background(),
		agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if runner.ran("colima start") || runner.ran("docker") {
		t.Fatalf("expected nothing to be started:\n%s", strings.Join(runner.lines(), "\n"))
	}
	if _, exists, _ := client.readRecord("gm-7-issue-coding"); exists {
		t.Fatal("expected the record to be dropped")
	}
}

func TestClient_Delete_ShouldRemoveTheContainerAndItsSessionWhenTheProfileIsUp(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("colima list --json", response{output: runningProfile("gm-7")})

	// Act
	err := client.Delete(context.Background(),
		agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !runner.ran("rm", "--force", "--volumes", "gm-7-issue-coding") {
		t.Fatalf("expected the container removed:\n%s", strings.Join(runner.lines(), "\n"))
	}
	// The session goes with the sandbox, or --continue would resume a
	// conversation whose sandbox was reclaimed.
	if !runner.ran("volume", "rm", "--force", "gm-session-gm-7-issue-coding") {
		t.Fatalf("expected the session volume removed:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_Delete_ShouldAcceptARecordThatIsAlreadyGone(t *testing.T) {
	// Arrange: reclaim is retried, and the second attempt must not fail over
	// work the first one finished.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("colima list --json", response{output: ""})

	// Act
	err := client.Delete(context.Background(),
		agent.SandboxRef{ProjectID: 7, Name: "never-created"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
}

func TestClient_Stop_ShouldNotStartTheProfileToStopSomethingInIt(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("colima list --json", response{output: ""})

	// Act
	err := client.Stop(context.Background(),
		agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if runner.ran("colima start") {
		t.Fatalf("expected no profile start:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_StopNow_ShouldCutThePower(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("colima list --json", response{output: runningProfile("gm-7")})
	runner.answer("docker --host", response{output: "running"})

	// Act
	err := client.StopNow(context.Background(),
		agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !runner.ran("kill", "gm-7-issue-coding") {
		t.Fatalf("expected the container killed:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_Run_ShouldExecInTheContainer(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	runner.fallback = response{output: "agent said so"}
	client := newClient(runner)

	// Act
	output, err := client.Run(context.Background(),
		agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"}, "run-1",
		[]string{"opencode", "run", "--continue"})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if output != "agent said so" {
		t.Fatalf("unexpected output %q", output)
	}
	// --workdir is what keeps the round's own mounts from counting as external
	// directories; see agent_runtime.GuestMountRoot.
	if !runner.ran("docker", "--host", "/gm-7/docker.sock",
		"exec", "--workdir", guestWork, "gm-7-issue-coding", "opencode run --continue") {
		t.Fatalf("unexpected command:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_Run_ShouldRejectAnEmptyCommand(t *testing.T) {
	// Arrange
	client := newClient(newFakeRunner())

	// Act
	_, err := client.Run(context.Background(),
		agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"}, "run-1", nil)

	// Assert
	if err == nil {
		t.Fatal("expected an empty command to be refused")
	}
}

func TestClient_Run_ShouldStoreTheTranscriptUnderTheRunID(t *testing.T) {
	// Arrange: the transcript is named after the run, not the sandbox and a
	// timestamp, so it is findable from an AgentRun alone.
	logDir := t.TempDir()
	runner := newFakeRunner()
	runner.fallback = response{output: `{"type":"text"}`}
	client := newClient(runner, logDir)

	// Act
	if _, err := client.Run(context.Background(),
		agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"}, "run-1",
		[]string{"opencode", "run"}); err != nil {
		t.Fatal(err)
	}

	// Assert
	contents, err := os.ReadFile(filepath.Join(logDir, "run-1.stdout.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != `{"type":"text"}` {
		t.Fatalf("unexpected transcript %q", contents)
	}
}

func TestClient_Run_ShouldRequireARunIDToNameTheTranscript(t *testing.T) {
	// Arrange: checked before sanitising, because safeLogName falls back to
	// "agent" and would send an unnamed run to a shared file.
	client := newClient(newFakeRunner(), t.TempDir())

	// Act
	_, err := client.Run(context.Background(),
		agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"}, "",
		[]string{"opencode", "run"})

	// Assert
	if err == nil {
		t.Fatal("expected a missing run ID to be refused")
	}
}

func TestSafeLogName_ShouldReduceToAFileName(t *testing.T) {
	// Arrange, Act, Assert
	for input, want := range map[string]string{
		"run-1":      "run-1",
		"a/b":        "a_b",
		"../escape":  "___escape",
		"":           "agent",
		"01KZX_AB-9": "01KZX_AB-9",
	} {
		if got := safeLogName(input); got != want {
			t.Fatalf("safeLogName(%q) = %q, want %q", input, got, want)
		}
	}
}

// A budget that fires has to name the operation and the limit. Left alone, a
// timed-out subprocess reports "signal: killed", which names neither.
func TestClient_ShouldNotWaitForeverOnAWedgedCommand(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	client.limit = time.Millisecond
	if err := client.writeRecord("gm-7-issue-coding", record{Profile: "gm-7"}); err != nil {
		t.Fatal(err)
	}
	runner.answer("colima list --json", response{err: context.DeadlineExceeded})

	// Act
	_, err := client.Inspect(context.Background(),
		agent.SandboxRef{ProjectID: 7, Name: "gm-7-issue-coding"})

	// Assert
	if err == nil {
		t.Fatal("expected the wedged command to fail")
	}
}
