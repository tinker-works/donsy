package usecases

import (
	"testing"

	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

func TestPurgeFinishedWorkUseCase_ShouldReclaimTheDirectoriesOfFinishedEpics(t *testing.T) {
	// The sandbox side of a finished epic is reconciliation's job, but a sandbox is not where
	// the bulk sits: the clones and checkouts an epic accumulates on the host outlive
	// every sandbox built from them, keyed by epic ID and removed by nothing.
	// Arrange
	repos := &fakeRepositoryWorkspace{}
	code := newFakeCodeWorkspace()
	useCase := PurgeFinishedWorkUseCase{repos: repos, code: code}

	// Act
	err := useCase.Handle(PurgeFinishedWorkCommand{ProjectID: 1, Epics: []epicpkg.Epic{
		{ID: "done-epic", State: epicpkg.EpicStateDone},
		{ID: "closed-epic", State: epicpkg.EpicStateClosed},
		{ID: "ready-epic", State: epicpkg.EpicStateReady},
	}})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"done-epic", "closed-epic"}
	if len(repos.purged) != 2 || repos.purged[0] != want[0] || repos.purged[1] != want[1] {
		t.Fatalf("expected %v purged from the workspace, got %#v", want, repos.purged)
	}
	if len(code.purgedEpics) != 2 || code.purgedEpics[0] != want[0] {
		t.Fatalf("expected %v purged from the checkouts, got %#v", want, code.purgedEpics)
	}
}

func TestPurgeFinishedWorkUseCase_ShouldSpareAnEpicStillInFlight(t *testing.T) {
	// A merged issue's siblings are still being worked in the same directories, so
	// one finished issue is not a reason to delete the epic's working copies.
	// Arrange
	repos := &fakeRepositoryWorkspace{}
	code := newFakeCodeWorkspace()
	useCase := PurgeFinishedWorkUseCase{repos: repos, code: code}

	// Act
	err := useCase.Handle(PurgeFinishedWorkCommand{ProjectID: 1, Epics: []epicpkg.Epic{{
		ID: "epic-1", State: epicpkg.EpicStateReady,
		Issues: []epicpkg.Issue{
			{ID: "merged", State: epicpkg.IssueStateMerged},
			{ID: "coding", State: epicpkg.IssueStateCoding},
		},
	}}})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(repos.purged) != 0 || len(code.purgedEpics) != 0 {
		t.Fatalf("expected nothing purged, got %#v %#v", repos.purged, code.purgedEpics)
	}
}

func TestPurgeFinishedWorkUseCase_ShouldSpareAnEpicWithARoundInFlight(t *testing.T) {
	// Closing an epic from the UI while its issues are still being worked is the
	// case this exists for: the epic reads as finished, but purging it deletes the
	// mounted issue tree and the checkout a live round is committing into.
	// Arrange
	repos := &fakeRepositoryWorkspace{}
	code := newFakeCodeWorkspace()
	useCase := PurgeFinishedWorkUseCase{repos: repos, code: code}

	// Act
	err := useCase.Handle(PurgeFinishedWorkCommand{
		ProjectID: 1,
		Epics: []epicpkg.Epic{
			{ID: "closed-but-busy", State: epicpkg.EpicStateClosed},
			{ID: "closed-and-idle", State: epicpkg.EpicStateClosed},
		},
		InFlightEpics: map[string]struct{}{"closed-but-busy": {}},
	})

	// Assert: the idle one still goes, so this spares a round rather than
	// disabling the purge whenever anything at all is running.
	if err != nil {
		t.Fatal(err)
	}
	if len(repos.purged) != 1 || repos.purged[0] != "closed-and-idle" {
		t.Fatalf("expected only the idle epic purged from the workspace, got %#v", repos.purged)
	}
	if len(code.purgedEpics) != 1 || code.purgedEpics[0] != "closed-and-idle" {
		t.Fatalf("expected only the idle epic purged from the checkouts, got %#v", code.purgedEpics)
	}
}

func TestPurgeFinishedWorkUseCase_ShouldDiscardTranscriptsOfFinishedSubjects(t *testing.T) {
	// Per subject rather than per epic: an issue that merged is done with even while
	// its epic runs on, and its transcripts are the ones nobody will open again. The
	// run records survive — that is the history the Runs screen lists.
	// Arrange
	merged := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "merged"}
	active := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "coding"}
	output := &fakeRunOutput{}
	registry := &fakeAgentRegistry{runs: []agent.AgentRun{
		finishedRun("run-merged", merged),
		finishedRun("run-active", active),
		liveRun("run-live", "sandbox-1", merged),
	}}
	useCase := PurgeFinishedWorkUseCase{output: output, registry: registry}

	// Act
	err := useCase.Handle(PurgeFinishedWorkCommand{ProjectID: 1, Epics: []epicpkg.Epic{{
		ID: "epic-1", State: epicpkg.EpicStateReady,
		Issues: []epicpkg.Issue{
			{ID: "merged", State: epicpkg.IssueStateMerged},
			{ID: "coding", State: epicpkg.IssueStateCoding},
		},
	}}})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	// Not run-active (its subject is still being worked) and not run-live (nothing
	// should be live for a finished subject, but a transcript still being written is
	// not the file to remove out from under an open handle).
	if len(output.discarded) != 1 || output.discarded[0] != "run-merged" {
		t.Fatalf("expected only the finished subject's transcript discarded, got %#v",
			output.discarded)
	}
	if len(registry.runs) != 3 {
		t.Fatalf("expected the run records to survive, got %#v", registry.runs)
	}
}

func finishedRun(runID string, subject agent.AgentSubject) agent.AgentRun {
	run := liveRun(runID, "sandbox-1", subject)
	run.Status = agent.AgentRunStatusSucceeded
	return run
}
