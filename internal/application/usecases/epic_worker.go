package usecases

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

// IssueLoop holds the execution-side use cases: cutting branches, running
// coding and review rounds, and closing an epic once everything is merged.
// It is optional — a worker without it drives drafting only, which is what
// the loop did before the coding roles existed.
type IssueLoop struct {
	GetEpic          *GetEpicUseCase
	OpenPullRequests *OpenPullRequestsUseCase
	RunIssueAgent    *RunIssueAgentUseCase
	CompleteEpic     *CompleteEpicUseCase
	ReviewApproved   *ReviewApprovedBranchesUseCase
}

func (l IssueLoop) ready() bool {
	return l.GetEpic != nil && l.OpenPullRequests != nil &&
		l.RunIssueAgent != nil && l.CompleteEpic != nil &&
		l.ReviewApproved != nil
}

// approvedBranchSweepInterval is how often the worker re-checks branches that
// are approved and waiting to be merged. It is deliberately slower than the
// tick: every other step of a tick reads the store, while this one fetches from
// the remote for each approved branch, and base does not move often enough to
// be worth paying that on every pass.
//
// It is a constant rather than a parameter for the same reason
// idleSandboxReclaimAfter is: the cadence follows from what the sweep costs, not
// from anything a caller knows better.
const approvedBranchSweepInterval = time.Minute

// subjectKey identifies one subject's round across the whole worker.
//
// It carries the project because agent.AgentSubject is only a kind and an ID,
// which two projects can repeat. It deliberately leaves out the role: a subject
// has exactly one eligible role at a time, and keying on the subject alone is
// what makes "this subject is busy" true regardless of which role is running.
type subjectKey struct {
	project uint
	subject agent.AgentSubject
}

// roundResult is what a finished round hands back to the scheduler.
type roundResult struct {
	key    subjectKey
	epicID string
	err    error
}

// EpicWorker continuously advances configured epics without UI interaction.
//
// It is a scheduler and nothing else: Tick decides what is eligible and hands
// each subject to a goroutine of its own, rather than running the round itself.
// A round blocks for as long as the agent takes - minutes, and up to
// maxRoundDuration - so running them inline meant one subject at a time across
// every project, with reconciliation, reclamation and every other epic waiting
// behind whichever round was slowest.
//
// Everything below is owned by the goroutine in Run. Rounds report back on
// results rather than touching any of it, which is what keeps the fields that
// were never guarded from needing to be.
type EpicWorker struct {
	projects  application.ProjectRegistry
	listEpics *ListEpicsUseCase
	reconcile *ReconcileSandboxesUseCase
	run       *RunEpicAgentUseCase
	// purge reclaims the host-side copies of finished work. It is separate from
	// reconcile because it is not about sandboxes: the clones and checkouts an epic
	// accumulates outlive every sandbox built from them. Nil disables it.
	purge    *PurgeFinishedWorkUseCase
	issues   IssueLoop
	clock    application.Clock
	interval time.Duration
	// lastSweep is when the approved branches were last fetched. Run drives
	// Tick from one goroutine, so this needs no guarding.
	lastSweep time.Time
	// recovered records that the first tick has run its recovery pass. Runs left
	// live by a process that died are only recognisable as leftovers before this
	// one has started a round of its own, so the pass happens once and never again.
	// Run drives Tick from one goroutine, so this needs no guarding either.
	recovered bool
	// failures is the last reported message per subject. A condition that
	// persists across ticks (a missing role profile, a wedged sandbox) would
	// otherwise be reported every interval, drowning the log in one line; it is
	// reported when it appears and again only when it changes.
	//
	// It is per subject rather than one string because rounds now finish
	// whenever they finish: a single joined message would differ from tick to
	// tick purely by which rounds happened to be included, and never match the
	// previous one. Run drives Tick from one goroutine, so this needs no
	// guarding either.
	failures map[subjectKey]string
	// inflight is the subjects with a round running right now, and the epic each
	// belongs to.
	//
	// It is the only thing that stops two rounds starting on one subject. The
	// run record cannot do it: Handle reads the subject's runs and reaps the
	// live ones as orphans rather than standing down, which was correct only
	// while rounds were sequential and a live run therefore had to be a
	// leftover. That reasoning still holds, but it is this set that makes it
	// hold - a subject in here is never dispatched again, so a live run Handle
	// meets still cannot be one of ours.
	//
	// It also keeps reconciliation and purging off the sandboxes and directories of a
	// round in progress. Entries are added before the goroutine starts and
	// removed when its result is drained.
	inflight map[subjectKey]string
	// retries holds the subjects whose last round failed and when each may run
	// again. See retryBase. Run drives Tick from one goroutine: no guarding.
	retries map[subjectKey]retry
	// results carries finished rounds back. It is buffered so a round that ends
	// while Tick is busy does not block waiting to be heard.
	results chan roundResult
	// reconcileBudget is reconcileTimeout unless a test shortens it to make a
	// hanging provider observable in milliseconds instead of a minute.
	reconcileBudget time.Duration
	// rounds bounds how many rounds are dispatched at once, underneath the host
	// budget that SandboxManager.Reserve enforces. Reserve shells out to the provider
	// on every call, so without a ceiling every eligible subject would spawn a
	// goroutine and a subprocess only to be turned away.
	rounds chan struct{}
	// running tracks the round goroutines so Run can wait for them. Quitting
	// without that wait closes the registry under rounds that are still
	// persisting their terminal status.
	running sync.WaitGroup
}

