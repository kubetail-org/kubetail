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

import { DoubleTailedArray } from './double-tailed-array';

describe('DoubleTailedArray', () => {
  it('is empty to begin with', () => {
    const arr = new DoubleTailedArray<string>();
    expect(arr.length).toBe(0);
    expect(arr.first()).toBeUndefined();
    expect(arr.last()).toBeUndefined();
    expect(() => arr.at(0)).toThrow(RangeError);
  });

  it('appends in order, keyed from 0', () => {
    const arr = new DoubleTailedArray<string>();
    arr.append(['a', 'b']);
    arr.append(['c']);
    expect([0, 1, 2].map((i) => arr.at(i))).toEqual(['a', 'b', 'c']);
    expect([0, 1, 2].map((i) => arr.keyAt(i))).toEqual([0, 1, 2]);
    expect(arr.indexOfKey(2)).toBe(2);
    expect(arr.first()).toBe('a');
    expect(arr.last()).toBe('c');
    expect(() => arr.at(3)).toThrow(RangeError);
    expect(() => arr.at(-1)).toThrow(RangeError);
  });

  it('prepends in their own order, keeping the keys it holds', () => {
    const arr = new DoubleTailedArray<string>();
    arr.append(['c', 'd']);
    arr.prepend(['a', 'b']);
    expect([0, 1, 2, 3].map((i) => arr.at(i))).toEqual(['a', 'b', 'c', 'd']);
    expect([0, 1, 2, 3].map((i) => arr.keyAt(i))).toEqual([-2, -1, 0, 1]);
    expect(arr.indexOfKey(0)).toBe(2);
    expect(arr.indexOfKey(-2)).toBe(0);

    arr.prepend(['z']);
    expect(arr.first()).toBe('z');
    expect(arr.keyAt(0)).toBe(-3);
    expect(arr.at(arr.indexOfKey(0))).toBe('c');
  });

  // Head `a b`, tail `c d`: keys -2..1.
  const split = () => {
    const arr = new DoubleTailedArray<string>();
    arr.append(['c', 'd']);
    arr.prepend(['a', 'b']);
    return arr;
  };
  const items = (arr: DoubleTailedArray<string>) => Array.from({ length: arr.length }, (_, i) => arr.at(i));
  const keys = (arr: DoubleTailedArray<string>) => Array.from({ length: arr.length }, (_, i) => arr.keyAt(i));

  it('drops from the front within the head, keeping every kept key', () => {
    const arr = split();
    arr.dropFirst(1);
    expect(items(arr)).toEqual(['b', 'c', 'd']);
    expect(keys(arr)).toEqual([-1, 0, 1]);
  });

  it('drops from the back within the tail, moving no key', () => {
    const arr = split();
    arr.dropLast(1);
    expect(items(arr)).toEqual(['a', 'b', 'c']);
    expect(keys(arr)).toEqual([-2, -1, 0]);
  });

  it('drops across the boundary from either end', () => {
    const front = split();
    front.dropFirst(3);
    expect(items(front)).toEqual(['d']);
    expect(keys(front)).toEqual([1]);

    const back = split();
    back.dropLast(3);
    expect(items(back)).toEqual(['a']);
    expect(keys(back)).toEqual([-2]);
  });

  it('drops everything, and keys what comes next past what it dropped', () => {
    const front = split();
    front.dropFirst(4);
    expect(front.length).toBe(0);
    front.append(['e']);
    expect(keys(front)).toEqual([2]);

    const back = split();
    back.dropLast(9);
    expect(back.length).toBe(0);
    back.prepend(['z']);
    expect(keys(back)).toEqual([-3]);
  });

  it('takes a batch larger than a spread allows', () => {
    const big = Array.from({ length: 300_000 }, (_, i) => i);
    const arr = new DoubleTailedArray<number>();
    arr.append(big);
    arr.prepend(big);
    expect(arr.length).toBe(600_000);
    expect(arr.at(0)).toBe(0);
    expect(arr.at(299_999)).toBe(299_999);
    expect(arr.at(300_000)).toBe(0);
  });
});
