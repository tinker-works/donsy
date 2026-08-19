package domain

// SetupState is what a project still needs before its worker can do anything.
//
// It holds counts rather than the aggregates they came from so this package
// stays dependency-free: domain/epic and domain/agent both import domain, so an
// import in the other direction would be a cycle.
type SetupState struct {
	// Organisations is how many GitHub organisations are registered locally.
	// Repository discovery iterates them, so zero means the repository pool can
	// never be populated.
	Organisations int
	// Repositories is how many code repositories the project is linked to.
	Repositories int
	// RolesSet is how many agent roles have a model assigned; RolesTotal is how
	// many exist. A role with no model is silently skipped by the worker.
	RolesSet   int
	RolesTotal int
}

// Complete reports whether the project is ready to run work.
func (s SetupState) Complete() bool {
	return !s.NeedsOrganisation() && !s.NeedsRepository() && !s.NeedsRoles()
}

func (s SetupState) NeedsOrganisation() bool { return s.Organisations < 1 }

func (s SetupState) NeedsRepository() bool { return s.Repositories < 1 }

// NeedsRoles is true until every role has a model. A partially assigned store
// runs some of the pipeline and stalls silently at the first unassigned role,
// which is the failure this whole state exists to prevent.
func (s SetupState) NeedsRoles() bool { return s.RolesTotal < 1 || s.RolesSet < s.RolesTotal }
