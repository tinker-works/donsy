package agent

import (
	"testing"
	"time"
)

// Every role that gets a second round on the same subject resumes its own
// conversation. The table is written against Roles() rather than a copy of the
// list so that a role added later has to answer this question deliberately
// instead of inheriting whichever branch the condition happens to fall through
// to.
func TestSessionModeFor_ShouldContinueEveryOpenCodeRoleButMerge(t *testing.T) {
	// Arrange
	want := map[AgentRole]SessionMode{
		AgentRoleRefiner:       SessionModeContinue,
		AgentRoleIssueReviewer: SessionModeContinue,
		AgentRoleCoding:        SessionModeContinue,
		AgentRolePRReviewer:    SessionModeContinue,
		AgentRoleMerge:         SessionModeFresh,
	}

	for _, role := range Roles() {
		t.Run(string(role), func(t *testing.T) {
			expected, ok := want[role]
			if !ok {
				t.Fatalf("role %q has no session mode decided for it", role)
			}

			// Act
			got := SessionModeFor(AgentEngineOpenCode, role)

			// Assert
			if got != expected {
				t.Fatalf("expected %q, got %q", expected, got)
			}
		})
	}
}

// An engine go-merge does not drive has no session of its own to resume, so the
// role allow-list must not be the only thing consulted.
func TestSessionModeFor_ShouldStartFreshForAnotherEngine(t *testing.T) {
	// Act
	got := SessionModeFor(AgentEngine("codex"), AgentRoleCoding)

	// Assert
	if got != SessionModeFresh {
		t.Fatalf("expected an unknown engine to start fresh, got %q", got)
	}
}

// A round the host never ran is not evidence about the work: a full disk or an
// image that will not build says the same thing about every subject on the
// machine at once, so counting it turns one host fault into every epic on it
// failing, blamed on the role.
func TestAgentRun_CountsTowardRoundLimit_ShouldExcludeOnlyHostFailures(t *testing.T) {
	// Arrange
	tests := []struct {
		status AgentRunStatus
		want   bool
	}{
		{AgentRunStatusSucceeded, true},
		{AgentRunStatusFailed, true},
		{AgentRunStatusStalled, true},
		{AgentRunStatusCancelled, true},
		{AgentRunStatusHostFailed, false},
	}

	for _, test := range tests {
		t.Run(string(test.status), func(t *testing.T) {
			// Act
			got := AgentRun{Status: test.status}.CountsTowardRoundLimit()

			// Assert
			if got != test.want {
				t.Fatalf("expected %t for %q, got %t", test.want, test.status, got)
			}
		})
	}
}

func TestSandbox_Apply_ShouldFollowLifecycle(t *testing.T) {
	// Arrange
	sandbox := Sandbox{Status: SandboxStatusAbsent}
	events := []SandboxEvent{
		SandboxEventCreate, SandboxEventReady, SandboxEventStart, SandboxEventReady,
		SandboxEventStop, SandboxEventStopped,
	}

	// Act
	for _, event := range events {
		if err := sandbox.Apply(event); err != nil {
			t.Fatal(err)
		}
	}

	// Assert
	if sandbox.Status != SandboxStatusStopped {
		t.Fatalf("expected stopped sandbox, got %q", sandbox.Status)
	}
}

func TestAgentRun_Apply_ShouldSetTimestampsForRunningAndTerminalStates(t *testing.T) {
	// Arrange
	run := AgentRun{Status: AgentRunStatusQueued}
	at := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)

	// Act
	for _, event := range []AgentRunEvent{
		AgentRunEventAdmit, AgentRunEventStart, AgentRunEventSucceed,
	} {
		if err := run.Apply(event, at); err != nil {
			t.Fatal(err)
		}
	}

	// Assert
	if run.Status != AgentRunStatusSucceeded || run.StartedAt == nil || run.FinishedAt == nil ||
		!run.StartedAt.Equal(at) || !run.FinishedAt.Equal(at) {
		t.Fatalf("unexpected run: %#v", run)
	}
}

