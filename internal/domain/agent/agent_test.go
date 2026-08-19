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

func TestRunStartRejectsAlreadyRunningRun(t *testing.T) {
	value := NewRun("agent", "project", "issue")
	started := time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	if err := value.Start(started); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	want := value

	if err := value.Start(started.Add(time.Minute)); err == nil {
		t.Fatal("Start() accepted an already-running run")
	}
	if value != want {
		t.Fatalf("Start() mutated the run on error: got %#v, want %#v", value, want)
	}
}

func TestRunCompleteUsesSuppliedFinishTime(t *testing.T) {
	value := NewRun("agent", "project", "issue")
	started := time.Date(2099, time.August, 19, 0, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	if err := value.Start(started); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := value.Complete(finished); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if value.Status != StatusCompleted {
		t.Fatalf("Complete() status = %q, want %q", value.Status, StatusCompleted)
	}
	if !value.FinishedAt.Equal(finished) {
		t.Fatalf("Complete() finished at = %v, want %v", value.FinishedAt, finished)
	}
}

func TestRunFailUsesSuppliedFinishTime(t *testing.T) {
	value := NewRun("agent", "project", "issue")
	started := time.Date(2099, time.August, 19, 0, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	if err := value.Start(started); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := value.Fail("failed", finished); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	if value.Status != StatusFailed {
		t.Fatalf("Fail() status = %q, want %q", value.Status, StatusFailed)
	}
	if !value.FinishedAt.Equal(finished) {
		t.Fatalf("Fail() finished at = %v, want %v", value.FinishedAt, finished)
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

func TestRunTimestampTransitionsRejectInvalidRestoredChronology(t *testing.T) {
	started := time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		transition func(*Run) error
	}{
		{
			name: "start",
			transition: func(value *Run) error {
				return value.Start(started.Add(-2 * time.Minute))
			},
		},
		{
			name: "complete",
			transition: func(value *Run) error {
				return value.Complete(started.Add(time.Minute))
			},
		},
		{
			name: "fail",
			transition: func(value *Run) error {
				return value.Fail("failed", started.Add(time.Minute))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := NewRun("agent", "project", "issue")
			value.Status = StatusRunning
			value.StartedAt = started
			value.FinishedAt = started.Add(-time.Minute)
			want := value

			if err := test.transition(&value); err == nil {
				t.Fatal("transition accepted an invalid restored run")
			}
			if value != want {
				t.Fatalf("transition mutated the run on error: got %#v, want %#v", value, want)
			}
		})
	}
}

func TestRunTransitionsRejectInvalidRestoredRun(t *testing.T) {
	started := time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		transition func(*Run) error
	}{
		{
			name: "transition",
			transition: func(value *Run) error {
				return value.Transition(StatusCancelled)
			},
		},
		{
			name: "complete",
			transition: func(value *Run) error {
				return value.Complete(started.Add(time.Minute))
			},
		},
		{
			name: "fail",
			transition: func(value *Run) error {
				return value.Fail("failed", started.Add(time.Minute))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := NewRun("agent", "project", "issue")
			value.Status = StatusRunning
			value.StartedAt = started
			value.InputTokens = -1
			want := value

			if err := test.transition(&value); err == nil {
				t.Fatal("transition accepted an invalid restored run")
			}
			if value != want {
				t.Fatalf("transition mutated the run on error: got %#v, want %#v", value, want)
			}
		})
	}
}

func TestRunTransitionsRejectInvalidRestoredStatus(t *testing.T) {
	started := time.Date(2026, time.August, 19, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		transition func(*Run) error
	}{
		{
			name: "transition",
			transition: func(value *Run) error {
				return value.Transition(StatusRunning)
			},
		},
		{
			name: "start",
			transition: func(value *Run) error {
				return value.Start(started)
			},
		},
		{
			name: "complete",
			transition: func(value *Run) error {
				return value.Complete(started.Add(time.Minute))
			},
		},
		{
			name: "fail",
			transition: func(value *Run) error {
				return value.Fail("failed", started.Add(time.Minute))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := NewRun("agent", "project", "issue")
			value.Status = Status("invalid")
			want := value

			if err := test.transition(&value); err == nil {
				t.Fatal("transition accepted an invalid restored status")
			}
			if value != want {
				t.Fatalf("transition mutated the run on error: got %#v, want %#v", value, want)
			}
		})
	}
}
