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

import { useAtomValue } from 'jotai';
import React, { memo, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';

import { Spinner } from '@kubetail/ui/elements/spinner';
import { stripAnsi } from 'fancy-ansi';
import { AnsiHtml } from 'fancy-ansi/react';

import { formatTimestamp, useTimestampFormat } from '@/lib/timestamp-format';
import { useTimezone } from '@/lib/timezone';
import { cn, cssEncode } from '@/lib/util';
import { LogViewer, useLogViewerState } from '@/components/widgets/log-viewer';
import type {
  LogRecord,
  LogViewerInitialPosition,
  LogViewerVirtualRow,
  LogViewerVirtualizer,
} from '@/components/widgets/log-viewer';

import { CellContextMenu } from './context-menu';
import { SelectionOverlay } from './selection-overlay';
import { hasMultipleSelectedCells, useSelection } from './selection';
import { PageContext, ViewerColumn } from './shared';
import { isFollowAtom, isWrapAtom, visibleColsAtom } from './state';

const DEFAULT_INITIAL_POSITION = { type: 'tail' } satisfies LogViewerInitialPosition;

const BATCH_SIZE_INITIAL = 300;
const BATCH_SIZE_REGULAR = 250;
const LOG_RECORD_ROW_HEIGHT = 24;
// Width of the leading Pos column. Used both to build the row's grid template
// and by SelectionOverlay to position cell rects — the two must match.
const POS_COL_WIDTH = 48;
const HAS_MORE_BEFORE_ROW_HEIGHT = 24;
const HAS_MORE_AFTER_ROW_HEIGHT = 24;
const IS_REFRESHING_ROW_HEIGHT = 24;
const HEADER_ROW_HEIGHT = 19;
const CELL_HORIZONTAL_PADDING_PX = 16;

/**
 * LoadingOverlay component
 */

const LoadingOverlay = () => (
  <div className="absolute inset-0 bg-muted opacity-85 flex items-center justify-center z-50">
    <div className="bg-background flex items-center space-x-4 p-3 border rounded-md">
      <div>Loading</div>
      <Spinner size="xs" />
    </div>
  </div>
);

/**
 * useStableInitialPosition - Custom hook for returning stable reference to initial position
 */

function useStableInitialPosition(): LogViewerInitialPosition {
  const { search } = useLocation();

  const [initialPosition] = useState(() => {
    const searchParams = new URLSearchParams(search);
    switch (searchParams.get('mode')) {
      case 'head':
        return { type: 'head' };
      case 'cursor': {
        const cursor = searchParams.get('cursor');
        if (cursor !== null) return { type: 'cursor', cursor };
        return DEFAULT_INITIAL_POSITION;
      }
      default:
        return DEFAULT_INITIAL_POSITION;
    }
  });

  return initialPosition;
}

/**
 * Custom hook for measuring column and row widths
 */

function newDefaultWidths() {
  return {
    maxRowWidth: 0,
    colWidths: new Map<ViewerColumn, number>(),
  };
}

function useMeasureWidths() {
  const [widths, setWidths] = useState(newDefaultWidths);
  const [triggerID, setTriggerID] = useState(0);

  const pendingRef = useRef(newDefaultWidths());
  const measuredRef = useRef(new WeakSet<Element>());
  // Tracks which fields changed since the last flush so we only swap the
  // affected reference. A new colWidths Map reference busts RecordRow's memo
  // for every row, so reusing it when only maxRowWidth changed avoids a
  // cascade of unrelated re-renders during scroll-driven measurement.
  const dirtyRef = useRef({ maxRowWidth: false, colWidths: false });

  const rafIDRef = useRef<number | null>(null);

  // Cleanup on unmount
  useEffect(
    () => () => {
      if (rafIDRef.current !== null) cancelAnimationFrame(rafIDRef.current);
    },
    [],
  );

  const flush = useCallback(() => {
    if (rafIDRef.current) return;
    rafIDRef.current = requestAnimationFrame(() => {
      rafIDRef.current = null;
      const pending = pendingRef.current;
      const dirty = dirtyRef.current;
      if (!dirty.maxRowWidth && !dirty.colWidths) return;
      setWidths((prev) => ({
        maxRowWidth: dirty.maxRowWidth ? pending.maxRowWidth : prev.maxRowWidth,
        colWidths: dirty.colWidths ? new Map(pending.colWidths) : prev.colWidths,
      }));
      dirty.maxRowWidth = false;
      dirty.colWidths = false;
    });
  }, []);

  const measureRowElement = useCallback(
    (el: HTMLDivElement | null) => {
      if (!el || measuredRef.current.has(el)) return;
      measuredRef.current.add(el);

      const prev = pendingRef.current.maxRowWidth;
      const next = Math.max(el.scrollWidth, prev);
      if (next !== prev) {
        pendingRef.current.maxRowWidth = next;
        dirtyRef.current.maxRowWidth = true;
        flush();
      }
    },
    [flush, triggerID],
  );

  const measureCellElement = useCallback(
    (el: HTMLDivElement | null) => {
      if (!el || measuredRef.current.has(el)) return;
      measuredRef.current.add(el);

      // Measure the inner content wrapper (an inline-block span), not the cell
      // itself. The cell's width is constrained by its grid track and our own
      // minWidth, so measuring it would either under-report (track clamps it)
      // or compound on each render (minWidth becomes the new measurement).
      // The inner span sizes to its content, independent of the cell's layout.
      const contentEl = el.firstElementChild as HTMLElement | null;
      if (!contentEl) return;

      const pendingColWidths = pendingRef.current.colWidths;
      const col = el.dataset.colId as ViewerColumn;
      const prev = pendingColWidths.get(col);
      const contentWidth = Math.ceil(contentEl.getBoundingClientRect().width);
      const next = Math.max(contentWidth + CELL_HORIZONTAL_PADDING_PX, prev ?? 0);
      if (next !== prev) {
        pendingColWidths.set(col, next);
        dirtyRef.current.colWidths = true;
        flush();
      }
    },
    [flush, triggerID],
  );

  const resetWidths = useCallback(() => {
    if (rafIDRef.current !== null) {
      cancelAnimationFrame(rafIDRef.current);
      rafIDRef.current = null;
    }
    pendingRef.current = newDefaultWidths();
    measuredRef.current = new WeakSet();
    dirtyRef.current = { maxRowWidth: false, colWidths: false };
    setWidths(newDefaultWidths);
    setTriggerID((id) => id + 1);
  }, []);

  return { widths, measureRowElement, measureCellElement, resetWidths };
}

/**
 * Custom hook for tracking message container width
 */

function useMessageContainerWidth(
  wrapperRef: React.RefObject<HTMLDivElement | null>,
  colWidths: Map<ViewerColumn, number>,
) {
  const wrap = useAtomValue(isWrapAtom);
  const visibleCols = useAtomValue(visibleColsAtom);
  const [messageContainerWidth, setMessageContainerWidth] = useState(400);

  useEffect(() => {
    const wrapperEl = wrapperRef.current;
    if (!wrapperEl) return;

    if (!wrap) return;

    const updateMessageContainerWidth = () => {
      const wrapperWidth = wrapperEl.clientWidth;
      let otherColsWidth = 0;
      colWidths.forEach((width, col) => {
        if (col !== ViewerColumn.Message && visibleCols.has(col)) {
          otherColsWidth += width;
        }
      });
      const newWidth = Math.max(100, wrapperWidth - otherColsWidth);
      setMessageContainerWidth(newWidth);
    };

    // Throttle resize observer callback
    let rafId: number | null = null;
    const throttledUpdate = () => {
      if (rafId !== null) return;
      rafId = requestAnimationFrame(() => {
        rafId = null;
        updateMessageContainerWidth();
      });
    };

    const resizeObserver = new ResizeObserver(throttledUpdate);
    resizeObserver.observe(wrapperEl);

    // Initial calculation
    updateMessageContainerWidth();

    return () => {
      if (rafId !== null) cancelAnimationFrame(rafId);
      resizeObserver.disconnect();
    };
  }, [colWidths, wrap, visibleCols]);

  return messageContainerWidth;
}

/**
 * HeaderRow component
 */

type HeaderRowProps = {
  scrollElRef: React.RefObject<HTMLDivElement | null>;
  gridTemplate: string;
  isLoading: boolean;
  maxRowWidth: number;
  colWidths: Map<ViewerColumn, number>;
  measureCellElement: (el: HTMLDivElement | null) => void;
};

const HeaderRow = ({
  scrollElRef,
  gridTemplate,
  isLoading,
  maxRowWidth,
  colWidths,
  measureCellElement,
}: HeaderRowProps) => {
  const visibleCols = useAtomValue(visibleColsAtom);
  const isWrap = useAtomValue(isWrapAtom);
  const headerScrollRef = useRef<HTMLDivElement>(null);

  // The body scroll area (overflow-auto) has a vertical scrollbar; this header
  // wrapper does not. That makes the body's client width narrower by the
  // scrollbar width, so it scrolls further right than the header. Reserve the
  // same gutter here so the two scroll in lockstep (and the muted spacer covers
  // the area above the body's scrollbar at the right extreme).
  const [scrollbarWidth, setScrollbarWidth] = useState(0);
  useEffect(() => {
    const el = scrollElRef.current;
    if (!el) return undefined;
    const measure = () =>
      setScrollbarWidth((prev) => {
        const next = el.offsetWidth - el.clientWidth;
        return prev !== next ? next : prev;
      });
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [isLoading]);

  // Sync horizontal scroll bidirectionally
  // Re-setup when isLoading becomes false to ensure listeners are attached after content loads
  useEffect(() => {
    if (isLoading) return; // Only setup after loading finishes

    const headerEl = headerScrollRef.current;
    const scrollEl = scrollElRef.current;
    if (!headerEl || !scrollEl) return;

    let isHeaderScrolling = false;
    let isContentScrolling = false;
    let contentScrollRaf: number | null = null;
    let headerScrollRaf: number | null = null;

    const handleContentScroll = () => {
      if (isHeaderScrolling) return;

      if (contentScrollRaf !== null) return;

      contentScrollRaf = requestAnimationFrame(() => {
        isContentScrolling = true;
        headerEl.scrollLeft = scrollEl.scrollLeft;
        contentScrollRaf = null;

        requestAnimationFrame(() => {
          isContentScrolling = false;
        });
      });
    };

    const handleHeaderScroll = () => {
      if (isContentScrolling) return;

      if (headerScrollRaf !== null) return;

      headerScrollRaf = requestAnimationFrame(() => {
        isHeaderScrolling = true;
        scrollEl.scrollLeft = headerEl.scrollLeft;
        headerScrollRaf = null;

        requestAnimationFrame(() => {
          isHeaderScrolling = false;
        });
      });
    };

    scrollEl.addEventListener('scroll', handleContentScroll, { passive: true });
    headerEl.addEventListener('scroll', handleHeaderScroll, { passive: true });

    return () => {
      if (contentScrollRaf !== null) cancelAnimationFrame(contentScrollRaf);
      if (headerScrollRaf !== null) cancelAnimationFrame(headerScrollRaf);
      scrollEl.removeEventListener('scroll', handleContentScroll);
      headerEl.removeEventListener('scroll', handleHeaderScroll);
    };
  }, [isLoading]);

  return (
    <div ref={headerScrollRef} className="w-full overflow-x-scroll no-scrollbar shrink-0 cursor-default">
      <div className="flex">
        <div
          className="grid shrink-0 border-y bg-muted [&>*:not(:last-child)]:border-r"
          style={{
            height: HEADER_ROW_HEIGHT,
            gridTemplateColumns: gridTemplate,
            minWidth: isWrap ? '100%' : maxRowWidth || '100%',
          }}
        >
          <div data-col-id="Pos" />
          {[...visibleCols].map((col) => {
            const minWidth = isWrap && col === ViewerColumn.Message ? undefined : colWidths.get(col);
            return (
              <div
                key={col}
                ref={measureCellElement}
                data-col-id={col}
                className="whitespace-nowrap uppercase px-2 flex items-center text-xs"
                style={minWidth ? { minWidth: `${minWidth}px` } : undefined}
              >
                {col !== ViewerColumn.ColorDot && <span className="inline-block">{col}</span>}
              </div>
            );
          })}
        </div>
        {scrollbarWidth > 0 && (
          <div
            aria-hidden
            className="shrink-0 border-y bg-muted"
            style={{ width: scrollbarWidth, height: HEADER_ROW_HEIGHT }}
          />
        )}
      </div>
    </div>
  );
};

/**
 * RecordRow component
 */

const getAttribute = (record: LogRecord, col: ViewerColumn, timezone: string, timestampFormat: string) => {
  switch (col) {
    case ViewerColumn.Timestamp:
      return formatTimestamp(record.timestamp, timezone, timestampFormat);
    case ViewerColumn.ColorDot: {
      const k = cssEncode(`${record.source.namespace}/${record.source.podName}/${record.source.containerName}`);
      const el = <div className="inline-block w-2 h-2 rounded-full" style={{ backgroundColor: `var(--${k}-color)` }} />;
      return el;
    }
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
      return <AnsiHtml text={record.message} />;
    default:
      throw new Error('not implemented');
  }
};

function selectionBoxShadow(isTop: boolean, isBottom: boolean): string | undefined {
  if (isTop && isBottom) return 'inset 0 1px 0 0 var(--ring), inset 0 -1px 0 0 var(--ring)';
  if (isTop) return 'inset 0 1px 0 0 var(--ring)';
  if (isBottom) return 'inset 0 -1px 0 0 var(--ring)';
  return undefined;
}

type SelectionBand = { top: number; height: number };

/**
 * computeSelectionBands — Groups the selected virtual rows into contiguous
 * pixel runs. The row-selection fill is painted as one continuous band per run
 * (rather than a translucent background on every row's cells) so adjacent rows
 * never leave a sub-pixel seam between their fills.
 */
function computeSelectionBands(rows: LogViewerVirtualRow[], selectedKeys: Set<number>): SelectionBand[] {
  const bands: SelectionBand[] = [];
  let start: LogViewerVirtualRow | null = null;
  let end: LogViewerVirtualRow | null = null;
  const flush = () => {
    if (start && end) bands.push({ top: start.start, height: end.start + end.size - start.start });
    start = null;
    end = null;
  };
  rows.forEach((row) => {
    if (!selectedKeys.has(row.key)) {
      flush();
      return;
    }
    const adjacent = end !== null && row.key === end.key + 1 && Math.abs(row.start - (end.start + end.size)) < 0.5;
    if (adjacent) {
      end = row;
    } else {
      flush();
      start = row;
      end = row;
    }
  });
  flush();
  return bands;
}

type RecordRowProps = {
  row: LogViewerVirtualRow;
  gridTemplate: string;
  visibleCols: Set<ViewerColumn>;
  timezone: string;
  timestampFormat: string;
  isWrap: boolean;
  isSelected: boolean;
  isSelectionTop: boolean;
  isSelectionBottom: boolean;
  maxRowWidth: number;
  colWidths: Map<ViewerColumn, number>;
  selectedCellCols: Set<ViewerColumn> | undefined;
  selectedCellColsAbove: Set<ViewerColumn> | undefined;
  selectedCellColsBelow: Set<ViewerColumn> | undefined;
  anchorCol: ViewerColumn | undefined;
  isCursorText: boolean;
  isCellTextSelectable: boolean;
  measureElement: (node: Element | null) => void;
  measureRowElement: (el: HTMLDivElement | null) => void;
  measureCellElement: (el: HTMLDivElement | null) => void;
  onRowMouseDown: (key: number, event: React.MouseEvent) => void;
  onCellMouseDown: (rowKey: number, col: ViewerColumn, event: React.MouseEvent) => void;
};

export const RecordRow = memo(
  ({
    row,
    gridTemplate,
    visibleCols,
    timezone,
    timestampFormat,
    isWrap,
    isSelected,
    isSelectionTop,
    isSelectionBottom,
    maxRowWidth,
    colWidths,
    selectedCellCols,
    selectedCellColsAbove,
    selectedCellColsBelow,
    anchorCol,
    isCursorText,
    isCellTextSelectable,
    measureElement,
    measureRowElement,
    measureCellElement,
    onRowMouseDown,
    onCellMouseDown,
  }: RecordRowProps) => {
    const els: React.ReactElement[] = [];

    // Pos column (always first, acts as row selector)
    els.push(
      <div
        key="__pos__"
        data-col-id="Pos"
        role="button"
        tabIndex={0}
        className={cn(
          !isSelected && row.index % 2 !== 0 && 'bg-muted',
          row.key === 0 && 'border-l-2 border-success font-extrabold pl-[7px]',
          row.key !== 0 && 'text-muted-foreground',
          'whitespace-nowrap tabular-nums text-[0.65rem] text-center pr-1.5 cursor-default select-none outline-none',
        )}
        onMouseDown={(e) => onRowMouseDown(row.key, e)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            onRowMouseDown(row.key, e as unknown as React.MouseEvent);
          }
        }}
      >
        {row.key !== 0 && <span className="text-muted-foreground text-[0.9rem]">{row.key > 0 ? '+' : '-'}</span>}
        {Math.abs(row.key)}
      </div>,
    );

    const colsArray = [...visibleCols];
    for (let i = 0; i < colsArray.length; i += 1) {
      const col = colsArray[i];
      const minWidth = isWrap && col === ViewerColumn.Message ? undefined : colWidths.get(col);
      const shouldWrap = isWrap && col === ViewerColumn.Message;
      const isTimestamp = col === ViewerColumn.Timestamp;
      const isCellSelected = selectedCellCols?.has(col) ?? false;
      const isColorDot = col === ViewerColumn.ColorDot;

      // Selected rows are filled by a continuous band behind the rows (see
      // computeSelectionBands), so cells paint no background of their own.
      let cellBg: string | false;
      if (isSelected) {
        cellBg = false;
      } else {
        cellBg = isTimestamp ? 'bg-input/80' : row.index % 2 !== 0 && 'bg-secondary';
      }

      const isNativeTextSelectable = isCellSelected && isCellTextSelectable;

      const cellClassName = cn(
        cellBg,
        'px-2',
        shouldWrap ? 'whitespace-pre-wrap wrap-break-word' : 'whitespace-nowrap',
        !isColorDot &&
          (isCellSelected && isCursorText ? 'cursor-text group-data-[mod-key]:cursor-default' : 'cursor-default'),
        'select-none',
      );

      const cellStyle: React.CSSProperties = {
        ...(minWidth && { minWidth: `${minWidth}px` }),
        ...(isNativeTextSelectable && {
          userSelect: 'text' as const,
          WebkitUserSelect: 'text' as const,
        }),
      };

      els.push(
        <CellContextMenu key={col} col={col} record={row.record} timezone={timezone} timestampFormat={timestampFormat}>
          <div
            ref={shouldWrap ? undefined : measureCellElement}
            data-col-id={col}
            role={isColorDot ? undefined : 'gridcell'}
            tabIndex={isColorDot ? undefined : 0}
            className={cellClassName}
            style={cellStyle}
            onMouseDown={isColorDot ? undefined : (e) => onCellMouseDown(row.key, col, e)}
            onKeyDown={
              isColorDot
                ? undefined
                : (e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      onCellMouseDown(row.key, col, e as unknown as React.MouseEvent);
                    }
                  }
            }
          >
            {shouldWrap ? (
              getAttribute(row.record, col, timezone, timestampFormat)
            ) : (
              <span className="inline-block">{getAttribute(row.record, col, timezone, timestampFormat)}</span>
            )}
          </div>
        </CellContextMenu>,
      );
    }

    return (
      <div
        ref={(el) => {
          measureElement(el);
          measureRowElement(el);
        }}
        data-index={row.index}
        data-row-key={row.key}
        role="row"
        aria-selected={isSelected}
        className="absolute top-0 left-0 grid leading-6 group"
        style={{
          gridTemplateColumns: gridTemplate,
          minWidth: isWrap ? '100%' : maxRowWidth || '100%',
          height: isWrap ? undefined : LOG_RECORD_ROW_HEIGHT,
          lineHeight: `${LOG_RECORD_ROW_HEIGHT}px`,
          transform: `translateY(${row.start}px)`,
          boxShadow: selectionBoxShadow(isSelectionTop, isSelectionBottom),
        }}
      >
        {els}
        {selectedCellCols && (
          <SelectionOverlay
            selectedCols={selectedCellCols}
            selectedColsAbove={selectedCellColsAbove}
            selectedColsBelow={selectedCellColsBelow}
            anchorCol={anchorCol}
            visibleCols={visibleCols}
            colWidths={colWidths}
            posColWidth={POS_COL_WIDTH}
            rowWidth={maxRowWidth}
          />
        )}
      </div>
    );
  },
  (prev, next) => {
    if (prev.row.record !== next.row.record) return false;
    if (prev.row.start !== next.row.start) return false;
    if (prev.gridTemplate !== next.gridTemplate) return false;
    if (prev.visibleCols !== next.visibleCols) return false;
    if (prev.timezone !== next.timezone) return false;
    if (prev.timestampFormat !== next.timestampFormat) return false;
    if (prev.isWrap !== next.isWrap) return false;
    if (prev.isSelected !== next.isSelected) return false;
    if (prev.isSelectionTop !== next.isSelectionTop) return false;
    if (prev.isSelectionBottom !== next.isSelectionBottom) return false;
    if (prev.maxRowWidth !== next.maxRowWidth) return false;
    if (prev.colWidths !== next.colWidths) return false;
    if (prev.selectedCellCols !== next.selectedCellCols) return false;
    if (prev.selectedCellColsAbove !== next.selectedCellColsAbove) return false;
    if (prev.selectedCellColsBelow !== next.selectedCellColsBelow) return false;
    if (prev.anchorCol !== next.anchorCol) return false;
    if (prev.isCursorText !== next.isCursorText && (prev.selectedCellCols || next.selectedCellCols)) return false;
    if (prev.isCellTextSelectable !== next.isCellTextSelectable && (prev.selectedCellCols || next.selectedCellCols))
      return false;
    return true;
  },
);