// Merge folds base in once, so there is no earlier conversation about that
// branch for a round to claim it is resuming.
func TestAgentRun_Validate_ShouldRejectMergeContinuation(t *testing.T) {
	// Arrange
	run := AgentRun{
		ID: "run-1", SandboxID: "sandbox-1", Role: AgentRoleMerge,
		Subject: AgentSubject{Kind: AgentSubjectIssue, ID: "issue-1"},
		Engine:  AgentEngineOpenCode, Agent: "merger", SessionMode: SessionModeContinue,
		Status: AgentRunStatusQueued, Round: 1,
	}

	// Act
	err := run.Validate()

	// Assert
	if err == nil {
		t.Fatal("expected merge continuation to be rejected")
	}
}

func TestAgentRun_Apply_ShouldFailFromQueuedOrAdmitted(t *testing.T) {
	// A run can fail while still Queued or Admitted, before it ever reaches
	// Running: sandbox provisioning happens between those states. Without these
	// transitions a provisioning failure could not be recorded, and the run
	// would stay "live" forever.
	// Arrange
	tests := []struct {
		name string
		from AgentRunStatus
	}{
		{name: "queued", from: AgentRunStatusQueued},
		{name: "admitted", from: AgentRunStatusAdmitted},
	}
	at := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := AgentRun{Status: test.from}

			// Act
			err := run.Apply(AgentRunEventFail, at)

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != AgentRunStatusFailed || run.FinishedAt == nil || !run.FinishedAt.Equal(at) {
				t.Fatalf("unexpected run: %#v", run)
			}
		})
	}
}

func TestAgentRun_Apply_ShouldStallFromAnyLiveStatus(t *testing.T) {
	// Arrange
	tests := []struct {
		name string
		from AgentRunStatus
	}{
		{name: "queued", from: AgentRunStatusQueued},
		{name: "admitted", from: AgentRunStatusAdmitted},
		{name: "running", from: AgentRunStatusRunning},
	}
	at := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := AgentRun{Status: test.from}

			// Act
			err := run.Apply(AgentRunEventStall, at)

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != AgentRunStatusStalled || run.FinishedAt == nil || !run.FinishedAt.Equal(at) {
				t.Fatalf("unexpected run: %#v", run)
			}
		})
	}
}

func TestAgentRun_Apply_ShouldRejectFailOrStallFromTerminalStatus(t *testing.T) {
	// Arrange
	at := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		from  AgentRunStatus
		event AgentRunEvent
	}{
		{name: "succeeded fail", from: AgentRunStatusSucceeded, event: AgentRunEventFail},
		{name: "failed fail", from: AgentRunStatusFailed, event: AgentRunEventFail},
		{name: "cancelled stall", from: AgentRunStatusCancelled, event: AgentRunEventStall},
		{name: "stalled stall", from: AgentRunStatusStalled, event: AgentRunEventStall},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := AgentRun{Status: test.from}

			// Act
			err := run.Apply(test.event, at)

			// Assert
			if err == nil {
				t.Fatalf("expected %q from %q to be rejected", test.event, test.from)
			}
			if run.Status != test.from {
				t.Fatalf("rejected event changed status to %q", run.Status)
			}
		})
	}
}

func TestSandbox_Apply_ShouldRejectInvalidTransition(t *testing.T) {
	// Arrange
	sandbox := Sandbox{Status: SandboxStatusAbsent}

	// Act
	err := sandbox.Apply(SandboxEventStart)

	// Assert
	if err == nil {
		t.Fatal("expected start from absent to be rejected")
	}
	if sandbox.Status != SandboxStatusAbsent {
		t.Fatalf("rejected event changed status to %q", sandbox.Status)
	}
}

func TestSandbox_Reconcile_ShouldAdoptStatusTheFSMWouldReject(t *testing.T) {
	// Arrange: Running to Stopped needs two events through Apply, but a crashed
	// process or an out-of-band stop leaves the real instance there directly.
	sandbox := Sandbox{Status: SandboxStatusRunning}

	// Act
	err := sandbox.Reconcile(SandboxStatusStopped)

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.Status != SandboxStatusStopped {
		t.Fatalf("expected stopped sandbox, got %q", sandbox.Status)
	}
}

