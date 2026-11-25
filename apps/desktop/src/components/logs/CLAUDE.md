# src/components/logs — log views

The log panel, the log windows, the log viewer and its headless core, and the transcript's log cards (`components/chat/log-cards/`). Also owns `src/lib/`'s `logs-view.tsx`, `log-windows.tsx`, `log-window-push.ts`, `logs-client.ts`, `log-viewport.ts` and `log-viewer/`. The webview-wide rules, security invariants first, are [`src/CLAUDE.md`](../../CLAUDE.md). How each card draws in the transcript is in the chat guide ([`../chat/CLAUDE.md`](../chat/CLAUDE.md), *Tool calls*).

Every data access here is named for its contract: [`docs/data-access.md`](../../../docs/data-access.md).

## The focused view

**The log views around the viewer** are `components/chat/log-cards/`, exporting the transcript's `LogsViewCard`, `LogsReadCard` and `LogsWatchCard`; `log-panel.tsx`, the panel and the `LogsViewHeader` the log window reuses; and `components/logs/log-window/`, exporting `LogsWindow`. All three draw a view's sources through `SourceLine` (`log-source-line.tsx`).

- **The focused view is `useLogsView`** (`src/lib/logs-view.tsx`): `{ clusterId, chatId, callId, action, grep? } | null`, chrome, not persisted. `grep` is the user's from the viewer's toolbar, kept while the same call stays focused, so it survives the panel unmounting while the sidebar is collapsed.
- `set` also opens the right sidebar, so `LogsViewProvider` mounts inside `RightSidebarProvider`. The panel's close collapses the sidebar and leaves the view focused, like the toggle. A cluster switch from the picker clears it, and so does moving it to a log window.
- **A view opens on a transcript card's button alone**, Expand (`log-cards/view-card.tsx`) or *View around this* (`log-cards/read-card.tsx`, `log-cards/watch-card.tsx`), never by itself. `viewOfCall` (`logs-view.tsx`) is the call → view mapping those buttons make.

## Log windows


**A view moves to a log window** on *Open in window*, on the panel's header or beside a card's button. A view is in one place at a time: `log-cards/buttons.tsx` decides each card's (`useViewPlace`: a log window, then the sidebar while it is open and focused on the call, else the card), and the card hands it to `LogsViewButtons`, which draws *Showing in a window* with *Show window*, *Showing in the sidebar*, or the card's button and *Open in window*. Only a LogsView card hides its inline lines while the view is elsewhere.

**`LogWindowsProvider`** (`src/lib/log-windows.tsx`, under `LogsViewProvider` in `AppLayout`) is the app window's side of the host's pair (`src-tauri/CLAUDE.md`, *Log windows*):

- It subscribes once per mount, each subscribe with a channel of its own (Tauri ends a channel when the host drops the side a subscribe handed it, which a later subscribe does), all folding through one handler. It keeps the **largest page** any subscribe answers, since two can land in either order. It folds `snapshot`/`opened`/`closed`/`focused` into a map by label, and navigates on `showChat`.
- **Each window's viewport is a mirror kept out of React state** (`createMirror`, `src/lib/log-window-push.ts`): made when a `snapshot` or `opened` first names the window, folded from every push those frames and `viewport` frames carry, never replaced while the window lives, and dropped on `closed` or a `snapshot` without it. A `viewport` frame re-renders only what reads the mirror. A mirror keeps its selection array while the push's generation and `selectionRevision` hold.
- `shownWindowOf(chatId)` is the chat's window with the highest `focusSeq`, with its mirror. It is `ready` once the page and the cluster are known; the window buttons are disabled until then.
- **Opens and closes go through one queue**, each after the last settles, since the host's open is async and its close is not. An open carries the focused view's grep when it is the same call, is dropped at its turn if its view is not on the held cluster, and on success clears the focused view if it is still that call, closing the sidebar if it showed it.
- **The held cluster** is the last defined `clusterID` since the page; the first is held with no close, and a later different one queues `log_window_close_all`. The windows are checked against the held cluster once per snapshot and once per page, and a window on another cluster queues a close too. An undefined `clusterID` changes nothing.

## The log window's page


**The log window's page** (`components/logs/log-window/window.tsx`) asks the host `log_window_view` once per mount (a refusal draws *This log window could not load.* with *Try again*), then reads the chat, its messages and the clusters.

- It draws *Loading…* until all three bookmarks, then *This chat was deleted.* or *This log view is not available.*, else the panel's header (`LogsViewHeader`, no buttons) over `LogViewer` with a viewport of its own. A reconnect masks a watch's data until its new bookmark, so the view drawn before it stays mounted through that, keeping the viewer's grep, scroll and selection.
- The grep is the user's from this window, else the open's, else the action's. It sends `log_window_title` with the first source, the chat's title and the cluster's name, an empty part for one missing.
- **It pushes its viewport** (`createViewportPublisher`, `src/lib/log-window-push.ts`) through `log_window_viewport`, stamped with the answer's `generation` and the grep in effect, which the host keeps, so a reload keeps the toolbar's grep. The handle, the applied grep and the publisher live in `Paired`, above the viewer a notice unmounts.
- A push carries whole selected lines, at most 50 within 192 KiB of UTF-8 (`cutSelection`), the whole `selectedCount`, and a `selectionRevision` that steps on each new selection array. One is in flight at a time; a change to the selection, the count or the grep goes on the next frame, the rest after 250 ms, and `blur` sends at once. A refused push is reported and not resent.
- `LogsWindowBar` is its 40px drag bar: the cluster's dot and name and the chat's title through `VisibleText`, *Show chat*, the traffic-light gutter on macOS and `WindowControls` elsewhere, with no `AppMenu`.

