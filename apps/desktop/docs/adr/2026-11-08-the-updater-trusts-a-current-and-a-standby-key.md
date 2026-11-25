---
title: The updater trusts a current and a standby key
date: 2026-11-08
scope: cross-cutting
status: Accepted
---

# The updater trusts a current and a standby key

## Context

Kubetail had no in-app updater: a user stayed on the version they installed, past any security fix
(H-4b, [#814](https://github.com/kstackhq/kstack/issues/814)). `tauri-plugin-updater` (2.14.1) is
the mechanism. It downloads an artifact named by a JSON manifest, checks a minisign signature
against a public key compiled into the app, and installs it. Whoever holds the private key, and can
get a manifest in front of the app, can run code on every installed copy.

A desktop release builds on runners that install `node_modules` and run dependency build scripts.
The Windows MSI is Authenticode-signed by SignPath after the build, in a job that checks out and
installs nothing, so the SignPath token never shares a runner with installed dependencies. The
release is a draft until the releaser publishes it. Linux ships only a `.deb` and an `.rpm`
([ADR](2026-11-07-linux-ships-only-the-deb-and-the-rpm.md)). CLI and desktop releases share one
repository, and so share GitHub's one "latest release".

## Decision

**Every build trusts two minisign keys: the current one and a standby.** Each is made with
`tauri signer generate` and protected by a password. The current public key is
`plugins.updater.pubkey` in `src-tauri/tauri.conf.json`; the standby's is
`src-tauri/updater-standby.json`. The current private key and its password are the `production`
environment's secrets `TAURI_SIGNING_PRIVATE_KEY` and `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`. A key
retired by a rotation can sit beside them as `TAURI_SIGNING_PRIVATE_KEY_PREVIOUS` and its password.
**The standby private key is never a secret**: it is kept offline, apart from the current key's
offline copy, and signs nothing until a rotation promotes it. **Only `release-desktop.yml`'s
`sign-updater` job reads the secrets.** That job checks out the release tag, downloads the
bundles, and runs `scripts/sign-updater.sh`, which fetches the pinned `cargo-tauri` by SHA-256 and
nothing else. Who can use a key is who can run a job in `production`: the environment's protection
rules are the boundary, and they live in the repository settings, not in code.

**What is signed is what is installed.** The macOS bundle job archives the signed, notarized
`Kubetail.app` in Tauri's layout (`kubetail-desktop-<version>-darwin-universal.app.tar.gz`); the MSIs are
signed after SignPath has signed them. Each signature carries its version (`--app-version`), and
the app sets `requireSignedVersion`, so a manifest cannot pair a new version number with an older
signed artifact.

**Each key has its own manifest**, `latest-<key id>.json`, unsigned, attached to each desktop
release and covered by `SHA256SUMS`. The plugin verifies against one key per check, so the host
runs one check per key, the current one first, and offers the first update found; the update is
verified against the key whose manifest named it. The standby's manifest is absent until it is
promoted, which the check reads as no update. The script signs with every key in the secrets,
writes each key's manifest, and fails when the current key is not the committed public key, when
the standby is unset or equals a signing key, or when an endpoint does not name its key's id.

**The app reads the manifests from a host the project controls**:
`https://updates.kubetail.com/desktop/stable/latest-<id>.json` for each key it was built with.
Publishing a desktop final copies its manifests there; a pre-release's are never copied, so the
updater offers finals only. The manifest's download URLs still name the release's files on GitHub.
The path's `desktop/` and `stable/` leave room for another product's manifests and a pre-release
channel without a new URL scheme. An installed app reads its endpoint forever, so the endpoint
names no hosting vendor: the host behind `updates.kubetail.com` can change without a release.

**Rotation promotes the standby.** A rotation commits the standby's public key and endpoint as the
current ones and a new standby in its place, and moves the promoted private key into the secrets.
Every app built since the rotation before it already trusts the promoted key, so it updates through
that key's manifest with no release signed by the old key. A planned rotation and an emergency are the same
steps; only the old key's fate differs. A lost or compromised current key strands no app: releases
stop signing with it, and the apps move on the standby. A thief holding it can still sign for the
apps that have not updated, if they can also serve a manifest, which means writing to
`updates.kubetail.com` or breaking TLS to it. An app two rotations behind trusts neither
key in service; keeping the retired key as the previous key serves it, and retiring that is a
choice. The procedures are in [`docs/updater-key.md`](../updater-key.md).

**Linux applies no update.** The plugin is not built there. The `.deb` and `.rpm` belong to the
system's package manager; replacing them from inside the app would need root through `pkexec` and
would bypass the user's package tooling. H-4a's notice, which shares the manifest, covers Linux.

**The host checks and installs; the webview is granted nothing.** In a release build, on macOS and
Windows, `updater/platform.rs` checks at launch and every 24 hours and offers what it finds as a
tray item. The user's click downloads, verifies and installs it, stops the sidecar as a quit does
(`stop_for_install`, `lib.rs`) and restarts, so the old sidecar stops before the new app starts and
only one process migrates the stores. The stop runs before the restart is requested, because Tauri
lets nothing prevent a restart's `ExitRequested`. On Windows the plugin ends the process itself
after starting `msiexec`, so its `on_before_exit` runs the same stop.

**What an interrupted install leaves**, read from the plugin's source:

- The download is held in memory and verified before anything is written. An interrupted or
  refused download changes nothing.
- macOS extracts into a temp directory on the app's volume, then exchanges it with the installed
  bundle in one `renamex_np(RENAME_SWAP)`. Without the swap (not APFS) it renames twice and
  restores on failure; a crash between the two renames leaves the old app in a temp directory. An
  install that cannot restore keeps the old bundle and names its path in the error.
- Windows hands the MSI to `msiexec /passive`. Windows Installer rolls a failed or cancelled install
  back to the old version. A refused UAC prompt leaves the old version installed and the app closed.

## Alternatives considered

- **Let `tauri build` sign the updater artifacts.** The key would sit on the build runners beside
  `node_modules`, and the Windows signature would cover the MSI before SignPath changed it.
- **Apply updates on Linux** with the plugin's `pkexec dpkg -i` / `rpm -U`. It works, but asks for
  root from a GUI to replace a package the system owns, and a repository (apt, dnf) is the native
  update path when Linux gets one.
- **One key at a time, a manifest per key.** A planned rotation works by signing each release with
  the old and the new key until the old one retires, but a lost key strands every app built with
  it, and a stolen one must keep signing to move its apps off.
- **One `latest.json` and a single key at a time.** Rotation would then need a release signed by
  the old key that carries the new one, and an app older than it could update only while it was the
  latest, so the releaser would hold back every later release to keep that window open.
- **Two current keys and a standby.** An app needs one signature it can verify; the standby
  already carries apps across a rotation, so a second current key adds a secret and a manifest
  and covers nothing more.
- **Manifests numbered by slot**, the app trying each. The plugin takes the first manifest it can
  fetch and verifies only at download, so an app would offer updates it then refuses; and a number
  can be paired with the wrong key, which the key id cannot.
- **The latest release's files** (`releases/latest/download/latest-<id>.json`). GitHub has one
  latest release per repository, which the CLI's releases, and later the mobile app's, share. A
  release of another product made latest would stop every app's updates with no error, since a
  missing manifest reads as no update, and the URL would tie installed apps to GitHub for good.
- **A path on `kubetail.com`** (`kubetail.com/updates/...`). It ties the endpoint to however the website
  is built and hosted, and gives the website's deploys the power to withhold updates.
- **Install without asking.** The restart stops every running chat and watch; the user chooses when.

## Consequences

- The `production` environment's rules now guard code execution on every installed app. Restrict
  who and which refs can deploy to it.
- Losing the current key promotes the standby. Losing both, or both passwords, ends in-app updates
  for every installed copy, so their offline copies are kept apart.
- The manifests, `latest-<key id>.json`, are the release files not named
  `kubetail-desktop-<version>-…`: installed apps read them by name, so the name cannot change.
- Each check asks `updates.kubetail.com` for two manifests, the standby's a 404 until it is
  promoted.
- Whoever can write to `updates.kubetail.com` can withhold an update or leave a stale one, though not
  install unsigned code. Its write access belongs to the publish workflow alone.
- Until the publish workflow and the host exist, no installed app sees an update. They must exist
  before the second desktop release with the updater is published ([TODO](../TODO.md#security)).
- A rotation can keep a retired private key in `production` for apps two rotations behind.
- The macOS archive is made by `tar`, not Tauri; its layout must keep the `.app` as the archive's
  one top-level entry.
- Bumping `tauri-plugin-updater` means rereading what it guarantees on an interrupted install, and
  bumping the Tauri CLI means updating `scripts/sign-updater.sh`'s pin.

## Revisit when

Linux gains a package repository, or the release moves off GitHub Releases.
