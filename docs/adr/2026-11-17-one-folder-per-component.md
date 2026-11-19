---
title: Keep one folder per component
date: 2026-11-17
scope: repo
status: Accepted
---

# Keep one folder per component

## Context

The repo grouped code by language: the Go modules under `modules/`, the Rust workspace under `crates/`, the gRPC contract under `proto/`, the UI under `dashboard-ui/` and the release Dockerfiles under `build/package/`. A reader looking for the Cluster API found half of it in `modules/cluster-api/` and its Dockerfile three folders away. The Cluster API and the Cluster Agent ship together, yet sat in two trees. A desktop and a mobile app are planned, and the layout had no place for a second app.

## Decision

Each component has one top-level folder, which holds its code, guide and release `Dockerfile`:

- `cli/`, `dashboard/`: Go modules.
- `cluster/api/`: the Cluster API's Go module. `cluster/agent/`: the Cargo workspace of the Cluster Agent, with `rust-toolchain.toml`.
- `apps/web/`: the React app. `apps/desktop/` and `apps/mobile/` hold a README until work on them starts.
- `libs/`: what more than one component reads. `libs/go-common/` is the shared Go module, `libs/proto/` the gRPC contract, `libs/graphql/<server>/` each GraphQL server's `schema.graphqls`.

`go.work` and `go.work.sum` sit at the repo root and list the four Go modules. `tools/` keeps its own module, with a `go.work` of its own so the root workspace does not claim it. The Dockerfiles build from the repo root and copy each folder to the same relative place it has in the repo, which keeps each `go.mod`'s `replace` lines valid inside the image.

These stay as they were: the `Makefile`, the release skills, Dependabot, one Cargo workspace, a committed `go.work`, the Helm chart in its own repo, all ADRs in `docs/adr/` with dated names and the existing `scope` values, no shared Rust or TypeScript library, and the release tags.

## Alternatives considered

**A `justfile` in place of the `Makefile`.** CI and the `*-changed.sh` scripts are built on the `Makefile`'s targets. A rewrite would move every one of them for no gain the layout needs.

**release-please in place of the release skills.** A person cuts each release: [Release each component by its own tag](2026-11-09-release-each-component-by-its-own-tag.md).

**Renovate in place of Dependabot.** Already decided: [Dependabot moves dependencies, and a person merges every update](2026-11-09-dependabot-moves-dependencies-and-a-person-merges-every-update.md).

**Separate crates, one `Cargo.lock` each.** The workspace holds the shared dependency versions and lints, and only the agent uses Rust.

**A local, gitignored `go.work`.** Go coverage is measured across the workspace in CI: [Measure Go coverage across the workspace](2026-11-09-measure-go-coverage-across-the-workspace.md).

**The Helm chart in this repo.** Out of scope; it has its own release cycle.

**ADRs in each component's folder.** The decisions with the most reasoning cross components and would have no natural home.

**Numbered ADRs, or `scope` values renamed after the folders.** Dated names are already in use, and a scope labels a component, not a path.

**`libs/rust-common` and `libs/js-common`.** Each would have one consumer today.

**Release tags named after the folders** (`cluster/api/v1.2.3`). The release workflows and the CLI's update check parse the tags.

## Consequences

A component's code, guide and Dockerfile are found under one folder, and the path filters of CI map one-to-one to components.

Module paths change (`github.com/kubetail-org/kubetail/cli`, `…/dashboard`, `…/cluster/api`, `…/libs/go-common`). Package recipes outside this repo that build `modules/cli` must follow, and a stale `-X …/cmd.version` path does not fail a build: the binary reports the wrong version.

gqlgen embeds a schema as a file only from inside the generated package's folder. From `libs/graphql/` it writes the schema into `generated.go` as a string. The server behaves the same.

A release tag such as `cli/v1.2.3` now also reads as a version of the Go module `…/kubetail/cli`. `go install` of it fails on the `replace` lines, as it did before.

The `release-<component>` path filters keep the old paths beside the new ones until each component has released once from this layout, so `tools/releasenotes` still finds the commits made before the move.

## Revisit when

The desktop or mobile app starts and the UI must split into a shared library and thin shells, or a second component starts to use Rust or TypeScript.
