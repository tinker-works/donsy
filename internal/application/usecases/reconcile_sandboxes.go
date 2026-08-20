package usecases

import (
	"context"
	"errors"
	"fmt"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"time"

	"github.com/tinker-works/donsy/internal/application"
)

type ReconcileSandboxesUseCase struct {
	registry  agent_runtime.AgentRegistry
	inspector agent_runtime.SandboxInspector
	sandboxes agent_runtime.SandboxManager
	// creds stages a sandbox's credentials and is what removes them again when the sandbox is
	// reclaimed. Nil when no agent runtime is configured.
	creds agent_runtime.AgentCredentials
	clock application.Clock
	// idleAfter reclaims a Stopped sandbox once it has sat unused this long, so idle
	// containers don't hold host disk indefinitely. Zero disables reclaim,
	// matching the "zero means unlimited/off" convention used by AgentProfile.MaxRounds.
	// EpicSandboxSpec always requests a stable sandbox identity per epic+role, and Ensure
	// recreates a sandbox from scratch whenever Inspect reports it Absent, so a reclaimed
	// Sandbox comes back automatically the next time its role is needed.
	//
	// It is the fallback rather than the main trigger: finished work is reclaimed by
	// ReconcileSandboxesCommand.Terminal as soon as it finishes, so what is left for this
	// clock is work that was abandoned instead of completed.
	idleAfter time.Duration
	// maxRuntime force-stops a sandbox the provider still reports Running this long after
	// the record says it started. Zero disables the cap. It catches what a round's own
	// deadline cannot: a sandbox left running by a process that died, or one started out of
	// band. Keep it longer than maxRoundDuration so the round's deadline is what
	// normally fires and this only ever meets genuine leftovers.
	maxRuntime time.Duration
	// disk reports free space, which shortens idleAfter when the host is close to
	// full. Nil leaves reclaim on its clock alone.
	disk agent_runtime.HostDisk
	// pressureBelow is the free space under which idleUnderPressure replaces
	// idleAfter. Zero disables the check, as does a nil disk.
	pressureBelow int64
	// idleUnderPressure is the idle window used while the host is under disk
	// pressure. It buys back the disk of work that was abandoned rather than
	// finished, at the cost of the OpenCode session a resumed round would have
	// continued — which is the cheaper of the two once builds start failing.
	idleUnderPressure time.Duration
	// host is the per-project machine the sandboxes live in. Nil when the
	// runtime has no such thing to stop.
	host agent_runtime.ProjectHost
	// hostIdleAfter is how long a project must have had nothing running before
	// its host is stopped. It is short — a host costs memory continuously and
	// comes back on its own — but not zero: on a five-second tick a project
	// running rounds back to back would otherwise stop and lazily restart its
	// host in every gap between them, and starting one costs tens of seconds.
	//
	// It is deliberately unrelated to idleAfter, which governs deleting a
	// sandbox. That is expensive and loses the session; this is neither.
	hostIdleAfter time.Duration
	// hostRunningAfter and hostStoppedAfter bound orphaned Docker workloads that
	// would otherwise prevent an idle VM from stopping forever.
	hostRunningAfter time.Duration
	hostStoppedAfter time.Duration
	// lastBusy is when each project was last seen with something running, and
	// hostReleased which projects have already had their host stopped since
	// then. Both are process-local on purpose: losing them on a restart costs
	// one redundant stop, which is cheaper than a record to keep in step.
	lastBusy     map[uint]time.Time
	hostReleased map[uint]struct{}
}

// idleWindow is how long a Stopped sandbox may sit before it is reclaimed. It is
// resolved once per reconciliation rather than per sandbox: the answer cannot change
// part way through, and a statfs per sandbox would be a syscall per instance for a
// number that moves only as sandboxes are deleted.
//
// A disk that cannot be read is treated as a disk under no pressure. Free space
// is an optimisation on top of the clock, and failing reconciliation — which is
// what corrects sandbox records after a crash — because a statfs failed would cost
// more than reclaiming late.
func (u *ReconcileSandboxesUseCase) idleWindow() time.Duration {
	if u.disk == nil || u.pressureBelow <= 0 || u.idleUnderPressure <= 0 {
		return u.idleAfter
	}
	free, err := u.disk.FreeBytes()
	if err != nil || free >= u.pressureBelow {
		return u.idleAfter
	}
	// Never longer than the clock already allows: pressure brings reclaim
	// forward, it does not postpone it.
	if u.idleAfter > 0 && u.idleUnderPressure > u.idleAfter {
		return u.idleAfter
	}
	return u.idleUnderPressure
}

