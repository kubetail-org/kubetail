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

import { act, renderHook, fireEvent } from '@testing-library/react';
import { createStore, Provider } from 'jotai';
import { createRef } from 'react';

import type { LogRecord, LogViewerVirtualizer } from '@/components/widgets/log-viewer';
import { PreferencesProvider } from '@/lib/preferences';

import {
  getPlainAttribute,
  formatRowsForCopy,
  formatCellsForCopy,
  computeSelection,
  computeCellRange,
  useSelection,
  useSelectionState,
  useRowDrag,
  useCellDrag,
  useSelectionKeyboard,
} from './selection';
import { ViewerColumn } from './shared';
import {
  isCursorTextAtom,
  isTextSelectModeAtom,
  lastClickedCellAtom,
  lastClickedKeyAtom,
  selectedCellsAtom,
  selectedKeysAtom,
  visibleColsAtom,
} from './state';

const makeRecord = (overrides: Partial<LogRecord> = {}): LogRecord => ({
  timestamp: '2024-06-15T10:30:01.123Z',
  message: 'ERROR: something failed',
  cursor: 'cursor-1',
  source: {
    metadata: { region: 'us-east-1', zone: 'us-east-1a', os: 'linux', arch: 'amd64', node: 'node-1' },
    namespace: 'default',
    podName: 'my-pod-abc',
    containerName: 'my-container',
  },
  ...overrides,
});

describe('getPlainAttribute', () => {
  const record = makeRecord();

  it('returns formatted timestamp in ISO 8601 / UTC by default', () => {
    const result = getPlainAttribute(record, ViewerColumn.Timestamp, 'UTC');
    expect(result).toBe('2024-06-15T10:30:01.123+00:00');
  });

  it('returns formatted timestamp in the given timezone', () => {
    const result = getPlainAttribute(record, ViewerColumn.Timestamp, 'America/New_York');
    // 10:30 UTC = 06:30 EDT (June is DST)
    expect(result).toBe('2024-06-15T06:30:01.123-04:00');
  });

  it('returns formatted timestamp in the given format', () => {
    const result = getPlainAttribute(record, ViewerColumn.Timestamp, 'UTC', 'rfc1123');
    expect(result).toBe('Sat, 15 Jun 2024 10:30:01 +0000');
  });

  it('returns empty string for ColorDot', () => {
    const result = getPlainAttribute(record, ViewerColumn.ColorDot);
    expect(result).toBe('');
  });

  it('returns pod name', () => {
    expect(getPlainAttribute(record, ViewerColumn.Pod)).toBe('my-pod-abc');
  });

  it('returns container name', () => {
    expect(getPlainAttribute(record, ViewerColumn.Container)).toBe('my-container');
  });

  it('returns region', () => {
    expect(getPlainAttribute(record, ViewerColumn.Region)).toBe('us-east-1');
  });

  it('returns zone', () => {
    expect(getPlainAttribute(record, ViewerColumn.Zone)).toBe('us-east-1a');
  });

  it('returns os', () => {
    expect(getPlainAttribute(record, ViewerColumn.OS)).toBe('linux');
  });

  it('returns arch', () => {
    expect(getPlainAttribute(record, ViewerColumn.Arch)).toBe('amd64');
  });

  it('returns node', () => {
    expect(getPlainAttribute(record, ViewerColumn.Node)).toBe('node-1');
  });

  it('returns ANSI-stripped message', () => {
    const ansiRecord = makeRecord({ message: '\x1b[31mERROR\x1b[0m: failed' });
    expect(getPlainAttribute(ansiRecord, ViewerColumn.Message)).toBe('ERROR: failed');
  });

  it('returns plain message when no ANSI codes', () => {
    expect(getPlainAttribute(record, ViewerColumn.Message)).toBe('ERROR: something failed');
  });
});

describe('formatRowsForCopy', () => {
  const records = [
    makeRecord({ message: 'line one', timestamp: '2024-06-15T10:30:01.000Z' }),
    makeRecord({ message: 'line two', timestamp: '2024-06-15T10:30:02.000Z' }),
  ];

  it('formats single row with visible columns tab-separated', () => {
    const visibleCols = new Set([ViewerColumn.Timestamp, ViewerColumn.Pod, ViewerColumn.Message]);
    const result = formatRowsForCopy([records[0]], visibleCols);
    expect(result).toBe('2024-06-15T10:30:01.000+00:00\tmy-pod-abc\tline one');
  });

  it('formats multiple rows separated by newlines', () => {
    const visibleCols = new Set([ViewerColumn.Message]);
    const result = formatRowsForCopy(records, visibleCols);
    expect(result).toBe('line one\nline two');
  });

  it('skips ColorDot column', () => {
    const visibleCols = new Set([ViewerColumn.ColorDot, ViewerColumn.Message]);
    const result = formatRowsForCopy([records[0]], visibleCols);
    expect(result).toBe('line one');
  });

  it('strips ANSI codes from messages', () => {
    const ansiRecords = [makeRecord({ message: '\x1b[32mOK\x1b[0m' })];
    const visibleCols = new Set([ViewerColumn.Message]);
    const result = formatRowsForCopy(ansiRecords, visibleCols);
    expect(result).toBe('OK');
  });

  it('returns empty string for empty records array', () => {
    const visibleCols = new Set([ViewerColumn.Message]);
    expect(formatRowsForCopy([], visibleCols)).toBe('');
  });
});

