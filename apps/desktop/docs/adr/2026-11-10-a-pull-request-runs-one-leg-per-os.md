---
title: A pull request runs one leg per OS, and main runs every leg
date: 2026-11-10
scope: repo
status: Accepted
amended_by:
  - [CI checks every platform split the code has on a pull request](2026-11-10-ci-tests-every-platform-split-the-code-has.md)
---

# A pull request runs one leg per OS, and main runs every leg

## Context

`ci.yml` ran Go · Build and Test, Rust · Build and Test and Rust · Vet on nine runners: Linux,
macOS 15, 26 and 27 and Windows, on amd64 and arm64. A PR touching Go and Rust started 27
matrix jobs, 15 of them on macOS. #831 asked which of them catch anything the others miss.

What the audit found, from the 400 `ci.yml` runs between 2026-10-31 and 2026-11-10 (62 failed):

- **No ci.yml job is a required check.** The `main` ruleset requires `DCO` alone.
- **No ci.yml job feeds Codecov.** `coverage.yml` measures on its own triggers, on one runner
  per OS (`ubuntu-24.04`, `xcode-27`, `windows-2025-vs2026`).
- **Every platform split in the code is by OS.** `src-tauri` uses `target_os`, `target_vendor`,
  `unix` and `windows`, never `target_arch`; the sidecar and the CLI have no file or build tag
  split by architecture, and nothing branches on the macOS release.
- **No failure was caught by one architecture or one macOS release alone.** Of the 23
  single-leg failures, 14 were the runner itself: "The hosted runner lost communication with the
  server", a DNS failure fetching an action, a timeout reaching `proxy.golang.org`. Most were on
  `macos-26-intel`. The other nine were test failures on one leg, their logs since expired.
  Five passed on the branch's next run, two were their branch's last run, and two in a row on
  `macos-26` arm64 stopped after #650, which fixed a race deleting an SQLite pool's file first
  reported on Windows (#648). The Windows bugs on the branches for #813 and #823 failed both
  Windows legs together, and a broken change on #655's branch failed every leg.
- **Earlier version-specific failures were the runner's environment**, not Kubetail's code:
  osascript timing out on macOS 15 Intel (#122), the race pass outrunning `go test`'s timeout
  there (#139), a macOS 26 Intel runner with no address (#199).
- **`main` must keep every leg.** `stress.yml` restores `go-test-<runner>` for each of the nine
  runners, and `audit.yml` restores `rust-vet-ubuntu-24.04`; a push to `main` saves them.

## Decision

The legs live in `.github/ci-config/runners.json`, one entry per runner. An entry marked
`pull_request` is its OS's one PR leg: `ubuntu-24.04`, `xcode-27` and `windows-2025-vs2026`, the
runners `coverage.yml` uses. `ci.yml`'s `Changes` job reads the file and outputs `legs`, the
legs this event runs, and `pull-request-legs`.

| Job | Leg | Pull request | Push to `main`, dispatch | Why |
| --- | --- | --- | --- | --- |
| Go · Build and Test | linux amd64, macos-27 arm64, windows amd64 | Keep | Keep | One per OS covers every split the code has, and keeps `-race` on each OS |
| Go · Build and Test | linux arm64, macos-15 ×2, macos-26 ×2, windows arm64 | Move to `main` | Keep | Caught nothing the OS's leg missed; their failures were runner faults and flakes; `stress.yml` reads their caches |
| Rust · Build and Test | linux amd64, macos-27 arm64, windows amd64 | Keep | Keep | As Go's |
| Rust · Build and Test | the other six | Move to `main` | Keep | As Go's |
| Rust · Vet | linux amd64, macos-27 arm64, windows amd64 | Keep | Keep | Clippy's result turns on `target_os` alone |
| Rust · Vet | the other six | Remove | Remove | The same lints on the same `cfg`; they found nothing but runner faults |
| Sandbox · AppArmor | ubuntu-24.04, ubuntu-24.04-arm | Keep | Keep | It builds bwrap and pasta from C natively, and pasta's seccomp filter is per architecture; two minutes on Linux |
| Go · Lint, Go · Vet, Go · Staticcheck | linux | Keep | Keep | Each check is its own; Go · Vet is the one place `GOOS=windows` code is vetted |
| Rust · Lint, Shell · Lint, YAML · Lint, Markdown · Lint | linux | Keep | Keep | Each check is its own, and costs a minute or less |
| TypeScript · Lint, Test, Build | linux | Keep | Keep | Build alone runs the production bundle; the webview is one platform |
| Fork gate, Changes, Caches · Prune | — | Keep | Keep | Plumbing |

A PR's run starts at most 9 matrix jobs, 3 on macOS, where it started 27, 15 on macOS. A push to
`main` starts 21. Check names keep their `(<os>, <arch>)` suffix, so a leg's history reads the
same on PRs and on `main`.

## Alternatives considered

- **Keep the full matrix on PRs.** Its extra legs found runner faults, which a PR author then
  re-runs, and flakes, which `stress.yml` finds on every runner every night.
- **Drop the extra legs from `main` too.** `stress.yml` reads their Go caches, and they are the
  one place a change is built on every runner users install on before a release.
- **Filter the matrix with `exclude` per event.** GitHub applies `include` after `exclude`, and an
  `include` entry matching no combination adds a new one, so an excluded leg's `runner` entry
  would bring it back. An `include` list read from a file has no such trap.
- **Vet Rust once, cross-compiling to each OS.** `objc2`'s build script needs an Apple `cc`, so
  clippy for macOS needs a macOS runner anyway.

## Consequences

- A regression only an extra leg catches lands on `main` first. GitHub notifies whoever merged
  the PR that the push run failed; the fix is a revert or a follow-up PR. Before merging a change
  that touches a platform's edge (an architecture, a macOS release), `gh workflow run ci.yml --ref
  <branch>` runs every leg on the branch.
- `runners.json` and `stress.yml`'s matrix must list the same runners. Workflow YAML cannot read
  the file into a matrix without a job to read it, which `stress.yml` has not got.
- No ruleset changes: none of these jobs is required.

## Revisit when

- Code branches on `target_arch`, `GOARCH` or the macOS release: give that platform its PR leg,
  and give Rust · Vet that architecture.
- A failure on `main` turns out to be one only an extra leg could catch, more than once.
- A ci.yml job becomes a required check.
