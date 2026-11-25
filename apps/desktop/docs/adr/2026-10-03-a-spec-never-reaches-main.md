---
title: A spec never reaches main
date: 2026-10-03
scope: repo
status: Accepted
---

# A spec never reaches main

## Context

A spec says what we are about to build. It is updated as the work moves and deleted when the work
lands, so it never outlives its code. Specs were committed to `docs/specs/` on `main` as soon as
they were settled, which put plans on `main` months before any code, and left the directory
listing as the only index of what was proposed.

A spec is most useful next to the code it describes: tasks are handed to a person or an agent with
the spec, the `CLAUDE.md` files and the current code, in one checkout.

## Decision

A spec is proposed as a draft pull request into `main`, labelled `spec`, and reviewed there. Once
accepted it is merged into a `wip/<topic>` branch of this repository instead, each implementation
PR targets that branch, and a PR from it into `main` lands the whole change, folding the spec into
`CLAUDE.md` and an ADR and deleting it, so `main` never holds it. A sequence shares one
`wip/<topic>` and lands once its last step is built; its steps are numbered in build order (`3`),
or by wave and letter (`3B`) when steps in one wave can be built at the same time.
`docs/specs/README.md` is the procedure.

The agent-security sequence was on `main` before this, and stays until its steps land.

## Alternatives considered

- **Specs on `main` (as before).** Keeps the directory as the index, but `main` carries plans that
  may wait months or never be built.
- **A separate repository.** Keeps `main` clean, but landing becomes two changes in two repos,
  nothing ensures the second happens, and links between the spec and the code break as files move.
- **GitHub Discussions.** Good for threaded debate, but a post has no line-level review and no
  diffable history, and starting work means copying it into the repo, after which the two copies
  drift.
- **The implementation on the spec's PR.** Keeps one branch, but there is no state between
  proposed and landed, and every task of the work piles onto one PR too large to review.
- **The issue body.** Same limits as a Discussion, and an issue already holds the question of
  whether to build at all.

## Consequences

`main` holds only what is true now, plus the agent-security steps. A spec gets line-level review
and full history, implementation PRs stay small, and the work lands on `main` at once.

A `wip/<topic>` branch is long-lived, so it drifts from `main` unless it is kept current, and
the landing PR is large, though each part of it was reviewed. How it keeps current and how it
lands — squash, rebase or merge commit — is left to the maintainers for each piece of work. A spec
is not on `main`, so it cannot be found by searching a checkout of it: the open `spec` PRs and
`wip/*` PRs are the index, and an issue or `TODO.md` line that depends on a spec links its PR.

## Revisit when

A sequence grows so long that landing it at once becomes harder than landing it step by step.
