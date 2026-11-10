---
title: Check generated code in CI by regenerating it
date: 2026-11-10
scope: repo
status: Accepted
---

# Check generated code in CI by regenerating it

## Context

Generated Go and TypeScript are committed ([ADR: commit generated Go and TypeScript](2026-11-09-commit-generated-go-and-typescript-generate-rust-at-build-time.md)): the protoc output in `modules/shared`, gqlgen's in `modules/dashboard` and `modules/cluster-api`, and the UI's `__generated__/`. The CLI embeds a copy of `config/default/cli.*` in `modules/cli/assets/`, made by `go generate`. Nothing checked any of them, so a PR could change a source and leave its output stale (#1220, #1335).

The `*.pb.go` headers record the versions of `protoc`, `protoc-gen-go` and `protoc-gen-go-grpc` that wrote them. CI's `arduino/setup-protoc` installed the newest `23.x`, while the committed files came from `protoc` 33.1, and the plugins were whatever a developer had on their `PATH`.

## Decision

`scripts/check-generated.sh` (`make check-generated`) runs every generator, `go generate ./...` in each module and `pnpm graphql-codegen`, and fails if any file under their outputs changed. CI's `Generated · Check` runs it when a path in the `generated` filter of `.github/ci-config/path-filters.yml` changes, and on every push to `main`.

Each generator runs at one version. `protoc` is the `protoc-version` default of `.github/actions/setup-environment/action.yml`, which every CI job that sets up `protoc` installs; the script refuses any other. The two plugins are `tool` lines in `tools/go.mod`, which the script builds. gqlgen and codegen are already pinned by each module's `go.mod` and `pnpm-lock.yaml`.

## Alternatives considered

**Ignore the version lines of the `*.pb.go` headers.** Any `protoc` would then do. But a newer plugin can also change the code it writes, and a check that tolerates one difference has to decide which others it tolerates. Pinning makes the output a function of the source.

**Compare against `git diff --exit-code`.** Simple in CI, but it fails on any uncommitted change, so it cannot run on a working tree. The script compares checksums of the outputs before and after it runs instead, and leaves the regenerated files in place to commit.

**A job per generator.** Each would set up less, but the gqlgen and codegen checks need the same schema paths, and one job keeps the filter and the script in one place.

## Consequences

A PR that edits a `.proto`, a schema, a UI operation or `config/default/cli.*` without regenerating fails CI. Running the check locally needs Go, pnpm and the pinned `protoc`.

Dependabot moves the plugins in `tools/go.mod`. Such an update changes the `*.pb.go` headers, so its PR fails `Generated · Check` until someone regenerates on it. Bumping `protoc-version` likewise needs a regeneration in the same commit. The Rust jobs build with the pinned `protoc` too.
