package epic

import "testing"

func TestEpic_Apply_ShouldTransitionUsingNamedEvent(t *testing.T) {
	// Arrange
	tests := []struct {
		name  string
		from  EpicState
		event EpicEvent
		to    EpicState
	}{
		{name: "concept to refine", from: EpicStateConcept, event: EpicEventRefine, to: EpicStateRefine},
		{name: "review to proposed", from: EpicStateReview,
			event: EpicEventPropose, to: EpicStateProposed},
		{name: "ready to changes requested", from: EpicStateReady,
			event: EpicEventRequestChanges, to: EpicStateChangesRequested},
		{name: "done to closed", from: EpicStateDone, event: EpicEventClose, to: EpicStateClosed},
		{name: "failed to concept", from: EpicStateFailed, event: EpicEventReset, to: EpicStateConcept},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			epic := Epic{State: tt.from}

			// Act
			if err := epic.Apply(tt.event); err != nil {
				t.Fatal(err)
			}

			// Assert
			if epic.State != tt.to {
				t.Fatalf("expected state %s, got %s", tt.to, epic.State)
			}
		})
	}
}

func TestEpic_Apply_ShouldRejectInvalidEventWithoutChangingState(t *testing.T) {
	// Arrange
	epic := Epic{State: EpicStateClosed}

	// Act
	err := epic.Apply(EpicEventFail)

	// Assert
	if err == nil {
		t.Fatal("expected invalid event to fail")
	}
	if epic.State != EpicStateClosed {
		t.Fatalf("failed event changed state to %s", epic.State)
	}
}

func TestEpicStateAllowsConfiguredTransitions(t *testing.T) {
	// Arrange
	tests := []struct {
		name string
		from EpicState
		to   EpicState
	}{
		{name: "concept to refine", from: EpicStateConcept, to: EpicStateRefine},
		{name: "refine to review", from: EpicStateRefine, to: EpicStateReview},
		{name: "review to proposed", from: EpicStateReview, to: EpicStateProposed},
		{name: "review to changes requested", from: EpicStateReview, to: EpicStateChangesRequested},
		{name: "changes requested to review", from: EpicStateChangesRequested, to: EpicStateReview},
		{name: "proposed to ready", from: EpicStateProposed, to: EpicStateReady},
		{name: "proposed to changes requested", from: EpicStateProposed, to: EpicStateChangesRequested},
		{name: "ready to done", from: EpicStateReady, to: EpicStateDone},
		{name: "ready to changes requested", from: EpicStateReady, to: EpicStateChangesRequested},
		{name: "done to closed", from: EpicStateDone, to: EpicStateClosed},
		{name: "concept to failed", from: EpicStateConcept, to: EpicStateFailed},
		{name: "failed to concept", from: EpicStateFailed, to: EpicStateConcept},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			epic := Epic{State: tt.from}

			// Act
			if err := epic.TransitionTo(tt.to); err != nil {
				t.Fatal(err)
			}

			// Assert
			if epic.State != tt.to {
				t.Fatalf("expected state %s, got %s", tt.to, epic.State)
			}
		})
	}
}

func TestEpicStateAllowsStayingInSameState(t *testing.T) {
	// Arrange
	epic := Epic{State: EpicStateReview}

	// Act
	if err := epic.TransitionTo(EpicStateReview); err != nil {
		t.Fatal(err)
	}

	// Assert
	if epic.State != EpicStateReview {
		t.Fatalf("unexpected state: %s", epic.State)
	}
}

func TestEpicStateAllowsClosingFromAnyState(t *testing.T) {
	// Arrange: abandoning an epic is a decision about work that no longer
	// matters, so it must not require walking it forward first.
	states := []EpicState{
		EpicStateConcept, EpicStateRefine, EpicStateReview, EpicStateChangesRequested,
		EpicStateProposed, EpicStateReady, EpicStateDone, EpicStateFailed,
	}

	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			// Act
			viaTransition := Epic{State: state}
			transitionErr := viaTransition.TransitionTo(EpicStateClosed)
			viaClose := Epic{State: state}
			closeErr := viaClose.Close()

			// Assert: both entry points agree, so CloseEpicUseCase and a
			// state-transition modal cannot disagree about what is legal.
			if transitionErr != nil || viaTransition.State != EpicStateClosed {
				t.Fatalf("TransitionTo from %s: %v (state %s)", state, transitionErr, viaTransition.State)
			}
			if closeErr != nil || viaClose.State != EpicStateClosed {
				t.Fatalf("Close from %s: %v (state %s)", state, closeErr, viaClose.State)
			}
		})
	}
}

func TestEpicStateClosingAClosedEpicIsRejected(t *testing.T) {
	// Arrange
	closed := Epic{State: EpicStateClosed}

	// Act
	err := closed.Close()

	// Assert
	if err == nil {
		t.Fatal("expected closing an already closed epic to be rejected")
	}
}

func TestEpicStateRejectsInvalidTransitions(t *testing.T) {
	// Arrange
	tests := []struct {
		name string
		from EpicState
		to   EpicState
	}{
		{name: "concept to review", from: EpicStateConcept, to: EpicStateReview},
		{name: "refine to concept", from: EpicStateRefine, to: EpicStateConcept},
		{name: "review to ready", from: EpicStateReview, to: EpicStateReady},
		{name: "changes requested to proposed", from: EpicStateChangesRequested, to: EpicStateProposed},
		{name: "proposed to done", from: EpicStateProposed, to: EpicStateDone},
		{name: "done to concept", from: EpicStateDone, to: EpicStateConcept},
		{name: "closed to failed", from: EpicStateClosed, to: EpicStateFailed},
		{name: "failed to review", from: EpicStateFailed, to: EpicStateReview},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			epic := Epic{State: tt.from}

			// Act
			if err := epic.TransitionTo(tt.to); err == nil {
				// Assert
				t.Fatal("expected transition to fail")
			}
			// Assert
			if epic.State != tt.from {
				t.Fatalf("failed transition changed state to %s", epic.State)
			}
		})
	}
}

