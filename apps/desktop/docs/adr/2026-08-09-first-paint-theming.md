---
title: Class-based color scheme with pre-paint inline script and host-painted native background
date: 2026-08-09
scope: cross-cutting
status: Accepted
---

# Class-based color scheme with pre-paint inline script and host-painted native background

## Context

A dark-mode user opening a window must never see a white flash. Two distinct gaps can flash:
the native window's first frame before the webview draws anything, and the document's first
paint before React hydrates and applies theming. The color scheme is a user preference
(`system`/`light`/`dark`) persisted in `host.json`
([ADR: host.json settings](2026-08-09-host-json-settings.md)).

## Decision

Two mechanisms, one per gap — deliberately independent:

**Host side** (`src-tauri/src/window_manager.rs`): `build_window` reads `host.json` once per
window and, on the opaque platforms, sets the native `background_color` to the resolved
`--background` token (`background_color_for`), so the window's first frame is themed before
the webview draws. There is **no reveal step** — no `visible(false)`, no `show_window`
command, no page-load handler, no fallback timer; the webview must not drive when its window
appears. On macOS this only works with the **`tauri/macos-private-api`** feature: it enables
`wry/transparent`, which clears WKWebView's opaque white backing (the private
`drawsBackground` KVC key) — without it macOS flashes white no matter what the native
background is. We don't ship through the Mac App Store, so the private-API prohibition doesn't
apply. Two consequences: `background_color` must always be passed (wry only clears the
backing when a color is set — `os_theme.rs` exists to resolve `system` pre-window), and the
document must be opaque (`index.css` paints `html` from the same token off-Linux).
`os_theme.rs` is hand-rolled rather than the `dark-light` crate, which does the same two OS
reads but pins objc2 0.5 against this crate's 0.6; every failure path degrades to light —
one frame of the wrong color, never white.

**Webview side** (`src/lib/theme.tsx`): the resolved scheme toggles a `.dark` class on
`<html>` that Tailwind's `dark:` variant keys on. An inline script in `index.html`
(hand-mirroring `resolveColorScheme`) reads `window.__KUBETAIL_HOST__` and applies `.dark`
before first paint; `ThemeProvider` initializes from the same global — no async reconcile.
Changes apply optimistically and write through `update_host_file`; the broadcast keeps other
windows in step.

"Theme" (a named skin within a scheme, e.g. "github-light") is reserved vocabulary for a
future axis — `resolveColorScheme` is the seam; not built.

## Alternatives considered

**A reveal step (create hidden, show on page load).** Rejected: it makes window appearance
depend on webview startup — a hung or slow renderer means an invisible window — and still
needs a fallback timer, which reintroduces the flash it was meant to fix.

**Media-query theming (`prefers-color-scheme`) without the class.** Rejected: cannot express
an explicit user override of the OS scheme.

**Letting React apply the class after mount.** Rejected: guarantees one unthemed paint. The
inline script duplicates ~10 lines of resolve logic; that duplication is the accepted cost and
must be kept in sync with `resolveColorScheme`.

## Consequences

First frames are themed on all platforms with zero flash and zero reveal machinery. The
obligations: keep the inline script mirroring `resolveColorScheme`; always pass a native
background color on opaque platforms; never reintroduce a reveal step; keep
`app.macOSPrivateApi` in `tauri.conf.json` mirroring the Cargo feature (the CLI reads the
config to compute features).