describe('formatCellsForCopy', () => {
  const records = [
    makeRecord({ message: 'line one', timestamp: '2024-06-15T10:30:01.000Z' }),
    makeRecord({ message: 'line two', timestamp: '2024-06-15T10:30:02.000Z' }),
    makeRecord({ message: 'line three', timestamp: '2024-06-15T10:30:03.000Z' }),
  ];

  const getRecord = (key: number) => records[key];

  it('formats a single selected cell', () => {
    const selectedCells = new Map([[0, new Set([ViewerColumn.Message])]]);
    const visibleCols = new Set([ViewerColumn.Timestamp, ViewerColumn.Message]);
    const result = formatCellsForCopy(selectedCells, visibleCols, getRecord);
    expect(result).toBe('line one');
  });

  it('formats multiple cells in the same row tab-separated', () => {
    const selectedCells = new Map([[0, new Set([ViewerColumn.Timestamp, ViewerColumn.Message])]]);
    const visibleCols = new Set([ViewerColumn.Timestamp, ViewerColumn.Message]);
    const result = formatCellsForCopy(selectedCells, visibleCols, getRecord);
    expect(result).toBe('2024-06-15T10:30:01.000+00:00\tline one');
  });

  it('formats cells from different rows newline-separated', () => {
    const selectedCells = new Map([
      [0, new Set([ViewerColumn.Message])],
      [1, new Set([ViewerColumn.Message])],
    ]);
    const visibleCols = new Set([ViewerColumn.Message]);
    const result = formatCellsForCopy(selectedCells, visibleCols, getRecord);
    expect(result).toBe('line one\nline two');
  });

  it('respects visibleCols order and skips unselected columns', () => {
    const selectedCells = new Map([[0, new Set([ViewerColumn.Pod])]]);
    const visibleCols = new Set([ViewerColumn.Timestamp, ViewerColumn.Pod, ViewerColumn.Message]);
    const result = formatCellsForCopy(selectedCells, visibleCols, getRecord);
    expect(result).toBe('my-pod-abc');
  });

  it('skips ColorDot column', () => {
    const selectedCells = new Map([[0, new Set([ViewerColumn.ColorDot, ViewerColumn.Message])]]);
    const visibleCols = new Set([ViewerColumn.ColorDot, ViewerColumn.Message]);
    const result = formatCellsForCopy(selectedCells, visibleCols, getRecord);
    expect(result).toBe('line one');
  });

  it('skips rows where getRecord returns undefined', () => {
    const selectedCells = new Map([
      [0, new Set([ViewerColumn.Message])],
      [99, new Set([ViewerColumn.Message])],
    ]);
    const visibleCols = new Set([ViewerColumn.Message]);
    const result = formatCellsForCopy(selectedCells, visibleCols, getRecord);
    expect(result).toBe('line one');
  });

  it('sorts rows by key ascending', () => {
    const selectedCells = new Map([
      [2, new Set([ViewerColumn.Message])],
      [0, new Set([ViewerColumn.Message])],
    ]);
    const visibleCols = new Set([ViewerColumn.Message]);
    const result = formatCellsForCopy(selectedCells, visibleCols, getRecord);
    expect(result).toBe('line one\nline three');
  });

  it('returns empty string for empty selection', () => {
    const selectedCells = new Map<number, Set<ViewerColumn>>();
    const visibleCols = new Set([ViewerColumn.Message]);
    const result = formatCellsForCopy(selectedCells, visibleCols, getRecord);
    expect(result).toBe('');
  });
});

