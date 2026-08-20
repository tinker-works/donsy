package colima

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A profile is one Colima VM, and go-merge gives every project its own.
//
// The alternative — one VM for the whole host — was rejected for isolation:
// every project's VM has its own Docker daemon and the nested daemon used by a
// round is confined to that round's container. Scoping the VM to the project
// bounds what the runtime itself can reach.
//
// The cost is that Docker's layer cache lives inside each VM, so the first
// round of each project pays a full image build.
const profilePrefix = "gm-"

// profileName is the Colima profile one project's containers live in.
//
// Lima builds "<colima home>/_lima/colima-<profile>/ssh.sock.<16 digits>" and
// refuses anything reaching UNIX_PATH_MAX, which leaves roughly 36 characters
// for the profile name, and the name must match
// "^[A-Za-z0-9]+([._-][A-Za-z0-9]+)*$". A project ID keeps this far inside both
// bounds and — unlike anything derived from the project's name — cannot drift
// when the project is renamed.
func profileName(projectID uint) string {
	return profilePrefix + strconv.FormatUint(uint64(projectID), 10)
}

// colimaHome is where Colima keeps its profiles, each with the docker socket
// this adapter talks to. COLIMA_HOME is honoured because a caller that set it
// for colima means it for us too.
func colimaHome() string {
	if home := os.Getenv("COLIMA_HOME"); home != "" {
		return home
	}
	return filepath.Join(os.Getenv("HOME"), ".colima")
}

// dockerHost addresses one profile's daemon.
//
// It is passed to every docker invocation as --host rather than set in the
// environment or selected with --context. The context colima creates is deleted
// again by `colima stop`, and if it happened to be the active one that also
// clears the user's own currentContext — so go-merge never reads or writes the
// user's docker configuration at all.
func dockerHost(profile string) string {
	return "unix://" + filepath.Join(colimaHome(), profile, "docker.sock")
}

func vmType() string {
	return vmTypeFor(runtime.GOOS)
}

// vmTypeFor picks Apple's hypervisor on macOS and QEMU elsewhere. It cannot be
// changed after a profile is created: colima warns, keeps the old value, and
// still exits zero.
func vmTypeFor(goos string) string {
	if goos == "darwin" {
		return "vz"
	}
	return "qemu"
}

// profileInstance is one line of `colima list --json`. That output is NDJSON —
// one object per line, not an array — and is empty when no profile exists.
type profileInstance struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	CPUs   int    `json:"cpus"`
	Memory int64  `json:"memory"`
}

// maxListAttempts allows the listing one retry. A read is safe to repeat and
// sits on the hot path of Inspect and Reserve; the operations that mutate get
// no retry here — their safe retry is the worker's next tick.
const maxListAttempts = 2

