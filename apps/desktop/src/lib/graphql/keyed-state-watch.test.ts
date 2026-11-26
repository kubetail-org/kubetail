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

import { keyedStateWatchReducer } from './keyed-state-watch';

type Reading = { key: string; v: number };

describe('keyedStateWatchReducer', () => {
  const reduce = keyedStateWatchReducer(
    (frame: { reading: Reading }) => frame.reading,
    (reading) => reading.key,
  );

  it('keeps one value per key', () => {
    const state = reduce(undefined, [{ reading: { key: 'a', v: 1 } }, { reading: { key: 'b', v: 2 } }]);
    expect([...state.entries()]).toEqual([
      ['a', { key: 'a', v: 1 }],
      ['b', { key: 'b', v: 2 }],
    ]);
  });

  it('replaces only the key that changed, leaving the previous map untouched', () => {
    const first = reduce(undefined, [{ reading: { key: 'a', v: 1 } }, { reading: { key: 'b', v: 2 } }]);
    const next = reduce(first, [{ reading: { key: 'a', v: 3 } }]);
    expect(next.get('a')).toEqual({ key: 'a', v: 3 });
    expect(next.get('b')).toEqual({ key: 'b', v: 2 });
    expect(first.get('a')).toEqual({ key: 'a', v: 1 });
  });
});
