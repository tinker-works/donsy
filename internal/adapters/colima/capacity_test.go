package colima

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

func reserveSpec(projectID uint, name string) agent_runtime.SandboxSpec {
	return agent_runtime.SandboxSpec{
		Sandbox: agent.Sandbox{
			ID: "sandbox-" + name, ProjectID: projectID, Name: name,
			Role:    agent.AgentRoleCoding,
			Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: name},
			Status:  agent.SandboxStatusCreating,
		},
	}
}

func TestClient_Reserve_ShouldAdmitARoundOntoAnIdleHost(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := newClient(runner)
	runner.answer("colima list --json", response{output: ""})

	// Act
	release, admitted, err := client.Reserve(
		context.Background(), reserveSpec(7, "gm-7-a"))

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !admitted {
		t.Fatal("expected an idle host to admit a round")
	}
	release()
}

// Sandbox names are derived from the subject, so admitting a second round would
// have two of them creating, starting and stopping the same container.
func TestClient_Reserve_ShouldRefuseASecondRoundOnTheSameSandbox(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := newClient(runner)
	runner.answer("colima list --json", response{output: ""})
	spec := reserveSpec(7, "gm-7-a")
	release, admitted, err := client.Reserve(context.Background(), spec)
	if err != nil || !admitted {
		t.Fatalf("expected the first round admitted: %v %v", admitted, err)
	}

	// Act
	_, admitted, err = client.Reserve(context.Background(), spec)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if admitted {
		t.Fatal("expected the second round on the same sandbox to be turned away")
	}
	release()
	if _, admitted, _ := client.Reserve(context.Background(), spec); !admitted {
		t.Fatal("expected the released sandbox to be admissible again")
	}
}

// A host commits its whole configured CPU and memory whether or not anything
// inside it is busy, so the machine admits hosts before a host admits rounds.
func TestClient_Reserve_ShouldRefuseAHostTheMachineHasNoRoomFor(t *testing.T) {
	// Arrange: another profile is already running and has taken the budget.
	runner := newFakeRunner()
	client := newClient(runner)
	runner.answer("colima list --json", response{output: fmt.Sprintf(
		`{"name":"someone-else","status":"Running","cpus":%d,"memory":%d}`+"\n",
		1<<20, int64(1)<<62)})

	// Act
	_, admitted, err := client.Reserve(
		context.Background(), reserveSpec(7, "gm-7-a"))

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if admitted {
		t.Fatal("expected no room for another host")
	}
}

// A round on a project whose host is already up is not a second copy of that
// host, so it is admitted whatever else the machine is running.
func TestClient_Reserve_ShouldAdmitOntoAHostThatIsAlreadyRunning(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := newClient(runner)
	runner.answer("colima list --json", response{output: fmt.Sprintf(
		`{"name":"gm-7","status":"Running","cpus":%d,"memory":%d}`+"\n",
		1<<20, int64(1)<<62)})

	// Act
	_, admitted, err := client.Reserve(
		context.Background(), reserveSpec(7, "gm-7-a"))

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if !admitted {
		t.Fatal("expected a round onto a running host to be admitted")
	}
}

func TestClient_Reserve_ShouldKeepHalfTheProfileMemoryFree(t *testing.T) {
	// Arrange: each agent sandbox reserves 2 GiB, so an 8 GiB profile admits two
	// and leaves the other half available for nested Docker work and VM overhead.
	runner := newFakeRunner()
	client := newClient(runner)
	runner.answer("colima list --json", response{output: runningProfile("gm-7")})

	// Act
	firstRelease, firstAdmitted, firstErr := client.Reserve(context.Background(), reserveSpec(7, "gm-7-a"))
	secondRelease, secondAdmitted, secondErr := client.Reserve(context.Background(), reserveSpec(7, "gm-7-b"))
	_, thirdAdmitted, thirdErr := client.Reserve(context.Background(), reserveSpec(7, "gm-7-c"))
	if firstAdmitted {
		defer firstRelease()
	}
	if secondAdmitted {
		defer secondRelease()
	}
	// Assert
	if firstErr != nil || secondErr != nil || thirdErr != nil {
		t.Fatalf("unexpected reserve errors: %v, %v, %v", firstErr, secondErr, thirdErr)
	}
	if !firstAdmitted || !secondAdmitted {
		t.Fatalf("expected two sandboxes admitted, got %v and %v", firstAdmitted, secondAdmitted)
	}
	if thirdAdmitted {
		t.Fatal("expected the third sandbox rejected after half the profile memory was reserved")
	}
}

