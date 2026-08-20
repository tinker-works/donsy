package prompts

import (
	"bytes"
	_ "embed"
	"fmt"
	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
	"strings"
	"text/template"
)

//go:embed epic_refiner.md
var epicRefiner string

//go:embed epic_refiner_continue.md
var epicRefinerContinue string

//go:embed epic_reviewer.md
var epicReviewer string

//go:embed epic_reviewer_continue.md
var epicReviewerContinue string

// epicTemplate picks the template for one role in one session mode. See
// issueTemplate for why the mode is part of the lookup rather than a check
// inside it.
func epicTemplate(role agent.AgentRole, mode agent.SessionMode) (string, error) {
	switch {
	case role == agent.AgentRoleRefiner && mode == agent.SessionModeFresh:
		return epicRefiner, nil
	case role == agent.AgentRoleRefiner && mode == agent.SessionModeContinue:
		return epicRefinerContinue, nil
	case role == agent.AgentRoleIssueReviewer && mode == agent.SessionModeFresh:
		return epicReviewer, nil
	case role == agent.AgentRoleIssueReviewer && mode == agent.SessionModeContinue:
		return epicReviewerContinue, nil
	}
	return "", fmt.Errorf("epic loop does not support role %q in %q mode", role, mode)
}

// Epic renders the editable role prompt compiled into the binary, with the
// shared environment section appended the way every issue-scoped role gets it.
func Epic(detail epic.Epic, role agent.AgentRole, mode agent.SessionMode) (string, error) {
	source, err := epicTemplate(role, mode)
	if err != nil {
		return "", err
	}
	tmpl, err := template.New(string(role)).Option("missingkey=error").Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse %s prompt: %w", role, err)
	}
	var rendered bytes.Buffer
	data := struct {
		Title        string
		Body         string
		Repositories []string
		Critique     string
	}{
		Title: detail.Title, Body: detail.Body, Repositories: detail.Repositories,
		Critique: critiqueSection(detail),
	}
	if err := tmpl.Execute(&rendered, data); err != nil {
		return "", fmt.Errorf("render %s prompt: %w", role, err)
	}
	return rendered.String() + "\n" + environmentFor(mode), nil
}

// critiqueSection puts the last review in front of the refiner. It is stored as
// a comment on the root issue and reaches the guest as root-comments.md, but a
// round that has to go looking for its own feedback is a round that ignores it.
func critiqueSection(detail epic.Epic) string {
	root, err := detail.RootIssue()
	if err != nil {
		return noCritique
	}
	latest := ""
	for _, comment := range root.Comments {
		if comment.Author == string(agent.AgentRoleIssueReviewer) {
			latest = strings.TrimSpace(comment.Body)
		}
	}
	if latest == "" {
		return noCritique
	}
	return "## The last review of this plan\n\n" + latest +
		"\n\nThis is what the round has to answer. Address every finding, either by " +
		"changing the plan or by saying in the issue why the finding does not hold."
}

const noCritique = "## No review yet\n\nThis is the first pass over this epic; " +
	"nothing has been reviewed yet."
