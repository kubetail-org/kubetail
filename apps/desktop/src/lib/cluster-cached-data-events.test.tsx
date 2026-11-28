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

import { renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

// Same seams as dashboard-nav.test.tsx: mock useFoldedSubscription (an accumulator
// stand-in that captures the reducer), the clusters provider, and the active-context
// hook.
const { useFoldedSubscriptionMock } = vi.hoisted(() => ({ useFoldedSubscriptionMock: vi.fn() }));
const { useClustersMock, useActiveKubeContextMock } = vi.hoisted(() => ({
  useClustersMock: vi.fn(),
  useActiveKubeContextMock: vi.fn(),
}));
const { statusState } = vi.hoisted(() => ({
  statusState: { snapshot: { connected: true, generation: 0 } },
}));

vi.mock('@/lib/graphql/use-folded-subscription', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/graphql/use-folded-subscription')>()),
  useFoldedSubscription: useFoldedSubscriptionMock,
}));
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: useClustersMock,
}));
vi.mock('@/lib/active-kube-context', () => ({ useActiveKubeContext: useActiveKubeContextMock }));

const { countText, lastSeenMs, lastSeenSortValue, useClusterCachedDataEvents } =
  await import('./cluster-cached-data-events');

// Event fixtures. `at` drives lastSeen (ISO), used to assert newest-first ordering.
function evt(uid: string, over: Partial<{ type: string; reason: string; count: number; at: string }> = {}) {
  const at = over.at ?? '2026-07-20T10:00:00Z';
  return {
    uid,
    type: over.type ?? 'Warning',
    reason: over.reason ?? 'BackOff',
    message: 'Back-off restarting',
    count: over.count ?? 1,
    firstSeen: at,
    lastSeen: at,
    involvedKind: 'Pod',
    involvedNamespace: 'default',
    involvedName: 'my-pod',
  };
}

function clusterFixture(hasCache: boolean, cacheId = 'c1', serverUid = 'uid-1') {
  return {
    id: '1',
    spec: { source: { kubeconfig: { context: 'prod' } } },
    activeCache: hasCache ? { id: cacheId, spec: { serverUid }, status: { conditions: [] } } : null,
  };
}

const uids = (events: { uid: string }[]) => events.map((e) => e.uid);

// urql accumulator stand-in — folds a delta through the reducer captured on the last
// render, exactly as urql's live handler would. Each frame carries its own cache
// provenance (defaulting to the subscribed cache, overridable to model a straggler).
let acc: unknown;
let lastArgs: { variables?: { cacheID?: string }; pause?: boolean } | undefined;
let lastReducer: ((prev: unknown, frames: unknown[]) => unknown) | undefined;

function pushFrame(type: string, event: unknown, cacheID = lastArgs?.variables?.cacheID) {
  acc = lastReducer!(acc, [{ clusterCachedDataEventCollectionWatch: { type, cacheID, event } }]);
}

// The Bookmark closing the snapshot: what flips the watch from connecting to live.
function pushBookmark(cacheID = lastArgs?.variables?.cacheID) {
  acc = lastReducer!(acc, [{ clusterCachedDataEventCollectionWatch: { type: 'Bookmark', cacheID, event: null } }]);
}

function pushReset() {
  acc = undefined;
  statusState.snapshot = { connected: true, generation: statusState.snapshot.generation + 1 };
}

beforeEach(() => {
  vi.clearAllMocks();
  acc = undefined;
  lastArgs = undefined;
  lastReducer = undefined;
  statusState.snapshot = { connected: true, generation: 0 };
  useActiveKubeContextMock.mockReturnValue({ context: 'prod' });
  useFoldedSubscriptionMock.mockImplementation((args: typeof lastArgs, reducer: typeof lastReducer) => {
    lastArgs = args;
    lastReducer = reducer;
    return { data: acc, connected: statusState.snapshot.connected };
  });
});

