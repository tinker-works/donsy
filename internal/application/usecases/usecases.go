package usecases

import (
	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"time"
)

// Error wrapping in this package follows one rule: leaf use cases return the
// underlying error as-is, because they cannot know what the caller is doing;
// orchestrators that fan out over projects or epics (EpicWorker,
// ReviewApprovedBranches) wrap with the subject they were working on, because
// there the context would otherwise be lost.
type UseCases struct {
	// CurrentUser is the authenticated GitHub login for this process, set once
	// at startup. Comments and epics it creates are attributed to it.
	CurrentUser string

	ListProjects            *ListProjectsUseCase
	OpenProject             *OpenProjectUseCase
	CreateProject           *CreateProjectUseCase
	ListEpics               *ListEpicsUseCase
	GetEpic                 *GetEpicUseCase
	CreateEpic              *CreateEpicUseCase
	CloseEpic               *CloseEpicUseCase
	TransitionEpicState     *TransitionEpicStateUseCase
	SetBranchPrefix         *SetBranchPrefixUseCase
	CreateIssue             *CreateIssueUseCase
	CloseIssue              *CloseIssueUseCase
	CreatePullRequest       *CreatePullRequestUseCase
	TransitionPullRequest   *TransitionPullRequestUseCase
	GrantCodingRound        *GrantCodingRoundUseCase
	MergePullRequest        *MergePullRequestUseCase
	ReviewApprovedBranches  *ReviewApprovedBranchesUseCase
	AddComment              *AddCommentUseCase
	ReconcileSandboxes      *ReconcileSandboxesUseCase
	RunEpicAgent            *RunEpicAgentUseCase
	DiscoverOrganisations   *DiscoverOrganisationsUseCase
	ListOrganisations       *ListOrganisationsUseCase
	AddOrganisation         *AddOrganisationUseCase
	RemoveOrganisation      *RemoveOrganisationUseCase
	SyncRepositories        *SyncRepositoriesUseCase
	ListRepositories        *ListRepositoriesUseCase
	UpdateRepositories      *UpdateProjectRepositoriesUseCase
	ListProjectRepositories *ListProjectRepositoriesUseCase
	ForgetProject           *ForgetProjectUseCase
	ListProjectSummaries    *ListProjectSummariesUseCase
	GetAgentSettings        *GetAgentSettingsUseCase
	SetAgentRole            *SetAgentRoleUseCase
	ListAgentRuns           *ListAgentRunsUseCase
	GetAgentRun             *GetAgentRunUseCase
	ListSandboxes           *ListSandboxesUseCase
	PurgeFinishedWork       *PurgeFinishedWorkUseCase
	ReadRunOutput           *ReadRunOutputUseCase
	GetPullRequestDiff      *GetPullRequestDiffUseCase
	OpenPullRequests        *OpenPullRequestsUseCase
	RunIssueAgent           *RunIssueAgentUseCase
	CompleteEpic            *CompleteEpicUseCase
	CancelAgentRun          *CancelAgentRunUseCase
	ResetIssue              *ResetIssueUseCase
	AddRepository           *AddRepositoryUseCase
	StoreSetup              *StoreSetupUseCase
	InitialiseStore         *InitialiseStoreUseCase
}

// IssueLoop groups the execution-side use cases for EpicWorker. It is empty
// when the agent dependencies were not supplied.
func (u *UseCases) IssueLoop() IssueLoop {
	return IssueLoop{
		GetEpic:          u.GetEpic,
		OpenPullRequests: u.OpenPullRequests,
		RunIssueAgent:    u.RunIssueAgent,
		CompleteEpic:     u.CompleteEpic,
		ReviewApproved:   u.ReviewApprovedBranches,
	}
}