describe('computeSelection', () => {
  describe('plain click (no modifiers)', () => {
    it('selects single row, clearing previous selection', () => {
      const prev = new Set([1, 2, 3]);
      const result = computeSelection({
        prev,
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
      const prev = new Set([1, 3]);
      const result = computeSelection({
        prev,
        clickedKey: 5,
        shiftKey: false,
        metaOrCtrlKey: true,
        lastClickedKey: null,
      });
      expect(result).toEqual(new Set([1, 3, 5]));
    });

    it('removes row from selection if already selected', () => {
      const prev = new Set([1, 3, 5]);
      const result = computeSelection({
        prev,
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
      const prev = new Set([2]);
      const result = computeSelection({
        prev,
        clickedKey: 6,
        shiftKey: true,
        metaOrCtrlKey: false,
        lastClickedKey: 2,
      });
      expect(result).toEqual(new Set([2, 3, 4, 5, 6]));
    });

    it('selects range in reverse direction', () => {
      const prev = new Set([6]);
      const result = computeSelection({
        prev,
        clickedKey: 2,
        shiftKey: true,
        metaOrCtrlKey: false,
        lastClickedKey: 6,
      });
      expect(result).toEqual(new Set([2, 3, 4, 5, 6]));
    });

    it('falls back to single select when lastClickedKey is null', () => {
      const prev = new Set([1]);
      const result = computeSelection({
        prev,
        clickedKey: 5,
        shiftKey: true,
        metaOrCtrlKey: false,
        lastClickedKey: null,
      });
      expect(result).toEqual(new Set([5]));
    });

    it('preserves existing selection when extending with shift+click', () => {
      const prev = new Set([2, 3, 4]);
      const result = computeSelection({
        prev,
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
  const visibleCols = new Set([
    ViewerColumn.Timestamp,
    ViewerColumn.ColorDot,
    ViewerColumn.Pod,
    ViewerColumn.Container,
    ViewerColumn.Message,
  ]);

  it('selects all columns between anchor and target in the same row', () => {
    const result = computeCellRange(
      { rowKey: 0, col: ViewerColumn.Timestamp },
      { rowKey: 0, col: ViewerColumn.Container },
      visibleCols,
    );
    expect(result).toEqual(new Map([[0, new Set([ViewerColumn.Timestamp, ViewerColumn.Pod, ViewerColumn.Container])]]));
  });

  it('selects a single column across multiple rows', () => {
    const result = computeCellRange(
      { rowKey: 1, col: ViewerColumn.Pod },
      { rowKey: 3, col: ViewerColumn.Pod },
      visibleCols,
    );
    expect(result).toEqual(
      new Map([
        [1, new Set([ViewerColumn.Pod])],
        [2, new Set([ViewerColumn.Pod])],
        [3, new Set([ViewerColumn.Pod])],
      ]),
    );
  });

  it('selects a full rectangle across rows and columns', () => {
    const result = computeCellRange(
      { rowKey: 0, col: ViewerColumn.Pod },
      { rowKey: 2, col: ViewerColumn.Container },
      visibleCols,
    );
    expect(result).toEqual(
      new Map([
        [0, new Set([ViewerColumn.Pod, ViewerColumn.Container])],
        [1, new Set([ViewerColumn.Pod, ViewerColumn.Container])],
        [2, new Set([ViewerColumn.Pod, ViewerColumn.Container])],
      ]),
    );
  });

  it('produces the same result regardless of direction', () => {
    const forward = computeCellRange(
      { rowKey: 0, col: ViewerColumn.Pod },
      { rowKey: 2, col: ViewerColumn.Message },
      visibleCols,
    );
    const backward = computeCellRange(
      { rowKey: 2, col: ViewerColumn.Message },
      { rowKey: 0, col: ViewerColumn.Pod },
      visibleCols,
    );
    expect(forward).toEqual(backward);
  });

  it('returns a single cell when anchor equals target', () => {
    const result = computeCellRange(
      { rowKey: 1, col: ViewerColumn.Message },
      { rowKey: 1, col: ViewerColumn.Message },
      visibleCols,
    );
    expect(result).toEqual(new Map([[1, new Set([ViewerColumn.Message])]]));
  });

  it('skips ColorDot column in the range', () => {
    // Timestamp -> Pod spans across ColorDot, which should be excluded
    const result = computeCellRange(
      { rowKey: 0, col: ViewerColumn.Timestamp },
      { rowKey: 0, col: ViewerColumn.Pod },
      visibleCols,
    );
    expect(result).toEqual(new Map([[0, new Set([ViewerColumn.Timestamp, ViewerColumn.Pod])]]));
  });

  it('returns single target cell when anchor column is ColorDot', () => {
    const result = computeCellRange(
      { rowKey: 0, col: ViewerColumn.ColorDot },
      { rowKey: 1, col: ViewerColumn.Message },
      visibleCols,
    );
    expect(result).toEqual(new Map([[1, new Set([ViewerColumn.Message])]]));
  });

  it('returns single target cell when target column is ColorDot', () => {
    const result = computeCellRange(
      { rowKey: 0, col: ViewerColumn.Message },
      { rowKey: 1, col: ViewerColumn.ColorDot },
      visibleCols,
    );
    expect(result).toEqual(new Map([[1, new Set([ViewerColumn.ColorDot])]]));
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

function renderWithState<T>(
  hookFn: (state: ReturnType<typeof useSelectionState>) => T,
  storeOverrides?: (store: ReturnType<typeof createStore>) => void,
) {
  const store = createStore();
  store.set(visibleColsAtom, new Set([ViewerColumn.Message]));
  storeOverrides?.(store);

  const result = renderHook(
    () => {
      const state = useSelectionState();
      return hookFn(state);
    },
    {
      wrapper: ({ children }) => (
        <PreferencesProvider>
          <Provider store={store}>{children}</Provider>
        </PreferencesProvider>
      ),
    },
  );

  return { ...result, store };
}

describe('useSelection', () => {
  const records = [
    makeRecord({ message: 'log message 0', timestamp: '2024-06-15T10:30:00.000Z' }),
    makeRecord({ message: 'log message 1', timestamp: '2024-06-15T10:30:01.000Z' }),
    makeRecord({ message: 'log message 2', timestamp: '2024-06-15T10:30:02.000Z' }),
  ];

  const fakeVirtualizer = {
    getRecord: (key: number) => records[key],
    getIndexOfKey: (key: number) => key,
    getKeyAtIndex: (index: number) => (index >= 0 && index < records.length ? index : undefined),
  } as LogViewerVirtualizer;

  function renderUseSelection(storeOverrides?: (store: ReturnType<typeof createStore>) => void) {
    const store = createStore();
    store.set(visibleColsAtom, new Set([ViewerColumn.Message]));
    storeOverrides?.(store);

    const virtualizerRef =
      createRef<LogViewerVirtualizer | null>() as React.MutableRefObject<LogViewerVirtualizer | null>;
    virtualizerRef.current = fakeVirtualizer;

    const result = renderHook(() => useSelection(virtualizerRef), {
      wrapper: ({ children }) => (
        <PreferencesProvider>
          <Provider store={store}>{children}</Provider>
        </PreferencesProvider>
      ),
    });

    return { ...result, store, virtualizerRef };
  }

  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });

  describe('handleRowMouseDown', () => {
    it('selects a single row on plain mousedown', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });

      expect(result.current.selectedKeys).toEqual(new Set([1]));
    });

    it('replaces selection on plain mousedown', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });

      expect(result.current.selectedKeys).toEqual(new Set([1]));
    });

    it('toggles individual rows with meta+mousedown', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleRowMouseDown(1, clickEvent({ metaKey: true })));

      expect(result.current.selectedKeys).toEqual(new Set([0, 1]));
    });

    it('deselects a row with meta+mousedown when already selected', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      expect(result.current.selectedKeys).toEqual(new Set([0]));

      act(() => result.current.handleRowMouseDown(0, clickEvent({ metaKey: true })));
      expect(result.current.selectedKeys).toEqual(new Set());
    });

    it('selects range with shift+mousedown', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleRowMouseDown(2, clickEvent({ shiftKey: true })));

      expect(result.current.selectedKeys).toEqual(new Set([0, 1, 2]));
    });
  });

  describe('selection boundaries', () => {
    it('single selected row is both top and bottom', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });

      expect(result.current.selectionTopKeys).toEqual(new Set([1]));
      expect(result.current.selectionBottomKeys).toEqual(new Set([1]));
    });

    it('contiguous block has top on first and bottom on last', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleRowMouseDown(2, clickEvent({ shiftKey: true })));

      expect(result.current.selectionTopKeys).toEqual(new Set([0]));
      expect(result.current.selectionBottomKeys).toEqual(new Set([2]));
    });

    it('non-contiguous selection creates separate boundary groups', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
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
    it('selects a cell on click', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent()));

      expect(result.current.selectedCells).toEqual(new Map([[1, new Set([ViewerColumn.Message])]]));
    });

    it('clears row selection when clicking a cell', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      expect(result.current.selectedKeys.size).toBe(1);

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));

      expect(result.current.selectedKeys.size).toBe(0);
      expect(result.current.selectedCells).toEqual(new Map([[0, new Set([ViewerColumn.Message])]]));
    });

    it('clears text-select mode when clicking a new cell', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));

      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent()));

      expect(result.current.isTextSelectMode).toBe(false);
    });

    it('isCursorText is false after cell mousedown', () => {
      const { result, store } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));

      expect(store.get(isCursorTextAtom)).toBe(false);
    });

    it('isCursorText becomes true after mousemove following mouseup', () => {
      const { result, store } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => {
        fireEvent.mouseMove(document);
      });

      expect(store.get(isCursorTextAtom)).toBe(true);
    });

    it('isCursorText is false after modifier click', () => {
      const { result, store } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent({ metaKey: true })));

      expect(store.get(isCursorTextAtom)).toBe(false);
    });

    it('isCursorText becomes true after mousemove following modifier click', () => {
      const { result, store } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent({ metaKey: true })));
      act(() => {
        fireEvent.mouseMove(document);
      });

      expect(store.get(isCursorTextAtom)).toBe(true);
    });

    it('clears native text selection on modifier click', () => {
      const removeAllRanges = vi.fn();
      vi.spyOn(window, 'getSelection').mockReturnValue({ removeAllRanges, isCollapsed: false } as unknown as Selection);

      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });

      removeAllRanges.mockClear();
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent({ metaKey: true })));

      expect(removeAllRanges).toHaveBeenCalled();

      vi.restoreAllMocks();
    });

    it('isCursorText resets on clearSelection', () => {
      const { result, store } = renderUseSelection();

      act(() => {
        store.set(isCursorTextAtom, true);
      });
      act(() => result.current.resetSelection());

      expect(store.get(isCursorTextAtom)).toBe(false);
    });

    it('isCursorText resets on row mousedown', () => {
      const { result, store } = renderUseSelection();

      act(() => {
        store.set(isCursorTextAtom, true);
      });
      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });

      expect(store.get(isCursorTextAtom)).toBe(false);
    });

    it('ignores ColorDot column clicks', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.ColorDot, clickEvent()));

      expect(result.current.selectedCells).toEqual(new Map());
    });

    it('replaces previously selected cell on plain click', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Pod, clickEvent()));

      expect(result.current.selectedCells).toEqual(new Map([[1, new Set([ViewerColumn.Pod])]]));
    });

    it('adds cell to selection with meta+click', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Pod, clickEvent({ metaKey: true })));

      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set([ViewerColumn.Message])],
          [1, new Set([ViewerColumn.Pod])],
        ]),
      );
    });

    it('adds another column in same row with meta+click', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Timestamp, clickEvent({ metaKey: true })));

      expect(result.current.selectedCells).toEqual(
        new Map([[0, new Set([ViewerColumn.Message, ViewerColumn.Timestamp])]]),
      );
    });

    it('removes cell from selection with meta+click when already selected', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent({ metaKey: true })));

      expect(result.current.selectedCells).toEqual(new Map());
    });

    it('removes cell and cleans up empty row entry', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Pod, clickEvent({ metaKey: true })));
      // Now remove the only cell in row 0
      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent({ metaKey: true })));

      expect(result.current.selectedCells).toEqual(new Map([[1, new Set([ViewerColumn.Pod])]]));
    });

    it('adds cell with ctrl+click', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent({ ctrlKey: true })));

      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set([ViewerColumn.Message])],
          [1, new Set([ViewerColumn.Message])],
        ]),
      );
    });

    it('selects range with shift+click from anchor to target', () => {
      const { result } = renderUseSelection((store) => {
        store.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.Container, ViewerColumn.Message]));
      });

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Pod, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleCellMouseDown(2, ViewerColumn.Container, clickEvent({ shiftKey: true })));

      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set([ViewerColumn.Pod, ViewerColumn.Container])],
          [1, new Set([ViewerColumn.Pod, ViewerColumn.Container])],
          [2, new Set([ViewerColumn.Pod, ViewerColumn.Container])],
        ]),
      );
    });

    it('shift+click with no prior anchor behaves like plain click', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent({ shiftKey: true })));

      expect(result.current.selectedCells).toEqual(new Map([[1, new Set([ViewerColumn.Message])]]));
    });

    it('shift+click within same column selects column range', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleCellMouseDown(2, ViewerColumn.Message, clickEvent({ shiftKey: true })));

      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set([ViewerColumn.Message])],
          [1, new Set([ViewerColumn.Message])],
          [2, new Set([ViewerColumn.Message])],
        ]),
      );
    });

    it('shift+click clears row selection', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      expect(result.current.selectedKeys.size).toBe(1);

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent({ shiftKey: true })));

      expect(result.current.selectedKeys.size).toBe(0);
    });

    it('anchor stays on shift+click so subsequent shift+click extends from original', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent({ shiftKey: true })));

      // Extend further from the same anchor (row 0)
      act(() => result.current.handleCellMouseDown(2, ViewerColumn.Message, clickEvent({ shiftKey: true })));

      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set([ViewerColumn.Message])],
          [1, new Set([ViewerColumn.Message])],
          [2, new Set([ViewerColumn.Message])],
        ]),
      );
    });

    it('shift+click after cmd+click is additive to existing selection', () => {
      const { result } = renderUseSelection((store) => {
        store.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.Container, ViewerColumn.Message]));
      });

      // Select a cell, then Cmd+click another
      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Pod, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent({ metaKey: true })));

      // Shift+click should add the range to existing selection, not replace it
      act(() => result.current.handleCellMouseDown(2, ViewerColumn.Message, clickEvent({ shiftKey: true })));

      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set([ViewerColumn.Pod, ViewerColumn.Message])],
          [1, new Set([ViewerColumn.Message])],
          [2, new Set([ViewerColumn.Message])],
        ]),
      );
    });

    it('shift+click merges range columns with existing columns in same row', () => {
      const { result } = renderUseSelection((store) => {
        store.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.Container, ViewerColumn.Message]));
      });

      // Select Pod in row 0
      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Pod, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      // Cmd+click Container in row 1 (sets new anchor to row 1, Container)
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Container, clickEvent({ metaKey: true })));
      // Shift+click Message in row 2 — range is Container+Message in rows 1-2, merged with existing
      act(() => result.current.handleCellMouseDown(2, ViewerColumn.Message, clickEvent({ shiftKey: true })));

      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set([ViewerColumn.Pod])],
          [1, new Set([ViewerColumn.Container, ViewerColumn.Message])],
          [2, new Set([ViewerColumn.Container, ViewerColumn.Message])],
        ]),
      );
    });

    it('plain click after shift-range resets anchor', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleCellMouseDown(2, ViewerColumn.Message, clickEvent({ shiftKey: true })));

      // Plain click on unselected cell sets new anchor
      act(() => result.current.handleCellMouseDown(5, ViewerColumn.Message, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });

      // Now shift+click from new anchor (row 5)
      act(() => result.current.handleCellMouseDown(7, ViewerColumn.Message, clickEvent({ shiftKey: true })));

      expect(result.current.selectedCells).toEqual(
        new Map([
          [5, new Set([ViewerColumn.Message])],
          [6, new Set([ViewerColumn.Message])],
          [7, new Set([ViewerColumn.Message])],
        ]),
      );
    });
  });

  describe('drag selection', () => {
    function makeRowEl(key: number) {
      const el = document.createElement('div');
      el.dataset.rowKey = String(key);
      el.closest = (selector: string) => (selector === '[data-row-key]' ? el : null);
      return el;
    }

    let mockElementFromPoint: ReturnType<typeof vi.fn>;

    beforeEach(() => {
      vi.useFakeTimers();
      mockElementFromPoint = vi.fn().mockReturnValue(null);
      document.elementFromPoint = mockElementFromPoint as typeof document.elementFromPoint;
    });

    afterEach(() => {
      vi.useRealTimers();
      delete (document as Partial<Document>).elementFromPoint;
    });

    it('selects range when dragging from one row to another', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));

      mockElementFromPoint.mockReturnValue(makeRowEl(3));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 30 });
        vi.advanceTimersByTime(16);
      });

      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3]));

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('updates range dynamically as mouse moves', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));

      // Drag to row 4
      mockElementFromPoint.mockReturnValue(makeRowEl(4));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 40 });
        vi.advanceTimersByTime(16);
      });
      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3, 4]));

      // Move back to row 2 — range shrinks
      mockElementFromPoint.mockReturnValue(makeRowEl(2));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 20 });
        vi.advanceTimersByTime(16);
      });
      expect(result.current.selectedKeys).toEqual(new Set([1, 2]));

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('supports dragging upward (to lower keys)', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(2, clickEvent()));

      mockElementFromPoint.mockReturnValue(makeRowEl(0));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 5 });
        vi.advanceTimersByTime(16);
      });

      expect(result.current.selectedKeys).toEqual(new Set([0, 1, 2]));

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('ends drag on mouseup — subsequent mousemove has no effect', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));

      mockElementFromPoint.mockReturnValue(makeRowEl(2));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 20 });
        vi.advanceTimersByTime(16);
      });
      expect(result.current.selectedKeys).toEqual(new Set([1, 2]));

      act(() => {
        fireEvent.mouseUp(document);
      });

      // Move to row 4 after mouseup — selection should NOT change
      mockElementFromPoint.mockReturnValue(makeRowEl(4));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 40 });
        vi.advanceTimersByTime(16);
      });
      expect(result.current.selectedKeys).toEqual(new Set([1, 2]));
    });

    it('does not start drag on shift+mousedown', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleRowMouseDown(3, clickEvent({ shiftKey: true })));

      // Should be a range select from shift, not a drag
      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3]));

      // mousemove should NOT extend the selection
      mockElementFromPoint.mockReturnValue(makeRowEl(5));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 50 });
      });
      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3]));
    });

    it('does not start drag on meta+mousedown', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleRowMouseDown(3, clickEvent({ metaKey: true })));

      expect(result.current.selectedKeys).toEqual(new Set([1, 3]));

      // mousemove should NOT change selection
      mockElementFromPoint.mockReturnValue(makeRowEl(5));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 50 });
      });
      expect(result.current.selectedKeys).toEqual(new Set([1, 3]));
    });

    it('clears cell selection when drag starts', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      expect(result.current.selectedCells.size).toBeGreaterThan(0);

      act(() => result.current.handleRowMouseDown(1, clickEvent()));

      expect(result.current.selectedCells.size).toBe(0);
      expect(result.current.isTextSelectMode).toBe(false);

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('sets lastClickedKey on mouseup so shift-click extends from drag endpoint', () => {
      const { result, store } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));

      mockElementFromPoint.mockReturnValue(makeRowEl(3));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 30 });
      });
      act(() => {
        fireEvent.mouseUp(document);
      });

      // lastClickedKey should be set to the drag endpoint
      expect(store.get(lastClickedKeyAtom)).toBe(3);

      // shift+mousedown from there should extend
      act(() => result.current.handleRowMouseDown(5, clickEvent({ shiftKey: true })));
      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3, 4, 5]));
    });

    it('ignores mousemove when elementFromPoint returns null', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));

      mockElementFromPoint.mockReturnValue(null);
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 999 });
      });

      // Should still just have the initial selection
      expect(result.current.selectedKeys).toEqual(new Set([1]));

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('skips state update when mouse stays on same row', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));

      // Drag to row 3
      mockElementFromPoint.mockReturnValue(makeRowEl(3));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 30 });
        vi.advanceTimersByTime(16);
      });
      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3]));

      // Capture reference to verify it doesn't change
      const prevKeys = result.current.selectedKeys;

      // Move mouse within same row 3 (different coordinates, same row)
      act(() => {
        fireEvent.mouseMove(document, { clientX: 15, clientY: 35 });
        vi.advanceTimersByTime(16);
      });

      // selectedKeys reference should be unchanged (no unnecessary state update)
      expect(result.current.selectedKeys).toBe(prevKeys);

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('throttles mousemove via requestAnimationFrame', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));

      mockElementFromPoint.mockReturnValue(makeRowEl(3));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 30 });
      });

      // State should NOT be updated yet (deferred to rAF)
      expect(result.current.selectedKeys).toEqual(new Set([1]));

      // Flush the rAF
      act(() => {
        vi.advanceTimersByTime(16);
      });

      // Now state should be updated
      expect(result.current.selectedKeys).toEqual(new Set([1, 2, 3]));

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('cleans up document listeners on unmount during active drag', () => {
      const { result, unmount } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));

      // Unmount without mouseup
      unmount();

      // Reset mock to track new calls after unmount
      mockElementFromPoint.mockClear();

      // Subsequent mousemove should NOT trigger elementFromPoint (listener was removed)
      fireEvent.mouseMove(document, { clientX: 10, clientY: 50 });
      expect(mockElementFromPoint).not.toHaveBeenCalled();
    });
  });

  describe('cell drag selection', () => {
    function makeCellEl(rowKey: number, colId: string) {
      const cellEl = document.createElement('div');
      cellEl.dataset.colId = colId;
      const rowEl = document.createElement('div');
      rowEl.dataset.rowKey = String(rowKey);
      rowEl.appendChild(cellEl);
      cellEl.closest = (selector: string) => {
        if (selector === '[data-col-id]') return cellEl;
        if (selector === '[data-row-key]') return rowEl;
        return null;
      };
      return cellEl;
    }

    let mockElementFromPoint: ReturnType<typeof vi.fn>;

    beforeEach(() => {
      vi.useFakeTimers();
      mockElementFromPoint = vi.fn().mockReturnValue(null);
      document.elementFromPoint = mockElementFromPoint as typeof document.elementFromPoint;
    });

    afterEach(() => {
      vi.useRealTimers();
      delete (document as Partial<Document>).elementFromPoint;
    });

    it('drag across cells selects rectangular range', () => {
      const { result } = renderUseSelection((store) => {
        store.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.Container, ViewerColumn.Message]));
      });

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Pod, clickEvent()));

      mockElementFromPoint.mockReturnValue(makeCellEl(2, ViewerColumn.Message));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 100, clientY: 40 });
        vi.advanceTimersByTime(16);
      });

      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set([ViewerColumn.Pod, ViewerColumn.Container, ViewerColumn.Message])],
          [1, new Set([ViewerColumn.Pod, ViewerColumn.Container, ViewerColumn.Message])],
          [2, new Set([ViewerColumn.Pod, ViewerColumn.Container, ViewerColumn.Message])],
        ]),
      );

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('drag within single row selects column range', () => {
      const { result } = renderUseSelection((store) => {
        store.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.Container, ViewerColumn.Message]));
      });

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Pod, clickEvent()));

      mockElementFromPoint.mockReturnValue(makeCellEl(0, ViewerColumn.Message));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 200, clientY: 10 });
        vi.advanceTimersByTime(16);
      });

      expect(result.current.selectedCells).toEqual(
        new Map([[0, new Set([ViewerColumn.Pod, ViewerColumn.Container, ViewerColumn.Message])]]),
      );

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('drag within single column selects row range', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));

      mockElementFromPoint.mockReturnValue(makeCellEl(2, ViewerColumn.Message));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 40 });
        vi.advanceTimersByTime(16);
      });

      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set([ViewerColumn.Message])],
          [1, new Set([ViewerColumn.Message])],
          [2, new Set([ViewerColumn.Message])],
        ]),
      );

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('dragging over ColorDot cell does not change selection', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));

      mockElementFromPoint.mockReturnValue(makeCellEl(2, ViewerColumn.Message));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 40 });
        vi.advanceTimersByTime(16);
      });

      const selectionBefore = result.current.selectedCells;

      // Mouse moves over a ColorDot cell — selection should not change
      mockElementFromPoint.mockReturnValue(makeCellEl(1, ViewerColumn.ColorDot));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 25 });
        vi.advanceTimersByTime(16);
      });

      expect(result.current.selectedCells).toBe(selectionBefore);

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('drag clears row selection', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      expect(result.current.selectedKeys.size).toBe(1);

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));

      expect(result.current.selectedKeys.size).toBe(0);

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('sets lastClickedCell on mouseup', () => {
      const { result, store } = renderUseSelection((s) => {
        s.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.Container, ViewerColumn.Message]));
      });

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Pod, clickEvent()));

      mockElementFromPoint.mockReturnValue(makeCellEl(2, ViewerColumn.Message));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 100, clientY: 40 });
        vi.advanceTimersByTime(16);
      });

      act(() => {
        fireEvent.mouseUp(document);
      });

      expect(store.get(lastClickedCellAtom)).toEqual({ rowKey: 2, col: ViewerColumn.Message });
    });

    it('modifier mousedown does not start drag', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });

      act(() => result.current.handleCellMouseDown(2, ViewerColumn.Message, clickEvent({ shiftKey: true })));

      // Should be range select, not drag
      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set([ViewerColumn.Message])],
          [1, new Set([ViewerColumn.Message])],
          [2, new Set([ViewerColumn.Message])],
        ]),
      );

      // Mousemove should NOT change selection
      mockElementFromPoint.mockReturnValue(makeCellEl(4, ViewerColumn.Message));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 50 });
        vi.advanceTimersByTime(16);
      });

      expect(result.current.selectedCells).toEqual(
        new Map([
          [0, new Set([ViewerColumn.Message])],
          [1, new Set([ViewerColumn.Message])],
          [2, new Set([ViewerColumn.Message])],
        ]),
      );
    });

    it('isCursorText is false during drag', () => {
      const { result, store } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));

      mockElementFromPoint.mockReturnValue(makeCellEl(2, ViewerColumn.Message));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 40 });
        vi.advanceTimersByTime(16);
      });

      expect(store.get(isCursorTextAtom)).toBe(false);

      act(() => {
        fireEvent.mouseUp(document);
      });
    });

    it('isCursorText becomes true after mousemove following drag mouseup', () => {
      const { result, store } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));

      mockElementFromPoint.mockReturnValue(makeCellEl(2, ViewerColumn.Message));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 40 });
        vi.advanceTimersByTime(16);
      });
      act(() => {
        fireEvent.mouseUp(document);
      });

      expect(store.get(isCursorTextAtom)).toBe(false);

      act(() => {
        fireEvent.mouseMove(document);
      });

      expect(store.get(isCursorTextAtom)).toBe(true);
    });

    it('enters text-select mode after multi-cell drag mouseup', () => {
      const { result } = renderUseSelection();

      // Start drag on row 0
      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));

      // Drag to row 2
      mockElementFromPoint.mockReturnValue(makeCellEl(2, ViewerColumn.Message));
      act(() => {
        fireEvent.mouseMove(document, { clientX: 10, clientY: 40 });
        vi.advanceTimersByTime(16);
      });

      // mouseup completes the drag
      act(() => {
        fireEvent.mouseUp(document);
      });

      expect(result.current.isTextSelectMode).toBe(true);
    });

    it('enters text-select mode after modifier click', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent({ metaKey: true })));

      expect(result.current.isTextSelectMode).toBe(true);
    });

    it('defers to browser on re-click of any selected cell in text-select mode', () => {
      const { result } = renderUseSelection();

      // Select multiple cells via Cmd+click (sets isTextSelectMode to true)
      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent({ metaKey: true })));

      const selectionBefore = result.current.selectedCells;

      // Click on one of the selected cells — should NOT start a new drag
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent()));

      // Selection should be unchanged (early return, browser handles text selection)
      expect(result.current.selectedCells).toBe(selectionBefore);
    });

    it('exits text-select mode when clicking unselected cell from multi-cell selection', () => {
      const { result } = renderUseSelection();

      // Select multiple cells via Cmd+click (sets isTextSelectMode to true)
      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent({ metaKey: true })));

      // Click on an unselected cell — should start new selection
      act(() => result.current.handleCellMouseDown(2, ViewerColumn.Message, clickEvent()));

      expect(result.current.selectedCells).toEqual(new Map([[2, new Set([ViewerColumn.Message])]]));
      expect(result.current.isTextSelectMode).toBe(false);
    });
  });

  describe('handleRowMouseDown clears cell state', () => {
    it('clears cell selection when mousedown on Pos column', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      expect(result.current.selectedCells.size).toBeGreaterThan(0);

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });

      expect(result.current.selectedCells.size).toBe(0);
    });

    it('clears text-select mode when mousedown on Pos column', () => {
      const { result, store } = renderUseSelection();

      act(() => {
        store.set(selectedCellsAtom, new Map([[0, new Set([ViewerColumn.Message])]]));
        store.set(isTextSelectModeAtom, true);
      });

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });

      expect(result.current.isTextSelectMode).toBe(false);
    });
  });

  describe('resetSelection', () => {
    it('clears selection state', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      expect(result.current.selectedKeys.size).toBe(1);

      act(() => result.current.resetSelection());
      expect(result.current.selectedKeys.size).toBe(0);
    });

    it('clears cell selection and text-select mode', () => {
      const { result, store } = renderUseSelection();

      act(() => {
        store.set(selectedCellsAtom, new Map([[0, new Set([ViewerColumn.Message])]]));
        store.set(isTextSelectModeAtom, true);
      });

      act(() => result.current.resetSelection());

      expect(result.current.selectedCells.size).toBe(0);
      expect(result.current.isTextSelectMode).toBe(false);
    });
  });

  describe('keyboard shortcuts', () => {
    it('clears selection on Escape', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      expect(result.current.selectedKeys.size).toBe(1);

      act(() => {
        fireEvent.keyDown(document, { key: 'Escape' });
      });

      expect(result.current.selectedKeys.size).toBe(0);
    });

    it('copies selected rows to clipboard on Cmd+C', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleRowMouseDown(1, clickEvent({ metaKey: true })));

      act(() => {
        fireEvent.keyDown(document, { key: 'c', metaKey: true });
      });

      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('log message 0\nlog message 1');
    });

    it('does not prevent default Cmd+C when nothing is selected', () => {
      renderUseSelection();

      const event = new KeyboardEvent('keydown', { key: 'c', metaKey: true, cancelable: true, bubbles: true });
      document.dispatchEvent(event);

      expect(navigator.clipboard.writeText).not.toHaveBeenCalled();
      expect(event.defaultPrevented).toBe(false);
    });

    it('excludes ColorDot column from Cmd+C copy', () => {
      const { result } = renderUseSelection((store) => {
        store.set(visibleColsAtom, new Set([ViewerColumn.ColorDot, ViewerColumn.Pod, ViewerColumn.Message]));
      });

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });

      act(() => {
        fireEvent.keyDown(document, { key: 'c', metaKey: true });
      });

      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('my-pod-abc\tlog message 0');
    });

    it('copies cell text on Cmd+C when a cell is selected', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));

      act(() => {
        fireEvent.keyDown(document, { key: 'c', metaKey: true });
      });

      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('log message 0');
    });

    it('copies cell text for non-message columns', () => {
      const { result } = renderUseSelection((store) => {
        store.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.Message]));
      });

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Pod, clickEvent()));

      act(() => {
        fireEvent.keyDown(document, { key: 'c', metaKey: true });
      });

      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('my-pod-abc');
    });

    it('prefers cell copy over row copy when cell is selected', () => {
      const { result } = renderUseSelection();

      // Select a row, then select a cell (which clears row selection)
      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent()));

      act(() => {
        fireEvent.keyDown(document, { key: 'c', metaKey: true });
      });

      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('log message 1');
    });

    it('copies multiple selected cells on Cmd+C', () => {
      const { result } = renderUseSelection((store) => {
        store.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.Message]));
      });

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent({ metaKey: true })));

      act(() => {
        fireEvent.keyDown(document, { key: 'c', metaKey: true });
      });

      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('log message 0\nlog message 1');
    });

    it('copies multiple cells in same row tab-separated on Cmd+C', () => {
      const { result } = renderUseSelection((store) => {
        store.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.Message]));
      });

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Pod, clickEvent()));
      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent({ metaKey: true })));

      act(() => {
        fireEvent.keyDown(document, { key: 'c', metaKey: true });
      });

      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('my-pod-abc\tlog message 0');
    });

    it('clears cell selection on Escape', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
      expect(result.current.selectedCells.size).toBeGreaterThan(0);

      act(() => {
        fireEvent.keyDown(document, { key: 'Escape' });
      });

      expect(result.current.selectedCells.size).toBe(0);
    });

    it('clears text-select mode on Escape', () => {
      const { result, store } = renderUseSelection();

      act(() => {
        store.set(selectedCellsAtom, new Map([[0, new Set([ViewerColumn.Message])]]));
        store.set(isTextSelectModeAtom, true);
      });

      act(() => {
        fireEvent.keyDown(document, { key: 'Escape' });
      });

      expect(result.current.isTextSelectMode).toBe(false);
    });

    it('excludes ColorDot from default columns (Timestamp + ColorDot + Message)', () => {
      const { result } = renderUseSelection((store) => {
        store.set(visibleColsAtom, new Set([ViewerColumn.Timestamp, ViewerColumn.ColorDot, ViewerColumn.Message]));
      });

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });

      act(() => {
        fireEvent.keyDown(document, { key: 'c', metaKey: true });
      });

      const copied = (navigator.clipboard.writeText as ReturnType<typeof vi.fn>).mock.calls[0][0];
      // Should have exactly one tab (between timestamp and message), no extra spaces
      expect(copied).toMatch(/^[^\t]+\t[^\t]+$/);
      expect(copied).not.toMatch(/\t\t/);
      expect(copied).toContain('log message 0');
    });
  });
});

