---
title: Run coverage and CodeQL on a push to a wip branch
date: 2026-11-19
scope: repo
status: Accepted
---

# Run coverage and CodeQL on a push to a wip branch

## Context

A `wip/<topic>` branch takes pull requests that are reviewed and merged into it before it lands on `main` (#1501). Two workflows compare a pull request with its base branch, and both get the base from push runs that only `main` had.

[The coverage gate](2026-11-09-gate-prs-on-changed-lines-with-codecovs-merged-result.md) compares a pull request with its base commit's Codecov report, and only a push run of `coverage.yml` uploads one. It ran on `main` alone, so a `wip/*` tip never had a report, and every pull request into it failed the gate with "Codecov has no report for base commit".

`codeql.yml` ran only for pull requests into `main` and pushes to it. A pull request into `wip/*` got no analysis, and Reuse · Base analysis, which uploads the base branch's analysis for each language a pull request skips, had no `wip/*` analysis to reuse.

## Decision

- `coverage.yml` runs on a push to `wip/**` as well as `main`. Each such run has a concurrency group of its own, so every commit on the branch gets a report. A push makes each area's Plan (`.github/actions/plan`) run every job, and no coverage job saves a Go or Rust cache, so the run reads `main`'s caches and saves none. `tools/coveragegate` takes a push's base as the head's parent, the branch's previous tip.
- `codeql.yml` runs on a pull request into `wip/**` and a push to it. Pushes share a concurrency group per branch (`github.ref`), so each branch keeps its own baseline. Only `main` saves a cache or prunes one.
- `ci.yml` keeps its triggers: the pull request a push to `wip/*` merges already ran its checks.

## Alternatives considered

- **Add `wip/**` to `ci.yml`'s push trigger too.** It repeats the merged pull request's checks, including the platform matrices, for no new signal.
- **Compare a pull request into `wip/*` with the nearest ancestor that has a report.** That ancestor is usually on `main`, so the patch would include `wip/*` work a reviewer has already judged.
- **Skip CodeQL on a pull request into `wip/*` and analyze the branch when it lands on `main`.** A finding would then reach a reviewer only after the code it flags had been merged once.

## Consequences

- Each merge into a `wip/*` branch runs every coverage job and every CodeQL language.
- A `wip/*` branch runs the workflows it holds, so a branch cut before this change gets the triggers only after it merges `main`.
- A pull request opened on a `wip/*` tip whose push run is still uploading fails the coverage gate with "no report for base commit", and a pull request into a new `wip/*` branch fails Reuse · Base analysis until the branch's first CodeQL push run finishes. Re-run the job once it has.
- `coverage-web.yml` restores the pnpm store through `actions/setup-node`, which saves it under the branch when the lockfile changed, as it does on a pull request.
