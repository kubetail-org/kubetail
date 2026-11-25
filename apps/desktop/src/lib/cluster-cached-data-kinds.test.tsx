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

import { render, renderHook, screen } from '@testing-library/react';
import { useState } from 'react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

// Mocks ---------------------------------------------------------------

// Same seams as cluster-cached-data-events.test.tsx: an accumulator stand-in for
// `useFoldedSubscription` that captures the reducer, and the active-cluster join
// stubbed to name a cache.
const { useFoldedSubscriptionMock, useActiveClusterMock, useNamespaceScopeMock } = vi.hoisted(() => ({
  useFoldedSubscriptionMock: vi.fn(),
  useActiveClusterMock: vi.fn(),
  useNamespaceScopeMock: vi.fn(),
}));

vi.mock('@/lib/graphql/use-folded-subscription', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/graphql/use-folded-subscription')>()),
  useFoldedSubscription: useFoldedSubscriptionMock,
}));
vi.mock('@/lib/active-cluster', () => ({ useActiveCluster: useActiveClusterMock }));
vi.mock('@/lib/namespace-scope', () => ({ useNamespaceScope: useNamespaceScopeMock }));

const { ClusterCachedDataKindsProvider, useClusterCachedDataKinds } = await import('./cluster-cached-data-kinds');

// Helpers -------------------------------------------------------------

function kind(resource: string, over: Partial<{ apiVersion: string; count: number }> = {}) {
  return {
    apiVersion: over.apiVersion ?? 'apps/v1',
    kind: resource.replace(/s$/, ''),
    resource,
    scope: 'Namespaced',
    isCRD: false,
    count: over.count ?? 1,
    printerColumns: [],
  };
}

// The provider holds the subscription; the hook under test reads it.
function wrap(enabled = true) {
  return ({ children }: { children: ReactNode }) => (
    <ClusterCachedDataKindsProvider enabled={enabled}>{children}</ClusterCachedDataKindsProvider>
  );
}

function renderKinds(enabled = true) {
  return renderHook(() => useClusterCachedDataKinds(), { wrapper: wrap(enabled) });
}

const resources = (kinds: { resource: string }[]) => kinds.map((k) => k.resource);

// urql accumulator stand-in — folds a delta through the reducer captured on the last
// render, exactly as urql's live handler would.
let acc: unknown;
let lastArgs: { variables?: { cacheID?: string; namespace?: string }; pause?: boolean } | undefined;
let lastReducer: ((prev: unknown, frames: unknown[]) => unknown) | undefined;
let connected: boolean;

// A frame echoes the namespace it was subscribed with, "" for the whole cache.
function pushFrame(
  type: string,
  item: unknown,
  cacheID = lastArgs?.variables?.cacheID,
  namespace = lastArgs?.variables?.namespace ?? '',
) {
  acc = lastReducer!(acc, [{ clusterCachedDataKindCollectionWatch: { type, cacheID, namespace, kind: item } }]);
}

// The Bookmark closing the snapshot: what flips the watch from connecting to live.
function pushBookmark(cacheID = lastArgs?.variables?.cacheID, namespace = lastArgs?.variables?.namespace ?? '') {
  acc = lastReducer!(acc, [
    { clusterCachedDataKindCollectionWatch: { type: 'Bookmark', cacheID, namespace, kind: null } },
  ]);
}

beforeEach(() => {
  vi.clearAllMocks();
  acc = undefined;
  lastArgs = undefined;
  lastReducer = undefined;
  connected = true;
  useActiveClusterMock.mockReturnValue({ clusterID: '1', cacheID: 'cache-1', active: true });
  useNamespaceScopeMock.mockReturnValue({ namespace: undefined, setNamespace: vi.fn() });
  useFoldedSubscriptionMock.mockImplementation((args: typeof lastArgs, reducer: typeof lastReducer) => {
    lastArgs = args;
    lastReducer = reducer;
    return { data: acc, connected };
  });
});

