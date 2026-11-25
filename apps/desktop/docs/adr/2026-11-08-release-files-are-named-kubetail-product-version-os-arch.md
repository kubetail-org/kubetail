---
title: Release files are named kubetail-<product>-<version>-<os>-<arch>
date: 2026-11-08
scope: repo
status: Accepted
amended_by: 2026-11-25-import-kstack-without-its-history-and-reset-its-migrations.md
---

# Release files are named kubetail-\<product\>-\<version\>-\<os\>-\<arch\>

## Context

Kubetail has several products under one name: the CLI, the desktop app and, later, a mobile app. The
CLI's release named its files `kubetail-cli-<version>-<os>-<arch>.{tar.gz,zip}`. The desktop release
attached Tauri's default names, which follow `productName` (`Kubetail`) and differ per format:
`Kubetail_<version>_amd64.deb`, `Kubetail-<version>-1.x86_64.rpm`, and the `.dmg` and `.msi` each in
their own style. Its SBOM was `kubetail-<version>.cdx.json` and its source archive
`embedded-sources.tar.gz`. No release had shipped, so a rename had nothing to migrate
([#801](https://github.com/kstackhq/kstack/issues/801)).

## Decision

Replaces the `Kubetail_*` package files in
The desktop app is Kubetail, and its binary kubetail-desktop,
which kept Tauri's names.

Every release file is named `kubetail-<product>-<version>-<os>-<arch>.<ext>`, all lowercase. `<os>` is
`linux`, `darwin` or `windows` and `<arch>` `amd64` or `arm64`, the CLI's words. The macOS app is one
universal build, so its `<arch>` is `universal`. `<version>` is the release's SemVer, also for the
RPM and MSI, whose package versions differ. A file that covers every platform drops `-<os>-<arch>`.
`SHA256SUMS` and `SHA256SUMS.asc` keep their names.

A desktop release attaches:

- `kubetail-desktop-<version>-linux-{amd64,arm64}.{deb,rpm}`
- `kubetail-desktop-<version>-darwin-universal.dmg`
- `kubetail-desktop-<version>-windows-{amd64,arm64}.msi`
- `kubetail-desktop-<version>.cdx.json`
- `kubetail-desktop-<version>-embedded-sources.tar.gz`
- `SHA256SUMS`, `SHA256SUMS.asc`

`productName` stays `Kubetail`, so Tauri still names the files it builds after it.
`release-desktop.yml` renames each one as it collects it: the `.deb`, `.rpm` and `.dmg` in their
build jobs, the `.msi` after SignPath has signed it, since SignPath's artifact configuration matches
Tauri's name. The release job fails unless its files are exactly the list above.

**The Linux packages' own name stays `kubetail`.** Tauri's bundler (tauri-cli 2.12) writes the
`.deb`'s `Package:` and the `.rpm`'s `Name:` as `productName` in kebab case and offers no setting for
either. A `productName` of `kubetail-desktop` in `tauri.linux.conf.json` would name them, but it also
names the desktop entry (`kubetail-desktop.desktop`), its `Name=`, and the resource folder, which this
decision keeps. So `apt remove kubetail` and `dnf remove kubetail` remove the desktop app.

## Alternatives considered

- **Keep Tauri's names.** Four styles in one release, none saying which product it holds, and no
  template the CLI's downloader could build a name from.
- **Rebuild the `.deb` and `.rpm` to set their package name.** `dpkg-deb -R`, edit the control file,
  `dpkg-deb -b` works for the `.deb`; the `.rpm` has no such round trip without a spec. Two
  hand-repacked formats is more release machinery than a name that no packaged CLI needs yet.

## Consequences

- One template builds every file name, for the workflows and for `kubetail app`'s downloader.
- A later mobile app's files are `kubetail-mobile-…`.
- A packaged CLI cannot take the package name `kubetail` while the desktop app holds it. Packaging the
  CLI means taking another name, or repacking the desktop's `.deb` and `.rpm`.
