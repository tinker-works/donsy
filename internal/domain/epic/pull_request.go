package epic

import (
	"fmt"
	"github.com/tinker-works/donsy/internal/domain"
	"strings"
	"time"
)

type PullRequest struct {
	ID         string
	IssueID    string
	Title      string
	Status     PullRequestStatus
	Repository string
	Number     int
	URL        string
	Head       string
	Base       string
	Flags      []PullRequestFlag
	// ReviewedHead and ReviewedBase record the commits a verdict was about.
	// A review is about a commit, not a branch: once head moves past
	// ReviewedHead the verdict no longer covers what is on the branch, and
	// the loop sends the pull request back for another review.
	ReviewedHead string
	ReviewedBase string
	// Rounds counts coding rounds spent and Reviews counts review rounds.
	// Comparing them is what tells a pull request awaiting judgement
	// (Reviews < Rounds) from one whose latest code was already judged.
	// RoundsGranted counts one-shot allowances a human added after the limit.
	//
	// Rounds counts every round of commits this branch received, whoever
	// produced them — a coding round, a merge resolution, or a person pushing
	// by hand. CodingRounds counts only the first kind, and only that is
	// rationed: falling behind base or being corrected by hand must never use
	// up the attempts an issue has to actually be implemented.
	Rounds        int
	Reviews       int
	RoundsGranted int
	CodingRounds  int
	// Approved records the last verdict. Without it, a request-changes
	// verdict is indistinguishable from never having been reviewed.
	Approved  bool
	CreatedAt time.Time
	Comments  []Comment
}

// MaxCodingRounds is how many times the loop will send one pull request back
// to a coding agent before it stops and waits for a human. A coder and a
// reviewer can disagree indefinitely; this is what makes that terminate.
const MaxCodingRounds = 10

// ReviewIsStale reports whether the recorded verdict still covers the given
// commits. An unreviewed pull request is stale by definition.
func (p PullRequest) ReviewIsStale(head, base string) bool {
	return p.ReviewedHead != head || p.ReviewedBase != base
}

// CanCode reports whether the pull request has a coding round left. Granted
// rounds are one-shot allowances a human added after the limit.
//
// Only coding is rationed. Merge resolutions and hand-pushed commits also add
// rounds, and counting those here would let a branch that keeps falling behind
// exhaust the attempts the issue has to be implemented at all.
func (p PullRequest) CanCode() bool {
	return p.CodingRounds < MaxCodingRounds+p.RoundsGranted
}

// invalidateReview drops the recorded verdict. It described commits that are
// no longer what is on the branch, and keeping it would let stale approval
// carry over to code nobody judged.
func (p *PullRequest) invalidateReview() {
	p.Approved = false
	p.ReviewedHead, p.ReviewedBase = "", ""
}

// RecordCodingRound counts a published coding pass. Reviews now trails Rounds,
// which is what sends this pull request to a reviewer next, and the previous
// verdict is dropped because it described code that is no longer at the head.
// FlagRoundLimit is recomputed here because coding is the counter it rations.
func (p *PullRequest) RecordCodingRound() error {
	p.Rounds++
	p.CodingRounds++
	p.invalidateReview()
	p.RemoveFlag(FlagFailed)
	return p.SetFlag(FlagRoundLimit, !p.CanCode())
}

// GrantCodingRound adds one coding allowance on top of the limit. It is the
// only way a pull request the loop has parked at MaxCodingRounds ever moves
// again: nothing else raises the ceiling, and IssueRole hands out no role once
// CanCode is false.
//
// It is one round rather than a reset because the limit exists to make a coder
// and a reviewer that disagree terminate. Granting one puts a human back in
// the loop each time instead of handing the pair another five rounds to repeat
// themselves in.
func (p *PullRequest) GrantCodingRound() error {
	p.RoundsGranted++
	p.RemoveFlag(FlagFailed)
	return p.SetFlag(FlagRoundLimit, !p.CanCode())
}

// RecordMergeRound counts a resolved merge. It adds a round without adding a
// coding round: the branch has commits nobody has judged, which is what puts a
// reviewer in front of it, but resolving a merge must not spend an attempt at
// implementing the issue. The verdict on record described the branch before
// base was folded into it — and a resolution is exactly where a subtle mistake
// hides — so it is dropped. The resolution is also what clears FlagStale.
func (p *PullRequest) RecordMergeRound() {
	p.Rounds++
	p.invalidateReview()
	p.RemoveFlag(FlagFailed)
	p.RemoveFlag(FlagStale)
}

// RecordExternalPush counts commits that arrived outside the loop as a round
// nobody judged. It is deliberately not a coding round: a person correcting
// the branch themselves must not use up the attempts the agent has to
// implement the issue.
func (p *PullRequest) RecordExternalPush() {
	p.Rounds++
	p.invalidateReview()
}

// RecordReview records a verdict against the commits it was about, so a later
// push invalidates it rather than inheriting an approval. Reviews catches up
// to Rounds either way: the round happened, and a verdict of request-changes
// is still a verdict.
func (p *PullRequest) RecordReview(approved bool, head, base string) error {
	p.Reviews = p.Rounds
	p.Approved = approved
	p.ReviewedHead, p.ReviewedBase = head, base
	p.RemoveFlag(FlagFailed)
	return p.SetFlag(FlagRoundLimit, !approved && !p.CanCode())
}

func CreatePullRequest(issueID, title, repository, head, base string) (PullRequest, error) {
	pullRequest := PullRequest{
		ID:         domain.MintULID(),
		IssueID:    issueID,
		Title:      strings.TrimSpace(title),
		Status:     PullRequestOpen,
		Repository: strings.TrimSpace(repository),
		Head:       strings.TrimSpace(head),
		Base:       strings.TrimSpace(base),
		CreatedAt:  time.Now().UTC(),
	}
	if err := pullRequest.Validate(); err != nil {
		return PullRequest{}, err
	}
	return pullRequest, nil
}

func (p PullRequest) Validate() error {
	if p.ID == "" || p.IssueID == "" || strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("pull request id, issue, and title are required")
	}
	if !isPullRequestStatus(p.Status) {
		return fmt.Errorf("pull request %s has invalid status %q", p.ID, p.Status)
	}
	for _, flag := range p.Flags {
		if !isPullRequestFlag(flag) {
			return fmt.Errorf("pull request %s has invalid flag %q", p.ID, flag)
		}
	}
	if p.Rounds < 0 || p.Reviews < 0 || p.RoundsGranted < 0 || p.CodingRounds < 0 {
		return fmt.Errorf("pull request %s has negative round counts", p.ID)
	}
	if p.Reviews > p.Rounds {
		return fmt.Errorf("pull request %s reviewed more rounds than it ran", p.ID)
	}
	if p.CodingRounds > p.Rounds {
		return fmt.Errorf("pull request %s counts more coding rounds than it ran", p.ID)
	}
	for _, comment := range p.Comments {
		if err := comment.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (p *PullRequest) AddComment(comment Comment) error {
	if err := comment.Validate(); err != nil {
		return err
	}
	p.Comments = append(p.Comments, comment)
	return nil
}
