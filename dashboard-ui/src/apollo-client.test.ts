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

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

import { CONSOLE_NODES_LIST_FETCH } from '@/lib/graphql/dashboard/ops';

import { DashboardCustomCache, k8sPagination, waitUntilOnline, waitUntilVisible } from './apollo-client';

describe('k8sPagination merge logic', () => {
  let mergeFunction: ReturnType<typeof k8sPagination>['merge'];

  beforeEach(() => {
    mergeFunction = k8sPagination().merge;
  });

  it('should return incoming data on first call when existing is undefined', () => {
    const incoming = {
      metadata: { continue: 'token1' },
      items: [{ id: 1 }, { id: 2 }],
    };

    const result = mergeFunction(undefined, incoming, { args: { options: { continue: '' } } });

    expect(result).toEqual(incoming);
  });

  it('should return incoming data on refetch call when continue is empty string', () => {
    const existing = {
      metadata: { continue: 'token1' },
      items: [{ id: 1 }, { id: 2 }],
    };
    const incoming = {
      metadata: { continue: 'token2' },
      items: [{ id: 3 }, { id: 4 }],
    };

    const result = mergeFunction(existing, incoming, { args: { options: { continue: '' } } });

    expect(result).toEqual(incoming);
  });

  it('should merge items when continue token matches existing continue token', () => {
    const existing = {
      metadata: { continue: 'token1' },
      items: [{ id: 1 }, { id: 2 }],
    };
    const incoming = {
      metadata: { continue: 'token2' },
      items: [{ id: 3 }, { id: 4 }],
    };

    const result = mergeFunction(existing, incoming, { args: { options: { continue: 'token1' } } });

    expect(result).toEqual({
      metadata: { continue: 'token2' },
      items: [{ id: 1 }, { id: 2 }, { id: 3 }, { id: 4 }],
    });
  });

  it('should return existing data when continue token does not match', () => {
    const existing = {
      metadata: { continue: 'token1' },
      items: [{ id: 1 }, { id: 2 }],
    };
    const incoming = {
      metadata: { continue: 'token2' },
      items: [{ id: 3 }, { id: 4 }],
    };

    const result = mergeFunction(existing, incoming, { args: { options: { continue: 'differentToken' } } });

    expect(result).toEqual(existing);
  });
});

describe('DashboardCustomCache', () => {
  const nodesPage = (names: string[], continueToken: string) => ({
    coreV1NodesList: {
      __typename: 'CoreV1NodeList' as const,
      metadata: { __typename: 'MetaV1ListMeta' as const, continue: continueToken, resourceVersion: 'v1' },
      items: names.map((name) => ({
        __typename: 'CoreV1Node' as const,
        id: name,
        metadata: {
          __typename: 'MetaV1ObjectMeta' as const,
          name,
          uid: name,
          creationTimestamp: '2024-01-01T00:00:00Z',
          deletionTimestamp: null,
          resourceVersion: 'v1',
          labels: null,
          annotations: null,
        },
      })),
    },
  });

  it('merges the pages of the nodes list', () => {
    const cache = new DashboardCustomCache();

    cache.writeQuery({
      query: CONSOLE_NODES_LIST_FETCH,
      variables: { kubeContext: '', continue: '' },
      data: nodesPage(['node-1', 'node-2'], 'token1'),
    });
    cache.writeQuery({
      query: CONSOLE_NODES_LIST_FETCH,
      variables: { kubeContext: '', continue: 'token1' },
      data: nodesPage(['node-3'], ''),
    });

    const result = cache.readQuery({
      query: CONSOLE_NODES_LIST_FETCH,
      variables: { kubeContext: '', continue: '' },
    });

    expect(result?.coreV1NodesList?.items.map((item) => item.metadata.name)).toEqual(['node-1', 'node-2', 'node-3']);
    expect(result?.coreV1NodesList?.metadata.continue).toBe('');
  });
});

describe('waitUntilVisible', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('resolves immediately when the document is already visible', async () => {
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');

    await expect(waitUntilVisible()).resolves.toBeUndefined();
  });

  it('resolves when visibility transitions to visible', async () => {
    const stateSpy = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');

    const pending = waitUntilVisible();

    stateSpy.mockReturnValue('visible');
    document.dispatchEvent(new Event('visibilitychange'));

    await expect(pending).resolves.toBeUndefined();
  });

  it('does not resolve while the document remains hidden', async () => {
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');

    let resolved = false;
    void waitUntilVisible().then(() => {
      resolved = true;
    });

    document.dispatchEvent(new Event('visibilitychange'));
    await Promise.resolve();

    expect(resolved).toBe(false);
  });
});

describe('waitUntilOnline', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('resolves immediately when navigator is online', async () => {
    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(true);

    await expect(waitUntilOnline()).resolves.toBeUndefined();
  });

  it('resolves when an online event fires', async () => {
    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false);

    const pending = waitUntilOnline();

    window.dispatchEvent(new Event('online'));

    await expect(pending).resolves.toBeUndefined();
  });
});
