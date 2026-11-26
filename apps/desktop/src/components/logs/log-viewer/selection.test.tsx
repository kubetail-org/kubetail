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

import { act, fireEvent, renderHook } from '@testing-library/react';
import type React from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { type AppError, onError } from '@/lib/error-bus';
import { lineTime } from '@/lib/log-viewer';
import type { LogLine } from '@/lib/log-viewer';

import type { Column } from './columns';
import {
  type SelectionRows,
  computeCellRange,
  computeSelection,
  formatCellsForCopy,
  formatRowsForCopy,
  getPlainAttribute,
  hasMultipleSelectedCells,
  useSelection,
} from './selection';

const makeLine = (overrides: Partial<LogLine> = {}): LogLine => ({
  timestamp: '2024-06-15T10:30:01.123Z',
  cursor: 'cursor-1',
  message: 'ERROR: something failed',
  truncatedBytes: 0,
  stream: { namespace: 'default', pod: 'my-pod-abc', container: 'my-container', previous: false },
  place: { node: 'node-1', region: 'us-east-1', zone: 'us-east-1a', os: 'linux', arch: 'amd64' },
  gap: null,
  ...overrides,
});

describe('getPlainAttribute', () => {
  const line = makeLine();

  it('returns the timestamp as the cell draws it', () => {
    expect(getPlainAttribute(line, 'Timestamp')).toBe(lineTime(line.timestamp));
  });

  it('returns empty string for Dot', () => {
    expect(getPlainAttribute(line, 'Dot')).toBe('');
  });

  it.each([
    ['Pod', 'my-pod-abc'],
    ['Container', 'my-container'],
    ['Region', 'us-east-1'],
    ['Zone', 'us-east-1a'],
    ['OS', 'linux'],
    ['Arch', 'amd64'],
    ['Node', 'node-1'],
    ['Message', 'ERROR: something failed'],
  ] as const)('returns %s', (col, want) => {
    expect(getPlainAttribute(line, col)).toBe(want);
  });

  it('returns the message without its colours', () => {
    expect(getPlainAttribute(makeLine({ message: '\x1b[31merror\x1b[0m: disk' }), 'Message')).toBe('error: disk');
  });

  it('returns the message raw, invisible characters included', () => {
    expect(getPlainAttribute(makeLine({ message: 'a​b' }), 'Message')).toBe('a​b');
  });
});

describe('formatRowsForCopy', () => {
  const lines = [
    makeLine({ message: 'line one', timestamp: '2024-06-15T10:30:01.000Z' }),
    makeLine({ message: 'line two', timestamp: '2024-06-15T10:30:02.000Z' }),
  ];

  it('formats single row with visible columns tab-separated', () => {
    const result = formatRowsForCopy([lines[0]], ['Timestamp', 'Pod', 'Message']);
    expect(result).toBe(`${lineTime(lines[0].timestamp)}\tmy-pod-abc\tline one`);
  });

  it('formats multiple rows separated by newlines', () => {
    expect(formatRowsForCopy(lines, ['Message'])).toBe('line one\nline two');
  });

  it("writes a line's hole in brackets above it, and a cell copy leaves it out", () => {
    const gap = { after: '2024-06-15T10:00:00.000Z', before: '2024-06-15T10:30:02.000Z' };
    const holed = [lines[0], { ...lines[1], gap }];
    expect(formatRowsForCopy(holed, ['Message'])).toBe(
      `line one\n[Lines may be missing from default/my-pod-abc/my-container between ${lineTime(gap.after)} and ${lineTime(gap.before)}: the cluster no longer holds them.]\nline two`,
    );
    expect(formatCellsForCopy(new Map([[1, new Set(['Message' as const])]]), ['Message'], () => holed[1])).toBe(
      'line two',
    );
  });

  it('skips Dot column', () => {
    expect(formatRowsForCopy([lines[0]], ['Dot', 'Message'])).toBe('line one');
  });

  it('returns empty string for empty lines array', () => {
    expect(formatRowsForCopy([], ['Message'])).toBe('');
  });
});

describe('formatCellsForCopy', () => {
  const lines = [
    makeLine({ message: 'line one', timestamp: '2024-06-15T10:30:01.000Z' }),
    makeLine({ message: 'line two', timestamp: '2024-06-15T10:30:02.000Z' }),
    makeLine({ message: 'line three', timestamp: '2024-06-15T10:30:03.000Z' }),
  ];

  const getLine = (key: number) => lines[key];

  it('formats a single selected cell', () => {
    const selectedCells = new Map([[0, new Set<Column>(['Message'])]]);
    expect(formatCellsForCopy(selectedCells, ['Timestamp', 'Message'], getLine)).toBe('line one');
  });

  it('formats multiple cells in the same row tab-separated', () => {
    const selectedCells = new Map([[0, new Set<Column>(['Timestamp', 'Message'])]]);
    expect(formatCellsForCopy(selectedCells, ['Timestamp', 'Message'], getLine)).toBe(
      `${lineTime(lines[0].timestamp)}\tline one`,
    );
  });

  it('formats cells from different rows newline-separated', () => {
    const selectedCells = new Map([
      [0, new Set<Column>(['Message'])],
      [1, new Set<Column>(['Message'])],
    ]);
    expect(formatCellsForCopy(selectedCells, ['Message'], getLine)).toBe('line one\nline two');
  });

  it('respects column order and skips unselected columns', () => {
    const selectedCells = new Map([[0, new Set<Column>(['Pod'])]]);
    expect(formatCellsForCopy(selectedCells, ['Timestamp', 'Pod', 'Message'], getLine)).toBe('my-pod-abc');
  });

  it('skips Dot column', () => {
    const selectedCells = new Map([[0, new Set<Column>(['Dot', 'Message'])]]);
    expect(formatCellsForCopy(selectedCells, ['Dot', 'Message'], getLine)).toBe('line one');
  });

  it('skips rows where getLine returns undefined', () => {
    const selectedCells = new Map([
      [0, new Set<Column>(['Message'])],
      [99, new Set<Column>(['Message'])],
    ]);
    expect(formatCellsForCopy(selectedCells, ['Message'], getLine)).toBe('line one');
  });

  it('sorts rows by key ascending', () => {
    const selectedCells = new Map([
      [2, new Set<Column>(['Message'])],
      [0, new Set<Column>(['Message'])],
    ]);
    expect(formatCellsForCopy(selectedCells, ['Message'], getLine)).toBe('line one\nline three');
  });

  it('returns empty string for empty selection', () => {
    expect(formatCellsForCopy(new Map(), ['Message'], getLine)).toBe('');
  });
});

