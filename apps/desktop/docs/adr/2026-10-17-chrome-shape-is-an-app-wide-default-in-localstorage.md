---
title: Keep the chrome's shape as an app-wide default in localStorage
date: 2026-10-17
scope: frontend
status: Accepted
---

# Keep the chrome's shape as an app-wide default in localStorage

## Context

This decision predates this file; it was recorded in the root `CLAUDE.md` and moved here when
that file was compacted.

Every Kubetail window's webview shares one `localStorage`, and each has its own `sessionStorage`.
The chrome has shape the user adjusts — sidebar widths, whether a pane is open — and a webview
has its own history stack. User settings already go to the host's `host.json`
(→ [host.json settings](2026-08-09-host-json-settings.md)).

## Decision

`usePersistedWidth` (`src/lib/persisted-width.ts`) and `usePersistedFlag`
(`src/lib/persisted-flag.ts`) write the chrome's shape to `localStorage` on every drag and toggle,
with no live sync between windows. Resizing one window leaves the others alone, and the next
window opens the way the last one was left. The history ceiling (`history-nav.tsx`) lives in
`sessionStorage`, since it describes one webview's own stack. Every key is formed by `storageKey`
(`src/lib/storage-key.ts`): `kubetail:`-prefixed, kebab-case, apart from what `@kubetail/ui`
persists.

## Alternatives considered

**Per-webview widths (`sessionStorage`).** The sidebars belong to the window's chrome, not to
what it shows, and tabs will be webviews of their own. Per-webview widths would have two tabs of
one window disagreeing about the sidebar.

## Consequences

Chrome shape is a default, not window state: it is not in the URL and not restored per window.
A setting the user picks in the Settings dialog never goes here.
