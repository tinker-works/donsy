// Package agent contains the agent configuration aggregate and the small
// value objects needed to validate agent activity restored from storage.
package agent

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tinker-works/donsy/internal/domain/id"
)

// Status is the lifecycle state shared by agent runs and sandboxes.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Compatibility names keep the persisted state vocabulary readable at call
// sites while all state values remain one domain type.
const (
	Pending   = StatusPending
	Running   = StatusRunning
	Completed = StatusCompleted
	Failed    = StatusFailed
	Cancelled = StatusCancelled
)

func (value Status) Valid() bool {
	switch value {
	case StatusPending, StatusRunning, StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

func (value Status) Terminal() bool {
	return value == StatusCompleted || value == StatusFailed || value == StatusCancelled
}

// Agent is the persisted configuration for an agent role.
type Agent struct {
	ID          id.ID  `json:"id"`
	Name        string `json:"name"`
	Variant     string `json:"variant,omitempty"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
}

func New(name string, variant ...string) (Agent, error) {
	value := Agent{ID: id.New(), Name: strings.TrimSpace(name), Enabled: true}
	if len(variant) > 0 {
		value.Variant = strings.TrimSpace(variant[0])
	}
	if err := value.Validate(); err != nil {
		return Agent{}, err
	}
	return value, nil
}

func (value Agent) Validate() error {
	if strings.TrimSpace(value.Name) == "" {
		return errors.New("agent name cannot be empty")
	}
	if strings.ContainsAny(value.Name, "\r\n") || strings.ContainsAny(value.Variant, "\r\n") {
		return errors.New("agent name and variant cannot contain newlines")
	}
	return nil
}

func (value Agent) Valid() bool { return value.Validate() == nil }

func (value *Agent) Enable() error {
	if value == nil {
		return errors.New("agent is nil")
	}
	if err := value.Validate(); err != nil {
		return err
	}
	value.Enabled = true
	return nil
}

func (value *Agent) Disable() error {
	if value == nil {
		return errors.New("agent is nil")
	}
	if err := value.Validate(); err != nil {
		return err
	}
	value.Enabled = false
	return nil
}

// Settings contains the values selected for an agent invocation.
type Settings struct {
	ID      id.ID  `json:"id"`
	AgentID id.ID  `json:"agent_id"`
	Variant string `json:"variant,omitempty"`
	Model   string `json:"model,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
	Enabled bool   `json:"enabled"`
}

func (value Settings) Validate() error {
	if strings.ContainsAny(value.Variant, "\r\n") || strings.ContainsAny(value.Model, "\r\n") {
		return errors.New("agent settings contain a newline")
	}
	return nil
}

func (value Settings) Valid() bool { return value.Validate() == nil }

// Run records a single invocation of an agent. It is kept in this package so
// persisted activity can be validated without importing a runtime adapter.
type Run struct {
	ID           id.ID     `json:"id"`
	AgentID      id.ID     `json:"agent_id"`
	ProjectID    id.ID     `json:"project_id"`
	IssueID      id.ID     `json:"issue_id"`
	SandboxID    id.ID     `json:"sandbox_id"`
	Variant      string    `json:"variant,omitempty"`
	Status       Status    `json:"status"`
	SessionID    string    `json:"session_id,omitempty"`
	Error        string    `json:"error,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
	InputTokens  int64     `json:"input_tokens,omitempty"`
	OutputTokens int64     `json:"output_tokens,omitempty"`
}

func NewRun(agentID id.ID, projectID id.ID, issueID id.ID) Run {
	return Run{ID: id.New(), AgentID: agentID, ProjectID: projectID, IssueID: issueID, Status: StatusPending}
}

func (value Run) Validate() error {
	if !value.Status.Valid() {
		return fmt.Errorf("unknown agent run status %q", value.Status)
	}
	if err := value.validateFinishedAt(value.FinishedAt); err != nil {
		return err
	}
	if value.InputTokens < 0 || value.OutputTokens < 0 {
		return errors.New("agent token usage cannot be negative")
	}
	return nil
}

func (value Run) Valid() bool { return value.Validate() == nil }

func (value Run) validateFinishedAt(at time.Time) error {
	if at.Before(value.StartedAt) && !at.IsZero() {
		return errors.New("agent run finished before it started")
	}
	return nil
}

func (value *Run) Transition(to Status) error {
	if value == nil {
		return errors.New("agent run is nil")
	}
	if !to.Valid() {
		return fmt.Errorf("unknown agent run status %q", to)
	}
	if value.Status.Terminal() {
		return fmt.Errorf("agent run is already %s", value.Status)
	}
	if value.Status == StatusPending && to != StatusRunning && to != StatusCancelled {
		return fmt.Errorf("cannot transition agent run from %s to %s", value.Status, to)
	}
	if value.Status == StatusRunning && to == StatusPending {
		return fmt.Errorf("cannot transition agent run from %s to %s", value.Status, to)
	}
	finishedAt := value.FinishedAt
	if to.Terminal() {
		if finishedAt.IsZero() {
			finishedAt = time.Now()
		}
		if err := value.validateFinishedAt(finishedAt); err != nil {
			return err
		}
	}
	value.Status = to
	value.FinishedAt = finishedAt
	return nil
}

func (value *Run) Start(at time.Time) error {
	if value == nil {
		return errors.New("agent run is nil")
	}

	started := *value
	started.StartedAt = at
	if err := started.Transition(StatusRunning); err != nil {
		return err
	}
	if err := started.Validate(); err != nil {
		return err
	}
	*value = started
	return nil
}

func (value *Run) Complete(at time.Time) error {
	if value == nil {
		return errors.New("agent run is nil")
	}
	if err := value.validateFinishedAt(at); err != nil {
		return err
	}
	if err := value.Transition(StatusCompleted); err != nil {
		return err
	}
	value.FinishedAt = at
	return nil
}

func (value *Run) Fail(message string, at time.Time) error {
	if value == nil {
		return errors.New("agent run is nil")
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("agent run error cannot be empty")
	}
	if err := value.validateFinishedAt(at); err != nil {
		return err
	}
	if err := value.Transition(StatusFailed); err != nil {
		return err
	}
	value.Error = message
	value.FinishedAt = at
	return nil
}

// Sandbox records the lifecycle of an execution sandbox.
type Sandbox struct {
	ID        id.ID     `json:"id"`
	RunID     id.ID     `json:"run_id"`
	Status    Status    `json:"status"`
	Container string    `json:"container,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

func (value Sandbox) Validate() error {
	if !value.Status.Valid() {
		return fmt.Errorf("unknown sandbox status %q", value.Status)
	}
	if value.EndedAt.Before(value.StartedAt) && !value.EndedAt.IsZero() {
		return errors.New("sandbox ended before it started")
	}
	return nil
}

func (value Sandbox) Valid() bool { return value.Validate() == nil }
