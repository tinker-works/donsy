package epic

import "fmt"

// PullRequestFlag is a tag the agent loop reads and writes alongside a pull
// request's status. Flags are deliberately not states: a pull request that is
// blocked or out of rounds is still open, and the FSM in
// pull_request_state.go stays a three-value machine.
type PullRequestFlag string

const (
	// FlagBlocked marks a pull request whose issue is still waiting on other
	// work. It is recomputed from the tree on every sweep, never set by hand,
	// because merging what it waits on clears it.
	FlagBlocked PullRequestFlag = "blocked"
	// FlagRoundLimit marks a pull request that has spent its coding rounds.
	// The loop stops starting rounds until a human grants another.
	FlagRoundLimit PullRequestFlag = "round-limit"
	// FlagFailed marks a pull request whose last round errored.
	FlagFailed PullRequestFlag = "failed"
	// FlagHumanNeeded marks a pull request the loop cannot advance on its own.
	FlagHumanNeeded PullRequestFlag = "human-needed"
	// FlagStale marks a pull request whose branch no longer contains its base,
	// so it cannot be published as-is. The merge role clears it.
	FlagStale PullRequestFlag = "stale"
)

func isPullRequestFlag(flag PullRequestFlag) bool {
	switch flag {
	case FlagBlocked, FlagRoundLimit, FlagFailed, FlagHumanNeeded, FlagStale:
		return true
	default:
		return false
	}
}

func (p PullRequest) HasFlag(flag PullRequestFlag) bool {
	for _, current := range p.Flags {
		if current == flag {
			return true
		}
	}
	return false
}

func (p *PullRequest) AddFlag(flag PullRequestFlag) error {
	if !isPullRequestFlag(flag) {
		return fmt.Errorf("unknown pull request flag %q", flag)
	}
	if p.HasFlag(flag) {
		return nil
	}
	p.Flags = append(p.Flags, flag)
	return nil
}

func (p *PullRequest) RemoveFlag(flag PullRequestFlag) {
	remaining := make([]PullRequestFlag, 0, len(p.Flags))
	for _, current := range p.Flags {
		if current != flag {
			remaining = append(remaining, current)
		}
	}
	if len(remaining) == 0 {
		p.Flags = nil
		return
	}
	p.Flags = remaining
}

// SetFlag adds or removes a flag to match present, so a caller recomputing a
// derived flag does not have to branch.
func (p *PullRequest) SetFlag(flag PullRequestFlag, present bool) error {
	if present {
		return p.AddFlag(flag)
	}
	p.RemoveFlag(flag)
	return nil
}
