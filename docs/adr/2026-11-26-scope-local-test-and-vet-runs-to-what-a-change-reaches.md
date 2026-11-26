---
title: Scope local test and vet runs to what a change reaches
date: 2026-11-26
scope: repo
status: Accepted
---

# Scope local test and vet runs to what a change reaches

## Context

"Run each area's own checks locally, with CI as the backstop" removed `test-changed` and `vet-go-changed` (#1541) for two reasons. Their output hid what they checked behind one verdict per area, and their mapping from paths to checks was a second copy of the job selection in `.github/ci-config/path-filters.yml`. It left `make <verb>-<area>` as the only way to run an area's tests or vet it, with the guides asking for hand-scoped commands while working.

Some full targets are too slow to run during development. `make test-desktop` rebuilds the whole sidecar under `-race` before it reaches `cargo test`, and grows the Go build cache by gigabytes. `vet-desktop` vetted the sidecar for four platforms, each a recompile of every dependency: #1492 measured 44s and 3G of memory for a cold cross-platform vet alone. A hand-scoped `go test ./internal/<pkg>/...` misses the packages that import the changed one, and an agent given that choice tends to run the full target instead.

`scripts/changed.sh` already lists what a branch changed, for `cover-<area>-changed`.

## Decision

Each area with tests has `make test-<area>-changed`, and each area with vet passes `make vet-<area>-changed`: `cli`, `libs`, `cluster-api`, `cluster-agent`, `web` and `desktop`, plus `make test-repo-changed`. They wrap `scripts/test-changed.sh` and `scripts/vet-changed.sh`, which read the change from `scripts/changed.sh`:

- A Go area runs the changed packages and every package of the area whose build or tests import one, from any module of the workspace (`go_scope`). A `go.mod` or `go.sum` change runs its module whole. Tests keep `-race`, with `GORACE=atexit_sleep_ms=0`.
- Vetting is `go vet` and the `go fix` check for this platform and the default build tags, `tsc -b` on a TypeScript change, and clippy when a crate's inputs changed. Staticcheck, the other platforms and the other build tags stay in `vet-<area>` and CI.
- A web app runs the tests vitest relates to its changed files; a crate runs its whole suite when its inputs changed.
- `test-repo-changed` runs the bats files of the changed scripts, and of the scripts that source a changed helper (`bats_scope`).

The targets answer both objections. Each covers one area, prints each command it runs (`→ …`) and each suite or pass it skips, and gives no verdict of its own. It scopes by changed files alone: it picks no CI jobs and reads nothing from `path-filters.yml`.

These targets trade correctness for speed. `make <verb>-<area>` remains what runs before a PR, and CI remains the backstop.

`make vet-desktop` vets the sidecar for this platform alone, untagged and under each tag. CI's `desktop-sidecar-vet` runs the other platforms' passes with its own commands.

This amends "Run each area's own checks locally, with CI as the backstop": the removal of `test-changed` and `vet-go-changed` is reversed in this form. The rest of it stands.

## Alternatives considered

- **One target over every area that changed.** It would bring back the aggregate verdict, and deciding which areas a change touches is the CI mapping the earlier ADR removed.
- **Hand-scoped commands in the guides.** That was the state before: it misses the importers of a changed package, and contributors and agents fell back to the full target.
- **Drop `-race` from the scoped tests.** It would be faster again, but a race found only in CI costs a round trip, and the changed packages are the ones most likely to hold one.
- **Scope from `.github/ci-config/path-filters.yml`.** One source for both, but the filters select CI jobs, not packages, and it would make these targets the second copy of CI's job selection that the earlier ADR objected to.

## Consequences

- During development an area's tests and vet cost what the change reaches, not the whole area.
- A guide's "while working" list names `make vet-<area>-changed test-<area>-changed` first.
- `go_scope` runs `go list -test` over the workspace on every call, under a second on this repo.
- A test reached only at run time, through a dynamic import or a file read by path from another package, is missed. So is a Windows-only vet finding.
- A new area or a new kind of input adds a case to both scripts and to their bats files.

## Revisit when

The scoped runs miss enough failures that CI round trips cost more than running the full target would, or `go list` over the workspace gets slow enough to matter.