// reconcileTimeout bounds one project's reconciliation. Reconciliation shells
// out to the provider per sandbox, and Tick visits projects in sequence: without a
// bound, one hung provider call would stall every project after it for as long
// as the process lives. When it fires, the remaining sandboxes fail fast with a
// context error, the tick moves on, and the next tick redoes what this one did
// not reach.
const reconcileTimeout = time.Minute

// retryBase and retryCap pace a subject whose round failed. The worker ticks
// every few seconds, so without a hold a systemic failure — a full disk, a
// wedged provider — is retried a dozen times a minute for as long as the
// process lives, a subprocess and a log line each time. Consecutive failures
// double the hold up to retryCap; a success clears it. Backpressure (the
// concurrency ceiling, a full host) never arms it: being turned away is not
// failing.
//
// The cap is an hour because nothing downstream ends the retrying any more. A
// round the host could not run does not spend one of the role's attempts (see
// agent.AgentRun.CountsTowardRoundLimit), which is what stops one bad disk from
// failing every epic on the machine — but it also means a subject on a broken
// host retries for as long as the process lives. At this cap that settles into
// a cheap heartbeat that recovers on its own within the hour once the host is
// fixed, rather than a sandbox creation attempt every few minutes forever.
const (
	retryBase = 15 * time.Second
	retryCap  = time.Hour
)

// hostFailureBreaker trips a subject that the host has failed to run this many
// times in a row, and breakerCooldown is how long it then waits.
//
// A host failure spends none of the role's attempts, which is what keeps one
// bad disk from failing every epic on the machine — but it also removes the
// only thing that ever stopped a subject retrying. Most host failures are worth
// retrying through: a disk fills and is reclaimed, an image build loses a race.
// What this catches is the kind that never resolves on its own — a model with
// no tool-capable endpoint, credentials that no longer work — where retrying on
// the hour forever is a sandbox boot per subject per hour and no way to notice.
//
// It stays half-open rather than latching: the cooldown expires, one round goes
// through, and either it succeeds and clears the ledger or it trips again. A
// breaker that had to be reset by hand would turn every transient host problem
// into one.
const (
	hostFailureBreaker = 5
	breakerCooldown    = 6 * time.Hour
)

// retry is when a failed subject may be dispatched again, and how many
// consecutive failures set that. Run drives Tick from one goroutine, so this
// needs no guarding — the same argument failures and inflight make.
type retry struct {
	failures int
	until    time.Time
	// hostFailures counts consecutive rounds the host could not run. It is
	// separate from failures because the two mean opposite things: a failure is
	// the agent getting it wrong, which the round limit ends, while a host
	// failure costs the role nothing and so has nothing that ends it.
	hostFailures int
}

// maxConcurrentRounds is the ceiling on dispatched rounds. It is a backstop
// rather than the real limit: SandboxManager.Reserve turns a round away when the
// host has no room, and on any host that is the smaller number. What this
// prevents is the pile of goroutines that would otherwise queue up behind it.
const maxConcurrentRounds = 8

// NewEpicWorker takes the issue loop by value; its zero value means the worker
// drives epic drafting only.
func NewEpicWorker(
	projects application.ProjectRegistry,
	listEpics *ListEpicsUseCase,
	reconcile *ReconcileSandboxesUseCase,
	run *RunEpicAgentUseCase,
	clock application.Clock,
	interval time.Duration,
	issues IssueLoop,
	purge *PurgeFinishedWorkUseCase,
) *EpicWorker {
	return &EpicWorker{
		projects: projects, listEpics: listEpics, reconcile: reconcile, run: run,
		clock: clock, interval: interval, issues: issues, purge: purge,
		failures:        map[subjectKey]string{},
		inflight:        map[subjectKey]string{},
		retries:         map[subjectKey]retry{},
		reconcileBudget: reconcileTimeout,
		results:         make(chan roundResult, maxConcurrentRounds),
		rounds:          make(chan struct{}, maxConcurrentRounds),
	}
}

