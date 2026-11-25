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
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { useClusterCachedDataObjectsMock } = vi.hoisted(() => ({
  useClusterCachedDataObjectsMock:
    vi.fn<typeof import('@/lib/cluster-cached-data-objects').useClusterCachedDataObjects>(),
}));
vi.mock('@/lib/cluster-cached-data-objects', () => ({ useClusterCachedDataObjects: useClusterCachedDataObjectsMock }));

const { ClusterNamespacesProvider, useClusterNamespaces } = await import('./cluster-namespaces');

function ns(name: string) {
  return { uid: name, apiVersion: 'v1', kind: 'Namespace', namespace: '', name, creationTimestamp: null, rawJSON: {} };
}

function wrapper(enabled: boolean) {
  return ({ children }: { children: ReactNode }) => (
    <ClusterNamespacesProvider enabled={enabled}>{children}</ClusterNamespacesProvider>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  useClusterCachedDataObjectsMock.mockReturnValue({ objects: [], active: true, phase: 'connecting' });
});

describe('ClusterNamespacesProvider', () => {
  it('lists the namespaces by name, with the watch’s phase', () => {
    useClusterCachedDataObjectsMock.mockReturnValue({
      objects: [ns('checkout'), ns('default'), ns('kube-system')],
      active: true,
      phase: 'live',
    });
    const { result } = renderHook(useClusterNamespaces, { wrapper: wrapper(true) });
    expect(result.current).toEqual({ namespaces: ['checkout', 'default', 'kube-system'], active: true, phase: 'live' });
  });

  // A Namespaces page watches the same kind and can mount after this one, or before.
  it('watches namespaces on a connection of its own, only while enabled', () => {
    renderHook(useClusterNamespaces, { wrapper: wrapper(false) });
    expect(useClusterCachedDataObjectsMock).toHaveBeenCalledWith(
      { apiVersion: 'v1', resource: 'namespaces' },
      { enabled: false, unshared: true },
    );
  });

  it('refuses to be read outside the provider', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => renderHook(useClusterNamespaces)).toThrow(/ClusterNamespacesProvider/);
  });
});
