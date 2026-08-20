package colima

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
)

// managedLabel marks a container as one go-merge created, which is what lets
// orphan pruning tell ours from anything else running in the profile.
const managedLabel = "go-merge.managed"

// containerMemoryLimit is also used by admission control in capacity.go. CPU
// remains unconstrained so a sandbox can use idle capacity.
const containerMemoryLimit = "2g"

// sessionVolumePrefix names the volume holding one subject's OpenCode session.
//
// The session outlives the container on purpose. A container's writable layer
// would survive a stop, but not a recreate — and a recreate is exactly what an
// OpenCode bump or a changed setup script causes. Keeping the conversation on a
// volume means a rebuilt toolchain no longer costs the agent its memory of what
// it was doing, which is a strict improvement on the golden image, where any
// change to the image key orphaned the VM.
//
// It covers "storage" and not its parent: auth.json is that directory's sibling
// and must not outlive the round on a volume nobody is watching.
const (
	sessionVolumePrefix = "gm-session-"
	sessionPath         = guestHome + "/.local/share/opencode/storage"
)

func sessionVolume(name string) string {
	return sessionVolumePrefix + name
}

// createArgs builds the docker create for one sandbox.
//
// Everything that cannot be changed on an existing container is decided here —
// the binds above all — which is why the result is fingerprinted and the
// fingerprint is what Ensure compares against.
func createArgs(spec agent_runtime.SandboxSpec, image, socketGID string) []string {
	args := []string{
		"create",
		"--name", spec.Sandbox.Name,
		"--hostname", spec.Sandbox.Name,
		"--label", managedLabel + "=1",
		"--label", "go-merge.spec=" + specFingerprint(spec, image),
		"--memory", containerMemoryLimit,
		"--volume", sessionVolume(spec.Sandbox.Name) + ":" + sessionPath,
	}
	for _, mount := range mountArgs(spec) {
		args = append(args, "--volume", mount)
	}
	if spec.InstallDocker {
		// The profile's own daemon, so a repository's test suite can start
		// containers. They are siblings of this one rather than children: there
		// is no engine in here, only the client.
		//
		// This is a real widening. A round can reach every container in its
		// project's profile and mount any path the VM can see. The boundary is
		// the profile, which is why there is one per project.
		args = append(args, "--volume", "/var/run/docker.sock:/var/run/docker.sock")
		if socketGID != "" {
			// The socket is root:docker and the agent is neither. The group's
			// numeric ID is assigned when the VM is built, so it is probed
			// rather than assumed.
			args = append(args, "--group-add", socketGID)
		}
	}
	return append(args, image)
}

// mountArgs renders the spec's mounts as docker binds, read-only unless the
// spec says otherwise.
func mountArgs(spec agent_runtime.SandboxSpec) []string {
	mounts := make([]string, 0, len(spec.Mounts))
	for _, mount := range spec.Mounts {
		bind := mount.HostLocation + ":" + mount.GuestLocation
		if !mount.Writable {
			bind += ":ro"
		}
		mounts = append(mounts, bind)
	}
	return mounts
}

// validateMountLocations keeps ordinary mounts inside the paths the container
// runtime owns. A host-identity mount is the narrow exception: Docker in the VM
// resolves its bind source against that same absolute path, so the checkout is
// deliberately mounted there read-only as well as at /work/repo.
func validateMountLocations(mounts []agent_runtime.SandboxMount) error {
	for _, mount := range mounts {
		guest := filepath.Clean(mount.GuestLocation)
		if withinGuestRoot(guest, agent_runtime.GuestMountRoot) ||
			withinGuestRoot(guest, guestHostRoot) ||
			guest == filepath.Clean(mount.HostLocation) {
			continue
		}
		return fmt.Errorf("sandbox mount point %q is outside guest work roots", mount.GuestLocation)
	}
	return nil
}

func withinGuestRoot(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// specFingerprint covers everything docker bakes into a container at creation
// and cannot change afterwards.
//
// Ensure recreates when it moves, which is what makes a changed spec reach an
// existing subject. The Lima adapter had no equivalent — it returned early for
// any instance that existed — so adding a repository to an epic left its
// refiner permanently without the new mount.
func specFingerprint(spec agent_runtime.SandboxSpec, image string) string {
	parts := []string{
		"image=" + image,
		"docker=" + strconv.FormatBool(spec.InstallDocker),
		"memory=" + containerMemoryLimit,
	}
	// Sorted, because the order the application happens to append mounts in is
	// not a difference worth recreating a container over.
	binds := mountArgs(spec)
	sort.Strings(binds)
	parts = append(parts, "mounts="+strings.Join(binds, ","))
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return fmt.Sprintf("%x", sum)[:16]
}
