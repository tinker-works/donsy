## Where you are running

You are inside a virtual machine that exists for this one round. It is disposable, and it
is the only place your work has any effect.

The round's working directories are under `/work`:

- `/work/issues/` — the issue tree as Markdown files. Your prompt says whether you may
  write here.
- `/work/repo/` — the repository this round implements, when the round has one. This
  remains the editing directory for coding work.
- `/work/repos/<owner>__<name>/` — one checkout per repository this round can read.
- `/run/go-merge/` — read-only. What the host put here for this round.

Rounds that verify repository work have a Docker client connected to the profile daemon.
Use `docker info` to confirm access. Issue coding and review rounds set
`GO_MERGE_DOCKER_BIND_SOURCE` to the checkout's host-absolute path; use that variable,
not `/work/repo`, as the source of Docker or Testcontainers bind mounts.

Your role prompt names the writable paths. The host resets disposable repository checkouts
before the next round; do not leave source changes there as a deliverable.

## No route to GitHub

There is no GitHub credential in this machine and nothing here can reach the GitHub API.
That is deliberate.

The host owns publishing. It takes what you leave in the writable directory, checks it,
and pushes on your behalf. Everything you want a person to read, you say in your final
answer.

## One directory that is closed

`.github/` may not be changed, in any commit, in any repository.

It holds input the *host* acts on: workflows that would run with the repository's Actions
secrets, and `CODEOWNERS`, which decides who reviews the work this loop is asking a human
to merge. A round that could edit it would be writing instructions for the host rather
than code for the repository, so the gate rejects the whole branch — not just that file.

If the work genuinely needs a change in there, do not attempt it. Say so in your answer:
name the file and what it would have needed. Nothing downstream of you can make that
change either, so saying it plainly is what keeps it from reading as an oversight.
