---
title: Persist app settings in host-owned host.json, not webview localStorage
date: 2026-08-09
scope: cross-cutting
status: Accepted
amended_by:
  - [Keep host.json in the Kubetail config directory](2026-11-21-host-json-lives-under-the-kubetail-config-directory.md)
  - [A reload reads the latest host.json from the window's sessionStorage](2026-11-26-a-reload-reads-the-latest-host-json-from-session-storage.md)
---

# Persist app settings in host-owned host.json, not webview localStorage

## Context

The first persisted setting was the color-scheme preference. It must be readable before any
page script runs (first-paint theming — see
[ADR: first-paint theming](2026-08-09-first-paint-theming.md)), consistent across multiple
windows, and readable by the *host* itself, which paints each window's native background at
creation — before any webview exists to consult.

## Decision

Settings live in `host.json` (`app_config_dir()/host.json`), owned by the host
(`src-tauri/src/host_file.rs`): a single versioned JSON object with optional camelCase fields,
defensive reads (missing/corrupt → defaults), atomic writes (temp file + rename). Updates are
general partial patches (`HostFilePatch`, all-`Option`, merged then version-stamped) through
one `update_host_file` command — adding a setting extends the structs, not the command list.

The file reaches the webview two ways from one read in `build_window`: injected as
`window.__KUBETAIL_HOST__` via an `initialization_script` before any page script (synchronous
first-paint reads), and broadcast as `host-file-updated` after every write so open windows
track changes live. On the webview side `src/lib/host-file.ts` owns the whole protocol
(`readInjectedHostFile` / `updateHostFile` / `subscribeHostFile`); feature modules like
`theme.tsx` pick their field out of it.

## Alternatives considered

**localStorage.** Rejected: it is per-webview state the host cannot read — the host needs the
preference to paint the native window background pre-webview — and multi-window consistency
would need a hand-rolled broadcast anyway. It is also invisible to the injection path that
makes first paint synchronous.

**One Tauri command per setting.** Rejected: the command list grows per setting and every
consumer re-implements read/subscribe plumbing. The patch-shaped single command keeps the
protocol fixed while the schema grows.

**tauri-plugin-store.** Rejected in favor of a hand-rolled ~200-line module: the plugin gives
neither the injection-before-page-script path nor the merged-broadcast semantics, which are
the actual requirements; persistence itself is the easy part.

## Consequences

One file, one protocol, three flows (boot injection, optimistic write-through, broadcast
sync). The host stays authoritative; the webview never caches settings elsewhere. The
maintenance rule: the module is persistence only — how a setting is *used* lives with its
consumer (e.g. `window_manager`'s `background_color_for`). New persisted settings add fields
to `HostFile`/`HostFilePatch` and a picker in the consumer, nothing else.
