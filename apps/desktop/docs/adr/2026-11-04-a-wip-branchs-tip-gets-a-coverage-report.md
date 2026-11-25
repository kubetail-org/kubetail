---
title: A wip branch's tip gets a coverage report, and a missing base fails the gate at once
date: 2026-11-04
scope: repo
status: Accepted
amended_by:
  - [Coverage runs on its own triggers, reading the path filters ci.yml reads](2026-11-04-coverage-runs-on-its-own-triggers-from-shared-path-filters.md)
  - [The coverage gate diffs the commits itself](2026-11-05-the-coverage-gate-diffs-the-commits-itself.md)
---

# A wip branch's tip gets a coverage report, and a missing base fails the gate at once

## Context

[The coverage gate runs in CI](2026-10-30-the-coverage-gate-runs-in-ci-on-codecovs-merged-result.md)
compares a pull request with its base's Codecov report. Only CI's push runs upload a base's
report, and CI ran on push for `main` alone. A `wip/<topic>` branch moves forward through merge
commits no run uploaded for, so Codecov could never compare a PR into it (#699). `/compare/`
answered 404, and the gate retried it as a Codecov delay for 15 minutes before saying so.

## Decision

- The coverage jobs and the gate move to a reusable workflow, `.github/workflows/coverage.yml`.
  `ci.yml` calls it as its `coverage` job, passing the languages its path filter selected and its
  cache mode. A push to `wip/**` starts it alone and runs every language. Its checks read
  `coverage / Go (linux)`, `coverage / Gate` and so on.
- `tools/coveragegate` takes the PR's base commit (`-base`). When `/compare/` answers 404 for a
  complete head, it reads `/commits/<base>/`; a 404 there is a `noBaseError`, which fails the gate
  at once and names the base. A push's base is the head's parent.

This replaces the earlier ADR's verdict "on pull requests and on pushes to `main`" with one on
pushes to `wip/*` too, and its "Codecov down or slow fails the job after 15 minutes" for a base
with no report.

## Alternatives considered

- **Add `wip/**` to `ci.yml`'s push trigger and run everything.** It repeats the merged PR's
  checks, including the three-OS test matrices, for no new signal.
- **Add `wip/**` to `ci.yml`'s push trigger and skip the checks there.** Every check job, or every
  coverage job, carries a condition for that one push, and a new job must remember it.
- **Compare with the base's nearest ancestor that has a report.** The patch would then include
  earlier `wip/*` work the reviewer has already judged.

## Consequences

- Each merge into a `wip/*` branch runs the seven coverage jobs. They read `main`'s caches and save
  none.
- A `wip/*` branch runs the `coverage.yml` it holds, so an existing branch has the trigger only
  after it merges `main`.
- `coverage.yml` sees none of `ci.yml`'s `env`: it sets `CARGO_PROFILE_DEV_DEBUG` itself and takes
  the cache mode as an input.
- A PR opened on a `wip/*` tip whose push run is still uploading fails with `noBaseError`; re-run
  the gate once that run's coverage jobs finish.
