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

import { cleanup, render, screen, act, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { Provider as UrqlProvider } from 'urql';
import { afterEach, beforeAll, beforeEach, describe, expect, it, onTestFinished, vi } from 'vitest';

import type { ClusterDetail } from '@/lib/dialog';
import { clusterRecord, flushSubscriptionsSynchronously, mockTauriCore } from '@/test-utils';

// Mocks ---------------------------------------------------------------

// Frames pushed here are asserted on immediately.
flushSubscriptionsSynchronously();

const { invokeMock, channels, channelFor, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { createGraphqlClient } = await import('@/lib/graphql/client');
const { ClustersProvider } = await import('@/lib/clusters');
const { ClusterSyncPanel, ConnectionDetail } = await import('./panel');
const { onError } = await import('@/lib/error-bus');
const { DialogProvider, useDialog } = await import('@/lib/dialog');

// Helpers -------------------------------------------------------------

const flush = () => act(async () => {});

// One data-rate sample, and a frame of the feed carrying samples.
const rate = (received: number, sent: number) => ({
  at: '2026-11-02T12:00:00Z',
  receivedBytesPerSecond: received,
  sentBytesPerSecond: sent,
});
const rateWindow = (...samples: ReturnType<typeof rate>[]) =>
  JSON.stringify({
    type: 'next',
    payload: { data: { clusterDataRateSampleFeed: { intervalSeconds: 2, windowSeconds: 120, samples } } },
  });

const subscriptionsTo = (field: string) =>
  invokeMock.mock.calls.filter(
    ([cmd, arg]) => cmd === 'graphql_subscribe' && (arg as { query: string }).query.includes(field),
  ).length;

type Row = {
  uuid: string; // '' = pending (the UID probe never succeeded → server.uid null)
  name: string;
  enabled: boolean; // the sync toggle
  // The cluster's own switch, off: nothing connects. Defaults to on.
  disabled?: boolean;
  present: boolean;
  isCurrent?: boolean;
  lastUpdateAt?: string;
  lastLiveAt?: string;
  // The live `Connected` condition the connection column reads. Defaults to
  // 'True' (reachable); set 'False' to model a dropped connection (internet
  // off / probe failed), 'Unknown' for not-yet-probed.
  connected?: 'True' | 'False' | 'Unknown';
  // Model an engine-level sync failure (Synced=False/SyncFailed). Defaults to a
  // healthy Synced=True/Watching condition.
  syncFailed?: boolean;
  // Model a stalled-but-connected watch (Synced=False/Stale) — the cache may be
  // behind even though the connection is up.
  stale?: boolean;
  // Model a cache whose kinds are all still paused (Synced=False/Paused) — what a
  // just-resumed cluster reads as until its children reconcile.
  kindsPaused?: boolean;
  // Model a verdict this build doesn't recognise — a reason a newer sidecar emits.
  unknownReason?: boolean;
  // Model a sync waiting on credentials the connection hasn't produced yet
  // (Synced=False/NoConnection) — a sync's normal startup state.
  noConnection?: boolean;
  // Model conditions left behind by a previous sidecar process: beehive downgrades a
  // liveness condition it didn't write to Unknown, keeping the pre-restart reason and
  // stamps, and flags it `unconfirmed`.
  unconfirmed?: boolean;
  // Connection diagnostics surfaced in the Disconnected popover.
  connMessage?: string; // Connected condition's `message` (the probe error)
  disconnectedSince?: string; // Connected condition's `transitionedAt` (ISO)
};

// The cache's folded sync verdict for a row. The sidecar folds every kind's Synced into
// one reading, ignoring any condition a previous process wrote that this one hasn't
// re-confirmed — so an unconfirmed row rolls up as "nothing observed yet" rather than
// asserting its last-known verdict.
function healthOf(r: Row): {
  status: string;
  reason: string;
  unhealthyKindRefs: { apiVersion: string; resource: string; reason: string }[];
  unhealthyKinds: number;
} {
  if (r.unconfirmed) return { status: 'Unknown', reason: 'Syncing', unhealthyKindRefs: [], unhealthyKinds: 0 };
  if (r.syncFailed)
    return {
      status: 'False',
      reason: 'SyncFailed',
      unhealthyKindRefs: [{ apiVersion: 'example.com/v1', resource: 'widgets', reason: 'SyncFailed' }],
      unhealthyKinds: 1,
    };
  if (r.noConnection) {
    return { status: 'False', reason: 'NoConnection', unhealthyKindRefs: [], unhealthyKinds: 1 };
  }
  if (r.stale)
    return {
      status: 'False',
      reason: 'Stale',
      unhealthyKindRefs: [{ apiVersion: 'v1', resource: 'pods', reason: 'Stale' }],
      unhealthyKinds: 1,
    };
  if (r.kindsPaused) return { status: 'False', reason: 'Paused', unhealthyKindRefs: [], unhealthyKinds: 2 };
  if (r.unknownReason)
    return {
      status: 'False',
      reason: 'QuotaExceeded',
      unhealthyKindRefs: [{ apiVersion: 'v1', resource: 'pods', reason: 'QuotaExceeded' }],
      unhealthyKinds: 1,
    };
  return { status: 'True', reason: 'Watching', unhealthyKindRefs: [], unhealthyKinds: 0 };
}

// Deliver the `open` frame the host sends on each established connection (ahead of
// the snapshot). It marks the registry streams connected, so the panel reads
// "connected, empty" rather than "still connecting" — the distinction under test.
function openStreams() {
  channelFor('clusterCollectionWatch').onmessage!(JSON.stringify({ type: 'open' }));
  channelFor('clusterCacheCollectionWatch').onmessage!(JSON.stringify({ type: 'open' }));
  channelFor('clusterCacheHealthKeyedStateWatch').onmessage!(JSON.stringify({ type: 'open' }));
}

// Push each row as an Added change on the three subscriptions: a Cluster change on
// clusterCollectionWatch, plus (for a probed row) its ClusterCache change on
// clusterCacheCollectionWatch and that cache's folded sync verdict on
// clusterCacheEventsSyncsWatch. The provider joins them down the chain — cache onto
// cluster by spec.serverUid, sync onto cache by its owner — which is what gives a row its
// activeCache and its sync state.
function pushClusters(rows: Row[]) {
  const clusterCh = channelFor('clusterCollectionWatch');
  const cacheCh = channelFor('clusterCacheCollectionWatch');
  const healthCh = channelFor('clusterCacheHealthKeyedStateWatch');
  rows.forEach((r, i) => {
    const id = r.uuid || `pending-${i}`;
    clusterCh.onmessage!(
      JSON.stringify({
        type: 'next',
        payload: {
          data: {
            clusterCollectionWatch: {
              type: 'Added',
              cluster: {
                id,
                spec: {
                  name: r.name,
                  syncEnabled: r.enabled,
                  enabled: !r.disabled,
                  source: { kubeconfig: { context: r.name } },
                },
                status: {
                  source: {
                    kubeconfig: {
                      cluster: `${r.name}-cluster`,
                      user: `${r.name}-user`,
                      isPresent: r.present,
                      isDefault: r.isCurrent ?? false,
                    },
                  },
                  server: { uid: r.uuid || null },
                },
                conditions: [
                  {
                    type: 'Connected',
                    status: r.unconfirmed ? 'Unknown' : (r.connected ?? 'True'),
                    reason: '',
                    message: r.connMessage ?? '',
                    liveness: true,
                    unconfirmed: r.unconfirmed ?? false,
                    transitionedAt: r.disconnectedSince ?? null,
                  },
                ],
              },
            },
          },
        },
      }),
    );
    if (!r.uuid) return;
    cacheCh.onmessage!(
      JSON.stringify({
        type: 'next',
        payload: {
          data: {
            clusterCacheCollectionWatch: {
              type: 'Added',
              cache: {
                id: `cache-${r.uuid}`,
                clusterID: id,
                spec: { serverUid: r.uuid },
                // No status/conditions/stats here: the panel reads freshness and the
                // verdict off the sync record below, and the whole of the cache's
                // contents (existence, size, counts) streams per row via
                // clusterCacheStatsStateWatch. The query selects nothing else.
              },
            },
          },
        },
      }),
    );
    healthCh.onmessage!(
      JSON.stringify({
        type: 'next',
        payload: {
          data: {
            clusterCacheHealthKeyedStateWatch: {
              cacheID: `cache-${r.uuid}`,
              ...healthOf(r),
              lastUpdateAt: r.lastUpdateAt ?? null,
              lastLiveAt: r.lastLiveAt ?? null,
            },
          },
        },
      }),
    );
  });
  // Close both delta snapshots. The provider holds the cluster list back until the
  // clusterCollectionWatch Bookmark, so without this the panel stays in its connecting state.
  // (clusterCacheHealthKeyedStateWatch is a state watch, not a collection watch — no
  // Bookmark rides it.)
  clusterCh.onmessage!(
    JSON.stringify({
      type: 'next',
      payload: { data: { clusterCollectionWatch: { type: 'Bookmark', cluster: null } } },
    }),
  );
  cacheCh.onmessage!(
    JSON.stringify({
      type: 'next',
      payload: { data: { clusterCacheCollectionWatch: { type: 'Bookmark', cache: null } } },
    }),
  );
}

// Push one run on the cluster's connection timeline, which rides clusterEventFeed;
// the cache-side timelines ride objectEventFeed. The channel is picked by operation name.
function pushConnectionEvent(ev: {
  id: string;
  type: 'Normal' | 'Warning';
  reason: string;
  message: string;
  count: number;
  firstAt: string;
  lastAt: string;
}) {
  channelFor('ClusterEventFeedConnection').onmessage!(
    JSON.stringify({ type: 'next', payload: { data: { clusterEventFeed: { type: 'Run', event: ev } } } }),
  );
}

// Close an event timeline's snapshot. Until this lands the timeline is still
// arriving, so a consumer must not render its empty state.
function pushEventBookmark(operation: string) {
  const field = operation === 'ClusterEventFeedConnection' ? 'clusterEventFeed' : 'objectEventFeed';
  channelFor(operation).onmessage!(
    JSON.stringify({ type: 'next', payload: { data: { [field]: { type: 'Bookmark', event: null } } } }),
  );
}

// Push one run on the one kind's
// transition timeline. Call after a row's sync diagnostics are open (mounts the
// sync-events subscription).
function pushSyncEvent(ev: {
  id: string;
  type: 'Normal' | 'Warning';
  reason: string;
  message: string;
  count: number;
  firstAt: string;
  lastAt: string;
}) {
  channelFor('ObjectEventFeedSync').onmessage!(
    JSON.stringify({
      type: 'next',
      payload: { data: { objectEventFeed: { type: 'Run', event: ev } } },
    }),
  );
}

// The cache-contents state watch. It is a subscription, not a field on the cache record: the
// record stops changing once the sync settles, so a field there froze at whatever the
// cache held when the window subscribed.
function pushCacheStats(
  objectCount: number,
  kindCount: number,
  bytes = 0,
  parts = { dbBytes: bytes, walBytes: 0, shmBytes: 0 },
) {
  channelFor('clusterCacheStatsStateWatch').onmessage!(
    JSON.stringify({
      type: 'next',
      payload: { data: { clusterCacheStatsStateWatch: { exists: true, bytes, objectCount, kindCount, ...parts } } },
    }),
  );
}

// The state watch for ONE cache. Each row subscribes with its own cacheID, so a multi-cluster
// test can't use channelFor (which takes the last match) — this matches on the
// subscription's variables instead.
function pushCacheStatsFor(cacheId: string, bytes: number) {
  const subs = invokeMock.mock.calls.filter(([cmd]) => cmd === 'graphql_subscribe');
  const idx = subs.findIndex(([, arg]) => {
    const a = arg as { query: string; variables?: Record<string, unknown> };
    return a.query.includes('clusterCacheStatsStateWatch') && a.variables?.cacheID === cacheId;
  });
  if (idx < 0) throw new Error(`no stats subscription for ${cacheId}`);
  channels[idx].onmessage!(
    JSON.stringify({
      type: 'next',
      payload: {
        data: {
          clusterCacheStatsStateWatch: {
            exists: true,
            bytes,
            dbBytes: bytes,
            walBytes: 0,
            shmBytes: 0,
            objectCount: 0,
            kindCount: 0,
          },
        },
      },
    }),
  );
}

// The variables the most recent mutation named `name` went out with. The panel sends every
// mutation over the same invoke, so the assertion has to pick its own out of the log.
function lastMutation(name: string): Record<string, unknown> | undefined {
  const calls = invokeMock.mock.calls.filter(
    ([cmd, arg]) => cmd === 'graphql_query' && (arg as { body: string }).body.includes(name),
  );
  const last = calls.at(-1);
  if (!last) return undefined;
  return JSON.parse((last[1] as { body: string }).body).variables;
}

// Push a cache's folded verdict directly, for the fields the row fixture doesn't vary.
function pushHealth(cacheId: string, over: Record<string, unknown>) {
  channelFor('clusterCacheHealthKeyedStateWatch').onmessage!(
    JSON.stringify({
      type: 'next',
      payload: {
        data: {
          clusterCacheHealthKeyedStateWatch: {
            cacheID: cacheId,
            status: 'True',
            reason: 'Watching',
            unhealthyKindRefs: [],
            totalKinds: 0,
            unhealthyKinds: 0,
            pausedKinds: 0,
            lastUpdateAt: null,
            lastLiveAt: null,
            ...over,
          },
        },
      },
    }),
  );
}

// One per-kind sync record on the cache-scoped stream.
function pushCachedKind(id: string, resource: string, reason: string, apiVersion = 'v1') {
  channelFor('clusterCachedKindCollectionWatch').onmessage!(
    JSON.stringify({
      type: 'next',
      payload: {
        data: {
          clusterCachedKindCollectionWatch: {
            type: 'Added',
            kind: {
              id,
              spec: { apiVersion, resource },
              conditions: [
                {
                  type: 'Synced',
                  status: reason === 'Watching' ? 'True' : 'False',
                  reason,
                  message: '',
                  unconfirmed: false,
                },
              ],
            },
          },
        },
      },
    }),
  );
}

// The per-cache sync-detail state watch. The only thing on the wire that carries a per-kind
// verdict — the rollup names its offenders but not why each one is failing.
function pushSyncStatus(
  kinds: { apiVersion: string; resource: string; reason: string; message?: string }[],
  discoveryReason = 'Discovered',
  discoveryMessage = '',
  exists = true,
) {
  channelFor('clusterCacheSyncStateWatch').onmessage!(
    JSON.stringify({
      type: 'next',
      payload: {
        data: {
          clusterCacheSyncStateWatch: {
            exists,
            discovery: { reason: discoveryReason, message: discoveryMessage },
            kinds: kinds.map((k) => ({ message: '', objectCount: 0, ...k })),
          },
        },
      },
    }),
  );
}

// Push one run on a cache's kind-discovery timeline. What the cluster serves is the cache's
// own fact, so this rides the cache record rather than any kind's.
function pushDiscoveryEvent(ev: {
  id: string;
  type: 'Normal' | 'Warning';
  reason: string;
  message: string;
  count: number;
  firstAt: string;
  lastAt: string;
}) {
  channelFor('ObjectEventFeedDiscovery').onmessage!(
    JSON.stringify({ type: 'next', payload: { data: { objectEventFeed: { type: 'Run', event: ev } } } }),
  );
}

// Push one frame on the per-cluster clusterScheduleStateWatch state watch (the next-attempt
// time + the in-flight `probing` flag). Call after a row's diagnostics are open
// (mounts the schedule subscription).
function pushSchedule(nextRequeueAt: string | null, probing = false) {
  channelFor('clusterScheduleStateWatch').onmessage!(
    JSON.stringify({
      type: 'next',
      payload: { data: { clusterScheduleStateWatch: { nextRequeueAt, probing, failures: 0 } } },
    }),
  );
}

// The panel is controlled by its caller, so wrap it in a harness that owns the
// open state and exposes a "Clusters" button to open it — mirroring how the
// account menu drives it in the app.
function Harness() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Clusters
      </button>
      <ClusterSyncPanel open={open} onOpenChange={setOpen} />
    </>
  );
}

