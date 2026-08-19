package epic

import "fmt"

type EpicState string

const (
	EpicStateConcept          EpicState = "Concept"
	EpicStateRefine           EpicState = "Refine"
	EpicStateReview           EpicState = "Review"
	EpicStateChangesRequested EpicState = "ChangesRequested"
	EpicStateProposed         EpicState = "Proposed"
	EpicStateReady            EpicState = "Ready"
	EpicStateDone             EpicState = "Done"
	EpicStateClosed           EpicState = "Closed"
	EpicStateFailed           EpicState = "Failed"
)

type EpicEvent string

const (
	EpicEventRefine         EpicEvent = "refine"
	EpicEventReview         EpicEvent = "review"
	EpicEventRequestChanges EpicEvent = "request_changes"
	EpicEventPropose        EpicEvent = "propose"
	EpicEventReady          EpicEvent = "ready"
	EpicEventDone           EpicEvent = "done"
	EpicEventClose          EpicEvent = "close"
	EpicEventFail           EpicEvent = "fail"
	EpicEventReset          EpicEvent = "reset"
)

func (e *Epic) Apply(event EpicEvent) error {
	if !isEpicState(e.State) {
		return fmt.Errorf("epic has invalid state %q", e.State)
	}
	next, ok := epicTransition(e.State, event)
	if !ok {
		return fmt.Errorf("cannot apply epic event %q from state %q", event, e.State)
	}
	e.State = next
	return nil
}

// RecordDraftingPass counts one completed refine-review cycle and moves the
// epic by the verdict. A reviewer and a refiner can volley indefinitely: past
// MaxDraftingPasses the tree is proposed regardless of the verdict, because
// another round of the same disagreement is not converging on anything.
//
// Proposed is where the loop hands back. Ready is what cuts branches and
// pushes them, so committing to it — and to the branch prefix they are named
// after — is a person's call, not a reviewer's.
func (e *Epic) RecordDraftingPass(approved bool) error {
	passes := e.DraftingPasses + 1
	event := EpicEventRequestChanges
	if approved || passes >= MaxDraftingPasses {
		event = EpicEventPropose
	}
	// The counter commits only with the transition, so a pass recorded from a
	// state that cannot take the verdict does not burn one of the epic's cycles.
	if err := e.Apply(event); err != nil {
		return err
	}
	e.DraftingPasses = passes
	return nil
}

// ForceState sets the state without consulting the transition table. It is the
// escape hatch for an epic the loop has stranded — a state whose role no longer
// maps to anything, or one reached before a since-changed FSM — where every
// legal move out is itself illegal and the epic can otherwise never be revived.
//
// This is deliberately not TransitionTo: skipping the FSM can produce a state
// the rest of the loop does not expect, so it belongs behind a debug action a
// person chose, never on an automatic path.
func (e *Epic) ForceState(next EpicState) error {
	if !isEpicState(next) {
		return fmt.Errorf("epic has invalid state %q", next)
	}
	e.State = next
	return nil
}

func (e *Epic) TransitionTo(next EpicState) error {
	if !isEpicState(next) {
		return fmt.Errorf("epic has invalid state %q", next)
	}
	if e.State == next {
		return nil
	}

	event, ok := epicEventForTransition(e.State, next)
	if !ok {
		return fmt.Errorf("cannot transition epic from %q to %q", e.State, next)
	}
	return e.Apply(event)
}

// allEpicStates lists every state in pipeline order, for iteration.
var allEpicStates = []EpicState{
	EpicStateConcept,
	EpicStateRefine,
	EpicStateReview,
	EpicStateChangesRequested,
	EpicStateProposed,
	EpicStateReady,
	EpicStateDone,
	EpicStateClosed,
	EpicStateFailed,
}

// LegalTransitions lists the states an epic may move to from current, in
// pipeline order. It is derived from the transition table rather than written
// out, so a dialog offering these choices cannot drift from what Apply accepts.
func LegalTransitions(current EpicState) []EpicState {
	var out []EpicState
	for _, next := range allEpicStates {
		if next == current {
			continue
		}
		if _, ok := epicEventForTransition(current, next); ok {
			out = append(out, next)
		}
	}
	return out
}

