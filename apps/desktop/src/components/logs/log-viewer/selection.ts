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

// Row and cell selection over the viewer's rows, kubetail's console `selection.ts` ported.
// Keys are the core's, so a selection survives a page loading above it.
import { stripAnsi } from 'fancy-ansi';
import {
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type RefObject,
  useCallback,
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react';

import { copyToClipboard } from '@/lib/clipboard';
import { lineTime } from '@/lib/log-viewer';
import type { CoreRows, LogLine } from '@/lib/log-viewer';

import { COLUMNS, type Column } from './columns';

export type Cell = { rowKey: number; col: Column };

/** What the selection reads off the core's rows: kubetail's virtualizer lookups. */
export type SelectionRows = Pick<
  CoreRows,
  'firstKey' | 'count' | 'lineOfKey' | 'keyAt' | 'indexOfKey' | 'scrollIntoView'
>;

/** A press on a cell: the mouse, or Enter and Space standing in for it. */
type Press = ReactMouseEvent | ReactKeyboardEvent;

export function isSelectableColumn(col: Column): col is Exclude<Column, 'Dot'> {
  return col !== 'Dot';
}

/** A `data-col` value naming a selectable column: not the position, not the dot. */
function selectableColumnOf(value: string | undefined): Column | null {
  const col = COLUMNS.find((c) => c === value);
  return col && isSelectableColumn(col) ? col : null;
}

/** A cell's text as the cell draws it, unspelled and uncoloured: what a copy writes. */
export function getPlainAttribute(line: LogLine, col: Column): string {
  switch (col) {
    case 'Timestamp':
      return lineTime(line.timestamp);
    case 'Dot':
      return '';
    case 'Pod':
      return line.stream.pod;
    case 'Container':
      return line.stream.container;
    case 'Node':
      return line.place.node;
    case 'Region':
      return line.place.region;
    case 'Zone':
      return line.place.zone;
    case 'OS':
      return line.place.os;
    case 'Arch':
      return line.place.arch;
    case 'Message':
      return stripAnsi(line.message);
  }
}

function formatRow(line: LogLine, columns: Column[], filter?: Set<Column>): string {
  return columns
    .filter((col) => col !== 'Dot' && (!filter || filter.has(col)))
    .map((col) => getPlainAttribute(line, col))
    .join('\t');
}

/** A line's stream as its hole's notice names it: two instances of a container share a name. */
export function streamName({ namespace, pod, container, previous }: LogLine['stream']): string {
  return `${namespace}/${pod}/${container}${previous ? ' (previous)' : ''}`;
}

/** A line's hole as its notice draws it, unspelled; null with none. */
export function gapNotice(line: LogLine): string | null {
  if (!line.gap) return null;
  return `Lines may be missing from ${streamName(line.stream)} between ${lineTime(line.gap.after)} and ${lineTime(line.gap.before)}: the cluster no longer holds them.`;
}

// A row's copy writes its hole too, so a pasted log still shows it.
export function formatRowsForCopy(lines: LogLine[], columns: Column[]): string {
  return lines
    .map((line) => {
      const notice = gapNotice(line);
      const row = formatRow(line, columns);
      return notice ? `[${notice}]\n${row}` : row;
    })
    .join('\n');
}

export function formatCellsForCopy(
  selectedCells: Map<number, Set<Column>>,
  columns: Column[],
  lineOfKey: (key: number) => LogLine | undefined,
): string {
  return [...selectedCells.keys()]
    .toSorted((a, b) => a - b)
    .map((rowKey) => {
      const line = lineOfKey(rowKey);
      return line ? formatRow(line, columns, selectedCells.get(rowKey)) : '';
    })
    .filter((text) => text !== '')
    .join('\n');
}

/** The row selection after a click on `clickedKey`. */
export function computeSelection({
  prev,
  clickedKey,
  shiftKey,
  metaOrCtrlKey,
  lastClickedKey,
}: {
  prev: Set<number>;
  clickedKey: number;
  shiftKey: boolean;
  metaOrCtrlKey: boolean;
  lastClickedKey: number | null;
}): Set<number> {
  if (shiftKey && lastClickedKey !== null) {
    // Keys are consecutive, so the range is every integer between.
    const next = new Set(prev);
    for (let k = Math.min(lastClickedKey, clickedKey); k <= Math.max(lastClickedKey, clickedKey); k += 1) next.add(k);
    return next;
  }
  if (metaOrCtrlKey) {
    const next = new Set(prev);
    if (next.has(clickedKey)) next.delete(clickedKey);
    else next.add(clickedKey);
    return next;
  }
  return new Set([clickedKey]);
}

/** The rectangle between two cells. If either is the dot, the target alone. */
export function computeCellRange(anchor: Cell, target: Cell, columns: Column[]): Map<number, Set<Column>> {
  const cols: Column[] = columns.filter(isSelectableColumn);
  const anchorIdx = cols.indexOf(anchor.col);
  const targetIdx = cols.indexOf(target.col);
  if (anchorIdx === -1 || targetIdx === -1) return new Map([[target.rowKey, new Set([target.col])]]);

  // One Set shared by every row: a drag that only grows the row span keeps each row's
  // reference, so the row memo skips rows whose selection did not change.
  const colSet = new Set(cols.slice(Math.min(anchorIdx, targetIdx), Math.max(anchorIdx, targetIdx) + 1));
  const result = new Map<number, Set<Column>>();
  for (let r = Math.min(anchor.rowKey, target.rowKey); r <= Math.max(anchor.rowKey, target.rowKey); r += 1) {
    result.set(r, colSet);
  }
  return result;
}

/**
 * True when more than one cell is selected, in one row or many: the anchor's mark says
 * something only then, since one cell's own borders already name it.
 */
export function hasMultipleSelectedCells(selectedCells: Map<number, Set<Column>>): boolean {
  if (selectedCells.size > 1) return true;
  const only = selectedCells.values().next().value;
  return only !== undefined && only.size > 1;
}

/**
 * The selected cell after `after` in reading order — left to right, then top to bottom —
 * wrapping to the first. `after` need not be selected: it is usually the cell just removed.
 */
function nextSelectedCellInReadingOrder(
  selectedCells: Map<number, Set<Column>>,
  after: Cell,
  columns: Column[],
): Cell | null {
  const cols: Column[] = columns.filter(isSelectableColumn);
  const ordered = [...selectedCells.keys()]
    .toSorted((a, b) => a - b)
    .flatMap((rowKey) => cols.filter((col) => selectedCells.get(rowKey)?.has(col)).map((col) => ({ rowKey, col })));
  if (ordered.length === 0) return null;

  const afterIdx = cols.indexOf(after.col);
  const next = ordered.find(
    (cell) => cell.rowKey > after.rowKey || (cell.rowKey === after.rowKey && cols.indexOf(cell.col) > afterIdx),
  );
  return next ?? ordered[0];
}

const ARROW_KEYS = ['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'] as const;
type ArrowKey = (typeof ARROW_KEYS)[number];
const isArrowKey = (key: string): key is ArrowKey => (ARROW_KEYS as readonly string[]).includes(key);

function getCellInArrowDirection(cell: Cell, key: ArrowKey, columns: Column[], rows: SelectionRows): Cell | null {
  const cols: Column[] = columns.filter(isSelectableColumn);
  const colIndex = cols.indexOf(cell.col);
  if (colIndex === -1) return null;

  if (key === 'ArrowLeft' || key === 'ArrowRight') {
    const col = cols[colIndex + (key === 'ArrowRight' ? 1 : -1)];
    return col ? { rowKey: cell.rowKey, col } : null;
  }
  const rowIndex = rows.indexOfKey(cell.rowKey);
  if (rowIndex < 0) return null;
  const rowKey = rows.keyAt(rowIndex + (key === 'ArrowDown' ? 1 : -1));
  return rowKey === undefined ? null : { rowKey, col: cell.col };
}

type SelectionFields = {
  selectedKeys: Set<number>;
  /** The row a shift-click extends from. */
  lastClickedKey: number | null;
  selectedCells: Map<number, Set<Column>>;
  /** Always a selected cell, or null with none selected: the arrow keys' cursor. */
  anchorCell: Cell | null;
};

// Kubetail keeps these in jotai atoms its handlers read and write synchronously. A store
// per hook keeps that shape, so a handler reads what the last one wrote in the same event,
// and each viewer has its own.
type SelectionStore = {
  get: <K extends keyof SelectionFields>(key: K) => SelectionFields[K];
  set: <K extends keyof SelectionFields>(key: K, value: SelectionFields[K]) => void;
  subscribe: (listener: () => void) => () => void;
  getSnapshot: () => SelectionFields;
};

function createSelectionStore(): SelectionStore {
  let state: SelectionFields = {
    selectedKeys: new Set(),
    lastClickedKey: null,
    selectedCells: new Map(),
    anchorCell: null,
  };
  const listeners = new Set<() => void>();
  return {
    get: (key) => state[key],
    set: (key, value) => {
      if (state[key] === value) return;
      state = { ...state, [key]: value };
      listeners.forEach((listener) => listener());
    },
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    getSnapshot: () => state,
  };
}

function clearRowSelection(store: SelectionStore) {
  if (store.get('selectedKeys').size > 0) store.set('selectedKeys', new Set());
  store.set('lastClickedKey', null);
}

function clearCellSelection(store: SelectionStore) {
  if (store.get('selectedCells').size > 0) store.set('selectedCells', new Map());
  store.set('anchorCell', null);
}

// A drop hands its keys out again on the next read back, to other lines, so the selection lets
// go of every key outside `[from, to)` before one can name another line.
function pruneTo(store: SelectionStore, from: number, to: number) {
  const held = (key: number) => key >= from && key < to;
  const keys = store.get('selectedKeys');
  const keptKeys = [...keys].filter(held);
  if (keptKeys.length !== keys.size) store.set('selectedKeys', new Set(keptKeys));
  const cells = store.get('selectedCells');
  const keptCells = [...cells].filter(([key]) => held(key));
  if (keptCells.length !== cells.size) store.set('selectedCells', new Map(keptCells));
  const last = store.get('lastClickedKey');
  if (last !== null && !held(last)) store.set('lastClickedKey', null);
  const anchor = store.get('anchorCell');
  if (anchor !== null && !held(anchor.rowKey)) store.set('anchorCell', null);
}

function selectSingleCell(store: SelectionStore, cell: Cell) {
  store.set('selectedCells', new Map([[cell.rowKey, new Set([cell.col])]]));
  store.set('anchorCell', cell);
}

/**
 * A drag on `document`: moves coalesced into one frame, torn down through an AbortController.
 * `onMove` runs once per distinct target; `onCommit` runs at mouse up with the last target,
 * the initial one if the pointer never moved.
 */
function startDocumentDrag<T>(args: {
  initialTarget: T;
  resolveTarget: (x: number, y: number) => T | null;
  isSameTarget: (a: T, b: T) => boolean;
  onMove: (target: T) => void;
  onCommit: (lastTarget: T) => void;
  abortRef: RefObject<AbortController | null>;
}) {
  const { initialTarget, resolveTarget, isSameTarget, onMove, onCommit, abortRef } = args;

  let rafId: number | null = null;
  let pendingX = 0;
  let pendingY = 0;
  let lastTarget = initialTarget;

  const processMove = () => {
    rafId = null;
    const target = resolveTarget(pendingX, pendingY);
    if (target === null || isSameTarget(target, lastTarget)) return;
    lastTarget = target;
    onMove(target);
  };

  const onMouseMove = (e: MouseEvent) => {
    e.preventDefault();
    pendingX = e.clientX;
    pendingY = e.clientY;
    rafId ??= requestAnimationFrame(processMove);
  };

  const onMouseUp = (e: MouseEvent) => {
    if (rafId !== null) {
      cancelAnimationFrame(rafId);
      pendingX = e.clientX;
      pendingY = e.clientY;
      processMove();
    }
    onCommit(lastTarget);
    abortRef.current?.abort();
    abortRef.current = null;
  };

  abortRef.current?.abort();
  abortRef.current = new AbortController();
  const { signal } = abortRef.current;
  // An unmount mid-drag aborts with a frame still queued.
  signal.addEventListener('abort', () => {
    if (rafId !== null) cancelAnimationFrame(rafId);
  });
  document.addEventListener('mousemove', onMouseMove, { signal });
  document.addEventListener('mouseup', onMouseUp, { signal });
}

function useSelectionState() {
  const [store] = useState(createSelectionStore);
  const { selectedKeys, selectedCells, anchorCell } = useSyncExternalStore(store.subscribe, store.getSnapshot);
  const dragAbortRef = useRef<AbortController | null>(null);
  const clearSelection = useCallback(() => {
    clearRowSelection(store);
    clearCellSelection(store);
  }, [store]);
  return { store, selectedKeys, selectedCells, anchorCell, dragAbortRef, clearSelection };
}

type SelectionState = ReturnType<typeof useSelectionState>;

function useRowDrag({ store, dragAbortRef }: SelectionState) {
  return useCallback(
    (key: number, event: Press) => {
      // A modifier click changes the selection and starts no drag.
      if (event.shiftKey || event.metaKey || event.ctrlKey) {
        const next = computeSelection({
          prev: store.get('selectedKeys'),
          clickedKey: key,
          shiftKey: event.shiftKey,
          metaOrCtrlKey: event.metaKey || event.ctrlKey,
          lastClickedKey: store.get('lastClickedKey'),
        });
        store.set('selectedKeys', next);
        store.set('lastClickedKey', key);
        clearCellSelection(store);
        return;
      }

      store.set('selectedKeys', new Set([key]));
      clearCellSelection(store);

      startDocumentDrag<number>({
        initialTarget: key,
        resolveTarget: (x, y) => {
          const rowEl = document.elementFromPoint(x, y)?.closest<HTMLElement>('[data-row-key]');
          if (!rowEl) return null;
          const k = Number(rowEl.dataset.rowKey);
          return Number.isNaN(k) ? null : k;
        },
        isSameTarget: (a, b) => a === b,
        onMove: (endKey) => {
          const next = new Set<number>();
          for (let k = Math.min(key, endKey); k <= Math.max(key, endKey); k += 1) next.add(k);
          store.set('selectedKeys', next);
        },
        onCommit: (endKey) => store.set('lastClickedKey', endKey),
        abortRef: dragAbortRef,
      });
    },
    [store, dragAbortRef],
  );
}

function useCellDrag({ store, dragAbortRef }: SelectionState, columns: Column[]) {
  return useCallback(
    (rowKey: number, col: Column, event: Press) => {
      if (col === 'Dot') return;
      event.stopPropagation();

      // A right-click (or Ctrl+click on macOS) keeps the selection for the context menu.
      const button = 'button' in event ? event.button : undefined;
      if (button === 2 || (button === 0 && event.ctrlKey && !event.metaKey && !event.shiftKey)) return;

      if (event.shiftKey || event.metaKey || event.ctrlKey) {
        const anchor = store.get('anchorCell');
        if (event.shiftKey && anchor !== null) {
          // As in a spreadsheet: the rectangle from the anchor replaces the selection,
          // dropping disjoint cells a Cmd+click added.
          store.set('selectedCells', computeCellRange(anchor, { rowKey, col }, columns));
        } else if (event.metaKey || event.ctrlKey) {
          const next = new Map(store.get('selectedCells'));
          const rowCols = new Set(next.get(rowKey));
          const wasSelected = rowCols.delete(col);
          if (!wasSelected) rowCols.add(col);
          if (rowCols.size === 0) next.delete(rowKey);
          else next.set(rowKey, rowCols);
          store.set('selectedCells', next);

          // The anchor must stay a selected cell, or null when none is.
          if (!wasSelected) store.set('anchorCell', { rowKey, col });
          else if (anchor?.rowKey === rowKey && anchor.col === col) {
            store.set('anchorCell', nextSelectedCellInReadingOrder(next, { rowKey, col }, columns));
          }
        } else {
          selectSingleCell(store, { rowKey, col });
        }
        clearRowSelection(store);
        return;
      }

      // The anchor is where the drag starts, so a later shift-click extends from there.
      const start = { rowKey, col };
      selectSingleCell(store, start);
      clearRowSelection(store);

      startDocumentDrag<Cell>({
        initialTarget: start,
        resolveTarget: (x, y) => {
          const el = document.elementFromPoint(x, y);
          const cellEl = el?.closest<HTMLElement>('[data-col]');
          const rowEl = el?.closest<HTMLElement>('[data-row-key]');
          if (!cellEl || !rowEl) return null;
          const endRowKey = Number(rowEl.dataset.rowKey);
          const endCol = selectableColumnOf(cellEl.dataset.col);
          return Number.isNaN(endRowKey) || endCol === null ? null : { rowKey: endRowKey, col: endCol };
        },
        isSameTarget: (a, b) => a.rowKey === b.rowKey && a.col === b.col,
        onMove: (end) => store.set('selectedCells', computeCellRange(start, end, columns)),
        onCommit: () => {},
        abortRef: dragAbortRef,
      });
    },
    [store, columns, dragAbortRef],
  );
}

/** For each run of selected rows, its first key and its last, for the row's edge lines. */
function useSelectionEdges(selectedKeys: Set<number>) {
  return useMemo(() => {
    if (selectedKeys.size === 0) return { selectionTopKeys: selectedKeys, selectionBottomKeys: selectedKeys };
    const top = new Set<number>();
    const bottom = new Set<number>();
    selectedKeys.forEach((k) => {
      if (!selectedKeys.has(k - 1)) top.add(k);
      if (!selectedKeys.has(k + 1)) bottom.add(k);
    });
    return { selectionTopKeys: top, selectionBottomKeys: bottom };
  }, [selectedKeys]);
}

// On the viewer's scroller, not `document`, so a key reaches the viewer that has focus alone.
function useSelectionKeyboard(
  { store, clearSelection }: SelectionState,
  rows: SelectionRows,
  scroller: HTMLElement | null,
  columns: Column[],
) {
  const onKeyDown = useEffectEvent((e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      // Something else (a context menu closing) already took it.
      if (!e.defaultPrevented) clearSelection();
      return;
    }

    if ((e.metaKey || e.ctrlKey) && e.key === 'c') {
      const native = window.getSelection();
      if (native && !native.isCollapsed) return;

      const cells = store.get('selectedCells');
      const keys = store.get('selectedKeys');
      if (cells.size > 0) {
        e.preventDefault();
        copyToClipboard(formatCellsForCopy(cells, columns, rows.lineOfKey));
      } else if (keys.size > 0) {
        e.preventDefault();
        copyToClipboard(formatRowsForCopy(linesOf(keys, rows), columns));
      }
      return;
    }

    if (isArrowKey(e.key) && !e.metaKey && !e.ctrlKey && !e.altKey) {
      // The anchor is always a selected cell or null, so it doubles as the keyboard cursor.
      const anchor = store.get('anchorCell');
      const next = anchor && getCellInArrowDirection(anchor, e.key, columns, rows);
      if (!next) return;
      e.preventDefault();
      selectSingleCell(store, next);
      // The clicked cell holds focus, and its row unmounts once scrolled out of the
      // virtualizer's range, taking the keys with it; the scroller never unmounts.
      scroller?.focus({ preventScroll: true });
      // The key's own scroll is cancelled, so the cursor's row is brought into view here.
      rows.scrollIntoView(rows.indexOfKey(next.rowKey));
    }
  });

  useEffect(() => {
    if (scroller === null) return undefined;
    const handler = (e: KeyboardEvent) => onKeyDown(e);
    scroller.addEventListener('keydown', handler);
    return () => scroller.removeEventListener('keydown', handler);
  }, [scroller]);
}

