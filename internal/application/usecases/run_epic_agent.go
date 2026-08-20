package usecases

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/application/prompts"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
	"github.com/tinker-works/donsy/internal/repositorypath"
)

// DefaultMaxRounds caps a role whose profile never configured a limit. Zero
// used to mean unlimited, which in practice meant a persistently failing role
// retried every tick forever — observed as 113 straight failed reviewer
// rounds. A full drafting cycle needs six successful rounds, so ten leaves
// room for transient failures without letting a broken role run all afternoon.
const DefaultMaxRounds = 10

// roundLimit is the effective cap for one role's rounds on one subject.
func roundLimit(profile agent.AgentProfile) int {
	if profile.MaxRounds > 0 {
		return profile.MaxRounds
	}
	return DefaultMaxRounds
}

type RunEpicAgentCommand struct {
	Project domain.Project
	EpicID  string
	Spec    agent_runtime.SandboxSpec
}

// RunEpicAgentUseCase advances one Concept -> Refine -> Review workflow step.
// Each call completes one role invocation; a scheduler can call it repeatedly.
type RunEpicAgentUseCase struct {
	factory        application.WorkspaceFactory
	registry       agent_runtime.AgentRegistry
	sandboxes      agent_runtime.SandboxManager
	runtime        agent_runtime.AgentRuntime
	builder        application.AgentCommandBuilder
	creds          agent_runtime.AgentCredentials
	repos          agent_runtime.RepositoryWorkspace
	issueTreeStore agent_runtime.IssueTreeStore
	clock          application.Clock
	supervisor     *RunSupervisor
	// output feeds the round's stall guard, which watches the transcript for
	// growth. Optional: without it a round keeps only its runaway guard.
	output agent_runtime.RunOutput
	// roundTimeout bounds one invocation. Zero means maxRoundDuration.
	roundTimeout time.Duration
	// roundSilence and silenceSample shorten the stall guard for tests. Zero
	// means maxRoundSilence and silenceInterval.
	roundSilence  time.Duration
	silenceSample time.Duration
}

