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

import { render, screen, act } from '@testing-library/react';
import { Provider as UrqlProvider } from 'urql';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  clusterOf,
  flushSubscriptionsSynchronously,
  mockTauriCore,
  pushClusters,
  pushCollectionWatchBookmark,
} from '@/test-utils';
import type { ClusterRow } from '@/test-utils';

// Frames pushed here are asserted on immediately.
flushSubscriptionsSynchronously();

// Mocks ---------------------------------------------------------------

const { invokeMock, channels, channelFor, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { createGraphqlClient } = await import('@/lib/graphql/client');
const { formatBytes, formatRate, ClustersProvider, useClusters } = await import('./clusters');

// Helpers -------------------------------------------------------------

const flush = () => act(async () => {});

// Drop both transports (the host's SSE streams died), advance past the backoff
// so subscribe-exchange re-establishes each subscription, then deliver the
// `open` frame the host sends on each re-established connection (which resets
// the accumulators before the snapshot replays).
async function reconnectBothStreams() {
  await act(async () => {
    channelFor('clusterCollectionWatch').onmessage!(JSON.stringify({ type: 'complete' }));
    channelFor('clusterCacheCollectionWatch').onmessage!(JSON.stringify({ type: 'complete' }));
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1_000);
  });
  await act(async () => {
    channelFor('clusterCollectionWatch').onmessage!(JSON.stringify({ type: 'open' }));
    channelFor('clusterCacheCollectionWatch').onmessage!(JSON.stringify({ type: 'open' }));
  });
}

// Push one Cluster change on clusterCollectionWatch.
function pushClusterChange(type: 'Added' | 'Modified' | 'Deleted', cluster: object) {
  channelFor('clusterCollectionWatch').onmessage!(
    JSON.stringify({ type: 'next', payload: { data: { clusterCollectionWatch: { type, cluster } } } }),
  );
}

// Push one ClusterCache change on clusterCacheCollectionWatch.
function pushCacheChange(type: 'Added' | 'Modified' | 'Deleted', cache: object) {
  channelFor('clusterCacheCollectionWatch').onmessage!(
    JSON.stringify({ type: 'next', payload: { data: { clusterCacheCollectionWatch: { type, cache } } } }),
  );
}

// Push one cache's folded sync verdict on the clusterCacheHealthKeyedStateWatch stream. A
// state watch, so there is no change type — each frame replaces the last.
function pushSyncChange(_type: 'Added' | 'Modified' | 'Deleted', health: object) {
  channelFor('clusterCacheHealthKeyedStateWatch').onmessage!(
    JSON.stringify({ type: 'next', payload: { data: { clusterCacheHealthKeyedStateWatch: health } } }),
  );
}

// The folded sync verdict of a row's cache — what the provider joins onto the cache by
// cacheID.
function syncOf(r: ClusterRow, reason = 'Watching') {
  return {
    cacheID: `cache-${r.id}`,
    status: reason === 'Watching' ? 'True' : 'False',
    reason,
    message: '',
    totalKinds: 1,
    unhealthyKinds: reason === 'Watching' ? 0 : 1,
    lastUpdateAt: null,
    lastLiveAt: null,
  };
}

// A cache mirroring a row's active identity (spec.serverUid matches the cluster's uid),
// so the provider joins it as that cluster's activeCache.
function cacheOf(r: ClusterRow) {
  return {
    id: `cache-${r.id}`,
    clusterID: r.id,
    spec: { serverUid: `uid-${r.id}` },
    stats: { exists: false, bytes: 0 },
  };
}

// A probe that renders the hook's value so tests can assert on it.
function Probe() {
  const { clusters } = useClusters();
  return <div data-testid="probe">{clusters === null ? 'null' : JSON.stringify(clusters.map((c) => c.spec.name))}</div>;
}