describe('useSelectionState', () => {
  it('returns atom values and setters', () => {
    const { result } = renderWithState((state) => state);

    expect(result.current.selectedKeys).toEqual(new Set());
    expect(result.current.selectedCells).toEqual(new Map());
    expect(result.current.visibleCols).toEqual(new Set([ViewerColumn.Message]));
    expect(result.current.isTextSelectMode).toBe(false);
    expect(result.current.isCursorText).toBe(false);
    expect(typeof result.current.setSelectedKeys).toBe('function');
    expect(typeof result.current.setSelectedCells).toBe('function');
  });

  it('clearSelection resets all state', () => {
    const { result, store } = renderWithState((state) => state);

    act(() => {
      store.set(selectedCellsAtom, new Map([[0, new Set([ViewerColumn.Message])]]));
      store.set(isTextSelectModeAtom, true);
      store.set(isCursorTextAtom, true);
    });

    act(() => result.current.clearSelection());

    expect(result.current.selectedKeys).toEqual(new Set());
    expect(result.current.selectedCells).toEqual(new Map());
    expect(result.current.isTextSelectMode).toBe(false);
    expect(result.current.isCursorText).toBe(false);
  });

  it('scheduleCursorText sets isCursorText on mousemove', () => {
    const { result, store } = renderWithState((state) => state);

    act(() => result.current.scheduleCursorText());
    act(() => {
      fireEvent.mouseMove(document);
    });

    expect(store.get(isCursorTextAtom)).toBe(true);
  });
});

