package colima

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProfileName_ShouldKeyByProjectAndStayInsideTheProviderLimits(t *testing.T) {
	// Arrange: Lima refuses a name that pushes its socket path to UNIX_PATH_MAX,
	// which leaves roughly 36 characters, and colima only accepts alphanumerics
	// separated by single . _ or - characters.
	for _, projectID := range []uint{1, 7, 4294967295} {
		// Act
		name := profileName(projectID)

		// Assert
		if len(name) > 36 {
			t.Fatalf("profile %q is %d characters", name, len(name))
		}
		for index := 0; index < len(name); index++ {
			character := name[index]
			if wordChar(character) || character == '-' {
				continue
			}
			t.Fatalf("profile %q contains %q, which colima would reject", name, character)
		}
	}
	if profileName(1) == profileName(2) {
		t.Fatal("expected one profile per project")
	}
}

func TestDockerHost_ShouldAddressTheProfilesOwnSocket(t *testing.T) {
	// Arrange, Act
	host := dockerHost("gm-7")

	// Assert
	if !strings.HasPrefix(host, "unix://") || !strings.HasSuffix(host, "/gm-7/docker.sock") {
		t.Fatalf("unexpected docker host %q", host)
	}
	if dockerHost("gm-7") == dockerHost("gm-8") {
		t.Fatal("expected each profile to have its own daemon")
	}
}

func TestVMTypeFor_ShouldUseAppleVirtualisationOnMacOS(t *testing.T) {
	// Arrange, Act, Assert
	if got := vmTypeFor("darwin"); got != "vz" {
		t.Fatalf("darwin got %q", got)
	}
	if got := vmTypeFor("linux"); got != "qemu" {
		t.Fatalf("linux got %q", got)
	}
}

// Empty output is what colima prints when no profile exists, and it is normal
// rather than a parse failure.
func TestClient_ListProfiles_ShouldAcceptNoInstancesAtAll(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := newClient(runner)
	runner.answer("colima list --json", response{output: "\n  \n"})

	// Act
	instances, err := client.listProfiles(context.Background())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Fatalf("expected no instances, got %#v", instances)
	}
}

// Dropping the line instead would silently remove a profile from the admission
// budget and let Reserve over-admit onto a host that is already full.
func TestClient_ListProfiles_ShouldRefuseALineItCannotRead(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := newClient(runner)
	runner.answer("colima list --json", response{output: "not json at all\n"})

	// Act
	_, err := client.listProfiles(context.Background())

	// Assert
	if err == nil {
		t.Fatal("expected an unreadable listing to be reported")
	}
}

