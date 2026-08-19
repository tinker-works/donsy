package epic

import "fmt"

type PullRequestEvent string

const (
	PullRequestEventMerge PullRequestEvent = "merge"
	PullRequestEventClose PullRequestEvent = "close"
)

func (p *PullRequest) Apply(event PullRequestEvent) error {
	if !isPullRequestStatus(p.Status) {
		return fmt.Errorf("pull request has invalid status %q", p.Status)
	}
	next, ok := pullRequestTransition(p.Status, event)
	if !ok {
		return fmt.Errorf("cannot apply pull request event %q from status %q", event, p.Status)
	}
	p.Status = next
	return nil
}

func (p *PullRequest) TransitionTo(next PullRequestStatus) error {
	if !isPullRequestStatus(next) {
		return fmt.Errorf("pull request has invalid status %q", next)
	}
	if p.Status == next {
		return nil
	}

	event, ok := pullRequestEventForTransition(p.Status, next)
	if !ok {
		return fmt.Errorf("cannot transition pull request from %q to %q", p.Status, next)
	}
	return p.Apply(event)
}

func isPullRequestStatus(status PullRequestStatus) bool {
	switch status {
	case PullRequestOpen, PullRequestClosed, PullRequestMerged:
		return true
	default:
		return false
	}
}

func pullRequestEventForTransition(current, next PullRequestStatus) (PullRequestEvent, bool) {
	switch {
	case current == PullRequestOpen && next == PullRequestMerged:
		return PullRequestEventMerge, true
	case current == PullRequestOpen && next == PullRequestClosed:
		return PullRequestEventClose, true
	}
	return "", false
}

func pullRequestTransition(
	status PullRequestStatus,
	event PullRequestEvent,
) (PullRequestStatus, bool) {
	if status != PullRequestOpen {
		return "", false
	}
	switch event {
	case PullRequestEventMerge:
		return PullRequestMerged, true
	case PullRequestEventClose:
		return PullRequestClosed, true
	default:
		return "", false
	}
}
