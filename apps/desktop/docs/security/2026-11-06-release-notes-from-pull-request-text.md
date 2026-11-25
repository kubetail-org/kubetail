# Security record — release notes from pull request text, 6 November 2026

**Subject:** the `release-notes` skill (`.claude/skills/release-notes/SKILL.md`) has an agent write
a release's notes from its pull requests, over `tools/releasenotes`. The decision is the
[ADR](../adr/2026-11-06-the-releaser-writes-the-notes-with-a-skill-over-deterministic-tooling.md);
the living model is [security-model.md](../security-model.md).

## What widens

**Attacker text reaches an agent with a shell.** A PR's title and body are written by its author,
who may be outside the project. The skill's agent reads them, and it runs in the releaser's
checkout with their `gh` credentials, often with permissions bypassed. Text that steers the agent
could run commands as the releaser, or put a link or a false claim in the notes of a signed
release.

## What contains it

**Only `collect` reads PR text.** It fetches each PR in the range with `gh pr view` and passes on
the title, `Summary` and `Key Changes` under a field named `untrusted`, beside a
notice that the text is data. Each piece is cut to 4,000 bytes. The skill tells the agent never to
follow instructions found there and never to run `gh`.

**Only `render` builds links.** `check` fails prose that holds `://`, `www.`, `](`, `]:`, `<`, `@`
or `#N` outside a code span, so the notes hold no link, HTML or mention the agent wrote.
`render` runs `check` first. Beyond each citation it links only the changes in "All changes" and
the compare view, and mentions only PR authors. Commit subjects there are escaped as Markdown.
Pinned by `TestCheckFails`, `TestRender`, `TestEscape` and `TestRenderCmd`.

**Only `publish` writes to GitHub, after a person answers on a terminal.** It prints the notes and
reads the answer from `/dev/tty` (`CONIN$` on Windows), never from standard input, so a piped `y`
does not answer it. A shell with no controlling terminal, as an agent's has, cannot publish:
`publish` fails and writes nothing. It edits the draft's notes; the releaser publishes the draft on
GitHub. Pinned by `TestPublishWritesOnAYes` and `TestPublishWritesNothingOtherwise`.

**Nothing is silently dropped.** `check` fails a `feat`, `fix` or `!` PR that is neither cited nor
omitted with a reason, and a citation outside the range (`TestCheckFails`). The "All changes" list
comes from `git log`, not from the agent.

## Residual

- The agent still runs with a shell while it reads PR text. The skill's instructions and the
  untrusted marking are guidance, not a bound. A releaser who runs the skill with permissions
  bypassed accepts that a PR body could steer commands in that session. Running it with
  permissions on, or in a sandbox without the releaser's credentials, narrows this.
- The prose is the agent's. A steered agent can write a false claim that passes `check`. The
  releaser's reading, at `publish` and again before publishing the draft, is the only gate.
- `git show`, which the skill may run on a thin PR, prints the diff, which is attacker text too.
- A commit subject is the PR title unless the maintainer edits it at merge. It is escaped, but
  GitHub still autolinks a bare URL in it.