// A probe that reveals each cluster's active cache's sync verdict — the two-hop join
// (cache onto cluster, verdict onto cache) the rollup surface needs.
function SyncProbe() {
  const { clusters } = useClusters();
  return (
    <div data-testid="probe">
      {clusters === null
        ? 'null'
        : JSON.stringify(clusters.map((c) => `${c.spec.name}:${c.activeCache?.health?.reason ?? '-'}`))}
    </div>
  );
}

// A probe that also reveals each cluster's joined activeCache (id or '-').
function JoinProbe() {
  const { clusters } = useClusters();
  return (
    <div data-testid="probe">
      {clusters === null ? 'null' : JSON.stringify(clusters.map((c) => `${c.spec.name}:${c.activeCache?.id ?? '-'}`))}
    </div>
  );
}

function renderProvider(probe: React.ReactNode = <Probe />) {
  return render(
    <UrqlProvider value={createGraphqlClient()}>
      <ClustersProvider>{probe}</ClustersProvider>
    </UrqlProvider>,
  );
}

// formatRate ----------------------------------------------------------

describe('formatRate', () => {
  it('renders an idle connection as zero, not a dash', () => {
    expect(formatRate(0)).toBe('0 B/s');
  });

  it('rounds bytes below 1 KiB to whole bytes', () => {
    expect(formatRate(512.4)).toBe('512 B/s');
  });

  it('scales as formatBytes does', () => {
    expect(formatRate(10 * 1024)).toBe('10.0 KB/s');
    expect(formatRate(2.5 * 1024 ** 2)).toBe('2.5 MB/s');
  });
});

// formatBytes ---------------------------------------------------------

describe('formatBytes', () => {
  it('renders an em dash for an uncached/zero size', () => {
    expect(formatBytes(0)).toBe('—');
  });

  it('reports raw bytes below 1 KiB (no decimals)', () => {
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(1)).toBe('1 B');
  });

  it('scales to KB, MB, GB with one decimal (binary base)', () => {
    expect(formatBytes(1536)).toBe('1.5 KB'); // 1.5 * 1024
    expect(formatBytes(5_000_000)).toBe('4.8 MB'); // 5e6 / 1024^2 ≈ 4.768
    expect(formatBytes(3 * 1024 ** 3)).toBe('3.0 GB');
  });

  it('crosses unit boundaries at 1024, not 1000', () => {
    expect(formatBytes(1023)).toBe('1023 B');
    expect(formatBytes(1024)).toBe('1.0 KB');
  });
});

// ClustersProvider / useClusters --------------------------------------

