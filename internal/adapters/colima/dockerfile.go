package colima

import (
	"strings"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
)

const (
	// guestUser and guestHome are fixed rather than taken from whoever runs
	// go-merge. Provisioning builds paths from the account name, and an image
	// shared between projects cannot depend on the host it was built on.
	//
	// The uid is deliberately whatever the base image assigns. Colima presents
	// a virtiofs mount to a container as owned by 0:0 and does not enforce it —
	// a process at any uid reads and writes through to the host user — so
	// matching the host's uid buys nothing. What it does cost is git, which
	// compares the repository's owner against its own euid and refuses; that is
	// what the safe.directory below answers.
	guestUser = "gomerge"
	guestHome = "/home/gomerge"

	// guestHostRoot is the root reserved for runtime-only host mounts. The
	// checkout's host-identity mount is the one exception to the normal guest
	// work roots, and it must remain the same absolute path on both sides.
	guestHostRoot = "/run/go-merge"

	// guestCredentials is where the run's staged auth.json is mounted, and
	// guestWork is where its code and issue tree are. Both are the paths the
	// application already names; see agent_runtime.GuestMountRoot.
	guestCredentials = guestHostRoot + "/credentials"
	guestWork        = agent_runtime.GuestMountRoot

	// readyMarker is written by the init once the credentials are in place.
	// Start waits for it, because docker start returns when the process is
	// spawned rather than when it has finished doing anything.
	readyMarker = "/tmp/go-merge-ready"

	// opencodeVersion pins the agent CLI baked into the image. Unpinned, the
	// image freezes whatever was latest the day it was built and OpenCode's CLI
	// and JSON output contract can shift under the adapter silently. The
	// version is part of the image tag, so bumping it here is what triggers a
	// rebuild.
	opencodeVersion = "1.18.18"

	// buildVersion keys the image by this adapter's own Dockerfile, which
	// nothing else in the tag covers: a host holding a cached image would
	// otherwise keep starting containers built by the recipe as it was, however
	// far renderDockerfile has since moved on. Bump it whenever the template
	// changes in a way existing hosts must pick up.
	buildVersion = 3

	// Ubuntu is pinned to its LTS release so an image build does not silently
	// move to another release.
	ubuntuImage = "ubuntu:24.04"
)

// renderDockerfile builds the recipe for one repository's agent image.
//
// The order of the layers is the whole point of moving a repository's setup
// script out of VM provisioning and into a build. Everything above the script
// is identical for every repository on the host, so Docker's cache makes it a
// one-time cost per profile; editing the script rebuilds one layer instead of
// the multi-gigabyte disk a golden image cost.
//
// The script is written into the context whether or not the repository has one
// — an absent script becomes a no-op — so there is one shape of Dockerfile
// rather than two, and the layer boundary does not move when a repository
// adopts a script for the first time.
func renderDockerfile(spec agent_runtime.SandboxSpec) string {
	var builder strings.Builder
	builder.WriteString("# syntax=docker/dockerfile:1\n")
	builder.WriteString("FROM " + ubuntuImage + "\n\n")
	builder.WriteString(packagesFor() + "\n\n")
	builder.WriteString(accountFor() + "\n\n")
	builder.WriteString(agentCLIStep() + "\n\n")
	builder.WriteString(ripgrepStep() + "\n\n")
	builder.WriteString(agentConfigStep() + "\n\n")
	builder.WriteString(safeDirectoryStep() + "\n\n")
	builder.WriteString(initStep() + "\n\n")
	builder.WriteString(setupScriptStep() + "\n\n")
	builder.WriteString(runtimeStep())
	return builder.String()
}

// packagesFor installs what a checkout needs before a repository's own script
// runs: a shell the OpenCode installer can use, git, curl, ripgrep, and the
// docker *client*.
//
// The client, not the engine. The daemon is the profile's, reached over the
// socket bound into the container, so a repository whose tests start containers
// gets them as siblings rather than through a nested engine — which is what
// makes this a package install instead of the cgroup and init-script work
// running dockerd inside a guest used to need.
func packagesFor() string {
	return `ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      bash git curl ca-certificates tar unzip ripgrep docker.io \
 && rm -rf /var/lib/apt/lists/*`
}

func accountFor() string {
	return "RUN useradd --create-home --home-dir " + guestHome +
		" --shell /bin/bash " + guestUser
}

