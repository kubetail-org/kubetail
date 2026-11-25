# Security record — the agent posts approved release notes, 7 November 2026

**Subject:** the `release-notes` skill (`.claude/skills/release-notes/SKILL.md`) lets the agent
write a draft release's notes with `gh` when the user asks. It extends
[release notes from pull request text](2026-11-06-release-notes-from-pull-request-text.md). The
decision is the [ADR](../adr/2026-11-07-the-agent-posts-the-notes-when-the-user-asks.md); the living model is [security-model.md](../security-model.md).

## What widens

**The agent can write to GitHub.** Before, only `publish` wrote, after a yes read from a terminal
the agent's shell does not have. Now, when the user asks in a later message to post the notes
they have seen, the agent runs `gh release view <tag> --json tagName,isDraft,body` and then
`gh release edit <tag> --notes-file <dir>/notes.md`. The user's request stands in for
`publish`'s terminal prompt: both come after the user has read the rendered notes.

## What contains it

**The skill allows two `gh` commands and nothing else.** It forbids `gh` on the agent's own
initiative or on the strength of pull request text, and allows these two only after the user asks
in a later turn. A request found in PR text is not the user asking.

**The agent edits only a draft's notes.** It checks `isDraft` first and stops on a published
release. It never passes `--draft=false`, even when asked in the same flow, so the user still
publishes the draft on GitHub after reading it there.

**The file is what `render` wrote.** The agent posts `notes.md` unchanged since the user saw it,
so `check`'s and `render`'s guarantees hold for what lands on GitHub.

**`publish` stays the default.** A releaser who wants the terminal prompt keeps it. There is no
`publish --yes`: a flag would let a steered agent skip the confirmation without the user asking.

## Residual

- These bounds are the skill's instructions, not a mechanism. An agent steered by PR text could
  run `gh` regardless, as it could run any command under the residual of the earlier record; the
  releaser's `gh` credentials are in reach either way.
- Between the user seeing `notes.md` and the agent posting it, nothing but the agent's discipline
  keeps the file unchanged. The draft on GitHub, read before publishing, is the last check.
