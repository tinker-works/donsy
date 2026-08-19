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

func validateStatus(value Status) error {
	if !value.Valid() {
		return fmt.Errorf("unknown status %q", value)
	}
	return nil
}

func transition(current, next Status, terminal bool) error {
	if err := validateStatus(next); err != nil {
		return err
	}
	if current == "" {
		return fmt.Errorf("current status is empty")
	}
	if terminal || current.Terminal() {
		return fmt.Errorf("cannot transition terminal status %s", current)
	}
	if next == StatusDraft {
		return fmt.Errorf("cannot transition to draft")
	}
	return nil
}
