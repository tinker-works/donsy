package agent

import (
	"testing"
	"time"
)

func TestAgentLifecycle(t *testing.T) {
	value, err := New("reviewer", "default")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := value.Disable(); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if value.Enabled {
		t.Fatal("Disable() left agent enabled")
	}
	if err := value.Enable(); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
}

func TestRunTransitions(t *testing.T) {
	value := NewRun("agent", "project", "issue")
	started := time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	if err := value.Start(started); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := value.Complete(started.Add(time.Minute)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := value.Transition(StatusRunning); err == nil {
		t.Fatal("Transition() reopened a completed run")
	}
}

func TestRunCompleteRejectsFinishBeforeStart(t *testing.T) {
	value := NewRun("agent", "project", "issue")
	started := time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	if err := value.Start(started); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	want := value

	if err := value.Complete(started.Add(-time.Minute)); err == nil {
		t.Fatal("Complete() accepted a finish time before the start")
	}
	if value != want {
		t.Fatalf("Complete() mutated the run on error: got %#v, want %#v", value, want)
	}
}

func TestRunFailRejectsFinishBeforeStart(t *testing.T) {
	value := NewRun("agent", "project", "issue")
	started := time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	if err := value.Start(started); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	want := value

	if err := value.Fail("failed", started.Add(-time.Minute)); err == nil {
		t.Fatal("Fail() accepted a finish time before the start")
	}
	if value != want {
		t.Fatalf("Fail() mutated the run on error: got %#v, want %#v", value, want)
	}
}

func TestRunStartRejectsRestoredFinishBeforeStart(t *testing.T) {
	value := NewRun("agent", "project", "issue")
	value.FinishedAt = time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	want := value

	if err := value.Start(value.FinishedAt.Add(time.Minute)); err == nil {
		t.Fatal("Start() accepted a finish time before the start")
	}
	if value != want {
		t.Fatalf("Start() mutated the run on error: got %#v, want %#v", value, want)
	}
}

func TestRunTransitionRejectsGeneratedFinishBeforeStart(t *testing.T) {
	value := NewRun("agent", "project", "issue")
	started := time.Date(2099, time.August, 19, 0, 0, 0, 0, time.UTC)
	if err := value.Start(started); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	want := value

	if err := value.Transition(StatusCancelled); err == nil {
		t.Fatal("Transition() accepted a finish time before the start")
	}
	if value != want {
		t.Fatalf("Transition() mutated the run on error: got %#v, want %#v", value, want)
	}
}

func TestRunTransitionRejectsRestoredFinishBeforeStart(t *testing.T) {
	value := NewRun("agent", "project", "issue")
	value.Status = StatusRunning
	value.StartedAt = time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	value.FinishedAt = value.StartedAt.Add(-time.Minute)
	want := value

	if err := value.Transition(StatusCancelled); err == nil {
		t.Fatal("Transition() accepted a restored finish time before the start")
	}
	if value != want {
		t.Fatalf("Transition() mutated the run on error: got %#v, want %#v", value, want)
	}
}
