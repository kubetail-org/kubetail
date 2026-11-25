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

import { eventFeedReducer } from './event-feed';

type Run = { id: string; at: number };
type Frame = { type: string; run: Run | null };

const newestFirst = (a: Run, b: Run) => b.at - a.at;
const reduce = eventFeedReducer((frame: Frame) => ({ type: frame.type, event: frame.run }), {
  compare: newestFirst,
  cap: 2,
});
const added = (id: string, at: number): Frame => ({ type: 'Added', run: { id, at } });
const BOOKMARK: Frame = { type: 'Bookmark', run: null };

describe('eventFeedReducer', () => {
  it('holds the history it has before the Bookmark as incomplete', () => {
    expect(reduce(undefined, [added('a', 1)])).toEqual({ events: [{ id: 'a', at: 1 }], snapshotComplete: false });
  });

  it('completes the snapshot on the Bookmark, even an empty one', () => {
    expect(reduce(undefined, [BOOKMARK])).toEqual({ events: [], snapshotComplete: true });
  });

  // An event that changes is sent again under the same id.
  it('upserts by id and orders by the event, not by arrival', () => {
    const history = reduce(undefined, [added('a', 1), added('b', 2), added('a', 3)]);
    expect(history.events).toEqual([
      { id: 'a', at: 3 },
      { id: 'b', at: 2 },
    ]);
  });

  // The publisher prunes without saying so, so the subscriber caps what it keeps.
  it('keeps no more than the cap, dropping the last in order', () => {
    const history = reduce(undefined, [added('a', 1), added('b', 2), added('c', 3)]);
    expect(history.events.map((e) => e.id)).toEqual(['c', 'b']);
  });

  it('drops a change that carries no event', () => {
    expect(reduce(undefined, [{ type: 'Added', run: null }])).toEqual({ events: [], snapshotComplete: false });
  });
});
