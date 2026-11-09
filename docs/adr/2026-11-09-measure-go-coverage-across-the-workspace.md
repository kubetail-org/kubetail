---
title: Measure Go coverage across the workspace
date: 2026-11-09
scope: repo
status: Accepted
---

# Measure Go coverage across the workspace

## Context

`modules/` is four Go modules under one `go.work`. `shared` holds the connection manager, the log fetchers and the GraphQL helpers, and much of it is exercised by the Dashboard's and the Cluster API's tests rather than its own. Go's default coverage counts only the package a test binary belongs to, so measured that way most of `shared` would look untested and fail the coverage gate on lines that are tested.

## Decision

The `Coverage · Go` job runs one `go test` from `modules/` over `github.com/kubetail-org/kubetail/modules/...` with `-coverpkg` set to the same pattern, so every test binary counts every package in the workspace. `tools/coverignore -dir ../modules` merges the blocks the binaries share and maps import paths to files through `go.work`. One profile per OS goes to Codecov. `scripts/cover-changed.sh` measures the same way.

## Alternatives considered

- **Measure each module alone, `-coverpkg=./...` per module, and let Codecov merge the four uploads.** Each module's tests would still count only that module, so a `shared` line covered by the Dashboard's tests stays uncovered.
- **Measure each module with `-coverpkg` naming its sibling modules.** Four runs that each instrument the whole workspace, for the result one run gives.
- **Leave `shared` to its own tests.** It would hold every PR that touches `shared` to tests duplicating the ones its callers already have.

## Consequences

- A `shared` line any module's tests reach is covered.
- Every package is instrumented in every test binary, which makes the run slower than the plain test job. It is measured once per OS, not per architecture.
- The local `make cover-changed` runs only the changed packages' tests by default, so it can list a `shared` line that another module's tests reach; `FULL=1` runs them all, as CI does.
- A new module joins the measurement by being added to `go.work`.
