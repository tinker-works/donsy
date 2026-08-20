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

// stubLimactl puts a limactl on PATH that answers `list` with the instances
// named and appends every invocation to a log the test reads back.
func stubLimactl(t *testing.T, instances ...string) string {
	t.Helper()
	directory := t.TempDir()
	log := filepath.Join(directory, "calls.log")
	var listing strings.Builder
	for _, name := range instances {
		fmt.Fprintf(&listing, `{"name":"%s","status":"Stopped"}`+"\n", name)
	}
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + log + "\n" +
		"if [ \"$1\" = list ]; then printf '%s' '" + listing.String() + "'; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(directory, "limactl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	return log
}

// The previous runtime left a full disk image per subject and role. Nothing
// will ever name them again, so they are handed back — but only the ones it
// minted: a Lima instance the user created for something else is not ours.
func TestRetireLima_ShouldDeleteOnlyTheAgentInstances(t *testing.T) {
	// Arrange
	root := t.TempDir()
	log := stubLimactl(t, "gm-3-issue-coding", "my-own-vm", "gm-3-epic-refiner")

	// Act
	RetireLima(context.Background(), root)

	// Assert
	contents, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	calls := string(contents)
	for _, ours := range []string{"gm-3-issue-coding", "gm-3-epic-refiner"} {
		if !strings.Contains(calls, "delete --force "+ours) {
			t.Fatalf("expected %q deleted:\n%s", ours, calls)
		}
	}
	if strings.Contains(calls, "my-own-vm") {
		t.Fatalf("expected somebody else's instance left alone:\n%s", calls)
	}
}

func TestRetireLima_ShouldSurviveAListingItCannotRead(t *testing.T) {
	// Arrange: best effort throughout — this hands back disk, and nothing
	// depends on it, so it must never fail a launch.
	root := t.TempDir()
	directory := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = list ]; then echo 'not json'; fi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(directory, "limactl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)

	// Act, Assert: the marker is written whatever happened, so a host is never
	// asked to shell out to a retired provider twice.
	RetireLima(context.Background(), root)
	if _, err := os.Stat(filepath.Join(root, "state", "lima-retired")); err != nil {
		t.Fatal(err)
	}
}

func TestNewClient_ShouldBeReadyToLockAProfile(t *testing.T) {
	// Arrange: the per-profile lock map has to exist before the first Ensure,
	// or the lazy start would panic rather than serialize.
	root := t.TempDir()

	// Act
	client := NewClient(
		filepath.Join(root, "log"), root,
		filepath.Join(root, "containers"), filepath.Join(root, "build"))

	// Assert
	unlock := client.lockProfile("gm-7")
	unlock()
	if client.hostRoot != root {
		t.Fatalf("unexpected host root %q", client.hostRoot)
	}
}

func TestColimaHome_ShouldHonourTheEnvironment(t *testing.T) {
	// Arrange: a caller that set COLIMA_HOME for colima means it for us too.
	t.Setenv("COLIMA_HOME", "/somewhere/colima")

	// Act, Assert
	if got := colimaHome(); got != "/somewhere/colima" {
		t.Fatalf("unexpected home %q", got)
	}
	if !strings.HasPrefix(dockerHost("gm-7"), "unix:///somewhere/colima/gm-7/") {
		t.Fatalf("unexpected docker host %q", dockerHost("gm-7"))
	}
}

// A bare "signal: killed" names neither the operation nor the limit.
func TestClient_ExplainDeadline_ShouldNameTheOperationAndTheBudget(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := newClient(runner)
	client.limit = time.Nanosecond
	runner.answer("colima list", response{err: fmt.Errorf("signal: killed")})

	// Act
	_, err := client.listProfiles(context.Background())

	// Assert
	if err == nil {
		t.Fatal("expected the wedged command to fail")
	}
	if !strings.Contains(err.Error(), "budget") || !strings.Contains(err.Error(), "colima list") {
		t.Fatalf("expected the operation and the budget in %q", err)
	}
}

func TestClient_FingerprintMatches_ShouldTreatAMissingContainerAsAMismatch(t *testing.T) {
	// Arrange
	runner := newFakeRunner()
	client := clientWith(t, runner)
	runner.answer(`docker --host `+dockerHost("gm-7")+` inspect --format {{index`, noSuchObject())

	// Act
	matches, err := client.fingerprintMatches(
		context.Background(), "gm-7", "gm-7-issue-coding", "abc")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if matches {
		t.Fatal("expected a container that is not there not to match")
	}
}

func TestSpecFingerprint_ShouldIgnoreTheOrderMountsWereAppendedIn(t *testing.T) {
	// Arrange: the order the application happens to append mounts in is not a
	// difference worth recreating a container over.
	first := agent_runtime.SandboxSpec{
		Sandbox: agent.Sandbox{Name: "gm-7-a"},
		Mounts: []agent_runtime.SandboxMount{
			{HostLocation: "/a", GuestLocation: "/work/a"},
			{HostLocation: "/b", GuestLocation: "/work/b"},
		},
	}
	second := first
	second.Mounts = []agent_runtime.SandboxMount{
		{HostLocation: "/b", GuestLocation: "/work/b"},
		{HostLocation: "/a", GuestLocation: "/work/a"},
	}

	// Act, Assert
	if specFingerprint(first, "image") != specFingerprint(second, "image") {
		t.Fatal("expected mount order not to change the fingerprint")
	}
	if specFingerprint(first, "image") == specFingerprint(first, "other-image") {
		t.Fatal("expected the image to change the fingerprint")
	}
	writable := first
	writable.Mounts = []agent_runtime.SandboxMount{
		{HostLocation: "/a", GuestLocation: "/work/a", Writable: true},
		{HostLocation: "/b", GuestLocation: "/work/b"},
	}
	if specFingerprint(first, "image") == specFingerprint(writable, "image") {
		t.Fatal("expected writability to change the fingerprint")
	}
}
