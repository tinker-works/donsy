package usecases

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/application/prompts"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
	"github.com/tinker-works/donsy/internal/repositorypath"
)

type RunIssueAgentCommand struct {
	Project domain.Project
	EpicID  string
	IssueID string
	Spec    agent_runtime.SandboxSpec
}

// RunIssueAgentUseCase advances one issue's pull request by a single round:
// either a coding pass that publishes commits, or a review pass that records
// a verdict. Which one runs is decided by issueRole from the record's state.
type RunIssueAgentUseCase struct {
	factory        application.WorkspaceFactory
	registry       agent_runtime.AgentRegistry
	sandboxes      agent_runtime.SandboxManager
	runtime        agent_runtime.AgentRuntime
	builder        application.AgentCommandBuilder
	creds          agent_runtime.AgentCredentials
	code           agent_runtime.CodeWorkspace
	repos          agent_runtime.RepositoryWorkspace
	issueTreeStore agent_runtime.IssueTreeStore
	clock          application.Clock
	supervisor     *RunSupervisor
	// output feeds the round's stall guard, which watches the transcript for
	// growth. Optional: without it a round keeps only its runaway guard.
	output agent_runtime.RunOutput
	// roundTimeout bounds one invocation. Zero means maxRoundDuration.
	roundTimeout time.Duration
}

func (u *RunIssueAgentUseCase) Handle(ctx context.Context, command RunIssueAgentCommand) error {
	workspace := u.factory.Open(command.Project.Name)
	current, err := workspace.ReadEpic(command.EpicID)
	if err != nil {
		return err
	}
	issue, err := current.FindIssue(command.IssueID)
	if err != nil {
		return err
	}
	pullRequest, ok := current.OpenPullRequestFor(command.IssueID)
	if !ok {
		return nil
	}
	role, ok := IssueRole(current, pullRequest)
	if !ok {
		return nil
	}
	settings, err := workspace.AgentSettings()
	if err != nil {
		return err
	}
	repoSettings, err := workspace.RepositorySettings(pullRequest.Repository)
	if err != nil {
		return err
	}
	// effective folds the repository's override over agent.yaml, so a
	// repository.yaml that only sets one key still inherits the rest from the
	// project-wide settings rather than losing them.
	effective := settings.Override(repoSettings)
	profile, err := effective.Profile(role)
	if err != nil {
		return err
	}
	if effective.SetupScript != "" {
		content, err := workspace.ReadFile(effective.SetupScript)
		if err != nil {
			return err
		}
		command.Spec.SetupScript = content
	}
	if command.Spec.Sandbox.ProjectID != command.Project.ID || command.Spec.Sandbox.Role != role ||
		command.Spec.Sandbox.Subject != (agent.AgentSubject{
			Kind: agent.AgentSubjectIssue, ID: issue.ID,
		}) {
		return fmt.Errorf("sandbox does not belong to issue %q and role %q", issue.ID, role)
	}
	runs, err := u.registry.ListAgentRuns(command.Project.ID, command.Spec.Sandbox.Subject)
	if err != nil {
		return err
	}
	if err := u.round().reapOrphaned(runs); err != nil {
		return err
	}
	// Admission control: never start a sandbox the host cannot afford alongside every
	// other sandbox currently running. A "no" here just leaves the pull request
	// where it is for a later tick to retry — it is backpressure, not a failure.
	//
	// The reservation is held for the whole round rather than released once the sandbox
	// is up, because that is how long the sandbox occupies the host. One deferred
	// release covers every path out of here, panics included.
	release, admitted, err := u.sandboxes.Reserve(ctx, command.Spec)
	if err != nil {
		return err
	}
	if !admitted {
		return nil
	}
	defer release()

	checkout := agent_runtime.CodeCheckout{
		EpicID: current.ID, IssueID: issue.ID, Repository: pullRequest.Repository,
	}
	repoPath, err := u.code.Checkout(ctx, checkout, pullRequest.Head, pullRequest.Base)
	if err != nil {
		return err
	}
	base, err := u.code.Resolve(checkout, pullRequest.Base)
	if err != nil {
		return err
	}
	if err := u.mount(
		ctx, &command.Spec, current, pullRequest.Repository, repoPath, profile.Agent,
	); err != nil {
		return err
	}

	round := u.round()
	run, runCtx, finish, err := round.begin(ctx, &command.Spec, profile, nextRound(runs), runs)
	if err != nil {
		return err
	}
	defer finish()
	// run.SessionMode is read rather than recomputed: begin may have downgraded
	// it to fresh because the sandbox had to be rebuilt, and the prompt has to agree
	// with the session the engine will actually be given.
	prompt, err := prompts.Issue(
		issueContext(current, issue, pullRequest, base, role, run.SessionMode),
		role, run.SessionMode,
	)
	if err != nil {
		return round.fail(run, err)
	}
	answer, err := round.invoke(runCtx, &command.Spec, &run, prompt, map[string]string{
		"GO_MERGE_DOCKER_BIND_SOURCE": repoPath,
	})
	if err != nil {
		return u.reportFailure(workspace, current, pullRequest, role, err)
	}
	if role == agent.AgentRolePRReviewer && answer == "" {
		return u.reportFailure(workspace, current, pullRequest, role,
			round.fail(run, fmt.Errorf("agent did not return a marked answer")))
	}
	if err := u.complete(
		ctx, workspace, current, pullRequest, checkout, role, base, answer,
	); err != nil {
		return u.reportFailure(workspace, current, pullRequest, role, round.fail(run, err))
	}
	return round.succeed(run)
}