describe('hasMultipleSelectedCells', () => {
  it('is false for no cell or one cell', () => {
    expect(hasMultipleSelectedCells(new Map())).toBe(false);
    expect(hasMultipleSelectedCells(new Map([[0, new Set<Column>(['Message'])]]))).toBe(false);
  });

  it('is true for two cells in one row or across rows', () => {
    expect(hasMultipleSelectedCells(new Map([[0, new Set<Column>(['Pod', 'Message'])]]))).toBe(true);
    expect(
      hasMultipleSelectedCells(
        new Map([
          [0, new Set<Column>(['Message'])],
          [1, new Set<Column>(['Message'])],
        ]),
      ),
    ).toBe(true);
  });
});

describe('computeSelection', () => {
  describe('plain click (no modifiers)', () => {
    it('selects single row, clearing previous selection', () => {
      const result = computeSelection({
        prev: new Set([1, 2, 3]),
        clickedKey: 5,
        shiftKey: false,
        metaOrCtrlKey: false,
        lastClickedKey: null,
      });
      expect(result).toEqual(new Set([5]));
    });
  });

  describe('meta/ctrl click', () => {
    it('adds row to existing selection', () => {
      const result = computeSelection({
        prev: new Set([1, 3]),
        clickedKey: 5,
        shiftKey: false,
        metaOrCtrlKey: true,
        lastClickedKey: null,
      });
      expect(result).toEqual(new Set([1, 3, 5]));
    });

    it('removes row from selection if already selected', () => {
      const result = computeSelection({
        prev: new Set([1, 3, 5]),
        clickedKey: 3,
        shiftKey: false,
        metaOrCtrlKey: true,
        lastClickedKey: null,
      });
      expect(result).toEqual(new Set([1, 5]));
    });
  });

  describe('shift click', () => {
    it('selects range from lastClickedKey to clickedKey', () => {
      const result = computeSelection({
        prev: new Set([2]),
        clickedKey: 6,
        shiftKey: true,
        metaOrCtrlKey: false,
        lastClickedKey: 2,
      });
      expect(result).toEqual(new Set([2, 3, 4, 5, 6]));
    });

    it('selects range in reverse direction', () => {
      const result = computeSelection({
        prev: new Set([6]),
        clickedKey: 2,
        shiftKey: true,
        metaOrCtrlKey: false,
        lastClickedKey: 6,
      });
      expect(result).toEqual(new Set([2, 3, 4, 5, 6]));
    });

    it('selects a range across the landed row', () => {
      const result = computeSelection({
        prev: new Set([-2]),
        clickedKey: 1,
        shiftKey: true,
        metaOrCtrlKey: false,
        lastClickedKey: -2,
      });
      expect(result).toEqual(new Set([-2, -1, 0, 1]));
    });

    it('falls back to single select when lastClickedKey is null', () => {
      const result = computeSelection({
        prev: new Set([1]),
        clickedKey: 5,
        shiftKey: true,
        metaOrCtrlKey: false,
        lastClickedKey: null,
      });
      expect(result).toEqual(new Set([5]));
    });

    it('preserves existing selection when extending with shift+click', () => {
      const result = computeSelection({
        prev: new Set([2, 3, 4]),
        clickedKey: 7,
        shiftKey: true,
        metaOrCtrlKey: false,
        lastClickedKey: 4,
      });
      expect(result).toEqual(new Set([2, 3, 4, 5, 6, 7]));
    });
  });
});

