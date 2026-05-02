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

import { useAtom, useAtomValue } from 'jotai';
import { useCallback, useEffect, useMemo, useRef } from 'react';

import { stripAnsi } from 'fancy-ansi';

import { TimestampFormat, formatTimestamp, useTimestampFormat } from '@/lib/timestamp-format';
import { useTimezone } from '@/lib/timezone';

import type { LogRecord, LogViewerVirtualizer } from '@/components/widgets/log-viewer';

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

type SelectableViewerColumn = Exclude<ViewerColumn, ViewerColumn.ColorDot>;

function isSelectableViewerColumn(col: ViewerColumn): col is SelectableViewerColumn {
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
 * computeCellRange - Pure function that computes a rectangular cell selection between two cells.
 * If either anchor or target is ColorDot, returns just the target cell.
 */
export function computeCellRange(
  anchor: { rowKey: number; col: ViewerColumn },
  target: { rowKey: number; col: ViewerColumn },
  visibleCols: Set<ViewerColumn>,
): Map<number, Set<ViewerColumn>> {
  const cols: ViewerColumn[] = [...visibleCols].filter((c) => c !== ViewerColumn.ColorDot);
  const anchorIdx = cols.indexOf(anchor.col);
  const targetIdx = cols.indexOf(target.col);

  if (anchorIdx === -1 || targetIdx === -1) {
    return new Map([[target.rowKey, new Set([target.col])]]);
  }

  const minRow = Math.min(anchor.rowKey, target.rowKey);
  const maxRow = Math.max(anchor.rowKey, target.rowKey);
  const minCol = Math.min(anchorIdx, targetIdx);
  const maxCol = Math.max(anchorIdx, targetIdx);

  const result = new Map<number, Set<ViewerColumn>>();
  for (let r = minRow; r <= maxRow; r += 1) {
    const colSet = new Set<ViewerColumn>();
    for (let c = minCol; c <= maxCol; c += 1) {
      colSet.add(cols[c]);
    }
    result.set(r, colSet);
  }
  return result;
}

function getActiveSelectedCell(
  selectedCells: Map<number, Set<ViewerColumn>>,
  lastClickedCell: { rowKey: number; col: ViewerColumn } | null,
  visibleCols: Set<ViewerColumn>,
): { rowKey: number; col: ViewerColumn } | null {
  if (lastClickedCell && selectedCells.get(lastClickedCell.rowKey)?.has(lastClickedCell.col)) {
    return lastClickedCell;
  }

  const cols = [...visibleCols].filter(isSelectableViewerColumn);
  const rowKey = [...selectedCells.keys()].sort((a, b) => a - b)[0];
  if (rowKey === undefined) return null;

  const selectedCols = selectedCells.get(rowKey);
  const col = cols.find((c) => selectedCols?.has(c));
  return col ? { rowKey, col } : null;
}

function getAdjacentSelectedCell(
  activeCell: { rowKey: number; col: ViewerColumn },
  key: string,
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

export function useSelectionState() {
  const visibleCols = useAtomValue(visibleColsAtom);
  const [selectedKeys, setSelectedKeys] = useAtom(selectedKeysAtom);
  const [lastClickedKey, setLastClickedKey] = useAtom(lastClickedKeyAtom);
  const [selectedCells, setSelectedCells] = useAtom(selectedCellsAtom);
  const [lastClickedCell, setLastClickedCell] = useAtom(lastClickedCellAtom);
  const [isTextSelectMode, setIsTextSelectMode] = useAtom(isTextSelectModeAtom);
  const [isCursorText, setIsCursorText] = useAtom(isCursorTextAtom);

  const selectedKeysRef = useRef(selectedKeys);
  const lastClickedKeyRef = useRef(lastClickedKey);
  const selectedCellsRef = useRef(selectedCells);
  const lastClickedCellRef = useRef(lastClickedCell);
  const isTextSelectModeRef = useRef(isTextSelectMode);
  const dragAbortRef = useRef<AbortController | null>(null);
  const cursorTextPendingRef = useRef(false);
  const scheduleCursorText = useCallback(() => {
    if (cursorTextPendingRef.current) return;
    cursorTextPendingRef.current = true;
    document.addEventListener(
      'mousemove',
      () => {
        cursorTextPendingRef.current = false;
        setIsCursorText(true);
      },
      { once: true },
    );
  }, [setIsCursorText]);

  useEffect(() => {
    selectedKeysRef.current = selectedKeys;
    lastClickedKeyRef.current = lastClickedKey;
    selectedCellsRef.current = selectedCells;
    lastClickedCellRef.current = lastClickedCell;
    isTextSelectModeRef.current = isTextSelectMode;
  }, [selectedKeys, lastClickedKey, selectedCells, lastClickedCell, isTextSelectMode]);

  const clearSelection = useCallback(() => {
    setSelectedKeys(new Set());
    setLastClickedKey(null);
    setSelectedCells(new Map());
    setLastClickedCell(null);
    setIsTextSelectMode(false);
    setIsCursorText(false);
  }, [setSelectedKeys, setLastClickedKey, setSelectedCells, setLastClickedCell, setIsTextSelectMode, setIsCursorText]);

  return {
    visibleCols,
    selectedKeys,
    setSelectedKeys,
    setLastClickedKey,
    selectedCells,
    setSelectedCells,
    setLastClickedCell,
    isTextSelectMode,
    setIsTextSelectMode,
    isCursorText,
    setIsCursorText,
    selectedKeysRef,
    lastClickedKeyRef,
    selectedCellsRef,
    lastClickedCellRef,
    isTextSelectModeRef,
    dragAbortRef,
    clearSelection,
    scheduleCursorText,
  };
}

export function useRowDrag(state: ReturnType<typeof useSelectionState>) {
  const {
    selectedKeys,
    setSelectedKeys,
    setLastClickedKey,
    setSelectedCells,
    setLastClickedCell,
    setIsTextSelectMode,
    setIsCursorText,
    selectedKeysRef,
    lastClickedKeyRef,
    selectedCellsRef,
    dragAbortRef,
  } = state;

  const dragStartKeyRef = useRef<number | null>(null);
  const dragEndKeyRef = useRef<number | null>(null);

  const { selectionTopKeys, selectionBottomKeys } = useMemo(() => {
    if (selectedKeys.size === 0) return { selectionTopKeys: selectedKeys, selectionBottomKeys: selectedKeys };
    const top = new Set<number>();
    const bottom = new Set<number>();
    selectedKeys.forEach((k) => {
      if (!selectedKeys.has(k - 1)) top.add(k);
      if (!selectedKeys.has(k + 1)) bottom.add(k);
    });
    return { selectionTopKeys: top, selectionBottomKeys: bottom };
  }, [selectedKeys]);

  const handleRowMouseDown = useCallback(
    (key: number, event: React.MouseEvent) => {
      // Modifier clicks don't start a drag
      if (event.shiftKey || event.metaKey || event.ctrlKey) {
        const next = computeSelection({
          prev: selectedKeysRef.current,
          clickedKey: key,
          shiftKey: event.shiftKey,
          metaOrCtrlKey: event.metaKey || event.ctrlKey,
          lastClickedKey: lastClickedKeyRef.current,
        });
        setSelectedKeys(next);
        setLastClickedKey(key);
        if (selectedCellsRef.current.size > 0) setSelectedCells(new Map());
        setLastClickedCell(null);
        setIsTextSelectMode(false);
        setIsCursorText(false);
        return;
      }

      dragStartKeyRef.current = key;
      dragEndKeyRef.current = key;
      setSelectedKeys(new Set([key]));
      if (selectedCellsRef.current.size > 0) setSelectedCells(new Map());
      setLastClickedCell(null);
      setIsTextSelectMode(false);
      setIsCursorText(false);

      let rafId: number | null = null;
      let pendingX = 0;
      let pendingY = 0;

      const processMove = () => {
        rafId = null;
        if (dragStartKeyRef.current === null) return;
        const el = document.elementFromPoint(pendingX, pendingY);
        const rowEl = el?.closest('[data-row-key]') as HTMLElement | null;
        if (!rowEl) return;
        const endKey = Number(rowEl.dataset.rowKey);
        if (Number.isNaN(endKey) || endKey === dragEndKeyRef.current) return;
        dragEndKeyRef.current = endKey;
        const minKey = Math.min(dragStartKeyRef.current, endKey);
        const maxKey = Math.max(dragStartKeyRef.current, endKey);
        const next = new Set<number>();
        for (let k = minKey; k <= maxKey; k += 1) next.add(k);
        setSelectedKeys(next);
      };

      const onMouseMove = (e: MouseEvent) => {
        if (dragStartKeyRef.current === null) return;
        e.preventDefault();
        pendingX = e.clientX;
        pendingY = e.clientY;
        if (rafId !== null) return;
        rafId = requestAnimationFrame(processMove);
      };

      const onMouseUp = (e: MouseEvent) => {
        if (rafId !== null) {
          cancelAnimationFrame(rafId);
          rafId = null;
          pendingX = e.clientX;
          pendingY = e.clientY;
          processMove();
        }
        setLastClickedKey(dragEndKeyRef.current);
        dragStartKeyRef.current = null;
        dragEndKeyRef.current = null;
        dragAbortRef.current?.abort();
        dragAbortRef.current = null;
      };

      dragAbortRef.current?.abort();
      dragAbortRef.current = new AbortController();
      const { signal } = dragAbortRef.current;

      document.addEventListener('mousemove', onMouseMove, { signal });
      document.addEventListener('mouseup', onMouseUp, { signal });
    },
    [
      setSelectedKeys,
      setLastClickedKey,
      setSelectedCells,
      setLastClickedCell,
      setIsTextSelectMode,
      setIsCursorText,
      selectedKeysRef,
      lastClickedKeyRef,
      selectedCellsRef,
      dragAbortRef,
    ],
  );

  return { handleRowMouseDown, selectionTopKeys, selectionBottomKeys };
}

export function useCellDrag(state: ReturnType<typeof useSelectionState>) {
  const {
    visibleCols,
    setSelectedKeys,
    setLastClickedKey,
    setSelectedCells,
    setLastClickedCell,
    setIsTextSelectMode,
    setIsCursorText,
    selectedKeysRef,
    selectedCellsRef,
    lastClickedCellRef,
    isTextSelectModeRef,
    dragAbortRef,
    scheduleCursorText,
  } = state;

  const cellDragStartRef = useRef<{ rowKey: number; col: ViewerColumn } | null>(null);
  const cellDragEndRef = useRef<{ rowKey: number; col: ViewerColumn } | null>(null);

  const handleCellMouseDown = useCallback(
    (rowKey: number, col: ViewerColumn, event: React.MouseEvent) => {
      if (col === ViewerColumn.ColorDot) return;
      event.stopPropagation();

      // Right-click (or macOS Ctrl+click): preserve selection for the context menu
      const isContextClick =
        event.button === 2 || (event.button === 0 && event.ctrlKey && !event.metaKey && !event.shiftKey);
      if (isContextClick) return;

      if (event.shiftKey || event.metaKey || event.ctrlKey) {
        if (event.shiftKey && lastClickedCellRef.current !== null) {
          const range = computeCellRange(lastClickedCellRef.current, { rowKey, col }, visibleCols);
          const merged = new Map(selectedCellsRef.current);
          range.forEach((cols, rk) => {
            const existing = merged.get(rk);
            merged.set(rk, existing ? new Set([...existing, ...cols]) : cols);
          });
          setSelectedCells(merged);
        } else if (event.metaKey || event.ctrlKey) {
          const next = new Map(selectedCellsRef.current);
          const newCols = new Set(next.get(rowKey) ?? []);
          if (newCols.has(col)) {
            newCols.delete(col);
          } else {
            newCols.add(col);
          }
          if (newCols.size === 0) {
            next.delete(rowKey);
          } else {
            next.set(rowKey, newCols);
          }
          setSelectedCells(next);
          if (lastClickedCellRef.current?.rowKey !== rowKey || lastClickedCellRef.current?.col !== col) {
            setLastClickedCell({ rowKey, col });
          }
        } else {
          setSelectedCells(new Map([[rowKey, new Set([col])]]));
          setLastClickedCell({ rowKey, col });
        }

        if (selectedKeysRef.current.size > 0) setSelectedKeys(new Set());
        setLastClickedKey(null);
        setIsTextSelectMode(true);
        setIsCursorText(false);
        scheduleCursorText();
        window.getSelection()?.removeAllRanges();
        return;
      }

      // In text-select mode on a selected cell, let the browser handle it
      if (isTextSelectModeRef.current && selectedCellsRef.current.get(rowKey)?.has(col)) {
        return;
      }

      const start = { rowKey, col };
      cellDragStartRef.current = start;
      cellDragEndRef.current = start;
      setSelectedCells(new Map([[rowKey, new Set([col])]]));
      if (selectedKeysRef.current.size > 0) setSelectedKeys(new Set());
      setLastClickedKey(null);
      setIsTextSelectMode(false);
      setIsCursorText(false);

      let rafId: number | null = null;
      let pendingX = 0;
      let pendingY = 0;

      const processMove = () => {
        rafId = null;
        if (cellDragStartRef.current === null) return;
        const el = document.elementFromPoint(pendingX, pendingY);
        const cellEl = el?.closest('[data-col-id]') as HTMLElement | null;
        const rowEl = el?.closest('[data-row-key]') as HTMLElement | null;
        if (!cellEl || !rowEl) return;
        const endRowKey = Number(rowEl.dataset.rowKey);
        const endCol = cellEl.dataset.colId as ViewerColumn | undefined;
        if (Number.isNaN(endRowKey) || !endCol || endCol === ViewerColumn.ColorDot) return;
        const end = { rowKey: endRowKey, col: endCol };
        if (end.rowKey === cellDragEndRef.current?.rowKey && end.col === cellDragEndRef.current?.col) return;
        cellDragEndRef.current = end;
        setSelectedCells(computeCellRange(cellDragStartRef.current, end, visibleCols));
      };

      const onMouseMove = (e: MouseEvent) => {
        if (cellDragStartRef.current === null) return;
        e.preventDefault();
        pendingX = e.clientX;
        pendingY = e.clientY;
        if (rafId !== null) return;
        rafId = requestAnimationFrame(processMove);
      };

      const onMouseUp = (e: MouseEvent) => {
        if (rafId !== null) {
          cancelAnimationFrame(rafId);
          rafId = null;
          pendingX = e.clientX;
          pendingY = e.clientY;
          processMove();
        }
        const end = cellDragEndRef.current;
        if (end) {
          setLastClickedCell(end);
          setIsTextSelectMode(true);
        }
        cellDragStartRef.current = null;
        cellDragEndRef.current = null;
        dragAbortRef.current?.abort();
        dragAbortRef.current = null;
        scheduleCursorText();
      };

      dragAbortRef.current?.abort();
      dragAbortRef.current = new AbortController();
      const { signal } = dragAbortRef.current;

      document.addEventListener('mousemove', onMouseMove, { signal });
      document.addEventListener('mouseup', onMouseUp, { signal });
    },
    [
      visibleCols,
      setSelectedCells,
      setLastClickedCell,
      setSelectedKeys,
      setLastClickedKey,
      setIsTextSelectMode,
      setIsCursorText,
      selectedKeysRef,
      selectedCellsRef,
      lastClickedCellRef,
      isTextSelectModeRef,
      dragAbortRef,
      scheduleCursorText,
    ],
  );

  return { handleCellMouseDown };
}

export function useSelectionKeyboard(
  state: ReturnType<typeof useSelectionState>,
  virtualizerRef: React.RefObject<LogViewerVirtualizer | null>,
) {
  const {
    visibleCols,
    selectedKeysRef,
    selectedCellsRef,
    lastClickedCellRef,
    setSelectedCells,
    setLastClickedCell,
    setIsTextSelectMode,
    setIsCursorText,
    clearSelection,
  } = state;
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

        const cells = selectedCellsRef.current;
        if (cells.size > 0) {
          e.preventDefault();
          const v = virtualizerRef.current;
          if (!v) return;
          const text = formatCellsForCopy(cells, visibleCols, (k) => v.getRecord(k), timezone, timestampFormat);
          navigator.clipboard.writeText(text);
          return;
        }

        // Copy selected rows as TSV
        if (selectedKeysRef.current.size > 0) {
          e.preventDefault();
          const v = virtualizerRef.current;
          if (!v) return;
          const sorted = [...selectedKeysRef.current].sort((a, b) => a - b);
          const records = sorted.map((k) => v.getRecord(k)).filter((r): r is LogRecord => r !== undefined);
          const text = formatRowsForCopy(records, visibleCols, timezone, timestampFormat);
          navigator.clipboard.writeText(text);
        }
      }

      if (e.key === 'ArrowUp' || e.key === 'ArrowDown' || e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
        if (e.metaKey || e.ctrlKey || e.altKey) return;

        const cells = selectedCellsRef.current;
        if (cells.size === 0) return;

        const v = virtualizerRef.current;
        if (!v) return;

        const activeCell = getActiveSelectedCell(cells, lastClickedCellRef.current, visibleCols);
        if (!activeCell) return;

        const nextCell = getAdjacentSelectedCell(activeCell, e.key, visibleCols, v);
        if (!nextCell) return;

        e.preventDefault();
        window.getSelection()?.removeAllRanges();
        const nextCells = new Map([[nextCell.rowKey, new Set([nextCell.col])]]);
        selectedCellsRef.current = nextCells;
        lastClickedCellRef.current = nextCell;
        setSelectedCells(nextCells);
        setLastClickedCell(nextCell);
        setIsTextSelectMode(true);
        setIsCursorText(false);
      }
    };

    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [
    visibleCols,
    timezone,
    timestampFormat,
    clearSelection,
    selectedKeysRef,
    selectedCellsRef,
    lastClickedCellRef,
    setSelectedCells,
    setLastClickedCell,
    setIsTextSelectMode,
    setIsCursorText,
    virtualizerRef,
  ]);
}

export function useSelection(virtualizerRef: React.RefObject<LogViewerVirtualizer | null>) {
  const state = useSelectionState();
  const { handleRowMouseDown, selectionTopKeys, selectionBottomKeys } = useRowDrag(state);
  const { handleCellMouseDown } = useCellDrag(state);
  useSelectionKeyboard(state, virtualizerRef);

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
    isTextSelectMode: state.isTextSelectMode,
    isCursorText: state.isCursorText,
    handleRowMouseDown,
    handleCellMouseDown,
    resetSelection: state.clearSelection,
  };
}