// Run schedules rounds until ctx is cancelled, then waits for the ones already
// dispatched. The wait is what lets a round record its terminal status before
// the caller closes the registry underneath it; the caller bounds how long it
// is willing to wait for that.
func (w *EpicWorker) Run(ctx context.Context, report func(error)) {
	slog.Info("epic worker started")
	defer slog.Info("epic worker stopped")
	defer w.running.Wait()
	w.reportChanged(report, w.Tick(ctx))
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case result := <-w.results:
			// Drained here as well as at the top of a tick so a subject becomes
			// eligible again as soon as its round ends, rather than waiting out
			// however much of the interval is left.
			w.finish(report, result)
		case <-ticker.C:
			w.reportChanged(report, w.Tick(ctx))
		}
	}
}

// TickAndWait dispatches a tick and waits for every round it started, reporting
// their failures together with the tick's own. It is what a caller uses when it
// needs the tick's effects to have happened by the time it returns.
func (w *EpicWorker) TickAndWait(ctx context.Context) error {
	failures := []error{w.Tick(ctx)}
	for len(w.inflight) > 0 {
		result := <-w.results
		w.record(result)
		if result.err != nil {
			failures = append(failures, result.err)
		}
	}
	w.running.Wait()
	return errors.Join(failures...)
}

// finish records a drained round and frees its subject for the next tick.
func (w *EpicWorker) finish(report func(error), result roundResult) {
	w.record(result)
	err := result.err
	// Reported on the round that trips it, not on every one after: reportSubject
	// dedupes identical text, and the repeats carry the same cause as the trip.
	// Saying it once is what turns an hourly retry nobody reads into a line that
	// names the subject, the count and the wait.
	if w.retries[result.key].hostFailures == hostFailureBreaker {
		err = fmt.Errorf(
			"the host failed to run this subject %d times running; holding it for %s: %w",
			hostFailureBreaker, breakerCooldown, err,
		)
	}
	w.reportSubject(report, result.key, err)
}

// record drains one finished round out of the in-flight set and books its
// outcome into the retry ledger. Every path that forgets a round goes through
// here: one drained at the top of a tick would otherwise dodge its backoff.
func (w *EpicWorker) record(result roundResult) {
	delete(w.inflight, result.key)
	if result.err == nil {
		delete(w.retries, result.key)
		return
	}
	held := w.retries[result.key]
	held.failures++
	delay := retryBase << (held.failures - 1)
	if delay <= 0 || delay > retryCap {
		delay = retryCap
	}
	// A host failure the agent never got past keeps its own count, and enough of
	// them in a row hold the subject for a good deal longer than the ordinary
	// cap. A round that reached the agent — right or wrong — says the host is
	// working and clears it.
	if errors.Is(result.err, ErrHostFailure) {
		held.hostFailures++
		if held.hostFailures >= hostFailureBreaker {
			delay = breakerCooldown
		}
	} else {
		held.hostFailures = 0
	}
	held.until = w.clock.Now().Add(delay)
	w.retries[result.key] = held
}

// reportChanged forwards a tick's own failure. Round failures are reported
// through reportSubject, which dedupes per subject.
func (w *EpicWorker) reportChanged(report func(error), err error) {
	w.reportSubject(report, subjectKey{}, err)
}

// reportSubject forwards a failure only when it differs from the last one for
// that subject, and rearms once the subject succeeds so a recurrence is
// reported again.
func (w *EpicWorker) reportSubject(report func(error), key subjectKey, err error) {
	if err == nil {
		delete(w.failures, key)
		return
	}
	if w.failures[key] == err.Error() {
		return
	}
	w.failures[key] = err.Error()
	report(err)
}

