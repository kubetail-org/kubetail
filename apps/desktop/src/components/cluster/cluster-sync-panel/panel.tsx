// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Cluster-registry dialog, grouped into Active (in the current kubeconfig) and
// Orphaned (leftover cache, context gone). Mutations write through; the resulting
// clusterCollectionWatch push updates the table.
import { Button } from '@kubetail/ui/elements/button';
import { Spinner } from '@kubetail/ui/elements/spinner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@kubetail/ui/elements/table';
import { ChevronDown, Database, Pause, Play, Power, PowerOff, Slash, Trash2 } from 'lucide-react';
import { type ReactNode, useEffect, useMemo, useRef, useState } from 'react';
import { useMutation } from 'urql';

import { DataRateSparkline } from '@/components/cluster/data-rate-sparkline';
import { Countdown } from '@/components/primitives/countdown';
import { Dialog } from '@/components/primitives/dialog';
import { RelativeTime } from '@/components/primitives/relative-time';
import { RetryButton } from '@/components/primitives/retry-button';
import { graphql } from '@/gql';
import type { ClusterCachedKindCollectionWatchSubscription } from '@/gql/graphql';
import {
  type CacheContents,
  parseTimeOrNull,
  useClusterCacheStatsStateWatch,
  useClusterDataRateSampleFeed,
  useClusterScheduleStateWatch,
} from '@/lib/cluster-diagnostics';
import {
  type Group,
  type Tone,
  TONE,
  connectionStatus,
  countLabel,
  findCondition,
  isPending,
  kindsSyncingLabel,
  offenderList,
  overallStatus,
  syncStatus,
} from '@/lib/cluster-health';
import {
  ClusterCacheClearMutation,
  ClusterDeleteMutation,
  ClusterEnabledSetMutation,
  ClusterSyncEnabledSetMutation,
} from '@/lib/cluster-mutations';
import {
  type Cluster,
  type ClusterCacheHealth,
  displayName,
  formatBytes,
  formatRate,
  useClusters,
} from '@/lib/clusters';
import { type AppDialogProps, useDialogHost } from '@/lib/dialog';
import { collectionWatchReducer } from '@/lib/graphql/collection-watch';
import { eventFeedReducer } from '@/lib/graphql/event-feed';
import type { EventFeedHistory } from '@/lib/graphql/event-feed';
import { stateWatchReducer } from '@/lib/graphql/state-watch';
import { useFoldedSubscription, subscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import { EVENTS_GVR, gvrKey } from '@/lib/gvr';
import { humanDuration, humanMinutes } from '@/lib/human-duration';

// One kind's own switch. Pausing keeps the cached rows, which is the whole difference from
// clusterCacheClear — the objects stay listed and readable while the watch is stopped.
const ClusterCachedKindSyncEnabledSetMutation = graphql(`
  mutation ClusterCachedKindSyncEnabledSet($id: ObjectID!, $syncEnabled: Boolean!) {
    clusterCachedKindSyncEnabledSet(id: $id, syncEnabled: $syncEnabled) {
      id
      spec {
        syncEnabled
      }
    }
  }
`);

// Connection-probe history (event-log category "connection") — decoupled from
// clusterCollectionWatch so probe chatter never re-emits the registry. Subscribed only
// while a row's diagnostics are open.
const ClusterEventFeedConnectionDocument = graphql(`
  subscription ClusterEventFeedConnection($id: ClusterID!) {
    clusterEventFeed(id: $id, category: "connection") {
      type
      event {
        id
        type
        reason
        message
        count
        firstAt
        lastAt
      }
    }
  }
`);

// One aggregated event run (`ok` = Normal type); shared by both histories.
type EventRun = {
  id: string;
  ok: boolean;
  reason: string;
  message: string;
  count: number;
  firstAt: string;
  lastAt: string;
};

// A raw Event off an event feed, before `ok` is derived.
type RawEvent = Omit<EventRun, 'ok'> & { type: string };

// One frame: a run, or the bookmark closing the snapshot (which carries none).
type RawEventFrame = { type: string; event: RawEvent | null };

// The server prunes each history past this many runs, and never announces a prune, so a
// long-lived subscription would otherwise keep runs the server has already dropped.
// Mirrors maxEventRuns in the sidecar.
const MAX_EVENT_RUNS = 20;

function runOf(ev: RawEvent): EventRun {
  return {
    id: ev.id,
    ok: ev.type === 'Normal',
    reason: ev.reason,
    message: ev.message,
    count: ev.count,
    firstAt: ev.firstAt,
    lastAt: ev.lastAt,
  };
}

// Newest first by lastAt, oldest beyond the retention bound dropped.
function eventRunReducer<Data>(frameOf: (data: Data) => RawEventFrame) {
  return eventFeedReducer(
    (data: Data) => {
      const { type, event } = frameOf(data);
      return { type, event: event && runOf(event) };
    },
    { compare: (a, b) => Date.parse(b.lastAt) - Date.parse(a.lastAt), cap: MAX_EVENT_RUNS },
  );
}

const reduceClusterEvents = eventRunReducer((data: { clusterEventFeed: RawEventFrame }) => data.clusterEventFeed);
const reduceObjectEvents = eventRunReducer((data: { objectEventFeed: RawEventFrame }) => data.objectEventFeed);

const EMPTY_HISTORY: EventFeedHistory<EventRun> = { events: [], snapshotComplete: false };

function useClusterEventFeedConnection(clusterId: string): EventRun[] {
  const { data } = useFoldedSubscription(
    { query: ClusterEventFeedConnectionDocument, variables: { id: clusterId } },
    reduceClusterEvents,
  );
  return data?.events ?? [];
}

// Sync-event history, keyed by a ClusterCachedKind record's id (each kind's
// worker logs to its own record), not the cache's. Subscribed only while sync
// detail is open, one kind at a time.
const ObjectEventFeedSyncDocument = graphql(`
  subscription ObjectEventFeedSync($id: ObjectID!) {
    objectEventFeed(id: $id, category: "sync") {
      type
      event {
        id
        type
        reason
        message
        count
        firstAt
        lastAt
      }
    }
  }
`);

// One kind's sync-transition log; paused until there's an id (a placeholder
// subscription would carry nothing). Returns the timeline, not bare runs: the empty
// state must not render before the bookmark says the history really is empty.
function useObjectEventFeedSync(syncId: string | undefined): EventFeedHistory<EventRun> {
  const { data } = useFoldedSubscription(
    { query: ObjectEventFeedSyncDocument, variables: { id: syncId ?? '' }, pause: !syncId },
    reduceObjectEvents,
  );
  return data ?? EMPTY_HISTORY;
}

// Kind-discovery history, keyed by the CACHE's record id: what the cluster serves is the
// cache's own fact, where each kind's sync transitions live on that kind's record.
const ObjectEventFeedDiscoveryDocument = graphql(`
  subscription ObjectEventFeedDiscovery($id: ObjectID!) {
    objectEventFeed(id: $id, category: "discovery") {
      type
      event {
        id
        type
        reason
        message
        count
        firstAt
        lastAt
      }
    }
  }
`);

// One cache's discovery log.
function useObjectEventFeedDiscovery(cacheId: string): EventFeedHistory<EventRun> {
  const { data } = useFoldedSubscription(
    { query: ObjectEventFeedDiscoveryDocument, variables: { id: cacheId } },
    reduceObjectEvents,
  );
  return data ?? EMPTY_HISTORY;
}

// One cache's sync detail — the discovery verdict, and a row per mirrored kind carrying its
// OWN reason. The rollup above it names the offenders but not why each is failing, and a
// cache's kinds fail independently, so this is the only thing that can say.
const ClusterCacheSyncStateWatchDocument = graphql(`
  subscription ClusterCacheSyncStateWatch($id: ClusterID!, $cacheID: ObjectID!) {
    clusterCacheSyncStateWatch(id: $id, cacheID: $cacheID) {
      exists
      discovery {
        reason
        message
      }
      kinds {
        apiVersion
        resource
        reason
        message
        objectCount
      }
    }
  }
`);

type KindSyncStatus = {
  apiVersion: string;
  resource: string;
  reason: string;
  message: string;
  objectCount: number;
};

type CacheSyncState = {
  exists: boolean;
  discovery: { reason: string; message: string };
  kinds: KindSyncStatus[];
};

// A state watch: each frame replaces the last outright. null until the first frame, which is what
// keeps a still-arriving detail from rendering as "no kinds", and for a cache that is gone.
function useClusterCacheSyncStateWatch(clusterId: string, cacheId: string): CacheSyncState | null {
  const { data } = useFoldedSubscription<{ clusterCacheSyncStateWatch: CacheSyncState }, CacheSyncState>(
    { query: ClusterCacheSyncStateWatchDocument, variables: { id: clusterId, cacheID: cacheId } },
    stateWatchReducer((resp) => resp.clusterCacheSyncStateWatch),
  );
  return data?.exists ? data : null;
}

// One cache's per-kind sync records — cache-scoped (one per synced kind; a
// cluster-wide stream would be a hundred-plus records per cache). Identity only,
// no conditions: the verdict comes from the sidecar's rollup (see syncStatus), and
// this stream is only asked which record owns the timeline worth showing.
const ClusterCachedKindCollectionWatchDocument = graphql(`
  subscription ClusterCachedKindCollectionWatch($cacheID: ObjectID!) {
    clusterCachedKindCollectionWatch(cacheID: $cacheID) {
      type
      kind {
        id
        spec {
          apiVersion
          resource
        }
      }
    }
  }
`);

// NonNullable: null only on a Bookmark, folded away in the reducer below.
type CachedKind = NonNullable<ClusterCachedKindCollectionWatchSubscription['clusterCachedKindCollectionWatch']['kind']>;

// The kind syncs render per-kind detail, not an empty state, so they need no
// snapshot-complete gate.
const reduceCachedKinds = collectionWatchReducer(
  ({ clusterCachedKindCollectionWatch: { type, kind } }: ClusterCachedKindCollectionWatchSubscription) => ({
    type,
    item: kind,
  }),
);

// The cache's kind syncs, id-keyed.
function useClusterCachedKindCollectionWatch(cacheId: string): CachedKind[] {
  const { data } = useFoldedSubscription(
    { query: ClusterCachedKindCollectionWatchDocument, variables: { cacheID: cacheId } },
    reduceCachedKinds,
  );
  const items = data?.items;
  return useMemo(() => (items ? [...items.values()] : []), [items]);
}

// Single source for the colSpan numbers below.
const COLUMN_COUNT = 6;

// Status-circle column, shared so header/rows/detail align. Non-zero width so
// table-fixed doesn't collapse it.
const STATUS_CELL_CLASS = 'w-5 pr-0';

// A tinted status label (the connection/sync column word).
function ToneText({ tone, children }: { tone: Tone; children: ReactNode }) {
  return (
    <span data-tone={tone} className={TONE[tone].text}>
      {children}
    </span>
  );
}

// Connection diagnostics: up?, and `stateSinceMs` (the `Connected` condition's last
// transition). `unconfirmed` = the status is a downgrade of a previous sidecar
// process's write — neither half is knowable yet, so report both as unknown.
function connectionDetail(c: Cluster): {
  connected: boolean;
  unconfirmed: boolean;
  stateSinceMs: number | null;
} {
  const cond = findCondition(c.conditions, 'Connected');
  const unconfirmed = cond?.unconfirmed ?? false;
  return {
    connected: cond?.status === 'True',
    unconfirmed,
    stateSinceMs: unconfirmed ? null : parseTimeOrNull(cond?.transitionedAt),
  };
}

// Run span; "Once" for a single probe.
function formatRunDuration(count: number, firstMs: number | null, lastMs: number | null): string {
  if (count <= 1 || firstMs === null || lastMs === null) return 'Once';
  return humanDuration(lastMs - firstMs);
}

// One diagnostics line. Non-null `ms` ticks live; null shows `fallback` or omits
// the row. `prefix` renders ahead of the ticking value ("137 kinds · 45s ago").
function DetailRow({
  label,
  ms,
  prefix,
  fallback,
  format,
}: {
  label: string;
  ms: number | null;
  prefix?: string;
  fallback?: ReactNode;
  format?: (ms: number) => string;
}) {
  if (ms === null && fallback === undefined) return null;
  return (
    <div className="flex gap-2">
      <dt className="w-28 shrink-0">{label}</dt>
      <dd className="tabular-nums">
        {ms !== null ? (
          <>
            {prefix}
            <RelativeTime ms={ms} format={format} />
          </>
        ) : (
          fallback
        )}
      </dd>
    </div>
  );
}

// Newest-first aggregated event runs — shared body of both detail panes.
// `showDuration` adds a per-run duration column (the sync pane's runs are one-shot).
function EventRunList({
  title,
  runs,
  labelOf,
  showDuration = true,
}: {
  title: string;
  runs: EventRun[];
  labelOf: (r: EventRun) => string;
  showDuration?: boolean;
}) {
  if (runs.length === 0) return null;
  return (
    <div className="space-y-1">
      <p className="text-xs font-medium text-muted-foreground">{title}</p>
      {/* Scrolls so a long history doesn't blow out the panel; subgrid aligns cells. */}
      <ul
        className={`grid max-h-40 ${
          showDuration ? 'grid-cols-[auto_auto_auto_1fr]' : 'grid-cols-[auto_auto_1fr]'
        } divide-y overflow-auto  rounded-md border text-xs`}
      >
        {runs.map((a) => {
          const firstMs = parseTimeOrNull(a.firstAt);
          const lastMs = parseTimeOrNull(a.lastAt);
          return (
            <li
              key={a.id}
              className={`${
                showDuration ? 'col-span-4' : 'col-span-3'
              } grid grid-cols-subgrid items-baseline gap-x-2 px-2 py-1`}
            >
              {/* End time; full window on hover. */}
              <span
                className="whitespace-nowrap font-mono text-muted-foreground tabular-nums"
                title={a.count > 1 ? `${a.firstAt} – ${a.lastAt}` : a.lastAt}
              >
                {a.lastAt}
              </span>
              <span className={`whitespace-nowrap ${a.ok ? TONE.ok.text : TONE.error.text}`}>
                {a.count > 1 ? `${labelOf(a)} ×${a.count}` : labelOf(a)}
              </span>
              {showDuration ? (
                <span className="whitespace-nowrap font-mono text-muted-foreground tabular-nums">
                  {`(${formatRunDuration(a.count, firstMs, lastMs)})`}
                </span>
              ) : null}
              <span className="wrap-break-word text-muted-foreground">{a.message}</span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

// The next check: a countdown, "checking…" with a spinner while a probe runs, or a dash when
// none is due. Always something, so the row it fills never reflows.
export function NextCheck({ clusterId }: { clusterId: string }) {
  const { atMs, probing } = useClusterScheduleStateWatch(clusterId);
  if (probing) {
    return (
      <span className="inline-flex items-center gap-1">
        <Spinner size="xs" className="mr-0" />
        checking…
      </span>
    );
  }
  if (atMs === null) return <>—</>;
  return <Countdown ms={atMs} />;
}

// Expanded connection diagnostics + Retry-now. In the dialog it is inline, not a
// popover: the modal dialog inerts everything outside its subtree, so a body-portaled
// popover would be unclickable.
export function ConnectionDetail({ cluster, connFailed }: { cluster: Cluster; connFailed: boolean }) {
  const detail = connectionDetail(cluster);
  const attempts = useClusterEventFeedConnection(cluster.id);
  const rates = useClusterDataRateSampleFeed(cluster.id, !cluster.spec.enabled);
  const rate = rates?.samples.at(-1);

  return (
    <div className="space-y-2 rounded-md border bg-muted/30 p-3">
      <p className="text-sm font-medium">{connFailed ? 'Connection failed' : 'Connection'}</p>
      <dl className="space-y-0.5 text-xs text-muted-foreground">
        {/* Coarse uptime; "0m" while down. */}
        <DetailRow
          label="Uptime"
          ms={detail.connected ? detail.stateSinceMs : null}
          fallback={detail.unconfirmed ? '—' : '0m'}
          format={humanMinutes}
        />
        <div className="flex gap-2">
          <dt className="w-28 shrink-0">Next check</dt>
          <dd className="tabular-nums">
            <NextCheck clusterId={cluster.id} />
          </dd>
        </div>
        <div className="flex gap-2">
          <dt className="w-28 shrink-0">Data rate</dt>
          <dd className="tabular-nums">
            {/* Each arrow is its line's color, so the numbers are the sparkline's legend. */}
            {rate ? (
              <>
                <span className="text-(--series-received)">↓</span> {formatRate(rate.receivedBytesPerSecond)} ·{' '}
                <span className="text-(--series-sent)">↑</span> {formatRate(rate.sentBytesPerSecond)}
              </>
            ) : (
              '—'
            )}
            {rates && <DataRateSparkline series={rates} />}
          </dd>
        </div>
      </dl>
      <EventRunList title="Recent attempts" runs={attempts} labelOf={(a) => (a.ok ? 'Success' : a.reason)} />
      <RetryButton clusterId={cluster.id} />
    </div>
  );
}

// formatBytes renders 0 as an em dash, which reads as "unknown" here — a cache nothing holds
// open has no WAL or shm file, and zero is the answer.
const partBytes = (n: number) => (n > 0 ? formatBytes(n) : '0 B');

// On-disk size, streamed for the same reason as the object counts: the cache
// record stops changing once its sync settles. One subscription per row, and the
// panel is a dialog — nothing mounts while closed.
//
// **The headline is the total**, which is what the cache costs on disk; the sqlite file alone
// reads low and stale while a sync fills the WAL, since it does not grow until a checkpoint
// lands. The breakdown is on hover because it answers a different question — whether
// checkpointing is keeping up.
function CacheSizeCell({ contents }: { contents: CacheContents | null }) {
  if (!contents?.exists) return <>—</>;
  const breakdown = `SQLite ${partBytes(contents.dbBytes)} · WAL ${partBytes(contents.walBytes)} · shm ${partBytes(contents.shmBytes)}`;
  // A native title, as the attempt timestamps use: the dialog inerts everything outside its
  // subtree, so a body-portaled tooltip would not show.
  return <span title={breakdown}>{formatBytes(contents.bytes)}</span>;
}

// Which per-kind record's transition log to show. Deterministic and sticky (first
// sorted offender) — picking "whichever unhealthy record arrived first" would
// re-key the subscription per frame. Falls back to Events, always present.
function timelineSyncFor(all: CachedKind[], health: ClusterCacheHealth): CachedKind | null {
  const firstOffender = health.unhealthyKindRefs[0];
  if (firstOffender) {
    // Match the whole kind, not the plural: a CRD may reuse a built-in's plural
    // under its own api group, and a loose match would open the wrong timeline.
    const match = all.find((s) => gvrKey(s.spec) === gvrKey(firstOffender));
    if (match) return match;
  }
  return all.find((s) => gvrKey(s.spec) === gvrKey(EVENTS_GVR)) ?? null;
}

// "2,203 objects across 120 kinds" (includes cached events, matching the engine's rollup).
function cacheSummary(objectCount: number, kindCount: number): string {
  return `${countLabel(objectCount, 'object')} across ${countLabel(kindCount, 'kind')}`;
}

// The reasons that are not a fault: caught up, or on the way there. Named as the set to
// exclude rather than the set to report, so a failure reason added later shows up unlisted
// rather than silently vanishing from the panel.
const SETTLING_REASONS = new Set(['Watching', 'Syncing', 'Resyncing', 'Resuming']);

// Expanded sync diagnostics. Inline for the same modal-inert reason as
// ConnectionDetail. Everything subscribes only while mounted — that's what makes
// the hundred-plus-record per-kind stream affordable.
export function SyncDetail({
  clusterId,
  health,
  cacheId,
  contents,
}: {
  clusterId: string;
  health: ClusterCacheHealth;
  cacheId: string;
  contents: CacheContents | null;
}) {
  const kindSyncs = useClusterCachedKindCollectionWatch(cacheId);
  const cacheSync = useClusterCacheSyncStateWatch(clusterId, cacheId);
  const timelineKind = timelineSyncFor(kindSyncs, health);
  const syncHistory = useObjectEventFeedSync(timelineKind?.id);
  const discoveryHistory = useObjectEventFeedDiscovery(cacheId);
  // Each offender with its own reason — which the rollup cannot carry, since it folds a
  // hundred kinds into one verdict. A kind with no reason yet has not answered, and a kind
  // still starting has not failed: a cache being armed, cleared, or resumed reports every one
  // of its kinds that way, and reading those as offenders would list all of them.
  const failingKinds = (cacheSync?.kinds ?? []).filter(
    (kind) => kind.reason && kind.reason !== 'Paused' && !SETTLING_REASONS.has(kind.reason),
  );
  // Paused kinds get their own section rather than joining SETTLING_REASONS: a kind the
  // user switched off is neither a fault nor settling, and folding it into the settling set
  // would hide it instead of putting the resume control beside it.
  const pausedKinds = (cacheSync?.kinds ?? []).filter((kind) => kind.reason === 'Paused');
  const [, setKindSyncEnabled] = useMutation(ClusterCachedKindSyncEnabledSetMutation);
  // The mutation takes the per-kind RECORD's id, and a sync-detail row carries only its
  // GVR — so the control resolves one against the other. A row with no record yet gets no
  // control rather than a guessed id.
  const idOf = (kind: KindSyncStatus) => kindSyncs.find((s) => gvrKey(s.spec) === gvrKey(kind))?.id ?? null;
  const setSyncEnabled = (id: string, syncEnabled: boolean) => {
    void setKindSyncEnabled({ id, syncEnabled });
  };
  // Newest write anywhere, beside the OLDEST proof — a cache is only as verified
  // as its least-recently proven watch.
  const lastUpdateMs = parseTimeOrNull(health.lastUpdateAt ?? null);
  const lastLiveMs = parseTimeOrNull(health.lastLiveAt ?? null);
  // Staleness is engine-derived, never inferred from a stamp's age — a
  // quiet-but-healthy cache legitimately has an old lastUpdateAt.
  const stale = health.reason === 'Stale';
  const offenders = offenderList(health);
  return (
    <div className="space-y-2 rounded-md border bg-muted/30 p-3">
      <p className="text-sm font-medium">Sync status</p>
      {stale ? (
        <p className={`text-xs ${TONE.attention.text}`}>
          Possibly stale —{' '}
          {offenders ? `${offenders} not receiving updates.` : 'the watch may have stopped delivering updates.'}
        </p>
      ) : null}
      <dl className="space-y-0.5 text-xs text-muted-foreground">
        {/* Only when the cache has objects (empty is covered by the freshness line). */}
        {contents && contents.objectCount > 0 ? (
          <DetailRow label="Cached" ms={null} fallback={cacheSummary(contents.objectCount, contents.kindCount)} />
        ) : null}
        {/* The two freshness stamps must not be conflated: "Last update" is data
            written (a quiet cluster legitimately goes hours without), "Sync verified"
            is the watch's last proof of life (delta or bookmark) — only the pair says
            an old update time is fine. */}
        {/* Per-kind sync health — kinds fail independently, so one forbidden CRD is
            invisible in every other reading. Omitted until something has streamed in. */}
        {health.totalKinds > 0 ? (
          <DetailRow label="Kinds syncing" ms={null} fallback={kindsSyncingLabel(health)} />
        ) : null}
        <DetailRow label="Last update received" ms={lastUpdateMs} fallback="No updates received yet." />
        {/* No fallback: omit rather than assert a verification that never happened. */}
        <DetailRow label="Sync verified" ms={lastLiveMs} />
      </dl>
      {cacheSync?.discovery.reason ? <DiscoveryVerdict discovery={cacheSync.discovery} /> : null}
      {failingKinds.length > 0 ? (
        <FailingKindList kinds={failingKinds} idOf={idOf} onSetSyncEnabled={setSyncEnabled} />
      ) : null}
      {pausedKinds.length > 0 ? (
        <PausedKindList kinds={pausedKinds} idOf={idOf} onSetSyncEnabled={setSyncEnabled} />
      ) : null}
      {syncHistory.events.length > 0 ? (
        <EventRunList
          title={timelineKind ? `Recent sync events — ${timelineKind.spec.resource}` : 'Recent sync events'}
          runs={syncHistory.events}
          labelOf={(e) => e.reason}
          showDuration={false}
        />
      ) : (
        // Only once the bookmark says the history really is empty — before that a
        // still-arriving timeline would read as "none".
        syncHistory.snapshotComplete && <p className="text-xs text-muted-foreground">No sync events yet.</p>
      )}
      {discoveryHistory.events.length > 0 ? (
        <EventRunList
          title="Recent kind discovery"
          runs={discoveryHistory.events}
          labelOf={(e) => e.reason}
          showDuration={false}
        />
      ) : null}
    </div>
  );
}

// The kind-discovery sweep's own verdict. Not any kind's: a cluster whose `/apis` document
// will not load, or whose cache file will not open, has no kind in a position to report it.
function DiscoveryVerdict({ discovery }: { discovery: { reason: string; message: string } }) {
  const tone = discovery.reason === 'StoreFailed' ? TONE.error.text : 'text-muted-foreground';
  return (
    <p className={`text-xs ${tone}`}>
      Kind discovery: {discovery.reason}
      {discovery.message ? ` — ${discovery.message}` : ''}
    </p>
  );
}

// How many kinds a section lists before it folds. A layout default rather than a ceiling —
// these rows carry the only control that can pause or resume a kind, so what is folded away
// has to stay reachable, and the section below expands.
const KIND_LIST_CAP = 5;

// One line per kind that is not Watching, each with the reason IT reported. A cache's kinds
// fail independently, so a single forbidden CRD is invisible in every other reading here.
function FailingKindList({ kinds, idOf, onSetSyncEnabled }: KindListProps) {
  return (
    <KindSection title="Kinds not syncing" kinds={kinds}>
      {(kind) => (
        <>
          <dd className="truncate">{kind.message ? `${kind.reason} — ${kind.message}` : kind.reason}</dd>
          {/* Pausing is what a user reaches for over a kind that is storming or forbidden,
              so the entry point sits on the row that reports it. */}
          <SyncEnabledButton kind={kind} id={idOf(kind)} syncEnabled={false} onSet={onSetSyncEnabled} />
        </>
      )}
    </KindSection>
  );
}

type KindListProps = {
  kinds: KindSyncStatus[];
  // The per-kind record's id for a row, or null while that stream has yet to carry it.
  idOf: (kind: KindSyncStatus) => string | null;
  onSetSyncEnabled: (id: string, syncEnabled: boolean) => void;
};

// The kinds the user switched off. Their own section, not a fault list: nothing here is
// wrong, and the rows are still cached and readable.
function PausedKindList({ kinds, idOf, onSetSyncEnabled }: KindListProps) {
  return (
    <KindSection title="Paused kinds" kinds={kinds}>
      {(kind) => (
        <>
          {/* What the pause kept, which is the whole difference from clearing the kind. */}
          <dd className="truncate">{countLabel(kind.objectCount, 'object')} kept</dd>
          <SyncEnabledButton kind={kind} id={idOf(kind)} syncEnabled onSet={onSetSyncEnabled} />
        </>
      )}
    </KindSection>
  );
}

// Stop or resume one kind. Nothing renders while the record's id is still owed: the
// mutation keys on that id, and a control without one has no kind to name.
function SyncEnabledButton({
  kind,
  id,
  syncEnabled,
  onSet,
}: {
  kind: KindSyncStatus;
  id: string | null;
  syncEnabled: boolean;
  onSet: (id: string, syncEnabled: boolean) => void;
}) {
  if (!id) return null;
  const verb = syncEnabled ? 'Resume' : 'Pause';
  return (
    <button
      type="button"
      // The plural is in the label, not the text: the row already renders it, and a second
      // copy would make every kind match twice in the section it is listed under.
      aria-label={`${verb} ${kind.resource}`}
      className="ml-auto shrink-0 underline underline-offset-2 hover:text-foreground"
      onClick={() => onSet(id, syncEnabled)}
    >
      {verb}
    </button>
  );
}

// A titled list of kinds, folded to the cap and expandable past it. A labelled group so a
// reader — and a test — can tell which section a kind is listed under, since the same plural
// can appear in either.
function KindSection({
  title,
  kinds,
  children,
}: {
  title: string;
  kinds: KindSyncStatus[];
  children: (kind: KindSyncStatus) => ReactNode;
}) {
  const [expanded, setExpanded] = useState(false);
  const hidden = kinds.length - KIND_LIST_CAP;
  const shown = expanded ? kinds : kinds.slice(0, KIND_LIST_CAP);
  return (
    <div className="space-y-0.5" role="group" aria-label={title}>
      <p className="text-xs font-medium">{title}</p>
      <dl className="space-y-0.5 text-xs text-muted-foreground">
        {shown.map((kind) => (
          <KindLine key={gvrKey(kind)} kind={kind}>
            {children(kind)}
          </KindLine>
        ))}
      </dl>
      {hidden > 0 ? (
        <button
          type="button"
          className="text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground"
          onClick={() => setExpanded(!expanded)}
        >
          {expanded ? 'Show less' : `Show ${hidden} more`}
        </button>
      ) : null}
    </div>
  );
}

// One kind's line: the plural, then whatever the section has to say about it.
function KindLine({ kind, children }: { kind: KindSyncStatus; children: ReactNode }) {
  return (
    <div className="flex gap-2">
      {/* The plural alone reads best, but the pair is what identifies a kind — so the api
          group is the title, where it disambiguates without crowding the line. */}
      <dt className="shrink-0 font-mono" title={kind.apiVersion}>
        {kind.resource}
      </dt>
      {children}
    </div>
  );
}

// A database icon with a diagonal line through it — "clear the cache".
function ClearCacheIcon() {
  return (
    <span className="relative inline-flex size-4" aria-hidden>
      <Database className="size-full" />
      <Slash className="absolute inset-0 size-full" />
    </span>
  );
}
// Which of a row's two disclosures is open; at most one.
type DetailPane = 'connection' | 'sync' | null;

// Status label that opens its detail row — one component for both columns so their
// styling can't drift.
function DisclosureLabel({
  tone,
  label,
  expanded,
  onToggle,
}: {
  tone: Tone;
  label: string;
  expanded: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      aria-expanded={expanded}
      onClick={onToggle}
      data-tone={tone}
      className={`${TONE[tone].text} inline-flex items-center gap-1 rounded-sm underline decoration-dotted underline-offset-2 outline-none focus-visible:ring-2 focus-visible:ring-ring`}
    >
      {label}
      <ChevronDown className={`size-3 transition-transform ${expanded ? 'rotate-180' : ''}`} aria-hidden />
    </button>
  );
}

function ClusterRow({
  cluster,
  group,
  initialDetail,
  onSetEnabled,
  onToggle,
  onClearCache,
  onRemove,
}: {
  cluster: Cluster;
  group: Group;
  // The pane the dialog was opened on, for this row alone.
  initialDetail: DetailPane;
  onSetEnabled: (enabled: boolean) => void;
  onToggle: (enabled: boolean) => void;
  onClearCache: () => void;
  onRemove: () => void;
}) {
  const name = displayName(cluster);
  // ONE cache-contents subscription per row, shared by the size cell and the sync
  // detail. Separate hooks with identical variables resolve to one urql operation,
  // and a second subscriber joins mid-stream with no replay — the later mount would
  // sit on null until the next frame.
  const { activeCache } = cluster;
  const contents = useClusterCacheStatsStateWatch(cluster.id, activeCache?.id ?? '', !activeCache);
  const connection = connectionStatus(cluster, group);
  const status = syncStatus(cluster, group);
  const overall = overallStatus(connection, status);
  const orphaned = group === 'orphaned';
  const connFailed = connection.tone === 'error';
  const [openDetail, setOpenDetail] = useState<DetailPane>(initialDetail);
  const rowRef = useRef<HTMLTableRowElement>(null);
  // Once per mount: `initialDetail` never changes.
  useEffect(() => {
    if (initialDetail) rowRef.current?.scrollIntoView({ block: 'nearest' });
  }, [initialDetail]);
  const toggleDetail = (pane: Exclude<DetailPane, null>) => setOpenDetail(openDetail === pane ? null : pane);
  // Sync label is a disclosure only once the rollup has streamed in — a
  // pending/never-synced row has nothing to open.
  const health = cluster.activeCache?.health;
  const pending = isPending(cluster);
  const { enabled } = cluster.spec;
  // Sync toggles only for an enabled, identified cluster still in the kubeconfig.
  const syncing = cluster.spec.syncEnabled && enabled && !orphaned && !pending;
  const canToggle = enabled && !orphaned && !pending;

  return (
    <>
      <TableRow ref={rowRef}>
        <TableCell className={`${STATUS_CELL_CLASS} align-top`}>
          <span
            role="img"
            aria-label={overall.summary}
            title={overall.summary}
            data-tone={overall.tone}
            className={`mt-1 block size-2.5 shrink-0 rounded-full ${TONE[overall.tone].dot}`}
          />
        </TableCell>
        <TableCell className="max-w-0 truncate font-medium align-top">{name}</TableCell>
        <TableCell className="align-top">
          <DisclosureLabel
            tone={connection.tone}
            label={connection.label}
            expanded={openDetail === 'connection'}
            onToggle={() => toggleDetail('connection')}
          />
        </TableCell>
        <TableCell className="align-top">
          {/* Disclosure once the rollup has streamed in, else plain text. */}
          {health ? (
            <DisclosureLabel
              tone={status.tone}
              label={status.label}
              expanded={openDetail === 'sync'}
              onToggle={() => toggleDetail('sync')}
            />
          ) : (
            <ToneText tone={status.tone}>{status.label}</ToneText>
          )}
        </TableCell>
        <TableCell className="tabular-nums">
          <CacheSizeCell contents={contents} />
        </TableCell>
        <TableCell>
          <div className="flex items-center justify-end gap-0.5">
            {/* Enable/disable: a disabled cluster stays tracked but dormant. */}
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`${enabled ? 'Disable' : 'Enable'} ${name}`}
              disabled={orphaned}
              onClick={() => onSetEnabled(!enabled)}
            >
              {enabled ? <Power className="size-4" aria-hidden /> : <PowerOff className="size-4" aria-hidden />}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`${syncing ? 'Pause' : 'Resume'} sync for ${name}`}
              disabled={!canToggle}
              onClick={() => onToggle(!cluster.spec.syncEnabled)}
            >
              {syncing ? <Pause className="size-4" aria-hidden /> : <Play className="size-4" aria-hidden />}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`Clear cache for ${name}`}
              disabled={!contents?.exists}
              onClick={onClearCache}
            >
              <ClearCacheIcon />
            </Button>
            {/* Always rendered so columns align; remove applies only to an orphan
              (a present context would just be re-discovered). */}
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`Remove ${name}`}
              className="hover:text-destructive"
              disabled={!orphaned}
              onClick={onRemove}
            >
              <Trash2 className="size-4" aria-hidden />
            </Button>
          </div>
        </TableCell>
      </TableRow>
      {openDetail === 'connection' ? (
        <TableRow className="hover:bg-transparent">
          <TableCell className={STATUS_CELL_CLASS} />
          <TableCell colSpan={COLUMN_COUNT - 1} className="pt-0">
            <ConnectionDetail cluster={cluster} connFailed={connFailed} />
          </TableCell>
        </TableRow>
      ) : null}
      {openDetail === 'sync' && health ? (
        <TableRow className="hover:bg-transparent">
          <TableCell className={STATUS_CELL_CLASS} />
          <TableCell colSpan={COLUMN_COUNT - 1} className="pt-0">
            <SyncDetail clusterId={cluster.id} health={health} cacheId={health.cacheID} contents={contents} />
          </TableCell>
        </TableRow>
      ) : null}
    </>
  );
}

// Active = in the current kubeconfig; anything else shows as Orphaned so no known
// cluster is silently dropped. Empty groups are omitted by the caller.
const GROUPS: { key: Group; label: string; suffix: string; match: (c: Cluster) => boolean }[] = [
  {
    key: 'active',
    label: 'Active',
    suffix: 'in kubeconfig',
    match: (c) => c.status.source.kubeconfig?.isPresent ?? false,
  },
  {
    key: 'orphaned',
    label: 'Orphaned',
    suffix: 'cache on disk, no longer in kubeconfig',
    match: (c) => !c.status.source.kubeconfig?.isPresent,
  },
];

// Open state is caller-controlled (`AppDialogs`); mounts only while open. Per-row
// event/schedule streams mount only when a row's diagnostics open.
export function ClusterSyncPanel({ open, onOpenChange }: AppDialogProps) {
  const { clusters, connected } = useClusters();
  const rows = clusters ?? [];
  const groups = GROUPS.map((g) => ({ ...g, clusters: rows.filter(g.match) })).filter((g) => g.clusters.length > 0);

  // `clusters !== null` IS the snapshot-complete signal subscriptionPhase wants (the provider
  // holds the list back until the Bookmark), so "No clusters yet." below can only be
  // reached once the fleet is genuinely known.
  const phase = subscriptionPhase(clusters !== null, connected);
  // Read once as the dialog mounts: it mounts per open, and the detail is that open's.
  const host = useDialogHost();
  const [detail] = useState(host?.clusterDetail ?? null);

  // Actions write through; the clusterCollectionWatch push is the source of truth. A failure needs
  // no handling here — the client's errorReportExchange puts every operation error on the
  // bus, so a second report at the call site would show one failure twice.
  const [, clusterEnabledSetMut] = useMutation(ClusterEnabledSetMutation);
  const [, clusterSyncEnabledSetMut] = useMutation(ClusterSyncEnabledSetMutation);
  const [, clusterCacheClearMut] = useMutation(ClusterCacheClearMutation);
  const [, clusterDeleteMut] = useMutation(ClusterDeleteMutation);

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Clusters"
      description="Clusters in your kubeconfig and any leftover local caches."
      // Widened past the dialog default to fit the table.
      className="w-[calc(100%-4rem)] max-w-4xl sm:max-w-4xl"
    >
      {/* Transport dropped with the registry loaded: the table is last-known state. */}
      {phase === 'reconnecting' ? (
        <div className={`mb-2 flex items-center gap-1.5 text-xs ${TONE.attention.text}`}>
          <Spinner size="xs" className="mr-0" />
          Reconnecting…
        </div>
      ) : null}
      {/* Guarded branches, not a nested ternary; `rows` is only non-empty once
          loaded, so table and connecting states never overlap. */}
      {phase === 'connecting' && (
        <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
          <Spinner size="sm" className="mr-0" />
          Connecting…
        </div>
      )}
      {phase !== 'connecting' && rows.length === 0 && (
        <p className="py-6 text-sm text-muted-foreground">No clusters yet.</p>
      )}
      {rows.length > 0 && (
        <Table className="table-fixed">
          <TableHeader>
            <TableRow>
              <TableHead className={STATUS_CELL_CLASS}>
                <span className="sr-only">Status</span>
              </TableHead>
              <TableHead>Cluster</TableHead>
              <TableHead className="w-28">Connection</TableHead>
              <TableHead className="w-32">Sync status</TableHead>
              <TableHead className="w-20">Cache</TableHead>
              <TableHead className="w-36 text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          {groups.map((g) => (
            <TableBody key={g.key} aria-label={`${g.label} clusters`}>
              <TableRow className="hover:bg-transparent">
                <TableHead colSpan={COLUMN_COUNT} className="h-auto py-1 text-xs tracking-wide text-muted-foreground">
                  <span className="font-semibold uppercase">{g.label}</span>
                  <span className="font-normal"> · {g.suffix}</span>
                </TableHead>
              </TableRow>
              {g.clusters.map((c) => (
                <ClusterRow
                  key={c.id}
                  cluster={c}
                  group={g.key}
                  initialDetail={detail?.clusterId === c.id ? detail.pane : null}
                  onSetEnabled={(enabled) => clusterEnabledSetMut({ id: c.id, enabled })}
                  onToggle={(syncEnabled) => clusterSyncEnabledSetMut({ id: c.id, syncEnabled })}
                  // The cache's own id, not the cluster's: a UID migration leaves the
                  // cluster owning more than one. The button is disabled until the
                  // active cache reports a file, so the guard never fires in practice.
                  onClearCache={() => {
                    /* v8 ignore else -- the button is disabled until the active cache reports a file */
                    if (c.activeCache) void clusterCacheClearMut({ id: c.activeCache.id });
                  }}
                  onRemove={() => clusterDeleteMut({ id: c.id })}
                />
              ))}
            </TableBody>
          ))}
        </Table>
      )}
    </Dialog>
  );
}
