---
title: Linux ships only the .deb and the .rpm
date: 2026-11-07
scope: repo
status: Accepted
---

# Linux ships only the .deb and the .rpm

## Context

A desktop release attached a `.deb`, an `.rpm` and an AppImage for each Linux architecture
([Kubetail ships its own bwrap](2026-09-28-kubetail-ships-its-own-bwrap.md),
[Kubetail ships its own pasta](2026-10-04-kubetail-ships-its-own-pasta.md)).

The AppImage's bwrap and pasta sit under a mount no AppArmor profile names, so on Ubuntu 23.10 and
24.04 it has no sandbox and no network.

## Decision

Linux ships the `.deb` and the `.rpm` alone. `release-desktop.yml` builds `--bundles deb`, then the
RPM, and `tauri.linux.conf.json` configures no AppImage.

This replaces the AppImage in both ADRs above.

## Alternatives considered

- **Keep the AppImage.** It runs without admin rights on any distro, but on Ubuntu 23.10 and 24.04
  it runs with no sandbox and no network, and nothing tells a user to pick a package instead.

## Consequences

Ubuntu 23.10 and 24.04 users have no build without a sandbox to pick by mistake. Installing Kubetail
on Linux needs admin rights. `kubetail app` on Linux still installs an AppImage, and cannot install
from a real release until it installs a package (`docs/TODO.md`, *CLI*).
