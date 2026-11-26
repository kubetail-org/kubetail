# src/components/logs — log views

The log panel, the log windows, the log viewer and its headless core, and the transcript's log cards (`components/chat/log-cards/`). Also owns `src/lib/`'s `logs-view.tsx`, `log-windows.tsx`, `log-window-push.ts`, `logs-client.ts`, `log-viewport.ts` and `log-viewer/`. How each card draws in the transcript is in the chat guide ([`../chat/CLAUDE.md`](../chat/CLAUDE.md), *Tool calls*).

## The focused view

- **The focused view is `useLogsView`** (`src/lib/logs-view.tsx`): `{ clusterId, chatId, callId, action, grep? } | null`, chrome, not persisted. `grep` is the user's from the viewer's toolbar, kept while the same call stays focused, so it survives the panel unmounting while the sidebar is collapsed.
- `set` also opens the right sidebar, so `LogsViewProvider` mounts inside `RightSidebarProvider`. The panel's close collapses the sidebar and leaves the view focused, like the toggle. A cluster switch from the picker clears it, and so does moving it to a log window.
- **A view opens on a transcript card's button alone**, never by itself. `viewOfCall` (`logs-view.tsx`) is the call → view mapping those buttons make.

## Log windows

**A view is in one place at a time**: `useViewPlace` (`log-cards/buttons.tsx`) decides each card's — a log window, then the sidebar while it is open and focused on the call, else the card. Only a LogsView card hides its inline lines while the view is elsewhere.

**`LogWindowsProvider`** (`src/lib/log-windows.tsx`, under `LogsViewProvider` in `AppLayout`) is the app window's side of the host's pair (`src-tauri/CLAUDE.md`, *Log windows*):

- It subscribes once per mount, each subscribe with a channel of its own (Tauri ends a channel when the host drops the side a subscribe handed it, which a later subscribe does), all folding through one handler. It keeps the **largest page** any subscribe answers, since two can land in either order.
- **Each window's viewport is a mirror kept out of React state** (`createMirror`, `src/lib/log-window-push.ts`): made when a `snapshot` or `opened` first names the window, folded from every push, never replaced while the window lives, and dropped on `closed` or a `snapshot` without it. A `viewport` frame re-renders only what reads the mirror. A mirror keeps its selection array while the push's generation and `selectionRevision` hold.
- `shownWindowOf(chatId)` is the chat's window with the highest `focusSeq`, with its mirror. It is `ready` once the page and the cluster are known; the window buttons are disabled until then.
- **Opens and closes go through one queue**, each after the last settles, since the host's open is async and its close is not. An open carries the focused view's grep when it is the same call, is dropped at its turn if its view is not on the held cluster, and on success clears the focused view if it is still that call, closing the sidebar if it showed it.
- **The held cluster** is the last defined `clusterID` since the page; the first is held with no close, and a later different one queues `log_window_close_all`. The windows are checked against the held cluster once per snapshot and once per page, and a window on another cluster queues a close too. An undefined `clusterID` changes nothing.

## The log window's page

`log-window/window.tsx` asks the host `log_window_view` once per mount, then reads the chat, its messages and the clusters.

- A reconnect masks a watch's data until its new bookmark, so the view drawn before it stays mounted through that, keeping the viewer's grep, scroll and selection.
- The grep is the user's from this window, else the open's, else the action's.
- **It pushes its viewport** (`createViewportPublisher`) through `log_window_viewport`, stamped with the answer's `generation` and the grep in effect, which the host keeps, so a reload keeps the toolbar's grep. The handle, the applied grep and the publisher live in `Paired`, above the viewer a notice unmounts.
- A push carries whole selected lines, at most 50 within 192 KiB of UTF-8 (`cutSelection`), the whole `selectedCount`, and a `selectionRevision` that steps on each new selection array. One is in flight at a time; a change to the selection, the count or the grep goes on the next frame, the rest after 250 ms, and `blur` sends at once. A refused push is reported and not resent.
- `LogsWindowBar` draws the chat's title and the cluster's name through `VisibleText`, and no `AppMenu`.

## The log viewer

`log-viewer/index.ts` exports what the transcript's cards share; nothing outside imports a part.

- **Its client is built once per mount**, in a `useState` initializer, by **`createLogsClient(urql, clusterId, action)`** (`src/lib/logs-client.ts`, the one place the app makes a `Client`). `logsFetch` runs with `requestPolicy: 'network-only'`, since a page's variables repeat. A tail the sidecar could not open calls `onFailed`.
- Cursors pass through as the sidecar wrote them. `scopeInput` builds the `LogsScopeInput` field by field, since the action carries `__typename`s. It lands at the action's `openAt` on mount only. A line's time reads through `lineTime` (`src/lib/log-viewer/line-time.ts`), in the viewer and the cards alike.
- **The toolbar's grep box replaces the action's grep** for the focused view. A submitted pattern is checked with a one-line read through a client built with it, since the sidecar compiles a grep before it reads. Accepted, the viewer swaps to that client and reports it through `onGrepApplied` and the viewport's `grep`, so the panel's header and the chat's snapshot name the grep in effect. Refused, the reason shows at the box and the view stays.
- The dot colour is inline (no CSS name built from cluster text). A line's `gap` notice sits in the same row (one more `LOG_ROW_PX` in `estimateRowHeight`), its names through `VisibleText`.
- **Keys go on the scroller, never `document`**, so a page can hold two viewers. Enter/Space on a cell are taken in the capture phase, before the scroller's intent reader would read Space as scroll-to-bottom. A refused copy goes to `reportError` as `render`.
- A failed page or follow is drawn, not reported: `errorReportExchange` has reported it.
- It writes what is on screen to the window's one **`LogViewportHandle`** (`src/lib/log-viewport.ts`), and the empty viewport on unmount.

## The viewer's library

**`src/lib/log-viewer/` is the viewer's headless core**; its `README.md` is the whole statement. → [ADR: a log read is a window over a range](../../../docs/adr/2026-10-23-a-log-read-is-a-window-over-a-range.md), [ADR: a bounded log window reads its dropped lines back](../../../docs/adr/2026-10-24-a-bounded-log-window-reads-dropped-lines-back.md).

- **Cursors are opaque**; a line whose cursor is held is a repeat, dropped.
- `LogWindow` (`window.ts`) is React-free. **It is bounded at `maxLines`**: a page past it drops lines at one edge, never on screen, and that edge reads them back from a *seam*. A dropped key comes back on another line, so the selection prunes to `firstKey`..`firstKey + count` in a layout effect. `window.test.ts` ends in a seeded walk over `FakeClient` that checks the rules after every step.
- `LogViewerCore` reads intent off wheel, key, pointer and touch events, never a scroll event alone. Readers take its `CoreStore` through `useCoreFields`, so they re-render only for the fields they draw.
- `FakeClient` is the tests' client; `index.ts` does not export it, and nothing in the app imports it.
- **The directory imports `react`, `@tanstack/react-virtual` and its own files, nothing else** — no `@/`, no relative import out — and knows nothing of columns, the viewport, GraphQL or the error bus.
