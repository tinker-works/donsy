package colima

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

// Client runs agent sandboxes as Docker containers inside a Colima profile per
// project.
//
// The profile is started lazily, on the first operation that needs it, and the
// sweep stops it again once the project has nothing running — so a machine with
// four registered projects is not holding four VMs for the one that is busy.
type Client struct {
	runner commandRunner
	// logDir holds one transcript per run, named after the run.
	logDir string
	// hostRoot is the single host directory mounted into every profile. Every
	// path a sandbox binds lives beneath it.
	hostRoot string
	// stateDir holds the ledger: one record per container. See ledger.go for
	// why the adapter needs host-side state at all.
	stateDir string
	// buildDir holds the image build contexts, which are three small files
	// each and are removed after the build.
	buildDir string

	// profileMu guards profiles, the per-profile locks themselves.
	profileMu sync.Mutex
	profiles  map[string]*sync.Mutex
	// prepared records profiles whose daemon configuration and firewall have
	// completed successfully during this process. A failed preparation can
	// leave a VM running, so the next Ensure must not mistake that for ready.
	prepared map[string]bool

	// admission guards reserved, the CPU and memory this process has admitted
	// but the provider does not report as running yet. See Reserve.
	admission sync.Mutex
	reserved  map[string]reservation

	// limit overrides every per-operation budget when set. Zero means the
	// constants apply; only tests set it, to make a wedged CLI observable in
	// milliseconds instead of minutes.
	limit time.Duration
}

// NewClient creates a client that drives colima and docker on the local host.
//
// hostRoot is passed whole rather than as the individual directories a sandbox
// binds, because it is what gets mounted into the profile: one mount, replacing
// colima's default of the user's entire home.
func NewClient(logDir, hostRoot, stateDir, buildDir string) *Client {
	return &Client{
		runner: execRunner{}, logDir: logDir, hostRoot: hostRoot,
		stateDir: stateDir, buildDir: buildDir,
		profiles: map[string]*sync.Mutex{}, prepared: map[string]bool{},
	}
}

func newClient(runner commandRunner, dirs ...string) *Client {
	client := &Client{
		runner: runner, profiles: map[string]*sync.Mutex{}, prepared: map[string]bool{},
	}
	for index, target := range []*string{
		&client.logDir, &client.hostRoot, &client.stateDir, &client.buildDir,
	} {
		if index < len(dirs) {
			*target = dirs[index]
		}
	}
	return client
}

func (c *Client) within(budget time.Duration) time.Duration {
	if c.limit > 0 {
		return c.limit
	}
	return budget
}

// bounded runs one operation under its own budget, and says so when the budget
// is what stopped it — the bare "signal: killed" a timed-out subprocess reports
// names neither the operation nor the limit.
func (c *Client) bounded(
	ctx context.Context, budget time.Duration, name string, args ...string,
) error {
	opCtx, cancel := context.WithTimeout(ctx, c.within(budget))
	defer cancel()
	err := c.runner.Run(opCtx, name, args...)
	return c.explainDeadline(ctx, opCtx, budget, name, args, err)
}

func (c *Client) boundedOutput(
	ctx context.Context, budget time.Duration, name string, args ...string,
) ([]byte, error) {
	opCtx, cancel := context.WithTimeout(ctx, c.within(budget))
	defer cancel()
	output, err := c.runner.Output(opCtx, name, args...)
	return output, c.explainDeadline(ctx, opCtx, budget, name, args, err)
}

func (c *Client) explainDeadline(
	ctx, opCtx context.Context, budget time.Duration, name string, args []string, err error,
) error {
	if err != nil && errors.Is(opCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		operation := name
		if len(args) > 0 {
			operation += " " + args[0]
		}
		return fmt.Errorf("%s exceeded its %s budget: %w", operation, c.within(budget), err)
	}
	return err
}

// docker runs one docker command against a named profile's daemon.
//
// The --host flag rather than an environment variable or a context: it keeps
// the whole truth of which daemon was addressed in the argv, which is what a
// test can assert, and it never touches the user's docker configuration.
func (c *Client) docker(
	ctx context.Context, profile string, budget time.Duration, args ...string,
) error {
	return c.bounded(ctx, budget, "docker", dockerArgs(profile, args)...)
}

