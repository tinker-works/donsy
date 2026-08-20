package colima

import (
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
)

func indexOf(t *testing.T, dockerfile, needle string) int {
	t.Helper()
	at := strings.Index(dockerfile, needle)
	if at < 0 {
		t.Fatalf("expected the Dockerfile to contain %q:\n%s", needle, dockerfile)
	}
	return at
}

// The order is the point of the whole change. Everything above the setup script
// is identical for every repository, so Docker's cache makes it a one-time cost
// per profile — and a script change rebuilds one layer instead of an image.
func TestRenderDockerfile_ShouldRunTheSetupScriptLast(t *testing.T) {
	// Arrange
	spec := agent_runtime.SandboxSpec{
		SetupScript: "#!/bin/sh\napt-get install -y sqlite3\n",
	}

	// Act
	dockerfile := renderDockerfile(spec)

	// Assert
	setup := indexOf(t, dockerfile, "COPY "+setupScriptName)
	for _, earlier := range []string{
		"apt-get install", "useradd", "opencode.ai/install", "opencode/bin/rg",
		"opencode.json", "safe.directory",
	} {
		if indexOf(t, dockerfile, earlier) > setup {
			t.Fatalf("expected %q above the setup script so its layer is cached", earlier)
		}
	}
	if indexOf(t, dockerfile, "USER "+guestUser) < setup {
		t.Fatal("expected the runtime stanza below the setup script")
	}
}

// The script is copied whether or not the repository has one, so the layer
// boundary does not move the first time a repository adopts one — which would
// invalidate every cached layer above it for no reason.
func TestRenderDockerfile_ShouldCopyTheScriptEvenWithoutOne(t *testing.T) {
	// Arrange, Act
	dockerfile := renderDockerfile(agent_runtime.SandboxSpec{})

	// Assert
	indexOf(t, dockerfile, "COPY "+setupScriptName)
	if content := setupScriptContent(""); !strings.Contains(content, "#!/bin/sh") {
		t.Fatalf("expected a runnable no-op script, got %q", content)
	}
}

func TestRenderDockerfile_ShouldPinTheAgentCLIAndAssertIt(t *testing.T) {
	// Arrange, Act: unpinned, the image freezes whatever was latest the day it
	// was built and OpenCode's output contract can shift under the adapter.
	dockerfile := renderDockerfile(agent_runtime.SandboxSpec{})

	// Assert
	indexOf(t, dockerfile, "--version "+opencodeVersion)
	indexOf(t, dockerfile, "grep -qF "+opencodeVersion)
}

// OpenCode looks for ripgrep only in this cache path and never falls back to
// $PATH, so the Ubuntu package is seeded before the container runs.
func TestRenderDockerfile_ShouldSeedTheRipgrepCache(t *testing.T) {
	// Arrange, Act
	dockerfile := renderDockerfile(agent_runtime.SandboxSpec{})

	// Assert
	indexOf(t, dockerfile, guestHome+"/.cache/opencode/bin/rg")
}

// Every mount arrives owned by 0:0 while the agent runs as an ordinary account,
// and git refuses a repository it does not own before reading anything at all.
func TestRenderDockerfile_ShouldExemptEveryMountFromGitOwnership(t *testing.T) {
	// Arrange, Act
	dockerfile := renderDockerfile(agent_runtime.SandboxSpec{})

	// Assert
	indexOf(t, dockerfile, "safe.directory '*'")
}

func TestRenderDockerfile_ShouldNotRunAsRoot(t *testing.T) {
	// Arrange, Act
	dockerfile := renderDockerfile(agent_runtime.SandboxSpec{})

	// Assert
	indexOf(t, dockerfile, "USER "+guestUser)
	indexOf(t, dockerfile, "WORKDIR "+guestWork)
}

func TestRenderDockerfile_ShouldHoldTheContainerOpenWithoutSleepInfinity(t *testing.T) {
	// Arrange, Act
	dockerfile := renderDockerfile(agent_runtime.SandboxSpec{})

	// Assert
	indexOf(t, dockerfile, `CMD ["tail", "-f", "/dev/null"]`)
	if strings.Contains(dockerfile, "sleep infinity") {
		t.Fatal("expected the container to stay open without sleep infinity")
	}
}

func TestRenderDockerfile_ShouldUseTheUbuntuRecipe(t *testing.T) {
	// Arrange, Act
	dockerfile := renderDockerfile(agent_runtime.SandboxSpec{})

	// Assert
	indexOf(t, dockerfile, "FROM "+ubuntuImage)
	indexOf(t, dockerfile, "apt-get install")
	indexOf(t, dockerfile, "docker.io")
	indexOf(t, dockerfile, "useradd --create-home")
	if strings.Count(dockerfile, "FROM ") != 1 {
		t.Fatalf("expected exactly one fixed base image:\n%s", dockerfile)
	}
}

// Docker runs inside the sandbox rather than through the profile daemon, so the
// image needs the rootless engine and its user-namespace networking support.
func TestRenderDockerfile_ShouldInstallRootlessDockerDependencies(t *testing.T) {
	// Arrange, Act
	dockerfile := renderDockerfile(agent_runtime.SandboxSpec{})

	// Assert
	for _, dependency := range []string{"docker.io", "rootlesskit", "slirp4netns", "uidmap"} {
		indexOf(t, dockerfile, dependency)
	}
	indexOf(t, dockerfile, "gomerge:100000:65536")
	if strings.Contains(dockerfile, "/var/run/docker.sock") {
		t.Fatal("expected no profile Docker socket in the sandbox image")
	}
}

// Credentials are staged per round, so they cannot be baked in — the init is
// the only place per-start work can happen.
func TestInitScript_ShouldStageCredentialsAndSignalReady(t *testing.T) {
	// Arrange, Act
	script := initScript()

	// Assert
	if !strings.Contains(script, guestCredentials+"/auth.json") {
		t.Fatalf("expected the staged credential to be installed:\n%s", script)
	}
	if !strings.Contains(script, readyMarker) {
		t.Fatalf("expected a readiness marker Start can wait on:\n%s", script)
	}
	if !strings.HasSuffix(strings.TrimSpace(script), `exec "$@"`) {
		t.Fatalf("expected the init to exec the container's command last:\n%s", script)
	}
}

func TestInitScript_ShouldWaitForNestedDockerBeforeSignallingReady(t *testing.T) {
	// Arrange, Act
	script := initScript()

	// Assert: a started container is not ready until its requested nested daemon
	// answers, otherwise the first Docker command races daemon startup.
	docker := indexOf(t, script, "docker info")
	ready := indexOf(t, script, ": > "+readyMarker)
	if docker > ready {
		t.Fatalf("expected nested Docker to be ready before the marker:\n%s", script)
	}
	indexOf(t, script, "GO_MERGE_INSTALL_DOCKER")
	indexOf(t, script, "rootlesskit")
	indexOf(t, script, "dockerd --host=\"$DOCKER_HOST\" --storage-driver=vfs")
}

func TestSetupScriptContent_ShouldPassARepositoryScriptThrough(t *testing.T) {
	// Arrange, Act, Assert
	script := "#!/bin/sh\napt-get install -y sqlite3\n"
	if got := setupScriptContent(script); got != script {
		t.Fatalf("expected the script unchanged, got %q", got)
	}
}