// idleSandboxReclaimAfter is how long a Stopped sandbox sits unused before
// ReconcileSandboxesUseCase deletes it. EpicSandboxSpec's stable
// per-epic-and-role identity means the next tick that needs the role recreates
// the sandbox automatically, so this only trades a slower resume for not
// holding host disk for sandboxes nothing is using.
//
// It is generous because it is no longer the main way disk is freed: a sandbox whose epic
// or issue reached a terminal state is reclaimed on the next tick regardless of age
// (see TerminalSubjects). What remains for this clock is work that was abandoned
// rather than finished, and there a day of keeping the sandbox — and with it the OpenCode
// session a resumed round would continue — costs less than rebuilding it.
//
// maxSandboxRuntime is the other half: a sandbox the provider still reports running this long
// after it started is force-stopped whatever its records claim. It is deliberately
// longer than maxRoundDuration so a round's own deadline is what normally cuts a
// runaway off, leaving this to catch only what outlived the process that started it.
//
// idleSandboxReclaimUnderPressure is the same clock for a host that is nearly full,
// and diskPressureFreeBytes is where "nearly" starts. A day is generous only
// while there is disk to be generous with: a project's host grows toward its
// allowance as images and container layers accumulate, so abandoned work adds up
// faster than the clock frees it, and the first thing to fail is not a round but
// the image build every new sandbox waits on — reported as "no space left on
// device", which reads as the agents being broken rather than the disk being full.
//
// The threshold is an absolute size rather than a fraction of the disk: what
// has to fit is the same whether the host has 500GB or 2TB. It is set well
// above the point where a build actually fails — the host budget admits several
// Sandboxes at once, each of which grows to its full allowance, so by the time the
// remaining space merely looks sufficient the sandboxes already created can still
// consume it. Reclaiming early costs a resumable OpenCode session; reclaiming
// late costs every round on the host.
const (
	idleSandboxReclaimAfter         = 24 * time.Hour
	idleSandboxReclaimUnderPressure = time.Hour
	diskPressureFreeBytes           = 100 << 30
	maxSandboxRuntime               = 3 * time.Hour
)

// projectHostIdleAfter is how long a project must have had nothing running
// before reconciliation stops the machine its sandboxes live in.
//
// It is far shorter than idleSandboxReclaimAfter above, and the two are not
// comparable. Reclaiming a sandbox destroys the session a resumed round would
// have continued, so it waits a day; stopping the host costs nothing but the
// time to start it again, because stopping persists its disk — the images built
// inside it and the sessions in its stopped containers are all still there.
//
// What it buys is a laptop that is not holding a VM per registered project. What
// it must not do is thrash: on a five-second tick a project running rounds back
// to back would otherwise stop and lazily restart its host in every gap between
// them, and a start costs tens of seconds against a stop's none.
const (
	projectHostIdleAfter      = 5 * time.Minute
	hostRunningContainerAfter = time.Hour
	hostStoppedContainerAfter = 24 * time.Hour
)