func (c *Client) dockerOutput(
	ctx context.Context, profile string, budget time.Duration, args ...string,
) ([]byte, error) {
	return c.boundedOutput(ctx, budget, "docker", dockerArgs(profile, args)...)
}

func dockerArgs(profile string, args []string) []string {
	return append([]string{"--host", dockerHost(profile)}, args...)
}

// Ensure brings up whatever the round needs and reports whether the sandbox had
// to be created.
//
// "Created" is answered from the session volume rather than from the container.
// The container is recreated whenever the image or the spec moves — a new
// OpenCode pin, an edited setup script, a repository added to an epic — and
// none of those should cost the agent the conversation it was in the middle of.
// What genuinely ends a session is the volume being new, which happens on a
// first round and after a reclaim.
func (c *Client) Ensure(ctx context.Context, spec agent_runtime.SandboxSpec) (bool, error) {
	if err := spec.Validate(); err != nil {
		return false, err
	}
	if err := validateMountLocations(spec.Mounts); err != nil {
		return false, err
	}
	name := spec.Sandbox.Name
	profile := profileName(spec.Sandbox.ProjectID)
	unlock := c.lockProfile(profile)
	defer unlock()
	if err := c.startProfileLocked(ctx, profile); err != nil {
		return false, err
	}
	image, err := c.ensureImage(ctx, profile, spec)
	if err != nil {
		return false, err
	}
	fingerprint := specFingerprint(spec, image)
	existing, err := c.containerStatus(ctx, profile, name)
	if err != nil {
		return false, err
	}
	if existing != agent.SandboxStatusAbsent {
		matches, err := c.fingerprintMatches(ctx, profile, name, fingerprint)
		if err != nil {
			return false, err
		}
		if matches {
			return false, nil
		}
		// The binds, the limits or the image have moved, and docker bakes all
		// of them in at creation. Replacing the container is the only way the
		// change reaches this subject; the session volume is deliberately left
		// alone, so the conversation survives.
		if err := c.docker(ctx, profile, deleteTimeout, "rm", "--force", name); err != nil {
			return false, err
		}
	}
	created, err := c.ensureSessionVolume(ctx, profile, name)
	if err != nil {
		return false, err
	}
	// Written before the container exists, never after: a record with nothing
	// behind it is harmless, while a container no record names is one nothing
	// can find again.
	if err := c.writeRecord(name, record{
		Profile: profile, Image: image, Spec: fingerprint,
	}); err != nil {
		return false, err
	}
	if err := c.docker(ctx, profile, createTimeout, createArgs(spec, image)...); err != nil {
		return false, err
	}
	return created, nil
}

// ensureSessionVolume reports whether it had to create the volume, which is
// what Ensure answers "created" from.
func (c *Client) ensureSessionVolume(
	ctx context.Context, profile, name string,
) (bool, error) {
	volume := sessionVolume(name)
	if err := c.docker(ctx, profile, listTimeout, "volume", "inspect", volume); err == nil {
		return false, nil
	}
	return true, c.docker(ctx, profile, createTimeout, "volume", "create", volume)
}

func (c *Client) Start(ctx context.Context, ref agent.SandboxRef) error {
	profile, err := c.ensureProfile(ctx, ref.ProjectID)
	if err != nil {
		return err
	}
	// A container can already be running if go-merge quit or crashed after
	// starting it but before recording that. Skip the redundant call instead of
	// disturbing one that may still be finishing an agent run.
	status, err := c.containerStatus(ctx, profile, ref.Name)
	if err != nil {
		return err
	}
	if status != agent.SandboxStatusRunning {
		if err := c.docker(ctx, profile, startTimeout, "start", ref.Name); err != nil {
			return err
		}
	}
	return c.waitReady(ctx, profile, ref.Name)
}

// waitReady holds the start open until the container's init has staged the
// round's credentials.
//
// docker start returns once the process is spawned, not once it has done
// anything. Without this the first agent command races the credential install
// and comes back as an authentication error that reads like the agent failing
// rather than the sandbox not being ready.
func (c *Client) waitReady(ctx context.Context, profile, name string) error {
	deadline := time.Now().Add(c.within(readyTimeout))
	for {
		err := c.docker(ctx, profile, listTimeout, "exec", name, "test", "-f", readyMarker)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("sandbox %q did not become ready within %s: %w",
				name, c.within(readyTimeout), err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readyPoll):
		}
	}
}

