package usecases

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

// errRunCancelled is what fail returns for a round somebody cancelled, so
// callers can tell a deliberate stop from a round that died on its own.
var errRunCancelled = errors.New("run was cancelled")

// ErrHostFailure marks a round the host never managed to run. The worker reads
// it to tell a subject whose agent keeps getting it wrong — which the round
// limit already handles — from one no machine on this host can currently serve,
// which needs the host looked at and the subject left alone until it is.
var ErrHostFailure = errors.New("host could not run the round")

const (
	// maxRoundDuration is a runaway guard, not a budget: a coding round that
	// runs the repository's own test suite can legitimately take a long time.
	// What it bounds is a round that will never return at all. Its own subject
	// would otherwise be stuck behind it forever — no further round on that epic
	// or issue, its sandbox never reclaimed, and its slot held against the host
	// budget for as long as the process lives.
	maxRoundDuration = 2 * time.Hour

	// maxRoundSilence bounds how long a round may produce nothing at all. It
	// exists because maxRoundDuration is the wrong instrument for a round that
	// is not slow but stopped: an agent waiting on an answer nobody will give it
	// writes no transcript, burns no CPU, and holds its sandbox — and with it a share
	// of the host budget that admits the next round — for two hours before the
	// runaway guard notices.
	//
	// It is generous rather than tight because silence is normal in the middle of
	// a long model call or a test suite that prints only at the end. What it
	// catches is silence no working round produces.
	maxRoundSilence = 15 * time.Minute

	// silenceInterval is how often the transcript is sampled for growth. It only
	// has to be fine enough that maxRoundSilence lands near its nominal value.
	silenceInterval = time.Minute

	// stopTimeout bounds stopping the sandbox after a round. The stop deliberately
	// survives cancellation (see invoke), and without a bound of its own a
	// runtime call that never returns would hold the whole process open instead.
	stopTimeout = 30 * time.Second
)

// agentRound owns the part of a round that does not depend on what the agent
// is working on: minting the run record, booting the sandbox, invoking the agent,
// and stopping the sandbox again. Epic-scoped and issue-scoped rounds differ only
// in their mounts, their prompt, and what they do with the answer.
type agentRound struct {
	registry   agent_runtime.AgentRegistry
	sandboxes  agent_runtime.SandboxManager
	runtime    agent_runtime.AgentRuntime
	builder    application.AgentCommandBuilder
	clock      application.Clock
	supervisor *RunSupervisor
	// output samples the round's transcript for the stall guard. It is optional:
	// without a configured reader the round keeps only its runaway guard, which
	// is what it had before.
	output agent_runtime.RunOutput
	// timeout bounds one invocation. Zero means maxRoundDuration; only tests
	// shorten it, so the production constructors say nothing about it.
	timeout time.Duration
	// silence and sample shorten the stall guard for tests, the same way timeout
	// shortens the runaway guard. Zero means the constants.
	silence time.Duration
	sample  time.Duration
}

func (r agentRound) roundTimeout() time.Duration {
	if r.timeout > 0 {
		return r.timeout
	}
	return maxRoundDuration
}

func (r agentRound) silenceTimeout() time.Duration {
	if r.silence > 0 {
		return r.silence
	}
	return maxRoundSilence
}

func (r agentRound) silenceSample() time.Duration {
	if r.sample > 0 {
		return r.sample
	}
	return silenceInterval
}

// watchForSilence cancels the round once its transcript has not grown for
// silenceTimeout. It returns a stop function and a flag the caller reads after
// the run to tell a stall apart from every other reason the context died.
//
// Size is sampled rather than tailed: growth is the whole signal, and a stat per
// minute costs nothing next to reading a transcript that can reach megabytes.
func (r agentRound) watchForSilence(
	cancel context.CancelFunc, runID string,
) (func(), *atomic.Bool) {
	stalled := &atomic.Bool{}
	if r.output == nil {
		return func() {}, stalled
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(r.silenceSample())
		defer ticker.Stop()
		var size int64
		quiet := time.Duration(0)
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			// An unreadable transcript is not evidence of a stall: the file does
			// not exist until the runtime creates it, and a round that has only
			// just started is the common case for that.
			current, err := r.output.Size(runID)
			if err != nil {
				continue
			}
			if current != size {
				size = current
				quiet = 0
				continue
			}
			if quiet += r.silenceSample(); quiet >= r.silenceTimeout() {
				stalled.Store(true)
				cancel()
				return
			}
		}
	}()
	return func() { close(done) }, stalled
}

