---
title: Run each area's own checks locally, with CI as the backstop
date: 2026-11-19
scope: repo
status: Accepted
---

# Run each area's own checks locally, with CI as the backstop

## Context

`make check-changed` worked out every local check a branch needed, across every area. It spanned five scripts of about 400 lines (`scripts/changed.sh`, `check-changed.sh`, `test-changed.sh`, `vet-go-changed.sh`, `cover-changed.sh`) that mapped changed paths to make targets, ran them side by side and printed `ok` or `FAILED` per job.

That mapping re-implemented in bash what `.github/ci-config/path-filters.yml` already decides for CI, so a new area or linter went into both, and the two drifted. Its output hid what it had checked: after the move to one folder per component, the UI half of `cover-changed` matched no line, because the coverage report's `projectRoot` was off by one directory, and `check-changed` still reported `ok` until #1540. And the guides never had to say what to run in an area, since one meta-target stood in for all of them.

With one folder per component, each area's checks are a handful of commands in that folder.

## Decision

Each area's `CLAUDE.md` and `README.md` have the same `## Checks` section: what to run while working, scoped to what was touched, and what to run before a PR. The four Go modules share theirs in the root guide's *Go modules › Checks*. The root guide and `README.md` hold a table of changes that reach other areas, and what to run for each.

Coverage stays a script, because it is the one check whose result is not a test passing: `scripts/cover-changed.sh <area>` measures one CI coverage area, as its `coverage-*.yml` does, and lists the changed lines no test reached. Each area has a `make cover-<area>-changed` target: `cli` (with `dashboard`), `libs`, `cluster-api`, `cluster-agent` and `web`. `scripts/cover-changed.bats` runs each area's real suite and checks that it lists a known uncovered line, so a broken path mapping fails a test.

CI checks every area on every platform and is the backstop. `check-changed`, `test-changed` and `vet-go-changed` are removed.

## Alternatives considered

- **Keep `check-changed` and fix it.** The `projectRoot` bug was fixed in #1540, but the script would stay a second copy of CI's job selection, and its one-line verdict per area would keep hiding what ran.
- **Generate the local checks from `path-filters.yml`.** One source for both, but the filters select CI jobs, not commands, and a generator would be one more layer between a contributor and the command they need.
- **Keep one `cover-changed` over every area.** One command to remember, but a run judged areas the change did not touch, and the Rust suite was slow enough to sit behind a flag that hid it.

## Consequences

- A contributor or an agent runs the commands an area's guide names, and sees each one's output.
- Nothing checks that a branch ran the right checks. CI does, after the push, so a missed local check costs a CI round trip.
- A guide's `## Checks` and its README's say the same; a change to one changes both.
- A new coverage area adds a case to `scripts/cover-changed.sh`, a `cover-<area>-changed` target and a case in `scripts/cover-changed.bats`.
- `scripts/cover-changed.bats` runs real Go, Vitest and Rust suites, and skips an area whose toolchain is missing.

## Revisit when

The areas' checks stop fitting in a short list each, or CI round trips get slow enough that a missed local check costs more than a meta-target would.