// reportFailure leaves a failed round on the pull request's thread; a round
// that dies only in the run registry is invisible on the board and to the next
// agent reading the conversation. Cancellations are skipped — those were asked
// for, and the Runs screen already shows them. The comment is best-effort: the
// original cause must survive, so a comment error is joined, never substituted.
func (u *RunIssueAgentUseCase) reportFailure(
	workspace application.Workspace,
	current epicpkg.Epic,
	pullRequest epicpkg.PullRequest,
	role agent.AgentRole,
	cause error,
) error {
	if cause == nil || errors.Is(cause, errRunCancelled) {
		return cause
	}
	commentErr := workspace.UpdateEpic(
		current.ID,
		func(target *epicpkg.Epic) error {
			comment, err := epicpkg.CreateComment(
				string(role), fmt.Sprintf("The %s round failed: %s", role, cause),
			)
			if err != nil {
				return err
			}
			return target.AddPullRequestComment(pullRequest.ID, comment)
		},
	)
	if commentErr != nil {
		return errors.Join(cause, fmt.Errorf("record failure comment: %w", commentErr))
	}
	return cause
}

// mount gives the guest a writable checkout of the repository it is implementing
// and read-only checkouts for the other repositories named by the epic. The prompt
// exposes those references at /work/repos, so each advertised path must be mounted.
// The active checkout is mounted a second time at its host path so Docker in the VM
// can resolve that path as a bind source.
func (u *RunIssueAgentUseCase) mount(
	ctx context.Context,
	spec *agent_runtime.SandboxSpec,
	current epicpkg.Epic,
	activeRepository, repoPath, model string,
) error {
	credentials, err := u.creds.OpenCodeMount(spec.Sandbox.Name, model)
	if err != nil {
		return err
	}
	spec.Mounts = append(spec.Mounts, credentials)
	treePath, err := u.issueTreeStore.Write(spec.Sandbox.Name, current)
	if err != nil {
		return err
	}
	spec.Mounts = append(spec.Mounts,
		agent_runtime.SandboxMount{HostLocation: treePath, GuestLocation: "/work/issues"},
		agent_runtime.SandboxMount{
			HostLocation: repoPath, GuestLocation: "/work/repo", Writable: true,
		},
		agent_runtime.SandboxMount{
			HostLocation:  filepath.Join(repoPath, ".git"),
			GuestLocation: "/work/repo/.git",
		},
		agent_runtime.SandboxMount{HostLocation: repoPath, GuestLocation: repoPath},
	)
	for _, repository := range current.Repositories {
		if repository == activeRepository {
			continue
		}
		path, err := u.repos.Ensure(ctx, current.ID, repository)
		if err != nil {
			return err
		}
		guestPath := "/work/repos/" + repositorypath.Encode(repository)
		spec.Mounts = append(spec.Mounts,
			agent_runtime.SandboxMount{HostLocation: path, GuestLocation: guestPath},
			agent_runtime.SandboxMount{
				HostLocation:  filepath.Join(path, ".git"),
				GuestLocation: guestPath + "/.git",
			},
		)
	}
	return nil
}