func TestEpic_RecordDraftingPass_ShouldMoveByTheVerdict(t *testing.T) {
	// Arrange
	tests := []struct {
		name     string
		passes   int
		approved bool
		want     EpicState
	}{
		{name: "approval proposes", approved: true, want: EpicStateProposed},
		{name: "changes requested goes back", want: EpicStateChangesRequested},
		{
			// A reviewer and a refiner can volley indefinitely; the limit is
			// what makes that terminate.
			name: "the last pass proposes regardless", passes: MaxDraftingPasses - 1,
			want: EpicStateProposed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange
			epic := Epic{State: EpicStateReview, DraftingPasses: test.passes}

			// Act
			if err := epic.RecordDraftingPass(test.approved); err != nil {
				t.Fatal(err)
			}

			// Assert
			if epic.State != test.want || epic.DraftingPasses != test.passes+1 {
				t.Fatalf("expected (%s, %d), got (%s, %d)",
					test.want, test.passes+1, epic.State, epic.DraftingPasses)
			}
		})
	}
}

func TestEpic_RecordDraftingPass_ShouldNotBurnAPassTheFSMRejected(t *testing.T) {
	// Arrange: Concept can take neither verdict, so nothing may change.
	epic := Epic{State: EpicStateConcept, DraftingPasses: 1}

	// Act
	err := epic.RecordDraftingPass(true)

	// Assert
	if err == nil {
		t.Fatal("expected a pass recorded outside review to fail")
	}
	if epic.State != EpicStateConcept || epic.DraftingPasses != 1 {
		t.Fatalf("rejected pass mutated the epic: %#v", epic)
	}
}

func TestEpic_ForceState_ShouldSetStateTheFSMWouldReject(t *testing.T) {
	// Arrange: Closed to Concept has no event, so only the escape hatch can
	// revive a stranded epic.
	epic := Epic{State: EpicStateClosed}

	// Act
	err := epic.ForceState(EpicStateConcept)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if epic.State != EpicStateConcept {
		t.Fatalf("expected state %s, got %s", EpicStateConcept, epic.State)
	}
}

func TestEpic_ForceState_ShouldRejectUnknownStateWithoutChangingState(t *testing.T) {
	// Arrange
	epic := Epic{State: EpicStateReview}

	// Act
	err := epic.ForceState(EpicState("Unknown"))

	// Assert
	if err == nil {
		t.Fatal("expected unknown state to fail")
	}
	if epic.State != EpicStateReview {
		t.Fatalf("rejected force changed state to %s", epic.State)
	}
}

func TestEpicStateRejectsUnknownState(t *testing.T) {
	// Arrange
	epic := Epic{State: EpicStateConcept}

	// Act
	if err := epic.TransitionTo(EpicState("Unknown")); err == nil {
		// Assert
		t.Fatal("expected unknown state to fail")
	}

	// Assert
	if epic.State != EpicStateConcept {
		t.Fatalf("unknown transition changed state to %s", epic.State)
	}
}

func TestLegalTransitions_ShouldOfferOnlyMovesTheMachineAccepts(t *testing.T) {
	// Arrange & Act & Assert: every offered move must apply cleanly, and every
	// unoffered move must be refused — the dialog built on this list shows
	// exactly what the FSM accepts, no more and no less.
	for _, current := range allEpicStates {
		legal := map[EpicState]bool{}
		for _, next := range LegalTransitions(current) {
			legal[next] = true
			epic := Epic{State: current}
			if err := epic.TransitionTo(next); err != nil {
				t.Fatalf("%s -> %s was offered but refused: %v", current, next, err)
			}
		}
		for _, next := range allEpicStates {
			if next == current || legal[next] {
				continue
			}
			epic := Epic{State: current}
			if err := epic.TransitionTo(next); err == nil {
				t.Fatalf("%s -> %s was not offered but the machine accepts it", current, next)
			}
		}
	}
}

func TestNextApprovalState_ShouldFollowThePipeline(t *testing.T) {
	// Arrange: one step per approval, in the order the pipeline runs.
	steps := map[EpicState]EpicState{
		EpicStateConcept:          EpicStateRefine,
		EpicStateRefine:           EpicStateReview,
		EpicStateReview:           EpicStateProposed,
		EpicStateChangesRequested: EpicStateReview,
		EpicStateProposed:         EpicStateReady,
		EpicStateReady:            EpicStateDone,
	}

	// Act & Assert: every step must also be a legal FSM move, or approving
	// would offer a transition the domain then refuses.
	for current, want := range steps {
		next, ok := NextApprovalState(current)
		if !ok || next != want {
			t.Fatalf("expected %s to approve into %s, got %s ok=%t", current, want, next, ok)
		}
		epic := Epic{State: current}
		if err := epic.TransitionTo(next); err != nil {
			t.Fatalf("approval step %s -> %s is not a legal move: %v", current, next, err)
		}
	}
	for _, terminal := range []EpicState{EpicStateDone, EpicStateClosed, EpicStateFailed} {
		if _, ok := NextApprovalState(terminal); ok {
			t.Fatalf("expected nothing to approve from %s", terminal)
		}
	}
}
