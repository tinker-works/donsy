package usecases

import (
	"fmt"
	"strings"
)

// protectedPrefixes are paths an agent may never change, in any commit, in
// any repository. They hold input the *host* acts on: workflows that run with
// the repository's own Actions secrets, and CODEOWNERS, which decides who
// reviews the work this loop asks a human to merge. A round that could edit
// them would be writing instructions for the host rather than code for the
// repository.
var protectedPrefixes = []string{".github/"}

// RunCommit is one commit a round authored, with the paths it touched.
type RunCommit struct {
	Hash string
	// Ancestors reports whether this commit descends from the recorded base.
	// A commit that does not means history was rewritten under us.
	DescendsFromBase bool
	Paths            []string
}

// EvaluatePushGate decides whether a round's work may be published. It fails
// closed: anything it cannot verify is a refusal, because the alternative is
// pushing a branch nobody checked.
//
// Rejecting is branch-wide, not per-file. A round that edited a protected
// path was operating outside what the loop grants it, and keeping the rest of
// its commits would mean publishing work from that same round.
func EvaluatePushGate(base string, commits []RunCommit) error {
	if strings.TrimSpace(base) == "" {
		return fmt.Errorf("push gate: no base commit was recorded for this round")
	}
	for _, commit := range commits {
		if !commit.DescendsFromBase {
			return fmt.Errorf(
				"push gate: commit %s does not descend from base %s; history was rewritten",
				short(commit.Hash), short(base),
			)
		}
		for _, path := range commit.Paths {
			if protected(path) {
				return fmt.Errorf(
					"push gate: commit %s changes protected path %q",
					short(commit.Hash), path,
				)
			}
		}
	}
	return nil
}

// protected matches root-level protected directories, case-folded. A nested
// `docs/.github/` is somebody's documentation, not the host's input.
func protected(path string) bool {
	normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(path), "./"))
	for _, prefix := range protectedPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func short(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}
