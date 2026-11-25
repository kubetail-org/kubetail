---
title: Record architecture decisions as ADRs
date: 2026-08-09
scope: repo
status: Accepted
---

# Record architecture decisions as ADRs

## Context

The three `CLAUDE.md` files (root/frontend, `src-tauri/`, `sidecar/`) had grown to roughly 25,000
words between them, `sidecar/CLAUDE.md` alone accounting for about 17,700. They had drifted into
doing two jobs at once: stating the current shape of the system, and arguing for it. The rationale
is what bloated them — why each cluster watch is its own stream, why the socket carries two
protocols, why the webview may not touch the network. That reasoning is read once and then costs
context on every unrelated task, and because several of these designs are contracts *between*
subsystems, the same argument appeared in more than one file, worded differently each time.

## Decision

Rationale moves to `docs/adr/`, one flat date-prefixed sequence for the whole repo. The
`CLAUDE.md` files keep only present-tense operational content — invariants, conventions, commands,
file locations, traps — and link out to an ADR wherever a reader might ask "why".

Filenames are `YYYY-MM-DD-short-slug.md`, dated by when the ADR was written. Scope (`frontend`,
`host`, `sidecar`, `cross-cutting`, …) is a frontmatter field, and `docs/adr/README.md` carries the
index. `docs/adr/` is exempt from the repo's "describe the present, not the past" rule; everywhere
else that rule stands.

## Alternatives considered

**Per-subsystem ADR directories** (`sidecar/docs/adr/`, `src-tauri/docs/adr/`, …). Rejected: the
decisions with the most rationale to extract are precisely the cross-subsystem ones — the h2c
multiplex, the delta-watch protocol, GraphQL-over-IPC, `host.json` as settings source of truth.
Each would have had to pick an arbitrary owning subsystem and then be cross-referenced or
duplicated from the other two, which is the failure the ADRs are meant to fix. A flat sequence also
gives stable links that survive a decision's scope changing.

**Sequential numbering** (`0001-`, `0002-`). Rejected in favour of dates: numbers need a registry to
avoid collisions across branches, and they carry no information. A date sorts chronologically for
free and immediately tells a reader how old the reasoning is — which for a young codebase is the
thing you most want to know before trusting an ADR.

**Leaving the rationale inline and just trimming.** Rejected: trimming is not durable. Without a
separate home, the next architectural change re-inflates the same file, because there is nowhere
else for the argument to go.

## Consequences

The `CLAUDE.md` files get short enough to read in full on every task, which is the point. The cost
is a second place to keep honest: an ADR that gets superseded while a `CLAUDE.md` still links to it
is worse than no ADR, so flipping the status and repointing the links is part of the same commit.

Retroactive ADRs — the first batch, documenting decisions already made — are reconstructions. Where
the original reasoning isn't recoverable from the code or the docs, they say so rather than invent
it.

## Revisit when

The repo splits into separately versioned packages with genuinely independent lifecycles. At that
point per-package ADR directories stop being arbitrary.