func (u *RunEpicAgentUseCase) Handle(ctx context.Context, command RunEpicAgentCommand) error {
	workspace := u.factory.Open(command.Project.Name)
	currentEpic, err := workspace.ReadEpic(command.EpicID)
	if err != nil {
		return err
	}
	role, ok := epicRole(currentEpic.State)
	if !ok {
		return nil
	}
	settings, err := workspace.AgentSettings()
	if err != nil {
		return err
	}
	profile, err := settings.Profile(role)
	if err != nil {
		return err
	}
	if settings.SetupScript != "" {
		content, err := workspace.ReadFile(settings.SetupScript)
		if err != nil {
			return err
		}
		command.Spec.SetupScript = content
	}
	if len(currentEpic.Repositories) == 0 {
		return fmt.Errorf("epic %q has no repository scope", currentEpic.ID)
	}
	// The scope is checked again here, not only where it was linked: an epic's
	// repositories are recorded in the tree, and a round is the point where they
	// would be cloned and written to.
	epicSubject := agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: currentEpic.ID}
	if command.Spec.Sandbox.ProjectID != command.Project.ID ||
		command.Spec.Sandbox.Role != role || command.Spec.Sandbox.Subject != epicSubject {
		return fmt.Errorf("sandbox does not belong to epic %q and role %q", currentEpic.ID, role)
	}
	runs, err := u.registry.ListAgentRuns(command.Project.ID, command.Spec.Sandbox.Subject)
	if err != nil {
		return err
	}
	if err := u.round().reapOrphaned(runs); err != nil {
		return err
	}
	if role == agent.AgentRoleRefiner && currentEpic.State != epicpkg.EpicStateRefine {
		change := func(current *epicpkg.Epic) error {
			return current.Apply(epicpkg.EpicEventRefine)
		}
		if err := workspace.UpdateEpic(currentEpic.ID, change); err != nil {
			return err
		}
		currentEpic.State = epicpkg.EpicStateRefine
	}

	next := nextRound(runs)
	if limit := roundLimit(profile); next > limit {
		return u.failEpicForRoundLimit(workspace, currentEpic, role, limit)
	}

	// Admission control: never start a sandbox the host cannot afford alongside every
	// other sandbox currently running. A "no" here just leaves the epic in its
	// current, still-recognized state for a later tick to retry - it is not a
	// failure, it is the runner queue's backpressure.
	//
	// It comes before the mounts are staged below because staging them clones or
	// pulls every repository in the epic, which is minutes of network for a round
	// that may then be turned away. The reservation is held for the whole round,
	// which is exactly how long this sandbox occupies the host.
	release, admitted, err := u.sandboxes.Reserve(ctx, command.Spec)
	if err != nil {
		return err
	}
	if !admitted {
		return nil
	}
	defer release()

	credentials, err := u.creds.OpenCodeMount(command.Spec.Sandbox.Name, profile.Agent)
	if err != nil {
		return err
	}
	command.Spec.Mounts = append(command.Spec.Mounts, credentials)
	treePath, err := u.issueTreeStore.Write(command.Spec.Sandbox.Name, currentEpic)
	if err != nil {
		return err
	}
	command.Spec.Mounts = append(command.Spec.Mounts, agent_runtime.SandboxMount{
		HostLocation: treePath, GuestLocation: "/work/issues", Writable: role == agent.AgentRoleRefiner,
	})
	for _, repository := range currentEpic.Repositories {
		path, err := u.repos.Ensure(ctx, currentEpic.ID, repository)
		if err != nil {
			return err
		}
		folder := repositorypath.Encode(repository)
		guestPath := "/work/repos/" + folder
		command.Spec.Mounts = append(command.Spec.Mounts,
			agent_runtime.SandboxMount{
				// Test commands write build outputs and test databases beside source.
				// AgentWorkspace resets this disposable checkout before the next round.
				HostLocation: path, GuestLocation: guestPath, Writable: true,
			},
			agent_runtime.SandboxMount{
				HostLocation:  filepath.Join(path, ".git"),
				GuestLocation: guestPath + "/.git",
			},
		)
	}
	round := u.round()
	run, runCtx, finish, err := round.begin(ctx, &command.Spec, profile, next, runs)
	if err != nil {
		return err
	}
	defer finish()
	prompt, err := prompts.Epic(withRepositoryFolders(currentEpic), role, run.SessionMode)
	if err != nil {
		return round.fail(run, err)
	}
	answer, err := round.invoke(runCtx, &command.Spec, &run, prompt, nil)
	if err != nil {
		return u.reportFailure(workspace, currentEpic, role, err)
	}
	if role == agent.AgentRoleIssueReviewer && answer == "" {
		return u.reportFailure(workspace, currentEpic, role,
			round.fail(run, fmt.Errorf("agent did not return a marked answer")))
	}
	if err := u.completeEpicRun(workspace, currentEpic, role, treePath, answer); err != nil {
		return u.reportFailure(workspace, currentEpic, role, round.fail(run, err))
	}
	return round.succeed(run)
}

// reportFailure leaves a failed round on the epic's root issue; a round that
// dies only in the run registry is invisible on the board. Cancellations are
// skipped — those were asked for, and the Runs screen already shows them. The
// comment is best-effort: the original cause must survive, so a comment error
// is joined, never substituted.
func (u *RunEpicAgentUseCase) reportFailure(
	workspace application.Workspace,
	currentEpic epicpkg.Epic,
	role agent.AgentRole,
	cause error,
) error {
	if cause == nil || errors.Is(cause, errRunCancelled) {
		return cause
	}
	commentErr := workspace.UpdateEpic(
		currentEpic.ID,
		func(current *epicpkg.Epic) error {
			root, err := current.RootIssue()
			if err != nil {
				return err
			}
			comment, err := epicpkg.CreateComment(
				string(role), fmt.Sprintf("The %s round failed: %s", role, cause),
			)
			if err != nil {
				return err
			}
			return current.AddIssueComment(root.ID, comment)
		},
	)
	if commentErr != nil {
		return errors.Join(cause, fmt.Errorf("record failure comment: %w", commentErr))
	}
	return cause
}