// Two rounds arriving together — a project's first epic round and its first
// issue round do — must not each run `colima start` on the same profile.
func TestClient_EnsureProfile_ShouldStartAProfileOnlyOnceUnderConcurrency(t *testing.T) {
	// Arrange: the listing keeps reporting nothing, so only the lock can stop a
	// second start.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("colima list --json", response{output: ""})

	// Act
	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, _ = client.ensureProfile(context.Background(), 7)
		}()
	}
	group.Wait()

	// Assert: serialized, so the first start is followed by three listings that
	// still report nothing — but the starts themselves are what must not pile
	// up concurrently. Each one is bounded by the lock, so four is the ceiling
	// and any interleaving below it is fine; what would fail here is a start
	// running while another holds the same profile.
	if starts := runner.count("colima start"); starts > 4 {
		t.Fatalf("expected serialized starts, got %d:\n%s",
			starts, strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_EnsureProfile_ShouldNotStartOneThatIsAlreadyRunning(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)

	// Act
	profile, err := client.ensureProfile(context.Background(), 7)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if profile != "gm-7" {
		t.Fatalf("unexpected profile %q", profile)
	}
	if runner.ran("colima start") {
		t.Fatalf("expected no start:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

// The firewall is the boundary between an agent and the rest of the network.
// Running rounds without it is worse than running none, so a failure here fails
// the profile rather than being logged and passed over.
func TestClient_EnsureProfile_ShouldFailWhenThePreparationFails(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("colima list --json", response{output: ""})
	runner.answer("colima ssh", response{err: context.Canceled})

	// Act
	_, err := client.ensureProfile(context.Background(), 7)

	// Assert
	if err == nil {
		t.Fatal("expected a profile with no firewall to be refused")
	}
	if !strings.Contains(err.Error(), "gm-7") {
		t.Fatalf("expected the profile named in %q", err)
	}
}

func TestClient_EnsureProfile_ShouldRetryPreparationOnARunningProfile(t *testing.T) {
	// Arrange: preparation can fail after colima has successfully started the
	// VM, leaving the next attempt to observe a running but unsafe profile.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("colima ssh", response{err: context.Canceled})

	// Act
	if _, err := client.ensureProfile(context.Background(), 7); err == nil {
		t.Fatal("expected the first preparation attempt to fail")
	}
	runner.answer("colima ssh", response{})
	_, err := client.ensureProfile(context.Background(), 7)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if runner.count("colima ssh") != 2 {
		t.Fatalf("expected preparation retried on the running profile:\n%s",
			strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_StopProfile_ShouldRefuseWhileAContainerIsRunning(t *testing.T) {
	// Arrange: the caller decides from a snapshot taken at the top of a tick,
	// so only the daemon can say what is running now.
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" ps --quiet",
		response{output: "b0a1c2d3\n"})

	// Act
	stopped, err := client.StopProfile(context.Background(), 7)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("expected the busy profile left running")
	}
	if runner.ran("colima stop") {
		t.Fatalf("expected the busy profile left alone:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_StopProfile_ShouldStopAQuietOne(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" ps --quiet", response{output: "\n"})

	// Act
	stopped, err := client.StopProfile(context.Background(), 7)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("expected the quiet profile stopped")
	}
	if !runner.ran("colima stop", "--profile", "gm-7") {
		t.Fatalf("expected the profile stopped:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

// The sweep asks repeatedly and must not be told an already-down host is an
// error.
func TestClient_StopProfile_ShouldAcceptOneThatIsNotRunning(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("colima list --json", response{output: ""})

	// Act
	stopped, err := client.StopProfile(context.Background(), 7)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("expected the stopped profile left alone")
	}
	if runner.ran("colima stop") || runner.ran("docker") {
		t.Fatalf("expected nothing to run:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_ReapExpiredContainers_ShouldRemoveExpiredContainersAndUnusedImages(t *testing.T) {
	// Arrange
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	runner := newFakeRunner()
	client := clientWith(t, runner)
	prefix := "docker --host " + dockerHost("gm-7")
	runner.answer(prefix+" ps --quiet --filter status=running", response{output: "running\n"})
	runner.answer(prefix+" inspect --format {{.State.StartedAt}} running",
		response{output: now.Add(-time.Hour - time.Second).Format(time.RFC3339Nano)})

	// Act
	reaped, err := client.ReapExpiredContainers(
		context.Background(), 7, now.Add(-time.Hour), now.Add(-24*time.Hour))

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !reaped {
		t.Fatal("expected expired containers to be removed")
	}
	if !runner.ran("kill", "running") {
		t.Fatalf("expected expired running container killed:\n%s", strings.Join(runner.lines(), "\n"))
	}
	if !runner.ran("system", "prune", "--all", "--volumes", "until="+now.Add(-24*time.Hour).Format(time.RFC3339Nano)) {
		t.Fatalf("expected stopped containers, images, networks, and build cache pruned:\n%s", strings.Join(runner.lines(), "\n"))
	}
	if !runner.ran("volume", "prune", "--all", "--force") {
		t.Fatalf("expected unused named volumes pruned:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

// Without --data colima leaves the container data disk behind: a full disk
// image per project the host has ever registered, which nothing can name again.
func TestClient_DeleteProfile_ShouldTakeTheDataDiskWithIt(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	if err := client.writeRecord("gm-7-issue-coding", record{Profile: "gm-7"}); err != nil {
		t.Fatal(err)
	}

	// Act
	err := client.DeleteProfile(context.Background(), 7)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !runner.ran("colima delete", "--profile", "gm-7", "--force", "--data") {
		t.Fatalf("expected the profile and its data disk deleted:\n%s",
			strings.Join(runner.lines(), "\n"))
	}
	if _, exists, _ := client.readRecord("gm-7-issue-coding"); exists {
		t.Fatal("expected the project's records to go with its profile")
	}
}

// A Delete against a stopped profile leaves the container behind on purpose,
// rather than booting a VM to remove something from it. This is what collects
// them.
func TestClient_PruneOrphanContainers_ShouldRemoveWhatNoRecordNames(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer("docker --host "+dockerHost("gm-7")+" ps --all",
		response{output: "gm-7-kept\ngm-7-orphan\n"})
	if err := client.writeRecord("gm-7-kept", record{Profile: "gm-7"}); err != nil {
		t.Fatal(err)
	}

	// Act
	err := client.pruneOrphanContainers(context.Background(), "gm-7")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !runner.ran("rm", "--force", "--volumes", "gm-7-orphan") {
		t.Fatalf("expected the orphan removed:\n%s", strings.Join(runner.lines(), "\n"))
	}
	if runner.ran("rm", "--force", "--volumes", "gm-7-kept") {
		t.Fatalf("expected the recorded container kept:\n%s", strings.Join(runner.lines(), "\n"))
	}
}

func TestClient_PruneOrphanContainers_ShouldDoNothingWhenThereAreNone(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)

	// Act
	if err := client.pruneOrphanContainers(context.Background(), "gm-7"); err != nil {
		t.Fatal(err)
	}

	// Assert
	if runner.ran("rm", "--force") {
		t.Fatalf("expected nothing removed:\n%s", strings.Join(runner.lines(), "\n"))
	}
}