// NextApprovalState is the state an approval advances an epic to: one step
// along the drafting pipeline. It lives here, beside the transition table,
// because a UI keeping its own copy of the pipeline order is exactly how an
// approve key ends up disagreeing with the FSM it fronts.
func NextApprovalState(current EpicState) (EpicState, bool) {
	switch current {
	case EpicStateConcept:
		return EpicStateRefine, true
	case EpicStateRefine:
		return EpicStateReview, true
	case EpicStateReview:
		return EpicStateProposed, true
	case EpicStateChangesRequested:
		return EpicStateReview, true
	case EpicStateProposed:
		return EpicStateReady, true
	case EpicStateReady:
		return EpicStateDone, true
	default:
		return "", false
	}
}

func isEpicState(state EpicState) bool {
	switch state {
	case EpicStateConcept,
		EpicStateRefine,
		EpicStateReview,
		EpicStateChangesRequested,
		EpicStateProposed,
		EpicStateReady,
		EpicStateDone,
		EpicStateClosed,
		EpicStateFailed:
		return true
	default:
		return false
	}
}

func epicEventForTransition(current, next EpicState) (EpicEvent, bool) {
	switch {
	case current == EpicStateConcept && next == EpicStateRefine,
		current == EpicStateChangesRequested && next == EpicStateRefine:
		return EpicEventRefine, true
	case current == EpicStateRefine && next == EpicStateReview,
		current == EpicStateChangesRequested && next == EpicStateReview:
		return EpicEventReview, true
	case current == EpicStateReview && next == EpicStateChangesRequested,
		current == EpicStateProposed && next == EpicStateChangesRequested,
		current == EpicStateReady && next == EpicStateChangesRequested:
		return EpicEventRequestChanges, true
	case current == EpicStateReview && next == EpicStateProposed:
		return EpicEventPropose, true
	case current == EpicStateProposed && next == EpicStateReady:
		return EpicEventReady, true
	case current == EpicStateReady && next == EpicStateDone:
		return EpicEventDone, true
	case next == EpicStateClosed:
		return EpicEventClose, true
	case current == EpicStateConcept && next == EpicStateFailed,
		current == EpicStateRefine && next == EpicStateFailed,
		current == EpicStateReview && next == EpicStateFailed,
		current == EpicStateChangesRequested && next == EpicStateFailed,
		current == EpicStateProposed && next == EpicStateFailed,
		current == EpicStateReady && next == EpicStateFailed,
		current == EpicStateDone && next == EpicStateFailed:
		return EpicEventFail, true
	case current == EpicStateFailed && next == EpicStateConcept:
		return EpicEventReset, true
	}
	return "", false
}

func epicTransition(state EpicState, event EpicEvent) (EpicState, bool) {
	switch event {
	case EpicEventRefine:
		if state == EpicStateConcept || state == EpicStateChangesRequested {
			return EpicStateRefine, true
		}
	case EpicEventReview:
		if state == EpicStateRefine || state == EpicStateChangesRequested {
			return EpicStateReview, true
		}
	case EpicEventRequestChanges:
		if state == EpicStateReview || state == EpicStateProposed || state == EpicStateReady {
			return EpicStateChangesRequested, true
		}
	case EpicEventPropose:
		if state == EpicStateReview {
			return EpicStateProposed, true
		}
	case EpicEventReady:
		if state == EpicStateProposed {
			return EpicStateReady, true
		}
	case EpicEventDone:
		if state == EpicStateReady {
			return EpicStateDone, true
		}
	case EpicEventClose:
		// Closing is always legal. Abandoning an epic is a decision a person
		// makes about work that no longer matters, and it must not depend on
		// first walking the epic through states nobody intends to reach.
		if state != EpicStateClosed {
			return EpicStateClosed, true
		}
	case EpicEventFail:
		if state != EpicStateClosed && state != EpicStateFailed {
			return EpicStateFailed, true
		}
	case EpicEventReset:
		if state == EpicStateFailed {
			return EpicStateConcept, true
		}
	}
	return "", false
}