// Tick reconciles external sandbox state then advances each eligible epic by one role round.
// One failing project or epic must not stop the sweep: a single misconfigured epic would
// otherwise stall every other epic in every project for as long as it exists.
func (w *EpicWorker) Tick(ctx context.Context) error {
	// Drained first so this tick sees the rounds that ended since the last one
	// as finished: reconciliation and purging below are handed the in-flight set,
	// and a subject left in it needlessly keeps its sandbox and directories reserved
	// for another interval.
	w.drain()
	projects, err := w.projects.List()
	if err != nil {
		return err
	}
	// Whether to fetch is decided once for the whole tick rather than per epic,
	// so every epic is swept on the same beat instead of the first one through
	// the loop consuming the interval for the rest.
	sweep := w.dueForSweep()
	recovering := !w.recovered
	w.recovered = true
	var failures []error
	for _, project := range projects {
		// The epics are read before reconciliation rather than after, because
		// reconciliation needs to know which subjects are finished to reclaim their
		// Sandboxes. A read that fails costs this project its reconcile for one tick,
		// which the next tick redoes.
		epics, err := w.listEpics.Handle(ListEpicsQuery{Project: project})
		if err != nil {
			failures = append(failures, fmt.Errorf("list epics for project %q: %w", project.Name, err))
			continue
		}
		// The deadline is on reconciliation's ctx only, never on the tick's own:
		// that ctx flows into every round dispatched below and lives as long as
		// the round does.
		priority := eligibleSubjects(epics)
		reconcileCtx, cancel := context.WithTimeout(ctx, w.reconcileBudget)
		err = w.reconcile.Handle(reconcileCtx, ReconcileSandboxesCommand{
			ProjectID: project.ID,
			Recover:   recovering,
			Terminal:  TerminalSubjects(epics),
			InFlight:  w.inflightSubjects(project.ID),
			Priority:  priority,
		})
		cancel()
		unreconciled := map[agent.AgentSubject]struct{}{}
		if err != nil {
			failures = append(failures, fmt.Errorf("reconcile project %q: %w", project.Name, err))
			var failure reconcileFailure
			if errors.As(err, &failure) {
				unreconciled = failure.unreconciled
			} else {
				for subject := range priority {
					unreconciled[subject] = struct{}{}
				}
			}
		}
		// Reclaiming the host-side copies is reported but never fatal: it frees disk
		// rather than unblocking anything, so a directory that will not delete must
		// not cost the project its sweep.
		if w.purge != nil {
			if err := w.purge.Handle(PurgeFinishedWorkCommand{
				ProjectID: project.ID, Epics: epics,
				InFlightEpics: w.inflightEpics(project.ID),
			}); err != nil {
				failures = append(failures, fmt.Errorf("purge project %q: %w", project.Name, err))
			}
		}
		for _, current := range epics {
			if role, ok := epicRole(current.State); ok {
				subject := agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: current.ID}
				if _, blocked := unreconciled[subject]; blocked {
					continue
				}
				spec := EpicSandboxSpec(project.ID, current.ID, role, w.clock.Now().UTC())
				w.dispatch(ctx, project.ID, current.ID,
					subject,
					func(ctx context.Context) error {
						if err := w.run.Handle(ctx, RunEpicAgentCommand{
							Project: project,
							EpicID:  current.ID,
							Spec:    spec,
						}); err != nil {
							return fmt.Errorf("advance epic %q: %w", current.ID, err)
						}
						return nil
					})
				continue
			}
			if err := w.advanceIssues(ctx, project, current.ID, sweep, unreconciled); err != nil {
				failures = append(failures, fmt.Errorf("advance issues of epic %q: %w", current.ID, err))
			}
		}
	}
	return errors.Join(failures...)
}

// dispatch starts one subject's round on its own goroutine, unless that subject
// already has one running or the concurrency ceiling is full.
//
// The subject is marked in flight here, on the scheduler's goroutine, and not
// inside the round: reconciliation reads the set between a dispatch and the
// round's first registry write, and a subject missing from it in that window is
// one whose sandbox gets stopped out from under it.
func (w *EpicWorker) dispatch(
	ctx context.Context,
	projectID uint,
	epicID string,
	subject agent.AgentSubject,
	round func(context.Context) error,
) {
	key := subjectKey{project: projectID, subject: subject}
	if _, busy := w.inflight[key]; busy {
		return
	}
	if held, ok := w.retries[key]; ok && w.clock.Now().Before(held.until) {
		// Failed recently. reportSubject already said so once; the hold keeps a
		// persistent failure from also being retried on every tick.
		return
	}
	select {
	case w.rounds <- struct{}{}:
	default:
		// At the ceiling. The subject stays eligible and the next tick retries,
		// which is the same backpressure a full host produces.
		return
	}
	w.inflight[key] = epicID
	w.running.Add(1)
	go func() {
		defer w.running.Done()
		result := roundResult{key: key, epicID: epicID}
		func() {
			// A round reaches go-git, the provider and the store. A panic in any
			// of them would otherwise take the process down with the registry
			// still open and every other round's sandbox still running.
			defer func() {
				if recovered := recover(); recovered != nil {
					result.err = fmt.Errorf("round panicked: %v", recovered)
				}
			}()
			result.err = round(ctx)
		}()
		<-w.rounds
		// Never an unguarded send: at shutdown nothing is draining results, and a
		// round blocked here would hold up the wait in Run until its grace period
		// expired. The round has already recorded its own outcome by this point;
		// what is lost is only the scheduler hearing about it.
		select {
		case w.results <- result:
		case <-ctx.Done():
		}
	}()
}

