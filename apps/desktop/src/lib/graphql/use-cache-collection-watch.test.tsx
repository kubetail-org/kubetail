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

// The reducer is the unit under test: mock useFoldedSubscription with a stand-in that
// captures it and hands back whatever the test folded, so batches are driven directly.
const { useFoldedSubscriptionMock } = vi.hoisted(() => ({ useFoldedSubscriptionMock: vi.fn() }));
vi.mock('@/lib/graphql/use-folded-subscription', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/graphql/use-folded-subscription')>()),
  useFoldedSubscription: useFoldedSubscriptionMock,
}));

const { joinProvenance, useCacheCollectionWatch } = await import('./use-cache-collection-watch');

type Row = { uid: string; name: string };
type Frame = { watch: { type: string; cacheID: string; row: Row | null } };

const frame = (type: string, row: Row | null, cacheID = 'c1'): Frame => ({ watch: { type, cacheID, row } });

let acc: unknown;
let reduce: ((prev: unknown, frames: Frame[]) => unknown) | undefined;
const push = (...frames: Frame[]) => {
  acc = reduce!(acc, frames);
};

function renderWatch(cacheID = 'c1') {
  return renderHook(
    ({ cache }: { cache: string }) =>
      useCacheCollectionWatch<Frame, Row>(
        { query: {} as never, variables: { cacheID: cache } },
        {
          select: (d) => ({ type: d.watch.type, item: d.watch.row, provenance: joinProvenance(d.watch.cacheID) }),
          keyOf: (r) => r.uid,
          currentProvenance: joinProvenance(cache),
        },
      ),
    { initialProps: { cache: cacheID } },
  );
}

beforeEach(() => {
  acc = undefined;
  reduce = undefined;
  useFoldedSubscriptionMock.mockImplementation((_args: unknown, r: typeof reduce) => {
    reduce = r;
    return { data: acc, connected: true };
  });
});

describe('useCacheCollectionWatch', () => {
  it('folds a batch of changes into one keyed set', () => {
    const { result, rerender } = renderWatch();
    push(
      frame('Added', { uid: 'a', name: 'a1' }),
      frame('Added', { uid: 'b', name: 'b1' }),
      frame('Modified', { uid: 'a', name: 'a2' }),
    );
    rerender({ cache: 'c1' });
    expect(result.current.items).toEqual([
      { uid: 'a', name: 'a2' },
      { uid: 'b', name: 'b1' },
    ]);
  });

  it('removes on Deleted', () => {
    const { result, rerender } = renderWatch();
    push(frame('Added', { uid: 'a', name: 'a1' }), frame('Deleted', { uid: 'a', name: 'a1' }));
    rerender({ cache: 'c1' });
    expect(result.current.items).toEqual([]);
  });

  it('reads as connecting until the Bookmark closes the snapshot', () => {
    const { result, rerender } = renderWatch();
    push(frame('Added', { uid: 'a', name: 'a1' }));
    rerender({ cache: 'c1' });
    expect(result.current.phase).toBe('connecting');
    push(frame('Bookmark', null));
    rerender({ cache: 'c1' });
    expect(result.current.phase).toBe('live');
    expect(result.current.items).toHaveLength(1);
  });

  // A nested non-null field erroring nulls its parent, so a change with no item is an
  // ordinary frame, not the snapshot boundary.
  it('drops a change with no item without closing the snapshot', () => {
    const { result, rerender } = renderWatch();
    push(frame('Added', null));
    rerender({ cache: 'c1' });
    expect(result.current.phase).toBe('connecting');
    expect(result.current.items).toEqual([]);
  });

  it("drops a superseded cache's stragglers from a batch", () => {
    const { result, rerender } = renderWatch();
    push(frame('Added', { uid: 'a', name: 'a1' }), frame('Added', { uid: 'old', name: 'x' }, 'c0'));
    rerender({ cache: 'c1' });
    expect(result.current.items.map((r) => r.uid)).toEqual(['a']);
  });

  it('restarts the fold when the current cache changes', () => {
    const { result, rerender } = renderWatch();
    push(frame('Added', { uid: 'a', name: 'a1' }), frame('Bookmark', null));
    rerender({ cache: 'c1' });
    expect(result.current.items).toHaveLength(1);

    rerender({ cache: 'c2' }); // the retained c1 set is masked at once
    expect(result.current.items).toEqual([]);
    expect(result.current.phase).toBe('connecting');

    push(frame('Added', { uid: 'z', name: 'z1' }, 'c2'));
    rerender({ cache: 'c2' });
    expect(result.current.items.map((r) => r.uid)).toEqual(['z']); // not ['a', 'z']
  });
});
