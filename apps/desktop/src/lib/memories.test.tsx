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

// Same seam as the chats suite: stand in for useFoldedSubscription, capture the
// reducer it was handed, and fold frames through it directly.
const { useFoldedSubscriptionMock } = vi.hoisted(() => ({ useFoldedSubscriptionMock: vi.fn() }));
vi.mock('@/lib/graphql/use-folded-subscription', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/graphql/use-folded-subscription')>()),
  useFoldedSubscription: useFoldedSubscriptionMock,
}));

const { useMemories } = await import('./memories');

let acc: unknown;
let lastReducer: ((prev: unknown, frames: unknown[]) => unknown) | undefined;
let lastArgs: { pause?: boolean; variables?: { clusterID?: string } } | undefined;

function push(...frames: { type: string; memory: unknown }[]) {
  acc = lastReducer!(
    acc,
    frames.map((f) => ({ memoryCollectionWatch: f })),
  );
}

function render(clusterID: string | undefined, connected = true) {
  useFoldedSubscriptionMock.mockImplementation((args, reduce) => {
    lastArgs = args;
    lastReducer = reduce;
    return { data: acc, connected };
  });
  return renderHook(() => useMemories(clusterID));
}

const memory = (id: string, name: string, clusterID: string | null) => ({
  id,
  clusterID,
  name,
  body: 'body',
  writtenBy: 'Model',
  updatedAt: '2026-09-23T10:00:00Z',
});

beforeEach(() => {
  acc = undefined;
  lastReducer = undefined;
  vi.clearAllMocks();
});

describe('useMemories', () => {
  it('watches the cluster', () => {
    render('c1');
    expect(lastArgs?.variables).toEqual({ clusterID: 'c1' });
    expect(lastArgs?.pause).toBe(false);
  });

  it('opens no watch without a cluster', () => {
    const { result } = render(undefined);
    expect(lastArgs?.pause).toBe(true);
    expect(result.current.memories).toEqual([]);
  });

  it('stays connecting until the Bookmark, however many rows arrived', () => {
    render('c1');
    push({ type: 'Added', memory: memory('1', 'pages', 'c1') });
    const { result } = render('c1');
    expect(result.current.phase).toBe('connecting');
    expect(result.current.memories.map((m) => m.name)).toEqual(['pages']);

    push({ type: 'Bookmark', memory: null });
    expect(render('c1').result.current.phase).toBe('live');
  });

  it("lists the cluster's own first, then every cluster's, each by name", () => {
    render('c1');
    push(
      { type: 'Added', memory: memory('1', 'zeta', 'c1') },
      { type: 'Added', memory: memory('2', 'alpha', null) },
      { type: 'Added', memory: memory('3', 'beta', 'c1') },
      { type: 'Bookmark', memory: null },
    );
    expect(render('c1').result.current.memories.map((m) => m.name)).toEqual(['beta', 'zeta', 'alpha']);
  });

  it('folds changes by id and drops a frame with no memory', () => {
    render('c1');
    push(
      { type: 'Added', memory: memory('1', 'pages', 'c1') },
      { type: 'Bookmark', memory: null },
      { type: 'Modified', memory: { ...memory('1', 'renamed', 'c1'), body: 'rewritten' } },
      { type: 'Modified', memory: null },
    );
    expect(render('c1').result.current.memories.map((m) => [m.name, m.body])).toEqual([['renamed', 'rewritten']]);

    push({ type: 'Deleted', memory: memory('1', 'renamed', 'c1') });
    expect(render('c1').result.current.memories).toEqual([]);
  });
});
