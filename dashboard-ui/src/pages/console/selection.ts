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

import { useAtomValue, useStore } from 'jotai';
import { useCallback, useEffect, useMemo, useRef } from 'react';

import { stripAnsi } from 'fancy-ansi';

import { TimestampFormat, formatTimestamp, useTimestampFormat } from '@/lib/timestamp-format';
import { useTimezone } from '@/lib/timezone';

import type { LogRecord, LogViewerVirtualizer } from '@/components/widgets/log-viewer';

import { ViewerColumn } from './shared';
import {
  isCursorTextAtom,
  isTextSelectModeAtom,
  anchorCellAtom,
  lastClickedKeyAtom,
  selectedCellsAtom,
  selectedKeysAtom,
  visibleColsAtom,
} from './state';

type SelectableViewerColumn = Exclude<ViewerColumn, ViewerColumn.ColorDot>;

export function isSelectableViewerColumn(col: ViewerColumn): col is SelectableViewerColumn {
  return col !== ViewerColumn.ColorDot;
}

/**
 * getPlainAttribute - Returns a plain text string for a given log record column.
 * This is the plain-text counterpart of getAttribute() in main.tsx (which returns JSX).
 */
export function getPlainAttribute(
  record: LogRecord,
  col: ViewerColumn,
  timezone = 'UTC',
  timestampFormat: string = TimestampFormat.ISO_8601,
): string {
  switch (col) {
    case ViewerColumn.Timestamp:
      return formatTimestamp(record.timestamp, timezone, timestampFormat);
    case ViewerColumn.ColorDot:
      return '';
    case ViewerColumn.Pod:
      return record.source.podName;
    case ViewerColumn.Container:
      return record.source.containerName;
    case ViewerColumn.Region:
      return record.source.metadata.region;
    case ViewerColumn.Zone:
      return record.source.metadata.zone;
    case ViewerColumn.OS:
      return record.source.metadata.os;
    case ViewerColumn.Arch:
      return record.source.metadata.arch;
    case ViewerColumn.Node:
      return record.source.metadata.node;
    case ViewerColumn.Message:
      return stripAnsi(record.message);
    default:
      throw new Error('not implemented');
  }
}

/**
 * formatRow - Formats a single record as tab-separated column values.
 * ColorDot is skipped. When filter is provided, only matching columns are included.
 */
function formatRow(
  record: LogRecord,
  visibleCols: Set<ViewerColumn>,
  timezone: string,
  timestampFormat: string,
  filter?: Set<ViewerColumn>,
): string {
  const parts: string[] = [];
  visibleCols.forEach((col) => {
    if (col === ViewerColumn.ColorDot) return;
    if (filter && !filter.has(col)) return;
    parts.push(getPlainAttribute(record, col, timezone, timestampFormat));
  });
  return parts.join('\t');
}

/**
 * formatRowsForCopy - Formats an array of log records as copyable text.
 * Columns are tab-separated, rows are newline-separated. ColorDot is skipped.
 */
export function formatRowsForCopy(
  records: LogRecord[],
  visibleCols: Set<ViewerColumn>,
  timezone = 'UTC',
  timestampFormat: string = TimestampFormat.ISO_8601,
): string {
  return records.map((record) => formatRow(record, visibleCols, timezone, timestampFormat)).join('\n');
}

/**
 * formatCellsForCopy - Formats selected cells as copyable text.
 * Columns are tab-separated within a row, rows are newline-separated. ColorDot is skipped.
 * Rows are sorted by key ascending; only selected columns (in visibleCols order) are included.
 */
export function formatCellsForCopy(
  selectedCells: Map<number, Set<ViewerColumn>>,
  visibleCols: Set<ViewerColumn>,
  getRecord: (key: number) => LogRecord | undefined,
  timezone = 'UTC',
  timestampFormat: string = TimestampFormat.ISO_8601,
): string {
  return [...selectedCells.keys()]
    .sort((a, b) => a - b)
    .map((rowKey) => {
      const record = getRecord(rowKey);
      if (!record) return null;
      const text = formatRow(record, visibleCols, timezone, timestampFormat, selectedCells.get(rowKey));
      return text || null;
    })
    .filter((line): line is string => line !== null)
    .join('\n');
}

