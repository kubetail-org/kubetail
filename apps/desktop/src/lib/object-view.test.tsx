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

import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { RightSidebarProvider, useRightSidebar } from '@/lib/right-sidebar';

import { ObjectViewProvider, useObjectView } from './object-view';

const active = vi.hoisted(() => ({ clusterID: 'c1' as string | undefined }));
vi.mock('@/lib/active-cluster', () => ({ useActiveCluster: () => active }));

const POD = { apiVersion: 'v1', resource: 'pods', kind: 'Pod', uid: 'u1', namespace: 'shop', name: 'web-1' };

const wrapper = ({ children }: { children: ReactNode }) => (
  <RightSidebarProvider mode="dashboard">
    <ObjectViewProvider>{children}</ObjectViewProvider>
  </RightSidebarProvider>
);

describe('useObjectView', () => {
  beforeEach(() => {
    active.clusterID = 'c1';
    // The right sidebar's open flag persists in localStorage.
    localStorage.clear();
  });

  it('focuses an object on a tab and opens the right sidebar, moves between tabs, and clears it', () => {
    const { result } = renderHook(() => ({ view: useObjectView(), sidebar: useRightSidebar() }), { wrapper });
    expect(result.current.view.object).toBeNull();
    expect(result.current.sidebar.open).toBe(false);

    act(() => result.current.view.set(POD, 'events'));
    expect(result.current.view.object).toEqual(POD);
    expect(result.current.view.tab).toBe('events');
    expect(result.current.sidebar.open).toBe(true);

    act(() => result.current.view.setTab('yaml'));
    expect(result.current.view.tab).toBe('yaml');

    act(() => result.current.view.clear());
    expect(result.current.view.object).toBeNull();
  });

  // Back, a deep link or a new default context, as well as the picker.
  it('drops the object once the window is on another cluster', () => {
    const { result, rerender } = renderHook(() => useObjectView(), { wrapper });
    act(() => result.current.set(POD, 'yaml'));

    active.clusterID = 'c2';
    rerender();
    expect(result.current.object).toBeNull();

    // And does not bring it back with its cluster.
    active.clusterID = 'c1';
    rerender();
    expect(result.current.object).toBeNull();
  });

  it('keeps the object while no cluster is resolved', () => {
    const { result, rerender } = renderHook(() => useObjectView(), { wrapper });
    act(() => result.current.set(POD, 'yaml'));
    active.clusterID = undefined;
    rerender();
    expect(result.current.object).toEqual(POD);
  });

  it('throws outside its provider', () => {
    expect(() => renderHook(() => useObjectView())).toThrow(/ObjectViewProvider/);
  });
});
