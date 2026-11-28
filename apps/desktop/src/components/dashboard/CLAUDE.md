# src/components/dashboard — the dashboard

The resource nav, the pages, the objects and events tables, the object panel and `VirtualTable`. Also owns `src/lib/`'s `dashboard-resources.ts`, `cluster-cached-data-*.tsx`, `cluster-namespaces.tsx`, `object-*.ts[x]`, `overview.ts`, `pod-status.ts`, `table-columns.ts`, `sort-rows.ts` and `jsonpath.ts`. How the window bounds a table's scroll is in `src/CLAUDE.md` (*Window state*).

## The resource nav

`buildDashboardNav(kinds)` (`src/lib/dashboard-resources.ts`) places each kind: **its scope is its section**; its group is the curated kind's (`GROUPS`), else its API group's (`API_GROUP_TO_GROUP`), else a core plural's (`CORE_KIND_TO_GROUP`), else *Custom resources* for a CRD and *Extensions* for the rest. A curated kind matches on API group **and** plural, so a CRD reusing a built-in's plural never takes its row. A curated or core kind's id is its plural, any other's `<group>/<resource>`; Events are the pinned row, never a section's kind. `DashboardResource` is an **open string**, validated leniently. → [ADR: dashboard nav merge](../../../docs/adr/2026-08-09-dashboard-nav-merge.md), [ADR: the dashboard nav groups kinds by scope and API group](../../../docs/adr/2026-11-12-the-dashboard-nav-groups-kinds-by-scope-and-api-group.md).

- **Before the catalog's `Bookmark` (`useDashboardNav` gates on it) the tree is the curated kinds without counts and nothing hides.** After it, `filterNav` hides kinds with no objects, the current kind excepted; a search reaches them.
- A closed group holding the current kind draws as selected (`SELECTED`, shared with `NavItem`). Each group's open state is a `usePersistedFlag` (`dashboard-nav-open:<section>/<group>`). Kind labels draw through `VisibleText`.

## Live data

