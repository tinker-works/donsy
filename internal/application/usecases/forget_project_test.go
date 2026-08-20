package usecases

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

func TestForgetProjectUseCase_Handle_ShouldRemoveOnlyTheLocalRegistration(t *testing.T) {
	// Arrange
	registry := &fakeRegistry{projects: []domain.Project{
		{ID: 1, Name: "One"},
		{ID: 2, Name: "Two"},
	}}
	useCase := &ForgetProjectUseCase{registry: registry}

	// Act
	err := useCase.Handle(context.Background(), ForgetProjectCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.projects) != 1 || registry.projects[0].ID != 2 {
		t.Fatalf("expected only project 2 to remain, got %+v", registry.projects)
	}
}

func TestForgetProjectUseCase_Handle_ShouldRequireAProjectID(t *testing.T) {
	// Arrange
	registry := &fakeRegistry{projects: []domain.Project{{ID: 1, Name: "One"}}}
	useCase := &ForgetProjectUseCase{registry: registry}

	// Act
	err := useCase.Handle(context.Background(), ForgetProjectCommand{})

	// Assert
	if err == nil {
		t.Fatal("expected a missing project ID to be rejected")
	}
	if len(registry.projects) != 1 {
		t.Fatalf("expected nothing to be deleted, got %+v", registry.projects)
	}
}

func TestForgetProjectUseCase_Handle_ShouldTearDownTheProjectsSandboxes(t *testing.T) {
	// Reconciliation reaches a sandbox by walking the projects that still exist, so a sandbox
	// left behind here is unreachable forever — a real container holding host
	// disk that nothing will ever inspect, stop or reclaim again.
	// Arrange
	registry := &fakeRegistry{projects: []domain.Project{{ID: 1, Name: "One"}}}
	agents := &fakeAgentRegistry{sandboxes: []agent.Sandbox{
		{
			ID: "sandbox-1", ProjectID: 1, Name: "live-sandbox", Role: agent.AgentRoleCoding,
			Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
			Status:  agent.SandboxStatusStopped,
		},
		{
			ID: "sandbox-2", ProjectID: 1, Name: "already-gone-sandbox", Role: agent.AgentRoleRefiner,
			Subject: agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: "epic-1"},
			Status:  agent.SandboxStatusAbsent,
		},
	}}
	sandboxes := &fakeSandboxManager{}
	creds := &fakeAgentCredentials{}
	useCase := &ForgetProjectUseCase{
		registry: registry, agents: agents, sandboxes: sandboxes, creds: creds,
	}

	// Act
	err := useCase.Handle(context.Background(), ForgetProjectCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	// The reclaimed one has no container left to delete, so the runtime is not asked to
	// find one it cannot.
	if len(sandboxes.deleted) != 1 || sandboxes.deleted[0] != "live-sandbox" {
		t.Fatalf("expected only the surviving instance to be deleted, got %#v", sandboxes.deleted)
	}
	if len(creds.discarded) != 1 || creds.discarded[0] != "live-sandbox" {
		t.Fatalf("expected its credentials to be discarded, got %#v", creds.discarded)
	}
	if len(agents.sandboxes) != 0 {
		t.Fatalf("expected the sandbox records to be dropped, got %#v", agents.sandboxes)
	}
	if len(registry.projects) != 0 {
		t.Fatalf("expected the registration to be dropped, got %#v", registry.projects)
	}
}

func TestForgetProjectUseCase_Handle_ShouldForgetEvenWhenTeardownFails(t *testing.T) {
	// The user asked for the project to go. A provider that cannot be reached must
	// not make it un-forgettable — the leftover is reported instead.
	// Arrange
	registry := &fakeRegistry{projects: []domain.Project{{ID: 1, Name: "One"}}}
	agents := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "wedged-sandbox", Role: agent.AgentRoleCoding,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
		Status:  agent.SandboxStatusStopped,
	}}}
	useCase := &ForgetProjectUseCase{
		registry: registry, agents: agents,
		sandboxes: &fakeSandboxManager{deleteErr: fmt.Errorf("provider offline")},
	}

	// Act
	err := useCase.Handle(context.Background(), ForgetProjectCommand{ProjectID: 1})

	// Assert
	if err == nil {
		t.Fatal("expected the leftover instance to be reported")
	}
	if len(registry.projects) != 0 {
		t.Fatalf("expected the project to be forgotten anyway, got %#v", registry.projects)
	}
}

// The machine outlives the containers inside it, so forgetting a project has to
// remove it separately — and only here. The sweep must never delete a host: it
// would discard the image cache, which is the whole reason the host is per
// project, and a forgotten project has no next round to warm it for.
func TestForgetProjectUseCase_Handle_ShouldDeleteTheProjectsHostAfterItsSandboxes(t *testing.T) {
	// Arrange
	registry := &fakeRegistry{projects: []domain.Project{{ID: 1, Name: "One"}}}
	agents := &fakeAgentRegistry{sandboxes: []agent.Sandbox{{
		ID: "sandbox-1", ProjectID: 1, Name: "live-sandbox", Role: agent.AgentRoleCoding,
		Subject: agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: "issue-1"},
		Status:  agent.SandboxStatusStopped,
	}}}
	host := &fakeProjectHost{}
	useCase := &ForgetProjectUseCase{
		registry: registry, agents: agents, sandboxes: &fakeSandboxManager{},
		creds: &fakeAgentCredentials{}, host: host,
	}

	// Act
	err := useCase.Handle(context.Background(), ForgetProjectCommand{ProjectID: 1})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(host.deleted) != 1 || host.deleted[0] != 1 {
		t.Fatalf("expected the project's host deleted, got %v", host.deleted)
	}
	if len(host.stopped) != 0 {
		t.Fatalf("expected a forgotten project's host deleted rather than stopped, got %v",
			host.stopped)
	}
}

// Forgetting must not be blocked by a runtime that cannot be reached: the user
// asked for the project to go. The failure is reported so the leftover machine
// is known about, and the registration is dropped regardless.
func TestForgetProjectUseCase_Handle_ShouldForgetEvenWhenTheHostSurvives(t *testing.T) {
	// Arrange
	registry := &fakeRegistry{projects: []domain.Project{{ID: 1, Name: "One"}}}
	useCase := &ForgetProjectUseCase{
		registry:  registry,
		agents:    &fakeAgentRegistry{},
		sandboxes: &fakeSandboxManager{},
		creds:     &fakeAgentCredentials{},
		host:      &failingProjectHost{},
	}

	// Act
	err := useCase.Handle(context.Background(), ForgetProjectCommand{ProjectID: 1})

	// Assert
	if err == nil {
		t.Fatal("expected the leftover machine to be reported")
	}
	if len(registry.projects) != 0 {
		t.Fatalf("expected the registration dropped anyway, got %#v", registry.projects)
	}
}

type failingProjectHost struct{}

func (failingProjectHost) ReapExpiredContainers(context.Context, uint, time.Time, time.Time) (bool, error) {
	return false, nil
}

func (failingProjectHost) StopProfile(context.Context, uint) (bool, error) { return true, nil }

func (failingProjectHost) DeleteProfile(context.Context, uint) error {
	return fmt.Errorf("colima is not installed")
}
