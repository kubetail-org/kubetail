---
title: The agent posts a draft's release notes with gh when the user asks
date: 2026-11-07
scope: repo
status: Accepted
amends:
  - [GitHub Releases is the changelog, and the releaser writes the notes with a skill over deterministic tooling](2026-11-06-the-releaser-writes-the-notes-with-a-skill-over-deterministic-tooling.md)
---

# The agent posts a draft's release notes with gh when the user asks

Amends [GitHub Releases is the changelog, and the releaser writes the notes with a skill over deterministic tooling](2026-11-06-the-releaser-writes-the-notes-with-a-skill-over-deterministic-tooling.md):
it replaces "only `publish` writes to GitHub".

## Context

`publish` reads its yes from the terminal, and an agent's shell has none. So when the releaser
had read the rendered notes and asked the agent to post them, the agent had to refuse and hand
back a command (#790). For `desktop/v0.0.1-rc.1` the releaser had to grant `gh` by hand.

The terminal prompt exists so a person confirms the notes after reading them, and so text in a
pull request cannot steer the agent into writing to GitHub. A request the user types in a later
turn, after seeing `notes.md`, is the same confirmation, and PR text cannot type it.

## Decision

When the user asks in a later message to post the notes they have seen, the skill lets the agent
run exactly two commands:

- `gh release view <tag> --json tagName,isDraft,body`, to confirm the release is a draft and show
  what its notes will replace. The agent stops if it is not a draft.
- `gh release edit <tag> --notes-file <dir>/notes.md`, with the file `render` wrote, unchanged
  since the user saw it.

The agent never passes `--draft=false` and never edits a published release. The user publishes
the draft on GitHub. Every other `gh` use stays forbidden, and `publish` stays the default.

## Alternatives considered

- **`publish --yes`.** One command, still checked by `publish`. But a steered agent could pass the
  flag without the user asking, so the confirmation would mean nothing.
- **Keep the agent off GitHub.** The releaser copies a command into another terminal, or grants
  `gh` ad hoc, which allows more than these two commands.

## Consequences

- The agent can write to GitHub, bounded by the skill's instructions rather than a mechanism. The
  record is [the agent posts approved release notes](../security/2026-11-07-the-agent-posts-approved-release-notes.md).
- `check` and `render` still govern what lands, since the agent posts their output unchanged.
- Publishing a release still takes a person on GitHub.

## Revisit when

An agent is expected to post notes with no person present, or the harness can bind an agent to an
allow-list of commands, which would turn these instructions into a mechanism.
