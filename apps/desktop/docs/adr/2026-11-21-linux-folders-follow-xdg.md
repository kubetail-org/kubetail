---
title: Linux folders follow XDG
date: 2026-11-21
scope: host
status: Accepted
amends: [Keep each file in the platform's directory for its kind](2026-09-27-kubetails-files-follow-the-platforms-directory-kinds.md)
amended_by:
  - [Each folder kind has its own folder on every platform](2026-11-22-each-folder-kind-has-its-own-folder-on-every-platform.md)
  - [The sidecar places Kubetail's folders itself](2026-11-23-the-sidecar-places-kubetails-folders-itself.md)
---

# Linux folders follow XDG

## Context

Kubetail named its Linux folders after the product, `~/.local/share/Kubetail`, `~/.config/Kubetail` and
`~/.cache/Kubetail` (`Kubetail-dev` for a dev build). Linux tools name theirs in lowercase. It also kept
its logs and `window-state.json` in the data folder, while XDG gives per-machine state its own root,
`$XDG_STATE_HOME` (`~/.local/state`).

The webview's profile, which holds `localStorage`, sat under the bundle id
(`~/.local/share/com.kubetail.desktop`, and `%LOCALAPPDATA%\com.kubetail.desktop` on Windows), shared by a
dev build and a release. No other well-known app names a folder by bundle id on either platform.

`desktop/v0.0.1` used the old folders, but it reached no users.

## Decision

On Linux each folder takes a lowercase leaf, `kubetail` or `kubetail-dev`. macOS and Windows keep
`Kubetail` and `Kubetail-dev`.

Linux gains a state folder, `$XDG_STATE_HOME/kubetail` (when absolute, else `~/.local/state/kubetail`).
It holds `window-state.json`, and the host's and the sidecar's logs in a `logs` folder, as on
Windows. The host hands the sidecar the state folder (`--state-dir`), which fences the whole of it.
On macOS and Windows state is the data folder, and the logs stay where they were.

On Linux and Windows the webview's profile is `webview` in the state folder
(`~/.local/state/kubetail/webview`, `%LOCALAPPDATA%\Kubetail\webview`), so each build has its own and the
sidecar's fence covers it. macOS's WKWebView takes no path, so its profile stays under the bundle id.

Nothing moves what the old folders hold.

## Alternatives considered

- **Move the old folders at launch.** Moving the data folder would break the absolute paths a
  transcript names, so an upgraded install would keep it for good. With no installs to upgrade, that
  is code and a second spelling of the data folder for nobody.
- **Put the webview's profile in the data folder on Linux.** The data folder is the sidecar's, and
  the profile is the host's view state.

## Consequences

- A `desktop/v0.0.1` install starts empty on Linux, and on Windows its webview starts with empty
  `localStorage`. Its old folders stay on disk until the user removes them.
- `make clean-data` deletes the dev build's lowercase folders and its state folder.
- A dev build and a release share `localStorage` on macOS alone.
- On Linux WebKitGTK keeps its cache in the profile, so that cache sits under the state folder.
