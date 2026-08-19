package epic

import "fmt"

// Blocked reports whether an issue is waiting on other work. Two things hold
// one up: its own children, and whatever its BlockedBy names.
//
// A parent cannot sensibly be implemented before the issues it decomposes into
// have landed, so the agent loop skips it until they are settled. BlockedBy is
// for the ordering nesting cannot express — a dependency on a sibling, or on
// an issue in another part of the tree entirely.
//
// Children merge into the repository's default branch, not into the parent's
// branch: stacking them would produce one enormous pull request instead of the
// piece-by-piece review the split exists to give. The dependency is about
// ordering the work, not about where the commits go.
//
// This is derived from the tree on every read rather than stored, because
// merging a blocker has to clear it and nothing would otherwise go back to
// update the waiting issue's record.
func (e *Epic) Blocked(issueID string) bool {
	issue, err := e.FindIssue(issueID)
	if err != nil {
		return false
	}
	for _, other := range e.Issues {
		if other.ParentID == issueID && !other.settled() {
			return true
		}
	}
	for _, blockerID := range issue.BlockedBy {
		// A reference the tree cannot resolve is Validate's problem. Treating it
		// as a block here would stall the issue behind something that does not
		// exist, with nothing that could ever clear it.
		if blocker, err := e.FindIssue(blockerID); err == nil && !blocker.settled() {
			return true
		}
	}
	return false
}

// validateBlockedBy checks the dependencies nesting cannot express. It runs
// after the hierarchy is known to be sound, so it can walk parents freely.
func validateBlockedBy(order []Issue, issues map[string]Issue) error {
	for _, issue := range order {
		named := make(map[string]struct{}, len(issue.BlockedBy))
		for _, blockerID := range issue.BlockedBy {
			if blockerID == issue.ID {
				return fmt.Errorf("issue %s is blocked by itself", issue.ID)
			}
			if _, exists := issues[blockerID]; !exists {
				return fmt.Errorf("issue %s is blocked by missing issue %s", issue.ID, blockerID)
			}
			if _, repeated := named[blockerID]; repeated {
				return fmt.Errorf("issue %s repeats blocker %s", issue.ID, blockerID)
			}
			named[blockerID] = struct{}{}
			if isAncestor(issues, issue, blockerID) {
				// waitsAcyclic below would catch this too, but an ancestor is the
				// one loop a plan arrives at by accident rather than by error, and
				// naming it is worth saying plainly.
				return fmt.Errorf("issue %s is blocked by its ancestor %s", issue.ID, blockerID)
			}
		}
	}
	return waitsAcyclic(order, issues)
}

func isAncestor(issues map[string]Issue, issue Issue, candidateID string) bool {
	for current := issue; current.ParentID != ""; current = issues[current.ParentID] {
		if current.ParentID == candidateID {
			return true
		}
	}
	return false
}

// waitsAcyclic rejects a loop in "waits for", which would leave every issue on
// it stalled forever with nothing that could ever clear it. Parent and child
// edges are acyclic by this point, so any loop runs through a BlockedBy.
func waitsAcyclic(order []Issue, issues map[string]Issue) error {
	children := make(map[string][]string, len(order))
	for _, issue := range order {
		if issue.ParentID != "" {
			children[issue.ParentID] = append(children[issue.ParentID], issue.ID)
		}
	}
	const (
		visiting = 1
		visited  = 2
	)
	progress := make(map[string]int, len(order))
	var walk func(id string) error
	walk = func(id string) error {
		switch progress[id] {
		case visiting:
			return fmt.Errorf("issue %s is part of a dependency cycle", id)
		case visited:
			return nil
		}
		progress[id] = visiting
		waitsFor := append([]string(nil), children[id]...)
		for _, blockerID := range issues[id].BlockedBy {
			if _, exists := issues[blockerID]; exists {
				waitsFor = append(waitsFor, blockerID)
			}
		}
		for _, next := range waitsFor {
			if err := walk(next); err != nil {
				return err
			}
		}
		progress[id] = visited
		return nil
	}
	for _, issue := range order {
		if err := walk(issue.ID); err != nil {
			return err
		}
	}
	return nil
}

// Delivered reports whether every non-root issue reached a terminal state, so
// there is nothing left for the loop to implement. An epic with no non-root
// issues is not delivered — it was never drafted.
func (e *Epic) Delivered() bool {
	delivered := false
	for _, issue := range e.Issues {
		if issue.ParentID == "" {
			continue
		}
		if !issue.settled() {
			return false
		}
		delivered = true
	}
	return delivered
}

// PullRequestFor returns the pull request recorded against an issue, whatever
// its status. The UI navigates to closed and merged records too, which is what
// separates this from OpenPullRequestFor below.
func (e *Epic) PullRequestFor(issueID string) (PullRequest, bool) {
	for _, pullRequest := range e.PullRequests {
		if pullRequest.IssueID == issueID {
			return pullRequest, true
		}
	}
	return PullRequest{}, false
}

// OpenPullRequestFor returns the issue's open pull request, if it has one.
// An issue has at most one at a time: CloseIssue closes any open record.
func (e *Epic) OpenPullRequestFor(issueID string) (PullRequest, bool) {
	for _, pullRequest := range e.PullRequests {
		if pullRequest.IssueID == issueID && pullRequest.Status == PullRequestOpen {
			return pullRequest, true
		}
	}
	return PullRequest{}, false
}

// UpdatePullRequest applies change to the named pull request in place.
func (e *Epic) UpdatePullRequest(pullRequestID string, change func(*PullRequest) error) error {
	for i := range e.PullRequests {
		if e.PullRequests[i].ID != pullRequestID {
			continue
		}
		if err := change(&e.PullRequests[i]); err != nil {
			return err
		}
		return e.PullRequests[i].Validate()
	}
	return fmt.Errorf("pull request not found")
}