## The log viewer


**The log viewer** is `components/logs/log-viewer/`; its `index.ts` exports `LogViewer`, plus `LOG_ROW_PX`, `CutNotice`, `Dot` and `GapNotice` for the transcript's cards, and nothing outside imports a part. See the directory's files for rows, header, toolbar, jump-to-time and wrap.

- **Its client is built once per mount**, in a `useState` initializer, by **`createLogsClient(urql, clusterId, action)`** (`src/lib/logs-client.ts`, the one place the app makes a `Client`). `logsFetch` runs with `requestPolicy: 'network-only'`, since a page's variables repeat. `At` is two disjoint pages joined, the older ending the nanosecond before the moment; `Until` and `Since` are one first page each, the bound as given, with `seen`. `logsTail` starts from a page's cursor; a reconnect sends the `seen` given and the newest line it handed on of each container; an `Ended` unsubscribes; a tail the sidecar could not open calls `onFailed`.
- Cursors pass through as the sidecar wrote them. `scopeInput` builds the `LogsScopeInput` field by field, since the action carries `__typename`s. It lands at the action's `openAt` on mount only. A line's time reads through `lineTime` (`src/lib/log-viewer/line-time.ts`), in the viewer and the cards alike.
- **The toolbar's grep box replaces the action's grep** for the focused view. A submitted pattern is checked with a one-line read through a client built with it, since the sidecar compiles a grep before it reads. Accepted, the viewer swaps to that client, lands at the end while pinned and else at the first line on screen, and reports it through `onGrepApplied` and the viewport's `grep`, so the panel's header and the chat's snapshot name the grep in effect. Refused, the reason shows at the box and the view stays.
- **Cells are drawn through `VisibleText`, the message through `AnsiText`**; the dot colour is inline (no CSS name built from cluster text). A cut line ends with `CutNotice`, its `truncatedBytes` in KiB. The notices of the window's pages and follow draw over the lines, `code` in a `title`. A line's `gap` draws a notice above it in the same row (one more `LOG_ROW_PX` in `estimateRowHeight`), its names through `VisibleText`; selection passes over it and a row's copy writes it in brackets.
- Selection is kubetail's, ported (`selection.ts`). **Keys go on the scroller, never `document`**, so a page can hold two viewers. Enter/Space on a cell are taken in the capture phase, before the scroller's intent reader would read Space as scroll-to-bottom. Copy goes through `navigator.clipboard.writeText`, a refusal to `reportError` as `render`.
- A failed page or follow is drawn, not reported: `errorReportExchange` has reported it.
- It writes what is on screen to the window's one **`LogViewportHandle`** (`src/lib/log-viewport.ts`), and the empty viewport on unmount. Columns and wrap persist as `kubetail:log-viewer-columns` / `kubetail:log-viewer-wrap`.

## The viewer's library

**`src/lib/log-viewer/` is the viewer's headless core**; its `README.md` is the whole statement. → [ADR: a log read is a window over a range](../../../docs/adr/2026-10-23-a-log-read-is-a-window-over-a-range.md). In brief:

- `types.ts` is the contract: a `Client` with `fetch` (a query of kind `First`, `Last`, `At`, `Continue`, `Until` or `Since`; a `Page` with `prev`/`next`/`follow` edges and `notices`) and `tail(cursor, seen)` (`Line`, `Notice`, `Ended`; a follow that cannot open throws or calls `onFailed`, and the window unpins). **Cursors are opaque**; a line whose cursor is held is a repeat, dropped. Every `Continue` from the after edge sends `seen`, the newest line held of each container; an `Until` sends the oldest.
- `LogWindow` (`window.ts`) is a React-free class holding the lines and the rules (stable keys, key 0 at the landing; only a `Tail` landing pins, then the pin is the user's; following is derived; one page per edge in flight). **It is bounded at `maxLines`** (the core's default is 5000): a page or batch past it drops lines at one edge, never on screen, and that edge reads them back from a *seam*, the farthest dropped stamp, with an `Until` or a `Since`. A dropped key comes back on another line, so the selection (`selection.ts`) prunes to `firstKey`..`firstKey + count` in a layout effect. → [ADR: a bounded log window reads its dropped lines back](../../../docs/adr/2026-10-24-a-bounded-log-window-reads-dropped-lines-back.md). `window.test.ts` ends in a seeded walk over `FakeClient` that checks the rules after every step.
- `LogViewerCore` (`core.tsx`) puts a fresh window, scroller and `@tanstack/react-virtual` virtualizer behind each landing. It reads intent off wheel, key, pointer and touch events, never a scroll event alone. It publishes to a `CoreStore` that readers take through `useCoreFields`, so they re-render only for the fields they draw.
- `FakeClient` (`fake-client.ts`) is the tests' client; `index.ts` does not export it, and nothing in the app imports it.
- **The directory imports `react`, `@tanstack/react-virtual` and its own files, nothing else** — no `@/`, no relative import out — and knows nothing of columns, the viewport, GraphQL or the error bus.