function renderPanel() {
  return render(
    <UrqlProvider value={createGraphqlClient()}>
      <ClustersProvider>
        <Harness />
      </ClustersProvider>
    </UrqlProvider>,
  );
}

// Mounts the panel the way AppDialogs does: only while the provider has it mounted, so
// the panel reads the open's cluster detail as it mounts.
function HostedHarness({ detail }: { detail: ClusterDetail }) {
  const { activeDialog, mountedDialog, openDialog, closeDialog } = useDialog();
  return (
    <>
      <button type="button" onClick={() => openDialog('clusters', detail)}>
        Details
      </button>
      {mountedDialog === 'clusters' && (
        <ClusterSyncPanel open={activeDialog === 'clusters'} onOpenChange={(open) => !open && closeDialog()} />
      )}
    </>
  );
}

async function openOnDetail(rows: Row[], detail: ClusterDetail) {
  const user = userEvent.setup();
  render(
    <UrqlProvider value={createGraphqlClient()}>
      <ClustersProvider>
        <DialogProvider>
          <HostedHarness detail={detail} />
        </DialogProvider>
      </ClustersProvider>
    </UrqlProvider>,
  );
  await flush();
  await act(async () => {
    openStreams();
    pushClusters(rows);
  });
  await user.click(screen.getByRole('button', { name: 'Details' }));
  return user;
}

// Render the panel, push rows, and open the dialog.
async function openWith(rows: Row[]) {
  const user = userEvent.setup();
  renderPanel();
  await flush();
  await act(async () => {
    openStreams();
    pushClusters(rows);
  });
  await user.click(screen.getByRole('button', { name: /clusters/i }));
  return user;
}