/**
 * hasMultipleSelectedCells - True when the selection contains more than one
 * cell (across one or many rows). Used to decide whether anchor markers add
 * useful information — for a single-cell selection the per-edge borders
 * already convey the cell's identity.
 */
export function hasMultipleSelectedCells(selectedCells: Map<number, Set<ViewerColumn>>): boolean {
  if (selectedCells.size > 1) return true;
  const only = selectedCells.values().next().value;
  return only !== undefined && only.size > 1;
}

/**
 * computeCellRange - Pure function that computes a rectangular cell selection between two cells.
 * If either anchor or target is ColorDot, returns just the target cell.
 */
export function computeCellRange(
  anchor: { rowKey: number; col: ViewerColumn },
  target: { rowKey: number; col: ViewerColumn },
  visibleCols: Set<ViewerColumn>,
): Map<number, Set<ViewerColumn>> {
  const cols = [...visibleCols].filter(isSelectableViewerColumn);
  const anchorIdx = isSelectableViewerColumn(anchor.col) ? cols.indexOf(anchor.col) : -1;
  const targetIdx = isSelectableViewerColumn(target.col) ? cols.indexOf(target.col) : -1;

  if (anchorIdx === -1 || targetIdx === -1) {
    return new Map([[target.rowKey, new Set([target.col])]]);
  }

  const minRow = Math.min(anchor.rowKey, target.rowKey);
  const maxRow = Math.max(anchor.rowKey, target.rowKey);
  const minCol = Math.min(anchorIdx, targetIdx);
  const maxCol = Math.max(anchorIdx, targetIdx);

  // Every row in the rectangle has the same selected cols, so share a single
  // Set across all entries. This keeps row-level Set references stable when
  // the col span doesn't change (only the row span grows/shrinks during a
  // drag) — which lets RecordRow's memo skip rows whose selection didn't
  // actually change frame-to-frame.
  const colSet = new Set<ViewerColumn>();
  for (let c = minCol; c <= maxCol; c += 1) colSet.add(cols[c]);

  const result = new Map<number, Set<ViewerColumn>>();
  for (let r = minRow; r <= maxRow; r += 1) result.set(r, colSet);
  return result;
}

/**
 * nextSelectedCellInReadingOrder - Returns the selected cell that comes after
 * `after` in spreadsheet reading order (left-to-right within a row, then
 * top-to-bottom across rows), wrapping around to the first selected cell when
 * `after` is past the end. Returns null when nothing is selected.
 *
 * `after` does not need to be in `selectedCells` — typical use is to find the
 * next anchor right after deselecting the current one, where `after` is the
 * just-deselected cell.
 */
function nextSelectedCellInReadingOrder(
  selectedCells: Map<number, Set<ViewerColumn>>,
  after: { rowKey: number; col: ViewerColumn },
  visibleCols: Set<ViewerColumn>,
): { rowKey: number; col: ViewerColumn } | null {
  if (selectedCells.size === 0) return null;

  const cols = [...visibleCols].filter(isSelectableViewerColumn);
  const sortedRows = [...selectedCells.keys()].sort((a, b) => a - b);

  // Flatten selection into reading order.
  const ordered = sortedRows.flatMap((rowKey) => {
    const rowCols = selectedCells.get(rowKey);
    return rowCols ? cols.filter((c) => rowCols.has(c)).map((col) => ({ rowKey, col })) : [];
  });
  if (ordered.length === 0) return null;

  const afterColIdx = isSelectableViewerColumn(after.col) ? cols.indexOf(after.col) : -1;
  const next = ordered.find((cell) => {
    const cellColIdx = cols.indexOf(cell.col);
    return cell.rowKey > after.rowKey || (cell.rowKey === after.rowKey && cellColIdx > afterColIdx);
  });
  // Past the last cell → wrap to the first.
  return next ?? ordered[0];
}

