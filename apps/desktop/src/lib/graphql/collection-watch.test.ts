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

import { describe, expect, it } from 'vitest';

import { collectionWatchReducer } from './collection-watch';

type Item = { id: string; v: number };
type Frame = { type: string; item: Item | null };

const reduce = collectionWatchReducer((frame: Frame) => ({ type: frame.type, item: frame.item }));
const added = (id: string, v = 0): Frame => ({ type: 'Added', item: { id, v } });
const BOOKMARK: Frame = { type: 'Bookmark', item: null };

describe('collectionWatchReducer', () => {
  it('holds a partial snapshot until the Bookmark', () => {
    const state = reduce(undefined, [added('a')]);
    expect(state.snapshotComplete).toBe(false);
    expect([...state.items.keys()]).toEqual(['a']);
  });

  it('completes the snapshot on the Bookmark, even an empty one', () => {
    expect(reduce(undefined, [BOOKMARK])).toEqual({ items: new Map(), snapshotComplete: true });
  });

  it('upserts on Added and Modified and removes on Deleted', () => {
    const first = reduce(undefined, [added('a'), added('b'), BOOKMARK]);
    const next = reduce(first, [
      { type: 'Modified', item: { id: 'a', v: 1 } },
      { type: 'Deleted', item: { id: 'b', v: 0 } },
    ]);
    expect([...next.items.values()]).toEqual([{ id: 'a', v: 1 }]);
    expect(next.snapshotComplete).toBe(true);
  });

  // A nested non-null field that errors nulls its parent, so a null item also arrives on
  // ordinary changes: it is dropped, never read as the Bookmark.
  it('drops a change that carries no item', () => {
    const state = reduce(undefined, [{ type: 'Modified', item: null }]);
    expect(state).toEqual({ items: new Map(), snapshotComplete: false });
  });

  it('leaves the previous state untouched', () => {
    const first = reduce(undefined, [added('a')]);
    reduce(first, [{ type: 'Deleted', item: { id: 'a', v: 0 } }]);
    expect(first.items.has('a')).toBe(true);
  });

  it('keys by the key function it is given', () => {
    const byValue = collectionWatchReducer(
      (frame: Frame) => ({ type: frame.type, item: frame.item }),
      (item) => String(item.v),
    );
    expect([...byValue(undefined, [added('a', 7)]).items.keys()]).toEqual(['7']);
  });
});