// ReconcileSandboxesCommand refreshes local sandbox records after a crash or app downtime.
type ReconcileSandboxesCommand struct {
	ProjectID uint
	// Priority maps subjects the scheduler could run this tick to their selected
	// roles. Only those sandboxes are reconciled while work is pending; stale
	// sandbox maintenance resumes on a tick with nothing eligible to dispatch.
	Priority map[agent.AgentSubject]agent.AgentRole
	// Recover stalls every live run in the project before anything else. It is set
	// on the first tick of a process, where no run can belong to work in progress
	// because no round has started yet — the same argument reapOrphaned makes for
	// one subject, widened to the project. Without it a run record left Running by
	// a crash would shield its leaked sandbox from the check below forever, in the case
	// where its subject never needs another round.
	Recover bool
	// Terminal names the subjects that will never receive another round, so their
	// Sandboxes go now rather than waiting out idleAfter. See TerminalSubjects.
	Terminal map[agent.AgentSubject]struct{}
	// InFlight names the subjects whose rounds are running right now, and whose
	// Sandboxes are therefore off limits to every reclaim below.
	//
	// The runs read above are one snapshot, taken before the sandboxes are listed. A
	// round dispatched between those two reads does not appear in it, yet its sandbox
	// does — so judged on the snapshot alone the sandbox looks unowned and gets
	// force-stopped out from under a round that is starting on it. Reconciliation
	// runs on the scheduler's own goroutine, which is where this set is kept, so
	// what it says is current by construction.
	//
	// Empty means nothing is in flight, which is what the first tick of a process
	// sees and what Recover relies on.
	InFlight map[agent.AgentSubject]struct{}
}

// reconcileFailure keeps reconciliation's error compatible with callers while
// telling the scheduler which subjects remained unsafe to dispatch. A provider
// failure for one stale sandbox must not strand unrelated work in the project.
type reconcileFailure struct {
	err          error
	unreconciled map[agent.AgentSubject]struct{}
}

func (e reconcileFailure) Error() string { return e.err.Error() }

func (e reconcileFailure) Unwrap() error { return e.err }

