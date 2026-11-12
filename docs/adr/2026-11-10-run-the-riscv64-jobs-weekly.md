---
title: Run the riscv64 jobs weekly
date: 2026-11-10
scope: repo
status: Accepted
---

# Run the riscv64 jobs weekly

## Context

[Run each OS once on a PR and the other platforms on main](2026-11-10-run-each-os-once-on-a-pr-and-the-other-platforms-on-main.md) left four jobs on riscv64 runners in `ci.yml`, on every push to `main`: `Go · Test (linux, riscv64)`, `CLI · Build (linux, riscv64)`, and `Docker · Build` of the cluster-api image's `final` and `final-alpine` targets.

riscv64 is no core platform. No release ships for it; Alpine and Launchpad build the CLI for it from source. The jobs are there to see a break coming.

The `ubuntu-24.04-riscv` runners are few and slow. On the runs of 2026-11-10, a riscv64 job waited 5 to 40 minutes for a runner, then ran 7 to 18 minutes. A push to `main` took about 60 minutes, against about 20 for every other job. #1467 made `main`'s runs queue behind the one in progress, so the riscv64 jobs set how often `main` gets a run.

## Decision

`riscv64.yml` runs the riscv64 jobs weekly and on `workflow_dispatch`. `ci.yml` runs none of them.

- `Go · Test and build CLI` runs the Go tests, then builds the CLI, in one job, so it waits for one runner. Both run with `CGO_ENABLED=0`, as the release builds do, so the build reuses the packages the tests compiled.
- `Docker · Build` builds and starts the cluster-api image in both targets.
- Neither job uses a cache. GitHub evicts a cache unused for 7 days, so a weekly run would rarely find one.
- A failure on `main` opens a `ci/cd` issue, or comments on the one still open, as `audit.yml` does.

`Alpine · Build (riscv64)` stays in `ci.yml`. It cross-compiles on an amd64 runner and starts the CLI under QEMU in about 3 minutes, so every push to `main` still checks that the CLI builds and starts for riscv64.

## Alternatives considered

**Keep the jobs on `main` and make them faster.** Merging the CLI build into the test job and giving each Docker target its own cache would cut the run time, but not the wait for a runner, which is most of the delay.

**Keep the jobs on `main` but let the run finish without them.** A workflow's run is not done until every job is, and the next push to `main` queues behind it.

**Run on `workflow_dispatch` alone.** Nobody would run it, and a break would show up only when someone did.

**Drop riscv64 except for `Alpine · Build`.** The Go tests on riscv64 are the only check of the code on a 64-bit platform besides amd64 and arm64, and they cost little once a week.

## Consequences

A push to `main` runs in about 20 minutes, and a burst of merges gets more of its commits tested.

A test failure specific to riscv64 shows up within a week of the change, not within the hour. A build that breaks still shows up on `main` through `Alpine · Build`.

A scheduled run is on `main`'s head, so a failure names no commit: the issue it opens is a bisect.

## Revisit when

riscv64 becomes a platform a release ships for, or the riscv64 runners stop queueing.