// NewUseCases wires every use case from the two required dependencies and two
// optional groups. github is nil when GitHub discovery is not configured — the
// discovery, organisation and repository-sync use cases stay nil with it.
// agents is nil when no agent runtime is configured — the run, cancel, sandbox and
// output use cases stay nil and the worker drives nothing.
func NewUseCases(
	registry application.Registry,
	factory application.WorkspaceFactory,
	clock application.Clock,
	github application.GitHubClient,
	agents *EpicAgentDependencies,
) *UseCases {
	// Closing work deletes the branch behind it, so the use cases that close
	// need the checkout even though they are otherwise store-only. It stays nil
	// when the loop runs drafting-only and no branch was ever cut.
	var code agent_runtime.CodeWorkspace
	var differ application.RepositoryDiffer
	if agents != nil {
		code = agents.Code
		differ = agents.Differ
	}
	useCases := &UseCases{
		ListProjects: &ListProjectsUseCase{registry: registry},
		OpenProject:  &OpenProjectUseCase{registry: registry},
		CreateProject: &CreateProjectUseCase{
			registry: registry, factory: factory, clock: clock,
		},
		ListEpics:               &ListEpicsUseCase{factory: factory},
		GetEpic:                 &GetEpicUseCase{factory: factory},
		CreateEpic:              &CreateEpicUseCase{factory: factory},
		CloseEpic:               &CloseEpicUseCase{factory: factory, code: code},
		TransitionEpicState:     &TransitionEpicStateUseCase{factory: factory},
		SetBranchPrefix:         &SetBranchPrefixUseCase{factory: factory},
		CreateIssue:             &CreateIssueUseCase{factory: factory},
		CloseIssue:              &CloseIssueUseCase{factory: factory, code: code},
		CreatePullRequest:       &CreatePullRequestUseCase{factory: factory},
		TransitionPullRequest:   &TransitionPullRequestUseCase{factory: factory, code: code},
		GrantCodingRound:        &GrantCodingRoundUseCase{factory: factory},
		MergePullRequest:        &MergePullRequestUseCase{factory: factory, code: code},
		ReviewApprovedBranches:  &ReviewApprovedBranchesUseCase{factory: factory, code: code},
		AddComment:              &AddCommentUseCase{factory: factory},
		UpdateRepositories:      &UpdateProjectRepositoriesUseCase{factory: factory},
		ListProjectRepositories: &ListProjectRepositoriesUseCase{factory: factory},
		ForgetProject:           &ForgetProjectUseCase{registry: registry, agents: registry},
		GetAgentSettings:        &GetAgentSettingsUseCase{factory: factory},
		SetAgentRole:            &SetAgentRoleUseCase{factory: factory},
		StoreSetup:              &StoreSetupUseCase{organisations: registry, factory: factory},
		InitialiseStore:         &InitialiseStoreUseCase{factory: factory},
		GetPullRequestDiff:      &GetPullRequestDiffUseCase{factory: factory, differ: differ},
	}
	useCases.ListAgentRuns = &ListAgentRunsUseCase{registry: registry}
	useCases.GetAgentRun = &GetAgentRunUseCase{registry: registry}
	useCases.ListSandboxes = &ListSandboxesUseCase{registry: registry}
	useCases.ListProjectSummaries = &ListProjectSummariesUseCase{
		registry: registry, agentRegistry: registry, factory: factory,
	}
	// Adding one repository by name needs no GitHub client: discovery is
	// what needs the network, naming a repository is not. The same holds for
	// naming, listing and dropping an organisation — and these have to stay
	// available regardless, because StoreSetup counts organisations to decide
	// whether a project can be set up at all.
	useCases.AddRepository = &AddRepositoryUseCase{registry: registry}
	useCases.ListOrganisations = &ListOrganisationsUseCase{registry: registry}
	useCases.AddOrganisation = &AddOrganisationUseCase{registry: registry}
	useCases.RemoveOrganisation = &RemoveOrganisationUseCase{registry: registry}
	if github != nil {
		useCases.DiscoverOrganisations = &DiscoverOrganisationsUseCase{
			client: github, registry: registry,
		}
		useCases.SyncRepositories = &SyncRepositoriesUseCase{
			client: github, organisations: registry, repositories: registry,
		}
		useCases.ListRepositories = &ListRepositoriesUseCase{registry: registry}
	}
	if agents != nil {
		if agents.Output != nil && agents.Builder != nil {
			useCases.ReadRunOutput = &ReadRunOutputUseCase{
				output: agents.Output, builder: agents.Builder,
			}
		}
		// Forgetting a project has to be able to tear its sandboxes down, or it leaves
		// instances nothing can ever reach again.
		useCases.ForgetProject.sandboxes = agents.Sandboxes
		useCases.ForgetProject.creds = agents.Credentials
		useCases.ForgetProject.host = agents.Host
		useCases.PurgeFinishedWork = &PurgeFinishedWorkUseCase{
			repos: agents.Repositories, code: agents.Code,
			output: agents.Output, registry: registry,
		}
		if agents.Inspector != nil {
			useCases.ReconcileSandboxes = &ReconcileSandboxesUseCase{
				registry: registry, inspector: agents.Inspector, sandboxes: agents.Sandboxes,
				creds: agents.Credentials,
				clock: clock, idleAfter: idleSandboxReclaimAfter, maxRuntime: maxSandboxRuntime,
				disk: agents.Disk, pressureBelow: diskPressureFreeBytes,
				idleUnderPressure: idleSandboxReclaimUnderPressure,
				host:              agents.Host,
				hostIdleAfter:     projectHostIdleAfter,
				hostRunningAfter:  hostRunningContainerAfter,
				hostStoppedAfter:  hostStoppedContainerAfter,
			}
		}
		// One supervisor is shared by every round, because cancelling is a
		// lookup across all of them rather than per use case.
		supervisor := NewRunSupervisor()
		useCases.CancelAgentRun = &CancelAgentRunUseCase{
			registry: registry, supervisor: supervisor, clock: clock,
		}
		if agents.Code != nil {
			useCases.ResetIssue = &ResetIssueUseCase{
				factory: factory, registry: registry, code: agents.Code, sandboxes: agents.Sandboxes,
				creds: agents.Credentials, output: agents.Output, cancel: useCases.CancelAgentRun,
			}
		}
		useCases.RunEpicAgent = &RunEpicAgentUseCase{
			factory: factory, registry: registry, sandboxes: agents.Sandboxes,
			runtime: agents.Runtime, builder: agents.Builder, creds: agents.Credentials,
			repos: agents.Repositories, issueTreeStore: agents.IssueTreeStore, clock: clock,
			supervisor: supervisor, output: agents.Output,
			roundTimeout: maxRoundDuration,
		}
		useCases.CompleteEpic = &CompleteEpicUseCase{factory: factory}
		if agents.Code != nil {
			useCases.OpenPullRequests = &OpenPullRequestsUseCase{
				factory: factory, code: agents.Code,
			}
			useCases.RunIssueAgent = &RunIssueAgentUseCase{
				factory: factory, registry: registry, sandboxes: agents.Sandboxes,
				runtime: agents.Runtime, builder: agents.Builder,
				creds: agents.Credentials, code: agents.Code, repos: agents.Repositories,
				issueTreeStore: agents.IssueTreeStore, clock: clock, supervisor: supervisor,
				output: agents.Output, roundTimeout: maxRoundDuration,
			}
		}
	}
	return useCases
}