// Handle refreshes every sandbox's status, force-stops the ones nothing owns, and reclaims
// the ones whose work is finished or that have sat idle past idleAfter, continuing past
// individual failures so that one sandbox the provider can no longer see (deleted
// out-of-band, provider crash, etc.) does not stop the rest of the project's sandboxes from
// being reconciled, tick after tick.
func (u *ReconcileSandboxesUseCase) Handle(
	ctx context.Context,
	command ReconcileSandboxesCommand,
) error {
	runs, err := u.registry.ListProjectAgentRuns(command.ProjectID)
	if err != nil {
		return err
	}
	if command.Recover {
		if err := stallLiveRuns(u.registry, u.clock, runs); err != nil {
			return err
		}
		// Nothing is live any more, so every sandbox below is judged unowned.
		runs = nil
	}
	live := liveRunsBySandbox(runs)
	sandboxes, err := u.registry.ListSandboxes(command.ProjectID)
	if err != nil {
		return err
	}
	sandboxes = prioritySandboxes(sandboxes, command.Priority)
	idleAfter := u.idleWindow()
	now := u.clock.Now().UTC()
	var errs []error
	unreconciled := map[agent.AgentSubject]struct{}{}
	// busy is set by the loop for any sandbox the provider still reports
	// running. It, and not the records, is what the host stop is decided from.
	busy := len(command.Priority) > 0
	for _, sandbox := range sandboxes {
		// Guarded before anything else, because every branch below can stop or
		// delete this sandbox: the unowned force-stop, the finished retire, the broken
		// reclaim and the idle reclaim alike. A round holds its sandbox for its whole
		// duration, and a subject in flight is owed all of them.
		if _, busy := command.InFlight[sandbox.Subject]; busy {
			continue
		}
		recorded := sandbox.Status
		status, err := u.inspector.Inspect(ctx, sandbox.Ref())
		if err != nil {
			errs = append(errs, fmt.Errorf("inspect sandbox %q: %w", sandbox.Name, err))
			unreconciled[sandbox.Subject] = struct{}{}
			continue
		}
		_, finished := command.Terminal[sandbox.Subject]
		var stopped bool
		if status == agent.SandboxStatusRunning {
			stopped, err = u.stopUnowned(ctx, sandbox, live[sandbox.ID], now, finished)
			if err != nil {
				errs = append(errs, err)
				unreconciled[sandbox.Subject] = struct{}{}
				continue
			}
			if stopped {
				status = agent.SandboxStatusStopped
				// UpdatedAt means "sat unused since" to the idle clock, and this sandbox
				// only just stopped. Leaving the timestamp at the moment it started
				// running would hand the idle branch below an age of hours and have
				// it delete the instance in the same pass that stopped it.
				sandbox.UpdatedAt = now
			} else {
				busy = true
			}
		}
		// The record must agree with the provider before any reclaim: the FSM only
		// permits Delete from Stopped or Broken, not from whatever stale status the
		// record last saw.
		if err := sandbox.Reconcile(status); err != nil {
			errs = append(errs, fmt.Errorf("reconcile sandbox %q: %w", sandbox.Name, err))
			unreconciled[sandbox.Subject] = struct{}{}
			continue
		}
		if finished {
			if err := u.retire(ctx, sandbox, now); err != nil {
				errs = append(errs, fmt.Errorf("reclaim finished sandbox %q: %w", sandbox.Name, err))
				unreconciled[sandbox.Subject] = struct{}{}
			}
			continue
		}
		// A broken instance never comes back on its own: Ensure only knows how
		// to start Stopped and reuse Running, so anything else would fail with a
		// name conflict on every future round, blocking the subject forever.
		// Deleting it here is what lets Ensure rebuild it from scratch next time.
		// The provider state is what decides — a sandbox the record calls Broken but
		// the provider reports Running is healthy and only needs its record fixed.
		if status == agent.SandboxStatusBroken {
			if err := u.reclaim(ctx, sandbox, now); err != nil {
				errs = append(errs, fmt.Errorf("reclaim broken sandbox %q: %w", sandbox.Name, err))
				unreconciled[sandbox.Subject] = struct{}{}
			}
			continue
		}
		if idleAfter > 0 && status == agent.SandboxStatusStopped &&
			now.Sub(sandbox.UpdatedAt) >= idleAfter {
			if err := u.reclaim(ctx, sandbox, now); err != nil {
				errs = append(errs, fmt.Errorf("reclaim idle sandbox %q: %w", sandbox.Name, err))
				unreconciled[sandbox.Subject] = struct{}{}
			}
			continue
		}
		// A force-stop is saved even when it left the status where the record already
		// had it, because it also restamped UpdatedAt — which is the idle clock.
		if recorded == status && !stopped {
			continue
		}
		sandbox.UpdatedAt = now
		if err := u.registry.SaveSandbox(sandbox); err != nil {
			errs = append(errs, fmt.Errorf("save sandbox %q: %w", sandbox.Name, err))
		}
	}
	// Passed the failures rather than appended to them first: a pass that could
	// not read every sandbox has not established that the project is quiet.
	if err := u.releaseHost(ctx, command, busy, now, errs); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return reconcileFailure{err: err, unreconciled: unreconciled}
	}
	return nil
}

func prioritySandboxes(
	sandboxes []agent.Sandbox,
	priority map[agent.AgentSubject]agent.AgentRole,
) []agent.Sandbox {
	if len(priority) == 0 {
		return sandboxes
	}
	selected := make([]agent.Sandbox, 0, len(priority))
	for _, sandbox := range sandboxes {
		if role, ok := priority[sandbox.Subject]; ok && sandbox.Role == role {
			selected = append(selected, sandbox)
		}
	}
	return selected
}