func (u *RunIssueAgentUseCase) round() agentRound {
	return agentRound{
		registry: u.registry, sandboxes: u.sandboxes, runtime: u.runtime,
		builder: u.builder, clock: u.clock, supervisor: u.supervisor,
		output: u.output, timeout: u.roundTimeout,
	}
}

func (u *RunIssueAgentUseCase) complete(
	ctx context.Context,
	workspace application.Workspace,
	current epicpkg.Epic,
	pullRequest epicpkg.PullRequest,
	checkout agent_runtime.CodeCheckout,
	role agent.AgentRole,
	base string,
	answer string,
) error {
	switch role {
	case agent.AgentRoleCoding:
		return u.completeCoding(ctx, workspace, current, pullRequest, checkout, base, answer)
	case agent.AgentRoleMerge:
		return u.completeMerge(ctx, workspace, current, pullRequest, checkout, answer)
	}
	return u.completeReview(workspace, current, pullRequest, checkout, base, answer)
}

// completeCoding commits whatever the agent left, checks it, and publishes.
// The host commits rather than the agent so that an unfinished round still
// produces something a reviewer can read — including an empty commit.
func (u *RunIssueAgentUseCase) completeCoding(
	ctx context.Context,
	workspace application.Workspace,
	current epicpkg.Epic,
	pullRequest epicpkg.PullRequest,
	checkout agent_runtime.CodeCheckout,
	base string,
	answer string,
) error {
	message := fmt.Sprintf("ai: round %d on issue %s", pullRequest.Rounds+1, pullRequest.IssueID)
	if _, err := u.code.CommitAll(checkout, message); err != nil {
		return err
	}
	if err := u.gate(checkout, base); err != nil {
		return err
	}
	if err := u.code.Push(ctx, checkout, pullRequest.Head); err != nil {
		return err
	}
	head, err := u.code.Resolve(checkout, pullRequest.Head)
	if err != nil {
		return err
	}
	return workspace.UpdateEpic(
		current.ID,
		func(target *epicpkg.Epic) error {
			return target.UpdatePullRequest(pullRequest.ID, func(record *epicpkg.PullRequest) error {
				if err := record.RecordCodingRound(); err != nil {
					return err
				}
				if err := addReport(target, record, "coding", answer, head); err != nil {
					return err
				}
				return target.TransitionIssue(record.IssueID, epicpkg.IssueStateReview)
			})
		},
	)
}

// gate refuses to publish a round that rewrote history or touched a path the
// host acts on. It runs before the push, so a rejected round leaves nothing
// on the remote.
func (u *RunIssueAgentUseCase) gate(checkout agent_runtime.CodeCheckout, base string) error {
	descends, err := u.code.DescendsFrom(checkout, base)
	if err != nil {
		return err
	}
	infos, err := u.code.CommitsSince(checkout, base)
	if err != nil {
		return err
	}
	commits := make([]RunCommit, 0, len(infos))
	for _, info := range infos {
		commits = append(commits, RunCommit{
			Hash: info.Hash, DescendsFromBase: descends, Paths: info.Paths,
		})
	}
	return EvaluatePushGate(base, commits)
}

// completeMerge publishes a resolved branch and sends it back to a reviewer.
// What the round does to the record's counters and verdict is
// PullRequest.RecordMergeRound's story.
func (u *RunIssueAgentUseCase) completeMerge(
	ctx context.Context,
	workspace application.Workspace,
	current epicpkg.Epic,
	pullRequest epicpkg.PullRequest,
	checkout agent_runtime.CodeCheckout,
	answer string,
) error {
	message := fmt.Sprintf("ai: merge %s into issue %s", pullRequest.Base, pullRequest.IssueID)
	if _, err := u.code.CommitAll(checkout, message); err != nil {
		return err
	}
	// The push gate is deliberately not run here. It rejects a head that no
	// longer descends from the base recorded at the start of the round, which
	// is the whole point of a merge round.
	if err := u.code.Push(ctx, checkout, pullRequest.Head); err != nil {
		return err
	}
	head, err := u.code.Resolve(checkout, pullRequest.Head)
	if err != nil {
		return err
	}
	return workspace.UpdateEpic(
		current.ID,
		func(target *epicpkg.Epic) error {
			return target.UpdatePullRequest(pullRequest.ID, func(record *epicpkg.PullRequest) error {
				record.RecordMergeRound()
				if err := addReport(target, record, "merge", answer, head); err != nil {
					return err
				}
				return target.TransitionIssue(record.IssueID, epicpkg.IssueStateReview)
			})
		},
	)
}