// readyPoll is how often waitReady asks. The init writes its marker in
// milliseconds, so this only ever costs one interval.
const readyPoll = 200 * time.Millisecond

func (c *Client) Stop(ctx context.Context, ref agent.SandboxRef) error {
	return c.stopContainer(ctx, ref, "stop", "--time", "10")
}

// StopNow cuts the container's power instead of asking it to shut down. The
// container holds nothing worth draining: the round's deliverable is already on
// the host through its mounts, and the session is on a volume.
func (c *Client) StopNow(ctx context.Context, ref agent.SandboxRef) error {
	return c.stopContainer(ctx, ref, "kill")
}

// stopContainer is a no-op when the profile is down, which is the honest
// answer: everything in a stopped VM is already stopped, and starting one in
// order to stop a container inside it would undo the sweep.
func (c *Client) stopContainer(
	ctx context.Context, ref agent.SandboxRef, args ...string,
) error {
	profile := profileName(ref.ProjectID)
	running, err := c.profileRunning(ctx, profile)
	if err != nil || !running {
		return err
	}
	status, err := c.containerStatus(ctx, profile, ref.Name)
	if err != nil || status != agent.SandboxStatusRunning {
		return err
	}
	return c.docker(ctx, profile, stopTimeout, append(args, ref.Name)...)
}

// Delete removes the container, its session volume, and the record naming
// them.
//
// With the profile down only the record goes. The container is then garbage
// inside a VM nobody is using, and pruneOrphanContainers collects it the next
// time that profile starts — which is much cheaper than booting a VM in order
// to delete something from it.
func (c *Client) Delete(ctx context.Context, ref agent.SandboxRef) error {
	profile := profileName(ref.ProjectID)
	running, err := c.profileRunning(ctx, profile)
	if err != nil {
		return err
	}
	if running {
		if err := c.docker(
			ctx, profile, deleteTimeout, "rm", "--force", "--volumes", ref.Name,
		); err != nil {
			return err
		}
		if err := c.docker(
			ctx, profile, deleteTimeout, "volume", "rm", "--force", sessionVolume(ref.Name),
		); err != nil {
			return err
		}
	}
	return c.removeRecord(ref.Name)
}

// Inspect reports the sandbox's status without ever starting its profile. See
// ledger.go: an Inspect that woke a VM would undo the sweep on the next tick.
func (c *Client) Inspect(
	ctx context.Context, ref agent.SandboxRef,
) (agent.SandboxStatus, error) {
	held, exists, err := c.readRecord(ref.Name)
	if err != nil {
		return "", err
	}
	if !exists {
		return agent.SandboxStatusAbsent, nil
	}
	running, err := c.profileRunning(ctx, held.Profile)
	if err != nil {
		return "", err
	}
	if !running {
		return agent.SandboxStatusStopped, nil
	}
	return c.containerStatus(ctx, held.Profile, ref.Name)
}

// containerStatus asks the daemon, and requires the profile to be up.
func (c *Client) containerStatus(
	ctx context.Context, profile, name string,
) (agent.SandboxStatus, error) {
	output, err := c.dockerOutput(ctx, profile, listTimeout,
		"inspect", "--format", "{{.State.Status}}", name)
	if err != nil {
		if isNoSuchContainer(err, output) {
			return agent.SandboxStatusAbsent, nil
		}
		return "", err
	}
	return sandboxStatusFor(name, strings.TrimSpace(string(output))), nil
}

// sandboxStatusFor maps a docker state onto the domain's.
//
// An unrecognised state is read as Broken, which is the safe reading — but it
// costs the sandbox, because reconciliation deletes Broken ones. A docker
// upgrade that renames a state must not do that silently.
func sandboxStatusFor(name, state string) agent.SandboxStatus {
	switch state {
	case "running":
		return agent.SandboxStatusRunning
	case "created", "exited", "paused":
		return agent.SandboxStatusStopped
	case "restarting":
		return agent.SandboxStatusStarting
	case "dead", "removing":
		return agent.SandboxStatusBroken
	default:
		slog.Warn("unrecognised docker container state",
			"sandbox", name, "state", state)
		return agent.SandboxStatusBroken
	}
}

