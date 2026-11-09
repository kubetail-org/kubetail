---
title: Dependabot moves dependencies, and a person merges every update
date: 2026-11-09
scope: repo
status: Accepted
---

# Dependabot moves dependencies, and a person merges every update

## Context

Until #1212, Renovate ran from `renovate.yml` on every push to `main`, with a bot token and SSH key from the `production` environment. Its config enabled only the `github-actions` manager and only pinned actions to digests; every version update was disabled. Go modules, Cargo crates and npm packages were updated by hand (#1196, #1198), and nothing in CI checked any of them for known advisories. Dependabot alerts were on; security updates were off.

Three constraints shape what an updater can do here:

- CI's jobs are not required checks, so a green run is not a gate anything can rely on.
- Each Go module builds without `go.work` (→ [ADR](2026-11-09-each-go-module-builds-without-go-work.md)), so an update has to land in each module's `go.mod` on its own.
- GitHub's dependency graph does not read `pnpm-lock.yaml` past its first YAML document, so Dependabot alerts, and the security updates they drive, do not see our npm packages. Version updates do: Dependabot reads the lockfile itself.

## Decision

Dependabot (`.github/dependabot.yml`) proposes updates for `github-actions`, `gomod` (each module under `modules/`, and `tools/`), `cargo` (`crates/`) and `npm` (`dashboard-ui/`). Each waits after a release (seven days; ten for npm, to match `minimumReleaseAge` in `pnpm-workspace.yaml`), puts minor and patch updates in one weekly PR per ecosystem, and leaves each major in a PR of its own. `k8s.io/*` and `sigs.k8s.io/*` move together. Security updates are grouped per ecosystem and skip the wait.

No update merges automatically. A maintainer reviews and merges each one.

`audit.yml` fails a PR only for the advisories it brings in: `dependency-review-action` for Go and Cargo, and `scripts/audit-npm-diff.sh`, which compares `pnpm audit` on the base and the head, for npm. On `main`, nightly and on dispatch it audits the whole tree with `govulncheck`, `cargo audit` and `pnpm audit`, and opens or comments on a `security` issue when that fails on `main`. On `main` it also submits the npm tree to the dependency graph (`scripts/npm-dependency-snapshot.sh`), so Dependabot alerts cover npm.

Toolchains are bumped by hand: the Go version (each `go.mod` and `setup-environment`'s `go-version`), `packageManager` in `dashboard-ui/package.json` and `setup-environment`'s `node-version`. The audit tools are not toolchains, so Dependabot moves them: `govulncheck` is a `tool` dependency in `tools/go.mod`, and `cargo-audit` is installed by `taiki-e/install-action` with no version, which takes the version and checksums that action's release names.

## Alternatives considered

- **Keep Renovate and enable its other managers.** It works, but it needs a bot token and SSH key in the `production` environment that the release workflows use, and a workflow of our own to run it. Dependabot runs on GitHub's side with no secret of ours, and its security updates follow the alerts we already have.
- **Auto-merge patch updates that pass CI** (a workflow on `dependabot/fetch-metadata`). CI is not required, so auto-merge would merge a red PR. A patch release is also where a compromised publish lands; the cooldown narrows that window without closing it.
- **Audit the whole tree on every PR.** A newly published advisory would then fail unrelated PRs, and their authors would own a fix they did not cause. The nightly run puts it on the maintainers instead.
- **Let Dependabot move toolchains** (the `go` directive, `packageManager`). A toolchain bump brings new lints and a cold cache, and wants a person choosing when.

## Consequences

- About four grouped PRs a week to review, plus a major now and then.
- A new `cargo-audit` arrives inside the weekly Actions PR, named only in install-action's changelog.
- Someone has to remember the hand-bumped toolchain pins; the comment at the top of `dependabot.yml` lists them.
- The `fsnotify` pin is a `replace` (→ [ADR](2026-11-09-pin-fsnotify-to-v1-8-0.md)), which Dependabot does not move. It may still raise the `require` line, which the `replace` overrides.
- Security updates need the repository setting turned on; `dependabot.yml` cannot do it.

## Revisit when

CI's jobs become required checks, which removes the objection to auto-merge; or GitHub's dependency graph reads `pnpm-lock.yaml` whole, which makes `npm-dependency-snapshot.sh` unnecessary.
