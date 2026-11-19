---
title: Require CI to merge to main
date: 2026-11-19
scope: repo
status: Accepted
---

# Require CI to merge to main

## Context

No CI job was a required check on `main`. A red pull request could merge, and Dependabot's ADR (→ [ADR](2026-11-09-dependabot-moves-dependencies-and-a-person-merges-every-update.md)) rejected auto-merge because of it, naming required checks as its "Revisit when". Splitting CI into one workflow per area (→ [ADR](2026-11-19-run-ci-as-one-workflow-per-area.md)) gave CI one job that always reports and needs every area: `CI · Passed`.

## Decision

The `main` ruleset requires two checks: `CI · Passed`, the last job of `ci.yml`, and `Coverage · Passed`, the last job of `coverage.yml` (→ [ADR: measure and gate coverage per area](2026-11-19-measure-and-gate-coverage-per-area.md)). Each needs every area its workflow calls. A person adds them in the repository settings; nothing in the repo can.

This replaces the first bullet of the Dependabot ADR's Context: CI's jobs are required checks. Its decision stands: a person still merges every update.

## Alternatives considered

- **Require each job by name.** Names repeat across workflows (`Go · Lint`, `Plan`), and a ruleset matches the job name alone, so `Go · Lint` would match either area's. A job skipped by its Plan also reports nothing a ruleset can wait on.
- **Require one check per area.** Ten checks, and a new area gates nothing until a person adds its check to the ruleset.
- **Auto-merge Dependabot's patch updates, now that CI gates them.** The cooldown narrows the window for a compromised patch release without closing it, and a person reading the changelog is the last check before it lands.

## Consequences

- A pull request cannot merge while an area it touched fails, or while coverage drops on a changed line.
- A runner outage blocks every pull request until a rerun passes.
- A new area is gated once `ci.yml` or `coverage.yml` calls it, which `make lint-yaml` enforces.

## Revisit when

Dependabot's updates become routine enough that reviewing each one costs more than it catches.
