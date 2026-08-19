package domain

import "testing"

func TestSetupState_Complete_ShouldRejectAFreshStore(t *testing.T) {
	// Arrange
	state := SetupState{RolesTotal: 5}

	// Act
	complete := state.Complete()

	// Assert
	if complete {
		t.Fatal("a store with no organisation, repository, or role should not be complete")
	}
	if !state.NeedsOrganisation() || !state.NeedsRepository() || !state.NeedsRoles() {
		t.Fatalf("every step should be outstanding, got %+v", state)
	}
}

func TestSetupState_Complete_ShouldAcceptAFullyConfiguredStore(t *testing.T) {
	// Arrange
	state := SetupState{
		Organisations: 1, Repositories: 1, RolesSet: 5, RolesTotal: 5,
	}

	// Act
	complete := state.Complete()

	// Assert
	if !complete {
		t.Fatalf("expected a fully configured store to be complete, got %+v", state)
	}
}

func TestSetupState_NeedsRoles_ShouldRejectAPartiallyAssignedStore(t *testing.T) {
	// Arrange
	// A store where only the first roles are assigned stalls silently at the
	// first unassigned one, so it must not count as done.
	state := SetupState{
		Organisations: 1, Repositories: 1, RolesSet: 3, RolesTotal: 5,
	}

	// Act
	needsRoles := state.NeedsRoles()

	// Assert
	if !needsRoles {
		t.Fatal("expected 3 of 5 assigned roles to still need setup")
	}
	if state.Complete() {
		t.Fatal("expected a partially assigned store to be incomplete")
	}
}

func TestSetupState_NeedsRoles_ShouldTreatAnUnknownRoleCountAsOutstanding(t *testing.T) {
	// Arrange
	// A zero RolesTotal means the count was never resolved. Reporting that as
	// satisfied would let an unconfigured store through the gate.
	state := SetupState{Organisations: 1, Repositories: 1}

	// Act
	needsRoles := state.NeedsRoles()

	// Assert
	if !needsRoles {
		t.Fatal("expected a zero role total to be outstanding rather than satisfied")
	}
}

func TestSetupState_Complete_ShouldRequireEachStepIndependently(t *testing.T) {
	// Arrange
	full := SetupState{
		Organisations: 1, Repositories: 1, RolesSet: 5, RolesTotal: 5,
	}
	missingOrganisation := full
	missingOrganisation.Organisations = 0
	missingRepository := full
	missingRepository.Repositories = 0

	// Act & Assert
	if missingOrganisation.Complete() {
		t.Fatal("expected a missing organisation to block completion")
	}
	if missingRepository.Complete() {
		t.Fatal("expected a missing repository to block completion")
	}
}
