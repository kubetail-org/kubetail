# src/components/cluster — the cluster's controls

The context omnibox (in `components/shell/`) and what it opens: the cluster picker, the status popover and the sync panel; and memory. Also owns `src/lib/`'s `cluster-health.ts`, `cluster-diagnostics.ts`, `cluster-mutations.ts`, `kube-config.tsx`, `memories.tsx` and `memory-writes.ts`.

## The context omnibox

The **context omnibox** (`components/shell/context-omnibox/`) is the cluster chip (the `StatusButton` and the picker's trigger), then the Chat and Dashboard tabs. The pill is as wide as its content, up to 420px; the `@container` is a wrapper filling the bar's middle column, so the name truncates first. The tabs are router `Link`s, so each mode is a deep-linkable route and `retainSearchParams` keeps the window's params across the switch. Hit areas cover the pill's padding and gaps, so every press lands on a control. → [ADR: the omnibox is the cluster picker](../../../docs/adr/2026-10-29-the-omnibox-is-the-cluster-picker.md).

- **The trigger opens `ClusterPicker`** through **`OmniboxPopover`**, kept in `WindowFrame`. Each context shows `contextHealth`'s verdict (`src/lib/cluster-health.ts`), the Clusters dialog's own: *Not reached* without a record or before the clusters `Bookmark`. *Manage clusters…* in its footer is the one way to the Clusters dialog.
- **The "Unverified TLS" badge** (`unverified-tls-badge.tsx`) rides beside the cluster it is about, on the trigger and on each picker row — the one way the app says a connection goes unverified.
- `HistoryNav` walks router history via `__TSR_index` through `useSyncExternalStore`.

## The status popover

`ClusterStatusPopover` (`cluster-status/`) is a glance. **Every value is always drawn, a dash for a missing one.**

- **It shares the Clusters dialog's streams** (the hooks in `src/lib/cluster-diagnostics.ts`), its sync wording (`syncReason`, `countLabel`, `src/lib/cluster-health.ts`) and its components (`RelativeTime`, `Countdown`, `RetryButton`, `DataRateSparkline`). The schedule state watch (`useClusterScheduleStateWatch`) is read once, in `ClusterStatus`, and handed down. The history stays in the Clusters dialog.
- **A `Stale` sync's alert is amber**: `Alert` has no amber variant, so it takes `TONE.attention`'s shade.
- **Size.** It hangs from the omnibox, at least 320px wide and as tall as its content. When the window is shorter, the alerts and sections scroll between a fixed header and footer, so a long message never hides the footer's buttons.
- **`clusterDetail` is the one way a dialog opens with state** (*View details* calls `openDialog('clusters', { clusterId, pane })`): `ClusterSyncPanel` reads it once as it mounts, opens that row's pane and scrolls to it, and every other open clears it.

## The kubeconfig mirror

`KubeConfig.defaultContext` is the usable default cluster, else `current-context`, else `''` → [ADR](../../../docs/adr/2026-11-25-a-window-fixes-its-cluster-and-never-falls-back.md).

**The sidecar mirrors the kubeconfig and draws no verdicts over it.** `status.source.kubeconfig.cluster` is `{ name, entry }` (`entry`: `server`, `insecureSkipTLSVerify`; null when undefined). `user` is the name alone — an authInfo entry holds credentials. **`tlsUnverifiedReason` in `src/lib/kube-config.tsx` is the one place that derives the badge**; a second inline copy is how the plain-`http` arm goes missing. → [ADR: status mirrors the kubeconfig](../../../docs/adr/2026-09-03-status-mirrors-the-kubeconfig.md).

## Memory

`useMemories(clusterID)` (`src/lib/memories.tsx`) lists the cluster's own notes first, then every cluster's, each by name — the model's order. Settings' Memory tab draws `memory-instructions.tsx` and `memory-list.tsx` for one scope. → [ADR: memory is rows](../../../docs/adr/2026-09-24-memory-is-rows-scoped-to-a-cluster-or-every-cluster.md), [memory](../../../docs/security/2026-09-24-memory.md).

- The list draws bodies as plain text, never markdown, and never shows a name. The name stays a slug because it is the model's key: a new note's is derived from its body, unique in the cluster, and the editor checks the body's size itself (`memory-writes.ts`).
- Cmd/Ctrl+Enter or blur saves, Escape discards; both keys are left to an IME while it composes. The box is read-only while its save is in flight, and an editor closes only once its save lands or it is discarded, so a refused draft stays open beside the next note's.
- The editor holds its draft fixed against the watch. Saving one of Kubetail's notes makes it the user's, which the editor says; Cmd/Ctrl+Enter saves one even unchanged, adopting it, and a blur never does. Each delete confirms.
- The instructions are local state until something stores them; Cmd/Ctrl+Enter or a blur saves them, never a pause, and *Saved* marks that alone.