// Tests ---------------------------------------------------------------

describe('useClusterCachedDataKinds', () => {
  it('accumulates the catalog in the order the cache served it', () => {
    const { result, rerender } = renderKinds();
    pushFrame('Added', kind('deployments'));
    pushFrame('Added', kind('pods', { apiVersion: 'v1' }));
    pushBookmark();
    rerender();

    expect(resources(result.current.kinds)).toEqual(['deployments', 'pods']);
    expect(result.current.phase).toBe('live');
  });

  it('stays connecting until the Bookmark closes the snapshot', () => {
    const { result, rerender } = renderKinds();
    pushFrame('Added', kind('deployments'));
    rerender();
    // A partial snapshot is not the catalog: rendering "no kinds" here is the bug.
    expect(result.current.phase).toBe('connecting');

    pushBookmark();
    rerender();
    expect(result.current.phase).toBe('live');
  });

  it('applies a live count change in place, keyed on apiVersion and plural', () => {
    const { result, rerender } = renderKinds();
    pushFrame('Added', kind('deployments', { count: 1 }));
    pushBookmark();
    pushFrame('Modified', kind('deployments', { count: 7 }));
    rerender();

    expect(result.current.kinds).toHaveLength(1);
    expect(result.current.kinds[0].count).toBe(7);
  });

  it('keeps two groups’ same plural apart', () => {
    const { result, rerender } = renderKinds();
    pushFrame('Added', kind('widgets', { apiVersion: 'a.example.com/v1' }));
    pushFrame('Added', kind('widgets', { apiVersion: 'b.example.com/v1' }));
    pushBookmark();
    rerender();

    expect(result.current.kinds).toHaveLength(2);
  });

  it('drops a kind the cache stopped serving', () => {
    const { result, rerender } = renderKinds();
    pushFrame('Added', kind('deployments'));
    pushFrame('Added', kind('pods', { apiVersion: 'v1' }));
    pushBookmark();
    pushFrame('Deleted', kind('pods', { apiVersion: 'v1' }));
    rerender();

    expect(resources(result.current.kinds)).toEqual(['deployments']);
  });

  it('ignores a straggler from a superseded cache', () => {
    const { result, rerender } = renderKinds();
    pushFrame('Added', kind('deployments'));
    pushBookmark();
    pushFrame('Added', kind('secrets', { apiVersion: 'v1' }), 'cache-0');
    rerender();

    expect(resources(result.current.kinds)).toEqual(['deployments']);
  });

  it('counts the whole cache with no namespace chosen', () => {
    renderKinds();
    expect(lastArgs?.variables?.namespace).toBe('');
  });

  // The counts are the namespace's, so a new namespace is a new catalog.
  it('re-subscribes counting in the namespace scope when it changes', () => {
    const { result, rerender } = renderKinds();
    pushFrame('Added', kind('deployments'));
    pushBookmark();
    rerender();
    expect(result.current.kinds).toHaveLength(1);

    useNamespaceScopeMock.mockReturnValue({ namespace: 'checkout', setNamespace: vi.fn() });
    rerender();
    expect(lastArgs?.variables?.namespace).toBe('checkout');
    expect(result.current.kinds).toEqual([]);
    expect(result.current.phase).toBe('connecting');
  });

  it('ignores a straggler counted in another namespace', () => {
    useNamespaceScopeMock.mockReturnValue({ namespace: 'checkout', setNamespace: vi.fn() });
    const { result, rerender } = renderKinds();
    pushFrame('Added', kind('deployments'));
    pushBookmark();
    pushFrame('Added', kind('secrets', { apiVersion: 'v1' }), 'cache-1', '');
    rerender();

    expect(resources(result.current.kinds)).toEqual(['deployments']);
  });

  it('serves every consumer from one subscription', () => {
    const { result } = renderHook(() => ({ nav: useClusterCachedDataKinds(), route: useClusterCachedDataKinds() }), {
      wrapper: wrap(),
    });

    // One render, two consumers, one watch.
    expect(useFoldedSubscriptionMock).toHaveBeenCalledTimes(1);
    expect(result.current.nav).toBe(result.current.route);
  });

  it('gives a consumer mounted late the whole catalog', () => {
    // As urql serves a running stream: the instance that opened it folds every frame,
    // one mounting later sees none of them. Only a watch held above the consumer passes.
    let instances = 0;
    useFoldedSubscriptionMock.mockImplementation((args: typeof lastArgs, reducer: typeof lastReducer) => {
      const [instance] = useState(() => {
        instances += 1;
        return instances;
      });
      if (instance > 1) return { data: undefined, connected };
      lastArgs = args;
      lastReducer = reducer;
      return { data: acc, connected };
    });
    function Names() {
      const { kinds, phase } = useClusterCachedDataKinds();
      return <p>{`${phase}: ${resources(kinds).join(',')}`}</p>;
    }
    function Window({ navOpen }: { navOpen: boolean }) {
      return <ClusterCachedDataKindsProvider enabled>{navOpen && <Names />}</ClusterCachedDataKindsProvider>;
    }
    const { rerender } = render(<Window navOpen />);
    pushFrame('Added', kind('deployments'));
    pushBookmark();
    rerender(<Window navOpen={false} />);

    // The nav's card reopening: its first read is the complete, live catalog.
    rerender(<Window navOpen />);
    expect(screen.getByText('live: deployments')).toBeInTheDocument();
    expect(instances).toBe(1);
  });

  it('pauses while disabled, so nothing subscribes outside the dashboard', () => {
    const { result, rerender } = renderKinds();
    pushFrame('Added', kind('deployments'));
    pushBookmark();
    rerender();
    expect(result.current.kinds).toHaveLength(1);

    // Leaving the dashboard: the catalog it read there does not linger.
    const { result: paused } = renderKinds(false);
    expect(lastArgs?.pause).toBe(true);
    expect(paused.current.kinds).toEqual([]);
    expect(paused.current.active).toBe(false);
    expect(paused.current.cacheID).toBeUndefined();
  });

  it('names the cache its kinds came from', () => {
    const { result } = renderKinds();
    expect(result.current.cacheID).toBe('cache-1');
  });

  it('refuses a read outside the provider', () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => renderHook(() => useClusterCachedDataKinds())).toThrow(/ClusterCachedDataKindsProvider/);
    error.mockRestore();
  });

  it('pauses without an active cache, so nothing subscribes', () => {
    useActiveClusterMock.mockReturnValue({ clusterID: undefined, cacheID: undefined, active: false });
    const { result } = renderKinds();

    expect(lastArgs?.pause).toBe(true);
    expect(result.current.active).toBe(false);
    expect(result.current.cacheID).toBeUndefined();
    expect(result.current.kinds).toEqual([]);
  });

  it('re-subscribes under the new cache when the cluster swaps', () => {
    const { result, rerender } = renderKinds();
    pushFrame('Added', kind('deployments'));
    pushBookmark();
    rerender();
    expect(result.current.kinds).toHaveLength(1);

    // The previous cache's set must not carry over into the new one's.
    useActiveClusterMock.mockReturnValue({ clusterID: '2', cacheID: 'cache-2', active: true });
    rerender();
    expect(result.current.kinds).toEqual([]);
    expect(result.current.phase).toBe('connecting');
    expect(lastArgs?.variables?.cacheID).toBe('cache-2');
  });

  it('holds the catalog while the transport reconnects', () => {
    const { result, rerender } = renderKinds();
    pushFrame('Added', kind('deployments'));
    pushBookmark();
    rerender();

    connected = false;
    rerender();
    expect(result.current.phase).toBe('reconnecting');
    expect(result.current.kinds).toHaveLength(1);
  });
});