describe('computeCellRange', () => {
  const columns: Column[] = ['Timestamp', 'Dot', 'Pod', 'Container', 'Message'];

  it('selects all columns between anchor and target in the same row', () => {
    const result = computeCellRange({ rowKey: 0, col: 'Timestamp' }, { rowKey: 0, col: 'Container' }, columns);
    expect(result).toEqual(new Map([[0, new Set(['Timestamp', 'Pod', 'Container'])]]));
  });

  it('selects a single column across multiple rows', () => {
    const result = computeCellRange({ rowKey: 1, col: 'Pod' }, { rowKey: 3, col: 'Pod' }, columns);
    expect(result).toEqual(
      new Map([
        [1, new Set(['Pod'])],
        [2, new Set(['Pod'])],
        [3, new Set(['Pod'])],
      ]),
    );
  });

  it('selects a full rectangle across rows and columns', () => {
    const result = computeCellRange({ rowKey: 0, col: 'Pod' }, { rowKey: 2, col: 'Container' }, columns);
    expect(result).toEqual(
      new Map([
        [0, new Set(['Pod', 'Container'])],
        [1, new Set(['Pod', 'Container'])],
        [2, new Set(['Pod', 'Container'])],
      ]),
    );
  });

  it('shares one column set across the rows', () => {
    const result = computeCellRange({ rowKey: 0, col: 'Pod' }, { rowKey: 2, col: 'Container' }, columns);
    expect(result.get(0)).toBe(result.get(2));
  });

  it('produces the same result regardless of direction', () => {
    const forward = computeCellRange({ rowKey: 0, col: 'Pod' }, { rowKey: 2, col: 'Message' }, columns);
    const backward = computeCellRange({ rowKey: 2, col: 'Message' }, { rowKey: 0, col: 'Pod' }, columns);
    expect(forward).toEqual(backward);
  });

  it('returns a single cell when anchor equals target', () => {
    const result = computeCellRange({ rowKey: 1, col: 'Message' }, { rowKey: 1, col: 'Message' }, columns);
    expect(result).toEqual(new Map([[1, new Set(['Message'])]]));
  });

  it('skips Dot column in the range', () => {
    const result = computeCellRange({ rowKey: 0, col: 'Timestamp' }, { rowKey: 0, col: 'Pod' }, columns);
    expect(result).toEqual(new Map([[0, new Set(['Timestamp', 'Pod'])]]));
  });

  it('returns single target cell when anchor column is Dot', () => {
    const result = computeCellRange({ rowKey: 0, col: 'Dot' }, { rowKey: 1, col: 'Message' }, columns);
    expect(result).toEqual(new Map([[1, new Set(['Message'])]]));
  });

  it('returns single target cell when target column is Dot', () => {
    const result = computeCellRange({ rowKey: 0, col: 'Message' }, { rowKey: 1, col: 'Dot' }, columns);
    expect(result).toEqual(new Map([[1, new Set(['Dot'])]]));
  });
});

const clickEvent = (overrides: Partial<React.MouseEvent> = {}) =>
  ({
    shiftKey: false,
    metaKey: false,
    ctrlKey: false,
    stopPropagation: vi.fn(),
    currentTarget: document.createElement('div'),
    ...overrides,
  }) as unknown as React.MouseEvent;

const mouseUp = () =>
  act(() => {
    fireEvent.mouseUp(document);
  });

// What `Rows` hands the hook: the core's lookups over `lines`, the first `before` of them
// keyed below 0.
function rowsOver(lines: LogLine[], before = 0): SelectionRows {
  return {
    firstKey: -before,
    count: lines.length,
    lineOfKey: (key) => lines[key + before],
    indexOfKey: (key) => (key + before >= 0 && key + before < lines.length ? key + before : -1),
    keyAt: (index) => (index >= 0 && index < lines.length ? index - before : undefined),
    scrollIntoView: vi.fn(),
  };
}

const LINES = [0, 1, 2].map((i) => makeLine({ message: `log message ${i}`, timestamp: `2024-06-15T10:30:0${i}.000Z` }));

function renderUseSelection(columns: Column[] = ['Message']) {
  const scroller = document.createElement('div');
  document.body.appendChild(scroller);
  const onLines = vi.fn();
  const rows = rowsOver(LINES);
  const result = renderHook(({ rows: r }) => useSelection(r, scroller, columns, onLines), {
    initialProps: { rows },
  });
  return { ...result, scroller, onLines, rows };
}