describe('useClusters', () => {
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
      throw new Error(`unexpected ${cmd}`);
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('throws outside the provider', () => {
    function Bare() {
      useClusters();
      return null;
    }
    expect(() => render(<Bare />)).toThrow(/ClustersProvider/);
  });

  it('reports null before the first push', async () => {
    renderProvider();
    await flush();
    expect(screen.getByTestId('probe')).toHaveTextContent('null');
  });

  // A change whose cluster is null is a server-side field error (a nested non-null
  // field erroring nulls its parent), not the snapshot boundary. Folding it as one
  // would publish a half-listed fleet as the whole fleet.
  it('keeps the list null on a change carrying no cluster', async () => {
    renderProvider();
    await flush();

    await act(async () => {
      pushClusterChange('Added', null as unknown as object);
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('null');

    await act(async () => {
      pushCollectionWatchBookmark(channelFor, 'clusterCollectionWatch', 'cluster');
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('[]');
  });

  it('subscribes to the cluster, cache, and sync collection watches', async () => {
    renderProvider();
    await flush();
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_subscribe',
      expect.objectContaining({ query: expect.stringContaining('clusterCollectionWatch') }),
    );
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_subscribe',
      expect.objectContaining({ query: expect.stringContaining('clusterCacheCollectionWatch') }),
    );
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_subscribe',
      expect.objectContaining({ query: expect.stringContaining('clusterCacheHealthKeyedStateWatch') }),
    );
  });

  it('accumulates Added changes into the cluster list', async () => {
    renderProvider();
    await flush();

    await act(async () => {
      pushClusters(channelFor, [
        { id: 'u-a', name: 'prod-us' },
        { id: 'u-b', name: 'staging', syncEnabled: false },
      ]);
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us","staging"]');
  });

  it('removes a cluster on a Deleted change', async () => {
    renderProvider();
    await flush();

    await act(async () => {
      pushClusters(channelFor, [
        { id: 'u-a', name: 'prod-us' },
        { id: 'u-b', name: 'staging' },
      ]);
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us","staging"]');

    await act(async () => {
      pushClusterChange('Deleted', clusterOf({ id: 'u-a', name: 'prod-us' }));
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["staging"]');
  });

  // The boundary streams a record being torn down rather than filtering it, so this
  // fold is the only thing keeping a dying cluster out of every view.
  it('drops a cluster whose record is marked for deletion', async () => {
    renderProvider();
    await flush();

    await act(async () => {
      pushClusters(channelFor, [
        { id: 'u-a', name: 'prod-us' },
        { id: 'u-b', name: 'staging' },
      ]);
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us","staging"]');

    // A Modified, not a Deleted: the row is still there, wearing a tombstone.
    await act(async () => {
      pushClusterChange('Modified', clusterOf({ id: 'u-a', name: 'prod-us', deleting: true }));
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["staging"]');
  });

  // A tombstoned record in the snapshot never enters the list to begin with.
  it('drops a deletion-marked cluster arriving in the snapshot', async () => {
    renderProvider();
    await flush();

    await act(async () => {
      pushClusters(channelFor, [
        { id: 'u-a', name: 'prod-us', deleting: true },
        { id: 'u-b', name: 'staging' },
      ]);
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["staging"]');
  });

  it('joins a cache onto its cluster by matching spec.serverUid', async () => {
    renderProvider(<JoinProbe />);
    await flush();

    // Cluster arrives first, with no cache yet.
    await act(async () => {
      pushClusters(channelFor, [{ id: 'u-a', name: 'prod-us' }]);
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us:-"]');

    // Its cache lands on the other stream and is joined as the active cache.
    await act(async () => {
      pushCacheChange('Added', cacheOf({ id: 'u-a', name: 'prod-us' }));
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us:cache-u-a"]');
  });

  it('joins a cache\u2019s sync verdict onto it by cacheID', async () => {
    renderProvider(<SyncProbe />);
    await flush();

    // Cluster + cache arrive; the verdict is its own stream, so it is
    // simply not reported yet.
    await act(async () => {
      pushClusters(channelFor, [{ id: 'u-a', name: 'prod-us' }]);
      pushCacheChange('Added', cacheOf({ id: 'u-a', name: 'prod-us' }));
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us:-"]');

    // The record lands and its verdict reaches the cluster through the two-hop join.
    await act(async () => {
      pushSyncChange('Added', syncOf({ id: 'u-a', name: 'prod-us' }));
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us:Watching"]');

    // Its own Modified re-emits only that record — the cache never moved.
    await act(async () => {
      pushSyncChange('Modified', syncOf({ id: 'u-a', name: 'prod-us' }, 'Stale'));
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us:Stale"]');
  });

  // The verdict stream is a state watch with no delete of its own, so a verdict's
  // lifetime is its cache's — the sidecar drops a gone cache's verdict from its own
  // snapshot the same way. Which verdicts are live is DERIVED from the cache stream, so a
  // deletion takes effect on the frame that reports it: sweeping inside the verdict reducer
  // instead meant a fleet that went quiet right after a deletion (no further verdict
  // frames) never ran the sweep at all.
  it("drops a cache's verdict on the frame that deletes the cache", async () => {
    renderProvider(<SyncProbe />);
    await flush();

    const a = { id: 'u-a', name: 'prod-us' };
    const b = { id: 'u-b', name: 'prod-eu' };
    await act(async () => {
      pushClusters(channelFor, [a, b]);
      pushCacheChange('Added', cacheOf(a));
      pushCacheChange('Added', cacheOf(b));
      pushSyncChange('Added', syncOf(a));
      pushSyncChange('Added', syncOf(b));
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us:Watching","prod-eu:Watching"]');

    // A's cache is retired (a server-UID migration, or the cluster removed) and nothing
    // else happens — no verdict frame follows. B's cache is live, so B is untouched.
    await act(async () => {
      pushCacheChange('Deleted', cacheOf(a));
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us:-","prod-eu:Watching"]');
  });

  // Whether a verdict survives must not depend on what else the fleet happened to be doing.
  // A destructive sweep inside the verdict reducer made it: a verdict frame landing between
  // a cache's Deleted and its re-Added threw that cache's reading away for good, while the
  // same sequence with a quiet fleet kept it. Since cache ids are AUTOINCREMENT and never
  // reused, a re-Added id IS the same cache, so its last reading is its own — and deriving
  // liveness from the cache stream rather than evicting makes that hold either way.
  it("keeps a cache's own verdict across a delete and re-add of that cache", async () => {
    renderProvider(<SyncProbe />);
    await flush();

    const a = { id: 'u-a', name: 'prod-us' };
    const b = { id: 'u-b', name: 'prod-eu' };
    await act(async () => {
      pushClusters(channelFor, [a, b]);
      pushCacheChange('Added', cacheOf(a));
      pushCacheChange('Added', cacheOf(b));
      pushSyncChange('Added', syncOf(a));
      pushSyncChange('Added', syncOf(b));
    });

    await act(async () => {
      pushCacheChange('Deleted', cacheOf(a));
      // Unrelated traffic on the state watch — the only thing that used to decide A's fate.
      pushSyncChange('Modified', syncOf(b, 'Stale'));
      pushCacheChange('Added', cacheOf(a));
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us:Watching","prod-eu:Stale"]');
  });

  // The streams carry no mutual ordering, so a verdict routinely arrives before its
  // cache's frame. Eviction keys off an observed Deleted rather than "absent from the
  // cache map" for exactly this reason: the latter cannot tell "not here yet" from "gone",
  // and would drop the verdict of every cache whose frame is still in flight.
  it('keeps a verdict that arrives before its cache', async () => {
    renderProvider(<SyncProbe />);
    await flush();

    const a = { id: 'u-a', name: 'prod-us' };
    await act(async () => {
      pushClusters(channelFor, [a]);
      pushSyncChange('Added', syncOf(a));
    });
    await act(async () => {
      pushCacheChange('Added', cacheOf(a));
    });

    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us:Watching"]');
  });

  it('drops clusters and caches deleted during an outage once the transport reconnects', async () => {
    vi.useFakeTimers();
    renderProvider(<JoinProbe />);
    await flush();

    await act(async () => {
      pushClusters(channelFor, [
        { id: 'u-a', name: 'prod-us' },
        { id: 'u-b', name: 'staging' },
      ]);
      pushCacheChange('Added', cacheOf({ id: 'u-a', name: 'prod-us' }));
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us:cache-u-a","staging:-"]');

    // While the transport is down, prod-us and its cache are deleted
    // server-side. The reconnect replays only what still exists (staging) —
    // no Deleted frame is ever sent for the ones that vanished.
    await reconnectBothStreams();
    await act(async () => {
      pushClusterChange('Added', clusterOf({ id: 'u-b', name: 'staging' }));
      pushCollectionWatchBookmark(channelFor, 'clusterCollectionWatch', 'cluster');
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["staging:-"]');
  });

  it('resets to the not-reported state when the reconnect snapshot is empty', async () => {
    vi.useFakeTimers();
    renderProvider();
    await flush();

    await act(async () => {
      pushClusters(channelFor, [{ id: 'u-a', name: 'prod-us' }]);
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('["prod-us"]');

    // Everything was deleted during the outage: the reconnected stream
    // replays nothing, so no frame ever arrives to displace the stale map —
    // the reset alone must clear it.
    await reconnectBothStreams();
    expect(screen.getByTestId('probe')).toHaveTextContent('null');
  });
});
