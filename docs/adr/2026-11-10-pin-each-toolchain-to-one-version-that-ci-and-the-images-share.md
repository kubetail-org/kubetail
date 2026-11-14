---
title: Pin each toolchain to one version that CI and the images share
date: 2026-11-10
scope: repo
status: Accepted
---

# Pin each toolchain to one version that CI and the images share

## Context

Until #1285, CI and the release images built with different toolchains. CI installed the latest Rust stable through `dtolnay/rust-toolchain`, and `crates/rust-toolchain.toml` said `channel = "stable"`, which the `rust:1.90.0-slim` image satisfied with the 1.90.0 it shipped. CI installed the newest Node 22 (`node-version: '22'`), and the images built the UI on `node:22.16.0`. A crate or package whose minimum version sat above the image's passed every language job and failed only in `Docker · Build`, or shipped in an image built by a toolchain CI never ran.

Go did not drift: `setup-environment`'s `go-version` named an exact version, and the `golang` images matched it by hand.

## Decision

Each toolchain has one exact version, set in one file. Go and Node are the `go-version` and `node-version` defaults in `.github/actions/setup-environment/action.yml`. Rust is `channel` in `crates/rust-toolchain.toml`, which `setup-environment` reads and passes to `dtolnay/rust-toolchain`, and which rustup reads in a local checkout.

Every `golang`, `node` and `rust` image in `build/package/` and `hack/tilt/` carries that version in its tag. `scripts/check-toolchain-versions.sh` (`make lint-toolchains`, CI's `Toolchains · Lint` on every PR) fails when a tag differs, or when a source names less than an exact `x.y.z`.

A person bumps a toolchain, in one commit that changes the source and every image.

## Alternatives considered

- **Keep CI on the latest and update the images when CI moves.** CI moves on its own, on any run, so nothing marks the moment the images fall behind; that is the drift this replaces.
- **Let Dependabot's `docker` ecosystem move the images.** It would bump an image alone, and the lint would fail its PR until a person bumped the source to match. Toolchains already move by hand (→ [ADR: Dependabot moves dependencies, and a person merges every update](2026-11-09-dependabot-moves-dependencies-and-a-person-merges-every-update.md)), so it would add only PRs that cannot pass.
- **Build-time arguments that CI fills from the source** (`ARG RUST_VERSION`). A plain `docker build` outside CI would need the argument too, and Tilt and `e2e/docker-bake.hcl` would each have to pass it.

## Consequences

CI tests with the toolchain a release ships with. A new Rust or Node release reaches CI only when someone bumps it, so new clippy lints and new minimum versions arrive in that PR, not in an unrelated one. Someone has to remember to bump; the comment at the top of `.github/dependabot.yml` lists the pins.

## Revisit when

Dependabot can move a version across several files in one PR, or an image tag can be read from a file the Dockerfile copies.