func TestSandbox_Reconcile_ShouldRejectUnknownStatusWithoutChangingIt(t *testing.T) {
	// Arrange
	sandbox := Sandbox{Status: SandboxStatusRunning}

	// Act
	err := sandbox.Reconcile(SandboxStatus("unknown"))

	// Assert
	if err == nil {
		t.Fatal("expected unknown status to be rejected")
	}
	if sandbox.Status != SandboxStatusRunning {
		t.Fatalf("rejected reconcile changed status to %q", sandbox.Status)
	}
}

func TestSandbox_Apply_ShouldFailFromAnyNonTerminalStatus(t *testing.T) {
	// Arrange
	tests := []SandboxStatus{
		SandboxStatusCreating, SandboxStatusStopped, SandboxStatusStarting,
		SandboxStatusRunning, SandboxStatusStopping,
	}

	for _, from := range tests {
		t.Run(string(from), func(t *testing.T) {
			sandbox := Sandbox{Status: from}

			// Act
			err := sandbox.Apply(SandboxEventFail)

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			if sandbox.Status != SandboxStatusBroken {
				t.Fatalf("expected broken sandbox, got %q", sandbox.Status)
			}
		})
	}
}

func TestSandbox_Apply_ShouldDeleteFromBrokenStatus(t *testing.T) {
	// A sandbox that failed to provision or run must still be reclaimable, or a
	// broken sandbox would be stuck forever with no automatic way to clean it up.
	// Arrange
	sandbox := Sandbox{Status: SandboxStatusBroken}

	// Act
	if err := sandbox.Apply(SandboxEventDelete); err != nil {
		t.Fatal(err)
	}
	if err := sandbox.Apply(SandboxEventDeleteDone); err != nil {
		t.Fatal(err)
	}

	// Assert
	if sandbox.Status != SandboxStatusAbsent {
		t.Fatalf("expected absent sandbox, got %q", sandbox.Status)
	}
}

func TestSandbox_Validate_ShouldAcceptSandboxWithoutProject(t *testing.T) {
	// Arrange: no ProjectID on purpose — the golden base image boots through a
	// host-global sandbox that belongs to no project.
	sandbox := Sandbox{
		ID: "sandbox-1", Name: "go-merge-base", Role: AgentRoleCoding,
		Subject: AgentSubject{Kind: AgentSubjectEpic, ID: "go-merge-base"},
		Status:  SandboxStatusCreating,
	}

	// Act
	err := sandbox.Validate()

	// Assert
	if err != nil {
		t.Fatal(err)
	}
}

func TestSandbox_Ref_ShouldCarryTheProjectAlongsideTheName(t *testing.T) {
	// Arrange: the project half is the point of the type. A runtime reaches a
	// sandbox through its project's host, so an address that dropped the project
	// would leave the adapter parsing it back out of the name.
	sandbox := Sandbox{
		ID: "sandbox-1", ProjectID: 7, Name: "gm-7-issue-coding",
		Role:    AgentRoleCoding,
		Subject: AgentSubject{Kind: AgentSubjectIssue, ID: "issue-1"},
		Status:  SandboxStatusStopped,
	}

	// Act
	ref := sandbox.Ref()

	// Assert
	if ref.ProjectID != 7 || ref.Name != "gm-7-issue-coding" {
		t.Fatalf("expected the project and name in the reference, got %+v", ref)
	}
}

func TestSandbox_Validate_ShouldRejectInvalidSandbox(t *testing.T) {
	// Arrange
	valid := Sandbox{
		ID: "sandbox-1", Name: "project-coding-issue-1", Role: AgentRoleCoding,
		Subject: AgentSubject{Kind: AgentSubjectIssue, ID: "issue-1"},
		Status:  SandboxStatusStopped,
	}
	tests := []struct {
		name   string
		mutate func(sandbox *Sandbox)
	}{
		{name: "missing id", mutate: func(sandbox *Sandbox) { sandbox.ID = "" }},
		{name: "missing name", mutate: func(sandbox *Sandbox) { sandbox.Name = "" }},
		{name: "invalid role", mutate: func(sandbox *Sandbox) { sandbox.Role = "janitor" }},
		{name: "invalid status", mutate: func(sandbox *Sandbox) { sandbox.Status = "hibernating" }},
		{name: "invalid subject", mutate: func(sandbox *Sandbox) { sandbox.Subject.Kind = "unknown" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sandbox := valid
			test.mutate(&sandbox)

			// Act
			err := sandbox.Validate()

			// Assert
			if err == nil {
				t.Fatal("expected invalid sandbox to be rejected")
			}
		})
	}
}

