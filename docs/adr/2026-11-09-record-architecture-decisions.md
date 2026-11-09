---
title: Record architecture decisions as ADRs
date: 2026-11-09
scope: repo
status: Accepted
---

# Record architecture decisions as ADRs

## Context

Until November 2026 the repo had one root `CLAUDE.md`, copied into `AGENTS.md` and `GEMINI.md`. #1216 (for #1201) split it into a guide per area and replaced the copies with `AGENTS.md` symlinks. The new guides state decisions as facts and carry almost no rationale, which is what an agent needs on every task.

That left the reasoning with no home. Some of it sat in code comments, several written to a reviewer ("See PR description for the rationale") or describing code that had since changed. Some sat in commit bodies and PR descriptions, which no guide can link to and no agent reads. The reasoning behind the oldest decisions was not written down at all. As a stopgap, the root guide sent rationale to "the commit body or the PR's Summary".

[kstackhq/kstack](https://github.com/kstackhq/kstack/tree/main/docs/adr) had solved the same problem with a flat `docs/adr/` of dated ADRs, linked from its `CLAUDE.md` files.

## Decision

Rationale lives in `docs/adr/`, one flat, date-prefixed sequence for the whole repo. The `CLAUDE.md` files keep only what is true now (invariants, conventions, commands, file locations, traps) and link an ADR with `→ [ADR: <title>](…)` wherever a reader might ask "why". Code comments state the current design and leave the argument for it to the ADR.

Filenames are `YYYY-MM-DD-short-slug.md`, dated by the day the ADR was written. Scope is a frontmatter field, and `docs/adr/README.md` carries the conventions and the index. A changed decision gets a new ADR; the old one's status flips to `Superseded by …`, and every guide's link moves in the same commit. `docs/adr/` is the one place exempt from the root guide's "describe the present, not the past" rule.

The conventions are ported from kstack as it runs them, with this repo's scopes.

## Alternatives considered

**Rationale in commit bodies and PR descriptions.** The stopgap. It records the reasoning, but by change rather than by decision: the argument for one design ends up spread over several PRs, and nothing in the tree points at any of them.

**Rationale in the `CLAUDE.md` files.** Rejected because the guides are read on every task. Reasoning that is read once then costs context on every unrelated task, and a decision that crosses areas gets argued in more than one guide, worded differently each time.

**Per-area ADR directories** (`modules/docs/adr/`, `crates/docs/adr/`). Rejected: the decisions with the most reasoning behind them cross areas. The route from the Dashboard through the kube-apiserver to the Cluster API touches `modules/`, `crates/` and the Helm chart, and would have to pick an owner arbitrarily. A flat sequence keeps links stable when a decision's scope changes.

**Sequential numbers** (`0001-`, `0002-`). Rejected: numbers collide across open branches and say nothing. A date sorts for free and tells a reader how old the reasoning is.

## Consequences

The guides stay short enough to read in full on every task. The cost is a second place to keep honest. An ADR superseded while a guide still links it is worse than no ADR, so flipping the status and repointing the links happen in one commit. Nothing checks Markdown links yet (#1210), so a broken link fails silently until a reader follows it.

The first ADRs, written on this date, are reconstructions of decisions made earlier. Where the reasoning could not be recovered from the code, the git history or the PRs, they say so rather than invent it.

## Revisit when

The components move to separate repositories with their own lifecycles. Per-repo ADR directories then stop being arbitrary.
