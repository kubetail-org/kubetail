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

// Mocks ---------------------------------------------------------------

// The two inputs of the join: the window's context and the clusters stream.
const { useClustersMock, useActiveKubeContextMock } = vi.hoisted(() => ({
  useClustersMock: vi.fn(),
  useActiveKubeContextMock: vi.fn(),
}));
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: useClustersMock,
}));
vi.mock('@/lib/active-kube-context', () => ({ useActiveKubeContext: useActiveKubeContextMock }));

const { useActiveCluster } = await import('./active-cluster');

// Helpers -------------------------------------------------------------

// A kubeconfig-sourced cluster, with or without a cache the sync has armed.
function kubeconfigCluster(context: string, cacheID: string | null, id = 'cluster-1') {
  return {
    id,
    spec: { source: { kubeconfig: { context } } },
    activeCache: cacheID ? { id: cacheID } : null,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  useActiveKubeContextMock.mockReturnValue({ context: 'prod' });
});

// Tests ---------------------------------------------------------------

describe('useActiveCluster', () => {
  it('joins the active context to its cluster and cache', () => {
    useClustersMock.mockReturnValue({
      clusters: [kubeconfigCluster('staging', 'cache-s', 'cluster-s'), kubeconfigCluster('prod', 'cache-p')],
    });
    const { result } = renderHook(() => useActiveCluster());
    expect(result.current.clusterID).toBe('cluster-1');
    expect(result.current.cacheID).toBe('cache-p');
    expect(result.current.active).toBe(true);
  });

  it('stays inactive while the cluster has no cache, so its data watches pause', () => {
    // A never-synced or paused cluster: there is a row, but nothing to read from.
    useClustersMock.mockReturnValue({ clusters: [kubeconfigCluster('prod', null)] });
    const { result } = renderHook(() => useActiveCluster());
    expect(result.current.cluster).toBeDefined();
    expect(result.current.cacheID).toBeUndefined();
    expect(result.current.active).toBe(false);
  });

  it('resolves nothing when no cluster carries the active context', () => {
    useClustersMock.mockReturnValue({ clusters: [kubeconfigCluster('staging', 'cache-s')] });
    const { result } = renderHook(() => useActiveCluster());
    expect(result.current.cluster).toBeUndefined();
    expect(result.current.active).toBe(false);
  });

  it('ignores clusters that came from anywhere but the kubeconfig', () => {
    // Only kubeconfig-sourced records carry a context, so nothing else can match.
    useClustersMock.mockReturnValue({ clusters: [{ id: 'x', spec: { source: {} }, activeCache: { id: 'c' } }] });
    const { result } = renderHook(() => useActiveCluster());
    expect(result.current.cluster).toBeUndefined();
    expect(result.current.active).toBe(false);
  });

  // The phase is what tells this apart from a context no cluster carries.
  it('resolves nothing before the clusters snapshot lands, and says it is still connecting', () => {
    useActiveKubeContextMock.mockReturnValue({ context: '', phase: 'connecting' });
    useClustersMock.mockReturnValue({ clusters: undefined });
    const { result } = renderHook(() => useActiveCluster());
    expect(result.current.cluster).toBeUndefined();
    expect(result.current.active).toBe(false);
    expect(result.current.phase).toBe('connecting');
  });

  it('follows the context to another cluster', () => {
    useClustersMock.mockReturnValue({
      clusters: [kubeconfigCluster('prod', 'cache-p'), kubeconfigCluster('staging', 'cache-s', 'cluster-s')],
    });
    const { result, rerender } = renderHook(() => useActiveCluster());
    expect(result.current.cacheID).toBe('cache-p');

    useActiveKubeContextMock.mockReturnValue({ context: 'staging' });
    rerender();
    expect(result.current.clusterID).toBe('cluster-s');
    expect(result.current.cacheID).toBe('cache-s');
  });
});
