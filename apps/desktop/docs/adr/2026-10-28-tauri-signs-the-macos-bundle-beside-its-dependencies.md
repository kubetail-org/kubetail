---
title: Tauri signs the macOS bundle in the job that installs its dependencies
date: 2026-10-28
scope: repo
status: Accepted
---

# Tauri signs the macOS bundle in the job that installs its dependencies

## Context

`release.yml`'s `build-bundle-macos` job runs `pnpm install` and `pnpm tauri build`. Tauri signs
the `.app` inside-out (sidecar, then app with `src-tauri/entitlements.plist`), builds the DMG,
signs it, notarizes it with the App Store Connect key and staples the ticket. To do that the job
holds `APPLE_CERTIFICATE`, its password and `APPLE_API_KEY_CONTENT`. Any dependency's install or
build script runs on the same runner and can read them.

Windows had the same exposure with the SignPath token. [#481](https://github.com/kstackhq/kstack/issues/481) moved that signing into
`sign-bundle-windows`, a job that checks out nothing and installs nothing. SignPath takes the
unsigned MSI as it is, so the split cost one job.

## Decision

Tauri keeps signing, notarizing and stapling the macOS bundle during `pnpm tauri build`, with the
Apple secrets in that job. The `production` environment's required reviewer decides when the job
runs; it does nothing about what runs inside it.

## Alternatives considered

- **Build unsigned, then sign in a job that installs nothing.** The second job would run
  `codesign` over the sidecar and the app, rebuild the DMG with `hdiutil`, sign it, and run
  `notarytool` and `stapler`. That replaces Tauri's bundler with our own script: the DMG loses its
  window layout and `Applications` link unless we copy Tauri's `bundle_dmg.sh`, and every Tauri
  upgrade that changes how it signs nested code is ours to track. None of it can be checked short
  of a signed, notarized run, and H-3 (`docs/TODO.md`) already waits on such runs.

## Consequences

- A compromised npm or Cargo dependency that runs code during install or build can read the Apple
  certificate and the App Store Connect key, and sign or notarize as Kubetail. pnpm's release-age,
  trust and exotic-subdependency settings, and `onlyBuiltDependencies`, narrow which code runs; they
  do not close the gap.
- The Windows and macOS release jobs differ in where their secrets sit. `docs/security-model.md`
  records both.

## Revisit when

A signed, notarized build can be run on demand (R-08's optional `ref` input, H-3's experiment), or
Tauri's bundler can sign and notarize an existing bundle as a step of its own.
