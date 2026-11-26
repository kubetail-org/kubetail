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

import { sampleFeedReducer, sampleFeedSlots } from './sample-feed';

describe('sampleFeedReducer', () => {
  type Point = { at: string; v: number };
  type Frame = { feed: { intervalSeconds: number; windowSeconds: number; samples: Point[] } };
  const frame = (...samples: Point[]): Frame => ({ feed: { intervalSeconds: 2, windowSeconds: 6, samples } });
  const reduce = sampleFeedReducer((data: Frame) => data.feed);
  const point = (v: number, at = `t${v}`): Point => ({ at, v });

  it('folds an empty first frame to an empty feed, not to nothing', () => {
    expect(reduce(undefined, [frame()])).toEqual({ intervalSeconds: 2, windowSeconds: 6, samples: [] });
  });

  it('appends each frame’s points, oldest first', () => {
    const first = reduce(undefined, [frame(point(1))]);
    expect(reduce(first, [frame(point(2)), frame(point(3))]).samples).toEqual([point(1), point(2), point(3)]);
  });

  // Six seconds at two a point: the client keeps what the sidecar's ring keeps.
  it('keeps the last window ÷ interval points', () => {
    const feed = reduce(undefined, [frame(point(1), point(2)), frame(point(3), point(4))]);
    expect(feed.samples).toEqual([point(2), point(3), point(4)]);
  });

  // A wall clock that steps back stamps the next point earlier than the last. The fold reads
  // no times, so it neither drops nor reorders it.
  it('appends a point whose time goes backwards', () => {
    const feed = reduce(undefined, [frame(point(1, '12:00:02')), frame(point(2, '12:00:00'))]);
    expect(feed.samples.map((p) => p.v)).toEqual([1, 2]);
  });
});

describe('sampleFeedSlots', () => {
  it('rounds the window down to whole intervals', () => {
    expect(sampleFeedSlots({ intervalSeconds: 2, windowSeconds: 120, samples: [] })).toBe(120 / 2);
    expect(sampleFeedSlots({ intervalSeconds: 2, windowSeconds: 7, samples: [] })).toBe(3);
  });

  // slice(-0) would keep every point, so a window shorter than its interval keeps one.
  it('holds at least one point', () => {
    expect(sampleFeedSlots({ intervalSeconds: 5, windowSeconds: 2, samples: [] })).toBe(1);
  });
});