func TestClient_Reserve_ShouldSurfaceAListingFailure(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := newClient(runner)
	runner.answer("colima list --json", response{err: fmt.Errorf("colima is not installed")})

	// Act
	_, _, err := client.Reserve(
		context.Background(), reserveSpec(7, "gm-7-a"))

	// Assert
	if err == nil {
		t.Fatal("expected the listing failure to be reported")
	}
}

// The ledger is what closes the gap the provider listing leaves: a host takes
// minutes to start and is not listed until it is up, so rounds that only read
// the snapshot would every one of them be told yes.
func TestClient_Reserve_ShouldNotAdmitEveryConcurrentRoundOffOneSnapshot(t *testing.T) {
	// Arrange: four different projects, so each would need a host of its own.
	runner := newFakeRunner()
	client := newClient(runner)
	runner.answer("colima list --json", response{output: ""})

	// Act
	var group sync.WaitGroup
	admissions := make([]bool, 8)
	for index := range admissions {
		group.Add(1)
		go func() {
			defer group.Done()
			project := uint(index + 1)
			_, admitted, err := client.Reserve(context.Background(),
				reserveSpec(project, fmt.Sprintf("gm-%d-a", project)))
			if err == nil {
				admissions[index] = admitted
			}
		}()
	}
	group.Wait()

	// Assert
	granted := 0
	for _, admitted := range admissions {
		if admitted {
			granted++
		}
	}
	if granted == 0 {
		t.Fatal("expected at least one round to be admitted")
	}
	if granted == len(admissions) {
		t.Fatal("expected the ledger to hold the budget against a stale snapshot")
	}
}

func TestClient_FreeBytes_ShouldReportSomethingForTheHostVolume(t *testing.T) {
	// Arrange
	client := newClient(newFakeRunner())

	// Act
	free, err := client.FreeBytes()

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if free <= 0 {
		t.Fatalf("expected free space, got %d", free)
	}
}

func TestProfileSize_ShouldUseFixedResources(t *testing.T) {
	// Arrange, Act
	cpus, memoryGiB := profileSize()

	// Assert
	if cpus != profileCPUs || memoryGiB != profileMemoryGiB {
		t.Fatalf("profile size = %d CPUs and %dGiB, want %d CPUs and %dGiB",
			cpus, memoryGiB, profileCPUs, profileMemoryGiB)
	}
}

func TestHostBudget_ShouldCommitOnlyPartOfTheMachine(t *testing.T) {
	// Arrange, Act
	cpus, memory, err := hostBudget(context.Background())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if cpus < 1 || memory <= 0 {
		t.Fatalf("unusable budget: %d CPUs, %d bytes", cpus, memory)
	}
}

func TestScrubbedEnvironment_ShouldRemoveTheDockerRedirections(t *testing.T) {
	// Arrange: a DOCKER_HOST inherited from the user's shell would outrank the
	// --host this adapter passes, sending agents to Docker Desktop.
	t.Setenv("DOCKER_HOST", "unix:///somewhere/else.sock")
	t.Setenv("DOCKER_CONTEXT", "desktop-linux")
	t.Setenv("GO_MERGE_KEEP_ME", "yes")

	// Act
	environment := scrubbedEnvironment()

	// Assert
	joined := strings.Join(environment, "\n")
	for _, removed := range []string{"DOCKER_HOST=", "DOCKER_CONTEXT="} {
		if strings.Contains(joined, removed) {
			t.Fatalf("expected %q to be scrubbed", removed)
		}
	}
	if !strings.Contains(joined, "GO_MERGE_KEEP_ME=yes") {
		t.Fatal("expected unrelated variables to survive")
	}
}