type EpicAgentDependencies struct {
	Sandboxes agent_runtime.SandboxManager
	// Host is the per-project machine the sandboxes run inside. Nil for a
	// runtime whose sandboxes have no shared host to stop.
	Host agent_runtime.ProjectHost
	// Inspector reads a sandbox's actual status back from the provider, which is
	// what reconciliation corrects the records against.
	Inspector      agent_runtime.SandboxInspector
	Runtime        agent_runtime.AgentRuntime
	Builder        application.AgentCommandBuilder
	Credentials    agent_runtime.AgentCredentials
	Repositories   agent_runtime.RepositoryWorkspace
	IssueTreeStore agent_runtime.IssueTreeStore
	// Code is the writable per-issue checkout the coding and review rounds
	// work in. Without it the worker drives drafting only.
	Code agent_runtime.CodeWorkspace
	// Output reads back the transcript Runtime writes, which is how a
	// finished or live round can be read without storing its output.
	Output agent_runtime.RunOutput
	// Differ computes a pull request's diff from the same host-side clones
	// the agents work from.
	Differ application.RepositoryDiffer
	// Disk reports free space where images and sandbox disks are kept, which brings
	// idle reclaim forward on a host that is nearly full. Nil leaves reclaim on
	// its clock alone.
	Disk agent_runtime.HostDisk
}
