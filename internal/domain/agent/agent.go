package agent

import (
	"fmt"
	"time"
)

type AgentRole string

const (
	AgentRoleRefiner       AgentRole = "refiner"
	AgentRoleIssueReviewer AgentRole = "issue-reviewer"
	AgentRoleCoding        AgentRole = "coding"
	AgentRolePRReviewer    AgentRole = "pr-reviewer"
	// AgentRoleMerge resolves a branch that base has moved past. It is a
	// separate role from coding because the job is narrower: combine two sides
	// that already exist, not decide what the code should do.
	AgentRoleMerge AgentRole = "merge"
)

type AgentEngine string

const (
	AgentEngineOpenCode AgentEngine = "opencode"
)

type AgentSubjectKind string

const (
	AgentSubjectEpic  AgentSubjectKind = "epic"
	AgentSubjectIssue AgentSubjectKind = "issue"
)

// AgentSubject identifies the durable work item an agent operates on.
type AgentSubject struct {
	Kind AgentSubjectKind
	ID   string
}

func (s AgentSubject) Validate() error {
	if s.Kind != AgentSubjectEpic && s.Kind != AgentSubjectIssue {
		return fmt.Errorf("agent subject has invalid kind %q", s.Kind)
	}
	if s.ID == "" {
		return fmt.Errorf("agent subject ID is required")
	}
	return nil
}

type SessionMode string

const (
	SessionModeFresh    SessionMode = "fresh"
	SessionModeContinue SessionMode = "continue"

	// sessionContinuationEnabled controls whether new runs resume an OpenCode
	// conversation. Keep the mode valid for persisted historical runs so they
	// remain readable while continuation is disabled.
	sessionContinuationEnabled = false
)

// SessionModeFor keeps a role's context on its dedicated sandbox across rounds. Every
// role that gets a second round on the same subject resumes its own conversation:
// the drafting and implementation roles because their work is cumulative, and the
// reviewing roles because a reviewer that cannot remember its last review re-raises
// findings that were already answered.
//
// A resumed review is not a deferred one. What keeps the verdict honest is the
// role's continue prompt, which tells the reviewer to judge the subject as it now
// stands rather than carry its previous verdict forward — not the absence of a
// session.
//
// Merge is the exception: it folds base back in once, and a branch that needs
// merging again is a new situation rather than a continuation of that one.
func SessionModeFor(engine AgentEngine, role AgentRole) SessionMode {
	if engine != AgentEngineOpenCode || role == AgentRoleMerge {
		return SessionModeFresh
	}
	return SessionModeContinue
}

// SessionModeAfter is SessionModeFor narrowed by what already happened to this
// subject: a round only resumes the previous conversation when the previous
// round of the same role actually finished. It is the first of two narrowings —
// agentRound.begin applies the second once the sandbox tells it whether the
// session this answer refers to still exists.
//
// Resuming after a failure is what makes a bad round permanent. The agent picks
// up a transcript in which it already believes the work is done, and a session
// the engine has failed on once fails the same way every round after — so the
// retry the round limit is counting can never differ from the attempt it is
// retrying.
func SessionModeAfter(engine AgentEngine, role AgentRole, previous []AgentRun) SessionMode {
	if !sessionContinuationEnabled {
		return SessionModeFresh
	}
	if SessionModeFor(engine, role) == SessionModeFresh {
		return SessionModeFresh
	}
	if !lastRoundSucceeded(role, previous) {
		return SessionModeFresh
	}
	return SessionModeContinue
}

// lastRoundSucceeded reports the outcome of the most recent round for one role,
// which is the only prior run a new round could sensibly resume.
func lastRoundSucceeded(role AgentRole, previous []AgentRun) bool {
	var latest *AgentRun
	for index := range previous {
		run := &previous[index]
		if run.Role != role {
			continue
		}
		if latest == nil || run.Round > latest.Round {
			latest = run
		}
	}
	return latest != nil && latest.Status == AgentRunStatusSucceeded
}

type SandboxStatus string

const (
	SandboxStatusAbsent   SandboxStatus = "absent"
	SandboxStatusCreating SandboxStatus = "creating"
	SandboxStatusStopped  SandboxStatus = "stopped"
	SandboxStatusStarting SandboxStatus = "starting"
	SandboxStatusRunning  SandboxStatus = "running"
	SandboxStatusStopping SandboxStatus = "stopping"
	SandboxStatusBroken   SandboxStatus = "broken"
	SandboxStatusDeleting SandboxStatus = "deleting"
)