// agentCLIStep installs the pinned OpenCode standalone binary. The official
// installer places it under the invoking user's home — the build runs as root —
// so it is copied onto the system PATH where the agent account finds it. No
// node or npm: the standalone build keeps the image smaller and the install
// surface down to one vendor artefact.
//
// The version is asserted here rather than probed at boot. It is part of the
// image tag, so a mismatch is impossible by the time a container starts; what
// the assertion catches is an installer that quietly served something else.
func agentCLIStep() string {
	// HOME is set for this RUN alone, not with ENV. The installer places the
	// binary under the invoking user's home and the build runs as root, but an
	// ENV would persist into the running container — where the agent is not
	// root, and every write it made to $HOME would land in root's home or be
	// refused outright.
	return "RUN HOME=/root; export HOME; " +
		"curl -fsSL https://opencode.ai/install | bash -s -- --version " + opencodeVersion + ` \
 && install -m 0755 /root/.opencode/bin/opencode /usr/local/bin/opencode \
 && opencode --version | grep -qF ` + opencodeVersion
}

// ripgrepStep puts the Ubuntu package's ripgrep where OpenCode expects to find
// the one it downloads. OpenCode checks only that path and never falls back to
// $PATH, so seeding the cache means it never downloads its own copy.
func ripgrepStep() string {
	return "RUN install -D -m 0755 \"$(command -v rg)\" " +
		guestHome + "/.cache/opencode/bin/rg"
}

// agentConfigStep grants the agent access to directories outside the one it
// runs from. OpenCode's own default is to ask, and "--auto" cannot override it:
// auto-approval is a catch-all rule and the ask is a more specific one that
// wins. A user config is merged after both, which is why this is a file.
//
// Only the permission is set. Which model and agent a round uses belongs to the
// round, and a config file here would silently outrank what the round asked for.
func agentConfigStep() string {
	return "RUN install -d -m 0755 " + guestHome + "/.config/opencode" + ` \
 && printf '%s\n' '{"permission":{"external_directory":"allow"}}' \
      > ` + guestHome + "/.config/opencode/opencode.json"
}

// safeDirectoryStep is not optional, and it is not about writable mounts.
//
// Colima presents every virtiofs mount to a container as owned by 0:0 while the
// agent runs as an ordinary account, and git refuses a repository whose owner
// is not its own euid — "detected dubious ownership" — before it will read so
// much as the current branch. That applies to the read-only clones as much as
// to the checkout being worked in, so the exemption is a wildcard rather than a
// list of the writable ones.
func safeDirectoryStep() string {
	return "RUN git config --system --add safe.directory '*'"
}

func initStep() string {
	// The session directory is created here, owned by the agent, because docker
	// seeds a new named volume from the image path it is mounted over —
	// ownership included. Left absent, docker would create it root-owned and
	// OpenCode could not write the conversation into it.
	return "COPY " + initScriptName + " /usr/local/bin/" + initScriptName + `
RUN chmod 0755 /usr/local/bin/` + initScriptName + ` \
 && install -d -m 0755 ` + sessionPath + ` \
 && chown -R ` + guestUser + ":" + guestUser + " " + guestHome
}

func setupScriptStep() string {
	return "# The repository's own customization, last: everything above is proven\n" +
		"# working first, and this is the only layer a script change invalidates.\n" +
		"COPY " + setupScriptName + " /tmp/" + setupScriptName + "\n" +
		"RUN sh /tmp/" + setupScriptName + " && rm -f /tmp/" + setupScriptName
}

// runtimeStep is what the container is when it is running: the agent account,
// the directory every mount lands in, and a process that does nothing.
//
// Nothing runs in the container until a round execs into it.
func runtimeStep() string {
	return "USER " + guestUser + "\n" +
		"WORKDIR " + guestWork + "\n" +
		// Explicit, because the agent reads its credentials and writes its
		// session through $HOME, and a container inherits whatever the build
		// left rather than the account's own.
		"ENV HOME=" + guestHome + "\n" +
		"ENV OPENCODE_DISABLE_AUTOUPDATE=1\n" +
		"ENTRYPOINT [\"/usr/local/bin/" + initScriptName + "\"]\n" +
		"CMD [\"tail\", \"-f\", \"/dev/null\"]\n"
}

const (
	initScriptName  = "gm-init"
	setupScriptName = "gm-setup.sh"
)

// initScript is the per-start work, as opposed to the per-build work above.
// Docker has no equivalent of a boot provisioning step, so anything that has to
// happen every time a container starts lives here.
//
// Credentials are the whole of it. They are staged per round into a read-only
// mount and copied into the place OpenCode reads them from, which cannot be
// baked into an image shared by every subject on the host.
func initScript() string {
	return `#!/bin/sh
set -eu
if [ -f ` + guestCredentials + `/auth.json ]; then
  install -d -m 700 "$HOME/.local/share/opencode"
  install -m 600 ` + guestCredentials + `/auth.json "$HOME/.local/share/opencode/auth.json"
fi
: > ` + readyMarker + `
exec "$@"
`
}

// setupScriptContent is what the repository asked for, or a no-op when it asked
// for nothing.
func setupScriptContent(script string) string {
	if strings.TrimSpace(script) == "" {
		return "#!/bin/sh\n# No repository setup script.\n"
	}
	return script
}
