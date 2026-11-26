# src/components/cluster — the cluster's controls

The context omnibox (in `components/shell/`) and what it opens: the cluster picker, the status popover and the sync panel; and memory. Also owns `src/lib/`'s `cluster-health.ts`, `cluster-diagnostics.ts`, `cluster-mutations.ts`, `kube-config.tsx`, `memories.tsx` and `memory-writes.ts`. The webview-wide rules, security invariants first, are [`src/CLAUDE.md`](../../CLAUDE.md).

Every data access here is named for its contract: [`docs/data-access.md`](../../../docs/data-access.md).

## The context omnibox

The **context omnibox** (`components/shell/context-omnibox/`, exporting the omnibox alone; `omnibox.tsx`) is a white pill in the app bar's centre: the cluster chip (the **`StatusButton`** and the trigger, the name semibold and left-aligned), a divider, then the Chat and Dashboard tabs (`mode-tabs.tsx`). The tabs are router `Link`s in `<nav aria-label="View">`, styled off `data-status="active"`, so each mode is a deep-linkable route and `retainSearchParams` keeps the window's params across the switch. Hit areas cover the pill's padding and gaps, so every press lands on a control. Narrow (a `@container`), the name truncates first; under 20rem the tabs are icons, named and with a tooltip; under 14rem the chip's padding goes and the TLS badge is an icon. With no context there is no status button; the trigger draws the bare icon. → [ADR: the omnibox is the cluster picker](../../../docs/adr/2026-10-29-the-omnibox-is-the-cluster-picker.md).

- **The trigger opens `ClusterPicker`** (`components/cluster/cluster-picker/`, exporting the picker alone), hung from the box through **`OmniboxPopover`** (`components/cluster/omnibox-popover.tsx`), kept in `WindowFrame`. It lists the kubeconfig's contexts, the window's first, each with `src/lib/cluster-health.ts`'s verdict (`contextHealth`: the Clusters dialog's own, *Not reached* without a record or before the clusters `Bookmark`); a filter appears from six contexts. A row switches the window's cluster; *Manage clusters…* in its footer is the one way to the Clusters dialog.
- **The "Unverified TLS" badge** (`unverified-tls-badge.tsx`) rides beside the cluster it is about, on the trigger and on each picker row — the one way the app says a connection goes unverified.
- `HistoryNav` walks router history via `__TSR_index` through `useSyncExternalStore`.

## The status popover

The status button is the server icon with the window's cluster's verdict as a dot in its corner. It opens **`ClusterStatusPopover`** (`components/cluster/cluster-status/`, exporting the popover alone; `popover.tsx`), a glance. **Every value is always drawn, a dash for a missing one.** Top to bottom:

- A header with the context and the overall verdict as a pill.
- Under an `error` connection tone, a destructive `Alert`: `connectionFailureSummary`'s words for the `Connected` reason as its title, then the condition's message and *N attempts · Retrying in D* off the schedule, *Retrying now…* while a probe runs.
- Under an `error` sync tone, one titled by `cacheFailureSummary` (a failed sync's `failure` class, such as *Permission denied*) over `syncReason`'s line.
- Under a `Stale` sync, an amber one (`Alert` has no amber variant, so it takes `TONE.attention`'s shade), titled by the kinds not receiving updates (up to three by name, else a count), over a line saying the data shown may be out of date and Kubetail restarts the watches until updates resume.
- Three sections. *Kubeconfig*: context, server, user. *Connection*: its verdict, uptime (or downtime while down), download rate and its received-only sparkline, on a square-root scale, whose hovered sample shows its rate and age in a small tooltip centred over the chart. *Cache*: its verdict, with *Syncing (N/M)…* counting the unpaused kinds caught up and *Current · N paused* naming paused kinds, `syncReason`'s one line under any other `attention` tone, populated kinds, objects, last event.
- A footer with *View details* left and `RetryButton` right: *Retry now*, held down as *Retrying…* while a probe runs, under an `error` connection tone; else *Reconnect*, held as *Reconnecting…* after a press and *Checking…* while any other probe runs.

Around it:

- **It shares the Clusters dialog's streams** (the hooks in `src/lib/cluster-diagnostics.ts`), its sync wording (`syncReason`, `countLabel`, `src/lib/cluster-health.ts`) and its components (`RelativeTime`, `Countdown` and `RetryButton` in `primitives/`, `DataRateSparkline` in `cluster/`). The schedule state watch (`useClusterScheduleStateWatch`) is read once, in `ClusterStatus`, and handed down. The history stays in the Clusters dialog.
- **Size.** It hangs from the omnibox, at least 320px wide and as tall as its content. When the window is shorter, the alerts and sections scroll between a fixed header and footer, so a long message never hides the footer's buttons. It neither resizes nor remembers a size.
- *View details* calls `openDialog('clusters', { clusterId, pane })`. **`clusterDetail` is the one way a dialog opens with state**: `ClusterSyncPanel` (`components/cluster/cluster-sync-panel/`, its one export) reads it once as it mounts, opens that row's pane and scrolls to it, and every other open clears it.

## The kubeconfig mirror

`KubeConfig.defaultContext` is the usable default cluster, else `current-context`, else `''` → [ADR](../../../docs/adr/2026-11-25-a-window-fixes-its-cluster-and-never-falls-back.md).

**The sidecar mirrors the kubeconfig and draws no verdicts over it.** `status.source.kubeconfig.cluster` is `{ name, entry }` (`entry`: `server`, `insecureSkipTLSVerify`; null when undefined). `user` is the name alone — an authInfo entry holds credentials. **`tlsUnverifiedReason` in `src/lib/kube-config.tsx` is the one place that derives the badge**; a second inline copy is how the plain-`http` arm goes missing. → [ADR: status mirrors the kubeconfig](../../../docs/adr/2026-09-03-status-mirrors-the-kubeconfig.md).

## Memory

**Memory** (`useMemories(clusterID)`, `src/lib/memories.tsx`: the cluster's own first, then every cluster's, each by name — the model's order). Settings' Memory tab (`components/settings/`) is where the user reads and writes them, each a *note*. It draws the instructions (`memory-instructions.tsx`) and the list (`memory-list.tsx`) of one scope. The list draws bodies as plain text, never markdown, and never shows a name. A click on a note's text edits it in place: Cmd/Ctrl+Enter or blur saves, Escape discards; both keys are left to an IME while it composes. The box is read-only while its save is in flight, and an editor closes only once its save lands or it is discarded, so a refused draft stays open beside the next note's. Cmd/Ctrl+Enter saves one of Kubetail's notes even unchanged, adopting it; a blur never does. A new note's name is derived from its body, unique in the cluster, and the editor checks the body's size itself (`memory-writes.ts`). The name stays a slug because it is the model's key. The editor holds its draft fixed against the watch, and saving one of Kubetail's notes makes it the user's — which the editor says. Each delete confirms. The instructions are local state until something stores them; Cmd/Ctrl+Enter or a blur saves them, never a pause, and *Saved* marks that alone. → [ADR: memory is rows](../../../docs/adr/2026-09-24-memory-is-rows-scoped-to-a-cluster-or-every-cluster.md), [memory](../../../docs/security/2026-09-24-memory.md).
