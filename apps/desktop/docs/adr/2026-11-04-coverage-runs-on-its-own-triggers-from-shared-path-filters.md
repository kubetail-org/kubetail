---
title: Coverage runs on its own triggers, reading the path filters ci.yml reads
date: 2026-11-04
scope: repo
status: Accepted
---

# Coverage runs on its own triggers, reading the path filters ci.yml reads

## Context

[A wip branch's tip gets a coverage report](2026-11-04-a-wip-branchs-tip-gets-a-coverage-report.md)
made `coverage.yml` a reusable workflow that `ci.yml` calls, so both read `ci.yml`'s one path
filter. Drift is the risk it guards against: a path that runs a language's tests but not its
coverage leaves the changed lines unmeasured, and the gate passes them as "0 of 0 changed lines
covered".

Being called had a cost. `coverage.yml` started two ways, as a call with inputs or on a push to
`wip/**` with none, so every job carried `inputs.<lang> || github.event_name == 'push'`, the cache
mode came in as an input, and `ci.yml`'s `env` had to be copied by hand (#706).

## Decision

- The language filters live in `.github/ci-config/path-filters.yml`, which `dorny/paths-filter` reads after
  a sparse checkout of `.github`. `ci.yml`'s and `coverage.yml`'s `Changes` jobs both point at it,
  and neither inlines a filter. The file is in its own `shared` list, so changing it runs every
  language's checks and coverage.
- `coverage.yml` runs on its own triggers: `pull_request`, `push` to `main` and `wip/**`, and
  `workflow_dispatch` with `ci.yml`'s `cache-mode` choice. It computes `CACHE_MODE` as `ci.yml`
  does, has its own concurrency group, and its own `Caches · Prune` after the Go and Rust jobs.
- Its jobs are named as `codeql.yml`'s are: `Coverage · Go (linux)`, `Coverage · Gate` and so on.

This replaces the earlier ADR's "`ci.yml` calls it as its `coverage` job" and its check names.
Its `wip/**` trigger and its `-base` handling in `tools/coveragegate` stand.

## Alternatives considered

- **Keep the call.** It works, at the price of the dual-start conditions above in every job.
- **Copy the filters into `coverage.yml`.** Nothing would keep the copies equal, which is the
  drift the call existed to prevent.

## Consequences

- Both workflows restore and save their own caches on `main`, and each prunes after its own jobs.
  The `prune-caches` action keeps the newest copy of each family, so two prunes on one ref agree.
- `codeql.yml` keeps its own filters: they follow what CodeQL extracts.
- A filter that `ci.yml` alone uses (`shell`, `yaml`) lives in the same file, since
  `dorny/paths-filter` takes a file or an inline list, not both.