func TestAgentSubject_Validate_ShouldRejectMissingFields(t *testing.T) {
	// Arrange
	tests := []struct {
		name    string
		subject AgentSubject
	}{
		{name: "invalid kind", subject: AgentSubject{Kind: "unknown", ID: "epic-1"}},
		{name: "missing id", subject: AgentSubject{Kind: AgentSubjectEpic, ID: ""}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Act
			err := test.subject.Validate()

			// Assert
			if err == nil {
				t.Fatal("expected invalid subject to be rejected")
			}
		})
	}
}

func TestRunUsage_Reported_ShouldTellNothingApartFromNothingRecorded(t *testing.T) {
	// Arrange: a run that used nothing and a run recorded before accounting
	// existed would both render as zeros presented as fact.
	cases := map[string]struct {
		usage RunUsage
		want  bool
	}{
		"nothing recorded": {RunUsage{}, false},
		"tokens in":        {RunUsage{TokensIn: 1}, true},
		"tokens out":       {RunUsage{TokensOut: 1}, true},
		"cost only":        {RunUsage{CostUSD: 0.01}, true},
	}

	// Act & Assert
	for name, tc := range cases {
		if got := tc.usage.Reported(); got != tc.want {
			t.Fatalf("%s: expected %t, got %t", name, tc.want, got)
		}
	}
}

func TestAgentRun_Validate_ShouldRejectAMalformedRecord(t *testing.T) {
	// Arrange: every case is a record a caller bug or a hand-edited store can
	// produce, and each must be refused rather than persisted.
	base := func() AgentRun {
		return AgentRun{
			ID: "run-1", SandboxID: "sandbox-1", Role: AgentRoleCoding, Agent: "opencode",
			Engine: AgentEngineOpenCode, SessionMode: SessionModeFresh,
			Status: AgentRunStatusQueued, Round: 1,
			Subject: AgentSubject{Kind: AgentSubjectIssue, ID: "cart"},
		}
	}
	cases := map[string]func(*AgentRun){
		"no id":          func(r *AgentRun) { r.ID = "" },
		"no sandbox":     func(r *AgentRun) { r.SandboxID = "" },
		"no agent":       func(r *AgentRun) { r.Agent = "" },
		"round zero":     func(r *AgentRun) { r.Round = 0 },
		"unknown role":   func(r *AgentRun) { r.Role = "nonsense" },
		"unknown engine": func(r *AgentRun) { r.Engine = "nonsense" },
		"unknown mode":   func(r *AgentRun) { r.SessionMode = "nonsense" },
		"unknown status": func(r *AgentRun) { r.Status = "nonsense" },
		"bad subject":    func(r *AgentRun) { r.Subject = AgentSubject{} },
	}

	// Act & Assert
	for name, break_ := range cases {
		run := base()
		break_(&run)
		if err := run.Validate(); err == nil {
			t.Fatalf("expected %q to be refused", name)
		}
	}
	valid := base()
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected the baseline record to validate: %v", err)
	}
}

func TestSessionModeAfter_ShouldStartFreshAfterAnyPreviousRoundWhileDisabled(t *testing.T) {
	// Arrange
	failed := []AgentRun{{
		ID: "run-1", Role: AgentRoleCoding, Status: AgentRunStatusFailed, Round: 1,
	}}
	succeeded := []AgentRun{{
		ID: "run-1", Role: AgentRoleCoding, Status: AgentRunStatusSucceeded, Round: 1,
	}}

	// Act & Assert
	if got := SessionModeAfter(AgentEngineOpenCode, AgentRoleCoding, failed); got != SessionModeFresh {
		t.Fatalf("expected a fresh session after a failure, got %q", got)
	}
	if got := SessionModeAfter(
		AgentEngineOpenCode, AgentRoleCoding, succeeded); got != SessionModeFresh {
		t.Fatalf("expected a fresh session after a success, got %q", got)
	}
	if got := SessionModeAfter(AgentEngineOpenCode, AgentRoleCoding, nil); got != SessionModeFresh {
		t.Fatalf("expected a first round to start fresh, got %q", got)
	}
}