func (u *RunEpicAgentUseCase) round() agentRound {
	return agentRound{
		registry: u.registry, sandboxes: u.sandboxes, runtime: u.runtime,
		builder: u.builder, clock: u.clock, supervisor: u.supervisor,
		output: u.output, timeout: u.roundTimeout,
		silence: u.roundSilence, sample: u.silenceSample,
	}
}

func withRepositoryFolders(epic epicpkg.Epic) epicpkg.Epic {
	epics := append([]string(nil), epic.Repositories...)
	for index, repository := range epics {
		epics[index] = repositorypath.Encode(repository)
	}
	epic.Repositories = epics
	return epic
}

func (u *RunEpicAgentUseCase) completeEpicRun(
	workspace application.Workspace,
	currentEpic epicpkg.Epic,
	role agent.AgentRole,
	treePath string,
	answer string,
) error {
	return workspace.UpdateEpic(currentEpic.ID, func(current *epicpkg.Epic) error {
		if role == agent.AgentRoleRefiner {
			refined, err := u.issueTreeStore.Read(treePath, *current)
			if err != nil {
				return err
			}
			// The host writes root.md itself, so a refiner that wrote nothing
			// still reads back as a valid epic — just one with no work in it.
			// Accepting that sends an epic to review with nothing to review and
			// then to coding with nothing to code, which is how an epic ends up
			// looking finished while never having been planned at all.
			if !hasPlannedWork(refined) {
				return fmt.Errorf(
					"refiner produced no issues: the epic tree still has only its root issue",
				)
			}
			*current = refined
			return current.Apply(epicpkg.EpicEventReview)
		}
		root, err := current.RootIssue()
		if err != nil {
			return err
		}
		critique, err := epicpkg.CreateComment("issue-reviewer", answer)
		if err != nil {
			return err
		}
		for index := range current.Issues {
			if current.Issues[index].ID == root.ID {
				if err := current.Issues[index].AddComment(critique); err != nil {
					return err
				}
			}
		}
		return current.RecordDraftingPass(u.builder.ReviewApproved(answer))
	})
}

// failEpicForRoundLimit stops an epic from retrying forever once its configured round
// limit is exhausted, so a persistently failing or endlessly bounced-back role stops
// consuming sandbox time and needs a human to intervene instead.
func (u *RunEpicAgentUseCase) failEpicForRoundLimit(
	workspace application.Workspace,
	currentEpic epicpkg.Epic,
	role agent.AgentRole,
	limit int,
) error {
	return workspace.UpdateEpic(currentEpic.ID, func(current *epicpkg.Epic) error {
		comment, err := epicpkg.CreateComment(
			"go-merge",
			fmt.Sprintf("%s reached its round limit of %d without completing.", role, limit),
		)
		if err != nil {
			return err
		}
		root, err := current.RootIssue()
		if err != nil {
			return err
		}
		for index := range current.Issues {
			if current.Issues[index].ID == root.ID {
				if err := current.Issues[index].AddComment(comment); err != nil {
					return err
				}
				break
			}
		}
		return current.Apply(epicpkg.EpicEventFail)
	})
}

// hasPlannedWork reports whether an epic's tree holds any issue below its root.
// The root issue is the epic restated, so on its own it is not a plan.
func hasPlannedWork(current epicpkg.Epic) bool {
	for _, issue := range current.Issues {
		if issue.ParentID != "" && issue.State != epicpkg.IssueStateClosed {
			return true
		}
	}
	return false
}

func epicRole(state epicpkg.EpicState) (agent.AgentRole, bool) {
	switch state {
	// EpicStateRefine is included so an epic interrupted mid-refine (the epic's
	// state is committed before its sandbox/run even exists, see Handle) stays
	// recognized by the worker and can be picked up again, instead of stalling
	// forever in a state epicRole no longer maps to a role.
	case epicpkg.EpicStateConcept, epicpkg.EpicStateChangesRequested, epicpkg.EpicStateRefine:
		return agent.AgentRoleRefiner, true
	case epicpkg.EpicStateReview:
		return agent.AgentRoleIssueReviewer, true
	default:
		return "", false
	}
}
