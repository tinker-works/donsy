package colima

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

// The fake runner pins the argv this adapter produces; only a real run pins
// that the argv works. This builds the actual image and takes one container
// through the whole lifecycle, which is what catches the failures that live
// between the two — an installer that needs a package the image does not have,
// a flag the CLI stopped accepting, a container that exits the moment it starts.
//
// It is slow and it downloads, so it is opt-in.
func TestClient_EndToEnd_ShouldRunTheLifecycleOnRealColima(t *testing.T) {
	requireRealColima(t)

	// Arrange: a project ID no real project will have, so this never touches a
	// registered project's machine.
	const projectID = 99989
	root := t.TempDir()
	client := NewClient(
		filepath.Join(root, "log"), root,
		filepath.Join(root, "containers"), filepath.Join(root, "build"))
	checkout := filepath.Join(root, "checkout")
	fixture := filepath.Join(checkout, "fixture.txt")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture, []byte("from host checkout\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setupScript := repositorySetupScript(t)
	spec := agent_runtime.SandboxSpec{
		Sandbox: agent.Sandbox{
			ID: "sandbox-e2e", ProjectID: projectID, Name: "gm-e2e-sandbox",
			Role:    agent.AgentRoleCoding,
			Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-e2e"},
			Status:  agent.SandboxStatusCreating,
		},
		InstallDocker: true,
		SetupScript:   setupScript,
		Mounts: []agent_runtime.SandboxMount{
			{HostLocation: checkout, GuestLocation: "/work/repo", Writable: true},
			{HostLocation: checkout, GuestLocation: checkout},
		},
	}
	ref := spec.Sandbox.Ref()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	t.Cleanup(func() {
		teardown, cancelTeardown := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancelTeardown()
		_ = client.Delete(teardown, ref)
		_ = client.DeleteProfile(teardown, projectID)
	})

	// Act
	created, err := client.Ensure(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if !created {
		t.Fatal("expected a first round on a fresh subject to report a new session")
	}
	if status, err := client.Inspect(ctx, ref); err != nil ||
		status != agent.SandboxStatusStopped {
		t.Fatalf("a created container should be Stopped, got %q (%v)", status, err)
	}
	if err := client.Start(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if status, err := client.Inspect(ctx, ref); err != nil ||
		status != agent.SandboxStatusRunning {
		t.Fatalf("a started container should be Running, got %q (%v)", status, err)
	}
	// The agent CLI is what every round depends on, and the reason this test
	// exists: it is installed from a vendor script into the image.
	output, err := client.Run(ctx, ref, "e2e-version",
		[]string{"opencode", "--version"})
	if err != nil {
		t.Fatalf("running the agent CLI failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, opencodeVersion) {
		t.Fatalf("expected the pinned version, got %q", output)
	}
	// Running as the agent account, from the directory every mount lands in.
	if output, err := client.Run(ctx, ref, "e2e-whoami", []string{"whoami"}); err != nil ||
		strings.TrimSpace(output) != guestUser {
		t.Fatalf("expected the round to run as %q, got %q (%v)", guestUser, output, err)
	}
	// The nested Docker daemon resolves the bind source in the VM, not in the
	// agent container. The host-identity mount and exported path make the source
	// usable without confusing /work/repo with the daemon's filesystem.
	dockerCommand := "export GO_MERGE_DOCKER_BIND_SOURCE=" + checkout +
		"; docker run --rm --volume \"$GO_MERGE_DOCKER_BIND_SOURCE:/src:ro\" " +
		"ubuntu:24.04 cat /src/fixture.txt"
	if output, err := client.Run(
		ctx, ref, "e2e-docker-bind", []string{"sh", "-c", dockerCommand},
	); err != nil ||
		strings.TrimSpace(output) != "from host checkout" {
		t.Fatalf("expected Docker to read the host checkout through the exported source, got %q (%v)",
			output, err)
	}
	// Seeded rather than downloaded: OpenCode fetches a glibc build that musl
	// cannot run, and checks only this path.
	// The image seeds ripgrep where OpenCode checks for it.
	if _, err := client.Run(ctx, ref, "e2e-rg",
		[]string{"sh", "-c", guestHome + "/.cache/opencode/bin/rg --version"}); err != nil {
		t.Fatalf("the seeded ripgrep is not runnable: %v", err)
	}

	if err := client.Stop(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if status, err := client.Inspect(ctx, ref); err != nil ||
		status != agent.SandboxStatusStopped {
		t.Fatalf("a stopped container should be Stopped, got %q (%v)", status, err)
	}
	// The second Ensure must reuse both, or every round would start a fresh
	// conversation and --continue would have nothing to continue.
	if created, err := client.Ensure(ctx, spec); err != nil || created {
		t.Fatalf("expected the second Ensure to reuse the session: created=%v (%v)", created, err)
	}
	if err := client.Delete(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if status, err := client.Inspect(ctx, ref); err != nil ||
		status != agent.SandboxStatusAbsent {
		t.Fatalf("a deleted container should be Absent, got %q (%v)", status, err)
	}
}

func requireRealColima(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_MERGE_E2E_COLIMA") != "1" {
		t.Skip("set GO_MERGE_E2E_COLIMA=1 to run the real colima lifecycle test " +
			"(slow, builds an image and boots a machine)")
	}
	for _, tool := range requiredTools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
}

func repositorySetupScript(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate the repository setup script")
	}
	setupScriptPath := filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "setup_script.sh")
	setupScript, err := os.ReadFile(setupScriptPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(setupScript)
}