// begin records a queued run, boots its sandbox, and marks the run started. What
// stops two rounds starting on one subject is EpicWorker.inflight — neither a
// lock here nor the order these records are written in.
// previous is the subject's run history, which decides whether this round may
// resume the last conversation or has to start a new one.
func (r agentRound) begin(
	ctx context.Context,
	spec *agent_runtime.SandboxSpec,
	profile agent.AgentProfile,
	round int,
	previous []agent.AgentRun,
) (agent.AgentRun, context.Context, func(), error) {
	now := r.clock.Now().UTC()
	run := agent.AgentRun{
		ID:        domain.MintULID(),
		ProjectID: spec.Sandbox.ProjectID,
		SandboxID: spec.Sandbox.ID,
		Role:      spec.Sandbox.Role,
		Subject:   spec.Sandbox.Subject,
		Engine:    agent.AgentEngineOpenCode,
		Agent:     profile.Agent,
		Variant:   profile.Variant,
		SessionMode: agent.SessionModeAfter(
			agent.AgentEngineOpenCode, spec.Sandbox.Role, previous,
		),
		Status:    agent.AgentRunStatusQueued,
		Round:     round,
		CreatedAt: now,
	}
	runCtx, release := r.supervisor.Begin(ctx, run.ID)
	finish := func() {
		release()
		r.supervisor.Complete(run.ID)
	}
	fail := func(err error) (agent.AgentRun, context.Context, func(), error) {
		finish()
		return run, nil, nil, err
	}
	if err := r.registry.SaveAgentRun(run); err != nil {
		return fail(err)
	}
	// Admitted is persisted before the sandbox is touched: Ensure can build an image
	// and create the instance, which takes minutes, and a run that stays Queued
	// that whole time reads in the UI as never having been picked up.
	if err := run.Apply(agent.AgentRunEventAdmit, now); err != nil {
		return fail(err)
	}
	if err := r.registry.SaveAgentRun(run); err != nil {
		return fail(err)
	}
	// Building an image and starting an instance are the host's job, not the
	// agent's. Whatever goes wrong here would go wrong for every subject on the
	// machine, so it is recorded as a host failure rather than spent as one of this
	// role's attempts.
	created, err := r.sandboxes.Ensure(runCtx, *spec)
	if err != nil {
		return fail(r.hostFailed(run, err))
	}
	// SessionModeAfter answered from the run history alone, which cannot see that
	// the instance holding that conversation was reclaimed and rebuilt. This is
	// the machine saying so. Left uncorrected the round would pass --continue to
	// an engine with nothing to continue, and — worse — render the prompt that
	// only states what is new, leaving the agent a task without the context it
	// names.
	if created && run.SessionMode == agent.SessionModeContinue {
		run.SessionMode = agent.SessionModeFresh
		if err := r.registry.SaveAgentRun(run); err != nil {
			return fail(err)
		}
	}
	if err := r.saveSandboxStatus(spec, agent.SandboxStatusStopped); err != nil {
		return fail(err)
	}
	if err := r.sandboxes.Start(runCtx, spec.Sandbox.Ref()); err != nil {
		return fail(r.hostFailed(run, err))
	}
	if err := r.saveSandboxStatus(spec, agent.SandboxStatusRunning); err != nil {
		return fail(err)
	}
	if err := run.Apply(agent.AgentRunEventStart, r.clock.Now().UTC()); err != nil {
		return fail(err)
	}
	if err := r.registry.SaveAgentRun(run); err != nil {
		return fail(err)
	}
	return run, runCtx, finish, nil
}