// releaseHost stops the project's container host on a tick where the whole
// project came out quiet.
//
// Every sandbox of a project shares one host, so the host outlives them: a
// project whose containers are all stopped is still paying for a running VM.
// Ensure starts it again on the next round, and stopping it persists its disk,
// so the images built inside it and the sessions in its stopped containers are
// all still there when it comes back.
//
// Three things have to hold, and the third is the one that is easy to miss.
// Nothing may be dispatched; nothing may be observed running; and this pass
// must not have failed to inspect anything, because a sandbox whose status
// could not be read may well be running, and stopping the host under it would
// kill a round nobody observed.
//
// None of this can race a round of this process. Reconciliation runs at the top
// of a tick, on the worker's single goroutine, before any dispatch — the same
// argument ReconcileSandboxesCommand.InFlight already makes — and a round
// dispatched later in the tick starts the host again through Ensure. Losing
// that race costs one wasted start, never a killed round.
func (u *ReconcileSandboxesUseCase) releaseHost(
	ctx context.Context, command ReconcileSandboxesCommand, busy bool, now time.Time,
	failures []error,
) error {
	if u.host == nil {
		return nil
	}
	if u.lastBusy == nil {
		u.lastBusy = map[uint]time.Time{}
	}
	if busy || len(command.InFlight) > 0 || len(failures) > 0 {
		u.lastBusy[command.ProjectID] = now
		delete(u.hostReleased, command.ProjectID)
		return nil
	}
	// Asked once per quiet period rather than once per tick. Stopping an
	// already-stopped host is harmless but not free — it shells out to read the
	// host's state — and a project nobody is working on would otherwise pay for
	// that every five seconds for as long as go-merge runs.
	if _, released := u.hostReleased[command.ProjectID]; released {
		return nil
	}
	quiet, seen := u.lastBusy[command.ProjectID]
	if !seen {
		// First sight of this project, and it is already quiet. Start its clock
		// rather than assuming it has been idle since the beginning of time.
		u.lastBusy[command.ProjectID] = now
		return nil
	}
	if now.Sub(quiet) < u.hostIdleAfter {
		return nil
	}
	if _, err := u.host.ReapExpiredContainers(
		ctx, command.ProjectID, now.Add(-u.hostRunningAfter), now.Add(-u.hostStoppedAfter),
	); err != nil {
		return fmt.Errorf("reap expired containers of project %d: %w", command.ProjectID, err)
	}
	stopped, err := u.host.StopProfile(ctx, command.ProjectID)
	if err != nil {
		return fmt.Errorf("stop host of project %d: %w", command.ProjectID, err)
	}
	if !stopped {
		return nil
	}
	if u.hostReleased == nil {
		u.hostReleased = map[uint]struct{}{}
	}
	u.hostReleased[command.ProjectID] = struct{}{}
	return nil
}

// stopUnowned force-stops a running sandbox that no round is legitimately using, and
// reports whether it did. Three things qualify: a sandbox with no live run at all, one
// running past maxRuntime — a round that will never report back — and one whose
// subject is finished, where no round can be owed anything.
//
// Reconciliation runs at the top of a tick, before any round, on the worker's single
// goroutine — so it never meets a round of its own process. The live-run check is what
// keeps that from being the only thing standing between a real round and a stop.
func (u *ReconcileSandboxesUseCase) stopUnowned(
	ctx context.Context,
	sandbox agent.Sandbox,
	owners []agent.AgentRun,
	now time.Time,
	finished bool,
) (bool, error) {
	overran := u.maxRuntime > 0 && now.Sub(sandbox.UpdatedAt) >= u.maxRuntime
	if len(owners) > 0 && !overran && !finished {
		return false, nil
	}
	if err := u.sandboxes.StopNow(ctx, sandbox.Ref()); err != nil {
		return false, fmt.Errorf("stop unowned sandbox %q: %w", sandbox.Name, err)
	}
	// A run still claiming a sandbox that just had its power cut is not going to
	// report back, and leaving it live would shield the sandbox again next tick.
	if err := stallLiveRuns(u.registry, u.clock, owners); err != nil {
		return false, fmt.Errorf("stall runs of sandbox %q: %w", sandbox.Name, err)
	}
	return true, nil
}

// retire reclaims a sandbox whose subject is finished. A provider that no longer has
// the instance leaves nothing to delete, so the record is simply brought in line.
func (u *ReconcileSandboxesUseCase) retire(
	ctx context.Context, sandbox agent.Sandbox, now time.Time,
) error {
	if sandbox.Status == agent.SandboxStatusAbsent {
		sandbox.UpdatedAt = now
		return u.registry.SaveSandbox(sandbox)
	}
	return u.reclaim(ctx, sandbox, now)
}

func (u *ReconcileSandboxesUseCase) reclaim(
	ctx context.Context, sandbox agent.Sandbox, now time.Time,
) error {
	if err := u.sandboxes.Delete(ctx, sandbox.Ref()); err != nil {
		return err
	}
	// The credentials were staged under the sandbox's name, so they go with it. Left
	// behind they are a real provider credential on disk for an instance that no
	// longer exists — the one copy nobody is watching.
	if u.creds != nil {
		if err := u.creds.Discard(sandbox.Name); err != nil {
			return err
		}
	}
	if err := sandbox.Apply(agent.SandboxEventDelete); err != nil {
		return err
	}
	if err := sandbox.Apply(agent.SandboxEventDeleteDone); err != nil {
		return err
	}
	sandbox.UpdatedAt = now
	return u.registry.SaveSandbox(sandbox)
}

// liveRunsBySandbox groups the runs that are still live by the sandbox they claim.
func liveRunsBySandbox(runs []agent.AgentRun) map[string][]agent.AgentRun {
	live := map[string][]agent.AgentRun{}
	for _, run := range runs {
		if isLiveAgentRunStatus(run.Status) {
			live[run.SandboxID] = append(live[run.SandboxID], run)
		}
	}
	return live
}