describe('useRowDrag', () => {
  it('selects a single row on plain mousedown', () => {
    const { result } = renderWithState((state) => useRowDrag(state));

    act(() => result.current.handleRowMouseDown(1, clickEvent()));
    act(() => {
      fireEvent.mouseUp(document);
    });

    expect(result.current.selectionTopKeys).toEqual(new Set([1]));
    expect(result.current.selectionBottomKeys).toEqual(new Set([1]));
  });

  it('computes selection boundaries for contiguous range', () => {
    const { result } = renderWithState((state) => useRowDrag(state));

    act(() => result.current.handleRowMouseDown(0, clickEvent()));
    act(() => {
      fireEvent.mouseUp(document);
    });
    act(() => result.current.handleRowMouseDown(2, clickEvent({ shiftKey: true })));

    expect(result.current.selectionTopKeys).toEqual(new Set([0]));
    expect(result.current.selectionBottomKeys).toEqual(new Set([2]));
  });
});

describe('useCellDrag', () => {
  it('selects a cell on click', () => {
    const { result, store } = renderWithState((state) => useCellDrag(state));

    act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent()));

    expect(store.get(selectedCellsAtom)).toEqual(new Map([[1, new Set([ViewerColumn.Message])]]));
  });

  it('enters text-select mode after mouseup', () => {
    const { result, store } = renderWithState((state) => useCellDrag(state));

    act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent()));
    act(() => {
      fireEvent.mouseUp(document);
    });

    expect(store.get(isTextSelectModeAtom)).toBe(true);
  });

  it('right-click (button=2) on a selected cell preserves multi-cell selection', () => {
    const { result, store } = renderWithState(
      (state) => useCellDrag(state),
      (s) => {
        s.set(visibleColsAtom, new Set([ViewerColumn.Timestamp, ViewerColumn.Message]));
      },
    );

    const multiSelection = new Map([
      [0, new Set([ViewerColumn.Timestamp, ViewerColumn.Message])],
      [1, new Set([ViewerColumn.Timestamp, ViewerColumn.Message])],
    ]);

    act(() => {
      store.set(selectedCellsAtom, multiSelection);
    });

    act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent({ button: 2 })));

    expect(store.get(selectedCellsAtom)).toEqual(multiSelection);
  });

  it('Ctrl+click (macOS right-click) on a selected cell preserves multi-cell selection', () => {
    const { result, store } = renderWithState(
      (state) => useCellDrag(state),
      (s) => {
        s.set(visibleColsAtom, new Set([ViewerColumn.Timestamp, ViewerColumn.Message]));
      },
    );

    const multiSelection = new Map([
      [0, new Set([ViewerColumn.Timestamp, ViewerColumn.Message])],
      [1, new Set([ViewerColumn.Timestamp, ViewerColumn.Message])],
    ]);

    act(() => {
      store.set(selectedCellsAtom, multiSelection);
    });

    // macOS Ctrl+click: button=0, ctrlKey=true
    act(() => result.current.handleCellMouseDown(0, ViewerColumn.Message, clickEvent({ button: 0, ctrlKey: true })));

    expect(store.get(selectedCellsAtom)).toEqual(multiSelection);
  });

  it('right-click on a non-selected cell preserves existing selection', () => {
    const { result, store } = renderWithState((state) => useCellDrag(state));

    const original = new Map([[0, new Set([ViewerColumn.Message])]]);
    act(() => {
      store.set(selectedCellsAtom, original);
    });

    act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent({ button: 2 })));

    expect(store.get(selectedCellsAtom)).toEqual(original);
  });

  it('Ctrl+click (macOS right-click) on a non-selected cell preserves existing selection', () => {
    const { result, store } = renderWithState((state) => useCellDrag(state));

    const original = new Map([[0, new Set([ViewerColumn.Message])]]);
    act(() => {
      store.set(selectedCellsAtom, original);
    });

    act(() => result.current.handleCellMouseDown(1, ViewerColumn.Message, clickEvent({ button: 0, ctrlKey: true })));

    expect(store.get(selectedCellsAtom)).toEqual(original);
  });
});