`ClusterCachedDataKindsProvider` (`src/lib/cluster-cached-data-kinds.tsx`) holds the window's **one** `clusterCachedDataKindCollectionWatch(id, cacheID, namespace)`, **subscribed with the namespace scope**: each namespaced kind's count (and Events') is the namespace's, a cluster-scoped kind's the whole cache's. The frame's echoed `namespace` joins `cacheID` in its provenance, so a namespace switch re-subscribes like a cache swap. `AppLayout` mounts it above the sidebar and the routes, enabled only on the dashboard, so the catalog outlives the nav's card and a reopened nav reads it whole.

All three cached-data hooks resolve the cluster/cache through `useActiveCluster()` and reduce through **`useCacheCollectionWatch`** (`src/lib/graphql/use-cache-collection-watch.ts`), which owns the id-keyed fold, `subscriptionPhase`, and the **cache/kind provenance guard** (rejects rows from a superseded cache — don't bypass it; → [`docs/read-contracts/webview.md`](../../../docs/read-contracts/webview.md)). `ClusterNamespacesProvider` (`src/lib/cluster-namespaces.tsx`), beside the catalog's and enabled the same way, holds the window's one `v1/namespaces` watch, `unshared`, since a Namespaces page watches the same kind.

**The namespace picker** (`breadcrumb-bar/scope-menu.tsx`): the caller's `entries(query)` decides what matches. The chosen namespace stays in the menu (*Not found* once the list has landed without it). Under a search, Enter never takes *All namespaces* (`explicit`) unless it is highlighted.

## Pages

`routes/dashboard.tsx` picks a page from `resource`. A resolved discovered kind's `KindPage` is keyed by cache **plus** `apiVersion`+`resource`, and held through a reconnect's replay of the catalog so the table stays mounted. **A page owns its watch and filters it by the namespace scope**; a cluster-scoped kind is not filtered and says so under a namespace. `useDashboardNavPlace` names the page in the breadcrumb bar by placing the one kind without building the tree. A resource the catalog lacks reads "not synced" with no active cache, a spinner before the Bookmark, and "not served" after it. **Overview** (`overview/`) takes every number from `useOverview` (`src/lib/overview.ts`), so a tile and its card never disagree; its watches are `unshared` and mount only while it shows, and each section spins until its own watches' snapshots land. A snapshot is the cache's, not the cluster's: until `cacheCatchingUp` clears, the page says its counts may be short and claims no empty state. Its layout follows its own width (container queries), since the right sidebar narrows a page to 320px. `rawJSON` is the full native body (`unknown`). Both tables take their rows, `active` and `phase` as props and gate on them: no active cache → "not synced", `connecting` → spinner, `live` with no rows → "No …".

## Columns

**A kind's columns come from `objects-table/columns/`:**

- **A hand-written entry wins**, else the kind's `printerColumns` evaluated by `src/lib/jsonpath.ts`. Those kubectl prints only under `-o wide` (`priority > 0`) are `hiddenByDefault`. A printer column's key is its name (`printer:<name>`), not its place, so a user's choices for it hold when the CRD adds or moves another.
- **Every built-in kind is hand-written**, one file per nav group, joined by `gvrKey` in `registry.tsx`. Each draws kubectl's columns; its `-o wide` columns and more of the object's fields are `hiddenByDefault`.
- **A kind's entry is `kind<Body>(apiVersion, resource, columns)`** (`common.tsx`). `Body` is the narrow shape the kind reads off `rawJSON`, named once. Each column is a builder over a reader of that body, or a `KindColumn` written out where none fits.
- **A status column gives the kind its tone.** `statusColumn`'s reader returns the label and tone of its `StatusPill`, and `toneForKind` reads the tone off that column, so the tabs and the pill cannot disagree. A PersistentVolume or claim being deleted reads *Terminating*, whatever its phase. A Deployment's status is `kubectl rollout status`'s reading: *Progressing* until the current generation is observed, whatever an earlier rollout's conditions say, and *Available* once every desired replica is updated, no old one is left, and every updated one is available.
- **A cell is redrawn only on a new frame**, so one that reads the clock ticks itself (`Age` through `react-timeago`, a running Job's Duration through `Elapsed`). A column whose sort value reads the clock says so (`sortsByClock`), and the table sorts by it again each second.
- **A pod's tone is its status's (`podStatusTone`), except that a running pod is `pending` until its Ready condition holds**, so a starting pod never reads failing and a never-ready one never reads healthy.

## The objects table

- **Rows are two lines tall** (`TALL_ROW`), their cells without vertical padding: a row's height is only a minimum, and two lines under the library's padding would outgrow the height the table's arithmetic assumes. A Name cell's owner is `ownerOf` (`src/lib/object-owner.ts`).
- **The text filter narrows the rows before the status tabs count them** (`matchesFilter`, `src/lib/object-filter.ts`). A custom resource can hold anything at `spec.nodeName`, so only a string there is matched.
- **The column picker's handle goes through dnd-kit's pointer and keyboard sensors, never HTML drag and drop**, which Tauri's file drop swallows on Windows.
- **`useTableColumns` (`src/lib/table-columns.ts`) keeps the user's choices and order per kind in localStorage**, each against the column's default, so a column the table gains later takes its own default place and visibility (`arrange`).
- **A column with a `sortValue` sorts.** `sortRows` (`src/lib/sort-rows.ts`) is stable and puts a missing value last. The sort is the table's own state, reset by keying the table on its kind.
- **The actions column** (`objects-table/actions.tsx`) is outside the column picker and its order, and `pinned` to the right edge so a narrow page's sideways scroll never hides it. *View logs* is offered for the kinds of `LogsViewSourceKind`: `logsSourceKind` (`src/lib/object-actions.ts`) maps a `gvrKey` onto one, so a CRD reusing a built-in's kind name is not one; the object panel's Logs tab reads it too.

## The Events page

**The open event is `EventsPage` state, never a search param**, so Back leaves the page; a namespace or cache switch closes it. `EventsPage` holds its rows through a reconnect's replay (`useLastSettled`), so the detail stays mounted.

## The object panel

**The focused object is `useObjectView`** (`src/lib/object-view.tsx`): chrome, not persisted. `set` opens the right sidebar. The object is bound to the cluster it was focused in: `ObjectViewProvider` drops it once the active cluster is another, however the window got there. `ObjectPanel` is keyed on the uid and mounts the open tab alone.

- **YAML**: the last body seen is held through a reconnect and, once the object is gone after a `Bookmark`, marked *Deleted*.
- **Events** draw through `VisibleText`.
- **Logs**, for a log kind alone: `LogViewer` over a `LogsViewAction` the tab builds, with no chat behind it. The grep the toolbar applies lasts the mount, and its viewport handle goes unread: the dashboard chat sends no snapshot.
- **YAML and Events each hold an `unshared` watch of their own**, so they stay live whatever page the window shows.

## The virtual table

**Both tables render through `VirtualTable`** (`virtual-table.tsx`), only the on-screen rows in the DOM between two spacer rows. These hold it up:

- **Every row of a table is one height, `ROW_H` unless the table passes a `rowSize` (`TALL_ROW`), and the pixels are read off a rendered row** (`TableRow`'s `border-b` under collapsed borders makes it not `h-9` arithmetic). A wrong constant drifts rows further down a long list.
- **The table is `table-fixed`**: widths come from a `<colgroup>` the header's table and the body's share (the body's first row is a spacer with no cells), each column's `width` in pixels; free-text columns `truncate` with a `title`. The one unsized column takes the slack and keeps its `minWidth`: the table's minimum width is the sum, so a narrow page scrolls sideways rather than squeezing that column to nothing.
- **The header is a table of its own above the scroller**, so the scrollbar starts under it. It pads its right edge by the scroller's bar (`offsetWidth − clientWidth`). It follows the scroller's `scrollLeft` by a `translateX`, never by scrolling itself: WebKit leaves that padding out of a box's scroll range, so a scrolled header would stop short at the far right. **Pinned columns come last, with no header text and no sort**; each body cell sticks at the width of the pinned columns after it.
- **The two tables read as one.** The wrapper is `role="table"`, both native tables are `role="none"`, and their sections, rows, headers and cells name their own roles, so a screen reader ties each cell to its column's header.
- **The scroller's bar is `table-scrollbar`** (`index.css`), a classic bar beside the rows, so their borders never show through a macOS overlay bar. It sets only the `-webkit-` pseudo-elements: Chromium drops them once `scrollbar-width` or `scrollbar-color` is set.

**The table corrects its own scroll**: after each commit it moves `scrollTop` by the *smallest* shift any surviving on-screen row made, doing nothing within one row of the top; the scroller sets `overflow-anchor: none`. The window is clamped to the rows there are, so a list a filter or a tab cuts short under a deep scroll still draws its rows. → [ADR: the virtual table owns its scroll anchoring](../../../docs/adr/2026-10-17-the-virtual-table-owns-its-scroll-anchoring.md).
