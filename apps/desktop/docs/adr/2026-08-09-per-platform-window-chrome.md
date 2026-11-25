---
title: Per-platform window chrome — native macOS, frameless-opaque Windows, frameless-transparent Linux
date: 2026-08-09
scope: host
status: Accepted
---

# Per-platform window chrome — native macOS, frameless-opaque Windows, frameless-transparent Linux

## Context

The webview's app bar (`src/components/widgets/app-bar.tsx`) is the title bar, so every window
is chromeless. But the three platforms draw borders, shadows, and corners differently for a
decoration-less window, and each has a different failure mode if treated alike. All chrome decisions live in
one place: `build_window` in `src-tauri/src/window_manager.rs` — `tauri.conf.json` declares
no windows (a unit test pins this), so there is no config/code split to keep in sync.

## Decision

- **macOS**: native decorations kept, Overlay title bar (traffic lights kept, title hidden),
  lights repositioned into the app bar via `traffic_light_position`. Opaque — a
  transparent macOS window changes how the OS draws the border/shadow (it reads darker).
- **Windows**: frameless (`decorations(false)`) but **opaque** — DWM still draws a borderless
  window's own shadow (and rounds corners on Win11), so a custom shadow would double-stack.
- **Linux**: frameless **and** transparent — the OS draws neither border nor shadow, so the
  webview's `WindowFrame` (`src/components/widgets/window-frame.tsx`) paints its own into a
  transparent gutter: inset, rounded corners, thin border, soft shadow, `contain: paint` so
  fixed-position chrome anchors to the frame. Passthrough on macOS/Windows. Collapses to
  full-bleed when maximized (no off-window space for a gutter), like native GTK apps.

Dropping decorations drops native resize borders on Linux and Windows, so
`WindowResizeHandles` renders invisible fixed edge/corner grips that start a native resize
drag via Tauri's `startResizeDragging`. It is a **sibling** of `WindowFrame`, not a child:
the frame's `contain: paint` would re-anchor its `position: fixed` grips to the inset box and
clip them.

New windows **cascade** 28px down-right of their anchor (the focused window, else the last
built), restarting from center when the step wouldn't fit the work area — one fit check that
both bounds the walk and absorbs a mostly-off-screen anchor. Set explicitly on all three
platforms: AppKit auto-cascades an unpositioned window, but Linux/Windows pile up.

## Alternatives considered

**One uniform frameless-transparent treatment.** Rejected: double shadow on Windows, darker
native chrome on macOS, and losing free native traffic-light behavior.

**CSS-only resize affordances.** No such API exists; resizing must be a native drag, hence
the grip components invoking `startResizeDragging` (gated by
`core:window:allow-start-resize-dragging`).

**`tauri-plugin-positioner` for placement.** Rejected: it only snaps to fixed anchors, which
is exactly the pile-up cascade avoids.

**Windows declared in `tauri.conf.json`.** Rejected: the first window would take a different
code path than later ones, splitting per-platform chrome across config and code.

## Consequences

Per-platform behavior concentrates in `build_window` plus two webview widgets. Obligations:
the traffic-light pixel constants (`APP_BAR_HEIGHT`, 56px) stay in sync with `app-bar.tsx`; the pure helpers
(`traffic_light_position`, `background_color_for`, `cascade_position`) stay free functions
compiled on every platform so CI covers them; full-height screens use
`min-h-[var(--app-min-h)]`, never `min-h-svh`, because the Linux frame insets the app.
