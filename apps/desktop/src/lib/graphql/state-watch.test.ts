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

import { stateWatchReducer } from './state-watch';

describe('stateWatchReducer', () => {
  const reduce = stateWatchReducer((frame: { value: number[] }) => frame.value);

  it('holds the first value, even an empty one', () => {
    expect(reduce(undefined, [{ value: [] }])).toEqual([]);
  });

  // Every frame is the whole value, so a batch reduces to its last frame.
  it('replaces what it holds with the last frame of the batch', () => {
    expect(reduce([1], [{ value: [1, 2] }, { value: [3] }])).toEqual([3]);
  });
});
