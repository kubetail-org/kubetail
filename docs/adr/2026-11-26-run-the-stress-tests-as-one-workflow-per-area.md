---
title: Run the stress tests as one workflow per area
date: 2026-11-26
scope: repo
status: Accepted
---

# Run the stress tests as one workflow per area

## Context

CI and coverage run as one workflow per area, called from `ci.yml` and `coverage.yml` (→ [ADR](2026-11-19-run-ci-as-one-workflow-per-area.md)). The nightly stress run did not. `stress.yml` ran `go test` over the whole Go workspace in one job per runner, `-race` setting and `-cpu` value, on `ci-cli.yml`'s matrix, and `stress-desktop.yml` ran the desktop sidecar on a schedule of its own, an hour later, so the two did not queue for the macOS and Windows runners at once.

So every workspace failure carried the same labels, `testing` and `lang/go`, and shared one outside-a-test issue across `cli`, `dashboard`, `cluster/api` and `libs/go-common`. One area could not be rerun alone. The Cluster API, which ships for linux alone, ran on macOS and Windows. No job restored a test cache, so each compiled the workspace from nothing. Nothing checked that a new stress file was ever scheduled.

## Decision

`stress.yml` is a caller, as `ci.yml` is: one schedule (05:00 UTC), the `count` and `cpu` dispatch inputs, and one job per area, each calling a reusable workflow:

- `stress-cli.yml`: `./cli/... ./dashboard/...`, on the CLI's platforms.
- `stress-libs.yml`: `./libs/go-common/...`, on the same platforms.
- `stress-cluster.yml`: `./cluster/api/...`, on linux amd64 and arm64, as `ci-cluster.yml` tests it.
- `stress-desktop.yml`: the sidecar, on the desktop's runners.

Each also runs alone by `workflow_dispatch`. Each restores its area's test cache from its `ci-*.yml` (`cli-test-<os>-<arch>`, `libs-test-…`, `cluster-api-test-…`, `desktop-sidecar-test-…`) and saves none, since its flags differ from a PR's.

Each area calls `report-stress.yml` with a `stress-id-prefix` of its own (`cli-`, `libs-`, `cluster-`, `desktop-`), its labels (`testing`, `lang/go`, and `cli`, `cluster` or `desktop`; `libs` has no area label) and its outside-a-test issue title. In one caller run every area's jobs and artifacts share the run, so the report reads only its own: it rebuilds each Test job's `STRESS_ID` from its name, `<caller job> / Test (<runner>, <race>, cpu <n>)` when called and `Test (…)` when run alone, keeps those that start with its prefix, and downloads only `stress-<prefix>*` artifacts. A caller job's name is its area's prefix without the dash.

`tools/cicallers` checks that `stress.yml` calls every `stress-*.yml` beside it, as it checks `ci.yml` and `coverage.yml`. The report is named `report-stress.yml` so that `stress-*.yml` means an area.

## Alternatives considered

- **Keep one workspace job and split only the report**, by package path. Labels would follow the area, but an area still could not run alone, the Cluster API would still run on macOS and Windows, and no area cache would fit the job.
- **Keep a schedule per area file, staggered by hand.** It is what `stress-desktop.yml` did. Each new area would need a slot and nothing would check it got one; jobs queue for a runner anyway, and `timeout-minutes` counts from when a job starts, so staggering buys nothing.

## Consequences

- A failing test opens or comments on an issue labelled with its area, and no other area's report files it. Each area has its own outside-a-test issue.
- `gh workflow run stress-cluster.yml --ref <branch>` reruns one area.
- The run has about three times the workspace's jobs before the split, each with its own setup, which the area cache keeps short. The macOS and Windows legs queue longer, which a nightly run can afford.
- The Cluster API is no longer stress-tested on macOS or Windows, where it never ships.
- A caller job's name and its area's `stress-id-prefix` must agree, and no prefix may start another's, or a report files another area's jobs or none.
- `make lint-repo-yaml` fails when `stress.yml` misses a `stress-*.yml`. A workflow named `stress-*.yml` that is not an area would fail it too.

## Revisit when

Rust's tests (`cluster/agent`, `apps/desktop/src-tauri`) are stress-tested, or the stress matrices come to read `runners.json`.
