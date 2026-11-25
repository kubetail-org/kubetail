---
title: A pull request fails only for the advisories it brings in
date: 2026-10-26
scope: repo
status: Accepted
---

# A pull request fails only for the advisories it brings in

## Context

`ci.yml` ran `govulncheck`, `cargo audit --deny unsound --deny unmaintained` and
`pnpm audit --audit-level moderate` over the whole dependency tree on every pull request. An
advisory published against a dependency already on `main` turned every open pull request red,
including ones that touched no dependency. The contributor could not fix it in their pull request
(#116).

Most pull requests come from forks. A fork's `GITHUB_TOKEN` is read-only, so a fork's run cannot
write to the repository's dependency graph.

GitHub's dependency graph reads `go.mod` and `Cargo.lock` whole. pnpm 12 writes `pnpm-lock.yaml`
as two YAML documents: the first pins pnpm itself, the second holds the project's tree. The graph
reads only the first, so it holds 15 npm packages, all pnpm's. Dependabot alerts and security
updates read the graph, so they did not cover our npm dependencies.

## Decision

A pull request is checked only for what it changes:

- `Dependency review` runs `actions/dependency-review-action` at `fail-on-severity: moderate`
  over every scope. It compares the base and head dependency graphs, which hold the Go and Cargo
  packages.
- `TypeScript · Dependency review` runs `pnpm audit` on the base and on the head
  (`scripts/audit-npm-diff.sh`) and fails on an advisory, at one version, that the base lacks.

The whole-tree audits move to `.github/workflows/audit.yml`, which runs daily, on push to `main`,
and on dispatch, with their flags unchanged. When one fails on `main`, the run opens an issue
labelled `security`, or comments on the one still open.

On `main`, `audit.yml` also submits the pnpm tree to the dependency graph. `pnpm sbom` reads the
lockfile and `scripts/npm-dependency-snapshot.sh` turns it into a snapshot. That gives npm
Dependabot alerts and security updates.

## Alternatives considered

- **Submit the pnpm tree on pull requests too, and let dependency-review cover npm.** A fork's
  read-only token cannot submit, so the head would hold no npm packages and a fork's npm changes
  would go unchecked.
- **Submit with `component-detection-dependency-submission-action`.** At run time it downloads
  the latest Component Detection release, unpinned, and runs it with a token that can write. Every
  other tool CI runs is pinned. `pnpm sbom` is part of the pinned pnpm.
- **Rely on Dependabot alone for `main`.** `govulncheck` reports only vulnerabilities our code
  calls, and `cargo audit` reads RustSec, which lists unsound and unmaintained crates that GitHub's
  advisory database does not.

## Consequences

- A pull request that changes no lockfile passes, whatever the advisories against `main`.
- dependency-review goes by version, so a Go bump can be flagged for code we never call. Add its
  GHSA to the action's `allow-ghsas` with a reason.
- A pull request that starts calling a vulnerable function in a Go dependency we already have,
  and a RustSec-only advisory, are caught by the next `audit.yml` run, not on the pull request.
- Every pull request's dependency review sees the base's npm packages, which the head lacks, as
  removed. A removal never fails it.
- The issue that `audit.yml` opens is the maintainers' to close; a later failure comments on it.

## Revisit when

- GitHub's dependency graph reads both documents of `pnpm-lock.yaml`. `Dependency review` then
  covers npm, and the snapshot script, its job, `TypeScript · Dependency review` and
  `scripts/audit-npm-diff.sh` can go.
- dependency-review reads RustSec.