describe('useSelectionKeyboard', () => {
  const records = [
    makeRecord({ message: 'log message 0', timestamp: '2024-06-15T10:30:00.000Z' }),
    makeRecord({ message: 'log message 1', timestamp: '2024-06-15T10:30:01.000Z' }),
  ];

  const fakeVirtualizer = {
    getRecord: (key: number) => records[key],
    getIndexOfKey: (key: number) => key,
    getKeyAtIndex: (index: number) => (index >= 0 && index < records.length ? index : undefined),
  } as LogViewerVirtualizer;

  function renderKeyboard(storeOverrides?: (store: ReturnType<typeof createStore>) => void) {
    const store = createStore();
    store.set(visibleColsAtom, new Set([ViewerColumn.Message]));
    storeOverrides?.(store);

    const virtualizerRef =
      createRef<LogViewerVirtualizer | null>() as React.MutableRefObject<LogViewerVirtualizer | null>;
    virtualizerRef.current = fakeVirtualizer;

    const result = renderHook(
      () => {
        const state = useSelectionState();
        useSelectionKeyboard(state, virtualizerRef);
        return state;
      },
      {
        wrapper: ({ children }) => (
          <PreferencesProvider>
            <Provider store={store}>{children}</Provider>
          </PreferencesProvider>
        ),
      },
    );

    return { ...result, store };
  }

  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });

  it('clears selection on Escape', () => {
    const { result, store } = renderKeyboard();

    act(() => {
      store.set(selectedCellsAtom, new Map([[0, new Set([ViewerColumn.Message])]]));
    });

    act(() => {
      fireEvent.keyDown(document, { key: 'Escape' });
    });

    expect(result.current.selectedCells.size).toBe(0);
  });

  it('does not clear selection on Escape when a context menu is open', () => {
    const { result, store } = renderKeyboard();

    act(() => {
      store.set(selectedCellsAtom, new Map([[0, new Set([ViewerColumn.Message])]]));
    });

    // Simulate Radix behavior: when a context menu is open and Escape is pressed,
    // Radix's useEscapeKeydown calls event.preventDefault() before our handler runs.
    // We use capture phase to ensure preventDefault() is called first.
    document.addEventListener(
      'keydown',
      (e) => {
        if (e.key === 'Escape') e.preventDefault();
      },
      { capture: true, once: true },
    );

    act(() => {
      fireEvent.keyDown(document, { key: 'Escape' });
    });

    // Selection should be preserved — Escape only closes the menu
    expect(result.current.selectedCells.size).toBe(1);

    // Press Escape again — no context menu this time, no preventDefault
    act(() => {
      fireEvent.keyDown(document, { key: 'Escape' });
    });

    // Now selection should be cleared
    expect(result.current.selectedCells.size).toBe(0);
  });

  it('copies selected rows on Cmd+C', () => {
    const { store } = renderKeyboard();

    act(() => {
      store.set(selectedKeysAtom, new Set([0, 1]));
    });

    act(() => {
      fireEvent.keyDown(document, { key: 'c', metaKey: true });
    });

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('log message 0\nlog message 1');
  });

  it('copies timestamps in the selected timezone on Cmd+C', () => {
    localStorage.setItem('kubetail:preferences', JSON.stringify({ version: 1, timezone: 'America/New_York' }));
    const { store } = renderKeyboard((s) => {
      s.set(visibleColsAtom, new Set([ViewerColumn.Timestamp, ViewerColumn.Message]));
    });

    act(() => {
      store.set(selectedKeysAtom, new Set([0]));
    });

    act(() => {
      fireEvent.keyDown(document, { key: 'c', metaKey: true });
    });

    // 10:30 UTC = 06:30 EDT (June is DST)
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('2024-06-15T06:30:00.000-04:00\tlog message 0');
  });

  it('moves selected cell right to the next selectable column', () => {
    const { result, store } = renderKeyboard((s) => {
      s.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.ColorDot, ViewerColumn.Message]));
    });

    act(() => {
      store.set(selectedCellsAtom, new Map([[0, new Set([ViewerColumn.Pod])]]));
      store.set(lastClickedCellAtom, { rowKey: 0, col: ViewerColumn.Pod });
    });

    act(() => {
      fireEvent.keyDown(document, { key: 'ArrowRight' });
    });

    expect(result.current.selectedCells).toEqual(new Map([[0, new Set([ViewerColumn.Message])]]));
    expect(store.get(lastClickedCellAtom)).toEqual({ rowKey: 0, col: ViewerColumn.Message });
  });

  it('moves selected cell left to the previous selectable column', () => {
    const { result, store } = renderKeyboard((s) => {
      s.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.ColorDot, ViewerColumn.Message]));
    });

    act(() => {
      store.set(selectedCellsAtom, new Map([[0, new Set([ViewerColumn.Message])]]));
      store.set(lastClickedCellAtom, { rowKey: 0, col: ViewerColumn.Message });
    });

    act(() => {
      fireEvent.keyDown(document, { key: 'ArrowLeft' });
    });

    expect(result.current.selectedCells).toEqual(new Map([[0, new Set([ViewerColumn.Pod])]]));
    expect(store.get(lastClickedCellAtom)).toEqual({ rowKey: 0, col: ViewerColumn.Pod });
  });

  it('moves selected cell down to the same column on the next row', () => {
    const { result, store } = renderKeyboard();

    act(() => {
      store.set(selectedCellsAtom, new Map([[0, new Set([ViewerColumn.Message])]]));
      store.set(lastClickedCellAtom, { rowKey: 0, col: ViewerColumn.Message });
    });

    act(() => {
      fireEvent.keyDown(document, { key: 'ArrowDown' });
    });

    expect(result.current.selectedCells).toEqual(new Map([[1, new Set([ViewerColumn.Message])]]));
    expect(store.get(lastClickedCellAtom)).toEqual({ rowKey: 1, col: ViewerColumn.Message });
  });

  it('keeps selected cell unchanged when arrow key has no target cell', () => {
    const { result, store } = renderKeyboard();

    act(() => {
      store.set(selectedCellsAtom, new Map([[0, new Set([ViewerColumn.Message])]]));
      store.set(lastClickedCellAtom, { rowKey: 0, col: ViewerColumn.Message });
    });

    act(() => {
      fireEvent.keyDown(document, { key: 'ArrowUp' });
    });

    expect(result.current.selectedCells).toEqual(new Map([[0, new Set([ViewerColumn.Message])]]));
    expect(store.get(lastClickedCellAtom)).toEqual({ rowKey: 0, col: ViewerColumn.Message });
  });
});