describe('useSelection', () => {
  beforeEach(() => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
  });

  afterEach(() => {
    document.body.replaceChildren();
  });

  describe('handleRowMouseDown', () => {
    it('selects a single row on plain mousedown', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      mouseUp();
      expect(result.current.selectedKeys).toEqual(new Set([1]));
    });

    it('replaces selection on plain mousedown', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      mouseUp();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      mouseUp();
      expect(result.current.selectedKeys).toEqual(new Set([1]));
    });

    it('toggles individual rows with meta+mousedown', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      mouseUp();
      act(() => result.current.handleRowMouseDown(1, clickEvent({ metaKey: true })));
      expect(result.current.selectedKeys).toEqual(new Set([0, 1]));
    });

    it('deselects a row with meta+mousedown when already selected', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      mouseUp();
      act(() => result.current.handleRowMouseDown(0, clickEvent({ metaKey: true })));
      expect(result.current.selectedKeys).toEqual(new Set());
    });

    it('selects range with shift+mousedown', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      mouseUp();
      act(() => result.current.handleRowMouseDown(2, clickEvent({ shiftKey: true })));
      expect(result.current.selectedKeys).toEqual(new Set([0, 1, 2]));
    });

    it('clears cell selection when mousedown on the position cell', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      mouseUp();
      expect(result.current.selectedCells.size).toBe(0);
      expect(result.current.anchorCell).toBeNull();
    });
  });

  describe('selection boundaries', () => {
    it('single selected row is both top and bottom', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      mouseUp();
      expect(result.current.selectionTopKeys).toEqual(new Set([1]));
      expect(result.current.selectionBottomKeys).toEqual(new Set([1]));
    });

    it('contiguous block has top on first and bottom on last', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      mouseUp();
      act(() => result.current.handleRowMouseDown(2, clickEvent({ shiftKey: true })));
      expect(result.current.selectionTopKeys).toEqual(new Set([0]));
      expect(result.current.selectionBottomKeys).toEqual(new Set([2]));
    });

    it('non-contiguous selection creates separate boundary groups', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      mouseUp();
      act(() => result.current.handleRowMouseDown(2, clickEvent({ metaKey: true })));
      expect(result.current.selectionTopKeys).toEqual(new Set([0, 2]));
      expect(result.current.selectionBottomKeys).toEqual(new Set([0, 2]));
    });

    it('empty selection returns empty boundary sets', () => {
      const { result } = renderUseSelection();
      expect(result.current.selectionTopKeys.size).toBe(0);
      expect(result.current.selectionBottomKeys.size).toBe(0);
    });
  });

  describe('handleCellMouseDown', () => {
    const three: Column[] = ['Pod', 'Container', 'Message'];

    it('selects a cell on click', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(1, 'Message', clickEvent()));
      expect(result.current.selectedCells).toEqual(new Map([[1, new Set(['Message'])]]));
    });

    it('clears row selection when clicking a cell', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      mouseUp();
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      expect(result.current.selectedKeys.size).toBe(0);
      expect(result.current.selectedCells).toEqual(new Map([[0, new Set(['Message'])]]));
    });

    it('ignores Dot column clicks', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(0, 'Dot', clickEvent()));
      expect(result.current.selectedCells).toEqual(new Map());
    });

    it('replaces previously selected cell on plain click', () => {
      const { result } = renderUseSelection(three);
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      act(() => result.current.handleCellMouseDown(1, 'Pod', clickEvent()));
      expect(result.current.selectedCells).toEqual(new Map([[1, new Set(['Pod'])]]));
    });

    it('a plain click on a selected cell selects that cell alone', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      act(() => result.current.handleCellMouseDown(1, 'Message', clickEvent({ metaKey: true })));
      act(() => result.current.handleCellMouseDown(1, 'Message', clickEvent()));
      expect(result.current.selectedCells).toEqual(new Map([[1, new Set(['Message'])]]));
    });

    it('adds cell to selection with meta+click', () => {
      const { result } = renderUseSelection(three);
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      act(() => result.current.handleCellMouseDown(1, 'Pod', clickEvent({ metaKey: true })));
      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set(['Message'])],
          [1, new Set(['Pod'])],
        ]),
      );
    });

    it('adds another column in same row with meta+click', () => {
      const { result } = renderUseSelection(['Timestamp', 'Message']);
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      act(() => result.current.handleCellMouseDown(0, 'Timestamp', clickEvent({ metaKey: true })));
      expect(result.current.selectedCells).toEqual(new Map([[0, new Set(['Message', 'Timestamp'])]]));
    });

    it('cmd+click on the only selected cell clears selection and anchor', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent({ metaKey: true })));
      expect(result.current.selectedCells).toEqual(new Map());
      expect(result.current.anchorCell).toBeNull();
    });

    it('cmd+click on a non-anchor cell removes it and leaves the anchor unchanged', () => {
      const { result } = renderUseSelection(three);
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      act(() => result.current.handleCellMouseDown(1, 'Pod', clickEvent({ metaKey: true })));
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent({ metaKey: true })));
      expect(result.current.selectedCells).toEqual(new Map([[1, new Set(['Pod'])]]));
      expect(result.current.anchorCell).toEqual({ rowKey: 1, col: 'Pod' });
    });

    it('cmd+click on the anchor moves the anchor to the next selected cell on the right', () => {
      const { result } = renderUseSelection(three);
      act(() => result.current.handleCellMouseDown(5, 'Message', clickEvent()));
      act(() => result.current.handleCellMouseDown(5, 'Pod', clickEvent({ metaKey: true })));
      expect(result.current.anchorCell).toEqual({ rowKey: 5, col: 'Pod' });

      act(() => result.current.handleCellMouseDown(5, 'Pod', clickEvent({ metaKey: true })));
      expect(result.current.selectedCells).toEqual(new Map([[5, new Set(['Message'])]]));
      expect(result.current.anchorCell).toEqual({ rowKey: 5, col: 'Message' });
    });

    it('cmd+click on the anchor wraps to the next row when no selected cell remains to the right', () => {
      const { result } = renderUseSelection(three);
      act(() => result.current.handleCellMouseDown(6, 'Pod', clickEvent()));
      act(() => result.current.handleCellMouseDown(5, 'Pod', clickEvent({ metaKey: true })));
      act(() => result.current.handleCellMouseDown(5, 'Pod', clickEvent({ metaKey: true })));
      expect(result.current.selectedCells).toEqual(new Map([[6, new Set(['Pod'])]]));
      expect(result.current.anchorCell).toEqual({ rowKey: 6, col: 'Pod' });
    });

    it('cmd+click on the anchor wraps around to the first selected cell when at the end', () => {
      const { result } = renderUseSelection(three);
      act(() => result.current.handleCellMouseDown(4, 'Pod', clickEvent()));
      act(() => result.current.handleCellMouseDown(5, 'Message', clickEvent({ metaKey: true })));
      act(() => result.current.handleCellMouseDown(5, 'Message', clickEvent({ metaKey: true })));
      expect(result.current.selectedCells).toEqual(new Map([[4, new Set(['Pod'])]]));
      expect(result.current.anchorCell).toEqual({ rowKey: 4, col: 'Pod' });
    });

    it('cmd+click on the anchor moves it to the nearest row below, whatever order the rows were picked in', () => {
      const { result } = renderUseSelection(three);
      act(() => result.current.handleCellMouseDown(7, 'Pod', clickEvent()));
      act(() => result.current.handleCellMouseDown(6, 'Pod', clickEvent({ metaKey: true })));
      act(() => result.current.handleCellMouseDown(5, 'Pod', clickEvent({ metaKey: true })));
      act(() => result.current.handleCellMouseDown(5, 'Pod', clickEvent({ metaKey: true })));
      expect(result.current.anchorCell).toEqual({ rowKey: 6, col: 'Pod' });
    });

    it('adds cell with ctrl+click', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      act(() => result.current.handleCellMouseDown(1, 'Message', clickEvent({ ctrlKey: true })));
      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set(['Message'])],
          [1, new Set(['Message'])],
        ]),
      );
    });

    it('selects range with shift+click from anchor to target', () => {
      const { result } = renderUseSelection(three);
      act(() => result.current.handleCellMouseDown(0, 'Pod', clickEvent()));
      mouseUp();
      act(() => result.current.handleCellMouseDown(2, 'Container', clickEvent({ shiftKey: true })));
      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set(['Pod', 'Container'])],
          [1, new Set(['Pod', 'Container'])],
          [2, new Set(['Pod', 'Container'])],
        ]),
      );
    });

    it('shift+click with no prior anchor behaves like plain click', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(1, 'Message', clickEvent({ shiftKey: true })));
      expect(result.current.selectedCells).toEqual(new Map([[1, new Set(['Message'])]]));
    });

    it('shift+click clears row selection', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      mouseUp();
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      mouseUp();
      act(() => result.current.handleCellMouseDown(1, 'Message', clickEvent({ shiftKey: true })));
      expect(result.current.selectedKeys.size).toBe(0);
    });

    it('anchor stays on shift+click so subsequent shift+click extends from original', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      mouseUp();
      act(() => result.current.handleCellMouseDown(1, 'Message', clickEvent({ shiftKey: true })));
      act(() => result.current.handleCellMouseDown(2, 'Message', clickEvent({ shiftKey: true })));
      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set(['Message'])],
          [1, new Set(['Message'])],
          [2, new Set(['Message'])],
        ]),
      );
      expect(result.current.anchorCell).toEqual({ rowKey: 0, col: 'Message' });
    });

    it('shift+click after cmd+click replaces selection with rectangle from new anchor', () => {
      const { result } = renderUseSelection(three);
      act(() => result.current.handleCellMouseDown(0, 'Pod', clickEvent()));
      mouseUp();
      act(() => result.current.handleCellMouseDown(1, 'Container', clickEvent({ metaKey: true })));
      act(() => result.current.handleCellMouseDown(2, 'Message', clickEvent({ shiftKey: true })));
      expect(result.current.selectedCells).toEqual(
        new Map([
          [1, new Set(['Container', 'Message'])],
          [2, new Set(['Container', 'Message'])],
        ]),
      );
    });

    it('plain click after shift-range resets anchor', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      mouseUp();
      act(() => result.current.handleCellMouseDown(2, 'Message', clickEvent({ shiftKey: true })));
      act(() => result.current.handleCellMouseDown(5, 'Message', clickEvent()));
      mouseUp();
      act(() => result.current.handleCellMouseDown(7, 'Message', clickEvent({ shiftKey: true })));
      expect(result.current.selectedCells).toEqual(
        new Map([
          [5, new Set(['Message'])],
          [6, new Set(['Message'])],
          [7, new Set(['Message'])],
        ]),
      );
    });
  });

  describe('drag selection', () => {
    function makeRowEl(key: number) {
      const el = document.createElement('div');
      el.dataset.rowKey = String(key);
      return el;
    }

    let elementFromPoint: ReturnType<typeof vi.fn>;

    beforeEach(() => {
      vi.useFakeTimers();
      elementFromPoint = vi.fn().mockReturnValue(null);
      document.elementFromPoint = elementFromPoint as typeof document.elementFromPoint;
    });

    afterEach(() => {
      vi.useRealTimers();
      delete (document as Partial<Document>).elementFromPoint;
    });

    const moveTo = (el: Element | null, frame = true) => {
      elementFromPoint.mockReturnValue(el);
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 30 });
        if (frame) vi.advanceTimersByTime(16);
      });
    };

    it('selects range when dragging from one row to another', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      moveTo(makeRowEl(3));
      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3]));
      mouseUp();
    });

    it('updates range dynamically as mouse moves', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      moveTo(makeRowEl(4));
      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3, 4]));
      moveTo(makeRowEl(2));
      expect(result.current.selectedKeys).toEqual(new Set([1, 2]));
      mouseUp();
    });

    it('supports dragging upward (to lower keys)', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(2, clickEvent()));
      moveTo(makeRowEl(-1));
      expect(result.current.selectedKeys).toEqual(new Set([-1, 0, 1, 2]));
      mouseUp();
    });

    it('ends drag on mouseup — subsequent mousemove has no effect', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      moveTo(makeRowEl(2));
      mouseUp();
      moveTo(makeRowEl(4));
      expect(result.current.selectedKeys).toEqual(new Set([1, 2]));
    });

    it('does not start drag on shift+mousedown', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      mouseUp();
      act(() => result.current.handleRowMouseDown(3, clickEvent({ shiftKey: true })));
      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3]));
      moveTo(makeRowEl(5));
      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3]));
    });

    it('does not start drag on meta+mousedown', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      mouseUp();
      act(() => result.current.handleRowMouseDown(3, clickEvent({ metaKey: true })));
      moveTo(makeRowEl(5));
      expect(result.current.selectedKeys).toEqual(new Set([1, 3]));
    });

    it('clears cell selection when drag starts', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      expect(result.current.selectedCells.size).toBe(0);
      mouseUp();
    });

    it('sets lastClickedKey on mouseup so shift-click extends from drag endpoint', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      moveTo(makeRowEl(3), false);
      mouseUp();
      act(() => result.current.handleRowMouseDown(5, clickEvent({ shiftKey: true })));
      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3, 4, 5]));
    });

    it('ignores mousemove when elementFromPoint returns null', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      moveTo(null);
      expect(result.current.selectedKeys).toEqual(new Set([1]));
      mouseUp();
    });

    it('skips state update when mouse stays on same row', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      moveTo(makeRowEl(3));
      const prevKeys = result.current.selectedKeys;
      moveTo(makeRowEl(3));
      expect(result.current.selectedKeys).toBe(prevKeys);
      mouseUp();
    });

    it('throttles mousemove via requestAnimationFrame', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      moveTo(makeRowEl(3), false);
      expect(result.current.selectedKeys).toEqual(new Set([1]));
      act(() => {
        vi.advanceTimersByTime(16);
      });
      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3]));
      mouseUp();
    });

    it('cleans up document listeners on unmount during active drag', () => {
      const { result, unmount } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      unmount();
      elementFromPoint.mockClear();
      fireEvent.mouseMove(document, { clientX: 10, clientY: 50 });
      act(() => {
        vi.advanceTimersByTime(16);
      });
      expect(elementFromPoint).not.toHaveBeenCalled();
    });
  });

  describe('cell drag selection', () => {
    function makeCellEl(rowKey: number, col: string) {
      const rowEl = document.createElement('div');
      rowEl.dataset.rowKey = String(rowKey);
      const cellEl = document.createElement('div');
      cellEl.dataset.col = col;
      rowEl.appendChild(cellEl);
      return cellEl;
    }

    let elementFromPoint: ReturnType<typeof vi.fn>;

    beforeEach(() => {
      vi.useFakeTimers();
      elementFromPoint = vi.fn().mockReturnValue(null);
      document.elementFromPoint = elementFromPoint as typeof document.elementFromPoint;
    });

    afterEach(() => {
      vi.useRealTimers();
      delete (document as Partial<Document>).elementFromPoint;
    });

    const moveTo = (el: Element) => {
      elementFromPoint.mockReturnValue(el);
      act(() => {
        fireEvent.mouseMove(document, { clientX: 100, clientY: 40 });
        vi.advanceTimersByTime(16);
      });
    };

    it('drag across cells selects rectangular range', () => {
      const { result } = renderUseSelection(['Pod', 'Container', 'Message']);
      act(() => result.current.handleCellMouseDown(0, 'Pod', clickEvent()));
      moveTo(makeCellEl(2, 'Message'));
      const all = new Set(['Pod', 'Container', 'Message']);
      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, all],
          [1, all],
          [2, all],
        ]),
      );
      mouseUp();
    });

    it('drag within single column selects row range', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      moveTo(makeCellEl(2, 'Message'));
      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set(['Message'])],
          [1, new Set(['Message'])],
          [2, new Set(['Message'])],
        ]),
      );
      mouseUp();
    });

    it.each(['Dot', 'Pos'])('dragging over the %s cell does not change selection', (col) => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      moveTo(makeCellEl(2, 'Message'));
      const before = result.current.selectedCells;
      moveTo(makeCellEl(1, col));
      expect(result.current.selectedCells).toBe(before);
      mouseUp();
    });

    it('sets anchor cell on mousedown (drag start), not on mouseup', () => {
      const { result } = renderUseSelection(['Pod', 'Container', 'Message']);
      act(() => result.current.handleCellMouseDown(0, 'Pod', clickEvent()));
      expect(result.current.anchorCell).toEqual({ rowKey: 0, col: 'Pod' });
      moveTo(makeCellEl(2, 'Message'));
      mouseUp();
      expect(result.current.anchorCell).toEqual({ rowKey: 0, col: 'Pod' });
    });

    it('modifier mousedown does not start drag', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      mouseUp();
      act(() => result.current.handleCellMouseDown(2, 'Message', clickEvent({ shiftKey: true })));
      const before = result.current.selectedCells;
      moveTo(makeCellEl(4, 'Message'));
      expect(result.current.selectedCells).toBe(before);
    });
  });

  describe('resetSelection', () => {
    it('clears row and cell selection', () => {
      const { result } = renderUseSelection();
      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      mouseUp();
      act(() => result.current.resetSelection());
      expect(result.current.selectedKeys.size).toBe(0);

      act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
      act(() => result.current.resetSelection());
      expect(result.current.selectedCells.size).toBe(0);
      expect(result.current.anchorCell).toBeNull();
    });
  });
});

