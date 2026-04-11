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
import { isTextSelectModeAtom, lastClickedKeyAtom, selectedCellAtom, selectedKeysAtom, visibleColsAtom } from './state';

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
 * formatRowsForCopy - Formats an array of log records as copyable text.
 * Columns are tab-separated, rows are newline-separated. ColorDot is skipped.
 */
export function formatRowsForCopy(records: LogRecord[], visibleCols: Set<ViewerColumn>): string {
  return records
    .map((record) => {
      const parts: string[] = [];
      visibleCols.forEach((col) => {
        if (col === ViewerColumn.ColorDot) return;
        parts.push(getPlainAttribute(record, col));
      });
      return parts.join('\t');
    })
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
  const [selectedCell, setSelectedCell] = useAtom(selectedCellAtom);
  const [isTextSelectMode, setIsTextSelectMode] = useAtom(isTextSelectModeAtom);

  // Refs to avoid stale closures in callbacks
  const selectedKeysRef = useRef(selectedKeys);
  const lastClickedKeyRef = useRef(lastClickedKey);
  const selectedCellRef = useRef(selectedCell);

  useEffect(() => {
    selectedKeysRef.current = selectedKeys;
    lastClickedKeyRef.current = lastClickedKey;
    selectedCellRef.current = selectedCell;
  }, [selectedKeys, lastClickedKey, selectedCell]);

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
    setSelectedCell(null);
    setIsTextSelectMode(false);
  }, [setSelectedKeys, setLastClickedKey, setSelectedCell, setIsTextSelectMode]);

  // Row click handler (Pos column)
  const handleRowClick = useCallback(
    (key: number, event: React.MouseEvent) => {
      const next = computeSelection({
        prev: selectedKeysRef.current,
        clickedKey: key,
        shiftKey: event.shiftKey,
        metaOrCtrlKey: event.metaKey || event.ctrlKey,
        lastClickedKey: lastClickedKeyRef.current,
      });
      setSelectedKeys(next);
      setLastClickedKey(key);
      setSelectedCell(null);
      setIsTextSelectMode(false);
    },
    [setSelectedKeys, setLastClickedKey, setSelectedCell, setIsTextSelectMode],
  );

  // Cell click handler (data cells)
  const handleCellClick = useCallback(
    (rowKey: number, col: ViewerColumn, event: React.MouseEvent) => {
      if (col === ViewerColumn.ColorDot) return;
      event.stopPropagation();
      const hasTextSelection = !window.getSelection()?.isCollapsed;
      setSelectedCell({ rowKey, col });
      setSelectedKeys(new Set());
      setLastClickedKey(null);
      setIsTextSelectMode(hasTextSelection);
    },
    [setSelectedCell, setSelectedKeys, setLastClickedKey, setIsTextSelectMode],
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

        // Copy selected cell text
        const cell = selectedCellRef.current;
        if (cell) {
          e.preventDefault();
          const v = virtualizerRef.current;
          if (!v) return;
          const record = v.getRecord(cell.rowKey);
          if (record) {
            navigator.clipboard.writeText(getPlainAttribute(record, cell.col));
          }
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

  return {
    selectedKeys,
    selectionTopKeys,
    selectionBottomKeys,
    selectedCell,
    isTextSelectMode,
    handleRowClick,
    handleCellClick,
    resetSelection: clearSelection,
  };
}
