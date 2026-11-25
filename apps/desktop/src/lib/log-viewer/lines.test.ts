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

import { Lines, compareTimestamps, previousNanosecond } from './lines';
import type { LogLine } from './types';

function line(cursor: string, timestamp = '2026-10-15T00:00:00Z', container = 'app'): LogLine {
  return {
    timestamp,
    cursor,
    message: `m-${cursor}`,
    truncatedBytes: 0,
    stream: { namespace: 'ns', pod: 'p', container, previous: false },
    place: { node: '', region: '', zone: '', os: '', arch: '' },
    gap: null,
  };
}

const cursors = (lines: Lines) => Array.from({ length: lines.length }, (_, i) => lines.at(i).cursor);

describe('Lines.reset', () => {
  it('puts key 0 where it is told', () => {
    const lines = new Lines();
    const page = [line('a'), line('b'), line('c')];

    lines.reset(page, 0);
    expect(lines.keyAt(0)).toBe(0);

    lines.reset(page, 2);
    expect(lines.keyAt(2)).toBe(0);
    expect(lines.indexOfKey(0)).toBe(2);

    lines.reset(page, 1);
    expect(lines.keyAt(1)).toBe(0);

    lines.reset(page, 9);
    expect(lines.keyAt(2)).toBe(0);
    expect(cursors(lines)).toEqual(['a', 'b', 'c']);
    expect(lines.first()?.cursor).toBe('a');
    expect(lines.last()?.cursor).toBe('c');
  });

  it('starts over, forgetting what it held', () => {
    const lines = new Lines();
    lines.reset([line('a')], 0);
    lines.reset([line('b')], 0);
    expect(cursors(lines)).toEqual(['b']);
    expect(lines.append([line('a')])).toBe(1);
  });

  it('holds nothing after an empty reset', () => {
    const lines = new Lines();
    lines.reset([], 0);
    expect(lines.length).toBe(0);
    expect(lines.first()).toBeUndefined();
  });
});

describe('Lines.append and Lines.prepend', () => {
  it('drops a line held, by its cursor alone, and answers how many it added', () => {
    const lines = new Lines();
    lines.reset([line('b'), line('c')], 0);

    // Same cursor, different everything else: still the line held.
    expect(lines.append([{ ...line('c', '2030-01-01T00:00:00Z'), message: 'other' }, line('d')])).toBe(1);
    expect(lines.prepend([line('a'), line('b')])).toBe(1);
    expect(cursors(lines)).toEqual(['a', 'b', 'c', 'd']);
    expect(lines.at(2).message).toBe('m-c');
  });

  it('adds a repeat within one batch once', () => {
    const lines = new Lines();
    expect(lines.append([line('a'), line('a')])).toBe(1);
    expect(lines.prepend([line('z'), line('z')])).toBe(1);
    expect(cursors(lines)).toEqual(['z', 'a']);
  });

  it('keeps two lines of one container at one moment when their cursors differ', () => {
    const lines = new Lines();
    lines.append([line('a'), line('b')]);
    expect(lines.length).toBe(2);
  });

  it('keeps a follow’s lines in the order they arrived', () => {
    const lines = new Lines();
    lines.append([line('x', '2026-10-15T00:00:02Z', 'one'), line('y', '2026-10-15T00:00:01Z', 'two')]);
    expect(cursors(lines)).toEqual(['x', 'y']);
  });

  it('keeps the keys it holds across a prepend', () => {
    const lines = new Lines();
    lines.reset([line('b')], 0);
    lines.prepend([line('a')]);
    expect(lines.keyAt(lines.length - 1)).toBe(0);
    expect(lines.keyAt(0)).toBe(-1);
  });
});

describe('Lines.dropFirst and Lines.dropLast', () => {
  it('answer the lines dropped in their held order, and keep the kept keys', () => {
    const lines = new Lines();
    lines.reset([line('a'), line('b'), line('c'), line('d'), line('e')], 2);

    expect(lines.dropFirst(2).map((l) => l.cursor)).toEqual(['a', 'b']);
    expect(lines.dropLast(2).map((l) => l.cursor)).toEqual(['d', 'e']);
    expect(cursors(lines)).toEqual(['c']);
    expect(lines.keyAt(0)).toBe(0);
  });

  it('let go of the dropped cursors, and still drop a kept line as a repeat', () => {
    const lines = new Lines();
    lines.reset([line('a'), line('b'), line('c')], 0);
    lines.dropFirst(1);
    lines.dropLast(1);

    expect(lines.prepend([line('a'), line('b')])).toBe(1);
    expect(lines.append([line('b'), line('c')])).toBe(1);
    expect(cursors(lines)).toEqual(['a', 'b', 'c']);
  });

  it('drop what is held when asked for more', () => {
    const lines = new Lines();
    lines.reset([line('a'), line('b')], 0);
    expect(lines.dropFirst(5)).toHaveLength(2);
    expect(lines.length).toBe(0);
    expect(lines.dropLast(1)).toEqual([]);
  });
});

describe('compareTimestamps', () => {
  it('orders stamps that differ below the millisecond', () => {
    expect(compareTimestamps('2026-10-15T00:00:00.000000001Z', '2026-10-15T00:00:00.000000002Z')).toBeLessThan(0);
    expect(compareTimestamps('2026-10-15T00:00:00.0000009Z', '2026-10-15T00:00:00.0000001Z')).toBeGreaterThan(0);
  });

  it('orders stamps with different fraction lengths as instants', () => {
    expect(compareTimestamps('2026-10-15T00:00:00.5Z', '2026-10-15T00:00:00.500000000Z')).toBe(0);
    expect(compareTimestamps('2026-10-15T00:00:00.5Z', '2026-10-15T00:00:00.49Z')).toBeGreaterThan(0);
    expect(compareTimestamps('2026-10-15T00:00:00Z', '2026-10-15T00:00:00.1Z')).toBeLessThan(0);
    expect(compareTimestamps('2026-10-15T00:00:01Z', '2026-10-15T00:00:00.999999999Z')).toBeGreaterThan(0);
  });

  it('orders stamps with an offset as the instant they name', () => {
    expect(compareTimestamps('2026-10-15T02:00:00+02:00', '2026-10-15T00:00:00Z')).toBe(0);
    expect(compareTimestamps('2026-10-15T01:59:59.999999999+02:00', '2026-10-15T00:00:00Z')).toBeLessThan(0);
    expect(compareTimestamps('2026-10-14T23:00:00.000000001-01:00', '2026-10-15T00:00:00Z')).toBeGreaterThan(0);
  });
});

describe('previousNanosecond', () => {
  it('steps a stamp back one nanosecond, in UTC with nine digits', () => {
    expect(previousNanosecond('2026-10-15T00:00:00.000000002Z')).toBe('2026-10-15T00:00:00.000000001Z');
    expect(previousNanosecond('2026-10-15T00:00:00.5Z')).toBe('2026-10-15T00:00:00.499999999Z');
    expect(previousNanosecond('2026-10-15T02:00:00.25+02:00')).toBe('2026-10-15T00:00:00.249999999Z');
  });

  it('borrows a second when the fraction is zero', () => {
    expect(previousNanosecond('2026-10-15T00:00:00Z')).toBe('2026-10-14T23:59:59.999999999Z');
    expect(previousNanosecond('2026-10-15T00:00:01.000Z')).toBe('2026-10-15T00:00:00.999999999Z');
  });
});
