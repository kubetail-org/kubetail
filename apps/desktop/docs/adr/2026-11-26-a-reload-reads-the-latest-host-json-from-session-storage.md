---
title: A reload reads the latest host.json from the window's sessionStorage
date: 2026-11-26
scope: cross-cutting
status: Accepted
amends:
  - [Persist app settings in host-owned host.json, not webview localStorage](2026-08-09-host-json-settings.md)
  - [Class-based color scheme with pre-paint inline script and host-painted native background](2026-08-09-first-paint-theming.md)
---

# A reload reads the latest host.json from the window's sessionStorage

## Context

`build_window` reads `host.json` once and bakes that snapshot into the webview's initialization script. The script reruns on every load, but its text is fixed when the window is built. A reload after a settings change (Cmd/Ctrl+R, or the inspector's) painted the old color scheme, and `ThemeProvider` seeded itself from the same stale copy. On the opaque platforms the native background also kept its build-time color.

The first-paint read must stay synchronous. Tauri cannot change a window's initialization script after the build, and the page-load event races the document's own scripts.

## Decision

After every write, `update_host_file` calls `window_manager::host_file_written`, which runs `host_file::update_script` in every open window. The script sets `window.__KUBETAIL_HOST__` to the merged file and stores it in `sessionStorage` under `kubetail:host-file`. The initialization script reads that key first and falls back to its build-time snapshot when the key is absent or holds anything but a JSON object. `sessionStorage` survives a reload and dies with the window, so a new window reads the file from disk as before.

On macOS and Windows `host_file_written` also sets each window's native background to the new scheme (`background_color_for`).

A write can land while a window is loading: the script runs in the outgoing document or not at all, and the new page has no listener yet for the broadcast. So `subscribeHostFile` calls the `read_host_file` command once its listener is registered. The host reads the file from disk, brings the calling window up to it the same way (`window_manager::send_host_file`) and returns it. The webview drops the answer if a broadcast arrived first.

The host writes the stored copy, and only from a file it has just persisted: the webview's own code never writes it. `host.json` stays the source of truth.

## Alternatives considered

**Evaluate the latest file on the page-load event.** The event fires as a navigation commits, and the evaluation races the inline script in `index.html`, so a reload could still paint the old scheme.

**A synchronous XHR from the initialization script to a custom protocol.** It would read the file itself, but each engine's support for synchronous loads from a custom scheme differs, and it blocks first paint on a round trip to the host.

**A cookie the host sets.** WKWebView exposes no `document.cookie` on a custom scheme.

**Validate each field in the initialization script.** Every setting would need a third hand-written copy of its rules, in a script string. The script checks only that the stored value is an object; each setting's module validates its own field.

**Have `host-file.ts` cache the broadcast.** The same storage, but written by webview code, which the host.json ADR keeps out of settings.

## Consequences

A reload paints the scheme last written, on every platform. The costs: anything running in the webview can write the stored copy, so a field read before first paint must stay display-only and be validated by its consumer, as `colorSchemePreference` is. A window that misses the update while loading paints its old copy once, then corrects itself and its stored copy from `read_host_file`. `SESSION_KEY` in `host_file.rs` names the key both scripts use.
