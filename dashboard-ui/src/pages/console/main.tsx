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
import type { LogRecord, LogViewerInitialPosition, LogViewerVirtualRow } from '@/components/widgets/log-viewer';

import { ALL_VIEWER_COLUMNS, PageContext, ViewerColumn } from './shared';
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
  const initialPositionRef = useRef<LogViewerInitialPosition>(DEFAULT_INITIAL_POSITION);
  const isInitializedRef = useRef(false);

  if (!isInitializedRef.current) {
    isInitializedRef.current = true;
    const searchParams = new URLSearchParams(search);
    switch (searchParams.get('mode')) {
      case 'head':
        initialPositionRef.current = { type: 'head' };
        break;
      case 'cursor': {
        const cursor = searchParams.get('cursor');
        if (cursor !== null) initialPositionRef.current = { type: 'cursor', cursor };
        break;
      }
      default:
        break;
    }
  }

  return initialPositionRef.current;
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

  const pendingRef = useRef(null) as unknown as React.RefObject<typeof widths>;
  if (!pendingRef.current) pendingRef.current = newDefaultWidths();

  const measuredRef = useRef(null) as unknown as React.RefObject<WeakSet<Element>>;
  if (!measuredRef.current) measuredRef.current = new WeakSet<Element>();

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
    [flush],
  );

  const measureCellElement = useCallback(
    (el: HTMLDivElement | null) => {
      if (!el || measuredRef.current.has(el)) return;
      measuredRef.current.add(el);

      const pendingColWidths = pendingRef.current.colWidths;
      const col = el.dataset.colId as ViewerColumn;
      const prev = pendingColWidths.get(col);
      const next = Math.max(el.scrollWidth, prev ?? 0);
      if (next !== prev) {
        pendingColWidths.set(col, next);
        flush();
      }
    },
    [flush],
  );

  const resetWidths = useCallback(() => {
    if (rafIDRef.current !== null) {
      cancelAnimationFrame(rafIDRef.current);
      rafIDRef.current = null;
    }
    pendingRef.current = newDefaultWidths();
    measuredRef.current = new WeakSet();
    setWidths(newDefaultWidths);
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
        {ALL_VIEWER_COLUMNS.map((col) => {
          if (visibleCols.has(col)) {
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
          }
          return null;
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
    case ViewerColumn.PodContainer:
      return `${record.source.podName}/${record.source.containerName}`;
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

type RecordRowProps = {
  row: LogViewerVirtualRow;
  gridTemplate: string;
  visibleCols: Set<ViewerColumn>;
  isWrap: boolean;
  maxRowWidth: number;
  colWidths: Map<ViewerColumn, number>;
  measureElement: (node: Element | null | undefined) => void;
  measureRowElement: (el: HTMLDivElement | null) => void;
  measureCellElement: (el: HTMLDivElement | null) => void;
};

const RecordRow = memo(
  ({
    row,
    gridTemplate,
    visibleCols,
    isWrap,
    maxRowWidth,
    colWidths,
    measureElement,
    measureRowElement,
    measureCellElement,
  }: RecordRowProps) => {
    const els: React.ReactElement[] = [];
    ALL_VIEWER_COLUMNS.forEach((col) => {
      if (visibleCols.has(col)) {
        const minWidth = isWrap && col === ViewerColumn.Message ? undefined : colWidths.get(col);
        const shouldWrap = isWrap && col === ViewerColumn.Message;
        els.push(
          <div
            key={col}
            ref={measureCellElement}
            data-col-id={col}
            className={cn(
              row.index % 2 !== 0 && 'bg-chrome-100',
              'px-2',
              shouldWrap ? 'whitespace-pre-wrap wrap-break-word' : 'whitespace-nowrap',
              col === ViewerColumn.Timestamp ? 'bg-chrome-200' : '',
            )}
            style={minWidth ? { minWidth: `${minWidth}px` } : undefined}
          >
            {getAttribute(row.record, col)}
          </div>,
        );
      }
    });

    return (
      <div
        ref={(el) => {
          measureElement(el);
          measureRowElement(el);
        }}
        data-index={row.index}
        className="absolute top-0 left-0 grid leading-6"
        style={{
          gridTemplateColumns: gridTemplate,
          minWidth: isWrap ? '100%' : maxRowWidth || '100%',
          height: isWrap ? undefined : LOG_RECORD_ROW_HEIGHT,
          lineHeight: `${LOG_RECORD_ROW_HEIGHT}px`,
          transform: `translateY(${row.start}px)`,
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
    if (prev.maxRowWidth !== next.maxRowWidth) return false;
    if (prev.colWidths !== next.colWidths) return false;
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

  // Generate grid template
  const gridTemplate = useMemo(() => {
    const visibleColumns = ALL_VIEWER_COLUMNS.filter((col) => visibleCols.has(col));
    // Keep natural sizing - let cells determine column width
    return visibleColumns.map((col) => (col === ViewerColumn.Message ? '1fr' : 'auto')).join(' ');
  }, [visibleCols]);

  // Reset column widths when loading new data
  useEffect(() => {
    if (isLoading) {
      resetWidths();
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
          {(virtualizer) => (
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
                  maxRowWidth={maxRowWidth}
                  colWidths={colWidths}
                  measureRowElement={measureRowElement}
                  measureCellElement={measureCellElement}
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
          )}
        </LogViewer>
      )}
    </div>
  );
};
