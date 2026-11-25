---
title: Dependabot moves dependencies, and a person merges every update
date: 2026-10-26
scope: repo
status: Accepted
---

# Dependabot moves dependencies, and a person merges every update

## Context

`.github/dependabot.yml` watched only GitHub Actions. We learned about vulnerable Go, Rust and npm
dependencies when an audit job in CI failed, and fixed them by hand (#398). Dependabot alerts were
on; security updates were off.

Three constraints shape what Dependabot can do here:

- `main`'s ruleset requires one approving review and the `DCO` and CodeQL checks. CI's test, lint
  and audit jobs are not required checks.
- Tauri's `tauri` crate and its `@tauri-apps/api` / `@tauri-apps/cli` packages must share a
  major.minor, and a Dependabot group cannot span two ecosystems.
- GitHub's dependency graph does not read `pnpm-lock.yaml` (#116), so Dependabot alerts, and the
  security updates they drive, do not cover our npm dependencies. Version updates do: Dependabot
  reads the lockfile itself.

## Decision

Dependabot proposes updates for `github-actions`, `gomod` (`/sidecar`, `/tools`), `cargo`
(`/src-tauri`) and `npm` (`/`). Each waits seven days after a release, puts minor and patch
updates in one weekly PR per ecosystem, and leaves each major in a PR of its own. Security updates
are on, grouped per ecosystem, and skip the wait.

Tauri's minor and major versions are ignored in both ecosystems and bumped by hand, in one PR.

No update merges automatically. A maintainer reviews and merges each one.

Toolchains are bumped by hand: the Go version (`sidecar/go.mod`, `tools/go.mod`, and
`setup-environment`'s `go-version`), `src-tauri/rust-toolchain.toml`, `packageManager` in
`package.json`, and `setup-environment`'s `node-version` and `kubectl-version`. The audit tools
are not toolchains, so Dependabot moves them: `govulncheck` moved into `tools/go.mod` as a `tool`
dependency, and `cargo-audit` is installed by `taiki-e/install-action` with no version, which
takes the version and checksums that action's release names.

## Alternatives considered

- **Auto-merge patch updates that pass CI** (a workflow on `dependabot/fetch-metadata`). CI is not
  required, so auto-merge would merge a red PR; and the required review would need a workflow
  allowed to approve, a standing bypass of review at the supply-chain boundary. A patch release
  is also where a compromised publish lands; the cooldown narrows that window without closing it.
- **Let Dependabot move toolchains** (`rust-toolchain`, the `go` directive). A toolchain bump
  brings new lints and a cold cache, and wants a person choosing when.
- **Keep `cargo-audit` a hand-bumped `cargo install --version` pin.** Cargo has no `tool`
  directive, so Dependabot cannot see it, and an old scanner can break on a new advisory-database
  format. The action adds taiki-e as a party we trust, in a job with a read-only token and no
  secrets.
- **Let Dependabot propose Tauri minors.** Each side's PR would fail on its own until the other
  merged.

## Consequences

- About four grouped PRs a week to review, plus a major now and then.
- A new `cargo-audit` arrives inside the weekly Actions PR, named only in install-action's
  changelog.
- Someone has to remember the hand-bumped pins; `docs/security-model.md` lists them.
- `ignore` suppresses security updates for the ignored Tauri versions too, so a Tauri advisory
  reaches us through CI's audit jobs.
- npm has no Dependabot alerts or security updates until #116 lands; `pnpm audit` in CI covers it.

## Revisit when

CI's jobs become required checks, which removes the first objection to auto-merge; or
Dependabot can group across ecosystems, which lets Tauri move with the rest.