describe('keyboard shortcuts', () => {
  beforeEach(() => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
  });

  afterEach(() => {
    document.body.replaceChildren();
  });

  const key = (target: Element, init: KeyboardEventInit) => {
    const event = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init });
    act(() => {
      target.dispatchEvent(event);
    });
    return event;
  };

  const selectRows = (result: { current: ReturnType<typeof useSelection> }, ...keys: number[]) => {
    act(() => result.current.handleRowMouseDown(keys[0], clickEvent()));
    mouseUp();
    keys.slice(1).forEach((k) => act(() => result.current.handleRowMouseDown(k, clickEvent({ metaKey: true }))));
  };

  it('clears selection on Escape', () => {
    const { result, scroller } = renderUseSelection();
    selectRows(result, 0);
    key(scroller, { key: 'Escape' });
    expect(result.current.selectedKeys.size).toBe(0);
  });

  it('clears cell selection on Escape', () => {
    const { result, scroller } = renderUseSelection();
    act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
    key(scroller, { key: 'Escape' });
    expect(result.current.selectedCells.size).toBe(0);
  });

  it('does not clear selection on an Escape something else handled', () => {
    const { result, scroller } = renderUseSelection();
    act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
    scroller.addEventListener('keydown', (e) => e.preventDefault(), { capture: true, once: true });
    key(scroller, { key: 'Escape' });
    expect(result.current.selectedCells.size).toBe(1);
    key(scroller, { key: 'Escape' });
    expect(result.current.selectedCells.size).toBe(0);
  });

  it('copies selected rows to clipboard on Cmd+C', () => {
    const { result, scroller } = renderUseSelection();
    selectRows(result, 1, 0);
    const event = key(scroller, { key: 'c', metaKey: true });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('log message 0\nlog message 1');
    expect(event.defaultPrevented).toBe(true);
  });

  it('copies on Ctrl+C', () => {
    const { result, scroller } = renderUseSelection();
    selectRows(result, 0);
    key(scroller, { key: 'c', ctrlKey: true });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('log message 0');
  });

  it('does not prevent default Cmd+C when nothing is selected', () => {
    const { scroller } = renderUseSelection();
    const event = key(scroller, { key: 'c', metaKey: true });
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });

  it('excludes Dot column from Cmd+C copy', () => {
    const { result, scroller } = renderUseSelection(['Dot', 'Pod', 'Message']);
    selectRows(result, 0);
    key(scroller, { key: 'c', metaKey: true });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('my-pod-abc\tlog message 0');
  });

  it('copies cell text on Cmd+C when a cell is selected', () => {
    const { result, scroller } = renderUseSelection(['Pod', 'Message']);
    act(() => result.current.handleCellMouseDown(0, 'Pod', clickEvent()));
    key(scroller, { key: 'c', metaKey: true });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('my-pod-abc');
  });

  it('copies multiple cells in same row tab-separated on Cmd+C', () => {
    const { result, scroller } = renderUseSelection(['Pod', 'Message']);
    act(() => result.current.handleCellMouseDown(0, 'Pod', clickEvent()));
    act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent({ metaKey: true })));
    key(scroller, { key: 'c', metaKey: true });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('my-pod-abc\tlog message 0');
  });

  it('leaves Cmd+C to the browser while text is selected natively', () => {
    const { result, scroller } = renderUseSelection();
    selectRows(result, 0);
    const getSelection = vi.spyOn(window, 'getSelection').mockReturnValue({ isCollapsed: false } as Selection);
    key(scroller, { key: 'c', metaKey: true });
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled();
    getSelection.mockRestore();
  });

  it('reports a refused write to the error bus', async () => {
    const reported: AppError[] = [];
    const off = onError((e) => reported.push(e));
    vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error('denied'));
    const { result, scroller } = renderUseSelection();
    selectRows(result, 0);
    key(scroller, { key: 'c', metaKey: true });
    await act(async () => {});
    expect(reported).toEqual([expect.objectContaining({ source: 'render', message: 'clipboard: write: denied' })]);
    off();
  });

  it('copies the same lines after rows load above', () => {
    const { result, scroller, rerender } = renderUseSelection();
    selectRows(result, 1);
    // A page above: key 1 now sits at index 3.
    rerender({ rows: rowsOver([makeLine({ message: 'older a' }), makeLine({ message: 'older b' }), ...LINES], 2) });
    key(scroller, { key: 'c', metaKey: true });
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('log message 1');
  });

  it('ignores keys sent to another element', () => {
    const { result } = renderUseSelection();
    selectRows(result, 0);
    const other = document.createElement('div');
    document.body.appendChild(other);
    key(other, { key: 'c', metaKey: true });
    key(other, { key: 'Escape' });
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled();
    expect(result.current.selectedKeys.size).toBe(1);
  });

  it('moves selected cell right to the next selectable column', () => {
    const { result, scroller } = renderUseSelection(['Pod', 'Dot', 'Message']);
    act(() => result.current.handleCellMouseDown(0, 'Pod', clickEvent()));
    const event = key(scroller, { key: 'ArrowRight' });
    expect(result.current.selectedCells).toEqual(new Map([[0, new Set(['Message'])]]));
    expect(result.current.anchorCell).toEqual({ rowKey: 0, col: 'Message' });
    expect(event.defaultPrevented).toBe(true);
  });

  it('moves selected cell left to the previous selectable column', () => {
    const { result, scroller } = renderUseSelection(['Pod', 'Dot', 'Message']);
    act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
    key(scroller, { key: 'ArrowLeft' });
    expect(result.current.selectedCells).toEqual(new Map([[0, new Set(['Pod'])]]));
    expect(result.current.anchorCell).toEqual({ rowKey: 0, col: 'Pod' });
  });

  it('moves selected cell down to the same column on the next row', () => {
    const { result, scroller } = renderUseSelection();
    act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
    key(scroller, { key: 'ArrowDown' });
    expect(result.current.selectedCells).toEqual(new Map([[1, new Set(['Message'])]]));
    expect(result.current.anchorCell).toEqual({ rowKey: 1, col: 'Message' });
  });

  it('scrolls the moved cell into view', () => {
    const { result, scroller, rows } = renderUseSelection(['Pod', 'Message']);
    act(() => result.current.handleCellMouseDown(1, 'Pod', clickEvent()));
    key(scroller, { key: 'ArrowUp' });
    expect(rows.scrollIntoView).toHaveBeenLastCalledWith(0);
    key(scroller, { key: 'ArrowDown' });
    key(scroller, { key: 'ArrowDown' });
    expect(rows.scrollIntoView).toHaveBeenLastCalledWith(2);
    key(scroller, { key: 'ArrowRight' });
    expect(rows.scrollIntoView).toHaveBeenLastCalledWith(2);
  });

  it('keeps selected cell unchanged when arrow key has no target cell, and lets the key scroll', () => {
    const { result, scroller, rows } = renderUseSelection();
    act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
    const event = key(scroller, { key: 'ArrowUp' });
    expect(result.current.selectedCells).toEqual(new Map([[0, new Set(['Message'])]]));
    expect(event.defaultPrevented).toBe(false);
    expect(rows.scrollIntoView).not.toHaveBeenCalled();
  });

  it('lets an arrow scroll when no cell is selected', () => {
    const { result, scroller } = renderUseSelection();
    selectRows(result, 0);
    expect(key(scroller, { key: 'ArrowDown' }).defaultPrevented).toBe(false);
  });
});

