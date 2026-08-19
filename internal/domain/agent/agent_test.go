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
