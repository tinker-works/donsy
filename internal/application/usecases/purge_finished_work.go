package usecases

import (
	"errors"
	"fmt"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

// PurgeFinishedWorkUseCase reclaims the host-side working copies and transcripts of
// work that has finished, which nothing else does.
//
// The sandbox side of a finished epic is handled by reconciliation, but a sandbox is not where
// the bulk sits: the clones and checkouts an epic accumulates on the host outlive
// every sandbox built from them, keyed by epic ID and never removed. Closing an epic
// deletes the branches behind its abandoned work; the directories those branches
// were worked in stay.
//
// It is driven by state rather than by the moment of transition, so it also collects
// what epics finished before anything cleaned up after them, and so that a purge
// interrupted halfway is simply redone on the next tick.
type PurgeFinishedWorkUseCase struct {
	// repos and code are the two roots an epic occupies. Either may be nil when the
	// loop runs drafting-only and no code was ever checked out.
	repos agent_runtime.RepositoryWorkspace
	code  agent_runtime.CodeWorkspace
	// output is the transcript store. Runs keep their records — that is the history
	// the Runs screen lists — while the raw output goes with the work.
	output   agent_runtime.RunOutput
	registry agent_runtime.AgentRegistry
}

type PurgeFinishedWorkCommand struct {
	ProjectID uint
	// Epics is the project's epics as the worker already read them, so this costs no
	// extra store read of its own.
	Epics []epicpkg.Epic
	// InFlightEpics names the epics that have a round running right now. Purging
	// deletes an epic's whole directory - the mounted issue trees, the read-only
	// clones and every per-issue checkout - so doing it under a live round takes
	// the working copy out from under it mid-write.
	//
	// An epic reaches a finished state while its issues are still being worked
	// whenever somebody closes it from the UI, which is exactly when this matters.
	// The next tick purges it once its rounds have drained.
	//
	// It is keyed by epic rather than by subject because that is what a purge
	// removes: a round on an issue protects the epic directory the issue lives in.
	InFlightEpics map[string]struct{}
}

func (u *PurgeFinishedWorkUseCase) Handle(command PurgeFinishedWorkCommand) error {
	var errs []error
	for _, current := range finishedEpics(command.Epics) {
		if _, busy := command.InFlightEpics[current]; busy {
			continue
		}
		if u.repos != nil {
			if err := u.repos.Purge(current); err != nil {
				errs = append(errs, fmt.Errorf("purge workspace of epic %q: %w", current, err))
			}
		}
		if u.code != nil {
			if err := u.code.PurgeEpic(current); err != nil {
				errs = append(errs, fmt.Errorf("purge checkouts of epic %q: %w", current, err))
			}
		}
	}
	if err := u.discardTranscripts(command); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// finishedEpics lists the IDs of the epics that are finished with. An epic still in
// flight is left alone whatever state its individual issues reached: a merged issue's
// siblings are still being worked in the same directories.
func finishedEpics(epics []epicpkg.Epic) []string {
	var finished []string
	for _, current := range epics {
		if current.State == epicpkg.EpicStateDone || current.State == epicpkg.EpicStateClosed {
			finished = append(finished, current.ID)
		}
	}
	return finished
}

// discardTranscripts drops the raw output of every run whose subject is finished.
// This is per subject rather than per epic because an issue that merged is done with
// even while its epic runs on, and a merged issue's transcripts are the ones nobody
// will open again.
func (u *PurgeFinishedWorkUseCase) discardTranscripts(command PurgeFinishedWorkCommand) error {
	if u.output == nil || u.registry == nil {
		return nil
	}
	terminal := TerminalSubjects(command.Epics)
	if len(terminal) == 0 {
		return nil
	}
	runs, err := u.registry.ListProjectAgentRuns(command.ProjectID)
	if err != nil {
		return err
	}
	var errs []error
	for _, run := range runs {
		if _, finished := terminal[run.Subject]; !finished {
			continue
		}
		// A live run is still writing its transcript. Nothing should be live for a
		// finished subject, but removing a file out from under an open handle is not
		// the way to find out.
		if isLiveAgentRunStatus(run.Status) {
			continue
		}
		if err := u.output.Discard(run.ID); err != nil {
			errs = append(errs, fmt.Errorf("discard transcript of run %q: %w", run.ID, err))
		}
	}
	return errors.Join(errs...)
}