/** The lines under `keys`, in key order. */
function linesOf(keys: Iterable<number>, rows: SelectionRows): LogLine[] {
  return [...keys]
    .toSorted((a, b) => a - b)
    .map(rows.lineOfKey)
    .filter((line) => line !== undefined);
}

/**
 * The selection of one landing's rows: its state, the handlers the rows mount, and the keys
 * on `scroller`. `onLines` gets the selected lines whenever the selection moves — a cell
 * block's rows' lines — for the viewport.
 */
export function useSelection(
  rows: SelectionRows,
  scroller: HTMLElement | null,
  columns: Column[],
  onLines: (lines: LogLine[]) => void,
) {
  const state = useSelectionState();
  const { selectedKeys, selectedCells, anchorCell, dragAbortRef, clearSelection } = state;
  const handleRowMouseDown = useRowDrag(state);
  const handleCellMouseDown = useCellDrag(state, columns);
  useSelectionKeyboard(state, rows, scroller, columns);
  const edges = useSelectionEdges(selectedKeys);
  const { firstKey, count } = rows;
  useLayoutEffect(() => pruneTo(state.store, firstKey, firstKey + count), [state.store, firstKey, count]);

  const report = useEffectEvent(() =>
    onLines(linesOf(selectedCells.size > 0 ? selectedCells.keys() : selectedKeys, rows)),
  );
  useEffect(() => report(), [selectedKeys, selectedCells]);

  useEffect(() => () => dragAbortRef.current?.abort(), [dragAbortRef]);

  return {
    selectedKeys,
    ...edges,
    selectedCells,
    anchorCell,
    handleRowMouseDown,
    handleCellMouseDown,
    resetSelection: clearSelection,
  };
}
