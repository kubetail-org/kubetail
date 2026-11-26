---
title: Keep host.json in the Kubetail config directory
date: 2026-11-21
scope: host
status: Accepted
amended_by:
  - [Each folder kind has its own folder on every platform](2026-11-22-each-folder-kind-has-its-own-folder-on-every-platform.md)
  - [The sidecar places Kubetail's folders itself](2026-11-23-the-sidecar-places-kubetails-folders-itself.md)
---

# Keep host.json in the Kubetail config directory

## Context

`host.json` was the one file Kubetail kept outside its `Kubetail` / `Kubetail-dev` directories. The
[host.json ADR](2026-08-09-host-json-settings.md) put it in Tauri's `app_config_dir`, which is keyed
by the bundle id `com.kubetail.desktop` and has no dev variant. A dev build and an installed release
therefore read and wrote the same file, and `make clean-data` did not reach it. On macOS Kubetail had
two directories under `Application Support`. The [directory ADR](2026-09-27-kubetails-files-follow-the-platforms-directory-kinds.md)
did not mention the file. `desktop/v0.0.1` wrote it there, but the app is not public yet.

## Decision

`paths.rs` gains `app_config_dir`: the platform config root with the `Kubetail` / `Kubetail-dev` leaf,
created owner-only. That is `~/Library/Application Support/Kubetail` on macOS (the data directory),
`%APPDATA%\Kubetail` on Windows and `$XDG_CONFIG_HOME/Kubetail` on Linux. `host.json` lives there.

`host_file::manage` resolves the path once in `setup`, before the first window, and manages it as
`host_file::Location`. Each write makes the directory owner-only again, unless it is a link, and the
file `0600`. The host passes the directory's path to the sidecar, unconverted, as `--config-dir`, so
the file tools and the sandbox deny it with Kubetail's other directories. Nothing copies the bundle-id
file across: the app is not public, so no user has a color scheme to keep.

`make clean-data` deletes the dev build's config directory with its data and cache directories, so
it resets the dev build's settings too.

## Alternatives considered

- **Put `host.json` in the data directory.** Windows keeps user settings in the roaming `%APPDATA%`
  and Linux in `$XDG_CONFIG_HOME`; the data directory is `%LOCALAPPDATA%` and `$XDG_DATA_HOME`.
- **Copy the bundle-id file across on first launch.** It would keep a color scheme no user outside
  the team has, at the cost of code that runs on every launch.
- **Leave `make clean-data` short of the config directory.** A dev build's first-run settings could
  then only be reached by hand on Linux and Windows, while macOS cleared them with the data.

## Consequences

- A dev build and a release each keep their own `host.json`.
- An install of `desktop/v0.0.1` starts with the default color scheme. Its old file stays in the
  bundle-id directory, which nothing reads.
- A config directory that the platform cannot name or that cannot be made leaves `host.json` with
  no location: windows paint the defaults and a change fails, but the app starts. The sidecar still
  fences any directory the platform named, made or not; it gets no `--config-dir` only when there is
  no name.
