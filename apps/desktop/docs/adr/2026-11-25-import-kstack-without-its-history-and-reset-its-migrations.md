---
title: Import Kstack without its history and reset its migrations
date: 2026-11-25
scope: repo
status: Accepted
---

# Import Kstack without its history and reset its migrations

## Context

The desktop app was built in `kstackhq/kstack` as Kstack Desktop. In November 2026 it moved into `kubetail-org/kubetail` as Kubetail Desktop, at `apps/desktop/`, built on `wip/desktop` in three steps: the rename in Kstack, the import, and the CI and release harness. Kstack's only releases were `desktop/v0.0.1` and its release candidates. Nothing installed needed a migration path, and no code outside the app read its names.

## Decision

**One import commit, no history.** The rename merged into Kstack's `main`, which froze. The import commit (#1570) copies that tree into `apps/desktop/` and names its source, `kstackhq/kstack@c024285` with the rename applied. Kstack's open PRs are reopened here; `kstackhq/kstack` is archived, not deleted.

**Every license header is `Copyright 2024 The Kubetail Authors`**, as `scripts/check-license-headers.sh` expects. `LICENSE` stays Apache 2.0.

**Contracts change outright.** Nothing reads the Kstack names, so no code cleans up after `desktop/v0.0.1`:

| What | Name |
| --- | --- |
| Brand | Kubetail Desktop |
| `productName` | `Kubetail` (`Kubetail.app`, `PFiles/Kubetail/`); `kubetail-desktop` on Linux |
| Tauri identifier | `com.kubetail.desktop` |
| Binaries | `kubetail-desktop`, `kubetail-sidecar` |
| deb and rpm packages, desktop entry | `kubetail-desktop`, the entry showing `Name=Kubetail` |
| Linux helpers and AppArmor profiles | `/usr/lib/kubetail-desktop/`, `kubetail-desktop-bwrap`, `kubetail-desktop-pasta` |
| Data and cache folders | `Kubetail` on macOS and Windows, `kubetail` on Linux; nothing shared with the CLI's `~/.kubetail` |
| Environment variables and GraphQL error codes | `KUBETAIL_*`, shared with the other components; `KSTACK_HOST_PID` became `KUBETAIL_DESKTOP_PID` |
| The webview's settings global | `window.__KUBETAIL_HOST__` |
| Endpoints | `updates.kubetail.com`, `oauth.kubetail.com`, `app.kubetail.com`, `hello@kubetail.com` |
| Tags | `desktop/v<semver>`, starting at `desktop/v0.1.0`; Kstack's tags are not carried over |

**The sidecar reserves only the variables it sets**: `KUBETAIL`, `KUBETAIL_SIDECAR_PID` and `KUBETAIL_DESKTOP_PID`. Kstack reserved the whole `KSTACK` prefix; renamed, that prefix would claim the CLI's own variables.

**Linux takes its own `productName`, `kubetail-desktop`**, in `tauri.linux.conf.json`. Tauri names the deb's `Package:` and the rpm's `Name:` after `productName` and has no setting for either. A `desktopTemplate` keeps the entry's `Name=Kubetail`. This replaces the part of [Release files are named kubetail-\<product\>-\<version\>-\<os\>-\<arch\>](2026-11-08-release-files-are-named-kubetail-product-version-os-arch.md) that kept the packages' name `kubetail`.

**The SQL migrations reset.** Each schema's migrations collapse into one `0001_init.sql`, and `persisted-data.md` clears its shipped marks. [A shipped schema changes only by a new migration](2026-11-08-a-shipped-schema-changes-only-by-a-new-migration.md) applies again from `desktop/v0.1.0`.

**Accepted ADRs were rewritten once**, with `kstack` renamed in their file names and text. Superseded ADRs, and those this repo's `docs/adr/` already holds, were dropped. Two that hold for the whole repo moved to `docs/adr/`.

## Alternatives considered

**Import with history** (`git subtree` or a merge of unrelated histories). It would carry Kstack's commits, tags and old names into `git log` and `git blame` here, for a repo that stays readable in its archive.

**Keep the Kstack names behind aliases** for the endpoints, folders and environment variables. Only pre-1.0 testers ever ran Kstack; migration code would guard nothing and stay forever.

**Keep `kubetail` as the Linux package name.** It holds the name a packaged CLI would take, and `apt remove kubetail` would remove the desktop app.

**Keep the migration history.** Its steps exist only to upgrade Kstack databases, which no Kubetail Desktop install holds.

## Consequences

A Kstack install and a Kubetail Desktop install are separate apps with separate data. `git log` here starts the app at the import commit; its earlier history is in the archived `kstackhq/kstack`, which the desktop docs link for old issue and PR numbers.

The CLI keeps the `kubetail` package name for the deb and rpm channels.
