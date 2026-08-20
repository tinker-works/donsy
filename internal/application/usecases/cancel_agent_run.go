package usecases

import (
	"fmt"
	"strings"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

type CancelAgentRunCommand struct {
	RunID string
}

// CancelAgentRunUseCase stops a round that is still executing. The round's
// own goroutine records the cancellation when its context dies, so this only
// signals — it does not write the run's terminal state itself, which would
// race with the goroutine still holding the run.
//
// A run that is queued or admitted has no goroutine to signal yet, so that
// one is cancelled here directly.
type CancelAgentRunUseCase struct {
	registry   agent_runtime.AgentRegistry
	supervisor *RunSupervisor
	clock      application.Clock
}

// Handle reports whether it stopped anything. False means the run had already
// finished — a lost race, not an error.
func (u *CancelAgentRunUseCase) Handle(command CancelAgentRunCommand) (bool, error) {
	if strings.TrimSpace(command.RunID) == "" {
		return false, fmt.Errorf("agent run ID is required")
	}
	run, err := u.registry.GetAgentRun(command.RunID)
	if err != nil {
		return false, err
	}
	if !isLiveAgentRunStatus(run.Status) {
		return false, nil
	}
	if u.supervisor.Cancel(command.RunID) {
		return true, nil
	}
	// Nothing in this process is executing it. Queued and admitted runs are
	// waiting to start; a Running row is a leftover from a prior process. In both
	// cases no goroutine will observe a cancelled context, so record it here.
	if err := run.Apply(agent.AgentRunEventCancel, u.clock.Now().UTC()); err != nil {
		return false, err
	}
	return true, u.registry.SaveAgentRun(run)
}
