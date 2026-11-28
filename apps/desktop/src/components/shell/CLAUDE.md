# src/components/shell — the app's frame

The app bar, both sidebars, the window frame and its controls, the startup gate and `layouts/app-layout.tsx`. Also owns `src/lib/`'s `window-drag.ts`, `focus-trap.ts`, `persisted-width.ts` and `right-sidebar.tsx`. **`context-omnibox/` is described in the cluster guide** ([`../cluster/CLAUDE.md`](../cluster/CLAUDE.md)): open it before changing the omnibox.

## Startup

**`ReadyGate` draws `AppMark`** at 48px, centred, in every state, with a 56px `data-tauri-drag-region` strip over its top edge so the window drags before the app bar mounts. `index.html` carries a static copy of the mark and the strip inside `#root`, so first paint and React's first commit show the same thing in the same place — keep them matching. The icons come from `src-icons/` (`apps/desktop/CLAUDE.md`).

## Window layout

`layouts/app-layout.tsx` is the one place the window's shape is expressed: a 56px app bar over an inset box, the content row, holding the left sidebar, `<main>` and the right sidebar. Nothing is fixed-positioned. Traps:

- **The app bar is a `deep` Tauri drag region**; controls inside stay clickable via Tauri's hit test, and the omnibox opts out with `data-tauri-drag-region="false"`. A press that drags never reaches the webview as a click, and Base UI's popovers dismiss on an outside click, so the bar's `onPointerDown` (`dismissPopupsOnWindowDrag`, `src/lib/window-drag.ts`) clicks the pressed element itself. `startsWindowDrag` there mirrors Tauri's drag hit test; diff it against `drag.js` on a Tauri upgrade.
- On macOS `TrafficLightGutter` (76px, shared with the log window's bar) reserves the native lights; `window_manager.rs`, the dialogs' drag band and both boot strips mirror the 56px. **Only the two bars check the platform** (this one and `log-window/bar.tsx`) — not mounting `AppMenu` on macOS keeps its `Ctrl/Cmd+N`/`Q` from firing beside the native menu's. Bar tokens are `--app-bar*` in `index.css`.
- **The left sidebar**: the library's `SidebarProvider` owns `Cmd/Ctrl+B`; open/collapsed is controlled from the layout via `usePersistedFlag`, since the library's `sidebar_state` cookie is never read back. Open is its stored width, capped to fit the row (`leftSidebarRoom`); its contents keep the open width, clipped and `inert` when closed. **Narrowness is the provider's own `isMobile`, never a second media query** — `toggleSidebar` branches on it to flip `open` or `openMobile`, so a component using another rule flips a state nothing shows. Narrow, the panel opens as a drawer over the content row. The controls at its top wear `SIDEBAR_CONTROL_CLASS` (`src/lib/control-style.ts`).
- **The drawer is `role="dialog" aria-modal`, held by `useFocusTrap`** (`src/lib/focus-trap.ts`). Both yield to an overlay opened inside it: the trap stands aside while its node sits under an `aria-hidden="true"` element, and the drawer's Escape listener is on `window`, after the `document` listener a dialog dismisses on, so one keypress closes the dialog alone.
- **The right sidebar** (`right-sidebar.tsx`) is a row item, taking width from the page. A drag stops where `<main>` reaches its floor, `min(320px, 50%)` of the row (`PAGE_MIN_WIDTH`); on a narrower window it shrinks to its minimum first, leaving the stored width alone. Its state is `useRightSidebar` (`src/lib/right-sidebar.tsx`), persisted **per mode** (open state and width); `usePersistedWidth` re-reads when its key changes. **The `aside` does not scroll**; its contents own a scroller each.

## The bell

**The bell** (`app-bar/notification-button.tsx`) lists every target's open incidents, newest first, and counts them on the button. Pressing one opens the Incidents dialog on it, in this window; *View all* opens it on none. With none open it says so, above the line of each target that is off or cannot be read. Its reads are `IncidentsProvider`'s ([`../monitoring/CLAUDE.md`](../monitoring/CLAUDE.md)).

## Window frame

**The window frame is per-platform** (macOS native / Windows frameless-opaque / Linux frameless-transparent). `WindowFrame` (`components/primitives/window-frame.tsx`, at `__root.tsx`) paints Linux's border and shadow. `WindowResizeHandles` is its **sibling**, never a child — the frame's `contain: paint` would clip it. `main.tsx` adds `<html class="frameless">` on Linux only. **Full-height screens use `min-h-(--app-min-h)`, never `min-h-svh`.** Maximized Linux goes full-bleed (`useWindowMaximized`). → [ADR: per-platform window chrome](../../../docs/adr/2026-08-09-per-platform-window-chrome.md).

Off macOS there is no native menu bar: `AppMenu` (`app-bar/menu.tsx`) owns `Ctrl/Cmd+N`/`Ctrl/Cmd+Q`, and `WindowControls` draws the caption buttons. They and the omnibox wear `APP_BAR_HOVER_CLASS` from `src/lib/control-style.ts`: the app bar imports `window-controls.tsx` and `context-omnibox/`, so neither imports from `app-bar/`.