type SandboxEvent string

const (
	SandboxEventCreate     SandboxEvent = "create"
	SandboxEventStart      SandboxEvent = "start"
	SandboxEventReady      SandboxEvent = "ready"
	SandboxEventStop       SandboxEvent = "stop"
	SandboxEventStopped    SandboxEvent = "stopped"
	SandboxEventFail       SandboxEvent = "fail"
	SandboxEventDelete     SandboxEvent = "delete"
	SandboxEventDeleteDone SandboxEvent = "delete_done"
)

// Sandbox is a host-local container an agent round runs in. Its identity is
// stable for one project, role, and subject so OpenCode's local session
// database can be resumed safely.
type Sandbox struct {
	ID        string
	ProjectID uint
	Name      string
	Role      AgentRole
	Subject   AgentSubject
	Status    SandboxStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SandboxRef addresses one sandbox for the runtime that owns it.
//
// The project is part of the address rather than the name alone, because a
// sandbox lives inside its project's own container host and every operation has
// to reach that host before it can reach the container. Nothing constrains the
// shape of Name, so an adapter that had only the name would have to parse the
// project back out of it — an undocumented contract with whoever mints names.
type SandboxRef struct {
	ProjectID uint
	Name      string
}

func (v *Sandbox) Ref() SandboxRef {
	return SandboxRef{ProjectID: v.ProjectID, Name: v.Name}
}

// Validate does not require a ProjectID: the golden base image boots through a
// host-global sandbox that belongs to no project, unlike an AgentRun, which always
// runs for one.
func (v *Sandbox) Validate() error {
	if v.ID == "" || v.Name == "" {
		return fmt.Errorf("sandbox ID and name are required")
	}
	if !IsAgentRole(v.Role) || !isSandboxStatus(v.Status) {
		return fmt.Errorf("sandbox has invalid role or status")
	}
	return v.Subject.Validate()
}

func (v *Sandbox) Apply(event SandboxEvent) error {
	next, ok := sandboxTransition(v.Status, event)
	if !ok {
		return fmt.Errorf("cannot apply sandbox event %q from status %q", event, v.Status)
	}
	v.Status = next
	return nil
}

// Reconcile adopts a status observed on the real instance, bypassing the
// transition table on purpose. The FSM orders the moves this process makes; a
// crash, an out-of-band docker command, or a provider operation that skips states
// moves the machine without consulting it, and the record has to follow the
// machine rather than argue with it. Everything the process decides itself goes
// through Apply.
func (v *Sandbox) Reconcile(observed SandboxStatus) error {
	if !isSandboxStatus(observed) {
		return fmt.Errorf("cannot reconcile sandbox to invalid status %q", observed)
	}
	v.Status = observed
	return nil
}

type AgentRunStatus string

const (
	AgentRunStatusQueued    AgentRunStatus = "queued"
	AgentRunStatusAdmitted  AgentRunStatus = "admitted"
	AgentRunStatusRunning   AgentRunStatus = "running"
	AgentRunStatusSucceeded AgentRunStatus = "succeeded"
	AgentRunStatusFailed    AgentRunStatus = "failed"
	AgentRunStatusCancelled AgentRunStatus = "cancelled"
	AgentRunStatusStalled   AgentRunStatus = "stalled"
	// AgentRunStatusHostFailed is a round the host could not run: the sandbox would
	// not build or start, or it was killed out from under the agent. It is kept
	// apart from Failed because the two answer different questions. Failed says
	// the agent had its turn and did not get there, which is worth acting on
	// after enough tries. This says the agent never had a turn at all, which
	// says nothing about the work and must not be counted against it — see
	// CountsTowardRoundLimit.
	AgentRunStatusHostFailed AgentRunStatus = "host-failed"
)

type AgentRunEvent string

const (
	AgentRunEventAdmit   AgentRunEvent = "admit"
	AgentRunEventStart   AgentRunEvent = "start"
	AgentRunEventSucceed AgentRunEvent = "succeed"
	AgentRunEventFail    AgentRunEvent = "fail"
	AgentRunEventCancel  AgentRunEvent = "cancel"
	AgentRunEventStall   AgentRunEvent = "stall"
	// AgentRunEventHostFail ends a round the host never managed to run.
	AgentRunEventHostFail AgentRunEvent = "host-fail"
)

// RunUsage is the token and cost accounting an engine reports for one run,
// summed across its steps. A zero value means the engine reported nothing —
// runs recorded before accounting existed, or an engine that does not emit it.
type RunUsage struct {
	TokensIn  int
	TokensOut int
	CostUSD   float64
}

// Reported tells a run that used nothing apart from one that predates
// accounting; both would otherwise render as zeros presented as fact.
func (u RunUsage) Reported() bool {
	return u.TokensIn != 0 || u.TokensOut != 0 || u.CostUSD != 0
}

// AgentRun records one attempt. It does not own epic or issue business state.
type AgentRun struct {
	ID          string
	ProjectID   uint
	SandboxID   string
	Role        AgentRole
	Subject     AgentSubject
	Engine      AgentEngine
	Agent       string
	Variant     string
	SessionMode SessionMode
	Status      AgentRunStatus
	Round       int
	Error       string
	Usage       RunUsage
	CreatedAt   time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
}

func (r *AgentRun) Validate() error {
	if r.ID == "" || r.SandboxID == "" || r.Agent == "" || r.Round < 1 {
		return fmt.Errorf("agent run ID, Sandbox ID, agent, and positive round are required")
	}
	if !IsAgentRole(r.Role) || r.Engine != AgentEngineOpenCode ||
		(r.SessionMode != SessionModeFresh && r.SessionMode != SessionModeContinue) ||
		!isAgentRunStatus(r.Status) {
		return fmt.Errorf("agent run has invalid configuration or status")
	}
	// The invariant is one-directional: a role with no session to resume may
	// never claim to resume one, but a role that is allowed to resume one does
	// not have to. Its first round has nothing to continue, a round retrying a
	// failure must not rejoin the transcript that failed, and a round whose
	// sandbox had to be rebuilt has had its session deleted underneath it.
	if r.SessionMode == SessionModeContinue &&
		SessionModeFor(r.Engine, r.Role) != SessionModeContinue {
		return fmt.Errorf("agent run may not continue a session for role %q", r.Role)
	}
	return r.Subject.Validate()
}

func (r *AgentRun) Apply(event AgentRunEvent, at time.Time) error {
	next, ok := agentRunTransition(r.Status, event)
	if !ok {
		return fmt.Errorf("cannot apply agent run event %q from status %q", event, r.Status)
	}
	r.Status = next
	if next == AgentRunStatusRunning {
		r.StartedAt = &at
	}
	if isTerminalAgentRunStatus(next) {
		r.FinishedAt = &at
	}
	return nil
}

// roles are the defined agent roles in pipeline order: the sequence work moves
// through, which is also the order a reader expects to see them listed in.
//
// This is the single source of the role list. Anything that needs every role —
// a settings screen, a setup gate, an attention check — must read it from here
// rather than re-listing them, or a new role silently falls through the copy.
var roles = []AgentRole{
	AgentRoleRefiner,
	AgentRoleIssueReviewer,
	AgentRoleCoding,
	AgentRolePRReviewer,
	AgentRoleMerge,
}

// Roles returns every agent role in pipeline order. The slice is copied so a
// caller cannot reorder or truncate the list every other caller depends on.
func Roles() []AgentRole {
	return append([]AgentRole(nil), roles...)
}

// IsAgentRole reports whether role is one of the defined agent roles. Anything
// classifying an author or a run by role must call this rather than re-listing
// the roles.
func IsAgentRole(role AgentRole) bool {
	for _, candidate := range roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func isSandboxStatus(status SandboxStatus) bool {
	switch status {
	case SandboxStatusAbsent, SandboxStatusCreating, SandboxStatusStopped,
		SandboxStatusStarting, SandboxStatusRunning, SandboxStatusStopping,
		SandboxStatusBroken, SandboxStatusDeleting:
		return true
	default:
		return false
	}
}

func sandboxTransition(status SandboxStatus, event SandboxEvent) (SandboxStatus, bool) {
	switch {
	case status == SandboxStatusAbsent && event == SandboxEventCreate:
		return SandboxStatusCreating, true
	case status == SandboxStatusCreating && event == SandboxEventReady:
		return SandboxStatusStopped, true
	case status == SandboxStatusStopped && event == SandboxEventStart:
		return SandboxStatusStarting, true
	case status == SandboxStatusStarting && event == SandboxEventReady:
		return SandboxStatusRunning, true
	case status == SandboxStatusRunning && event == SandboxEventStop:
		return SandboxStatusStopping, true
	case status == SandboxStatusStopping && event == SandboxEventStopped:
		return SandboxStatusStopped, true
	case (status == SandboxStatusStopped || status == SandboxStatusBroken) &&
		event == SandboxEventDelete:
		return SandboxStatusDeleting, true
	case status == SandboxStatusDeleting && event == SandboxEventDeleteDone:
		return SandboxStatusAbsent, true
	case status != SandboxStatusAbsent && status != SandboxStatusDeleting && event == SandboxEventFail:
		return SandboxStatusBroken, true
	default:
		return "", false
	}
}

func isAgentRunStatus(status AgentRunStatus) bool {
	switch status {
	case AgentRunStatusQueued, AgentRunStatusAdmitted, AgentRunStatusRunning,
		AgentRunStatusSucceeded, AgentRunStatusFailed, AgentRunStatusCancelled,
		AgentRunStatusStalled, AgentRunStatusHostFailed:
		return true
	default:
		return false
	}
}

func isTerminalAgentRunStatus(status AgentRunStatus) bool {
	return status == AgentRunStatusSucceeded || status == AgentRunStatusFailed ||
		status == AgentRunStatusCancelled || status == AgentRunStatusStalled ||
		status == AgentRunStatusHostFailed
}

// CountsTowardRoundLimit reports whether this run spends one of the role's
// attempts at its subject.
//
// The limit exists to stop a role that cannot get there from retrying forever,
// so what it has to measure is attempts the agent actually got. A round the
// host never ran is not evidence about the work: a full disk, an image that
// will not build, or a sandbox killed out from under the agent says the same
// thing about every subject on the machine at once. Counting those turns one
// host fault into every epic on it failing, blamed on the role.
func (r AgentRun) CountsTowardRoundLimit() bool {
	return r.Status != AgentRunStatusHostFailed
}

func agentRunTransition(status AgentRunStatus, event AgentRunEvent) (AgentRunStatus, bool) {
	switch {
	case status == AgentRunStatusQueued && event == AgentRunEventAdmit:
		return AgentRunStatusAdmitted, true
	case status == AgentRunStatusAdmitted && event == AgentRunEventStart:
		return AgentRunStatusRunning, true
	case status == AgentRunStatusQueued && event == AgentRunEventCancel:
		return AgentRunStatusCancelled, true
	case status == AgentRunStatusAdmitted && event == AgentRunEventCancel:
		return AgentRunStatusCancelled, true
	case status == AgentRunStatusRunning && event == AgentRunEventSucceed:
		return AgentRunStatusSucceeded, true
	// A queued or admitted run can still fail before ever reaching Running: sandbox
	// provisioning (Ensure/Start) happens between those states and the run being
	// marked Running. Without these, a provisioning failure could not be recorded
	// and the run would stay "live" forever, blocking its subject indefinitely.
	case status == AgentRunStatusQueued && event == AgentRunEventFail:
		return AgentRunStatusFailed, true
	case status == AgentRunStatusAdmitted && event == AgentRunEventFail:
		return AgentRunStatusFailed, true
	case status == AgentRunStatusRunning && event == AgentRunEventFail:
		return AgentRunStatusFailed, true
	case status == AgentRunStatusRunning && event == AgentRunEventCancel:
		return AgentRunStatusCancelled, true
	// A queued or admitted run can also be orphaned by a process restart before it
	// ever reached Running, so it must be reapable from those states too.
	case status == AgentRunStatusQueued && event == AgentRunEventStall:
		return AgentRunStatusStalled, true
	case status == AgentRunStatusAdmitted && event == AgentRunEventStall:
		return AgentRunStatusStalled, true
	case status == AgentRunStatusRunning && event == AgentRunEventStall:
		return AgentRunStatusStalled, true
	// A host failure is reachable from the same three states as a plain failure:
	// the sandbox is built between Admitted and Running, and the agent can still
	// be killed out from under a Running round.
	case status == AgentRunStatusQueued && event == AgentRunEventHostFail:
		return AgentRunStatusHostFailed, true
	case status == AgentRunStatusAdmitted && event == AgentRunEventHostFail:
		return AgentRunStatusHostFailed, true
	case status == AgentRunStatusRunning && event == AgentRunEventHostFail:
		return AgentRunStatusHostFailed, true
	default:
		return "", false
	}
}
