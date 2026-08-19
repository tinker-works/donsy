package epic

import "fmt"

// Status is the persisted lifecycle value for epics, issues, and pull
// requests. The aggregate-specific aliases below keep the public vocabulary
// clear without creating incompatible storage representations.
type Status string

const (
	StatusDraft      Status = "draft"
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusApproved   Status = "approved"
	StatusDone       Status = "done"
	StatusMerged     Status = "merged"
	StatusClosed     Status = "closed"
)

const (
	Draft      = StatusDraft
	Open       = StatusOpen
	InProgress = StatusInProgress
	Approved   = StatusApproved
	Done       = StatusDone
	Merged     = StatusMerged
	Closed     = StatusClosed
)

type EpicStatus = Status
type IssueStatus = Status
type PullRequestStatus = Status

func (value Status) Valid() bool {
	switch value {
	case StatusDraft, StatusOpen, StatusInProgress, StatusApproved, StatusDone, StatusMerged, StatusClosed:
		return true
	default:
		return false
	}
}

func (value Status) Terminal() bool {
	return value == StatusDone || value == StatusMerged || value == StatusClosed
}

func validEpicStatus(value Status) bool {
	switch value {
	case StatusDraft, StatusOpen, StatusInProgress, StatusDone, StatusClosed:
		return true
	default:
		return false
	}
}

func validIssueStatus(value Status) bool {
	switch value {
	case StatusOpen, StatusInProgress, StatusDone, StatusClosed:
		return true
	default:
		return false
	}
}

func validPullRequestStatus(value Status) bool {
	switch value {
	case StatusOpen, StatusApproved, StatusMerged, StatusClosed:
		return true
	default:
		return false
	}
}

func validateAggregateStatus(value Status, valid func(Status) bool, aggregate string) error {
	if !value.Valid() {
		return fmt.Errorf("unknown status %q", value)
	}
	if !valid(value) {
		return fmt.Errorf("%s does not support status %q", aggregate, value)
	}
	return nil
}

func transition(current, next Status, valid func(Status) bool, aggregate string, terminal bool) error {
	if err := validateAggregateStatus(next, valid, aggregate); err != nil {
		return err
	}
	if current == "" {
		return fmt.Errorf("current status is empty")
	}
	if err := validateAggregateStatus(current, valid, aggregate); err != nil {
		return err
	}
	if terminal || current.Terminal() {
		return fmt.Errorf("cannot transition terminal status %s", current)
	}
	if next == StatusDraft {
		return fmt.Errorf("cannot transition to draft")
	}
	return nil
}
