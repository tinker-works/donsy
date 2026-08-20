package usecases

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

type ResetIssueCommand struct {
	Project       domain.Project
	EpicID        string
	PullRequestID string
}

// ResetIssueUseCase removes the local and remote execution state behind an open
// pull request, then closes that record to make its issue eligible for a clean
// new branch. The old pull request remains as the audit trail of discarded work.
type ResetIssueUseCase struct {
	factory   application.WorkspaceFactory
	registry  agent_runtime.AgentRegistry
	code      agent_runtime.CodeWorkspace
	sandboxes agent_runtime.SandboxManager
	creds     agent_runtime.AgentCredentials
	output    agent_runtime.RunOutput
	cancel    *CancelAgentRunUseCase
}

func (u *ResetIssueUseCase) Handle(ctx context.Context, command ResetIssueCommand) error {
	if command.EpicID == "" || command.PullRequestID == "" {
		return fmt.Errorf("epic and pull request IDs are required")
	}
	if u.code == nil || u.sandboxes == nil || u.registry == nil {
		return fmt.Errorf("issue reset requires an agent runtime")
	}

	workspace := u.factory.Open(command.Project.Name)
	current, err := workspace.ReadEpic(command.EpicID)
	if err != nil {
		return err
	}
	pullRequest, err := resettablePullRequest(current, command.PullRequestID)
	if err != nil {
		return err
	}
	subject := agent.AgentSubject{Kind: agent.AgentSubjectIssue, ID: pullRequest.IssueID}
	runs, err := u.registry.ListAgentRuns(command.Project.ID, subject)
	if err != nil {
		return err
	}
	if err := u.cancelAndDrain(ctx, command.Project.ID, subject, runs); err != nil {
		return err
	}

	sandboxes, err := u.registry.ListSandboxes(command.Project.ID)
	if err != nil {
		return err
	}
	var cleanup []error
	for _, sandbox := range sandboxes {
		if sandbox.Subject != subject || sandbox.Status == agent.SandboxStatusAbsent {
			continue
		}
		if err := u.sandboxes.Delete(ctx, sandbox.Ref()); err != nil {
			cleanup = append(cleanup, fmt.Errorf("delete sandbox %q: %w", sandbox.Name, err))
			continue
		}
		if u.creds != nil {
			if err := u.creds.Discard(sandbox.Name); err != nil {
				cleanup = append(cleanup,
					fmt.Errorf("discard credentials for sandbox %q: %w", sandbox.Name, err))
			}
		}
	}
	checkout := agent_runtime.CodeCheckout{
		EpicID: command.EpicID, IssueID: pullRequest.IssueID, Repository: pullRequest.Repository,
	}
	if err := u.code.DeleteBranch(ctx, checkout, pullRequest.Head); err != nil {
		cleanup = append(cleanup, fmt.Errorf("delete branch %q: %w", pullRequest.Head, err))
	}
	if u.output != nil {
		for _, run := range runs {
			if err := u.output.Discard(run.ID); err != nil {
				cleanup = append(cleanup, fmt.Errorf("discard transcript %q: %w", run.ID, err))
			}
		}
	}
	if err := errors.Join(cleanup...); err != nil {
		return err
	}
	if err := u.registry.DeleteSubjectRuntime(command.Project.ID, subject); err != nil {
		return err
	}
	return workspace.UpdateEpic(
		command.EpicID, func(target *epic.Epic) error {
			return target.TransitionPullRequest(command.PullRequestID, epic.PullRequestClosed)
		},
	)
}

func (u *ResetIssueUseCase) cancelAndDrain(
	ctx context.Context, projectID uint, subject agent.AgentSubject, runs []agent.AgentRun,
) error {
	for _, run := range runs {
		if !isLiveAgentRunStatus(run.Status) {
			continue
		}
		if u.cancel == nil {
			return fmt.Errorf("cannot cancel live agent run %q", run.ID)
		}
		if _, err := u.cancel.Handle(CancelAgentRunCommand{RunID: run.ID}); err != nil {
			return err
		}
	}
	for {
		runs, err := u.registry.ListAgentRuns(projectID, subject)
		if err != nil {
			return err
		}
		live := false
		for _, run := range runs {
			live = live || isLiveAgentRunStatus(run.Status)
		}
		if !live {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func resettablePullRequest(current epic.Epic, pullRequestID string) (epic.PullRequest, error) {
	for _, pullRequest := range current.PullRequests {
		if pullRequest.ID != pullRequestID {
			continue
		}
		if pullRequest.Status != epic.PullRequestOpen {
			return epic.PullRequest{}, fmt.Errorf("pull request %q is not open", pullRequestID)
		}
		return pullRequest, nil
	}
	return epic.PullRequest{}, fmt.Errorf("pull request %q not found", pullRequestID)
}