describe('ClusterSyncPanel', () => {
  // base-ui's dialog relies on pointer-capture / scroll APIs jsdom lacks.
  beforeAll(() => {
    Element.prototype.hasPointerCapture = vi.fn();
    Element.prototype.releasePointerCapture = vi.fn();
    Element.prototype.scrollIntoView = vi.fn();
  });

  beforeEach(() => {
    invokeMock.mockReset();
    channels.length = 0;
    let id = 0;
    invokeMock.mockImplementation(async (cmd: string) => {
      if (cmd === 'graphql_subscribe') {
        id += 1;
        return id;
      }
      if (cmd === 'graphql_unsubscribe') return undefined;
      if (cmd === 'graphql_query') {
        // Any mutation succeeds; extra fields are ignored by urql.
        return {
          status: 200,
          body: JSON.stringify({
            data: {
              clusterEnabledSet: { __typename: 'Cluster', id: 'u', spec: { enabled: false } },
              clusterSyncEnabledSet: { __typename: 'Cluster', id: 'u', spec: { syncEnabled: false } },
              clusterCacheClear: { __typename: 'ClusterCache', id: 'cache-u' },
              clusterDelete: true,
              clusterConnectionRetry: true,
              clusterCachedKindSyncEnabledSet: {
                __typename: 'ClusterCachedKind',
                id: 'g-crd',
                spec: { syncEnabled: true },
              },
            },
          }),
        };
      }
      throw new Error(`unexpected ${cmd}`);
    });
  });

  afterEach(cleanup);

  it('subscribes to clusterCollectionWatch', async () => {
    renderPanel();
    await flush();
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_subscribe',
      expect.objectContaining({ query: expect.stringContaining('clusterCollectionWatch') }),
    );
  });

  it('shows an empty state when the registry is connected but carries no clusters', async () => {
    const user = await openWith([]);
    expect(user).toBeDefined();
    expect(await screen.findByText(/no clusters yet/i)).toBeInTheDocument();
    // Connected + empty is a real answer, not a spinner.
    expect(screen.queryByText(/connecting…/i)).not.toBeInTheDocument();
  });

  it('shows a connecting state while the registry stream has not reported', async () => {
    const user = userEvent.setup();
    renderPanel();
    await flush();
    // No `open`/`next` on clusterCollectionWatch: the transport is still dialing, so nothing
    // has been reported. This must read as connecting, not as an empty registry.
    await user.click(screen.getByRole('button', { name: /clusters/i }));
    expect(await screen.findByText(/connecting…/i)).toBeInTheDocument();
    expect(screen.queryByText(/no clusters yet/i)).not.toBeInTheDocument();
  });

  it('flags reconnecting but keeps the table when the transport drops after loading', async () => {
    await openWith([{ uuid: 'u-prod', name: 'prod-us', enabled: true, present: true }]);
    expect(await screen.findByText('prod-us')).toBeInTheDocument();
    expect(screen.queryByText(/reconnecting…/i)).not.toBeInTheDocument();

    // The host closes the clusterCollectionWatch SSE stream: the registry stays loaded (the
    // hook holds last-known data through the outage), so the table persists, flagged.
    await act(async () => {
      channelFor('clusterCollectionWatch').onmessage!(JSON.stringify({ type: 'closed' }));
    });
    expect(await screen.findByText(/reconnecting…/i)).toBeInTheDocument();
    expect(screen.getByText('prod-us')).toBeInTheDocument();
  });

  it('renders the table columns', async () => {
    await openWith([{ uuid: 'u-prod', name: 'prod-us', enabled: true, present: true }]);
    expect(await screen.findByRole('columnheader', { name: /^cluster$/i })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: /^connection$/i })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: /sync status/i })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: /^cache$/i })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: /actions/i })).toBeInTheDocument();
  });

  it('splits clusters into active and orphaned row groups by kubeconfig presence', async () => {
    await openWith([
      { uuid: 'u-prod', name: 'prod-us', enabled: true, present: true },
      { uuid: 'u-stg', name: 'staging', enabled: false, present: true },
      { uuid: 'u-old', name: 'old-cluster', enabled: true, present: false },
    ]);

    const active = await screen.findByRole('rowgroup', { name: /active/i });
    const orphaned = screen.getByRole('rowgroup', { name: /orphaned/i });

    expect(within(active).getByText('prod-us')).toBeInTheDocument();
    expect(within(active).getByText('staging')).toBeInTheDocument();
    expect(within(active).queryByText('old-cluster')).not.toBeInTheDocument();

    expect(within(orphaned).getByText('old-cluster')).toBeInTheDocument();
    expect(within(orphaned).queryByText('prod-us')).not.toBeInTheDocument();

    // Status + formatted cache sizes (binary units). The sizes stream per row — the
    // cache record they used to ride stops changing once its sync settles.
    expect(within(active).getByText(/^current$/i)).toBeInTheDocument();
    expect(within(active).getByText(/^paused$/i)).toBeInTheDocument();
    expect(within(orphaned).getByText(/^stopped$/i)).toBeInTheDocument();
    act(() => {
      pushCacheStatsFor('cache-u-prod', 1_300_000);
      pushCacheStatsFor('cache-u-stg', 524_288);
    });
    expect(await screen.findByText(/1\.2 MB/)).toBeInTheDocument();
    expect(screen.getByText(/512\.0 KB/)).toBeInTheDocument();
  });

  // The headline answers what the cache costs on disk; the breakdown is diagnostic — a WAL
  // that dwarfs the sqlite file means checkpointing is falling behind.
  it('breaks the cache size down on hover', async () => {
    await openWith([{ uuid: 'u-size', name: 'prod', enabled: true, present: true }]);

    act(() => pushCacheStats(12, 3, 4_640, { dbBytes: 4_096, walBytes: 512, shmBytes: 32 }));

    expect(await screen.findByText(/4\.5 KB/)).toHaveAttribute('title', 'SQLite 4.0 KB · WAL 512 B · shm 32 B');

    // A cache nothing holds open has no WAL or shm file, and zero is the answer — not the em
    // dash formatBytes renders for it, which reads as "unknown".
    act(() => pushCacheStats(12, 3, 4_096, { dbBytes: 4_096, walBytes: 0, shmBytes: 0 }));

    expect(await screen.findByText(/4\.0 KB/)).toHaveAttribute('title', 'SQLite 4.0 KB · WAL 0 B · shm 0 B');
  });

  it('shows a short connection status for each cluster', async () => {
    await openWith([
      {
        uuid: 'u-prod',
        name: 'prod-us',
        enabled: true,
        present: true,
        isCurrent: true,
        connected: 'True',
      },
      // Previously reachable but now unreachable (e.g. internet off): the
      // Connected condition flips to False even though server.uid lingers.
      { uuid: 'u-remote', name: 'remote', enabled: true, present: true, connected: 'False' },
      { uuid: '', name: 'minikube', enabled: false, present: true, connected: 'Unknown' },
      { uuid: 'u-old', name: 'old-cluster', enabled: true, present: false },
    ]);

    // Scoped to cells so the group-header "Active" label doesn't collide.
    const active = await screen.findByRole('rowgroup', { name: /active/i });
    const orphaned = screen.getByRole('rowgroup', { name: /orphaned/i });

    expect(within(active).getByRole('cell', { name: 'Active' })).toBeInTheDocument(); // reachable
    expect(within(active).getByRole('cell', { name: 'Disconnected' })).toBeInTheDocument(); // connection dropped
    expect(within(active).getByRole('cell', { name: 'Connecting' })).toBeInTheDocument(); // not yet probed
    expect(within(orphaned).getByRole('cell', { name: 'Unavailable' })).toBeInTheDocument(); // gone
  });

  it('reflects the live sync state, not just the enabled toggle', async () => {
    await openWith([
      // Enabled + connected + watching → Synced.
      { uuid: 'u-prod', name: 'prod-us', enabled: true, present: true, connected: 'True' },
      // Enabled but disconnected: the engine keeps a stale Watching state, so
      // gate on the connection — this is Stalled, not Synced.
      { uuid: 'u-remote', name: 'remote', enabled: true, present: true, connected: 'False' },
      // Enabled + connected but the engine reported an engine-level failure.
      {
        uuid: 'u-broke',
        name: 'broke',
        enabled: true,
        present: true,
        connected: 'True',
        syncFailed: true,
      },
    ]);

    const active = await screen.findByRole('rowgroup', { name: /active/i });
    expect(within(active).getByRole('cell', { name: 'Current' })).toBeInTheDocument();
    expect(within(active).getByRole('cell', { name: 'Stalled' })).toBeInTheDocument();
    expect(within(active).getByRole('cell', { name: 'Error' })).toBeInTheDocument();
  });

  // A sync waiting on credentials is not a fault and not progress: the connection axis
  // owns that story, so this reads muted rather than claiming work is underway.
  it('reads a sync waiting on the connection as Connecting, not Syncing', async () => {
    await openWith([
      {
        uuid: 'u-cold',
        name: 'prod-us',
        enabled: true,
        present: true,
        connected: 'Unknown',
        noConnection: true,
      },
    ]);

    const active = await screen.findByRole('rowgroup', { name: /active/i });
    // Two cells read "Connecting" — the connection axis and the sync one it gates.
    expect(within(active).getAllByRole('cell', { name: 'Connecting' })).toHaveLength(2);
    expect(screen.getAllByText('Connecting', { selector: '[data-tone]' }).at(-1)).toHaveAttribute('data-tone', 'muted');
  });

  // Resuming a paused cluster flips `spec.syncEnabled` at once, but its hundred sync
  // children stay paused until their reconciles land. The row read healthy green
  // "Syncing" for that whole gap — a cache doing nothing, painted as one catching up.
  it('reads a cache whose kinds are all still paused as Paused', async () => {
    await openWith([
      { uuid: 'u-resumed', name: 'prod-us', enabled: true, present: true, connected: 'True', kindsPaused: true },
    ]);

    const active = await screen.findByRole('rowgroup', { name: /active/i });
    expect(within(active).getByRole('cell', { name: 'Paused' })).toBeInTheDocument();
    expect(screen.getByText('Paused', { selector: '[data-tone]' })).toHaveAttribute('data-tone', 'muted');
  });

  // A verdict this build doesn't know is degraded, not healthy. The schema says so and
  // the sidecar's own fold ranks it that way; rendering it green would silently hide
  // every reason a newer sidecar learns to emit.
  it('reads an unrecognised sync verdict as degraded, not healthy', async () => {
    await openWith([
      { uuid: 'u-new', name: 'prod-us', enabled: true, present: true, connected: 'True', unknownReason: true },
    ]);

    const active = await screen.findByRole('rowgroup', { name: /active/i });
    expect(within(active).getByRole('cell', { name: 'Degraded' })).toBeInTheDocument();
    expect(screen.getByText('Degraded', { selector: '[data-tone]' })).toHaveAttribute('data-tone', 'attention');
  });

  // After a sidecar restart the conditions on disk were written by the previous
  // process. beehive downgrades them to Unknown and flags them `unconfirmed`, but their
  // reason survives and still names the pre-restart state. Reporting that reason would
  // assert a failure this process has never observed.
  it('does not report a pre-restart sync failure as current until it is re-confirmed', async () => {
    await openWith([
      {
        uuid: 'u-broke',
        name: 'broke',
        enabled: true,
        present: true,
        syncFailed: true,
        unconfirmed: true,
      },
    ]);

    const active = await screen.findByRole('rowgroup', { name: /active/i });
    // Not 'Error' — that reason is the last known state, not an observed one.
    expect(within(active).queryByRole('cell', { name: 'Error' })).not.toBeInTheDocument();
    expect(within(active).getByRole('cell', { name: 'Syncing' })).toBeInTheDocument();
    // The connection axis reads Unknown, which already renders as the pending state.
    expect(within(active).getByRole('cell', { name: 'Connecting' })).toBeInTheDocument();
  });

  // The stamps survive the downgrade too, so they predate this process. Rendering one
  // as uptime would claim a connection nobody has re-established; "0m" would claim a
  // definite outage. Neither is known yet.
  it('shows uptime as unknown, not 0m, while the connection condition is unconfirmed', async () => {
    await openWith([
      {
        uuid: 'u-prod',
        name: 'prod-us',
        enabled: true,
        present: true,
        unconfirmed: true,
        disconnectedSince: new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString(),
      },
    ]);

    await userEvent.click(await screen.findByRole('button', { name: /Connecting/i }));
    const uptime = await screen.findByText('Uptime');
    const value = uptime.parentElement?.textContent ?? '';
    expect(value).toContain('\u2014');
    expect(value).not.toContain('0m');
    expect(value).not.toMatch(/\dh/);
  });

  it('shows one overall status indicator per row, rolled up to the most severe axis', async () => {
    await openWith([
      { uuid: 'u-prod', name: 'prod-us', enabled: true, present: true, connected: 'True' },
      { uuid: 'u-remote', name: 'remote', enabled: true, present: true, connected: 'False' },
      { uuid: '', name: 'minikube', enabled: false, present: true, connected: 'Unknown' },
      { uuid: 'u-old', name: 'old-cluster', enabled: true, present: false },
    ]);

    // One indicator per row, named by both axes (the tooltip summary), tinted by
    // the most severe of the two.
    expect(await screen.findByRole('img', { name: 'Active · Current' })).toHaveAttribute('data-tone', 'ok');
    expect(screen.getByRole('img', { name: 'Disconnected · Stalled' })).toHaveAttribute('data-tone', 'error');
    expect(screen.getByRole('img', { name: 'Connecting · Not synced' })).toHaveAttribute('data-tone', 'attention');
    expect(screen.getByRole('img', { name: 'Unavailable · Stopped' })).toHaveAttribute('data-tone', 'muted');
  });

  it('color-codes the connection and sync text by their own tone, to explain the overall color', async () => {
    await openWith([
      { uuid: 'u-prod', name: 'prod-us', enabled: true, present: true, connected: 'True' },
      { uuid: 'u-remote', name: 'remote', enabled: true, present: true, connected: 'False' },
      { uuid: '', name: 'minikube', enabled: false, present: true, connected: 'Unknown' },
      { uuid: 'u-old', name: 'old-cluster', enabled: true, present: false },
    ]);

    await screen.findByRole('rowgroup', { name: /active/i });
    // Connection axis — the `[data-tone]` selector disambiguates the cell label
    // from the group-header text (which shares words like "Active").
    expect(screen.getByText('Active', { selector: '[data-tone]' })).toHaveAttribute('data-tone', 'ok');
    expect(screen.getByText('Disconnected', { selector: '[data-tone]' })).toHaveAttribute('data-tone', 'error');
    expect(screen.getByText('Connecting', { selector: '[data-tone]' })).toHaveAttribute('data-tone', 'attention');
    expect(screen.getByText('Unavailable', { selector: '[data-tone]' })).toHaveAttribute('data-tone', 'muted');
    // Sync axis.
    expect(screen.getByText('Current', { selector: '[data-tone]' })).toHaveAttribute('data-tone', 'ok');
    // Stalled is a *gated* value (its fault is the connection, not sync), so it
    // grays out rather than going amber — the red "Disconnected" carries the cause.
    expect(screen.getByText('Stalled', { selector: '[data-tone]' })).toHaveAttribute('data-tone', 'muted');
    expect(screen.getByText('Not synced', { selector: '[data-tone]' })).toHaveAttribute('data-tone', 'muted');
    expect(screen.getByText('Stopped', { selector: '[data-tone]' })).toHaveAttribute('data-tone', 'muted');
  });

  it('requests connection diagnostics in the clusterCollectionWatch subscription', async () => {
    renderPanel();
    await flush();
    const sub = invokeMock.mock.calls.find(
      ([cmd, arg]) =>
        cmd === 'graphql_subscribe' && (arg as { query: string }).query.includes('clusterCollectionWatch'),
    )?.[1] as { query: string };
    expect(sub.query).toContain('message');
    expect(sub.query).toContain('transitionedAt');
    // The probe history and the next-attempt countdown are not inlined on
    // the list — they stream per-row via objectEventFeed / clusterScheduleStateWatch.
    expect(sub.query).not.toContain('connectionAttempts');
    expect(sub.query).not.toContain('nextAttemptAt');
  });

  it('reveals the underlying error and a retry action when a disconnected cluster is opened', async () => {
    const user = await openWith([
      {
        uuid: 'u-remote',
        name: 'remote',
        enabled: true,
        present: true,
        connected: 'False',
        connMessage: 'dial tcp 10.0.0.1:6443: connect: connection refused',
        disconnectedSince: new Date(Date.now() - 3 * 60_000).toISOString(),
      },
    ]);

    // The Disconnected label is an interactive trigger (only the error state is).
    // Opening it mounts the per-row objectEventFeed + clusterScheduleStateWatch subs.
    await user.click(await screen.findByRole('button', { name: /disconnected/i }));

    // The next-attempt countdown streams in on the schedule state watch (decoupled from
    // the list), and the probe history on the events stream (bare runs, newest
    // first by lastAt), with `ok` derived from the generic event type.
    await act(async () => {
      pushSchedule(new Date(Date.now() + 15_000).toISOString());
      pushConnectionEvent({
        id: '2',
        type: 'Warning',
        reason: 'ProbeFailed',
        message: 'TLS handshake timeout',
        count: 2,
        firstAt: new Date(Date.now() - 30_000).toISOString(),
        lastAt: new Date(Date.now() - 20_000).toISOString(),
      });
      pushConnectionEvent({
        id: '1',
        type: 'Warning',
        reason: 'ProbeFailed',
        message: 'i/o timeout',
        count: 5,
        firstAt: new Date(Date.now() - 90_000).toISOString(),
        lastAt: new Date(Date.now() - 40_000).toISOString(),
      });
    });

    // The popover surfaces the connection timestamps, the countdown to the next
    // scheduled retry (streamed from the schedule state watch), and the recent-attempt
    // log (which carries the probe errors).
    expect(await screen.findByText(/uptime/i)).toBeInTheDocument();
    expect(screen.getByText(/next check/i)).toBeInTheDocument();
    // The ~15s-out schedule renders a live countdown, not the "now" fallback.
    expect(screen.getByText(/in \d+s/i)).toBeInTheDocument();
    // The attempt history lists each recorded outcome with its own probe message.
    expect(screen.getByText(/recent attempts/i)).toBeInTheDocument();
    expect(await screen.findByText(/i\/o timeout/i)).toBeInTheDocument();
    expect(screen.getByText(/tls handshake timeout/i)).toBeInTheDocument();

    // Retry fires clusterConnectionRetry.
    await user.click(screen.getByRole('button', { name: /retry/i }));
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_query',
      expect.objectContaining({ body: expect.stringContaining('clusterConnectionRetry') }),
    );
  });

  it('shows the newest sample of the connection’s data rate', async () => {
    const user = await openWith([{ uuid: 'u-prod', name: 'prod-us', enabled: true, present: true, connected: 'True' }]);
    await user.click(await screen.findByRole('button', { name: 'Active' }));

    // Nothing is known until the window arrives.
    const row = screen.getByText('Data rate').parentElement!;
    expect(row).toHaveTextContent('Data rate—');

    await act(async () => {
      channelFor('clusterDataRateSampleFeed').onmessage!(rateWindow(rate(512, 64), rate(2048, 0)));
    });
    expect(row).toHaveTextContent('↓ 2.0 KB/s · ↑ 0 B/s');
    // The window draws at once, beside the numbers.
    expect(row.querySelectorAll('svg polyline')).toHaveLength(2);
  });

  // A connection idle since the sidecar started has an empty window, which is an answer.
  it('shows no rate for an empty window', async () => {
    const user = await openWith([{ uuid: 'u-prod', name: 'prod-us', enabled: true, present: true, connected: 'True' }]);
    await user.click(await screen.findByRole('button', { name: 'Active' }));

    await act(async () => {
      channelFor('clusterDataRateSampleFeed').onmessage!(rateWindow());
    });
    expect(screen.getByText('Data rate').parentElement).toHaveTextContent('Data rate—');
  });

  // A view mounting into a shared stream would never see the window, so each opens its own.
  it('gives a second view of one cluster a stream of its own', async () => {
    const client = createGraphqlClient();
    const view = () => (
      <UrqlProvider value={client}>
        <ConnectionDetail cluster={clusterRecord({ context: 'prod' })} connFailed={false} />
      </UrqlProvider>
    );
    render(view());
    await flush();
    await act(async () => {
      channelFor('clusterDataRateSampleFeed').onmessage!(rateWindow(rate(100, 0)));
    });

    const second = render(view());
    await flush();
    await act(async () => {
      channelFor('clusterDataRateSampleFeed').onmessage!(rateWindow(rate(100, 0), rate(4096, 0)));
    });

    expect(subscriptionsTo('clusterDataRateSampleFeed')).toBe(2);
    expect(within(second.container).getByText('Data rate').parentElement).toHaveTextContent('↓ 4.0 KB/s · ↑ 0 B/s');
  });

  // The sidecar refuses a disabled cluster's state watch, and that refusal would reach the error bus.
  it('asks no data rate of a disabled cluster', async () => {
    const user = await openWith([
      { uuid: '', name: 'minikube', enabled: false, disabled: true, present: true, connected: 'Unknown' },
    ]);
    await user.click(await screen.findByRole('button', { name: 'Connecting' }));

    expect(screen.getByText('Data rate').parentElement).toHaveTextContent('Data rate—');
    expect(() => channelFor('clusterDataRateSampleFeed')).toThrow('no subscription for clusterDataRateSampleFeed');
  });

  // The mutation resolves when the probe it asked for has finished, so the button binds to
  // `fetching` and needs nothing of its own: it is busy for exactly as long as the probe is.
  it('keeps the retry busy for as long as its probe is out', async () => {
    let answer!: () => void;
    const probing = new Promise<void>((resolve) => {
      answer = resolve;
    });
    invokeMock.mockImplementation(async (cmd: string, arg: unknown) => {
      if (cmd === 'graphql_subscribe') return 1;
      if (cmd === 'graphql_unsubscribe') return undefined;
      if (cmd === 'graphql_query') {
        if ((arg as { body: string }).body.includes('clusterConnectionRetry')) await probing;
        return { status: 200, body: JSON.stringify({ data: { clusterConnectionRetry: true } }) };
      }
      throw new Error(`unexpected ${cmd}`);
    });
    const user = await openWith([
      { uuid: 'u-retry', name: 'remote', enabled: true, present: true, connected: 'False', connMessage: 'refused' },
    ]);
    await user.click(await screen.findByRole('button', { name: /disconnected/i }));

    await user.click(await screen.findByRole('button', { name: /retry now/i }));

    expect(await screen.findByRole('button', { name: /retrying…/i })).toBeDisabled();

    await act(async () => {
      answer();
      await flush();
    });

    expect(await screen.findByRole('button', { name: /retry now/i })).toBeEnabled();
  });

  it('reveals recent sync events when a cached cluster’s sync status is opened', async () => {
    const user = await openWith([{ uuid: 'u-sync', name: 'prod', enabled: true, present: true }]);

    // The sync-status label is a disclosure trigger for a row with an active cache (its
    // verdict has streamed in). A healthy cached row reads "Current".
    await user.click(await screen.findByRole('button', { name: /current/i }));

    // The transition log is per kind, so it stays paused until a kind record names one —
    // subscribing before that would open a stream guaranteed to carry nothing.
    await act(async () => pushCachedKind('g-events', 'events', 'Watching'));

    // The sync history streams in as bare runs (newest first by lastAt), with `ok`
    // derived from the generic event type (Normal = a healthy run).
    await act(async () => {
      pushSyncEvent({
        id: '2',
        type: 'Warning',
        reason: 'SyncDegraded',
        message: 'discovery returned no syncable resources',
        count: 3,
        firstAt: new Date(Date.now() - 60_000).toISOString(),
        lastAt: new Date(Date.now() - 20_000).toISOString(),
      });
      pushSyncEvent({
        id: '1',
        type: 'Normal',
        reason: 'SyncComplete',
        message: 'Initial sync complete — cached 5 objects across 3 kinds in 2s',
        count: 1,
        firstAt: new Date(Date.now() - 90_000).toISOString(),
        lastAt: new Date(Date.now() - 80_000).toISOString(),
      });
    });

    // The sync detail lists each recorded run by its raw reason code (the message
    // field already carries the human-readable detail, so a friendly label would
    // just repeat it) and its message.
    expect(await screen.findByText(/recent sync events/i)).toBeInTheDocument();
    expect(await screen.findByText(/discovery returned no syncable resources/i)).toBeInTheDocument();
    // Raw reason codes, with the run's ×count multiplier when > 1.
    expect(screen.getByText(/SyncDegraded ×3/i)).toBeInTheDocument();
    expect(screen.getByText('SyncComplete')).toBeInTheDocument();
  });

  // A timeline with no runs yet is indistinguishable from one still arriving until the
  // bookmark lands, so the empty state waits for it. Without the gate a kind with a
  // long history reads "No sync events yet." until its first run arrives.
  it('withholds the empty sync-event state until the snapshot closes', async () => {
    const user = await openWith([{ uuid: 'u-sync', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));
    await act(async () => pushCachedKind('g-events', 'events', 'Watching'));

    // Subscribed, nothing delivered: still loading, so no verdict either way.
    expect(screen.queryByText(/no sync events yet/i)).not.toBeInTheDocument();

    await act(async () => pushEventBookmark('ObjectEventFeedSync'));

    // Now the emptiness is a real answer.
    expect(await screen.findByText(/no sync events yet/i)).toBeInTheDocument();
  });

  it('shows a last-update freshness line in the sync detail, driven by lastUpdateAt', async () => {
    const updatedAt = new Date(Date.now() - 30_000).toISOString();
    const user = await openWith([
      {
        uuid: 'u-fresh',
        name: 'prod',
        enabled: true,
        present: true,
        lastUpdateAt: updatedAt,
        lastLiveAt: new Date(Date.now() - 3_000).toISOString(),
      },
    ]);

    // Open the sync-status disclosure (the row has an active cache).
    await user.click(await screen.findByRole('button', { name: /current/i }));

    // The freshness line answers "is my cache current?" directly from lastUpdateAt
    // — a live relative counter, independent of the (separate) sync-event history.
    expect(await screen.findByText(/last update received/i)).toBeInTheDocument();
    expect(await screen.findByText('30s ago')).toBeInTheDocument();
  });

  it('names each failing kind with the reason that kind reported', async () => {
    const user = await openWith([{ uuid: 'u-mixed', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));

    // A cache's kinds fail independently, so one forbidden CRD is invisible in every other
    // reading — the rollup can name it but cannot say why.
    await act(async () =>
      pushSyncStatus([
        { apiVersion: 'v1', resource: 'pods', reason: 'Watching' },
        { apiVersion: 'example.com/v1', resource: 'widgets', reason: 'SyncFailed', message: 'forbidden' },
      ]),
    );

    expect(await screen.findByText(/kinds not syncing/i)).toBeInTheDocument();
    expect(await screen.findByText('widgets')).toBeInTheDocument();
    expect(await screen.findByText(/SyncFailed — forbidden/)).toBeInTheDocument();
    // A healthy kind is not an offender, so it is not in this list.
    expect(screen.queryByText('pods')).not.toBeInTheDocument();
  });

  it('leaves a kind that is still starting out of the failing list', async () => {
    const user = await openWith([{ uuid: 'u-cold', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));

    // A cold-listing cache reports every kind as Syncing. Reading those as offenders would
    // name all of them the moment a cache is armed, cleared, or resumed.
    await act(async () =>
      pushSyncStatus([
        { apiVersion: 'v1', resource: 'pods', reason: 'Syncing' },
        { apiVersion: 'apps/v1', resource: 'deployments', reason: 'Resuming' },
        { apiVersion: 'batch/v1', resource: 'jobs', reason: 'Resyncing' },
        { apiVersion: 'example.com/v1', resource: 'widgets', reason: 'SyncFailed', message: 'forbidden' },
      ]),
    );

    expect(await screen.findByText('widgets')).toBeInTheDocument();
    expect(screen.queryByText('pods')).not.toBeInTheDocument();
    expect(screen.queryByText('deployments')).not.toBeInTheDocument();
    expect(screen.queryByText('jobs')).not.toBeInTheDocument();
  });

  // Every discovery verdict reaches the webview and is dropped today. StoreFailed is the one
  // that makes that a defect: nothing under the cache syncs, no kind is in a position to say
  // why, and the rollup alone renders an amber "Degraded" with no reason beside it.
  it("reports a cache whose file will not open, with the driver's message", async () => {
    const user = await openWith([{ uuid: 'u-store', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));

    await act(async () => pushSyncStatus([], 'StoreFailed', 'disk full'));

    expect(await screen.findByText(/StoreFailed/)).toBeInTheDocument();
    expect(await screen.findByText(/disk full/)).toBeInTheDocument();
  });

  // A detail for a cache that is gone is no detail: the row is about to leave with its cache,
  // and drawing the absent reading would show a verdict for nothing.
  it('draws nothing from a detail that says its cache does not exist', async () => {
    const user = await openWith([{ uuid: 'u-gone', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));

    await act(async () => pushSyncStatus([], 'StoreFailed', 'disk full', false));

    expect(screen.queryByText(/StoreFailed/)).not.toBeInTheDocument();
  });

  // A store that will not open clears on its own never, and nothing syncs until someone
  // presses Clear — at least as hard a fault as one kind failing, so it reads as an error
  // rather than falling to the unknown-reason amber.
  it('reads a cache-level store failure as an error, not an unknown verdict', async () => {
    await openWith([{ uuid: 'u-storebadge', name: 'prod', enabled: true, present: true }]);
    act(() => pushHealth('cache-u-storebadge', { status: 'False', reason: 'StoreFailed', totalKinds: 3 }));

    expect(await screen.findByRole('button', { name: /storage error/i })).toBeInTheDocument();
  });

  // A kind the user just paused is not a fault, and the failing list is where faults go —
  // rendering it there is the same false alarm the reason-from-the-record rule prevents one
  // layer down. Its own section instead, because that is where the resume control belongs.
  it('lists a paused kind apart from the kinds that are failing', async () => {
    const user = await openWith([{ uuid: 'u-paused', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));

    await act(async () => {
      pushCachedKind('g-pods', 'pods', 'Paused');
      pushSyncStatus([
        { apiVersion: 'v1', resource: 'pods', reason: 'Paused' },
        { apiVersion: 'example.com/v1', resource: 'widgets', reason: 'SyncFailed', message: 'forbidden' },
      ]);
    });

    const paused = within(await screen.findByRole('group', { name: /paused kinds/i }));
    expect(paused.getByText('pods')).toBeInTheDocument();
    const failing = within(await screen.findByRole('group', { name: /kinds not syncing/i }));
    expect(failing.getByText('widgets')).toBeInTheDocument();
    expect(failing.queryByText('pods')).not.toBeInTheDocument();
  });

  // The mutation takes the per-kind RECORD's id, which the sync-detail row does not carry —
  // it is identity-by-GVR. The control joins the two streams to find it, and a control that
  // sent the wrong id would pause somebody else's kind.
  it('resumes a paused kind by its own record id', async () => {
    const user = await openWith([{ uuid: 'u-resume', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));
    await act(async () => {
      pushCachedKind('g-crd', 'widgets', 'Paused', 'example.com/v1');
      pushCachedKind('g-pods', 'pods', 'Watching');
      pushSyncStatus([
        { apiVersion: 'example.com/v1', resource: 'widgets', reason: 'Paused' },
        { apiVersion: 'v1', resource: 'pods', reason: 'Watching' },
      ]);
    });

    await user.click(await screen.findByRole('button', { name: /resume widgets/i }));

    expect(lastMutation('clusterCachedKindSyncEnabledSet')).toEqual({ id: 'g-crd', syncEnabled: true });
  });

  // The entry point into the feature. A kind that is failing is the one a user reaches for
  // this over — a storming CRD or a forbidden collection — so the control sits on its row.
  it('pauses a failing kind from the row that reports it', async () => {
    const user = await openWith([{ uuid: 'u-pause', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));
    await act(async () => {
      pushCachedKind('g-crd', 'widgets', 'SyncFailed', 'example.com/v1');
      pushSyncStatus([{ apiVersion: 'example.com/v1', resource: 'widgets', reason: 'SyncFailed' }]);
    });

    await user.click(await screen.findByRole('button', { name: /pause widgets/i }));

    expect(lastMutation('clusterCachedKindSyncEnabledSet')).toEqual({ id: 'g-crd', syncEnabled: false });
  });

  // The cap keeps the panel compact, but the rows it hides now carry the only control that
  // can resume a kind — so a kind pushed past it would be paused with no way back.
  it('reaches a paused kind past the list cap', async () => {
    const user = await openWith([{ uuid: 'u-cap', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));
    const many = Array.from({ length: 6 }, (_, i) => ({
      apiVersion: 'example.com/v1',
      resource: `widget${i + 1}`,
      reason: 'Paused',
    }));
    await act(async () => {
      many.forEach((k, i) => pushCachedKind(`g-${i + 1}`, k.resource, 'Paused', k.apiVersion));
      pushSyncStatus(many);
    });

    // The sixth is behind the cap, so its control is unreachable until the list expands.
    expect(screen.queryByRole('button', { name: /resume widget6/i })).not.toBeInTheDocument();
    await user.click(await screen.findByRole('button', { name: /show 1 more/i }));
    await user.click(await screen.findByRole('button', { name: /resume widget6/i }));

    expect(lastMutation('clusterCachedKindSyncEnabledSet')).toEqual({ id: 'g-6', syncEnabled: true });
  });

  // One failure, one report. The client's errorReportExchange forwards every operation
  // error, so a second check at the call site puts the same message on the bus twice — and
  // the reader of a duplicated error looks for two causes.
  it('reports a rejected cluster action once', async () => {
    const errors: string[] = [];
    const stop = onError((e) => errors.push(e.message));
    invokeMock.mockImplementation(async (cmd: string) => {
      if (cmd === 'graphql_subscribe') return 1;
      if (cmd === 'graphql_unsubscribe') return undefined;
      if (cmd === 'graphql_query') {
        return { status: 200, body: JSON.stringify({ errors: [{ message: 'forbidden: sync is managed' }] }) };
      }
      throw new Error(`unexpected ${cmd}`);
    });
    const user = await openWith([{ uuid: 'u-once', name: 'prod', enabled: true, present: true }]);

    await user.click(await screen.findByRole('button', { name: /pause sync for prod/i }));
    await flush();
    stop();

    expect(errors.filter((m) => m.includes('sync is managed'))).toHaveLength(1);
  });

  // A rejected mutation has nowhere to show but the bus: the panel's actions write through
  // and read their outcome back off a watch. A guard rather than a red test — the client's
  // errorReportExchange forwards every operation error, mutations included — and this is
  // what keeps a refused pause from becoming a button that does nothing.
  it('reports a rejected pause on the error bus', async () => {
    const errors: string[] = [];
    const stop = onError((e) => errors.push(e.message));
    invokeMock.mockImplementation(async (cmd: string) => {
      if (cmd === 'graphql_subscribe') return 1;
      if (cmd === 'graphql_unsubscribe') return undefined;
      if (cmd === 'graphql_query') {
        return { status: 200, body: JSON.stringify({ errors: [{ message: 'forbidden: kinds are read-only' }] }) };
      }
      throw new Error(`unexpected ${cmd}`);
    });
    const user = await openWith([{ uuid: 'u-fail', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));
    await act(async () => {
      pushCachedKind('g-crd', 'widgets', 'SyncFailed', 'example.com/v1');
      pushSyncStatus([{ apiVersion: 'example.com/v1', resource: 'widgets', reason: 'SyncFailed' }]);
    });

    const before = errors.length;
    await user.click(await screen.findByRole('button', { name: /pause widgets/i }));
    await flush();
    stop();

    expect(errors.slice(before).join(' ')).toMatch(/kinds are read-only/);
  });

  it('shows no failing-kind list while every kind is watching', async () => {
    const user = await openWith([{ uuid: 'u-ok', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));

    await act(async () => pushSyncStatus([{ apiVersion: 'v1', resource: 'pods', reason: 'Watching' }]));

    expect(screen.queryByText(/kinds not syncing/i)).not.toBeInTheDocument();
  });

  it("shows the kind-discovery history off the cache record, not any kind's", async () => {
    const user = await openWith([{ uuid: 'u-disc', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));

    await act(async () =>
      pushDiscoveryEvent({
        id: 'd-1',
        type: 'Warning',
        reason: 'Partial',
        message: 'metrics.k8s.io/v1beta1 would not load',
        count: 2,
        firstAt: new Date(Date.now() - 60_000).toISOString(),
        lastAt: new Date(Date.now() - 5_000).toISOString(),
      }),
    );

    expect(await screen.findByText(/recent kind discovery/i)).toBeInTheDocument();
    // Aggregated runs render with their occurrence count.
    expect(await screen.findByText('Partial ×2')).toBeInTheDocument();
  });

  it('reports liveness apart from updates, so a quiet cache does not read as stalled', async () => {
    // The shape a quiet-but-healthy cluster produces: nothing has been written for an
    // hour, but the watch proved itself alive seconds ago (an api-server bookmark).
    // Showing only the update time would read as a stall that isn't happening.
    // The clock is frozen so the clicks below cannot age "5s ago" on a loaded machine.
    vi.useFakeTimers({ toFake: ['Date'] });
    onTestFinished(() => {
      vi.useRealTimers();
    });
    const user = await openWith([
      {
        uuid: 'u-quiet',
        name: 'prod',
        enabled: true,
        present: true,
        lastUpdateAt: new Date(Date.now() - 60 * 60_000).toISOString(),
        lastLiveAt: new Date(Date.now() - 5_000).toISOString(),
      },
    ]);

    await user.click(await screen.findByRole('button', { name: /current/i }));

    expect(await screen.findByText(/last update received/i)).toBeInTheDocument();
    expect(await screen.findByText('60m ago')).toBeInTheDocument();
    expect(await screen.findByText(/sync verified/i)).toBeInTheDocument();
    expect(await screen.findByText('5s ago')).toBeInTheDocument();
    // Staleness is engine-derived; an old update stamp alone must not raise the banner.
    expect(screen.queryByText(/possibly stale/i)).not.toBeInTheDocument();
  });

  it('summarises cache contents from the state watch, thousands-grouped', async () => {
    const user = await openWith([{ uuid: 'u-big', name: 'prod', enabled: true, present: true }]);

    await user.click(await screen.findByRole('button', { name: /current/i }));
    act(() => pushCacheStats(2203, 120));

    // Locale-grouped and pluralised.
    expect(await screen.findByText('2,203 objects across 120 kinds')).toBeInTheDocument();
  });

  // The row's size cell and the expanded detail both want the cache's contents. They used
  // to subscribe separately with identical variables, which urql resolves to ONE operation —
  // and a subscription's second subscriber joins mid-stream with no replay. So a detail
  // opened after the state watch had already delivered sat on null forever, and the "N objects
  // across M kinds" row never appeared.
  it('shows the cache summary when the detail is opened after the state watch has delivered', async () => {
    const user = await openWith([{ uuid: 'u-late', name: 'prod', enabled: true, present: true }]);

    // The state watch delivers while only the always-mounted size cell is listening.
    act(() => pushCacheStats(2203, 120, 1_300_000));
    expect(await screen.findByText('1.2 MB')).toBeInTheDocument();

    // Expanding must not need a fresh frame to show the same numbers.
    await user.click(await screen.findByRole('button', { name: /current/i }));
    expect(await screen.findByText('2,203 objects across 120 kinds')).toBeInTheDocument();
  });

  // The regression this stream exists for. The cache summary used to be a field on the
  // ClusterCache record, and that record stops changing once its sync settles — so the
  // panel rendered whatever the cache held when the window subscribed (an early slice of
  // a cold sync) and never moved, however much landed afterwards.
  it('updates the cache summary as the sync fills, without the cache record changing', async () => {
    const user = await openWith([{ uuid: 'u-grow', name: 'prod', enabled: true, present: true }]);

    await user.click(await screen.findByRole('button', { name: /current/i }));
    act(() => pushCacheStats(156, 12));
    expect(await screen.findByText('156 objects across 12 kinds')).toBeInTheDocument();

    act(() => pushCacheStats(1386, 62));
    expect(await screen.findByText('1,386 objects across 62 kinds')).toBeInTheDocument();
    expect(screen.queryByText('156 objects across 12 kinds')).not.toBeInTheDocument();
  });

  it('reports per-kind sync health from the rollup, naming the kinds that are not syncing', async () => {
    const user = await openWith([{ uuid: 'u-kinds', name: 'prod', enabled: true, present: true }]);

    await user.click(await screen.findByRole('button', { name: /current/i }));
    // The fold is the sidecar's — it knows all hundred-plus kinds and already skipped any
    // verdict a previous process wrote. The panel renders it, and must not re-derive it:
    // a second definition of health here could disagree with the badge above it.
    act(() =>
      pushHealth('cache-u-kinds', {
        status: 'False',
        reason: 'Stale',
        unhealthyKindRefs: [{ apiVersion: 'example.com/v1', resource: 'widgets', reason: 'Stale' }],
        totalKinds: 3,
        unhealthyKinds: 1,
      }),
    );

    expect(await screen.findByText(/2 of 3 kinds — widgets not syncing/)).toBeInTheDocument();
  });

  // unhealthyKinds counts every kind that is not currently Watching; unhealthyKindRefs
  // may name none of them. A cache mid-pause has the count with no names, which must not
  // render as an empty gap: "63 of 150 kinds —  not syncing".
  it('names no offenders when the rollup counts unhealthy kinds without naming any', async () => {
    const user = await openWith([{ uuid: 'u-pausing', name: 'prod', enabled: true, present: true }]);

    await user.click(await screen.findByRole('button', { name: /current/i }));
    act(() =>
      pushHealth('cache-u-pausing', {
        status: 'False',
        reason: 'Paused',
        unhealthyKindRefs: [],
        totalKinds: 150,
        unhealthyKinds: 87,
      }),
    );

    expect(await screen.findByText(/63 of 150 kinds syncing/)).toBeInTheDocument();
    expect(screen.queryByText(/not syncing/)).not.toBeInTheDocument();
  });

  // A paused kind is not unhealthy and stays in the census, so every field the label
  // already spends is unchanged — five kinds, none of them offenders. Without the paused
  // tally the two the user just switched off vanish from the summary entirely.
  it('accounts for paused kinds in the syncing summary', async () => {
    const user = await openWith([{ uuid: 'u-pcount', name: 'prod', enabled: true, present: true }]);

    await user.click(await screen.findByRole('button', { name: /current/i }));
    act(() => pushHealth('cache-u-pcount', { totalKinds: 5, unhealthyKinds: 0, pausedKinds: 2 }));

    expect(await screen.findByText(/3 of 5 kinds syncing, 2 paused/)).toBeInTheDocument();
  });

  it('names the first three offenders and counts the rest', async () => {
    const user = await openWith([{ uuid: 'u-many', name: 'prod', enabled: true, present: true }]);

    await user.click(await screen.findByRole('button', { name: /current/i }));
    act(() =>
      pushHealth('cache-u-many', {
        status: 'False',
        reason: 'Stale',
        unhealthyKindRefs: ['widgets', 'gadgets', 'gizmos', 'sprockets', 'cogs'].map((resource) => ({
          apiVersion: 'example.com/v1',
          resource,
          reason: 'Stale',
        })),
        totalKinds: 9,
        unhealthyKinds: 5,
      }),
    );

    expect(await screen.findByText(/4 of 9 kinds — widgets, gadgets, gizmos \+2 more not syncing/)).toBeInTheDocument();
  });

  // The clock is frozen so each age reads the same however long the clicks take.
  it('shows a connected cluster’s uptime by the whole minute', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    onTestFinished(() => {
      vi.useRealTimers();
    });
    const since = (ms: number) => new Date(Date.now() - ms).toISOString();
    const user = await openWith([
      { uuid: 'u-new', name: 'new', enabled: true, present: true, disconnectedSince: since(30_000) },
      { uuid: 'u-mid', name: 'mid', enabled: true, present: true, disconnectedSince: since(5 * 60_000) },
      { uuid: 'u-old', name: 'old', enabled: true, present: true, disconnectedSince: since(3 * 60 * 60_000) },
    ]);

    const [first, second, third] = await screen.findAllByRole('button', { name: /^active/i });
    await user.click(first);
    await user.click(second);
    await user.click(third);

    expect(await screen.findByText('<1m')).toBeInTheDocument();
    expect(screen.getByText('5m')).toBeInTheDocument();
    expect(screen.getByText('3h')).toBeInTheDocument();
  });

  it('spans each repeated attempt, and counts down past a minute', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    onTestFinished(() => {
      vi.useRealTimers();
    });
    const ago = (ms: number) => new Date(Date.now() - ms).toISOString();
    const user = await openWith([
      { uuid: 'u-remote', name: 'remote', enabled: true, present: true, connected: 'False' },
    ]);
    await user.click(await screen.findByRole('button', { name: /disconnected/i }));

    await act(async () => {
      pushSchedule(new Date(Date.now() + 90_000).toISOString());
      pushConnectionEvent({
        id: '2',
        type: 'Warning',
        reason: 'ProbeFailed',
        message: 'minutes apart',
        count: 3,
        firstAt: ago(10 * 60_000),
        lastAt: ago(60_000),
      });
      pushConnectionEvent({
        id: '1',
        type: 'Warning',
        reason: 'ProbeFailed',
        message: 'hours apart',
        count: 4,
        firstAt: ago(5 * 60 * 60_000),
        lastAt: ago(2 * 60 * 60_000),
      });
    });

    expect(await screen.findByText('(9m)')).toBeInTheDocument();
    expect(screen.getByText('(3h)')).toBeInTheDocument();
    expect(screen.getByText('in 90s')).toBeInTheDocument();
  });

  it('reports a plain kind count when every kind is healthy', async () => {
    const user = await openWith([{ uuid: 'u-ok', name: 'prod', enabled: true, present: true }]);

    await user.click(await screen.findByRole('button', { name: /current/i }));
    act(() => pushHealth('cache-u-ok', { totalKinds: 2 }));

    expect(await screen.findByText('2 kinds')).toBeInTheDocument();
  });

  it('omits the cache summary when the cache holds no objects', async () => {
    const user = await openWith([{ uuid: 'u-empty', name: 'prod', enabled: true, present: true }]);

    await user.click(await screen.findByRole('button', { name: /current/i }));

    // An empty cache is already covered by the freshness line; no "0 objects" noise.
    expect(await screen.findByText(/no updates received yet/i)).toBeInTheDocument();
    expect(screen.queryByText(/objects across/i)).not.toBeInTheDocument();
  });

  it('surfaces a stalled watch: the sync column reads Stale and the detail explains why', async () => {
    const user = await openWith([
      {
        uuid: 'u-stale',
        name: 'prod',
        enabled: true,
        present: true,
        stale: true,
        lastUpdateAt: new Date(Date.now() - 6 * 60_000).toISOString(),
        lastLiveAt: new Date(Date.now() - 6 * 60_000).toISOString(),
      },
    ]);

    // The sync column reflects the stalled state (not a healthy "Current").
    const staleBtn = await screen.findByRole('button', { name: /stale/i });
    await user.click(staleBtn);

    // The detail flags it and names the kinds behind it. A rollup can't carry one kind's
    // free-text message — the verdict spans a hundred of them — so what it reports is
    // which ones are not keeping up.
    expect(await screen.findByText(/possibly stale/i)).toBeInTheDocument();
    expect(screen.getByText(/pods not receiving updates/i)).toBeInTheDocument();

    // The log follows the kind behind the verdict, which the rollup names.
    await act(async () => pushCachedKind('g-pods', 'pods', 'Stale'));

    // A SyncStale event renders by its raw reason code alongside its message.
    await act(async () => {
      pushSyncEvent({
        id: '9',
        type: 'Warning',
        reason: 'SyncStale',
        message: 'Pod watch went quiet',
        count: 1,
        firstAt: new Date(Date.now() - 60_000).toISOString(),
        lastAt: new Date(Date.now() - 10_000).toISOString(),
      });
    });
    expect(await screen.findByText('SyncStale')).toBeInTheDocument();
    expect(screen.getByText(/pod watch went quiet/i)).toBeInTheDocument();
  });

  it('blames the watch, not a kind still syncing, when a cache is stale', async () => {
    const user = await openWith([{ uuid: 'u-stale', name: 'prod', enabled: true, present: true, stale: true }]);
    await act(async () => {
      pushHealth('cache-u-stale', {
        status: 'False',
        reason: 'Stale',
        unhealthyKindRefs: [{ apiVersion: 'v1', resource: 'pods', reason: 'Syncing' }],
        totalKinds: 1,
        unhealthyKinds: 1,
      });
    });

    await user.click(await screen.findByRole('button', { name: /stale/i }));

    expect(await screen.findByText(/the watch may have stopped delivering updates/i)).toBeInTheDocument();
    expect(screen.queryByText(/pods not receiving updates/i)).not.toBeInTheDocument();
  });

  // The rollup names its offenders by kind, and the timeline has to follow the exact one.
  // A CRD may reuse a built-in's plural under its own api group, so matching on the plural
  // alone opened whichever record happened to match first — rendering a healthy kind's
  // history under the failing kind's heading.
  it('opens the timeline of the offending kind, not another kind sharing its plural', async () => {
    const user = await openWith([{ uuid: 'u-gw', name: 'prod', enabled: true, present: true }]);
    await user.click(await screen.findByRole('button', { name: /current/i }));

    await act(async () => {
      pushCachedKind('g-builtin', 'gateways', 'Watching', 'gateway.networking.k8s.io/v1');
      pushCachedKind('g-crd', 'gateways', 'SyncFailed', 'example.com/v1');
      pushHealth('cache-u-gw', {
        status: 'False',
        reason: 'SyncFailed',
        unhealthyKindRefs: [{ apiVersion: 'example.com/v1', resource: 'gateways', reason: 'SyncFailed' }],
        totalKinds: 2,
        unhealthyKinds: 1,
      });
    });

    const subs = invokeMock.mock.calls.filter(([cmd]) => cmd === 'graphql_subscribe');
    const events = subs
      .map(([, arg]) => arg as { query: string; variables?: Record<string, unknown> })
      .filter((a) => a.query.includes('ObjectEventFeedSync'));
    expect(events.at(-1)?.variables?.id).toBe('g-crd');
  });

  it('holds the countdown across an in-flight probe, then clears it once the schedule is authoritatively empty', async () => {
    const user = await openWith([
      { uuid: 'u-remote', name: 'remote', enabled: true, present: true, connected: 'False' },
    ]);
    await user.click(await screen.findByRole('button', { name: /disconnected/i }));

    // A scheduled next attempt → a live countdown.
    await act(async () => pushSchedule(new Date(Date.now() + 15_000).toISOString()));
    expect(await screen.findByText(/in \d+s/i)).toBeInTheDocument();

    // The state watch reports the zero time (null) while the scheduled reconcile is
    // dispatched — but the controller asserts the probe is running (probing:
    // true). That is the in-flight case: show "checking…", not a blank countdown.
    await act(async () => pushSchedule(null, true));
    expect(await screen.findByText(/checking…/i)).toBeInTheDocument();

    // The probe ends and the cluster is now ineligible (e.g. disabled): nothing
    // scheduled and no probe running ({ null, false }). That is authoritative —
    // the stale countdown must clear, and nothing should imply an active probe.
    await act(async () => pushSchedule(null, false));
    expect(screen.queryByText(/in \d+s/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/checking…/i)).not.toBeInTheDocument();
  });

  it('shows the "checking…" spinner while a probe is in flight, even with a scheduled next attempt', async () => {
    const user = await openWith([
      { uuid: 'u-remote', name: 'remote', enabled: true, present: true, connected: 'False' },
    ]);
    await user.click(await screen.findByRole('button', { name: /disconnected/i }));

    // A scheduled next attempt → a live countdown.
    await act(async () => pushSchedule(new Date(Date.now() + 15_000).toISOString()));
    expect(await screen.findByText(/in \d+s/i)).toBeInTheDocument();

    // The controller asserts a probe is now running (probing: true) while the
    // schedule still carries the last time → "checking…" overrides the countdown.
    await act(async () => pushSchedule(new Date(Date.now() + 15_000).toISOString(), true));
    expect(await screen.findByText(/checking…/i)).toBeInTheDocument();
    expect(screen.getByRole('status')).toBeInTheDocument();
    expect(screen.queryByText(/in \d+s/i)).not.toBeInTheDocument();

    // The probe finishes and re-arms a schedule (probing: false) → back to a countdown.
    await act(async () => pushSchedule(new Date(Date.now() + 30_000).toISOString(), false));
    expect(await screen.findByText(/in \d+s/i)).toBeInTheDocument();
    expect(screen.queryByText(/checking…/i)).not.toBeInTheDocument();
  });

  it('shows a "checking…" spinner only while a probe is actually in flight, not merely when nothing is scheduled', async () => {
    const user = await openWith([
      { uuid: 'u-remote', name: 'remote', enabled: true, present: true, connected: 'False' },
    ]);
    await user.click(await screen.findByRole('button', { name: /disconnected/i }));

    // Nothing scheduled and no probe running (the pre-first-schedule window, or an
    // ineligible cluster): a neutral placeholder, never the spinner — an idle
    // cluster must not look like it is actively probing.
    await act(async () => pushSchedule(null, false));
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    expect(screen.queryByText(/checking…/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/in \d+s/i)).not.toBeInTheDocument();

    // The controller now asserts a probe is in flight → the spinner appears.
    await act(async () => pushSchedule(null, true));
    expect(await screen.findByText(/checking…/i)).toBeInTheDocument();
    expect(screen.getByRole('status')).toBeInTheDocument();
  });

  it('expands connection diagnostics for a reachable cluster with the neutral (non-failed) header', async () => {
    const user = await openWith([{ uuid: 'u-prod', name: 'prod-us', enabled: true, present: true, connected: 'True' }]);
    await screen.findByRole('rowgroup', { name: /active/i });
    // The connection label is a disclosure toggle in every state, including Active.
    await user.click(screen.getByRole('button', { name: /^active$/i }));
    // A healthy cluster gets the neutral "Connection" panel — not "Connection
    // failed" — but the Retry action is still available in every state.
    expect(await screen.findByText(/^connection$/i, { selector: 'p' })).toBeInTheDocument();
    expect(screen.queryByText(/connection failed/i)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
  });

  it('opens on the row and pane it was asked for, scrolled into view', async () => {
    const scroll = vi.mocked(Element.prototype.scrollIntoView);
    scroll.mockClear();
    await openOnDetail(
      [
        { uuid: 'u-prod', name: 'prod-us', enabled: true, present: true },
        { uuid: 'u-dev', name: 'dev-eu', enabled: true, present: true },
      ],
      { clusterId: 'u-dev', pane: 'connection' },
    );

    expect(await screen.findByText(/^connection$/i, { selector: 'p' })).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: /retry/i })).toHaveLength(1);
    const devRow = screen.getByText('dev-eu').closest('tr')!;
    expect(within(devRow).getByRole('button', { name: /^active$/i })).toHaveAttribute('aria-expanded', 'true');
    expect(scroll.mock.contexts).toContain(devRow);
  });

  it('opens no pane for a cluster it does not list', async () => {
    await openOnDetail([{ uuid: 'u-prod', name: 'prod-us', enabled: true, present: true }], {
      clusterId: 'u-gone',
      pane: 'connection',
    });

    await screen.findByRole('rowgroup', { name: /active/i });
    expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument();
  });

  it('omits a row group that has no members', async () => {
    await openWith([{ uuid: 'u-prod', name: 'prod-us', enabled: true, present: true }]);
    expect(await screen.findByRole('rowgroup', { name: /active/i })).toBeInTheDocument();
    expect(screen.queryByRole('rowgroup', { name: /orphaned/i })).not.toBeInTheDocument();
  });

  it('toggling sync fires the play/pause action via clusterSyncEnabledSet', async () => {
    const user = await openWith([{ uuid: 'u-prod', name: 'prod-us', enabled: true, present: true }]);

    // Enabled + active → a Pause button.
    await user.click(await screen.findByRole('button', { name: /pause sync for prod-us/i }));
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_query',
      expect.objectContaining({ body: expect.stringContaining('clusterSyncEnabledSet') }),
    );
  });

  it('toggling enable fires clusterEnabledSet', async () => {
    const user = await openWith([{ uuid: 'u-prod', name: 'prod-us', enabled: true, present: true }]);

    // The row's spec.enabled defaults to true (see pushClusters) → a Disable button.
    await user.click(await screen.findByRole('button', { name: /disable prod-us/i }));
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_query',
      expect.objectContaining({ body: expect.stringContaining('clusterEnabledSet') }),
    );
  });

  // Whether there is anything to clear comes off the live contents state watch, not the cache
  // record: the record settles and stops changing, so a file created after the window
  // subscribed would leave a populated cache visible in the size cell but unclearable.
  it('clearing a cache fires clusterCacheClear, and is disabled until a cache file exists', async () => {
    const user = await openWith([
      { uuid: 'u-prod', name: 'prod-us', enabled: true, present: true },
      { uuid: 'u-stg', name: 'staging', enabled: false, present: true },
    ]);

    expect(await screen.findByRole('button', { name: /clear cache for prod-us/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /clear cache for staging/i })).toBeDisabled();

    // prod-us's cache file appears well after its (long-settled) record arrived.
    act(() => pushCacheStatsFor('cache-u-prod', 1_300_000));

    expect(screen.getByRole('button', { name: /clear cache for staging/i })).toBeDisabled();
    await user.click(await screen.findByRole('button', { name: /clear cache for prod-us/i }));
    // The cache's own id, not the cluster's: a UID migration leaves a cluster owning
    // more than one, and the mutation clears exactly the one it is handed.
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_query',
      expect.objectContaining({ body: expect.stringContaining('cache-u-prod') }),
    );
  });

  it('shows an unreachable kubeconfig context as a pending active row with disabled actions', async () => {
    await openWith([{ uuid: '', name: 'minikube', enabled: false, present: true }]);

    const active = await screen.findByRole('rowgroup', { name: /active/i });
    expect(within(active).getByText('minikube')).toBeInTheDocument();
    expect(within(active).getByText(/not synced/i)).toBeInTheDocument();
    // Can't sync or clear a context with no identity / no cache yet.
    expect(screen.getByRole('button', { name: /sync for minikube/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /clear cache for minikube/i })).toBeDisabled();
    // Remove is rendered (so the Actions column aligns) but disabled in Active.
    expect(screen.getByRole('button', { name: /^remove minikube/i })).toBeDisabled();
  });

  it('disables play/pause for orphaned rows and removes them via clusterDelete', async () => {
    const user = await openWith([{ uuid: 'u-old', name: 'old-cluster', enabled: true, present: false }]);

    expect(await screen.findByRole('button', { name: /sync for old-cluster/i })).toBeDisabled();

    const remove = screen.getByRole('button', { name: /^remove old-cluster/i });
    expect(remove).toBeEnabled();
    await user.click(remove);
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_query',
      expect.objectContaining({ body: expect.stringContaining('clusterDelete') }),
    );
  });
});
