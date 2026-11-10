---
title: Run each OS once on a PR and the other platforms on main
date: 2026-11-10
scope: repo
status: Accepted
---

# Run each OS once on a PR and the other platforms on main

## Context

Until #1278, a PR that touched Go, Rust or TypeScript started 47 platform jobs in `ci.yml`, about 124 runner-minutes at the median. `Go · Build and Test`, `Shell · Test` and `CLI · Build` each ran on seven runners: linux, macOS 15 on Intel, macOS 15 and macOS 26 on arm64, and Windows, each on amd64 and arm64. `Docker · Build` built four components for amd64 and arm64, each in two targets. `Alpine · Build` cross-compiled the CLI for seven architectures and ran it under QEMU. E2E ran on Kubernetes 1.21, 1.25 and the latest. Only the riscv64 jobs ran on `main` alone.

The 200 runs from 2026-05-17 to 2026-11-10 showed no macOS, Windows or arm64 leg failing on a code bug when its Linux amd64 leg passed. Each failure of that kind was the runner or the network: the pnpm installer crashing on `macos-15-intel`, stream errors from `proxy.golang.org`, a Go runtime crash on Windows, a failed checkout. The three E2E versions always failed together. Alpine failed only alongside every other Go build. The failures that mattered came from the lints, `TypeScript · *` and the Rust tests.

Four facts made some legs duplicates rather than just rare. `coverage.yml` runs the full Go suite on linux, macOS 15 arm64 and Windows 2025 on every Go PR. `Docker · Build (cli, …)` builds the UI and the CLI module without `go.work`. E2E builds the amd64 `final` images of dashboard, cluster-api and cluster-agent itself, through `e2e/docker-bake.hcl`. `install.bats` mocks `uname`, so the runner's architecture is never under test.

A Docker build checks two things. Its builder stages build each component as a release does: a module without the workspace, on the toolchain the image pins. A code change can break those. Its `final` and `final-alpine` stages copy the binary onto `scratch`, `busybox` or `alpine`, and only a change to the Dockerfile or its base can break them.

## Decision

A job leg is in one of three groups, and `ci.yml` runs it accordingly:

- **Another job covers it on the same run:** removed. `CLI · Build` is gone: `TypeScript · Build` builds the UI, `Docker · Build (cli, …)` embeds it and builds the module alone, and `Go · Build and Test` compiles the CLI on each runner. The arm64 and second-macOS legs of `Shell · Test`, the macOS 15 arm64 leg of `Go · Build and Test` and the Kubernetes 1.25 leg of E2E are gone too. A PR that changes no Dockerfile builds only the cli image's `final` stage, since E2E builds the other images; the `docker` filter in `.github/ci-config/path-filters.yml` brings back every image and target.
- **It covers a platform nothing else does, but rarely fails:** `main` only. `Go · Build and Test` runs on every OS and arch the CLI ships for on `main`, and on `ubuntu-24.04` alone on a PR. `Docker · Build` builds arm64 on `main` only. `Alpine · Build` sits with the riscv64 jobs, on `main` only.
- **It catches failures:** on every PR. The lints, the TypeScript and Rust jobs, `Shell · Test` on one runner per OS, the cli image on amd64, and E2E on 1.21 and the latest.

A matrix that differs between a PR and `main` takes each list from `fromJSON` on `github.event_name` or a `changes` output. It has no `include`: GitHub adds an `include` entry that matches no combination as a job of its own, so a runner chosen by OS and arch comes from a lookup in `runs-on`.

## Alternatives considered

**Keep `CLI · Build` on linux/amd64 for PRs.** Its only check beyond the jobs above is that `make build` runs, and its `PLATFORM` variable was never read by the `Makefile`.

**Replace `CLI · Build` with one job that builds as `release-cli.yml` does**, vendored with `GOWORK=off`, cross-compiled for every release target and Alpine architecture. A cold build of the CLI took about a minute per target on eight cores, so the job would have run for ten minutes or more. A Go cache covering every target would have taken gigabytes of the repository's cache quota. `Docker · Build (cli, …)` already builds the module without the workspace; only `-mod=vendor` is left unchecked.

**Keep one 32-bit Alpine leg on PRs.** It is the only 32-bit build. It never failed alone in the sample, and an overflow on a 32-bit target is cheap to fix after merge.

**Build every image on a PR only when its own Dockerfile changed.** Dockerfile changes are rare, and one filter for all four keeps the matrix to three lists.

**Move every leg to `main` instead of removing duplicates.** A duplicate costs runner time on `main` and tells nothing new there either.

## Consequences

A PR that touches Go, Rust and TypeScript but no Dockerfile starts 7 platform jobs instead of 47, about 22 runner-minutes at the median instead of 124. It starts one macOS job in `ci.yml` instead of six, which matters most while macOS runners queue. A push to `main` runs about 86 runner-minutes instead of 124.

A failure specific to arm64, macOS on Intel, Windows on arm64, Alpine or riscv64 now shows up on `main`, after merge. Someone has to watch `main`'s runs.

Nothing checks that `make build` runs on macOS or Windows.

E2E is the only PR check that the dashboard, cluster-api and cluster-agent images build. It runs on the same paths as `Docker · Build`, so a PR that skips one skips both.

`Go · Build and Test` on a PR and `coverage.yml` together give one leg per OS. A change that drops a runner from `coverage.yml` drops it from PRs entirely.

## Revisit when

A platform-specific bug reaches `main` that a PR leg would have caught, or the code gains files or branches for one OS or architecture (`_windows.go`, `runtime.GOOS`) beyond the two it has now.
