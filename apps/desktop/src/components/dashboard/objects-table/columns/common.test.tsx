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

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import {
  ageColumn,
  conditionStatus,
  countColumn,
  kind,
  labelsText,
  listColumn,
  quantityColumn,
  quantityValue,
  readyColumn,
  selectorText,
  statusColumn,
  textColumn,
} from './common';
import type { KindColumn } from './common';
import { cellText, objectWith, pillTone } from './testutil';

type Body = {
  name?: string;
  tags?: (string | undefined)[];
  n?: number;
  ready?: { ready: number; total: number };
  at?: string;
  phase?: string;
};

// The column as the table gets it, reading a row's `rawJSON`.
const one = (column: KindColumn<Body>) => kind('example.com/v1', 'things', [column])[1][0];

const empty = objectWith(undefined);

describe('textColumn', () => {
  const column = one(textColumn('Name', 100, (b) => b.name, { mono: true }));

  it('draws the text, its whole on hover, and sorts by it', () => {
    const o = objectWith({ name: 'web' });
    const { container } = render(<>{column.cell(o)}</>);
    expect(container.firstElementChild).toHaveAttribute('title', 'web');
    expect(container.firstElementChild).toHaveClass('font-mono');
    expect(column.sortValue?.(o)).toBe('web');
  });

  it('dashes and does not sort a missing or empty text', () => {
    expect(cellText(column, empty)).toBe('—');
    expect(column.sortValue?.(objectWith({ name: '' }))).toBeUndefined();
  });

  it('shows unless hidden', () => {
    expect(column.hiddenByDefault).toBeUndefined();
    expect(one(textColumn('Name', 100, (b) => b.name, { hidden: true })).hiddenByDefault).toBe(true);
  });
});

describe('listColumn', () => {
  it('joins what is set', () => {
    const column = one(listColumn('Tags', 100, (b) => b.tags));
    expect(cellText(column, objectWith({ tags: ['a', undefined, 'b'] }))).toBe('a, b');
    expect(cellText(column, empty)).toBe('—');
  });
});

describe('countColumn', () => {
  const column = one(countColumn('N', 64, (b) => b.n, { hidden: true }));

  it('draws and sorts the count, largest first', () => {
    expect(cellText(column, objectWith({ n: 0 }))).toBe('0');
    expect(column.sortValue?.(objectWith({ n: 3 }))).toBe(3);
    expect(column.descFirst).toBe(true);
    expect(column.hiddenByDefault).toBe(true);
  });

  it('dashes a missing count', () => {
    expect(cellText(column, empty)).toBe('—');
    expect(column.sortValue?.(empty)).toBeUndefined();
  });
});

describe('readyColumn', () => {
  const column = one(readyColumn('Ready', (b) => b.ready, { hidden: true }));

  it('draws ready of total and sorts by the share, a total of zero as whole', () => {
    const o = objectWith({ ready: { ready: 1, total: 4 } });
    expect(cellText(column, o)).toBe('1/4');
    expect(column.sortValue?.(o)).toBe(0.25);
    expect(column.sortValue?.(objectWith({ ready: { ready: 0, total: 0 } }))).toBe(1);
    expect(column.hiddenByDefault).toBe(true);
  });

  it('dashes a missing count', () => {
    expect(cellText(column, empty)).toBe('—');
    expect(column.sortValue?.(empty)).toBeUndefined();
  });
});

describe('ageColumn', () => {
  const column = one(ageColumn('Since', 72, (b) => b.at, { hidden: true }));

  it('draws the time since and sorts youngest first', () => {
    const o = objectWith({ at: new Date(Date.now() - 5 * 60_000).toISOString() });
    expect(cellText(column, o)).toBe('5m');
    expect(column.sortValue?.(objectWith({ at: '1970-01-01T00:00:01Z' }))).toBe(-1000);
    expect(column.hiddenByDefault).toBe(true);
  });

  it('dashes a missing or unparsable time', () => {
    expect(cellText(column, empty)).toBe('—');
    expect(cellText(column, objectWith({ at: 'soon' }))).toBe('—');
    expect(column.sortValue?.(empty)).toBeUndefined();
  });
});

describe('statusColumn', () => {
  const read = (b: Body) =>
    b.phase ? { label: b.phase, tone: b.phase === 'Up' ? ('ok' as const) : ('failing' as const) } : undefined;
  const column = one(statusColumn('Status', 96, read));
  const tone = column.tone!;

  it('draws a pill in the status’s tone and sorts by its word', () => {
    const o = objectWith({ phase: 'Up' });
    expect(cellText(column, o)).toBe('Up');
    expect(pillTone(column, o)).toBe('ok');
    expect(column.sortValue?.(o)).toBe('Up');
    expect(tone(objectWith({ phase: 'Down' }))).toBe('failing');
  });

  it('dashes no status, which the tabs count as pending', () => {
    expect(cellText(column, empty)).toBe('—');
    expect(column.sortValue?.(empty)).toBeUndefined();
    expect(tone(empty)).toBe('pending');
  });
});

describe('conditionStatus', () => {
  it('reads a condition by type', () => {
    const conditions = [{ type: 'Ready', status: 'False' }];
    expect(conditionStatus(conditions, 'Ready')).toBe('False');
    expect(conditionStatus(conditions, 'Available')).toBeUndefined();
    expect(conditionStatus(undefined, 'Ready')).toBeUndefined();
  });
});

describe('selectorText', () => {
  it('prints labels and each operator as kubectl does', () => {
    expect(
      selectorText({
        matchLabels: { app: 'web' },
        matchExpressions: [
          { key: 'tier', operator: 'In', values: ['a', 'b'] },
          { key: 'env', operator: 'NotIn', values: ['dev'] },
          { key: 'canary', operator: 'Exists' },
          { key: 'legacy', operator: 'DoesNotExist' },
          { key: 'odd', operator: 'Weird' },
          { key: 'bare', operator: 'In' },
        ],
      }),
    ).toBe('app=web,tier in (a,b),env notin (dev),canary,!legacy,bare in ()');
  });

  it('reads an empty selector as nothing', () => {
    expect(selectorText(undefined)).toBeUndefined();
    expect(selectorText({})).toBeUndefined();
  });
});

describe('quantityValue', () => {
  it('reads milli, binary and decimal suffixes', () => {
    expect(['1Gi', '1.5Ki', '2G', '512', '250m'].map(quantityValue)).toEqual([2 ** 30, 1536, 2e9, 512, 0.25]);
  });

  it('reads an unknown suffix, an exponent or nothing as unreadable', () => {
    expect(['3constructor', '1e3', '', undefined].map(quantityValue)).toEqual([
      undefined,
      undefined,
      undefined,
      undefined,
    ]);
  });
});

describe('labelsText', () => {
  it('prints a label map as kubectl does, nothing for an empty one', () => {
    expect(labelsText({ app: 'web', tier: 'db' })).toBe('app=web,tier=db');
    expect(labelsText({})).toBeUndefined();
    expect(labelsText(undefined)).toBeUndefined();
  });
});

describe('quantityColumn', () => {
  const column = one(quantityColumn('Size', 88, (b) => b.name, { hidden: true }));

  it('draws the quantity as written and sorts by its size, largest first', () => {
    const o = objectWith({ name: '500m' });
    expect(cellText(column, o)).toBe('500m');
    expect(column.sortValue?.(o)).toBe(0.5);
    expect(column.descFirst).toBe(true);
    expect(column.hiddenByDefault).toBe(true);
    expect(one(quantityColumn('Size', 88, (b) => b.name)).hiddenByDefault).toBeUndefined();
  });
});
