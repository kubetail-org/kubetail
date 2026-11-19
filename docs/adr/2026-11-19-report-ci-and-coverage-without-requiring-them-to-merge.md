---
title: Report CI and coverage without requiring them to merge
date: 2026-11-19
scope: repo
status: Accepted
---

# Report CI and coverage without requiring them to merge

## Context

"Require CI to merge to main" (→ [ADR](2026-11-19-require-ci-to-merge-to-main.md)) made `CI · Passed` and `Coverage · Passed`, the last jobs of `ci.yml` and `coverage.yml`, the `main` ruleset's required checks. The ruleset was never changed to require them: its only required check is `DCO`. The two jobs gated nothing, and `tools/cipassed` checked that they existed. CI and coverage are not meant to block a merge: a runner outage or a flaky gate should not hold up a pull request a reviewer has read.

## Decision

CI and coverage report on every pull request and do not block a merge. The `main` ruleset requires `DCO` alone. `ci.yml` and `coverage.yml` have no `· Passed` job; each still calls every area file beside it, since an area file runs on a pull request only when its caller calls it. `tools/cicallers`, run by `make lint-repo-yaml`, fails a caller that misses one.

This supersedes "Require CI to merge to main". The first bullet of the Dependabot ADR's Context holds again: CI's jobs are not required checks (→ [ADR](2026-11-09-dependabot-moves-dependencies-and-a-person-merges-every-update.md)). Its decision stands: a person merges every update, and auto-merge stays rejected, now with no required check to lean on.

## Alternatives considered

- **Add `CI · Passed` and `Coverage · Passed` to the ruleset.** It makes the jobs do what they were written for, but a runner outage or a flaky coverage gate would then block every pull request until a rerun passes.
- **Keep the `· Passed` jobs unrequired, as a summary.** Each area already reports its own jobs, so a summary adds a check to read and a rule in `tools/cipassed` to maintain, and suggests a gate that does not exist.

## Consequences

- A pull request can merge while CI or coverage is red. The reviewer reads the checks before merging.
- A runner outage or a flaky gate does not block a merge.
- `make lint-repo-yaml` still fails when a caller misses an area file, so a new `ci-*.yml` or `coverage-*.yml` runs on pull requests once it exists.
- A job name no longer has to be unique to work as a required check, until one is required.

## Revisit when

A red pull request merges and breaks `main` often enough that a required check costs less than the reruns it would force.
