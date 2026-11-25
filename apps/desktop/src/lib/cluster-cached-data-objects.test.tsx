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

// Same seams as cluster-cached-data-events.test.tsx: mock useFoldedSubscription (an
// accumulator stand-in that captures the reducer), the clusters provider, and the
// active-context hook.
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

const { useClusterCachedDataObjects } = await import('./cluster-cached-data-objects');

const PODS = { apiVersion: 'v1', resource: 'pods' };

function obj(uid: string, over: Partial<{ namespace: string; name: string }> = {}) {
  return {
    uid,
    apiVersion: 'v1',
    kind: 'Pod',
    namespace: over.namespace ?? 'default',
    name: over.name ?? uid,
    creationTimestamp: '2026-07-20T10:00:00Z',
  };
}

function clusterFixture(hasCache: boolean, cacheId = 'c1', serverUid = 'uid-1') {
  return {
    id: '1',
    spec: { source: { kubeconfig: { context: 'prod' } } },
    activeCache: hasCache ? { id: cacheId, spec: { serverUid }, status: { conditions: [] } } : null,
  };
}

const uids = (objects: { uid: string }[]) => objects.map((o) => o.uid);

// urql accumulator stand-in — folds a delta through the reducer captured on the last
// render. Each frame carries its own provenance (cache + kind), defaulting to the
// subscribed variables so a normal frame is always accepted.
let acc: unknown;
let lastArgs: { variables?: { cacheID?: string; apiVersion?: string; resource?: string }; pause?: boolean } | undefined;
let lastReducer: ((prev: unknown, frames: unknown[]) => unknown) | undefined;

function pushFrame(
  type: string,
  object: unknown,
  cacheID = lastArgs?.variables?.cacheID,
  apiVersion = lastArgs?.variables?.apiVersion,
  resource = lastArgs?.variables?.resource,
) {
  acc = lastReducer!(acc, [
    { clusterCachedDataObjectCollectionWatch: { type, cacheID, apiVersion, resource, object } },
  ]);
}

