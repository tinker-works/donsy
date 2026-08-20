package usecases

import (
	"fmt"
	"strings"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

// StoreSetupUseCase reports what a project still needs before its worker can do
// anything. The three prerequisites live in three different places — the local
// organisation registry, meta/project.yaml and agents/agent.yaml — so reading
// them together is what this exists for.
type StoreSetupUseCase struct {
	organisations application.OrganisationRegistry
	factory       application.WorkspaceFactory
}

func (u *StoreSetupUseCase) Handle(project domain.Project) (domain.SetupState, error) {
	if project.Name == "" {
		return domain.SetupState{}, fmt.Errorf("project name is required")
	}
	organisations, err := u.organisations.ListOrganisations()
	if err != nil {
		return domain.SetupState{}, err
	}
	workspace := u.factory.Open(project.Name)
	repositories, err := workspace.Repositories()
	if err != nil {
		return domain.SetupState{}, err
	}
	settings, err := workspace.AgentSettings()
	if err != nil {
		return domain.SetupState{}, err
	}
	return domain.SetupState{
		Organisations: len(organisations),
		Repositories:  len(repositories),
		RolesSet:      settings.ConfiguredRoles(),
		RolesTotal:    len(agent.Roles()),
	}, nil
}

type InitialiseStoreCommand struct {
	Project domain.Project
	// Model is an OpenCode model, "provider/model". It is assigned to every
	// role: five separate answers on the setup screen would be five decisions
	// the user has no basis to make yet, and Settings can refine any of them.
	Model   string
	Variant string
	// Repositories are the code repositories the project's epics may touch.
	Repositories []string
}

// InitialiseStoreUseCase makes a freshly cloned store runnable: every role gets
// a model, and the project gets its linked repositories.
//
// The two writes are separate mutations. Roles are written first: a store with
// roles and no repositories can still refine epics, while the reverse can do
// nothing at all, so a failure between the two leaves the more useful half in
// place.
type InitialiseStoreUseCase struct {
	factory application.WorkspaceFactory
}

func (u *InitialiseStoreUseCase) Handle(command InitialiseStoreCommand) error {
	if command.Project.Name == "" {
		return fmt.Errorf("project name is required")
	}
	model := strings.TrimSpace(command.Model)
	if model == "" {
		return fmt.Errorf("a model is required to initialise the store")
	}
	variant := strings.TrimSpace(command.Variant)
	// Setup is a second door into the same linked set the settings screen writes,
	// so it answers to the same ownership rule. Checked before the role write so
	// a refused selection leaves the store entirely untouched.
	workspace := u.factory.Open(command.Project.Name)
	err := workspace.UpdateAgentSettings(
		func(settings *agent.AgentSettings) error {
			if settings.Roles == nil {
				settings.Roles = map[agent.AgentRole]agent.AgentProfile{}
			}
			// Only the fields this command carries are replaced, so a MaxRounds
			// already configured in agent.yaml is not silently reset to unlimited.
			for _, role := range agent.Roles() {
				profile := settings.Roles[role]
				profile.Agent = model
				profile.Variant = variant
				settings.Roles[role] = profile
			}
			return settings.Validate()
		},
	)
	if err != nil {
		return err
	}
	// Nothing to write is not the same as writing nothing: an empty selection
	// would clear a set the user never saw, so leave the file alone instead.
	if len(command.Repositories) == 0 {
		return nil
	}
	return workspace.UpdateRepositories(command.Repositories)
}
