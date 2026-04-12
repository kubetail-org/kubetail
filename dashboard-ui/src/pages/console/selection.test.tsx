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

import { getPlainAttribute, formatRowsForCopy, computeSelection, useSelection } from './selection';
import { ViewerColumn } from './shared';
import { isTextSelectModeAtom, lastClickedKeyAtom, selectedCellAtom, visibleColsAtom } from './state';

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

  it('returns formatted timestamp', () => {
    const result = getPlainAttribute(record, ViewerColumn.Timestamp);
    expect(result).toBe('Jun 15, 2024 10:30:01.123');
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
    expect(result).toBe('Jun 15, 2024 10:30:01.000\tmy-pod-abc\tline one');
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

describe('useSelection', () => {
  const records = [
    makeRecord({ message: 'log message 0', timestamp: '2024-06-15T10:30:00.000Z' }),
    makeRecord({ message: 'log message 1', timestamp: '2024-06-15T10:30:01.000Z' }),
    makeRecord({ message: 'log message 2', timestamp: '2024-06-15T10:30:02.000Z' }),
  ];

  const fakeVirtualizer = {
    getRecord: (key: number) => records[key],
  } as LogViewerVirtualizer;

  function renderUseSelection(storeOverrides?: (store: ReturnType<typeof createStore>) => void) {
    const store = createStore();
    store.set(visibleColsAtom, new Set([ViewerColumn.Message]));
    storeOverrides?.(store);

    const virtualizerRef =
      createRef<LogViewerVirtualizer | null>() as React.MutableRefObject<LogViewerVirtualizer | null>;
    virtualizerRef.current = fakeVirtualizer;

    const result = renderHook(() => useSelection(virtualizerRef), {
      wrapper: ({ children }) => <Provider store={store}>{children}</Provider>,
    });

    return { ...result, store, virtualizerRef };
  }

  const clickEvent = (overrides: Partial<React.MouseEvent> = {}) =>
    ({
      shiftKey: false,
      metaKey: false,
      ctrlKey: false,
      stopPropagation: vi.fn(),
      ...overrides,
    }) as unknown as React.MouseEvent;

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

  describe('handleCellClick', () => {
    it('selects a cell on click', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellClick(1, ViewerColumn.Message, clickEvent()));

      expect(result.current.selectedCell).toEqual({ rowKey: 1, col: ViewerColumn.Message });
    });

    it('clears row selection when clicking a cell', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });
      expect(result.current.selectedKeys.size).toBe(1);

      act(() => result.current.handleCellClick(0, ViewerColumn.Message, clickEvent()));

      expect(result.current.selectedKeys.size).toBe(0);
      expect(result.current.selectedCell).toEqual({ rowKey: 0, col: ViewerColumn.Message });
    });

    it('clears text-select mode when clicking without a native selection', () => {
      const { result } = renderUseSelection();

      // Simulate entering text-select mode via a prior drag
      act(() => result.current.handleCellClick(0, ViewerColumn.Message, clickEvent()));

      act(() => result.current.handleCellClick(1, ViewerColumn.Message, clickEvent()));

      expect(result.current.isTextSelectMode).toBe(false);
    });

    it('ignores ColorDot column clicks', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellClick(0, ViewerColumn.ColorDot, clickEvent()));

      expect(result.current.selectedCell).toBeNull();
    });

    it('replaces previously selected cell', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellClick(0, ViewerColumn.Message, clickEvent()));
      act(() => result.current.handleCellClick(1, ViewerColumn.Pod, clickEvent()));

      expect(result.current.selectedCell).toEqual({ rowKey: 1, col: ViewerColumn.Pod });
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

      act(() => result.current.handleCellClick(0, ViewerColumn.Message, clickEvent()));
      expect(result.current.selectedCell).not.toBeNull();

      act(() => result.current.handleRowMouseDown(1, clickEvent()));

      expect(result.current.selectedCell).toBeNull();
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

  describe('handleRowMouseDown clears cell state', () => {
    it('clears cell selection when mousedown on Pos column', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellClick(0, ViewerColumn.Message, clickEvent()));
      expect(result.current.selectedCell).not.toBeNull();

      act(() => result.current.handleRowMouseDown(0, clickEvent()));
      act(() => {
        fireEvent.mouseUp(document);
      });

      expect(result.current.selectedCell).toBeNull();
    });

    it('clears text-select mode when mousedown on Pos column', () => {
      const { result, store } = renderUseSelection();

      act(() => {
        store.set(selectedCellAtom, { rowKey: 0, col: ViewerColumn.Message });
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
        store.set(selectedCellAtom, { rowKey: 0, col: ViewerColumn.Message });
        store.set(isTextSelectModeAtom, true);
      });

      act(() => result.current.resetSelection());

      expect(result.current.selectedCell).toBeNull();
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

      act(() => result.current.handleCellClick(0, ViewerColumn.Message, clickEvent()));

      act(() => {
        fireEvent.keyDown(document, { key: 'c', metaKey: true });
      });

      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('log message 0');
    });

    it('copies cell text for non-message columns', () => {
      const { result } = renderUseSelection((store) => {
        store.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.Message]));
      });

      act(() => result.current.handleCellClick(0, ViewerColumn.Pod, clickEvent()));

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
      act(() => result.current.handleCellClick(1, ViewerColumn.Message, clickEvent()));

      act(() => {
        fireEvent.keyDown(document, { key: 'c', metaKey: true });
      });

      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('log message 1');
    });

    it('clears cell selection on Escape', () => {
      const { result } = renderUseSelection();

      act(() => result.current.handleCellClick(0, ViewerColumn.Message, clickEvent()));
      expect(result.current.selectedCell).not.toBeNull();

      act(() => {
        fireEvent.keyDown(document, { key: 'Escape' });
      });

      expect(result.current.selectedCell).toBeNull();
    });

    it('clears text-select mode on Escape', () => {
      const { result, store } = renderUseSelection();

      act(() => {
        store.set(selectedCellAtom, { rowKey: 0, col: ViewerColumn.Message });
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