describe('useClusterCachedDataEvents', () => {
  it('accumulates the snapshot burst and returns events newest-first by lastSeen', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    const { result, rerender } = renderHook(() => useClusterCachedDataEvents());
    expect(result.current.events).toEqual([]);

    pushFrame('Added', evt('a', { at: '2026-07-20T10:00:00Z' }));
    pushFrame('Added', evt('b', { at: '2026-07-20T10:05:00Z' }));
    pushFrame('Added', evt('c', { at: '2026-07-20T10:02:00Z' }));
    rerender();
    // Newest lastSeen first: b (10:05), c (10:02), a (10:00).
    expect(uids(result.current.events)).toEqual(['b', 'c', 'a']);
  });

  it('updates an event in place on a Modified frame (re-fire bumps count)', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    const { result, rerender } = renderHook(() => useClusterCachedDataEvents());
    pushFrame('Added', evt('a', { count: 1 }));
    rerender();
    expect(result.current.events[0].count).toBe(1);

    pushFrame('Modified', evt('a', { count: 9 }));
    rerender();
    expect(result.current.events).toHaveLength(1);
    expect(result.current.events[0].count).toBe(9);
  });

  it('adds an event on Added and removes it on Deleted', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    const { result, rerender } = renderHook(() => useClusterCachedDataEvents());
    pushFrame('Added', evt('a', { at: '2026-07-20T10:00:00Z' }));
    pushFrame('Added', evt('b', { at: '2026-07-20T10:01:00Z' }));
    rerender();
    expect(uids(result.current.events)).toEqual(['b', 'a']);

    pushFrame('Deleted', evt('a'));
    rerender();
    expect(uids(result.current.events)).toEqual(['b']);
  });

  it('drops the old cache’s events on a cache swap until the new cache streams', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true, 'c1', 'uid-1')] });
    const { result, rerender } = renderHook(() => useClusterCachedDataEvents());
    expect(lastArgs?.variables?.cacheID).toBe('c1');
    pushFrame('Added', evt('a'));
    rerender();
    expect(result.current.events).toHaveLength(1);

    // Swap to c2: urql retains c1's data until the new cache's first frame, so the
    // cache-aware guard rejects it → empty in the meantime.
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true, 'c2', 'uid-2')] });
    rerender();
    expect(lastArgs?.variables?.cacheID).toBe('c2');
    expect(result.current.events).toEqual([]);

    pushFrame('Added', evt('z'));
    rerender();
    expect(uids(result.current.events)).toEqual(['z']);
  });

  it('preserves the active cache’s events when a late old-cache straggler arrives', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true, 'c1', 'uid-1')] });
    const { result, rerender } = renderHook(() => useClusterCachedDataEvents());
    pushFrame('Added', evt('a'));
    rerender();

    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true, 'c2', 'uid-2')] });
    rerender();
    pushFrame('Added', evt('x'), 'c2');
    rerender();
    expect(uids(result.current.events)).toEqual(['x']);

    // A straggler from the superseded c1 subscription must NOT wipe or join c2's set.
    pushFrame('Added', evt('stale'), 'c1');
    rerender();
    expect(uids(result.current.events)).toEqual(['x']);

    // A legitimate c2 delta still patches the intact set.
    pushFrame('Added', evt('y', { at: '2026-07-20T11:00:00Z' }), 'c2');
    rerender();
    expect(uids(result.current.events)).toEqual(['y', 'x']);
  });

  it('resets on a transport reconnect so an event deleted during the outage is gone after replay', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    const { result, rerender } = renderHook(() => useClusterCachedDataEvents());
    pushFrame('Added', evt('a'));
    pushFrame('Added', evt('b'));
    rerender();
    expect(uids(result.current.events).toSorted()).toEqual(['a', 'b']);

    // Reconnect: 'a' was deleted during the outage, so the replay carries only 'b'
    // (no Deleted frame ever arrives for 'a').
    pushReset();
    pushFrame('Added', evt('b'));
    rerender();
    expect(uids(result.current.events)).toEqual(['b']);
  });

  it('pauses and reports active=false when there is no active cluster or cache', () => {
    useClustersMock.mockReturnValue({ clusters: [] });
    const { result } = renderHook(() => useClusterCachedDataEvents());
    expect(result.current.active).toBe(false);
    expect(result.current.events).toEqual([]);
    expect(lastArgs?.pause).toBe(true);
  });

  // The Bookmark carries no item. Keying it would put a phantom row in every table.
  it('folds the Bookmark away instead of keying it as a row', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    const { result, rerender } = renderHook(() => useClusterCachedDataEvents());

    pushFrame('Added', evt('a'));
    pushBookmark();
    rerender();
    expect(uids(result.current.events)).toEqual(['a']);
  });

  // A change whose item is null is a server-side field error (a nested non-null
  // field erroring nulls its parent), not the snapshot boundary. Folding it as one
  // would report a still-loading table as live and empty.
  it('does not treat a change with no item as the Bookmark', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    const { result, rerender } = renderHook(() => useClusterCachedDataEvents());

    pushFrame('Added', null);
    rerender();
    expect(result.current.phase).toBe('connecting');
    expect(result.current.events).toEqual([]);

    pushBookmark();
    rerender();
    expect(result.current.phase).toBe('live');
  });

  // An empty snapshot is a real answer, not a missing one — the whole point of the
  // Bookmark. Reported as live with no rows, so a table renders its empty state.
  it('reports live with no items on an empty snapshot', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    const { result, rerender } = renderHook(() => useClusterCachedDataEvents());
    expect(result.current.phase).toBe('connecting');

    pushBookmark();
    rerender();
    expect(result.current.phase).toBe('live');
    expect(result.current.events).toEqual([]);
  });

  it('reports connecting → live across the first frame, and reconnecting on a drop', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    statusState.snapshot = { connected: false, generation: 0 };
    const { result, rerender } = renderHook(() => useClusterCachedDataEvents());
    expect(result.current.active).toBe(true);
    expect(result.current.phase).toBe('connecting');

    statusState.snapshot = { connected: true, generation: 0 };
    pushFrame('Added', evt('a'));
    rerender();
    // A frame is not the whole snapshot: still connecting until the Bookmark.
    expect(result.current.phase).toBe('connecting');

    pushBookmark();
    rerender();
    expect(result.current.phase).toBe('live');

    statusState.snapshot = { connected: false, generation: 0 };
    rerender();
    expect(result.current.phase).toBe('reconnecting');
    expect(result.current.events).toHaveLength(1); // last-known held across the drop
  });
});

describe('event readings', () => {
  const seen = (lastSeen: string | null) => ({ lastSeen }) as Parameters<typeof lastSeenMs>[0];

  it('counts a repeated event with a ×, and one seen once as 1', () => {
    expect([countText(3), countText(1), countText(0)]).toEqual(['×3', '1', '1']);
  });

  it('reads when it was last seen, NaN when absent or unparsable', () => {
    expect(lastSeenMs(seen('2026-07-20T10:00:00Z'))).toBe(Date.parse('2026-07-20T10:00:00Z'));
    expect(lastSeenMs(seen(null))).toBeNaN();
    expect(lastSeenMs(seen('soon'))).toBeNaN();
  });

  it('sorts newest first, an event never seen with no value', () => {
    expect(lastSeenSortValue(seen('2026-07-20T10:00:00Z'))).toBe(-Date.parse('2026-07-20T10:00:00Z'));
    expect(lastSeenSortValue(seen(null))).toBeUndefined();
  });
});
