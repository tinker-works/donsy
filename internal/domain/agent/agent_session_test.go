package agent

import "testing"

func TestSessionModeAfter_ShouldStartFreshWhileContinuationIsDisabled(t *testing.T) {
	subject := AgentSubject{Kind: AgentSubjectEpic, ID: "epic-1"}
	run := func(round int, status AgentRunStatus) AgentRun {
		return AgentRun{
			ID: "r", Role: AgentRoleRefiner, Subject: subject,
			Round: round, Status: status,
		}
	}

	tests := []struct {
		name     string
		previous []AgentRun
		want     SessionMode
	}{
		{"no history at all", nil, SessionModeFresh},
		{"latest round succeeded",
			[]AgentRun{run(1, AgentRunStatusSucceeded)}, SessionModeFresh},
		{"latest round failed",
			[]AgentRun{run(1, AgentRunStatusFailed)}, SessionModeFresh},
		{"latest round stalled",
			[]AgentRun{run(1, AgentRunStatusStalled)}, SessionModeFresh},
		{"a success followed by a failure",
			[]AgentRun{run(1, AgentRunStatusSucceeded), run(2, AgentRunStatusFailed)},
			SessionModeFresh},
		{"a failure followed by a success",
			[]AgentRun{run(1, AgentRunStatusFailed), run(2, AgentRunStatusSucceeded)},
			SessionModeFresh},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := SessionModeAfter(AgentEngineOpenCode, AgentRoleRefiner, test.previous)
			if got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestSessionModeAfter_ShouldStartAReviewFreshWhileContinuationIsDisabled(t *testing.T) {
	for _, role := range []AgentRole{AgentRoleIssueReviewer, AgentRolePRReviewer} {
		t.Run(string(role), func(t *testing.T) {
			previous := []AgentRun{{Role: role, Round: 1, Status: AgentRunStatusSucceeded}}

			got := SessionModeAfter(AgentEngineOpenCode, role, previous)

			if got != SessionModeFresh {
				t.Fatalf("expected a second review to start fresh, got %q", got)
			}
		})
	}
}

// Merge has nothing to resume, so its history cannot change its mode.
func TestSessionModeAfter_ShouldKeepMergeFresh(t *testing.T) {
	previous := []AgentRun{{
		Role: AgentRoleMerge, Round: 1, Status: AgentRunStatusSucceeded,
	}}
	got := SessionModeAfter(AgentEngineOpenCode, AgentRoleMerge, previous)
	if got != SessionModeFresh {
		t.Fatalf("expected a merge to start fresh, got %q", got)
	}
}

// The failure rule is not specific to the roles it was written for: a review
// that died is a transcript the next attempt must not rejoin either.
func TestSessionModeAfter_ShouldStartAReviewFreshAfterAFailure(t *testing.T) {
	previous := []AgentRun{
		{Role: AgentRolePRReviewer, Round: 1, Status: AgentRunStatusSucceeded},
		{Role: AgentRolePRReviewer, Round: 2, Status: AgentRunStatusFailed},
	}
	got := SessionModeAfter(AgentEngineOpenCode, AgentRolePRReviewer, previous)
	if got != SessionModeFresh {
		t.Fatalf("expected a retry to start fresh, got %q", got)
	}
}

// SaveAgentRun and the OpenCode command builder both gate on Validate, so an
// invariant that is too strict here stops a round before it books a sandbox — with
// no run record left behind to say why.
func TestAgentRun_Validate_ShouldAllowFreshForAResumableRole(t *testing.T) {
	run := func(role AgentRole, mode SessionMode) AgentRun {
		return AgentRun{
			ID: "run-1", SandboxID: "sandbox-1", Agent: "github-copilot/claude-sonnet-5",
			Round: 1, Role: role, Engine: AgentEngineOpenCode, SessionMode: mode,
			Status:  AgentRunStatusQueued,
			Subject: AgentSubject{Kind: AgentSubjectEpic, ID: "epic-1"},
		}
	}

	tests := []struct {
		name    string
		run     AgentRun
		wantErr bool
	}{
		{"refiner may start fresh", run(AgentRoleRefiner, SessionModeFresh), false},
		{"refiner may resume", run(AgentRoleRefiner, SessionModeContinue), false},
		{"coding may start fresh", run(AgentRoleCoding, SessionModeFresh), false},
		{"reviewer starts fresh", run(AgentRoleIssueReviewer, SessionModeFresh), false},
		{"reviewer may resume", run(AgentRoleIssueReviewer, SessionModeContinue), false},
		{"PR reviewer may resume", run(AgentRolePRReviewer, SessionModeContinue), false},
		{"merge starts fresh", run(AgentRoleMerge, SessionModeFresh), false},
		{"merge may never resume", run(AgentRoleMerge, SessionModeContinue), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run.Validate()
			if test.wantErr && err == nil {
				t.Fatal("expected the session mode to be rejected")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("expected the session mode to be accepted, got %v", err)
			}
		})
	}
}