func isNoSuchContainer(err error, output []byte) bool {
	text := strings.ToLower(err.Error() + " " + string(output))
	return strings.Contains(text, "no such object") ||
		strings.Contains(text, "no such container")
}

func (c *Client) fingerprintMatches(
	ctx context.Context, profile, name, fingerprint string,
) (bool, error) {
	output, err := c.dockerOutput(ctx, profile, listTimeout, "inspect",
		"--format", "{{index .Config.Labels \"go-merge.spec\"}}", name)
	if err != nil {
		if isNoSuchContainer(err, output) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(output)) == fingerprint, nil
}

// pruneOrphanContainers removes the containers this adapter created and then
// lost track of, which is how a Delete against a stopped profile finishes.
//
// It runs on profile start, where the daemon is up anyway and nothing is racing
// it: the caller holds the profile lock and no round can have been dispatched
// against a profile that was not running.
func (c *Client) pruneOrphanContainers(ctx context.Context, profile string) error {
	output, err := c.dockerOutput(ctx, profile, listTimeout, "ps", "--all",
		"--filter", "label="+managedLabel, "--format", "{{.Names}}")
	if err != nil {
		return err
	}
	recorded, err := c.recordedNames(profile)
	if err != nil {
		return err
	}
	var orphans []string
	for line := range strings.SplitSeq(string(output), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if _, held := recorded[name]; !held {
			orphans = append(orphans, name)
		}
	}
	if len(orphans) == 0 {
		return nil
	}
	slog.Info("removing orphaned agent containers", "profile", profile, "count", len(orphans))
	return c.docker(ctx, profile, deleteTimeout,
		append([]string{"rm", "--force", "--volumes"}, orphans...)...)
}

// Run executes one command in an already-running container.
//
// A cancelled context kills the docker CLI and its process group, but that does
// not reach the process inside the container — docker exec does not propagate
// it. What actually stops the agent is the Stop the round always performs
// afterwards, which is why Stop must not be reduced to a no-op for a running
// container.
func (c *Client) Run(
	ctx context.Context, ref agent.SandboxRef, runID string, argv []string,
) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("agent command is required")
	}
	profile := profileName(ref.ProjectID)
	args := dockerArgs(profile, append(
		[]string{"exec", "--workdir", guestWork, ref.Name}, argv...,
	))
	if c.logDir == "" {
		output, err := c.runner.Output(ctx, "docker", args...)
		return string(output), err
	}
	stdout, stderr, err := c.openRunLogs(runID)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = stdout.Close()
		_ = stderr.Close()
	}()
	output, err := c.runner.OutputTo(ctx, stdout, stderr, "docker", args...)
	return string(output), err
}

// openRunLogs names the transcript after the run rather than after the sandbox
// and a timestamp. A run ID is already unique and is known to every caller, so
// the path stays computable from an AgentRun alone — nothing has to store it.
func (c *Client) openRunLogs(runID string) (*os.File, *os.File, error) {
	if err := os.MkdirAll(c.logDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create agent log directory: %w", err)
	}
	// Checked before sanitising, not after: safeLogName falls back to "agent"
	// rather than returning empty, so testing its output let a run with no ID
	// through to a shared log file instead of reporting the caller bug.
	if runID == "" {
		return nil, nil, fmt.Errorf("agent run ID is required to name its log")
	}
	prefix := filepath.Join(c.logDir, safeLogName(runID))
	stdout, err := os.OpenFile(prefix+".stdout.jsonl", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("create agent stdout log: %w", err)
	}
	stderr, err := os.OpenFile(prefix+".stderr.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_ = stdout.Close()
		return nil, nil, fmt.Errorf("create agent stderr log: %w", err)
	}
	return stdout, stderr, nil
}

func safeLogName(name string) string {
	var builder strings.Builder
	for _, character := range name {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "agent"
	}
	return builder.String()
}

var _ agent_runtime.SandboxManager = (*Client)(nil)
var _ agent_runtime.SandboxInspector = (*Client)(nil)
var _ agent_runtime.AgentRuntime = (*Client)(nil)
var _ agent_runtime.ProjectHost = (*Client)(nil)
