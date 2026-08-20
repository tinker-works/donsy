package usecases

import (
	"context"
	"sync"
)

// RunSupervisor tracks the rounds currently executing so something outside
// the goroutine running them can stop one.
//
// It exists because a round is a synchronous call: RunEpicAgentUseCase.Handle
// blocks in AgentRuntime.Run until the agent finishes, holding a context
// nobody else can reach. The supervisor is that reach.
//
// It is safe for concurrent use: the worker starts rounds from its own
// goroutine while a cancel arrives from the UI's.
type RunSupervisor struct {
	mu        sync.Mutex
	cancels   map[string]context.CancelFunc
	cancelled map[string]struct{}
	done      map[string]chan struct{}
}

func NewRunSupervisor() *RunSupervisor {
	return &RunSupervisor{
		cancels:   map[string]context.CancelFunc{},
		cancelled: map[string]struct{}{},
		done:      map[string]chan struct{}{},
	}
}

// Begin returns a context for the round and the function that stops accepting
// cancellation. The caller must always call the returned release, even on
// failure, or the run leaks a cancel func for the life of the process. The
// completion barrier remains until Complete so cleanup cannot race terminal
// run persistence.
func (s *RunSupervisor) Begin(
	ctx context.Context, runID string,
) (context.Context, func()) {
	if s == nil {
		return ctx, func() {}
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancels[runID] = cancel
	s.done[runID] = make(chan struct{})
	s.mu.Unlock()
	return runCtx, func() {
		s.mu.Lock()
		delete(s.cancels, runID)
		s.mu.Unlock()
		cancel()
	}
}

// Wait blocks until the round has finished its terminal run write. A canceled
// context stops the wait without permitting callers to continue with cleanup.
func (s *RunSupervisor) Wait(ctx context.Context, runID string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	done, tracked := s.done[runID]
	s.mu.Unlock()
	if !tracked {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Complete releases the completion barrier after the caller has persisted the
// round's terminal state. It is separate from the cancellation release because
// invoke finishes before the surrounding use case records the outcome.
func (s *RunSupervisor) Complete(runID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	done, tracked := s.done[runID]
	if tracked {
		delete(s.done, runID)
	}
	s.mu.Unlock()
	if tracked {
		close(done)
	}
}

// Cancel stops a live round and remembers that it was cancelled rather than
// having failed. It reports whether a live round was found: false means the
// run already finished, which is not an error, just a race the caller lost.
func (s *RunSupervisor) Cancel(runID string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	cancel, live := s.cancels[runID]
	if live {
		s.cancelled[runID] = struct{}{}
	}
	s.mu.Unlock()
	if !live {
		return false
	}
	cancel()
	return true
}

// WasCancelled reports whether this run was stopped on purpose. The round
// itself only sees a context error, which is indistinguishable from a crash
// without asking here.
func (s *RunSupervisor) WasCancelled(runID string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, cancelled := s.cancelled[runID]
	return cancelled
}

// Forget drops the record of a cancellation once the round has been recorded
// as cancelled, so a re-run of the same subject starts clean.
func (s *RunSupervisor) Forget(runID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.cancelled, runID)
	s.mu.Unlock()
}