// listProfiles reads every Colima profile on the host, go-merge's own and
// otherwise. `colima status` is not used for this: it exits non-zero for a
// profile that is stopped and for one that was never created, with the same
// message, so it cannot tell the two apart.
func (c *Client) listProfiles(ctx context.Context) ([]profileInstance, error) {
	var output []byte
	var err error
	for attempt := 0; attempt < maxListAttempts; attempt++ {
		output, err = c.boundedOutput(ctx, listTimeout, "colima", "list", "--json")
		if err == nil || ctx.Err() != nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("list Colima profiles: %w", err)
	}
	var instances []profileInstance
	for line := range strings.SplitSeq(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var instance profileInstance
		if err := json.Unmarshal([]byte(line), &instance); err != nil {
			// Dropping the line instead would silently remove a profile from
			// both status inspection and the admission budget, letting Reserve
			// over-admit onto a host that is already full. A line this code
			// cannot read means the colima contract changed, and then every
			// number derived from the listing is suspect.
			return nil, fmt.Errorf("parse colima list line %q: %w", line, err)
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func (c *Client) profileRunning(ctx context.Context, profile string) (bool, error) {
	instances, err := c.listProfiles(ctx)
	if err != nil {
		return false, err
	}
	for _, instance := range instances {
		if instance.Name == profile {
			return instance.Status == "Running", nil
		}
	}
	return false, nil
}

// lockProfile serializes everything that touches one profile's VM: starting it,
// preparing it, and building images inside it.
//
// Rounds run concurrently and every one of those collides with itself. Two
// rounds that both find the profile stopped would each run `colima start` on
// it; two that both miss an image would build it twice under the same tag. A
// project's first epic round and first issue round arrive together, so this is
// the ordinary case rather than a rare one.
func (c *Client) lockProfile(profile string) func() {
	c.profileMu.Lock()
	lock, ok := c.profiles[profile]
	if !ok {
		lock = &sync.Mutex{}
		c.profiles[profile] = lock
	}
	c.profileMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// ensureProfile brings the project's VM up, lazily, and returns its name.
//
// The same flag set is passed on every start rather than only at creation.
// Colima persists them (--save-config defaults on) and inherits anything
// omitted from the file it wrote last time, so passing them all is what keeps a
// profile somebody edited by hand from staying edited.
func (c *Client) ensureProfile(ctx context.Context, projectID uint) (string, error) {
	profile := profileName(projectID)
	unlock := c.lockProfile(profile)
	defer unlock()
	return profile, c.startProfileLocked(ctx, profile)
}

func (c *Client) startProfileLocked(ctx context.Context, profile string) error {
	running, err := c.profileRunning(ctx, profile)
	if err != nil {
		return err
	}
	if running && c.prepared[profile] {
		return nil
	}
	if !running {
		cpus, memory := profileSize()
		args := []string{
			"start", "--profile", profile,
			// Without this colima makes its own docker context the active one, and
			// every `docker` the user types afterwards talks to an agent's VM.
			"--activate=false",
			"--runtime", "docker",
			"--vm-type", vmType(),
			"--cpus", strconv.Itoa(cpus),
			"--memory", strconv.Itoa(memory),
			"--disk", strconv.Itoa(profileDiskGiB),
			"--mount-type", "virtiofs",
			// One mount, and it replaces colima's default of the whole home
			// directory writable. Everything a sandbox binds — the credentials, the
			// issue trees, the checkouts — lives under this one root, and the guest
			// sees it at the same absolute path.
			"--mount", c.hostRoot + ":w",
		}
		if err := c.bounded(ctx, profileStartTimeout, "colima", args...); err != nil {
			return fmt.Errorf("start Colima profile %q: %w", profile, err)
		}
		// A newly started VM always needs preparation, even if this Client
		// prepared the profile during an earlier run.
		c.prepared[profile] = false
	}
	if !c.prepared[profile] {
		if err := c.prepareProfile(ctx, profile); err != nil {
			return err
		}
		c.prepared[profile] = true
	}
	if running {
		return nil
	}
	return c.pruneOrphanContainers(ctx, profile)
}

// StopProfile releases a project's VM once nothing of the project is running.
//
// The container check is this adapter's own, on top of the caller's: the sweep
// decides from a snapshot taken at the top of a tick, and only the daemon can
// say what is running now. A profile that is already down, or was never
// created, is not an error — the caller is allowed to ask repeatedly.
func (c *Client) StopProfile(ctx context.Context, projectID uint) (bool, error) {
	profile := profileName(projectID)
	unlock := c.lockProfile(profile)
	defer unlock()
	running, err := c.profileRunning(ctx, profile)
	if err != nil || !running {
		return false, err
	}
	output, err := c.dockerOutput(ctx, profile, listTimeout, "ps", "--quiet")
	if err != nil {
		return false, err
	}
	if len(bytes.TrimSpace(output)) > 0 {
		return false, nil
	}
	if err := c.bounded(ctx, profileStopTimeout, "colima", "stop", "--profile", profile); err != nil {
		return false, err
	}
	c.prepared[profile] = false
	return true, nil
}

// ReapExpiredContainers clears leftovers that can keep an otherwise idle VM
// alive indefinitely. Named volumes have no age filter, so unused ones are
// removed whenever this quiet-host cleanup runs.
func (c *Client) ReapExpiredContainers(
	ctx context.Context, projectID uint, runningBefore, stoppedBefore time.Time,
) (bool, error) {
	profile := profileName(projectID)
	unlock := c.lockProfile(profile)
	defer unlock()
	running, err := c.profileRunning(ctx, profile)
	if err != nil || !running {
		return false, err
	}
	reaped := false
	output, err := c.dockerOutput(ctx, profile, listTimeout,
		"ps", "--quiet", "--filter", "status=running")
	if err != nil {
		return false, err
	}
	for id := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if id == "" {
			continue
		}
		output, err := c.dockerOutput(ctx, profile, listTimeout,
			"inspect", "--format", "{{.State.StartedAt}}", id)
		if err != nil {
			return false, err
		}
		started, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(output)))
		if err != nil {
			return false, fmt.Errorf("parse start time of container %s: %w", id, err)
		}
		if !started.Before(runningBefore) {
			continue
		}
		if err := c.docker(ctx, profile, listTimeout, "kill", id); err != nil {
			return false, err
		}
		reaped = true
	}
	if err := c.docker(ctx, profile, listTimeout,
		"system", "prune", "--all", "--volumes", "--force",
		"--filter", "until="+stoppedBefore.Format(time.RFC3339Nano)); err != nil {
		return false, err
	}
	if err := c.docker(ctx, profile, listTimeout, "volume", "prune", "--all", "--force"); err != nil {
		return false, err
	}
	return reaped, nil
}

// DeleteProfile removes a forgotten project's VM and everything in it.
//
// --force because delete otherwise prompts on stdin, and --data because without
// it colima leaves the container data disk behind: a full disk image per
// project the host has ever registered, which nothing will ever name again.
func (c *Client) DeleteProfile(ctx context.Context, projectID uint) error {
	profile := profileName(projectID)
	unlock := c.lockProfile(profile)
	defer unlock()
	if err := c.bounded(
		ctx, profileDeleteTimeout, "colima", "delete", "--profile", profile, "--force", "--data",
	); err != nil {
		return err
	}
	delete(c.prepared, profile)
	return c.forgetProjectRecords(projectID)
}
