package usecases

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain/agent"
)

// The scheduler's bookkeeping is exercised directly here: the retry ledger, the
// per-subject failure dedupe, and the in-flight set that keeps a subject's sandbox
// and directories reserved. Driving it through a whole tick would test the round
// rather than the bookkeeping.

// bareWorker is a scheduler with no use cases behind it, for the bookkeeping that
// does not touch them.
func bareWorker(now time.Time) *EpicWorker {
	return NewEpicWorker(nil, nil, nil, nil, fixedClock{now: now},
		time.Minute, IssueLoop{}, nil)
}

func subject(id string) subjectKey {
	return subjectKey{project: 1, subject: agent.AgentSubject{
		Kind: agent.AgentSubjectEpic, ID: id,
	}}
}

func TestEpicWorker_Finish_ShouldFreeTheSubjectAndReportItsFailure(t *testing.T) {
	// Arrange: a round drained here frees its subject for the next tick rather
	// than holding its sandbox for another interval.
	now := time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC)
	worker := bareWorker(now)
	key := subject("checkout")
	worker.inflight[key] = "checkout"
	var reported []error

	// Act
	worker.finish(func(err error) { reported = append(reported, err) },
		roundResult{key: key, epicID: "checkout", err: errors.New("round failed")})

	// Assert
	if _, held := worker.inflight[key]; held {
		t.Fatal("expected the subject freed")
	}
	if len(reported) != 1 {
		t.Fatalf("expected the failure reported once, got %v", reported)
	}
	if worker.retries[key].failures != 1 {
		t.Fatalf("expected the failure booked for backoff, got %+v", worker.retries[key])
	}
}

func TestEpicWorker_Finish_ShouldClearTheLedgerOnSuccess(t *testing.T) {
	// Arrange: a subject that succeeds is eligible again immediately.
	now := time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC)
	worker := bareWorker(now)
	key := subject("checkout")
	worker.inflight[key] = "checkout"
	worker.finish(func(error) {}, roundResult{key: key, err: errors.New("first failure")})
	worker.inflight[key] = "checkout"

	// Act
	worker.finish(func(error) {}, roundResult{key: key})

	// Assert
	if _, held := worker.retries[key]; held {
		t.Fatalf("expected the backoff cleared, got %+v", worker.retries[key])
	}
}

func TestEpicWorker_Record_ShouldBackOffFurtherOnEachFailure(t *testing.T) {
	// Arrange: repeated failures wait longer, and the wait is capped so a broken
	// subject does not stop being retried altogether.
	now := time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC)
	worker := bareWorker(now)
	key := subject("checkout")

	// Act
	var delays []time.Duration
	for range 12 {
		worker.record(roundResult{key: key, err: errors.New("failed")})
		delays = append(delays, worker.retries[key].until.Sub(now))
	}

	// Assert
	if delays[0] >= delays[1] {
		t.Fatalf("expected the second wait to be longer, got %s then %s", delays[0], delays[1])
	}
	for i, delay := range delays {
		if delay > retryCap {
			t.Fatalf("failure %d waits %s, past the %s cap", i+1, delay, retryCap)
		}
	}
	if delays[len(delays)-1] != retryCap {
		t.Fatalf("expected the wait to settle at the cap, got %s", delays[len(delays)-1])
	}
}

func TestEpicWorker_Record_ShouldTripABreakerOnRepeatedHostFailures(t *testing.T) {
	// A host failure spends none of the role's attempts, so nothing else ever
	// stops it retrying. Most are worth retrying through, but some never resolve
	// on their own — a model whose only endpoint cannot do tool calls answers 404
	// the same way forever — and on the ordinary cap that is a sandbox boot per
	// subject per hour with nothing to notice it by.
	// Arrange
	now := time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC)
	worker := bareWorker(now)
	key := subject("checkout")
	hostFailure := fmt.Errorf("%w: exit status 1", ErrHostFailure)

	// Act: one short of the breaker.
	for range hostFailureBreaker - 1 {
		worker.record(roundResult{key: key, err: hostFailure})
	}
	beforeTrip := worker.retries[key].until.Sub(now)

	// Assert
	if beforeTrip > retryCap {
		t.Fatalf("held %s before tripping, past the ordinary cap of %s", beforeTrip, retryCap)
	}

	// Act
	worker.record(roundResult{key: key, err: hostFailure})

	// Assert
	if held := worker.retries[key].until.Sub(now); held != breakerCooldown {
		t.Fatalf("expected the breaker to hold for %s, got %s", breakerCooldown, held)
	}
}

func TestEpicWorker_Record_ShouldKeepTheBreakerHalfOpen(t *testing.T) {
	// A breaker that had to be reset by hand would turn every transient host
	// problem into a permanent one, so a round that reaches the agent — whatever
	// it then decides — has to close it again.
	// Arrange
	now := time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC)
	hostFailure := fmt.Errorf("%w: exit status 1", ErrHostFailure)
	tests := []struct {
		name  string
		after error
	}{
		// The agent ran and got it wrong. The host is working; the round limit is
		// what deals with the rest.
		{name: "agent answered badly", after: errors.New("agent did not return a marked answer")},
		{name: "agent succeeded", after: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			worker := bareWorker(now)
			key := subject("checkout")
			for range hostFailureBreaker {
				worker.record(roundResult{key: key, err: hostFailure})
			}

			// Act
			worker.record(roundResult{key: key, err: test.after})

			// Assert
			if got := worker.retries[key].hostFailures; got != 0 {
				t.Fatalf("expected a round that reached the agent to close the breaker, got %d", got)
			}
			if test.after == nil {
				if _, held := worker.retries[key]; held {
					t.Fatal("a successful round must clear the ledger outright")
				}
				return
			}
			if held := worker.retries[key].until.Sub(now); held > retryCap {
				t.Fatalf("expected the ordinary cap to apply again, got %s", held)
			}
		})
	}
}

