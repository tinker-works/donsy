package agent_runtime

import (
	"context"
	"errors"
	"time"

	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

// SandboxManager exposes sandbox capabilities without leaking container commands
// into use cases.
//
// Every operation returns within a provider-side budget of its own, so a
// provider that hangs fails the call instead of holding the caller for the
// life of the process. Callers therefore bound the round, not the provider
// call: adding a second deadline here would only shadow the provider's.
type SandboxManager interface {
	// Ensure reports whether it had to create the instance. A created instance
	// carries a new OpenCode session database, so a round that meant to resume
	// the previous conversation has nothing left to resume — and it has to learn
	// that before it renders a prompt written on the assumption that it does.
	Ensure(ctx context.Context, spec SandboxSpec) (created bool, err error)
	Start(context.Context, agent.SandboxRef) error
	Stop(context.Context, agent.SandboxRef) error
	// StopNow stops without waiting for the guest to shut down cleanly. It is
	// for the cases where waiting is the problem rather than the risk: a process
	// that is exiting, and a sandbox reconciliation found running that nothing owns.
	// A sandbox is disposable — its identity is stable and Ensure rebuilds it — so
	// pulling its power costs nothing a graceful stop would have saved.
	StopNow(context.Context, agent.SandboxRef) error
	Delete(context.Context, agent.SandboxRef) error
	// Reserve admits one round against the host's admission budget, counting spec
	// on top of every sandbox the provider currently reports running (go-merge's own or
	// otherwise), and holds spec's share until release is called. Callers must
	// treat admitted=false as "try again later", not as an error: it is the
	// backpressure that keeps concurrent sandboxes from oversubscribing the host's
	// CPU and memory.
	//
	// Reserving rather than merely reporting is what makes the budget hold under
	// concurrent rounds. A sandbox takes minutes to create and start, and until it is
	// running the provider does not list it — so rounds that only asked whether
	// there was room would every one of them be told yes off the same snapshot,
	// and every one of them would go on to build a sandbox.
	//
	// release must be called on every path once admitted, or the budget leaks and
	// the host is never considered to have room again. Callers hold it for the
	// whole round, which is how long the sandbox is theirs.
	Reserve(ctx context.Context, spec SandboxSpec) (release func(), admitted bool, err error)
}

// SandboxInspector observes provider state for reconciliation after the application was offline.
type SandboxInspector interface {
	Inspect(context.Context, agent.SandboxRef) (agent.SandboxStatus, error)
}

// ProjectHost is the machine one project's sandboxes live inside.
//
// It exists because that machine now outlives the sandboxes it holds. A sandbox
// is a container, and the container host is a VM the runtime starts on demand
// and keeps: stopping every container of a project leaves a VM costing CPU and
// memory for nobody, and nothing else in the application is placed to notice.
//
// It is a port of its own rather than two more SandboxManager methods, because
// SandboxManager is per sandbox and this is per project — and the use case that
// deletes a host needs only half of it.
type ProjectHost interface {
	// ReapExpiredContainers removes containers, images, build cache, networks,
	// and unused volumes left behind by a project's workloads. It returns whether
	// it removed a running container.
	ReapExpiredContainers(ctx context.Context, projectID uint, runningBefore, stoppedBefore time.Time) (bool, error)
	// StopProfile releases the project's host. It reports whether it stopped the
	// VM; a running container leaves the profile up for a later reconciliation.
	StopProfile(ctx context.Context, projectID uint) (bool, error)
	// DeleteProfile removes the host and everything it holds, for a project
	// that is being forgotten.
	DeleteProfile(ctx context.Context, projectID uint) error
}

// AgentRuntime executes one command in an already-running sandbox. The run ID is
// passed so the runtime can name its transcript after the run, which is what
// makes the log addressable later without storing a path anywhere.
type AgentRuntime interface {
	Run(ctx context.Context, ref agent.SandboxRef, runID string, argv []string) (string, error)
}

// HostDisk reports free space where the sandbox runtime keeps its images and
// container hosts.
//
// Reclaim is otherwise on a clock alone, and a clock cannot tell the difference
// between a quiet host and one about to run out: a host's disk grows toward its
// allowance as images and layers accumulate, so a day's worth of abandoned work
// fills it while every sandbox is still comfortably inside its idle window. The
// symptom is not a reclaim that ran late — it is every image build failing with
// "no space left on device", which reads as the agents being broken.
type HostDisk interface {
	FreeBytes() (int64, error)
}

// RunOutput reads the transcript a round left on the host. Reading is a poll
// from a byte offset rather than a stream: the log is an ordinary growing
// file, and a caller that wants live output asks again from Next.
type RunOutput interface {
	Tail(runID string, from int64) (lines []string, next int64, err error)
	// Size reports how many bytes the transcript holds right now, without
	// reading it. Sampling growth is what drives the activity sparklines, and
	// a stat per tick is affordable where a full tail per runner is not.
	Size(runID string) (int64, error)
	// Discard removes a run's transcript. A run whose subject is finished keeps
	// its record, which is the history worth having; the raw output is the bulk,
	// and nothing reads it again once nobody can act on the round.
	Discard(runID string) error
}

// RepositoryWorkspace prepares host-side read-only code for an agent sandbox.
type RepositoryWorkspace interface {
	Ensure(context.Context, string, string) (string, error)
	// Purge removes everything held for one epic. Ensure re-clones on demand, so
	// this trades a re-download for the disk of work nobody will touch again.
	Purge(epicID string) error
}

// CodeCheckout names the working copy one issue's rounds happen in. It is
// per-issue rather than per-epic: two issues in the same repository run
// concurrently, each on its own branch, so they cannot share a working tree.
type CodeCheckout struct {
	EpicID     string
	IssueID    string
	Repository string
}

// CommitInfo is one commit with the paths it touched, for the push gate.
type CommitInfo struct {
	Hash  string
	Paths []string
}

// CodeWorkspace owns the git side of a coding round: cutting the branch,
// committing whatever the agent left behind, and publishing it. The guest
// never touches a remote — it has no credentials and no origin — so every
// operation here runs on the host, after the sandbox has stopped.
type CodeWorkspace interface {
	// DefaultBranch reports the repository's own default branch, which is
	// what every issue branch is cut from.
	DefaultBranch(ctx context.Context, repository string) (string, error)
	// PurgeEpic removes every checkout cut for one epic. A round pushes before it
	// finishes, so a finished epic's checkouts hold nothing that is not on the
	// remote, and Checkout clones again if one is ever needed.
	PurgeEpic(epicID string) error
	// Checkout prepares the working copy on branch, creating it at base when
	// it does not exist yet, and returns its host path.
	Checkout(ctx context.Context, checkout CodeCheckout, branch, base string) (string, error)
	// Resolve turns a ref into a commit hash within the checkout.
	Resolve(checkout CodeCheckout, ref string) (string, error)
	// CommitAll commits the whole working tree, including an empty commit
	// when the agent changed nothing, so every round leaves a reviewable
	// commit. It returns the new head.
	CommitAll(checkout CodeCheckout, message string) (string, error)
	// CommitsSince lists commits between base and the current head, newest
	// last, each with the paths it changed.
	CommitsSince(checkout CodeCheckout, base string) ([]CommitInfo, error)
	// DescendsFrom reports whether the checkout's head still has base in its
	// ancestry — false means history was rewritten.
	DescendsFrom(checkout CodeCheckout, base string) (bool, error)
	// Push publishes branch to origin without forcing.
	Push(ctx context.Context, checkout CodeCheckout, branch string) error
	// DeleteBranch removes branch from origin and drops the local checkout it
	// was worked in. A branch that is already gone is not an error: closing the
	// work it belonged to has to stay repeatable.
	DeleteBranch(ctx context.Context, checkout CodeCheckout, branch string) error
	// Merge merges head into base and publishes base. It reports
	// ErrMergeConflict when the two cannot be combined without a human or
	// another coding round, having left nothing behind.
	Merge(ctx context.Context, checkout CodeCheckout, head, base string) error
	// InspectBranches fetches once and reports where each of heads stands
	// against base. It is batched per repository rather than per branch
	// because a fetch brings down every remote ref anyway: comparing ten
	// branches costs one round trip, and asking ten times would cost ten.
	//
	// This is scoped by epicID and repository, not a CodeCheckout: it is a
	// repository-wide operation, not any one issue's, and reusing an issue's
	// own checkout here is exactly what let this fetch race a coding round's
	// Checkout/CommitAll/Push against the identical directory and corrupt the
	// git object store. Heads that do not exist are absent from the result
	// rather than an error; a branch somebody deleted is not a reason to fail
	// the whole sweep.
	InspectBranches(
		ctx context.Context, epicID, repository, base string, heads []string,
	) (map[string]BranchState, error)
}

// BranchState is what a sweep needs to know about a published branch: whether
// it can still be fast-forwarded onto base, and what is actually on its tip.
type BranchState struct {
	// Head is the branch's current commit on the remote. A value the store did
	// not record means somebody pushed outside the loop.
	Head string
	// Behind reports that base has commits the branch does not contain, so it
	// cannot be published until the merge role catches it up.
	Behind bool
}

// ErrMergeConflict reports that head and base could not be combined
// automatically. It is a normal outcome rather than a failure: the caller sends
// the pull request back for another coding round, so the conflict is resolved
// by whoever wrote the code.
var ErrMergeConflict = errors.New("merge conflict")

// IssueTreeStore reads and writes an agent sandbox's editable issue tree.
//
// Write takes the sandbox name because the tree belongs to one sandbox, not to the epic.
// Rounds on different subjects of the same epic run at the same time, and each
// one's tree stays mounted into its guest for the whole round - so a tree keyed
// by epic would have one round rewriting the directory another round's running
// Sandbox is reading. The name is stable per subject and role, which is what keeps a
// Sandbox's tree at the same path across its rounds.
type IssueTreeStore interface {
	Write(sandboxName string, epic epic.Epic) (string, error)
	Read(string, epic.Epic) (epic.Epic, error)
}

// AgentRegistry keeps machine-local runtime state out of the shared project store.
type AgentRegistry interface {
	SaveSandbox(agent.Sandbox) error
	ListSandboxes(uint) ([]agent.Sandbox, error)
	SaveAgentRun(agent.AgentRun) error
	ListAgentRuns(uint, agent.AgentSubject) ([]agent.AgentRun, error)
	// ListProjectAgentRuns reads every run in a project, newest first. The
	// subject-scoped read above answers "which round is this subject on";
	// this one answers "what is running right now", which no caller can
	// express without knowing every subject up front.
	ListProjectAgentRuns(uint) ([]agent.AgentRun, error)
	GetAgentRun(string) (agent.AgentRun, error)
	// DeleteSubjectRuntime removes the local sandbox and run history for one subject.
	// A reset uses it only after provider, branch, and transcript cleanup has
	// completed, so the next round cannot inherit a session or round number.
	DeleteSubjectRuntime(projectID uint, subject agent.AgentSubject) error
	// DeleteProjectRuntime drops every sandbox and run record of one project. Nothing
	// is keyed by project except through these rows, and reconciliation only ever
	// looks at projects that still exist — so a project that is forgotten without
	// this leaves records nothing will ever read, correct, or clean up again.
	DeleteProjectRuntime(projectID uint) error
}

// AgentCredentials materializes only the credentials a guest needs in a dedicated,
// read-only mount. It must never expose a host credential directory wholesale.
type AgentCredentials interface {
	// OpenCodeMount stages the credential for the provider that model
	// ("provider/model") names into a mount scoped to sandboxName. One sandbox gets one
	// provider's credential; the rest of the host's auth store stays home.
	OpenCodeMount(sandboxName, model string) (SandboxMount, error)
	// Discard removes the credentials staged for a sandbox. It is called when the sandbox
	// is reclaimed: a real provider credential left on disk for an instance that
	// no longer exists is a copy nothing needs and nobody is watching.
	Discard(sandboxName string) error
}
