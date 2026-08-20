package application

import "github.com/tinker-works/donsy/internal/application/agent_runtime"

// Registry is the machine-local state store in full: projects, GitHub
// organisations and repositories, and agent sandbox/run bookkeeping. There is one
// implementation and it always provides all of it, so naming the combination
// lets the composition require it at compile time instead of probing the
// project registry for extra capabilities at runtime.
type Registry interface {
	ProjectRegistry
	OrganisationRegistry
	RepositoryRegistry
	agent_runtime.AgentRegistry
}