func TestEpicWorker_ReportSubject_ShouldOnlyReportAChange(t *testing.T) {
	// Arrange: a subject failing the same way every tick would otherwise report
	// the same line every few seconds.
	worker := bareWorker(time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC))
	key := subject("checkout")
	var reported []string
	report := func(err error) { reported = append(reported, err.Error()) }

	// Act
	worker.reportSubject(report, key, errors.New("no agent profile"))
	worker.reportSubject(report, key, errors.New("no agent profile"))
	worker.reportSubject(report, key, errors.New("sandbox would not start"))

	// Assert
	if len(reported) != 2 {
		t.Fatalf("expected the repeat swallowed, got %v", reported)
	}
	if reported[1] != "sandbox would not start" {
		t.Fatalf("expected the new failure reported, got %v", reported)
	}
}

func TestEpicWorker_ReportSubject_ShouldRearmAfterASuccess(t *testing.T) {
	// Arrange: a recurrence is worth reporting again once the subject has been
	// seen working.
	worker := bareWorker(time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC))
	key := subject("checkout")
	var reported []string
	report := func(err error) { reported = append(reported, err.Error()) }

	// Act
	worker.reportSubject(report, key, errors.New("no agent profile"))
	worker.reportSubject(report, key, nil)
	worker.reportSubject(report, key, errors.New("no agent profile"))

	// Assert
	if len(reported) != 2 {
		t.Fatalf("expected the recurrence reported again, got %v", reported)
	}
}

func TestEpicWorker_ReportSubject_ShouldKeepSubjectsApart(t *testing.T) {
	// Arrange: two subjects failing the same way are two things to say.
	worker := bareWorker(time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC))
	var reported []string
	report := func(err error) { reported = append(reported, err.Error()) }

	// Act
	worker.reportSubject(report, subject("checkout"), errors.New("no agent profile"))
	worker.reportSubject(report, subject("search"), errors.New("no agent profile"))

	// Assert
	if len(reported) != 2 {
		t.Fatalf("expected one report per subject, got %v", reported)
	}
}

func TestEpicWorker_InflightEpics_ShouldProtectTheEpicARoundWorksIn(t *testing.T) {
	// Arrange: a round on an issue protects the epic directory that issue is
	// worked in, which is what purging has to leave alone.
	worker := bareWorker(time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC))
	worker.inflight[subjectKey{project: 1, subject: agent.AgentSubject{
		Kind: agent.AgentSubjectIssue, ID: "cart",
	}}] = "checkout"
	worker.inflight[subjectKey{project: 2, subject: agent.AgentSubject{
		Kind: agent.AgentSubjectEpic, ID: "search",
	}}] = "search"

	// Act
	busy := worker.inflightEpics(1)

	// Assert
	if _, protected := busy["checkout"]; !protected {
		t.Fatalf("expected the issue's epic protected, got %v", busy)
	}
	if _, leaked := busy["search"]; leaked {
		t.Fatalf("expected another project's epic left out, got %v", busy)
	}
}

func TestEpicWorker_InflightSubjects_ShouldBeScopedToOneProject(t *testing.T) {
	// Arrange: reconciliation is run per project, so it must not be told about
	// another project's live rounds.
	worker := bareWorker(time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC))
	mine := agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: "checkout"}
	theirs := agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: "search"}
	worker.inflight[subjectKey{project: 1, subject: mine}] = "checkout"
	worker.inflight[subjectKey{project: 2, subject: theirs}] = "search"

	// Act
	busy := worker.inflightSubjects(1)

	// Assert
	if _, held := busy[mine]; !held {
		t.Fatalf("expected this project's subject, got %v", busy)
	}
	if _, leaked := busy[theirs]; leaked {
		t.Fatalf("expected the other project's subject left out, got %v", busy)
	}
}

func TestEpicWorker_Drain_ShouldTakeEveryFinishedRoundWithoutBlocking(t *testing.T) {
	// Arrange: a subject left in the in-flight set needlessly keeps its sandbox and
	// directories reserved for another interval.
	worker := bareWorker(time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC))
	first, second := subject("checkout"), subject("search")
	worker.inflight[first], worker.inflight[second] = "checkout", "search"
	worker.results <- roundResult{key: first, epicID: "checkout"}
	worker.results <- roundResult{key: second, epicID: "search"}

	// Act
	worker.drain()

	// Assert
	if len(worker.inflight) != 0 {
		t.Fatalf("expected both rounds drained, got %v", worker.inflight)
	}
}

func TestEpicWorker_Drain_ShouldReturnImmediatelyWithNothingToTake(t *testing.T) {
	// Arrange: draining runs at the top of every tick whether or not a round has
	// ended, so it must never wait for one.
	worker := bareWorker(time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC))
	worker.inflight[subject("checkout")] = "checkout"

	// Act
	worker.drain()

	// Assert
	if len(worker.inflight) != 1 {
		t.Fatalf("expected the live round left alone, got %v", worker.inflight)
	}
}

func TestEpicWorker_DueForSweep_ShouldAlwaysSweepTheFirstTick(t *testing.T) {
	// Arrange: an approval made while the app was not running is exactly the one
	// most likely to have gone stale.
	now := time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC)
	worker := bareWorker(now)

	// Act
	first := worker.dueForSweep()
	again := worker.dueForSweep()

	// Assert
	if !first {
		t.Fatal("expected the first tick to sweep")
	}
	if again {
		t.Fatal("expected the next tick to skip the fetch")
	}
}
