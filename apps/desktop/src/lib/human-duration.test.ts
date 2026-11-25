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

import { humanDuration, humanMinutes } from './human-duration';

describe('humanDuration', () => {
  it('keeps finer units while they say something', () => {
    const s = 1000;
    const m = 60 * s;
    const h = 60 * m;
    expect([90 * s, 5 * m, 5 * m + 7 * s, 42 * m, 3 * h + 20 * m, 3 * h, 20 * h, 72 * h].map(humanDuration)).toEqual([
      '90s',
      '5m',
      '5m7s',
      '42m',
      '3h20m',
      '3h',
      '20h',
      '3d',
    ]);
  });

  it('reads a time before its start as none', () => {
    expect(humanDuration(-5000)).toBe('0s');
  });
});

describe('humanMinutes', () => {
  it('counts whole minutes, and never seconds', () => {
    const m = 60_000;
    const h = 60 * m;
    expect([-m, 59_000, 90_000, 5 * m + 7000, 3 * h + 20 * m + 59_000, 72 * h].map(humanMinutes)).toEqual([
      '<1m',
      '<1m',
      '1m',
      '5m',
      '3h20m',
      '3d',
    ]);
  });
});
