---
title: Define the CI runner matrix in one runners.json file
date: 2026-11-17
scope: repo
status: Accepted
---

# Define the CI runner matrix in one runners.json file

## Context

[Run each OS once on a PR](2026-11-10-run-each-os-once-on-a-pr-and-the-other-platforms-on-main.md) built each matrix that differs between a PR and `main` inline: a `fromJSON` list per axis, chosen on `github.event_name`, and a runner lookup or conditional in `runs-on`. Each job in the `ci-*.yml` files held its own copy. Changing a runner, or which legs a PR runs, meant editing each of them.

## Decision

`.github/ci-config/runners.json` lists every runner the `ci-*.yml` files test on, one `{os, arch, runner, pull_request}` entry per OS and arch. `pull_request` marks the legs a PR runs; every other event runs them all. This replaces the inline lists and lookups of the earlier ADR. The legs it chose are unchanged.

The Plan action (`.github/actions/plan`) reads the file with `jq` and outputs three values, which each area's `Plan` job passes on:

- `legs`, the `{os, arch}` pairs this event runs: the matrix `include` of `Build and Test` in `ci-cli.yml` and `ci-libs.yml`.
- `linux-arches`, the Linux arches among them: the `arch` axis of `Build · Docker`, `Build and Test · Go` in `ci-cluster.yml` and E2E.
- `runners`, each `<os>-<arch>` to its runner: the `runs-on` of those jobs, and of `Build and Test · Rust` and `Test · Shell`.

A job that runs a different set of legs keeps its own lists: Rust's two arches on every event, Shell's one arch per OS, E2E's `exclude` of arm64 on Kubernetes 1.21.

## Alternatives considered

**Put the runner in `legs` and use `matrix.runner`.** Every key of an `include`-only matrix shows in the job's default name, so `(linux, amd64)` would become `(linux, amd64, ubuntu-24.04)`, and a required check matches the name. It would also leave the jobs with a Linux `arch` axis without a runner.

**One `include` entry per runner in each job.** It is the GitHub idiom, but it keeps the runner list in every job.

**Read `runners.json` in the `coverage-*.yml` files and `stress.yml` too.** They run other legs: one runner per OS on every event, with `macos-15` for macos/arm64 where `Build and Test` uses `macos-26`. The file would need a second entry for the same OS and arch.

**Generate the workflows from a template.** It adds a build step and a check that its output is committed, for a few lists.

## Consequences

A runner image or a PR leg changes in one line of `runners.json`. A new OS or arch is one line there, and runs in every job that reads `legs` or `linux-arches`.

The `coverage-*.yml` files and `stress.yml` keep their own runner lists. A runner change that should reach them is made there too.

`Build and Test · Rust`'s cache key names the OS and arch, not the runner.

## Revisit when

The `coverage-*.yml` files or `stress.yml` come to run the same legs as the `ci-*.yml` files, or a job needs a property of a runner beyond its label.
