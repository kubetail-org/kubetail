---
title: Define the CI runner matrix in one runners.json file
date: 2026-11-17
scope: repo
status: Accepted
---

# Define the CI runner matrix in one runners.json file

## Context

[Run each OS once on a PR](2026-11-10-run-each-os-once-on-a-pr-and-the-other-platforms-on-main.md) built each matrix in `ci.yml` that differs between a PR and `main` inline: one `fromJSON` list per axis, chosen on `github.event_name`, and the runner from a lookup written into `runs-on`. `Go · Build and Test` held a six-entry lookup, `Shell · Test` a three-entry one, `Rust · Build and Test` an `include` per runner, and `Docker · Build` and E2E each picked the arm64 runner with a conditional. Changing a runner, or which legs a PR runs, meant finding and editing each of them.

## Decision

`.github/ci-config/runners.json` lists every runner `ci.yml` tests on, one `{os, arch, runner, pull_request}` entry per OS and arch. `pull_request` marks the legs a PR runs; a push or a dispatch runs them all.

`Plan` reads it with `jq` and outputs three values. `legs` is the `{os, arch}` pairs this event runs; `Go · Build and Test` takes it whole as its matrix's `include`. `linux-arches` is the Linux arches among them, the `arch` axis of `Docker · Build` and E2E. `runners` maps each `<os>-<arch>` to its runner, and every one of those jobs, `Rust · Build and Test` and `Shell · Test` too, takes its `runs-on` from it.

A job that runs a different set of legs keeps its own lists in its matrix: Rust's two arches on every event, Shell's one arch per OS, E2E's `exclude` of arm64 on Kubernetes 1.21. The race-test skip on windows/arm64 and `Go · Test (linux, 386)` stay in their jobs.

`legs` holds only `os` and `arch`, so a job's default name stays `(linux, amd64)`. Every key of an `include`-only matrix shows in that name, and required checks match names. A key that `include` adds to a combination the lists made does not show, which is how `Shell · Test` gets its arch.

## Alternatives considered

**One `include` entry per runner in each job, with the runner in it.** It is the GitHub idiom, but it puts the runner list back in every job, which is what this replaces.

**Put the runner in `legs` and use `matrix.runner`.** Every key of an `include`-only matrix shows in the job's default name, so each check would be renamed to `(linux, amd64, ubuntu-24.04, true)`.

**Read `runners.json` in `coverage.yml` too.** Its Go job runs one runner per OS on every event, and its macOS runner is `macos-15`, not the `macos-26` that `Go · Build and Test` uses for macos/arm64. Reading the file would change that leg, or need a second entry for the same OS and arch. Its three-entry list stays in the job.

**Generate the workflows from a template.** It would add a build step and a check that its output is committed, for a few lists.

## Consequences

A runner image or a PR leg changes in one line of `runners.json`. A new OS or arch takes one line there, and runs in every job whose matrix reads `legs` or `linux-arches`; a job with its own lists needs them edited too.

`Plan` checks out `.github` on every event, not only on a PR, to read the file.

`Rust · Build and Test`'s cache key is named by OS and arch, like `Go · Build and Test`'s, rather than by runner.

`coverage.yml` and `stress.yml` keep their own runner lists. A change to a runner in `runners.json` that should reach them is made there too.

## Revisit when

`coverage.yml` or `stress.yml` come to run the same legs as `ci.yml`, or a job needs a property of a runner beyond its label.