// The Bookmark closing the snapshot: what flips the watch from connecting to live.
function pushBookmark(
  cacheID = lastArgs?.variables?.cacheID,
  apiVersion = lastArgs?.variables?.apiVersion,
  resource = lastArgs?.variables?.resource,
) {
  acc = lastReducer!(acc, [
    { clusterCachedDataObjectCollectionWatch: { type: 'Bookmark', cacheID, apiVersion, resource, object: null } },
  ]);
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

describe('useClusterCachedDataObjects', () => {
  // A watch that may mount into a running one asks for a connection of its own.
  it('subscribes unshared when asked', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    renderHook(() => useClusterCachedDataObjects(PODS, { unshared: true }));
    expect(useFoldedSubscriptionMock.mock.lastCall?.[2]).toMatchObject({ unshared: true });
  });

  // Disabled, it reads as a paused watch: nothing streams and nothing is active.
  it('pauses while not enabled', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    const { result } = renderHook(() => useClusterCachedDataObjects(PODS, { enabled: false }));
    expect(lastArgs?.pause).toBe(true);
    expect(result.current.active).toBe(false);
  });

  it('accumulates the snapshot burst and returns objects sorted by (namespace, name)', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    const { result, rerender } = renderHook(() => useClusterCachedDataObjects(PODS));
    expect(result.current.objects).toEqual([]);

    pushFrame('Added', obj('b', { namespace: 'kube-system', name: 'b' }));
    pushFrame('Added', obj('a', { namespace: 'default', name: 'a' }));
    pushFrame('Added', obj('c', { namespace: 'default', name: 'c' }));
    rerender();
    // default/a, default/c, kube-system/b.
    expect(uids(result.current.objects)).toEqual(['a', 'c', 'b']);
  });

  it('updates an object in place on a Modified frame', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    const { result, rerender } = renderHook(() => useClusterCachedDataObjects(PODS));
    pushFrame('Added', obj('a', { name: 'pod-a' }));
    rerender();
    expect(result.current.objects[0].name).toBe('pod-a');

    pushFrame('Modified', obj('a', { name: 'pod-a-renamed' }));
    rerender();
    expect(result.current.objects).toHaveLength(1);
    expect(result.current.objects[0].name).toBe('pod-a-renamed');
  });

  it('adds an object on Added and removes it on Deleted', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    const { result, rerender } = renderHook(() => useClusterCachedDataObjects(PODS));
    pushFrame('Added', obj('a', { name: 'a' }));
    pushFrame('Added', obj('b', { name: 'b' }));
    rerender();
    expect(uids(result.current.objects)).toEqual(['a', 'b']);

    pushFrame('Deleted', obj('a', { name: 'a' }));
    rerender();
    expect(uids(result.current.objects)).toEqual(['b']);
  });

  it('drops the old cache’s objects on a cache swap until the new cache streams', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true, 'c1', 'uid-1')] });
    const { result, rerender } = renderHook(() => useClusterCachedDataObjects(PODS));
    expect(lastArgs?.variables?.cacheID).toBe('c1');
    pushFrame('Added', obj('a'));
    rerender();
    expect(result.current.objects).toHaveLength(1);

    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true, 'c2', 'uid-2')] });
    rerender();
    expect(lastArgs?.variables?.cacheID).toBe('c2');
    expect(result.current.objects).toEqual([]);

    pushFrame('Added', obj('z'));
    rerender();
    expect(uids(result.current.objects)).toEqual(['z']);
  });

  it('preserves the active cache’s objects when a late old-cache straggler arrives', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true, 'c1', 'uid-1')] });
    const { result, rerender } = renderHook(() => useClusterCachedDataObjects(PODS));
    pushFrame('Added', obj('a'));
    rerender();

    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true, 'c2', 'uid-2')] });
    rerender();
    pushFrame('Added', obj('x'), 'c2');
    rerender();
    expect(uids(result.current.objects)).toEqual(['x']);

    // A straggler from the superseded c1 subscription must NOT wipe or join c2's set.
    pushFrame('Added', obj('stale'), 'c1');
    rerender();
    expect(uids(result.current.objects)).toEqual(['x']);
  });

  it('drops the previous kind’s objects on a resource switch under one cache (same apiVersion)', () => {
    // Deployments and DaemonSets share apiVersion apps/v1 — the dashboard's workloads group,
    // the common navigation — so cacheID alone can't tell their frames apart; the fix keys on
    // the full (cacheID, apiVersion, resource) provenance carried by each frame.
    const deployments = { apiVersion: 'apps/v1', resource: 'deployments' };
    const daemonsets = { apiVersion: 'apps/v1', resource: 'daemonsets' };
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true, 'c1', 'uid-1')] });
    const { result, rerender } = renderHook((k: typeof deployments) => useClusterCachedDataObjects(k), {
      initialProps: deployments,
    });
    expect(lastArgs?.variables?.resource).toBe('deployments');
    pushFrame('Added', obj('dep'));
    rerender(deployments);
    expect(uids(result.current.objects)).toEqual(['dep']);

    // Switch to DaemonSets — same cache, so cacheID is unchanged; only the kind differs.
    rerender(daemonsets);
    expect(lastArgs?.variables?.resource).toBe('daemonsets');
    // The retained deployments set must not leak into the daemonsets view.
    expect(result.current.objects).toEqual([]);

    pushFrame('Added', obj('ds'));
    rerender(daemonsets);
    expect(uids(result.current.objects)).toEqual(['ds']);

    // A straggler from the still-draining deployments subscription (same cache, same
    // apiVersion, different resource) must NOT join the daemonsets set.
    pushFrame('Added', obj('stale-dep'), 'c1', 'apps/v1', 'deployments');
    rerender(daemonsets);
    expect(uids(result.current.objects)).toEqual(['ds']);
  });

  it('resets on a transport reconnect so an object deleted during the outage is gone after replay', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    const { result, rerender } = renderHook(() => useClusterCachedDataObjects(PODS));
    pushFrame('Added', obj('a', { name: 'a' }));
    pushFrame('Added', obj('b', { name: 'b' }));
    rerender();
    expect(uids(result.current.objects)).toEqual(['a', 'b']);

    pushReset();
    pushFrame('Added', obj('b', { name: 'b' }));
    rerender();
    expect(uids(result.current.objects)).toEqual(['b']);
  });

  it('pauses the watch and reports active=false when there is no active cache', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(false)] });
    const { result } = renderHook(() => useClusterCachedDataObjects(PODS));
    expect(result.current.active).toBe(false);
    expect(result.current.objects).toEqual([]);
    expect(lastArgs?.pause).toBe(true);
  });

  it('reports connecting → live across the first frame, and reconnecting on a drop', () => {
    useClustersMock.mockReturnValue({ clusters: [clusterFixture(true)] });
    statusState.snapshot = { connected: false, generation: 0 };
    const { result, rerender } = renderHook(() => useClusterCachedDataObjects(PODS));
    expect(result.current.active).toBe(true);
    expect(result.current.phase).toBe('connecting');

    statusState.snapshot = { connected: true, generation: 0 };
    pushFrame('Added', obj('a'));
    rerender();
    // A frame is not the whole snapshot: still connecting until the Bookmark.
    expect(result.current.phase).toBe('connecting');

    pushBookmark();
    rerender();
    expect(result.current.phase).toBe('live');

    statusState.snapshot = { connected: false, generation: 0 };
    rerender();
    expect(result.current.phase).toBe('reconnecting');
    expect(result.current.objects).toHaveLength(1);
  });
});