// invoke runs the agent and stops the sandbox again, whatever the outcome. It
// returns the extracted answer; the raw output is only kept as a run error.
// The run is a pointer so the usage parsed from the output lands on the record
// the caller goes on to persist.
func (r agentRound) invoke(
	ctx context.Context,
	spec *agent_runtime.SandboxSpec,
	run *agent.AgentRun,
	prompt string,
	environment map[string]string,
) (string, error) {
	argv, err := r.builder.Command(application.AgentInvocation{
		Run: *run, Prompt: prompt, Environment: environment,
	})
	if err != nil {
		return "", r.fail(*run, err)
	}
	runCtx, expire := context.WithTimeout(ctx, r.roundTimeout())
	runCtx, interrupt := context.WithCancel(runCtx)
	unwatch, stalled := r.watchForSilence(interrupt, run.ID)
	output, runErr := r.runtime.Run(runCtx, spec.Sandbox.Ref(), run.ID, argv)
	unwatch()
	interrupt()
	expire()
	// A round either guard cut off reports whatever the runtime made of being
	// killed, which names neither the reason nor its length. Nobody asked for
	// these stops, so they stay failures rather than becoming cancellations.
	switch {
	case runErr != nil && stalled.Load():
		runErr = fmt.Errorf(
			"round produced no output for %s and was stopped: %w", r.silenceTimeout(), runErr,
		)
	case runErr != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded):
		runErr = fmt.Errorf("round exceeded %s and was stopped: %w", r.roundTimeout(), runErr)
	}
	// Usage is summed whatever the outcome: a failed round still burned the
	// tokens it reports, and hiding that would understate what a retry costs.
	run.Usage = r.builder.ParseUsage(output)
	// Stopping must survive every cancellation: runCtx is already dead for a
	// cancelled round, and at process shutdown the outer ctx is the cancelled
	// worker context itself — CommandContext would then never even launch
	// the stop, leaving the real sandbox running after go-merge exits. It is
	// bounded all the same, so a runtime call that hangs cannot hold the quit open.
	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancelStop()
	stop := r.sandboxes.Stop
	if ctx.Err() != nil {
		// The process is on its way out. A graceful guest shutdown costs seconds
		// somebody is sitting through for nothing: this round's work is already
		// lost, and the container is started again from its image when it is next used.
		stop = r.sandboxes.StopNow
	}
	stopErr := stop(stopCtx, spec.Sandbox.Ref())
	// A failed stop leaves the real sandbox in an unknown state; recording Stopped
	// anyway would let the next round Start it as if nothing happened. Broken
	// keeps it out of idle reclaim and gets re-inspected by reconciliation,
	// which is what corrects the record to whatever the provider reports.
	status := agent.SandboxStatusStopped
	if stopErr != nil {
		status = agent.SandboxStatusBroken
	}
	if err := r.saveSandboxStatus(spec, status); err != nil {
		return "", err
	}
	// The engine returning non-zero means no answer came back at all — the sandbox
	// died, a guard stopped it, the API refused, the process crashed. None of
	// those are the agent's verdict on the work, so none of them spend one of
	// the role's attempts. What does spend one is the agent finishing and being
	// wrong: an unmarked answer, or a plan with no issues in it. Those come back
	// through fail from the callers, which is where the output is judged.
	if runErr != nil {
		run.Error = strings.TrimSpace(output)
		return "", r.hostFailed(*run, runErr)
	}
	// The agent finished; only stopping its sandbox did not. The round's work is
	// already in hand, so this must not cost an attempt at a subject it just
	// completed.
	if stopErr != nil {
		return "", r.hostFailed(*run, stopErr)
	}
	return r.builder.ExtractAnswer(output), nil
}

// succeed runs after the round's outcome landed in the tracker store, so a
// crash between the two writes loses only this record: the next start's
// reapOrphaned then marks the run stalled even though its work was published
// and the epic moved on. That is a cosmetic blemish on the run history, not a
// loss of work — which is why the tracker store is written first, never the
// other way around.
func (r agentRound) succeed(run agent.AgentRun) error {
	// A cancel can land after the runtime returned but before its release ran;
	// there is nothing left to stop, so drop the mark instead of leaking it.
	r.supervisor.Forget(run.ID)
	if err := run.Apply(agent.AgentRunEventSucceed, r.clock.Now().UTC()); err != nil {
		return err
	}
	return r.registry.SaveAgentRun(run)
}

