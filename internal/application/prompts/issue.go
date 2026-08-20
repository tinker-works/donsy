package prompts

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"

	"github.com/tinker-works/donsy/internal/domain/agent"
)

//go:embed issue_coding.md
var issueCoding string

//go:embed issue_coding_continue.md
var issueCodingContinue string

//go:embed issue_pr_reviewer.md
var issuePRReviewer string

//go:embed issue_pr_reviewer_continue.md
var issuePRReviewerContinue string

//go:embed issue_merge.md
var issueMerge string

//go:embed environment.md
var environment string

//go:embed environment_continue.md
var environmentContinue string

// environmentFor picks how much of the machine a round has to be told about.
// environment.md is static, so a resumed session already holds it word for word
// and re-sending it is most of what a continue prompt exists to avoid. What the
// short version keeps is the handful of rules that break a round silently when
// forgotten rather than loudly: what is writable, and that .github/ is not.
func environmentFor(mode agent.SessionMode) string {
	if mode == agent.SessionModeContinue {
		return environmentContinue
	}
	return environment
}

// IssueContext is everything an issue-scoped role's prompt can reference.
// text/template is parsed with missingkey=error, so a field a template names
// must exist here — adding a placeholder to a template without adding it here
// fails at render rather than silently producing "<no value>".
type IssueContext struct {
	IssuePath  string
	IssueTitle string
	RepoDir    string
	Branch     string
	BaseBranch string
	BaseCommit string
	// Conversation is the pre-rendered prior discussion, already a Markdown
	// section, or a sentence saying there is none.
	Conversation string
	// Repositories are mount folder names (owner__name) for read-only
	// checkouts other than RepoDir.
	Repositories []string
}

// issueTemplate picks the template for one role in one session mode. It is a
// total function over the pair rather than a switch on role with a mode check
// inside: the zero SessionMode is not SessionModeFresh, and falling through to
// the fresh template for an unmapped mode is the kind of wrong that reads as
// working.
func issueTemplate(role agent.AgentRole, mode agent.SessionMode) (string, error) {
	switch {
	case role == agent.AgentRoleCoding && mode == agent.SessionModeFresh:
		return issueCoding, nil
	case role == agent.AgentRoleCoding && mode == agent.SessionModeContinue:
		return issueCodingContinue, nil
	case role == agent.AgentRolePRReviewer && mode == agent.SessionModeFresh:
		return issuePRReviewer, nil
	case role == agent.AgentRolePRReviewer && mode == agent.SessionModeContinue:
		return issuePRReviewerContinue, nil
	// Merge folds base in once. There is no second round on that branch to
	// resume, so it has no continue prompt to render.
	case role == agent.AgentRoleMerge && mode == agent.SessionModeFresh:
		return issueMerge, nil
	}
	return "", fmt.Errorf("issue loop does not support role %q in %q mode", role, mode)
}

// Issue renders an issue-scoped role prompt with the shared environment
// section appended, the way every role gets the same description of the
// machine it is running in.
func Issue(context IssueContext, role agent.AgentRole, mode agent.SessionMode) (string, error) {
	source, err := issueTemplate(role, mode)
	if err != nil {
		return "", err
	}
	tmpl, err := template.New(string(role)).Option("missingkey=error").Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse %s prompt: %w", role, err)
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, context); err != nil {
		return "", fmt.Errorf("render %s prompt: %w", role, err)
	}
	return rendered.String() + "\n" + environmentFor(mode), nil
}
