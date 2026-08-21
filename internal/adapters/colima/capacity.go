package colima

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
)

// admissionFraction caps how much of the host's CPU and memory concurrently
// running Colima profiles may reserve. A profile is a full Linux VM, and this
// host runs them alongside everything else on the machine, so a project waking
// up must not be able to starve the rest of the system.
const admissionFraction = 0.5

// profileDiskGiB sizes a profile's disk. It is thin-provisioned and holds only
// images and container layers — checkouts and issue trees live on the host and
// are mounted in — so this is a ceiling rather than an allocation. Colima's own
// default of 100GiB is more ceiling than any project needs.
const profileDiskGiB = 60

// profileCPUs and profileMemoryGiB bound every project's VM. Sandboxes share
// these resources without per-container cgroup limits.
const (
	profileCPUs      = 4
	profileMemoryGiB = 8
	// sandboxMemoryGiB is the Docker hard limit assigned to one agent sandbox.
	// The admission controller uses the same value so it cannot admit more work
	// than the profile can safely sustain.
	sandboxMemoryGiB = 2
	// sandboxMemoryFraction reserves half the VM for nested Docker workloads and
	// the VM itself rather than letting agent containers fill it completely.
	sandboxMemoryFraction = 0.5
)

// profileSize is what every project's VM is created with. Host-level admission
// still stops profiles from consuming more than the configured host budget.
func profileSize() (cpus, memoryGiB int) {
	return profileCPUs, profileMemoryGiB
}

// FreeBytes reports the space left where profile disks are written.
//
// The Colima home is the right thing to measure: a profile's disk image is the
// largest single thing go-merge causes to be written, and it grows toward its
// allowance as images and layers accumulate inside it.
func (c *Client) FreeBytes() (int64, error) {
	path := colimaHome()
	// The directory does not exist until the first profile is created, and a
	// host with no profiles yet is not a host under pressure — measure the
	// volume it will be created on instead of reporting a failure.
	if _, err := os.Stat(path); err != nil {
		path = filepath.Dir(path)
	}
	if _, err := os.Stat(path); err != nil {
		path = os.TempDir()
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("read free space at %q: %w", path, err)
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

// reservation is one admitted round's share, held from the moment it is
// admitted until its round lets go.
type reservation struct {
	profile string
}

// Reserve admits one round against the host's VM budget.
//
// The host admits profiles: a profile costs its whole configured CPU and memory
// whether or not anything inside it is busy, so starting one for a project that
// has none running is what has to fit within admissionFraction of the machine.
//
// Reserving rather than merely reporting is what makes this budget hold under
// concurrent rounds. Starting a profile takes minutes and the provider does not
// list it until it is up, so rounds that only asked whether there was room
// would every one of them be told yes off the same snapshot.
//
// Reserve starts nothing. Ensure is where the profile actually boots, which is
// what keeps the slow work out of a call the scheduler makes for every eligible
// subject on every tick.
func (c *Client) Reserve(
	ctx context.Context, spec agent_runtime.SandboxSpec,
) (func(), bool, error) {
	profile := profileName(spec.Sandbox.ProjectID)
	// Listing shells out, so it stays off the lock. A concurrent round's own
	// reservation is in the ledger either way, and what this misses are
	// profiles no reservation covers — other people's Colima VMs, which were
	// only ever sampled at the moment of the check.
	instances, err := c.listProfiles(ctx)
	if err != nil {
		return nil, false, err
	}
	profileCPUs, profileMemoryGiB := profileSize()
	hostCPUs, hostMemory, err := hostBudget(ctx)
	if err != nil {
		return nil, false, err
	}

	c.admission.Lock()
	defer c.admission.Unlock()
	if _, held := c.reserved[spec.Sandbox.Name]; held {
		// This subject and role already have a round in flight. Sandbox names
		// are derived from the subject, so admitting a second would have two
		// rounds creating, starting and stopping the same container.
		return nil, false, nil
	}
	if !c.admitSandbox(profile) {
		return nil, false, nil
	}
	if !c.admitProfile(instances, profile, profileCPUs,
		int64(profileMemoryGiB)<<30, hostCPUs, hostMemory) {
		return nil, false, nil
	}
	if c.reserved == nil {
		c.reserved = map[string]reservation{}
	}
	c.reserved[spec.Sandbox.Name] = reservation{profile: profile}
	name := spec.Sandbox.Name
	return func() {
		c.admission.Lock()
		delete(c.reserved, name)
		c.admission.Unlock()
	}, true, nil
}

// admitSandbox keeps agent container limits below half the VM's memory. The
// remaining memory is available to the VM and workloads an agent starts through
// the profile's Docker socket.
func (c *Client) admitSandbox(profile string) bool {
	reservedMemoryGiB := 0
	for _, held := range c.reserved {
		if held.profile == profile {
			reservedMemoryGiB += sandboxMemoryGiB
		}
	}
	budgetGiB := int(float64(profileMemoryGiB) * sandboxMemoryFraction)
	return reservedMemoryGiB+sandboxMemoryGiB <= budgetGiB
}

// admitProfile decides whether this project's VM may be running, counting every
// Colima profile on the host — go-merge's own and otherwise, since they all
// compete for the same physical CPU and RAM.
func (c *Client) admitProfile(
	instances []profileInstance, profile string,
	profileCPUs int, profileMemory int64, hostCPUs int, hostMemory int64,
) bool {
	var cpus int
	var memory int64
	counted := map[string]struct{}{}
	for _, instance := range instances {
		if instance.Status != "Running" {
			continue
		}
		if instance.Name == profile {
			// Already up, and this round is not a second copy of it.
			return true
		}
		counted[instance.Name] = struct{}{}
		cpus += instance.CPUs
		memory += instance.Memory
	}
	// Profiles this process decided to start that colima cannot see yet. Held
	// reservations for a profile already counted above would double it.
	for _, held := range c.reserved {
		if _, running := counted[held.profile]; running || held.profile == profile {
			continue
		}
		counted[held.profile] = struct{}{}
		cpus += profileCPUs
		memory += profileMemory
	}
	return cpus+profileCPUs <= hostCPUs && memory+profileMemory <= hostMemory
}

// hostBudget returns the CPU core count and memory in bytes this host may
// commit to concurrently running Colima profiles.
func hostBudget(ctx context.Context) (cpus int, memory int64, err error) {
	total, err := totalMemory(ctx)
	if err != nil {
		return 0, 0, err
	}
	cpus = max(1, int(float64(runtime.NumCPU())*admissionFraction))
	memory = int64(float64(total) * admissionFraction)
	return cpus, memory, nil
}

func totalMemory(ctx context.Context) (int64, error) {
	if runtime.GOOS == "darwin" {
		// The probe goes through command() like every other subprocess here, so
		// its cancellation lands, and under its own small budget: a stuck
		// sysctl must fail the admission check, not hold it.
		probeCtx, cancel := context.WithTimeout(ctx, hostProbeTimeout)
		defer cancel()
		output, err := command(probeCtx, "sysctl", "-n", "hw.memsize").Output()
		if err != nil {
			return 0, fmt.Errorf("read host memory: %w", err)
		}
		return strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	}
	contents, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("read host memory: %w", err)
	}
	for line := range strings.SplitSeq(string(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "MemTotal:" {
			continue
		}
		kib, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse host memory: %w", err)
		}
		return kib * 1024, nil
	}
	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}
