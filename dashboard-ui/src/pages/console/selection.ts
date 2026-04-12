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

import { format, toZonedTime } from 'date-fns-tz';
import { useAtom, useAtomValue } from 'jotai';
import { useCallback, useEffect, useMemo, useRef } from 'react';

import { stripAnsi } from 'fancy-ansi';

import type { LogRecord, LogViewerVirtualizer } from '@/components/widgets/log-viewer';

import { ViewerColumn } from './shared';
import {
  isTextSelectModeAtom,
  lastClickedKeyAtom,
  selectedCellsAtom,
  selectedKeysAtom,
  visibleColsAtom,
} from './state';

/**
 * getPlainAttribute - Returns a plain text string for a given log record column.
 * This is the plain-text counterpart of getAttribute() in main.tsx (which returns JSX).
 */
export function getPlainAttribute(record: LogRecord, col: ViewerColumn): string {
  switch (col) {
    case ViewerColumn.Timestamp: {
      const tsWithTZ = toZonedTime(record.timestamp, 'UTC');
      return format(tsWithTZ, 'LLL dd, y HH:mm:ss.SSS', { timeZone: 'UTC' });
    }
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
function formatRow(record: LogRecord, visibleCols: Set<ViewerColumn>, filter?: Set<ViewerColumn>): string {
  const parts: string[] = [];
  visibleCols.forEach((col) => {
    if (col === ViewerColumn.ColorDot) return;
    if (filter && !filter.has(col)) return;
    parts.push(getPlainAttribute(record, col));
  });
  return parts.join('\t');
}

/**
 * formatRowsForCopy - Formats an array of log records as copyable text.
 * Columns are tab-separated, rows are newline-separated. ColorDot is skipped.
 */
export function formatRowsForCopy(records: LogRecord[], visibleCols: Set<ViewerColumn>): string {
  return records.map((record) => formatRow(record, visibleCols)).join('\n');
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
): string {
  return [...selectedCells.keys()]
    .sort((a, b) => a - b)
    .map((rowKey) => {
      const record = getRecord(rowKey);
      if (!record) return null;
      const text = formatRow(record, visibleCols, selectedCells.get(rowKey));
      return text || null;
    })
    .filter((line): line is string => line !== null)
    .join('\n');
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

/**
 * useSelection - Hook that manages row selection, cell selection, text-select mode,
 * click handling, boundary computation, and keyboard shortcuts.
 */
export function useSelection(virtualizerRef: React.RefObject<LogViewerVirtualizer | null>) {
  const visibleCols = useAtomValue(visibleColsAtom);
  const [selectedKeys, setSelectedKeys] = useAtom(selectedKeysAtom);
  const [lastClickedKey, setLastClickedKey] = useAtom(lastClickedKeyAtom);
  const [selectedCells, setSelectedCells] = useAtom(selectedCellsAtom);
  const [isTextSelectMode, setIsTextSelectMode] = useAtom(isTextSelectModeAtom);

  // Refs to avoid stale closures in callbacks
  const selectedKeysRef = useRef(selectedKeys);
  const lastClickedKeyRef = useRef(lastClickedKey);
  const selectedCellsRef = useRef(selectedCells);

  // Drag state refs (dragStartKeyRef !== null means a drag is active)
  const dragStartKeyRef = useRef<number | null>(null);
  const dragEndKeyRef = useRef<number | null>(null);
  const dragAbortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    selectedKeysRef.current = selectedKeys;
    lastClickedKeyRef.current = lastClickedKey;
    selectedCellsRef.current = selectedCells;
  }, [selectedKeys, lastClickedKey, selectedCells]);

  // Pre-compute selection boundary sets (keys are sequential integers)
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

  // Clear all selection state
  const clearSelection = useCallback(() => {
    setSelectedKeys(new Set());
    setLastClickedKey(null);
    setSelectedCells(new Map());
    setIsTextSelectMode(false);
  }, [setSelectedKeys, setLastClickedKey, setSelectedCells, setIsTextSelectMode]);

  // Row mousedown handler (Pos column) — supports click and drag-to-select
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
        setIsTextSelectMode(false);
        return;
      }

      // Start drag
      dragStartKeyRef.current = key;
      dragEndKeyRef.current = key;
      setSelectedKeys(new Set([key]));
      if (selectedCellsRef.current.size > 0) setSelectedCells(new Map());
      setIsTextSelectMode(false);

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
    [setSelectedKeys, setLastClickedKey, setSelectedCells, setIsTextSelectMode],
  );

  // Cell click handler (data cells) — supports Cmd/Ctrl+click for multi-cell selection
  const handleCellClick = useCallback(
    (rowKey: number, col: ViewerColumn, event: React.MouseEvent) => {
      if (col === ViewerColumn.ColorDot) return;
      event.stopPropagation();
      const hasTextSelection = !window.getSelection()?.isCollapsed;

      if (event.metaKey || event.ctrlKey) {
        // Toggle cell in/out of selection
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
      } else {
        setSelectedCells(new Map([[rowKey, new Set([col])]]));
      }

      if (selectedKeysRef.current.size > 0) setSelectedKeys(new Set());
      setLastClickedKey(null);
      setIsTextSelectMode(hasTextSelection);
    },
    [setSelectedCells, setSelectedKeys, setLastClickedKey, setIsTextSelectMode],
  );

  // Keyboard shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        clearSelection();
        window.getSelection()?.removeAllRanges();
        return;
      }

      const isMod = e.metaKey || e.ctrlKey;
      if (isMod && e.key === 'c') {
        // If user has native text selection (from text-select mode), let browser handle it
        const nativeSel = window.getSelection();
        if (nativeSel && !nativeSel.isCollapsed) return;

        // Copy selected cells
        const cells = selectedCellsRef.current;
        if (cells.size > 0) {
          e.preventDefault();
          const v = virtualizerRef.current;
          if (!v) return;
          const text = formatCellsForCopy(cells, visibleCols, (k) => v.getRecord(k));
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
          const text = formatRowsForCopy(records, visibleCols);
          navigator.clipboard.writeText(text);
        }
      }
    };

    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [visibleCols, clearSelection]);

  // Clean up drag listeners on unmount
  useEffect(
    () => () => {
      dragAbortRef.current?.abort();
    },
    [],
  );

  return {
    selectedKeys,
    selectionTopKeys,
    selectionBottomKeys,
    selectedCells,
    isTextSelectMode,
    handleRowMouseDown,
    handleCellClick,
    resetSelection: clearSelection,
  };
}