const ARROW_KEYS = ['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'] as const;
type ArrowKey = (typeof ARROW_KEYS)[number];
const isArrowKey = (key: string): key is ArrowKey => (ARROW_KEYS as readonly string[]).includes(key);

function getCellInArrowDirection(
  activeCell: { rowKey: number; col: ViewerColumn },
  key: ArrowKey,
  visibleCols: Set<ViewerColumn>,
  virtualizer: LogViewerVirtualizer,
): { rowKey: number; col: ViewerColumn } | null {
  if (!isSelectableViewerColumn(activeCell.col)) return null;

  const cols = [...visibleCols].filter(isSelectableViewerColumn);
  const colIndex = cols.indexOf(activeCell.col);
  if (colIndex === -1) return null;

  if (key === 'ArrowLeft' || key === 'ArrowRight') {
    const nextColIndex = colIndex + (key === 'ArrowRight' ? 1 : -1);
    const col = cols[nextColIndex];
    return col ? { rowKey: activeCell.rowKey, col } : null;
  }

  const rowIndex = virtualizer.getIndexOfKey(activeCell.rowKey);
  if (rowIndex < 0) return null;

  const nextRowIndex = rowIndex + (key === 'ArrowDown' ? 1 : -1);
  const rowKey = virtualizer.getKeyAtIndex(nextRowIndex);
  return rowKey === undefined ? null : { rowKey, col: activeCell.col };
}

/**
 * computeSelection - Pure function that computes the next selection state based on a click event.
 */
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
    // Range select: keys are sequential so we can iterate directly
    const minKey = Math.min(lastClickedKey, clickedKey);
    const maxKey = Math.max(lastClickedKey, clickedKey);
    const next = new Set(prev);
    for (let k = minKey; k <= maxKey; k += 1) {
      next.add(k);
    }
    return next;
  }

  if (metaOrCtrlKey) {
    // Toggle individual
    const next = new Set(prev);
    if (next.has(clickedKey)) {
      next.delete(clickedKey);
    } else {
      next.add(clickedKey);
    }
    return next;
  }

  // Single select
  return new Set([clickedKey]);
}

type Store = ReturnType<typeof useStore>;

/** Reset row-selection state. No-op write on selectedKeysAtom is skipped. */
function clearRowSelection(store: Store) {
  if (store.get(selectedKeysAtom).size > 0) store.set(selectedKeysAtom, new Set());
  store.set(lastClickedKeyAtom, null);
}

/**
 * Reset cell-selection state, including the text-select / cursor-text flags
 * (those modes only exist while a cell selection is active).
 */
function clearCellSelection(store: Store) {
  if (store.get(selectedCellsAtom).size > 0) store.set(selectedCellsAtom, new Map());
  store.set(anchorCellAtom, null);
  store.set(isTextSelectModeAtom, false);
  store.set(isCursorTextAtom, false);
}

/** Replace the cell selection with a single cell and move the anchor to it. */
function selectSingleCell(store: Store, cell: { rowKey: number; col: ViewerColumn }) {
  store.set(selectedCellsAtom, new Map([[cell.rowKey, new Set([cell.col])]]));
  store.set(anchorCellAtom, cell);
}

/**
 * Run a document-level drag: register mousemove/mouseup, coalesce moves into
 * one rAF, and tear down via an AbortController. The caller plugs in:
 *
 * - `resolveTarget` — mouse coords → drag target (or null if not over one).
 * - `isSameTarget` — skip redundant `onMove` calls for the same target.
 * - `onMove` — runs once per distinct target, rAF-throttled.
 * - `onCommit` — runs at mouseup with the most recent target. If the user
 *   never moved, it receives `initialTarget` (so a click-without-drag still
 *   commits the press location).
 */
