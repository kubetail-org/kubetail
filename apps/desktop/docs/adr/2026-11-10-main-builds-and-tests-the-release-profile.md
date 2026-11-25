---
title: Main builds and tests the desktop host's release profile on the release runners
date: 2026-11-10
scope: repo
status: Accepted
---

# Main builds and tests the desktop host's release profile on the release runners

## Context

[CI checks every platform split the code has](2026-11-10-ci-tests-every-platform-split-the-code-has.md)
found two splits CI never built, and filed #844 for them.

- **The release profile.** `cfg!(debug_assertions)` changes `is_app_origin` (the navigation
  allowlist admits the dev server only in debug), the `Kubetail`/`Kubetail-dev` data and runtime
  directories, the keychain service, the sidecar's flags and log routing, and `main.rs`'s
  `windows_subsystem`. CI built and tested `src-tauri` under the dev profile alone, so the shipped
  half of each branch was first compiled by `release-desktop.yml` at a tag, and never tested.
  `is_app_origin`'s test asserts against `cfg!(debug_assertions)`, so it had only ever pinned the
  debug answer.
- **Ubuntu 22.04.** `release-desktop.yml` builds the Linux app on `ubuntu-22.04` and
  `ubuntu-22.04-arm` for their glibc floor. CI built on `ubuntu-24.04`, so a change needing a newer
  glibc or `webkit2gtk` failed only at release.

A release build (`lto = true`, `codegen-units = 1`) takes several times as long as a dev build.

## Decision

`ci.yml`'s **Rust · Release Build and Test** runs `cargo build --release` and `cargo test --release`
in `src-tauri` on a push to `main` and on dispatch, never on a pull request. It runs one leg per OS,
on the runners `release-desktop.yml` builds the app on: `ubuntu-22.04`, `macos-26` and
`windows-2025-vs2026`. Its matrix is written out in the job, not read from `runners.json`, since
those runners are the release's, not CI's.

The Linux leg on `ubuntu-22.04` is the 22.04 build: it compiles and links the host against 22.04's
glibc and `webkit2gtk`, and runs its tests there.

## Alternatives considered

- **Run it on every pull request.** It would catch a release-only failure before merge, at the cost
  of a release build per OS on each PR touching Rust. The branches it adds are a handful of
  constants and one allowlist entry; `main` catches them a merge later, well before a tag.
- **Lower the profile for the tests** (`lto = false`, more codegen units). `debug_assertions` would
  still be off, but the build would no longer be the one that ships, and changing `CARGO_PROFILE_*`
  for one step rebuilds every dependency.
- **Run `pnpm tauri build` on 22.04**, bundling the `.deb` and `.rpm`. It needs the sidecar, the
  frontend, bwrap and pasta built first, which is the release workflow's whole Linux path. The
  failure #844 names is the host linking a newer system library, which `cargo build` on 22.04
  already catches; the bundle steps change with Tauri and the release workflow, not with a PR.
- **Add `ubuntu-22.04` to `runners.json`.** Every Go and dev-profile Rust test would run there too,
  doubling the Linux legs for a floor only the shipped host binary has.

## Consequences

- A test of a `cfg!(debug_assertions)` branch asserts against `cfg!(debug_assertions)`, so each
  profile pins its own half; a test hard-coding the debug answer fails on `main`.
- A change that needs a newer glibc or `webkit2gtk` than 22.04 has fails on `main`, not at a tag.
- The Tauri `custom-protocol` feature, which `tauri build` turns on to embed the frontend, is still
  built only by the release. Nothing in `src-tauri` branches on it.
- The arm64 Linux, Intel macOS and arm64 Windows release builds stay the release's alone; nothing
  in `src-tauri` branches on the architecture.
- Moving a release runner means moving this job's leg with it.

## Revisit when

- `src-tauri` gains a `target_arch` split or a branch on `custom-protocol`.
- The release moves its Linux build off Ubuntu 22.04.
