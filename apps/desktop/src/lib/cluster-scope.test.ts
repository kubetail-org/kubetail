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

import type { Cluster } from '@/lib/clusters';
import { clusterRecord } from '@/test-utils';

const { state } = vi.hoisted(() => ({ state: { clusters: null as Cluster[] | null, contexts: [] as string[] } }));
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: () => ({ clusters: state.clusters, connected: true }),
}));
vi.mock('@/lib/kube-config', () => ({
  useKubeConfig: () => ({ kubeConfig: state.clusters && { contexts: state.contexts.map((name) => ({ name })) } }),
}));

const { EVERY, useScope } = await import('./cluster-scope');

function cluster(context: string, present = true): Cluster {
  const c = clusterRecord({ context });
  return {
    ...c,
    status: { ...c.status, source: { kubeconfig: { ...c.status.source.kubeconfig!, isPresent: present } } },
  };
}

const ids = (clusters: Cluster[]) => clusters.map((c) => c.id);

beforeEach(() => {
  state.clusters = [cluster('prod'), cluster('zeta-old', false), cluster('dev'), cluster('alpha-old', false)];
  state.contexts = ['prod', 'dev'];
});

describe('useScope', () => {
  it("lists the kubeconfig's clusters in its order, then the archived by name", () => {
    const { result } = renderHook(() => useScope(EVERY));
    expect(ids(result.current.present)).toEqual(['id-prod', 'id-dev']);
    expect(ids(result.current.archived)).toEqual(['id-alpha-old', 'id-zeta-old']);
    expect(result.current.cluster).toBeUndefined();
  });

  it('finds the picked cluster, present or archived, and none once its record is gone', () => {
    const { result, rerender } = renderHook(({ picked }) => useScope(picked), { initialProps: { picked: 'id-dev' } });
    expect(result.current.cluster?.id).toBe('id-dev');

    rerender({ picked: 'id-zeta-old' });
    expect(result.current.cluster?.id).toBe('id-zeta-old');

    state.clusters = [cluster('prod')];
    rerender({ picked: 'id-zeta-old' });
    expect(result.current.cluster).toBeUndefined();
  });

  // The kubeconfig's watch can still name a context the clusters' watch has
  // already marked gone.
  it('lists a cluster once while the two watches disagree', () => {
    state.clusters = [cluster('prod'), cluster('dev', false)];
    const { result } = renderHook(() => useScope(EVERY));
    expect(ids(result.current.present)).toEqual(['id-prod', 'id-dev']);
    expect(result.current.archived).toEqual([]);
  });
});
