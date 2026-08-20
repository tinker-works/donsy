package usecases

import (
	"testing"

	"github.com/tinker-works/donsy/internal/application"
)

// fullFakeRegistry satisfies application.Registry by combining the per-slice
// fakes the other tests already use, since NewUseCases requires the composite.
type fullFakeRegistry struct {
	*fakeRegistry
	*fakeAgentRegistry
	*fakeOrganisationRegistry
	*fakeRepositoryRegistry
}

var _ application.Registry = (*fullFakeRegistry)(nil)

func newFullFakeRegistry() *fullFakeRegistry {
	return &fullFakeRegistry{
		&fakeRegistry{}, &fakeAgentRegistry{},
		&fakeOrganisationRegistry{}, &fakeRepositoryRegistry{},
	}
}

func fullAgentDependencies() *EpicAgentDependencies {
	return &EpicAgentDependencies{
		Sandboxes:      &fakeSandboxManager{},
		Inspector:      fakeSandboxInspector{},
		Runtime:        &fakeAgentRuntime{},
		Builder:        fakeCommandBuilder{},
		Credentials:    &fakeAgentCredentials{},
		Repositories:   &fakeRepositoryWorkspace{},
		IssueTreeStore: fakeIssueTreeStore{},
		Code:           newFakeCodeWorkspace(),
		Output:         &fakeRunOutput{},
		Differ:         &fakeDiffer{},
	}
}

func TestNewUseCases_ShouldLeaveGitHubUseCasesNilWithoutAClient(t *testing.T) {
	// Arrange & Act
	useCases := NewUseCases(
		newFullFakeRegistry(), &fakeFactory{workspace: &fakeWorkspace{}},
		fixedClock{}, nil, fullAgentDependencies(),
	)

	// Assert: everything that would call GitHub must be absent, not broken.
	if useCases.DiscoverOrganisations != nil || useCases.SyncRepositories != nil ||
		useCases.ListRepositories != nil {
		t.Fatal("expected the GitHub-backed use cases to stay nil without a client")
	}
	// Naming a repository or an organisation by hand needs no network, so these
	// must survive. StoreSetup counts organisations to decide whether a project
	// can be set up at all, so it would report a false verdict without them.
	if useCases.AddRepository == nil {
		t.Fatal("expected AddRepository without a GitHub client")
	}
	if useCases.ListOrganisations == nil || useCases.AddOrganisation == nil ||
		useCases.RemoveOrganisation == nil {
		t.Fatal("expected the registry-only organisation use cases without a GitHub client")
	}
	if useCases.StoreSetup == nil || useCases.InitialiseStore == nil {
		t.Fatal("expected the setup use cases to be wired regardless")
	}
	if useCases.ListProjects == nil || useCases.ListEpics == nil ||
		useCases.ListAgentRuns == nil || useCases.ListSandboxes == nil ||
		useCases.ListProjectSummaries == nil || useCases.GetEpic == nil ||
		useCases.CreateEpic == nil || useCases.GetAgentRun == nil {
		t.Fatal("expected the store-backed use cases to be wired regardless")
	}
}

func TestNewUseCases_ShouldLeaveAgentUseCasesNilWithoutAgentDependencies(t *testing.T) {
	// Arrange & Act
	useCases := NewUseCases(
		newFullFakeRegistry(), &fakeFactory{workspace: &fakeWorkspace{}},
		fixedClock{}, &fakeGitHubClient{}, nil,
	)

	// Assert
	if useCases.RunEpicAgent != nil || useCases.RunIssueAgent != nil ||
		useCases.OpenPullRequests != nil || useCases.CancelAgentRun != nil ||
		useCases.ResetIssue != nil ||
		useCases.ReadRunOutput != nil || useCases.ReconcileSandboxes != nil ||
		useCases.CompleteEpic != nil {
		t.Fatal("expected the agent-backed use cases to stay nil without a runtime")
	}
	// The worker's loop must come back empty rather than half-wired.
	loop := useCases.IssueLoop()
	if loop.OpenPullRequests != nil || loop.RunIssueAgent != nil || loop.CompleteEpic != nil {
		t.Fatalf("expected an empty issue loop, got %+v", loop)
	}
}

func TestNewUseCases_ShouldWireEverythingWhenFullySupplied(t *testing.T) {
	// Arrange & Act
	useCases := NewUseCases(
		newFullFakeRegistry(), &fakeFactory{workspace: &fakeWorkspace{}},
		fixedClock{}, &fakeGitHubClient{}, fullAgentDependencies(),
	)

	// Assert
	if useCases.DiscoverOrganisations == nil || useCases.ListOrganisations == nil ||
		useCases.AddOrganisation == nil || useCases.RemoveOrganisation == nil ||
		useCases.SyncRepositories == nil || useCases.ListRepositories == nil {
		t.Fatal("expected the GitHub-backed use cases to be wired")
	}
	if useCases.RunEpicAgent == nil || useCases.RunIssueAgent == nil ||
		useCases.OpenPullRequests == nil || useCases.CancelAgentRun == nil ||
		useCases.ResetIssue == nil ||
		useCases.ReadRunOutput == nil || useCases.ReconcileSandboxes == nil ||
		useCases.CompleteEpic == nil {
		t.Fatal("expected the agent-backed use cases to be wired")
	}
	loop := useCases.IssueLoop()
	if loop.GetEpic == nil || loop.OpenPullRequests == nil || loop.RunIssueAgent == nil ||
		loop.CompleteEpic == nil || loop.ReviewApproved == nil {
		t.Fatalf("expected a fully-populated issue loop, got %+v", loop)
	}
}
