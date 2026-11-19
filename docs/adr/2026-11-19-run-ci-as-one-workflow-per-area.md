---
title: Run CI as one workflow per area
date: 2026-11-19
scope: repo
status: Accepted
---

# Run CI as one workflow per area

## Context

Until this change, `ci.yml` held every job: 22 of them, for Go, Rust, TypeScript, shell, YAML, Markdown, Python, the images and the end-to-end suite. One `changes` job picked them by language from `.github/ci-config/path-filters.yml`. The repo had just moved to one folder per component (→ [ADR](2026-11-17-one-folder-per-component.md)), and the Go jobs still ran all four modules together, so a change to the Cluster API ran the CLI's six-platform test matrix and the reverse.

No job was a required check. A required check matches a job name alone, and a workflow-level `paths:` filter leaves a check that never reports, so a required check on a skipped workflow blocks a pull request forever. The Go caches had grown with the shared jobs: `Go · Vet` and `Go · Staticcheck` each saved a cache of about 1 GB that held what `Go · Build and Test` already held.

## Decision

Each area is one workflow file, which `ci.yml` calls as a reusable workflow:

- `ci-cli.yml`: the CLI and the Dashboard backend, on every platform the CLI ships for, with the Alpine and 386 jobs and the cli and dashboard images. The Dashboard ships inside the CLI, so its tests run there.
- `ci-libs.yml`: `libs/go-common`, on the same platforms, since it ships inside the CLI too. A change to it also runs `ci-cli.yml` and `ci-cluster.yml`, whose code imports it.
- `ci-cluster.yml`: the Cluster API on linux, the Cluster Agent's Rust jobs, and the cluster-api and cluster-agent images.
- `ci-web.yml`: the React app.
- `ci-e2e.yml`: the end-to-end suite, on every path the three above watch and `e2e/`. It builds its own images.
- `ci-repo.yml`: what belongs to no component: shell, YAML and actions, Markdown, toolchains and digests, the Python lint, `tools/` and `Generated · Check`.

`ci.yml` starts on each pull request and push to `main`, with no `paths:`, and calls every area. Each area also triggers on `workflow_dispatch`, so it runs alone. Its Plan job runs the composite action `.github/actions/plan`, which composes the workflow's jobs from narrow keys in `path-filters.yml` (`cli`, `dashboard`, `libs`, `go-common`, `graphql-*`, `cluster-api`, `cluster-agent`, `proto`, `web`, `e2e`, `repo`, and the existing `shared`, `docker`, `shell`, `yaml`, `generated`, `python`, `actions`, `codeql`). `codeql.yml` composes its languages from the same keys, and each area's `coverage-*.yml` composes the same keys as its `ci-*.yml` (→ [ADR](2026-11-19-measure-and-gate-coverage-per-area.md)). `ci.yml` ends in `CI · Passed`, which needs every area, runs `if: always()`, and fails when an area failed or was cancelled. `coverage.yml` calls the `coverage-*.yml` files the same way and ends in `Coverage · Passed`. Those two are the required checks; `tools/cipassed`, run by `make lint-yaml`, fails a caller that misses an area file or its `· Passed` job.

Cache keys start with the job id, and job ids are unique across the files. Vet and staticcheck restore their area's `*-test-linux-amd64` cache and save none. Each area has its own `CACHE_MODE` and `Caches · Prune`, which prunes only the caches its own jobs save (`.github/actions/prune-caches`, given their names). The callers hold the concurrency groups and grant each area its permissions.

## Alternatives considered

- **Keep `ci.yml` and add one required job to it.** It works, but the file stays one long list for every area, and an area cannot be dispatched alone.
- **One top-level workflow per area, each its own required check.** Every push would start ten workflow runs, most of them skipping every job, and a new push would cancel all ten, so the Actions log fills with runs that did nothing. A new area would gate nothing until a person added its check to the ruleset.
- **A workflow-level `paths:` filter per workflow.** The cheapest skip, and the one that blocks a pull request: a workflow that never starts never reports its required check.
- **Split by language instead of by component** (`ci-go.yml`, `ci-rust.yml`). The Cluster API and the CLI would share a matrix again, and a reader looking for the cluster's CI would read two files.
- **Test `libs/go-common` in the CLI's area.** It ships inside the CLI, but every Go component imports it, and a reader looking for the library's CI would find it under one consumer.
- **Test `libs/go-common` in both Go areas.** Its tests would run twice on linux, and neither area would own it.

## Consequences

- Every push starts one `ci` run and one `coverage` run. A pull request touching one area runs that area's jobs, plus `ci-repo.yml`'s and `ci-e2e.yml`'s where their paths apply; every other area runs its Plan alone. `ci-repo.yml` always runs Markdown and toolchains.
- A failing job reads one level deeper: `ci / cli / Go · Lint`.
- `gh workflow run ci-cli.yml --ref <branch>` runs one area. A dispatch runs every job of it, the Alpine and 386 jobs included.
- E2E no longer waits for `Docker · Build`: the two sit in different workflows. On a pull request it reads the BuildKit caches `main` saved.
- The `go-vet` and `go-staticcheck` caches are gone; the Cluster API's two test caches take their place.
- A new area file is gated once its caller calls it, which `make lint-yaml` enforces; the ruleset does not change.
- A path in no key runs nothing on a pull request. `main` runs every area, so it shows up there.

## Revisit when

A sixth area, such as the desktop or mobile app, starts. It gets a workflow of its own, and the ruleset a required check.