// fail records the run as failed and returns the original cause, so a caller
// can `return r.fail(run, err)` without losing what went wrong.
//
// A round somebody cancelled is recorded as cancelled instead. The round only
// ever sees a context error, which on its own is indistinguishable from the
// runtime dying, so the supervisor is what tells the two apart.
// hostFailed ends a round the host never managed to run. The record is kept so
// the failure stays visible, but it does not spend one of the role's attempts —
// see agent.AgentRun.CountsTowardRoundLimit.
func (r agentRound) hostFailed(run agent.AgentRun, cause error) error {
	err := r.end(run, cause, agent.AgentRunEventHostFail)
	// Only a round that actually ended host-failed is marked: end downgrades a
	// cancelled round, and somebody cancelling is not the host failing.
	if run.Status == agent.AgentRunStatusHostFailed && err != nil {
		return fmt.Errorf("%w: %w", ErrHostFailure, err)
	}
	return err
}

func (r agentRound) fail(run agent.AgentRun, cause error) error {
	return r.end(run, cause, agent.AgentRunEventFail)
}

func (r agentRound) end(run agent.AgentRun, cause error, event agent.AgentRunEvent) error {
	if r.supervisor.WasCancelled(run.ID) {
		event = agent.AgentRunEventCancel
		r.supervisor.Forget(run.ID)
		cause = errRunCancelled
	}
	// A caller may have already stored the agent's raw output on the run; a
	// bare cause like "exit status 1" must not erase the only diagnostic there is.
	if run.Error == "" {
		run.Error = cause.Error()
	} else {
		run.Error = cause.Error() + "\n\n" + run.Error
	}
	if transitionErr := run.Apply(event, r.clock.Now().UTC()); transitionErr != nil {
		return transitionErr
	}
	if saveErr := r.registry.SaveAgentRun(run); saveErr != nil {
		return saveErr
	}
	return cause
}

// saveSandboxStatus records what a provider operation just made of the sandbox. It is a
// reconcile, not a transition: Ensure and Start report where the instance
// landed, not which FSM edges it crossed to get there.
func (r agentRound) saveSandboxStatus(
	spec *agent_runtime.SandboxSpec, status agent.SandboxStatus,
) error {
	if err := spec.Sandbox.Reconcile(status); err != nil {
		return err
	}
	spec.Sandbox.UpdatedAt = r.clock.Now().UTC()
	return r.registry.SaveSandbox(spec.Sandbox)
}

func isLiveAgentRunStatus(status agent.AgentRunStatus) bool {
	return status == agent.AgentRunStatusQueued || status == agent.AgentRunStatusAdmitted ||
		status == agent.AgentRunStatusRunning
}

// reapOrphaned marks any still-live run as stalled.
//
// A run still Queued/Admitted/Running when Handle is entered cannot belong to
// this process: the worker never dispatches a subject that already has a round
// in flight, so no round of ours can be live for this subject while another one
// is starting. What is left is a leftover from a process that quit or crashed
// mid-run, whose sandbox may have died unobserved. Reaping it is what stops the
// subject being stuck forever behind a run nothing will ever finish.
//
// See EpicWorker.inflight, which is what makes that true — the run record
// cannot, because this function stalls live runs rather than standing down.
func (r agentRound) reapOrphaned(runs []agent.AgentRun) error {
	return stallLiveRuns(r.registry, r.clock, runs)
}

// stallLiveRuns marks every still-live run in runs as stalled. Both callers have
// established that none of them can belong to work in progress; what differs is
// how — reapOrphaned reasons about one subject, reconciliation about a process
// that has not run a round yet.
func stallLiveRuns(
	registry agent_runtime.AgentRegistry, clock application.Clock, runs []agent.AgentRun,
) error {
	now := clock.Now().UTC()
	for _, run := range runs {
		if !isLiveAgentRunStatus(run.Status) {
			continue
		}
		run.Error = "run did not complete before the process restarted"
		if err := run.Apply(agent.AgentRunEventStall, now); err != nil {
			return err
		}
		if err := registry.SaveAgentRun(run); err != nil {
			return err
		}
	}
	return nil
}

// nextRound is the round number the next attempt at a subject takes.
//
// A round the host never ran does not advance it, so its number is handed to
// the retry instead: the record of the failure is kept and visible, but the
// role's allowance is spent only on attempts the agent actually got. Two runs
// can therefore share a round number, which is what "attempt 2 at round 5"
// looks like when the first attempt never reached an agent.
func nextRound(runs []agent.AgentRun) int {
	round := 1
	for _, run := range runs {
		if !run.CountsTowardRoundLimit() {
			continue
		}
		if run.Round >= round {
			round = run.Round + 1
		}
	}
	return round
}