// completeReview records the verdict against the commits it was about, so a
// later push invalidates it rather than inheriting an approval.
func (u *RunIssueAgentUseCase) completeReview(
	workspace application.Workspace,
	current epicpkg.Epic,
	pullRequest epicpkg.PullRequest,
	checkout agent_runtime.CodeCheckout,
	base string,
	answer string,
) error {
	head, err := u.code.Resolve(checkout, pullRequest.Head)
	if err != nil {
		return err
	}
	approved := u.builder.ReviewApproved(answer)
	return workspace.UpdateEpic(
		current.ID,
		func(target *epicpkg.Epic) error {
			return target.UpdatePullRequest(pullRequest.ID, func(record *epicpkg.PullRequest) error {
				if err := record.RecordReview(approved, head, base); err != nil {
					return err
				}
				if err := addReport(target, record, "pr-reviewer", answer, head); err != nil {
					return err
				}
				// An approval is the only thing that puts an issue in Pr, where
				// the sole remaining move is a human merging it.
				next := epicpkg.IssueStateCoding
				if approved {
					next = epicpkg.IssueStatePR
				}
				return target.TransitionIssue(record.IssueID, next)
			})
		},
	)
}

// issueContext assembles what an issue-scoped prompt can reference. Editing and
// context paths use the guest layout; the host layout is supplied separately
// to Docker through the round invocation.
func issueContext(
	current epicpkg.Epic,
	issue epicpkg.Issue,
	pullRequest epicpkg.PullRequest,
	base string,
	role agent.AgentRole,
	mode agent.SessionMode,
) prompts.IssueContext {
	references := make([]string, 0, len(current.Repositories))
	for _, repository := range current.Repositories {
		if repository == pullRequest.Repository {
			continue
		}
		references = append(references, repositorypath.Encode(repository))
	}
	return prompts.IssueContext{
		IssuePath: fmt.Sprintf(
			"/work/issues/%s/%s.md", repositorypath.Encode(issue.Repository), issue.ID,
		),
		IssueTitle:   issue.Title,
		RepoDir:      "/work/repo",
		Branch:       pullRequest.Head,
		BaseBranch:   pullRequest.Base,
		BaseCommit:   base,
		Conversation: conversationFor(pullRequest, role, mode),
		Repositories: references,
	}
}

// conversationFor renders as much of the thread as this round has not already
// seen. A resumed session holds everything up to and including the role's own
// last turn, so re-sending it invites the agent to answer points it answered.
func conversationFor(
	pullRequest epicpkg.PullRequest, role agent.AgentRole, mode agent.SessionMode,
) string {
	if mode != agent.SessionModeContinue {
		return conversationSection(pullRequest.Comments, "## Discussion so far",
			"No discussion on this pull request yet.")
	}
	return conversationSection(commentsSince(pullRequest, role), "## Said since your last round",
		"Nothing has been said on the pull request since your last round.")
}

// commentsSince returns the thread after this role's own last turn. Every round
// posts its report as a comment authored by its role name, so the role's last
// comment is where its transcript ends — which is what makes this an index
// rather than a comparison between two clocks that are set independently.
//
// A role that has never spoken gets the whole thread: it has nothing in session
// to be redundant with.
func commentsSince(pullRequest epicpkg.PullRequest, role agent.AgentRole) []epicpkg.Comment {
	for index := len(pullRequest.Comments) - 1; index >= 0; index-- {
		if pullRequest.Comments[index].Author == string(role) {
			return pullRequest.Comments[index+1:]
		}
	}
	return pullRequest.Comments
}

