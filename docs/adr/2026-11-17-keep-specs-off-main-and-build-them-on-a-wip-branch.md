---
title: Keep specs off main and build them on a wip branch
date: 2026-11-17
scope: repo
status: Accepted
---

# Keep specs off main and build them on a wip branch

## Context

Large changes were planned in their issue or in the PR that built them. There was no shared place to review a plan before building it, and an agent had no instructions for writing one or building from one. The `needs-spec` and `spec` labels existed with no process behind them.

A spec says what we are about to build. It changes as the work moves and is worth nothing once the work lands. It is most useful beside the code it describes: a task goes to a person or an agent with the spec, the `CLAUDE.md` guides and the current code, in one checkout.

The kstack repository had settled the same question with a process of its own. In November 2026 it was copied here and adapted.

## Decision

A spec is proposed as a PR into `main`, titled `📜 Spec: …`, labelled `spec`, and opened from `.github/PULL_REQUEST_TEMPLATE/spec.md`. Once accepted it merges into a `wip/<topic>` branch of this repository instead. Each implementation PR targets that branch, and a PR from it into `main` lands the whole change: it folds the spec into the guides and ADRs, deletes it, and closes the issue. `main` never holds a spec.

A sequence of steps shares one `wip/<topic>` and lands once its last step is built. Its steps are numbered in build order (`3`), or by wave and letter (`3b`) when steps of one wave can be built at the same time. `docs/specs/README.md` is the procedure, including how an agent works a step.

A push to `wip/**` runs `coverage.yml`, so a PR into the branch has a base report for the coverage gate. `make lint-md` skips spec files, which may link to files their work has yet to add.

## Alternatives considered

- **Specs on `main` as soon as they are accepted.** The directory becomes the index, but `main` carries plans that may wait months or never be built, and the guides' rule that docs describe the present no longer holds for one directory.
- **A separate repository.** Keeps `main` clean, but landing becomes two changes in two repos, nothing ensures the second happens, and links between spec and code break as files move.
- **The issue body, or a GitHub Discussion.** No line-level review and no diffable history, and building means copying the plan into the repo, after which the two copies drift.
- **The implementation on the spec's PR.** One branch, but no state between proposed and landed, and every task piles onto one PR too large to review.

## Consequences

`main` holds only what is true now. A spec gets line-level review and a history, implementation PRs stay small, and the work lands on `main` at once.

A `wip/<topic>` branch is long-lived and drifts from `main` unless it is kept current, and its landing PR is large, though each part of it was reviewed. How it keeps current and how it lands are left to the maintainers for each piece of work.

A spec cannot be found in a checkout of `main`. The open `spec` PRs and the open PRs from `wip/*` are the index, and anything that depends on a spec links its PR.

Each push to a `wip/*` branch runs every coverage job. A `wip/*` branch runs the `coverage.yml` it holds, so a branch made before the trigger existed gets it only by merging `main`.

## Revisit when

A sequence grows so long that landing it at once is harder than landing it step by step.