// drain collects every round that has finished without waiting for any that
// have not.
func (w *EpicWorker) drain() {
	for {
		select {
		case result := <-w.results:
			w.record(result)
		default:
			return
		}
	}
}

// inflightSubjects is the project's busy subjects, as reconciliation wants them.
func (w *EpicWorker) inflightSubjects(projectID uint) map[agent.AgentSubject]struct{} {
	subjects := map[agent.AgentSubject]struct{}{}
	for key := range w.inflight {
		if key.project == projectID {
			subjects[key.subject] = struct{}{}
		}
	}
	return subjects
}

// inflightEpics is the project's busy epics, as purging wants them: a round on
// an issue protects the epic directory that issue is worked in.
func (w *EpicWorker) inflightEpics(projectID uint) map[string]struct{} {
	epics := map[string]struct{}{}
	for key, epicID := range w.inflight {
		if key.project == projectID {
			epics[epicID] = struct{}{}
		}
	}
	return epics
}

// dueForSweep reports whether this tick fetches the approved branches, and
// records that it did. The first tick always sweeps: an approval made while the
// app was not running is exactly the one most likely to have gone stale.
func (w *EpicWorker) dueForSweep() bool {
	now := w.clock.Now()
	if !w.lastSweep.IsZero() && now.Sub(w.lastSweep) < approvedBranchSweepInterval {
		return false
	}
	w.lastSweep = now
	return true
}

// advanceIssues runs the execution half for one Ready epic: open any branch
// that is missing, advance each unblocked pull request by a round, then close
// the epic if everything landed.
//
// The epic is re-read at each step rather than passed along, because opening
// pull requests and running a round both write to it — acting on a stale copy
// would re-cut branches that already exist.
func (w *EpicWorker) advanceIssues(
	ctx context.Context, project domain.Project, epicID string, sweep bool,
	unreconciled map[agent.AgentSubject]struct{},
) error {
	if !w.issues.ready() {
		return nil
	}
	if _, err := w.issues.OpenPullRequests.Handle(ctx, OpenPullRequestsCommand{
		Project: project, EpicID: epicID,
	}); err != nil {
		return err
	}
	// Approved branches are re-checked before roles are picked, so a branch
	// base has moved past is scheduled for the merge role on the same tick
	// rather than sitting in Pr looking ready until someone tries to land it.
	// This is the one step that reaches the network, so it runs on its own
	// slower beat — see approvedBranchSweepInterval.
	if sweep {
		if err := w.issues.ReviewApproved.Handle(ctx, ReviewApprovedBranchesCommand{
			Project: project, EpicID: epicID,
		}); err != nil {
			return err
		}
	}
	current, err := w.issues.GetEpic.Handle(GetEpicQuery{Project: project, EpicID: epicID})
	if err != nil {
		return err
	}
	var failures []error
	for _, pullRequest := range current.PullRequests {
		role, ok := IssueRole(current, pullRequest)
		if !ok {
			continue
		}
		subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: pullRequest.IssueID}
		if _, blocked := unreconciled[subject]; blocked {
			continue
		}
		spec := IssueSandboxSpec(
			project.ID, pullRequest.IssueID, role, w.clock.Now().UTC(),
		)
		issueID := pullRequest.IssueID
		w.dispatch(ctx, project.ID, epicID, subject,
			func(ctx context.Context) error {
				if err := w.issues.RunIssueAgent.Handle(ctx, RunIssueAgentCommand{
					Project: project, EpicID: epicID, IssueID: issueID, Spec: spec,
				}); err != nil {
					return fmt.Errorf("issue %q: %w", issueID, err)
				}
				return nil
			})
	}
	if _, err := w.issues.CompleteEpic.Handle(CompleteEpicCommand{
		Project: project, EpicID: epicID,
	}); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func eligibleSubjects(epics []epicpkg.Epic) map[agent.AgentSubject]agent.AgentRole {
	subjects := map[agent.AgentSubject]agent.AgentRole{}
	for _, current := range epics {
		if role, ok := epicRole(current.State); ok {
			subjects[agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: current.ID}] = role
		}
		for _, pullRequest := range current.PullRequests {
			if role, ok := IssueRole(current, pullRequest); ok {
				subjects[agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: pullRequest.IssueID}] = role
			}
		}
	}
	return subjects
}
