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
import { useAtomValue } from 'jotai';
import React, { memo, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';

import { Spinner } from '@kubetail/ui/elements/spinner';
import { stripAnsi } from 'fancy-ansi';
import { AnsiHtml } from 'fancy-ansi/react';

import { cn, cssEncode } from '@/lib/util';
import { LogViewer, useLogViewerState } from '@/components/widgets/log-viewer';
import type {
  LogRecord,
  LogViewerInitialPosition,
  LogViewerVirtualRow,
  LogViewerVirtualizer,
} from '@/components/widgets/log-viewer';

import { CellContextMenu } from './context-menu';
import { useSelection } from './selection';
import { PageContext, ViewerColumn } from './shared';
import { isFollowAtom, isWrapAtom, visibleColsAtom } from './state';

const DEFAULT_INITIAL_POSITION = { type: 'tail' } satisfies LogViewerInitialPosition;

const BATCH_SIZE_INITIAL = 300;
const BATCH_SIZE_REGULAR = 250;
const LOG_RECORD_ROW_HEIGHT = 24;
const HAS_MORE_BEFORE_ROW_HEIGHT = 24;
const HAS_MORE_AFTER_ROW_HEIGHT = 24;
const IS_REFRESHING_ROW_HEIGHT = 24;
const HEADER_ROW_HEIGHT = 19;

/**
 * LoadingOverlay component
 */

const LoadingOverlay = () => (
  <div className="absolute inset-0 bg-chrome-100 opacity-85 flex items-center justify-center z-50">
    <div className="bg-background flex items-center space-x-4 p-3 border border-chrome-200 rounded-md">
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
        return { type: 'head' } as LogViewerInitialPosition;
      case 'cursor': {
        const cursor = searchParams.get('cursor');
        if (cursor !== null) return { type: 'cursor', cursor } as LogViewerInitialPosition;
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
      setWidths({
        maxRowWidth: pending.maxRowWidth,
        colWidths: new Map(pending.colWidths),
      });
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
        flush();
      }
    },
    [flush, triggerID],
  );

  const measureCellElement = useCallback(
    (el: HTMLDivElement | null) => {
      if (!el || measuredRef.current.has(el)) return;
      measuredRef.current.add(el);

      const pendingColWidths = pendingRef.current.colWidths;
      const col = el.dataset.colId as ViewerColumn;
      const prev = pendingColWidths.get(col);
      const next = Math.max(Math.ceil(el.getBoundingClientRect().width), prev ?? 0);
      if (next !== prev) {
        pendingColWidths.set(col, next);
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
      <div
        className="grid border-b border-chrome-divider bg-chrome-200 *:border-r [&>*:not(:last-child)]:border-chrome-divider"
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
              {col !== ViewerColumn.ColorDot && col}
            </div>
          );
        })}
      </div>
    </div>
  );
};

/**
 * RecordRow component
 */

