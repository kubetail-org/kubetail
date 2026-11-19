---
title: Measure and gate coverage per area
date: 2026-11-19
scope: repo
status: Accepted
---

# Measure and gate coverage per area

## Context

`coverage.yml` measured every language in one workflow and one `coverage / Gate` judged Codecov's merged result of the whole commit. Its Go job ran one `go test` over the whole workspace with `-coverpkg` set to the workspace, so any module's tests could cover a `libs/go-common` line (→ [ADR: measure Go coverage across the workspace](2026-11-09-measure-go-coverage-across-the-workspace.md)).

CI had just split into one workflow per area (→ [ADR: run CI as one workflow per area](2026-11-19-run-ci-as-one-workflow-per-area.md)), and more areas are coming: `apps/desktop` will bring a codebase of its own. With one coverage workflow, each new area makes the one gate wait on more uploads, any Go change measures all of Go on three OSes, and a failure reads as one repo-wide verdict rather than the area at fault.

`libs/go-common`'s own tests covered 65.6% of its statements in November 2026. The Cluster API's tests reached 8 more.

## Decision

Each area has a `coverage-<area>.yml` beside its `ci-<area>.yml`, on the same filter keys, so a path that runs the area's tests measures them. `coverage.yml` calls each one, as `ci.yml` calls the `ci-*.yml` files:

- **Measure.** Its jobs upload under the area's flags: `cli-*` from `coverage-cli.yml`, `libs-*` from `coverage-libs.yml` (one per OS, `linux`, `darwin` and `windows`), `cluster-api` and `cluster-agent` from `coverage-cluster.yml`, `web` from `coverage-web.yml`. Go's `-coverpkg` names the area's packages alone: `cli/...` and `dashboard/...`; `libs/go-common/...`; `cluster/api/...`.
- **Own paths.** `.github/codecov.yml` defines one component per area (`cli`, `libs`, `cluster`, `web`) over its paths, and no other area's flags measure them. `libs/go-common` counts only its own tests.
- **Gate.** Its `Gate` job runs `tools/coveragegate -component <area>`. It waits for this run's uploads alone, then judges the component's report at the head against the component's report at the base: patch on the changed lines, project on the component's total.
- **Required check.** `coverage.yml`'s `Coverage · Passed` needs every area and is a required check, apart from `ci.yml`'s `CI · Passed`.
- **Carry forward.** Only the area flags carry forward, so an area a pull request skips keeps its last report and a retired flag drops out of the merge.

`scripts/cover-changed.sh` measures the same three Go areas.

This supersedes "Measure Go coverage across the workspace". It amends "Gate PRs on changed lines with Codecov's merged result": Codecov still stores and merges, and CI still judges, but per area, from one component of the merge, with no `coverage.yml`.

## Alternatives considered

- **Keep one `coverage.yml` and one gate.** Simplest today, and it grows with every area: the gate waits on every upload, and a Go change measures every Go module.
- **Measure and gate inside each `ci-*.yml`.** One fewer workflow per area, but a test failure and a coverage failure then share one required check, and the area's workflow mixes two concerns.
- **Gate each area on the commit's whole merged result.** Each gate would wait on every other area's uploads, which crosses workflows, or judge a report that changes as they land.
- **Count `libs/go-common` in the cli area, measured by its own and the CLI's and Dashboard's tests.** Its consumers' tests would cover its lines, but a library every component imports would be judged by one consumer, and a change to it would run that consumer's coverage on every OS.
- **Keep `-coverpkg` across the workspace in each Go area.** Each upload would then cover other areas' lines, and one area's verdict would depend on whether another area's workflow had uploaded yet.

## Consequences

- A pull request measures and gates only the areas it touches. A new area brings its own workflow, flags and component, and makes no other gate wait.
- A `libs/go-common` line its own tests do not reach is uncovered, whatever its consumers' tests do. A change there needs a test in `libs/go-common`.
- A Dashboard line only the CLI's tests reach is covered: the two share the cli area, as they share a binary.
- The first gates after the switch compare a component's total with a base measured the old way, across the workspace. A drop there can be the measurement, not the change.
- Codecov's components drive the gate, so a path moved out of every component is judged by no gate. A new area's directory goes into a component.
- A gate's flags and component are written in its workflow and in `codecov.yml`; a rename changes both.

## Revisit when

Codecov's API stops filtering a report by component, or one area's tests start owning another area's lines.