describe('the selected lines', () => {
  it('reports a row selection in key order, and none once it clears', () => {
    const { result, onLines } = renderUseSelection();
    act(() => result.current.handleRowMouseDown(2, clickEvent()));
    mouseUp();
    act(() => result.current.handleRowMouseDown(0, clickEvent({ metaKey: true })));
    expect(onLines).toHaveBeenLastCalledWith([LINES[0], LINES[2]]);
    act(() => result.current.resetSelection());
    expect(onLines).toHaveBeenLastCalledWith([]);
  });

  it('reports the rows of a cell block', () => {
    const { result, onLines } = renderUseSelection();
    act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
    mouseUp();
    act(() => result.current.handleCellMouseDown(1, 'Message', clickEvent({ shiftKey: true })));
    expect(onLines).toHaveBeenLastCalledWith([LINES[0], LINES[1]]);
  });
});

describe('a drop', () => {
  it('takes every selected row and cell, the anchor and the last click off the keys it drops', () => {
    const { result, rerender } = renderUseSelection();
    act(() => result.current.handleRowMouseDown(0, clickEvent()));
    mouseUp();
    act(() => result.current.handleRowMouseDown(2, clickEvent({ shiftKey: true })));
    rerender({ rows: rowsOver(LINES.slice(1), -1) });
    expect(result.current.selectedKeys).toEqual(new Set([1, 2]));
    rerender({ rows: rowsOver(LINES.slice(1, 2), -1) });
    expect(result.current.selectedKeys).toEqual(new Set([1]));

    act(() => result.current.handleRowMouseDown(1, clickEvent({ shiftKey: true })));
    expect(result.current.selectedKeys).toEqual(new Set([1]));
  });

  it('selects nothing under a dropped key a read back hands to another line', () => {
    const { result, rerender } = renderUseSelection();
    act(() => result.current.handleCellMouseDown(0, 'Message', clickEvent()));
    mouseUp();
    rerender({ rows: rowsOver(LINES.slice(1), -1) });
    expect(result.current.selectedCells.size).toBe(0);
    expect(result.current.anchorCell).toBeNull();

    rerender({ rows: rowsOver([LINES[2], LINES[1], LINES[2]]) });
    expect(result.current.selectedCells.size).toBe(0);
    expect(result.current.selectedKeys.size).toBe(0);
  });
});

describe('a context click', () => {
  it.each([
    ['right-click (button=2) on a selected cell', 0, { button: 2 }],
    ['Ctrl+click (macOS right-click) on a selected cell', 0, { button: 0, ctrlKey: true }],
    ['right-click on a non-selected cell', 2, { button: 2 }],
    ['Ctrl+click (macOS right-click) on a non-selected cell', 2, { button: 0, ctrlKey: true }],
  ] as const)('%s preserves the selection', (_, rowKey, event) => {
    const { result } = renderUseSelection(['Timestamp', 'Message']);
    act(() => result.current.handleCellMouseDown(0, 'Timestamp', clickEvent()));
    mouseUp();
    act(() => result.current.handleCellMouseDown(1, 'Message', clickEvent({ shiftKey: true })));
    const selection = result.current.selectedCells;
    expect(selection.size).toBe(2);
    act(() => result.current.handleCellMouseDown(rowKey, 'Message', clickEvent(event)));
    expect(result.current.selectedCells).toBe(selection);
  });
});
