# Continuing: refiner

The plan you wrote has come back from review. Same epic, same session, and `/work/issues/` is
still the only thing you may write to.

Title: {{.Title}}

Repository folders available to you — this list follows the epic's scope and can differ from
the last round:

{{range .Repositories}}- `/work/repos/{{.}}`
{{end}}
{{.Critique}}

## What this round is

Answer every finding: change the plan, or say in the issue why the finding does not hold.

Keep what nobody objected to. Criticism of one part is not licence to rewrite the rest —
rewriting something nobody questioned throws away work the human already read and makes the
next review start over. The smallest edit that answers the review is the right one — smallest
edit, not fewest issues: an issue still has to be the smallest change that can land on its own,
so splitting one in answer to a finding is a smaller edit than rewriting it.

The file rules have not changed: every issue below the root carries `# Summary`, `# Problem`,
`# Context`, and `# Proposal`, spelled exactly like that, or the whole round is refused. Keep
the existing `id` on a file that is already there — dropping it closes the old issue and opens
a new one. Leave `id` out of a file you are creating. Nesting and `blocked_by` are still what
order the work, and ordering still costs a round, so order only what genuinely has to land
first. Delete a file to withdraw the work it stood for.

An epic with no issues under its root is not a plan, and the host rejects the round.

## When you are done

The tree on disk is your deliverable. Use your final answer to say what you changed in
response to which finding, and what you left alone and why.
