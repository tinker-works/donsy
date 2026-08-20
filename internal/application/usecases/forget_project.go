package usecases

import (
	"context"
	"errors"
	"fmt"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

type ForgetProjectCommand struct {
	ProjectID uint
}

type ForgetProjectUseCase struct {
	registry application.ProjectRegistry
	// agents and sandboxes are nil when no agent runtime is configured, where a project
	// never had a sandbox to leave behind in the first place.
	agents    agent_runtime.AgentRegistry
	sandboxes agent_runtime.SandboxManager
	creds     agent_runtime.AgentCredentials
	// host is the project's container host, which outlives its sandboxes and
	// so has to be removed separately. Nil when the runtime has no such thing.
	host agent_runtime.ProjectHost
}

// Handle removes the local registration only. The tracker repository and the
// cloned working copy survive, so the project can be re-added from its URL.
//
// What cannot survive is the project's runtime state. Reconciliation reaches a sandbox
// by walking the projects that still exist, so anything left behind here is
// unreachable forever: a real container holding host disk that nothing will
// ever inspect, stop or reclaim. Its sandboxes are therefore torn down before the
// registration goes, and their records with them.
func (u *ForgetProjectUseCase) Handle(ctx context.Context, command ForgetProjectCommand) error {
	if command.ProjectID == 0 {
		return fmt.Errorf("project ID is required")
	}
	if err := u.releaseSandboxes(ctx, command.ProjectID); err != nil {
		// Forgetting must not be blocked by a provider that cannot be reached: the
		// user asked for the project to go. The failure is reported so the leftover
		// instance is known about, and the registration is dropped regardless.
		return errors.Join(err, u.registry.Delete(command.ProjectID))
	}
	return u.registry.Delete(command.ProjectID)
}

func (u *ForgetProjectUseCase) releaseSandboxes(ctx context.Context, projectID uint) error {
	if u.agents == nil || u.sandboxes == nil {
		return nil
	}
	sandboxes, err := u.agents.ListSandboxes(projectID)
	if err != nil {
		return err
	}
	var errs []error
	for _, sandbox := range sandboxes {
		// Absent is the status of a sandbox already reclaimed, where there is no instance
		// to delete and the runtime would only report one it cannot find.
		if sandbox.Status == agent.SandboxStatusAbsent {
			continue
		}
		if err := u.sandboxes.Delete(ctx, sandbox.Ref()); err != nil {
			errs = append(errs, fmt.Errorf("delete sandbox %q: %w", sandbox.Name, err))
			continue
		}
		if u.creds != nil {
			if err := u.creds.Discard(sandbox.Name); err != nil {
				errs = append(errs, fmt.Errorf("discard credentials of sandbox %q: %w", sandbox.Name, err))
			}
		}
	}
	// The host goes last, after the containers inside it: it takes their disk
	// image with it, so anything not deleted first is deleted anyway — but only
	// this order leaves the credentials discarded rather than orphaned.
	//
	// Deleting rather than stopping, and only here. The sweep must never delete
	// a host: it would discard the image cache, which is the whole reason the
	// host is per project. A forgotten project has no next round to warm it for.
	if u.host != nil {
		if err := u.host.DeleteProfile(ctx, projectID); err != nil {
			errs = append(errs, fmt.Errorf("delete host of project %d: %w", projectID, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return u.agents.DeleteProjectRuntime(projectID)
}