// conversationSection renders a run of comments under one heading. An agent that
// cannot see the previous round's findings repeats work that was answered.
func conversationSection(comments []epicpkg.Comment, heading, empty string) string {
	if len(comments) == 0 {
		return empty
	}
	var builder strings.Builder
	builder.WriteString(heading + "\n")
	for _, comment := range comments {
		builder.WriteString("\n### " + comment.Author + "\n\n")
		builder.WriteString(strings.TrimSpace(comment.Body) + "\n")
	}
	return builder.String()
}

func addReport(
	target *epicpkg.Epic, record *epicpkg.PullRequest, author, answer, head string,
) error {
	body := strings.TrimSpace(answer)
	if body == "" {
		body = "The agent returned no report for " + short(head) + "."
	}
	comment, err := epicpkg.CreateComment(author, body)
	if err != nil {
		return err
	}
	return target.AddPullRequestComment(record.ID, comment)
}

// TerminalSubjects lists the subjects that will never receive another round, so
// reconciliation can reclaim their sandboxes the moment the work is finished instead of
// waiting out the idle clock. It is the counterpart of epicRole and IssueRole:
// those answer "what runs next", this one answers "nothing ever will".
//
// It is deliberately narrower than "no role right now". An epic waiting on its
// children and a pull request blocked by its parent both have no role this tick,
// yet reclaiming their sandboxes would delete and rebuild the same instance minutes
// later. Only states nothing but a human can move out of count.
//
// EpicStateFailed is not among them: Failed transitions back to Concept, and
// whoever restarts a failed epic is exactly who benefits from its refiner sandbox and
// OpenCode session still being there. IssueStatePR is not either — an approval is
// invalidated by the next push, which sends the issue back to coding.
func TerminalSubjects(epics []epicpkg.Epic) map[agent.AgentSubject]struct{} {
	terminal := map[agent.AgentSubject]struct{}{}
	for _, current := range epics {
		epicDone := current.State == epicpkg.EpicStateDone ||
			current.State == epicpkg.EpicStateClosed
		if epicDone {
			terminal[agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: current.ID}] = struct{}{}
		}
		for _, issue := range current.Issues {
			if !epicDone && issue.State != epicpkg.IssueStateMerged &&
				issue.State != epicpkg.IssueStateClosed {
				continue
			}
			terminal[agent.AgentSubject{
				Kind: agent.AgentSubjectIssue, ID: issue.ID,
			}] = struct{}{}
		}
	}
	return terminal
}

// IssueRole decides what should run next for one pull request, or reports
// that nothing should. It is the issue-side counterpart of epicRole: pure,
// total, and driven by the two round counters rather than by a state label.
//
// The cycle it encodes, for a pull request that is open and unblocked:
//
//	stale                              -> merge (base moved past the branch)
//	Rounds 0, Reviews 0                -> coding
//	Rounds n, Reviews < n              -> pr-reviewer (new code to judge)
//	Rounds n, Reviews n, not approved  -> coding (changes were requested)
//	Rounds n, Reviews n, approved      -> nothing; waiting for a human
func IssueRole(
	current epicpkg.Epic, pullRequest epicpkg.PullRequest,
) (agent.AgentRole, bool) {
	if current.State != epicpkg.EpicStateReady ||
		pullRequest.Status != epicpkg.PullRequestOpen {
		return "", false
	}
	// A parent waits for its children, and any issue waits for what its
	// BlockedBy names: implementing work another issue is about to deliver only
	// means something once that work has landed.
	if current.Blocked(pullRequest.IssueID) {
		return "", false
	}
	// A branch base has moved past cannot be published or usefully judged in
	// that state, so catching it up comes before anything else. This is never
	// rationed: refusing to merge would strand work that is otherwise finished.
	if pullRequest.HasFlag(epicpkg.FlagStale) {
		return agent.AgentRoleMerge, true
	}
	// Code nobody has judged is reviewed before anything else, even when the
	// pull request is out of coding rounds — the last round still deserves a
	// verdict a human can read.
	if pullRequest.Reviews < pullRequest.Rounds {
		return agent.AgentRolePRReviewer, true
	}
	// An approval covering the latest round ends the loop. Nothing here
	// merges; that is the one thing only a human does.
	if pullRequest.Approved {
		return "", false
	}
	if !pullRequest.CanCode() {
		return "", false
	}
	return agent.AgentRoleCoding, true
}