function startDocumentDrag<T>(args: {
  initialTarget: T;
  resolveTarget: (x: number, y: number) => T | null;
  isSameTarget: (a: T, b: T) => boolean;
  onMove: (target: T) => void;
  onCommit: (lastTarget: T) => void;
  abortRef: React.RefObject<AbortController | null>;
}) {
  const { initialTarget, resolveTarget, isSameTarget, onMove, onCommit, abortRef } = args;

  let rafId: number | null = null;
  let pendingX = 0;
  let pendingY = 0;
  let lastTarget: T = initialTarget;

  const processMove = () => {
    rafId = null;
    const target = resolveTarget(pendingX, pendingY);
    if (target === null) return;
    if (isSameTarget(target, lastTarget)) return;
    lastTarget = target;
    onMove(target);
  };

  const onMouseMove = (e: MouseEvent) => {
    e.preventDefault();
    pendingX = e.clientX;
    pendingY = e.clientY;
    if (rafId === null) rafId = requestAnimationFrame(processMove);
  };

  const onMouseUp = (e: MouseEvent) => {
    if (rafId !== null) {
      cancelAnimationFrame(rafId);
      rafId = null;
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
  document.addEventListener('mousemove', onMouseMove, { signal });
  document.addEventListener('mouseup', onMouseUp, { signal });
}

export function useSelectionState() {
  const store = useStore();
  const visibleCols = useAtomValue(visibleColsAtom);
  const selectedKeys = useAtomValue(selectedKeysAtom);
  const selectedCells = useAtomValue(selectedCellsAtom);
  const anchorCell = useAtomValue(anchorCellAtom);
  const isTextSelectMode = useAtomValue(isTextSelectModeAtom);
  const isCursorText = useAtomValue(isCursorTextAtom);

  const dragAbortRef = useRef<AbortController | null>(null);
  const cursorTextPendingRef = useRef(false);
  // Long-lived controller for the cursor-text one-shot mousemove listener.
  // Aborted on unmount so a click immediately followed by unmount doesn't
  // leak a listener that fires on the next mousemove anywhere in the doc.
  const cursorTextAbortRef = useRef<AbortController | null>(null);
  const scheduleCursorText = useCallback(() => {
    if (cursorTextPendingRef.current) return;
    cursorTextPendingRef.current = true;
    if (!cursorTextAbortRef.current) cursorTextAbortRef.current = new AbortController();
    document.addEventListener(
      'mousemove',
      () => {
        cursorTextPendingRef.current = false;
        store.set(isCursorTextAtom, true);
      },
      { once: true, signal: cursorTextAbortRef.current.signal },
    );
  }, [store]);
  useEffect(() => () => cursorTextAbortRef.current?.abort(), []);

  const clearSelection = useCallback(() => {
    clearRowSelection(store);
    clearCellSelection(store);
  }, [store]);

  return {
    store,
    visibleCols,
    selectedKeys,
    selectedCells,
    anchorCell,
    isTextSelectMode,
    isCursorText,
    dragAbortRef,
    clearSelection,
    scheduleCursorText,
  };
}

export function useRowDrag(state: ReturnType<typeof useSelectionState>) {
  const { store, dragAbortRef } = state;

  const handleRowMouseDown = useCallback(
    (key: number, event: React.MouseEvent) => {
      // Modifier clicks don't start a drag
      if (event.shiftKey || event.metaKey || event.ctrlKey) {
        const next = computeSelection({
          prev: store.get(selectedKeysAtom),
          clickedKey: key,
          shiftKey: event.shiftKey,
          metaOrCtrlKey: event.metaKey || event.ctrlKey,
          lastClickedKey: store.get(lastClickedKeyAtom),
        });
        store.set(selectedKeysAtom, next);
        store.set(lastClickedKeyAtom, key);
        clearCellSelection(store);
        return;
      }

      store.set(selectedKeysAtom, new Set([key]));
      clearCellSelection(store);

      startDocumentDrag<number>({
        initialTarget: key,
        resolveTarget: (x, y) => {
          const el = document.elementFromPoint(x, y);
          const rowEl = el?.closest('[data-row-key]') as HTMLElement | null;
          if (!rowEl) return null;
          const k = Number(rowEl.dataset.rowKey);
          return Number.isNaN(k) ? null : k;
        },
        isSameTarget: (a, b) => a === b,
        onMove: (endKey) => {
          const minKey = Math.min(key, endKey);
          const maxKey = Math.max(key, endKey);
          const next = new Set<number>();
          for (let k = minKey; k <= maxKey; k += 1) next.add(k);
          store.set(selectedKeysAtom, next);
        },
        onCommit: (endKey) => store.set(lastClickedKeyAtom, endKey),
        abortRef: dragAbortRef,
      });
    },
    [store, dragAbortRef],
  );

  return { handleRowMouseDown };
}

/**
 * useSelectionEdges — For each selected row, derives whether it's the top
 * and/or bottom edge of a contiguous selection run. RecordRow uses this to
 * decide whether to draw the row-level selection borders.
 */
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

export function useCellDrag(state: ReturnType<typeof useSelectionState>) {
  const { store, visibleCols, dragAbortRef, scheduleCursorText } = state;

  const handleCellMouseDown = useCallback(
    (rowKey: number, col: ViewerColumn, event: React.MouseEvent) => {
      if (col === ViewerColumn.ColorDot) return;
      event.stopPropagation();

      // Right-click (or macOS Ctrl+click): preserve selection for the context menu
      const isContextClick =
        event.button === 2 || (event.button === 0 && event.ctrlKey && !event.metaKey && !event.shiftKey);
      if (isContextClick) return;

      if (event.shiftKey || event.metaKey || event.ctrlKey) {
        const anchor = store.get(anchorCellAtom);
        if (event.shiftKey && anchor !== null) {
          // Spreadsheet behavior: Shift+click replaces the selection with a
          // rectangle from the anchor to the click target. Prior selection
          // (including disjoint cells from Cmd+click) is discarded.
          store.set(selectedCellsAtom, computeCellRange(anchor, { rowKey, col }, visibleCols));
        } else if (event.metaKey || event.ctrlKey) {
          const next = new Map(store.get(selectedCellsAtom));
          const newCols = new Set(next.get(rowKey) ?? []);
          const wasSelected = newCols.has(col);
          if (wasSelected) {
            newCols.delete(col);
          } else {
            newCols.add(col);
          }
          if (newCols.size === 0) {
            next.delete(rowKey);
          } else {
            next.set(rowKey, newCols);
          }
          store.set(selectedCellsAtom, next);

          // Spreadsheet anchor invariant: anchor must point to a selected cell
          // (or be null when the selection is empty).
          if (!wasSelected) {
            store.set(anchorCellAtom, { rowKey, col });
          } else if (anchor?.rowKey === rowKey && anchor?.col === col) {
            store.set(anchorCellAtom, nextSelectedCellInReadingOrder(next, { rowKey, col }, visibleCols));
          }
        } else {
          selectSingleCell(store, { rowKey, col });
        }

        clearRowSelection(store);
        store.set(isTextSelectModeAtom, true);
        store.set(isCursorTextAtom, false);
        scheduleCursorText();
        window.getSelection()?.removeAllRanges();
        return;
      }

      // In text-select mode on a selected cell, let the browser handle it
      if (store.get(isTextSelectModeAtom) && store.get(selectedCellsAtom).get(rowKey)?.has(col)) {
        return;
      }

      const start = { rowKey, col };
      // Set anchor at drag-start (not drag-end) so a later Shift+click extends
      // from where the user began the selection.
      selectSingleCell(store, start);
      clearRowSelection(store);
      store.set(isTextSelectModeAtom, false);
      store.set(isCursorTextAtom, false);

      startDocumentDrag<{ rowKey: number; col: ViewerColumn }>({
        initialTarget: start,
        resolveTarget: (x, y) => {
          const el = document.elementFromPoint(x, y);
          const cellEl = el?.closest('[data-col-id]') as HTMLElement | null;
          const rowEl = el?.closest('[data-row-key]') as HTMLElement | null;
          if (!cellEl || !rowEl) return null;
          const endRowKey = Number(rowEl.dataset.rowKey);
          const endCol = cellEl.dataset.colId as ViewerColumn | undefined;
          if (Number.isNaN(endRowKey) || !endCol || endCol === ViewerColumn.ColorDot) return null;
          return { rowKey: endRowKey, col: endCol };
        },
        isSameTarget: (a, b) => a.rowKey === b.rowKey && a.col === b.col,
        onMove: (end) => store.set(selectedCellsAtom, computeCellRange(start, end, visibleCols)),
        onCommit: () => {
          store.set(isTextSelectModeAtom, true);
          scheduleCursorText();
        },
        abortRef: dragAbortRef,
      });
    },
    [store, visibleCols, dragAbortRef, scheduleCursorText],
  );

  return { handleCellMouseDown };
}

export function useSelectionKeyboard(
  state: ReturnType<typeof useSelectionState>,
  virtualizerRef: React.RefObject<LogViewerVirtualizer | null>,
) {
  const { store, visibleCols, clearSelection } = state;
  const [timezone] = useTimezone();
  const [timestampFormat] = useTimestampFormat();

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        // If Radix already handled Escape (e.g. closing a context menu), skip
        if (e.defaultPrevented) return;
        clearSelection();
        window.getSelection()?.removeAllRanges();
        return;
      }

      const isMod = e.metaKey || e.ctrlKey;
      if (isMod && e.key === 'c') {
        const nativeSel = window.getSelection();
        if (nativeSel && !nativeSel.isCollapsed) return;

        const cells = store.get(selectedCellsAtom);
        if (cells.size > 0) {
          e.preventDefault();
          const v = virtualizerRef.current;
          if (!v) return;
          const text = formatCellsForCopy(cells, visibleCols, (k) => v.getRecord(k), timezone, timestampFormat);
          navigator.clipboard.writeText(text);
          return;
        }

        // Copy selected rows as TSV
        const keys = store.get(selectedKeysAtom);
        if (keys.size > 0) {
          e.preventDefault();
          const v = virtualizerRef.current;
          if (!v) return;
          const sorted = [...keys].sort((a, b) => a - b);
          const records = sorted.map((k) => v.getRecord(k)).filter((r): r is LogRecord => r !== undefined);
          const text = formatRowsForCopy(records, visibleCols, timezone, timestampFormat);
          navigator.clipboard.writeText(text);
        }
      }

      if (isArrowKey(e.key)) {
        if (e.metaKey || e.ctrlKey || e.altKey) return;

        // Anchor is always a selected cell or null (the cmd+click branch
        // maintains this invariant), so it doubles as the keyboard cursor.
        const activeCell = store.get(anchorCellAtom);
        if (!activeCell) return;

        const v = virtualizerRef.current;
        if (!v) return;

        const nextCell = getCellInArrowDirection(activeCell, e.key, visibleCols, v);
        if (!nextCell) return;

        e.preventDefault();
        window.getSelection()?.removeAllRanges();
        selectSingleCell(store, nextCell);
        store.set(isTextSelectModeAtom, true);
        store.set(isCursorTextAtom, false);
      }
    };

    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [store, visibleCols, timezone, timestampFormat, clearSelection, virtualizerRef]);
}

export function useSelection(virtualizerRef: React.RefObject<LogViewerVirtualizer | null>) {
  const state = useSelectionState();
  const { handleRowMouseDown } = useRowDrag(state);
  const { handleCellMouseDown } = useCellDrag(state);
  useSelectionKeyboard(state, virtualizerRef);
  const { selectionTopKeys, selectionBottomKeys } = useSelectionEdges(state.selectedKeys);

  // Clean up drag listeners on unmount
  useEffect(
    () => () => {
      state.dragAbortRef.current?.abort();
    },
    [],
  );

  return {
    selectedKeys: state.selectedKeys,
    selectionTopKeys,
    selectionBottomKeys,
    selectedCells: state.selectedCells,
    anchorCell: state.anchorCell,
    isTextSelectMode: state.isTextSelectMode,
    isCursorText: state.isCursorText,
    handleRowMouseDown,
    handleCellMouseDown,
    resetSelection: state.clearSelection,
  };
}
