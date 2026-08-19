package epic

import "fmt"

type IssueState string

// The issue states track how far one issue's work has come. Coding and Review
// are the loop's own phases: an issue is in Coding while an agent is expected
// to write commits, and in Review while a verdict on those commits is owed.
// Pr means the reviewer approved and the only thing left is a human merging,
// which is why nothing but a review moves an issue into it.
const (
	IssueStateOpen   IssueState = "Open"
	IssueStateCoding IssueState = "Coding"
	IssueStateReview IssueState = "Review"
	IssueStatePR     IssueState = "Pr"
	IssueStateStale  IssueState = "Stale"
	IssueStateMerged IssueState = "Merged"
	IssueStateClosed IssueState = "Closed"
)

func (i *Issue) TransitionTo(next IssueState) error {
	if !isIssueState(next) {
		return fmt.Errorf("issue has invalid state %q", next)
	}
	if i.State == next {
		return nil
	}
	if !issueTransition(i.State, next) {
		return fmt.Errorf("cannot transition issue from %q to %q", i.State, next)
	}
	i.State = next
	return nil
}

func issueTransition(current, next IssueState) bool {
	if current == IssueStateMerged || current == IssueStateClosed {
		return false
	}
	switch next {
	case IssueStateClosed:
		// Abandoning an issue is a decision a person makes about work that no
		// longer matters, so it must not depend on the phase it stalled in.
		return true
	case IssueStateOpen:
		// Closing a pull request without merging rewinds the issue, so the
		// loop can cut a fresh branch for it rather than strand it in a phase
		// no round will ever pick up.
		return true
	case IssueStateCoding:
		return current == IssueStateOpen || current == IssueStateReview
	case IssueStateReview:
		// Stale returns here rather than to Pr: resolving a merge is a change
		// to the code, and every change on this branch gets judged before it
		// is allowed to land.
		//
		// Pr returns here when somebody pushes to the branch by hand. The
		// recorded verdict then describes commits that are no longer on it,
		// and an approval of something else is not an approval.
		return current == IssueStateCoding || current == IssueStateStale ||
			current == IssueStatePR
	case IssueStatePR:
		return current == IssueStateReview
	case IssueStateStale:
		// Only approved work goes stale. Anything earlier is still being
		// written or judged, and the coding agent merges base in as it goes.
		return current == IssueStatePR
	case IssueStateMerged:
		return current == IssueStatePR
	}
	return false
}

func isIssueState(state IssueState) bool {
	switch state {
	case IssueStateOpen, IssueStateCoding, IssueStateReview,
		IssueStatePR, IssueStateStale, IssueStateMerged, IssueStateClosed:
		return true
	default:
		return false
	}
}