const getAttribute = (record: LogRecord, col: ViewerColumn) => {
  switch (col) {
    case ViewerColumn.Timestamp: {
      const tsWithTZ = toZonedTime(record.timestamp, 'UTC');
      return format(tsWithTZ, 'LLL dd, y HH:mm:ss.SSS', { timeZone: 'UTC' });
    }
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
  if (isTop && isBottom) return 'inset 0 1px 0 0 var(--color-blue-500), inset 0 -1px 0 0 var(--color-blue-500)';
  if (isTop) return 'inset 0 1px 0 0 var(--color-blue-500)';
  if (isBottom) return 'inset 0 -1px 0 0 var(--color-blue-500)';
  return undefined;
}

type RecordRowProps = {
  row: LogViewerVirtualRow;
  gridTemplate: string;
  visibleCols: Set<ViewerColumn>;
  isWrap: boolean;
  isSelected: boolean;
  isSelectionTop: boolean;
  isSelectionBottom: boolean;
  maxRowWidth: number;
  colWidths: Map<ViewerColumn, number>;
  selectedCellCol: ViewerColumn | null;
  isCellTextSelectable: boolean;
  measureElement: (node: Element | null) => void;
  measureRowElement: (el: HTMLDivElement | null) => void;
  measureCellElement: (el: HTMLDivElement | null) => void;
  onRowMouseDown: (key: number, event: React.MouseEvent) => void;
  onCellClick: (rowKey: number, col: ViewerColumn, event: React.MouseEvent) => void;
};

export const RecordRow = memo(
  ({
    row,
    gridTemplate,
    visibleCols,
    isWrap,
    isSelected,
    isSelectionTop,
    isSelectionBottom,
    maxRowWidth,
    colWidths,
    selectedCellCol,
    isCellTextSelectable,
    measureElement,
    measureRowElement,
    measureCellElement,
    onRowMouseDown,
    onCellClick,
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
          isSelected && 'bg-blue-500/20 dark:bg-blue-500/25',
          !isSelected && row.index % 2 !== 0 && 'bg-chrome-100',
          row.key === 0 && 'border-l-2 border-green-500 font-extrabold pl-[7px]',
          row.key !== 0 && 'text-chrome-800',
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
        {row.key !== 0 && <span className="text-chrome-300 text-[0.9rem]">{row.key > 0 ? '+' : '-'}</span>}
        {Math.abs(row.key)}
      </div>,
    );

    visibleCols.forEach((col) => {
      const minWidth = isWrap && col === ViewerColumn.Message ? undefined : colWidths.get(col);
      const shouldWrap = isWrap && col === ViewerColumn.Message;
      const isTimestamp = col === ViewerColumn.Timestamp;
      const isCellSelected = selectedCellCol === col;
      const isColorDot = col === ViewerColumn.ColorDot;

      let cellBg: string | false;
      if (isSelected) {
        cellBg = isTimestamp ? 'bg-blue-500/25 dark:bg-blue-500/25' : 'bg-blue-500/15 dark:bg-blue-500/20';
      } else {
        cellBg = isTimestamp ? 'bg-chrome-200' : row.index % 2 !== 0 && 'bg-chrome-100';
      }

      const cellClassName = cn(
        cellBg,
        'px-2',
        shouldWrap ? 'whitespace-pre-wrap wrap-break-word' : 'whitespace-nowrap',
        !isColorDot && (isCellSelected ? 'cursor-text' : 'cursor-default'),
        'select-none',
        isCellSelected && 'ring-2 ring-blue-500 ring-inset',
      );

      const cellStyle: React.CSSProperties = {
        ...(minWidth && { minWidth: `${minWidth}px` }),
        ...(isCellSelected && isCellTextSelectable && { userSelect: 'auto' as const }),
      };

      els.push(
        <CellContextMenu key={col} col={col} record={row.record}>
          <div
            ref={measureCellElement}
            data-col-id={col}
            role={isColorDot ? undefined : 'gridcell'}
            tabIndex={isColorDot ? undefined : 0}
            className={cellClassName}
            style={cellStyle}
            onClick={isColorDot ? undefined : (e) => onCellClick(row.key, col, e)}
            onMouseDown={
              isColorDot || !isCellSelected
                ? undefined
                : (e) => {
                    // Enable text selection before drag starts
                    e.currentTarget.style.userSelect = 'auto';
                  }
            }
            onKeyDown={
              isColorDot
                ? undefined
                : (e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      onCellClick(row.key, col, e as unknown as React.MouseEvent);
                    }
                  }
            }
          >
            {getAttribute(row.record, col)}
          </div>
        </CellContextMenu>,
      );
    });

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
        className={cn('absolute top-0 left-0 grid leading-6 group', selectedCellCol && 'z-10')}
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
      </div>
    );
  },
  (prev, next) => {
    if (prev.row.record !== next.row.record) return false;
    if (prev.row.start !== next.row.start) return false;
    if (prev.gridTemplate !== next.gridTemplate) return false;
    if (prev.visibleCols !== next.visibleCols) return false;
    if (prev.isWrap !== next.isWrap) return false;
    if (prev.isSelected !== next.isSelected) return false;
    if (prev.isSelectionTop !== next.isSelectionTop) return false;
    if (prev.isSelectionBottom !== next.isSelectionBottom) return false;
    if (prev.maxRowWidth !== next.maxRowWidth) return false;
    if (prev.colWidths !== next.colWidths) return false;
    if (prev.selectedCellCol !== next.selectedCellCol) return false;
    if (prev.isCellTextSelectable !== next.isCellTextSelectable) return false;
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
    selectedCell,
    isTextSelectMode,
    handleRowMouseDown,
    handleCellClick,
    resetSelection,
  } = useSelection(virtualizerRef);

  // Generate grid template
  const gridTemplate = useMemo(
    () =>
      // Key column (auto) + visible columns
      `3rem ${[...visibleCols].map((col) => (col === ViewerColumn.Message ? '1fr' : 'auto')).join(' ')}`,
    [visibleCols],
  );

  // Reset column widths and selection when loading new data
  useEffect(() => {
    if (isLoading) {
      resetWidths();
      resetSelection();
    }
  }, [isLoading]);

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

  return (
    <div ref={wrapperRef} className="relative h-full w-full flex flex-col">
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
                {virtualizer.getVirtualRows().map((virtualRow) => (
                  <RecordRow
                    key={virtualRow.key}
                    row={virtualRow}
                    measureElement={virtualizer.measureElement}
                    gridTemplate={gridTemplate}
                    visibleCols={visibleCols}
                    isWrap={wrap}
                    isSelected={selectedKeys.has(virtualRow.key)}
                    isSelectionTop={selectionTopKeys.has(virtualRow.key)}
                    isSelectionBottom={selectionBottomKeys.has(virtualRow.key)}
                    maxRowWidth={maxRowWidth}
                    colWidths={colWidths}
                    selectedCellCol={selectedCell?.rowKey === virtualRow.key ? selectedCell.col : null}
                    isCellTextSelectable={selectedCell?.rowKey === virtualRow.key && isTextSelectMode}
                    measureRowElement={measureRowElement}
                    measureCellElement={measureCellElement}
                    onRowMouseDown={handleRowMouseDown}
                    onCellClick={handleCellClick}
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