/**
 * Main component
 */

export const Main = () => {
  const initialPosition = useStableInitialPosition();

  const { logServerClient, logViewerRef } = useContext(PageContext);
  const { isLoading } = useLogViewerState(logViewerRef, [logServerClient]);

  const follow = useAtomValue(isFollowAtom);
  const wrap = useAtomValue(isWrapAtom);
  const [timezone] = useTimezone();
  const [timestampFormat] = useTimestampFormat();

  const wrapperRef = useRef<HTMLDivElement>(null);
  const scrollElRef = useRef<HTMLDivElement>(null);

  const { widths, measureRowElement, measureCellElement, resetWidths } = useMeasureWidths();

  const { maxRowWidth, colWidths } = widths;
  const messageContainerWidth = useMessageContainerWidth(wrapperRef, colWidths);

  const visibleCols = useAtomValue(visibleColsAtom);
  const virtualizerRef = useRef<LogViewerVirtualizer | null>(null);

  const {
    selectedKeys,
    selectionTopKeys,
    selectionBottomKeys,
    selectedCells,
    anchorCell,
    isTextSelectMode,
    isCursorText,
    handleRowMouseDown,
    handleCellMouseDown,
    resetSelection,
  } = useSelection(virtualizerRef);

  // Generate grid template
  const gridTemplate = useMemo(
    () =>
      // Pos column + visible columns. Message uses `1fr` to fill remaining
      // width; the rest are content-sized. Pos width must match POS_COL_WIDTH
      // because SelectionOverlay uses it to position cell rects.
      `${POS_COL_WIDTH}px ${[...visibleCols].map((col) => (col === ViewerColumn.Message ? '1fr' : 'auto')).join(' ')}`,
    [visibleCols],
  );

  const isMultiCellSelection = hasMultipleSelectedCells(selectedCells);

  // Reset column widths and selection when loading new data
  useEffect(() => {
    if (isLoading) {
      resetWidths();
      resetSelection();
    }
  }, [isLoading]);

  // Re-measure columns when the timestamp format changes — the new string
  // length is usually different, so cached widths would leave the column
  // truncated or over-wide.
  const didMountRef = useRef(false);
  useEffect(() => {
    if (!didMountRef.current) {
      didMountRef.current = true;
      return;
    }
    resetWidths();
  }, [timestampFormat]);

  const estimateRowHeight = useCallback(
    (record: LogRecord) => {
      if (!wrap) return LOG_RECORD_ROW_HEIGHT;

      // Estimate character width for monospace font
      // For text-xs (12px) monospace, characters are roughly 7.2px wide
      const CHAR_WIDTH = 7.2;

      // Calculate text width using visible characters only (strip ANSI codes)
      const textWidth = stripAnsi(record.message).length * CHAR_WIDTH;

      // Calculate number of wrapped lines needed
      // Add 1 to be conservative and avoid underestimating
      const numLines = Math.max(1, Math.ceil(textWidth / messageContainerWidth));

      return numLines * LOG_RECORD_ROW_HEIGHT;
    },
    [wrap, messageContainerWidth],
  );

  const measureElement = useMemo(() => {
    if (!wrap) return undefined;
    return (el: Element) => el.getBoundingClientRect().height;
  }, [wrap]);

  // Re-measure when wrap changes
  useEffect(() => {
    logViewerRef.current?.measure();
  }, [wrap]);

  // Track Ctrl/Cmd held state via data attribute (no re-renders)
  useEffect(() => {
    const el = wrapperRef.current;
    if (!el) return;
    const onKey = (e: KeyboardEvent) => {
      el.toggleAttribute('data-mod-key', e.metaKey || e.ctrlKey);
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('keyup', onKey);
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('keyup', onKey);
    };
  }, []);

  return (
    <div ref={wrapperRef} className="group relative h-full w-full flex flex-col">
      {isLoading && <LoadingOverlay />}
      <HeaderRow
        scrollElRef={scrollElRef}
        gridTemplate={gridTemplate}
        isLoading={isLoading}
        maxRowWidth={maxRowWidth}
        colWidths={colWidths}
        measureCellElement={measureCellElement}
      />
      {logServerClient && (
        <LogViewer
          ref={logViewerRef}
          scrollElRef={scrollElRef}
          className="relative grow w-full font-mono text-xs leading-6"
          client={logServerClient}
          initialPosition={initialPosition}
          estimateRowHeight={estimateRowHeight}
          follow={follow}
          hasMoreBeforeRowHeight={HAS_MORE_BEFORE_ROW_HEIGHT}
          hasMoreAfterRowHeight={HAS_MORE_AFTER_ROW_HEIGHT}
          isRefreshingRowHeight={IS_REFRESHING_ROW_HEIGHT}
          batchSizeInitial={BATCH_SIZE_INITIAL}
          batchSizeRegular={BATCH_SIZE_REGULAR}
          measureElement={measureElement}
        >
          {(virtualizer) => {
            virtualizerRef.current = virtualizer;
            const virtualRows = virtualizer.getVirtualRows();

            return (
              <div
                className="relative"
                style={{ height: virtualizer.getTotalSize(), minWidth: wrap ? undefined : maxRowWidth || '100%' }}
              >
                {virtualizer.hasMoreBefore && (
                  <div
                    className="absolute top-0 left-0 text-gray-500"
                    style={{
                      height: `${virtualizer.hasMoreBeforeRowHeight}px`,
                      lineHeight: `${virtualizer.hasMoreBeforeRowHeight}px`,
                    }}
                  >
                    Loading...
                  </div>
                )}
                {computeSelectionBands(virtualRows, selectedKeys).map((band) => (
                  <div
                    key={band.top}
                    aria-hidden
                    className="pointer-events-none absolute inset-x-0"
                    style={{ top: band.top, height: band.height, backgroundColor: 'var(--selection-band)' }}
                  />
                ))}
                {virtualRows.map((virtualRow) => (
                  <RecordRow
                    key={virtualRow.key}
                    row={virtualRow}
                    measureElement={virtualizer.measureElement}
                    gridTemplate={gridTemplate}
                    visibleCols={visibleCols}
                    timezone={timezone}
                    timestampFormat={timestampFormat}
                    isWrap={wrap}
                    isSelected={selectedKeys.has(virtualRow.key)}
                    isSelectionTop={selectionTopKeys.has(virtualRow.key)}
                    isSelectionBottom={selectionBottomKeys.has(virtualRow.key)}
                    maxRowWidth={maxRowWidth}
                    colWidths={colWidths}
                    selectedCellCols={selectedCells.get(virtualRow.key)}
                    selectedCellColsAbove={selectedCells.get(virtualRow.key - 1)}
                    selectedCellColsBelow={selectedCells.get(virtualRow.key + 1)}
                    anchorCol={
                      isMultiCellSelection && anchorCell?.rowKey === virtualRow.key ? anchorCell.col : undefined
                    }
                    isCursorText={isCursorText}
                    isCellTextSelectable={isTextSelectMode && selectedCells.has(virtualRow.key)}
                    measureRowElement={measureRowElement}
                    measureCellElement={measureCellElement}
                    onRowMouseDown={handleRowMouseDown}
                    onCellMouseDown={handleCellMouseDown}
                  />
                ))}
                {virtualizer.hasMoreAfter && (
                  <div
                    className="absolute bottom-0 left-0 text-gray-500"
                    style={{
                      height: `${virtualizer.hasMoreAfterRowHeight}px`,
                      lineHeight: `${virtualizer.hasMoreAfterRowHeight}px`,
                    }}
                  >
                    Loading...
                  </div>
                )}
                {virtualizer.isRefreshing && (
                  <div
                    className="absolute bottom-0 left-0 text-gray-500"
                    style={{
                      height: `${virtualizer.isRefreshingRowHeight}px`,
                      lineHeight: `${virtualizer.isRefreshingRowHeight}px`,
                    }}
                  >
                    Refreshing...
                  </div>
                )}
              </div>
            );
          }}
        </LogViewer>
      )}
    </div>
  );
};
