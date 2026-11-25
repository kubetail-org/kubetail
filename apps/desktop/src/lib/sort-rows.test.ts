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

import { sortRows } from './sort-rows';

const rows = [
  { id: 'a', n: 2 },
  { id: 'b', n: undefined },
  { id: 'c', n: 5 },
  { id: 'd', n: 2 },
];
const ids = (rs: { id: string }[]) => rs.map((r) => r.id);

describe('sortRows', () => {
  it('orders by the value, ties in the order they came', () => {
    expect(ids(sortRows(rows, (r) => r.n, false))).toEqual(['a', 'd', 'c', 'b']);
  });

  it('reverses the order but not the ties, and keeps a missing value last', () => {
    expect(ids(sortRows(rows, (r) => r.n, true))).toEqual(['c', 'a', 'd', 'b']);
  });

  it('orders text', () => {
    expect(ids(sortRows(rows, (r) => r.id, true))).toEqual(['d', 'c', 'b', 'a']);
  });

  it('leaves its input alone', () => {
    sortRows(rows, (r) => r.n, true);
    expect(ids(rows)).toEqual(['a', 'b', 'c', 'd']);
  });
});
