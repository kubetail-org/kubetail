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

/* eslint-disable no-param-reassign */

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react';
import { useVirtualizer, elementScroll } from '@tanstack/react-virtual';
import type { Virtualizer, VirtualItem } from '@tanstack/react-virtual';

import { cn } from '@/lib/util';

import { useBeforePaint, type BeforePaintSubscribe } from './before-paint';
import { DoubleTailedArray, OutOfBoundsError } from './double-tailed-array';
import type { Client, Cursor, LogRecord } from './types';

/**
 * Internal constants
 */

const DEFAULT_FOLLOW = true;
const DEFAULT_INITIAL_POSITION = { type: 'head' } satisfies LogViewerInitialPosition;
const DEFAULT_OVERSCAN = 3;
const DEFAULT_BATCH_SIZE_INITIAL = 150;
const DEFAULT_BATCH_SIZE_REGULAR = 100;
const DEFAULT_LOAD_MORE_THRESHOLD = 50;
const DEFAULT_PIN_TO_BOTTOM_TOLERANCE = 10;
const DEFAULT_HAS_MORE_BEFORE_ROW_HEIGHT = 0;
const DEFAULT_HAS_MORE_AFTER_ROW_HEIGHT = 0;
const DEFAULT_IS_REFRESHING_ROW_HEIGHT = 0;

/**
 * Internal types
 */

export type LogRecordInternal = LogRecord & {
  key: number;
};

export type RecordStore = {
  new: (records: LogRecord[], skipSetCount?: boolean) => void;
  append: (records: LogRecord[]) => void;
  prepend: (records: LogRecord[]) => void;
  first: () => LogRecordInternal | undefined;
  last: () => LogRecordInternal | undefined;
  length: () => number;
};

type RuntimeConfig = {
  readonly initialPosition: LogViewerInitialPosition;
  readonly follow: boolean;
  readonly overscan: number;
  readonly batchSizeInitial: number;
  readonly batchSizeRegular: number;
  readonly loadMoreThreshold: number;
  readonly pinToBottomTolerance: number;
  readonly hasMoreBeforeRowHeight: number;
  readonly hasMoreAfterRowHeight: number;
  readonly isRefreshingRowHeight: number;
  estimateRowHeight: (record: LogRecord) => number;
  measureElement?: (
    element: Element,
    entry: ResizeObserverEntry | undefined,
    instance: Virtualizer<HTMLDivElement, Element>,
  ) => number;
};

type RuntimeState = {
  readonly count: number;
  readonly hasMoreBefore: boolean;
  readonly hasMoreAfter: boolean;
  readonly isLoading: boolean;
  readonly isRefreshing: boolean;
  readonly isRemeasuring: boolean;
};

type RuntimeRefs = {
  scrollEl: React.RefObject<HTMLDivElement | null>;
  isLoadingBefore: React.RefObject<boolean>;
  isLoadingAfter: React.RefObject<boolean>;
  isAutoScrollEnabled: React.RefObject<boolean>;
  isProgrammaticScroll: React.RefObject<boolean>;
};

type RuntimeActions = {
  setCount: React.Dispatch<React.SetStateAction<number>>;
  setIsLoading: React.Dispatch<React.SetStateAction<boolean>>;
  setHasMoreBefore: React.Dispatch<React.SetStateAction<boolean>>;
  setHasMoreAfter: React.Dispatch<React.SetStateAction<boolean>>;
  setIsRefreshing: React.Dispatch<React.SetStateAction<boolean>>;
};

export type Runtime = {
  client: Client;
  config: RuntimeConfig;
  state: RuntimeState;
  refs: RuntimeRefs;
  actions: RuntimeActions;
  services: {
    beforePaint: BeforePaintSubscribe;
    virtualizer: Virtualizer<HTMLDivElement, Element>;
    recordStore: RecordStore;
  };
};

/**
 * External constants
 */

export const LOG_VIEWER_INITIAL_STATE = {
  isLoading: true,
} satisfies LogViewerState;

/**
 * External types
 */

export type LogViewerVirtualRow = Pick<VirtualItem, 'key'> & {
  index: number;
  size: number;
  start: number;
  record: LogRecord;
};

export type LogViewerVirtualizer = {
  readonly isLoading: boolean;
  readonly isRemeasuring: boolean;
  readonly hasMoreBefore: boolean;
  readonly hasMoreAfter: boolean;
  readonly isRefreshing: boolean;
  readonly hasMoreAfterRowHeight: number;
  readonly hasMoreBeforeRowHeight: number;
  readonly isRefreshingRowHeight: number;
  readonly range: { startIndex: number; endIndex: number } | null;
  getTotalSize: () => number;
  getVirtualRows: () => LogViewerVirtualRow[];
  measureElement: (node: Element | null | undefined) => void;
};

export type LogViewerState = {
  isLoading: boolean;
};

export type LogViewerInitialPosition =
  | { type: 'head'; cursor?: never }
  | { type: 'tail'; cursor?: never }
  | { type: 'cursor'; cursor: Cursor };

/**
 * Internal utilities
 */

function isAtBottom(scrollEl: Element, tolerance: number) {
  const { scrollTop, clientHeight, scrollHeight } = scrollEl;
  return Math.abs(scrollTop + clientHeight - scrollHeight) <= tolerance;
}

/**
 * useRecordStore - Custom hook to add items to the log records cache
 */

type RecordStoreOptions = {
  recordsRef: React.RefObject<DoubleTailedArray<LogRecordInternal>>;
  setCount: React.Dispatch<React.SetStateAction<number>>;
};

export function useRecordStore({ recordsRef, setCount }: RecordStoreOptions): RecordStore {
  const keyRef = useRef(0);

  const addKeys = useCallback((records: LogRecord[]) => {
    for (let i = 0; i < records.length; i += 1) {
      (records[i] as LogRecordInternal).key = keyRef.current;
      keyRef.current += 1;
    }
  }, []);

  return useMemo(() => {
    // Initialize
    recordsRef.current = new DoubleTailedArray();

    return {
      new: (records: LogRecord[], skipSetCount = false) => {
        addKeys(records);
        recordsRef.current = new DoubleTailedArray(records as LogRecordInternal[]);
        if (!skipSetCount) setCount(recordsRef.current.length);
      },
      append: (records: LogRecord[]) => {
        addKeys(records);
        recordsRef.current.append(records as LogRecordInternal[]);
        setCount(recordsRef.current.length);
      },
      prepend: (records: LogRecord[]) => {
        addKeys(records);
        recordsRef.current.prepend(records as LogRecordInternal[]);
        setCount(recordsRef.current.length);
      },
      first: () => {
        try {
          return recordsRef.current?.first();
        } catch (e) {
          if (e instanceof OutOfBoundsError) return undefined;
          throw e;
        }
      },
      last: () => {
        try {
          return recordsRef.current?.last();
        } catch (e) {
          if (e instanceof OutOfBoundsError) return undefined;
          throw e;
        }
      },
      length: () => recordsRef.current?.length ?? 0,
    };
  }, []);
}

/**
 * useInit - Initializer hook
 */

export const useInit = ({ client, config, refs, actions, services }: Runtime) => {
  const isInitializedRef = useRef(false);

  useEffect(() => {
    if (isInitializedRef.current) return;
    isInitializedRef.current = true;

    const initFn = async () => {
      switch (config.initialPosition.type) {
        case 'head': {
          const result = await client.fetchSince({ limit: config.batchSizeInitial });

          // Update UI
          if (result.records.length) {
            if (result.nextCursor !== null) actions.setHasMoreAfter(true);
            services.recordStore.new(result.records);
          }

          refs.isAutoScrollEnabled.current = false;

          break;
        }
        case 'tail': {
          const result = await client.fetchUntil({ limit: config.batchSizeInitial });

          // Update UI
          if (result.records.length) {
            if (result.nextCursor !== null) actions.setHasMoreBefore(true);

            const beforePaintPromise = services.beforePaint(() => {
              const scrollElement = refs.scrollEl.current;
              if (scrollElement) scrollElement.scrollTop = scrollElement.scrollHeight;
            });

            services.recordStore.new(result.records);

            await beforePaintPromise;
          }

          refs.isAutoScrollEnabled.current = true;

          break;
        }
        case 'cursor': {
          // Fetch BATCH_SIZE records before and after the seek timestamp
          const [beforeResult, afterResult] = await Promise.all([
            client.fetchBefore({
              cursor: config.initialPosition.cursor,
              limit: config.batchSizeInitial,
            }),
            client.fetchSince({
              cursor: config.initialPosition.cursor,
              limit: config.batchSizeInitial,
            }),
          ]);

          // Update UI
          if (beforeResult.records.length || afterResult.records.length) {
            // Handle cursors for before results
            if (beforeResult.nextCursor !== null) actions.setHasMoreBefore(true);

            // Handle cursors for after results
            if (afterResult.nextCursor !== null) actions.setHasMoreAfter(true);

            // Scroll to the middle (where the seek timestamp should be)
            const beforePaintPromise = services.beforePaint(() => {
              services.virtualizer.scrollToIndex(beforeResult.records.length, { align: 'start' });
            });

            // Combine results
            services.recordStore.new(afterResult.records, true);
            services.recordStore.prepend(beforeResult.records);

            await beforePaintPromise;
          }

          refs.isAutoScrollEnabled.current = false;

          break;
        }
        default:
          throw new Error('Invalid initial position type');
      }
    };

    // Call init function
    let cancelID: number;

    actions.setIsLoading(true);

    initFn()
      .catch((error) => {
        // Log error but don't throw - allow the UI to continue functioning
        console.error('Failed to load records:', error);
      })
      .finally(() => {
        // Wait until paint finishes to turn off loading flag
        cancelID = requestAnimationFrame(() => actions.setIsLoading(false));
      });

    return () => {
      if (cancelID) {
        cancelAnimationFrame(cancelID);
        actions.setIsLoading(false);
      }
    };
  }, []);
};

/**
 * useLoadMoreBefore - Returns stable loadMoreBefore function
 */

export const useLoadMoreBefore = ({ client, config, refs, actions, services }: Runtime) =>
  useCallback(async () => {
    // Get data
    const result = await client.fetchBefore({
      cursor: services.recordStore.first()?.cursor,
      limit: config.batchSizeRegular,
    });

    // Update `hasMoreBefore`
    if (result.nextCursor === null) actions.setHasMoreBefore(false);

    // Update UI
    if (result.records.length) {
      const scrollElement = refs.scrollEl.current;
      if (!scrollElement) return;

      const { scrollTop: prevScrollTop, scrollHeight: prevScrollHeight } = scrollElement;

      // Hack to get around https://github.com/TanStack/virtual/issues/1094
      services.virtualizer.isScrolling = false;

      const beforePaintPromise = services.beforePaint(() => {
        const nextScrollHeight = scrollElement.scrollHeight;
        scrollElement.scrollTop = prevScrollTop + (nextScrollHeight - prevScrollHeight);
      });

      services.recordStore.prepend(result.records);

      await beforePaintPromise;
    }
  }, [client, config.batchSizeRegular]);

/**
 * useLoadMoreAfter - Returns stable loadMoreAfter function
 */

export const useLoadMoreAfter = ({ client, config, actions, services }: Runtime) =>
  useCallback(async () => {
    // Get data
    const result = await client.fetchAfter({
      cursor: services.recordStore.last()?.cursor,
      limit: config.batchSizeRegular,
    });

    // Update `hasMoreAfter`
    if (result.nextCursor === null) actions.setHasMoreAfter(false);

    // Update UI
    if (result.records.length) {
      // Hack to get around https://github.com/TanStack/virtual/issues/1094
      services.virtualizer.isScrolling = false;

      services.recordStore.append(result.records);
    }
  }, [client, config.batchSizeRegular]);

/**
 * useLoadMore - Load more hook
 */

export const useLoadMore = (runtime: Runtime) => {
  const loadMoreBefore = useLoadMoreBefore(runtime);
  const loadMoreAfter = useLoadMoreAfter(runtime);

  const { config, refs, state, services } = runtime;

  const virtualizerRange = services.virtualizer.range;

  const countRef = useRef(state.count);
  countRef.current = state.count;

  useEffect(() => {
    if (!virtualizerRange || state.isLoading || state.isRemeasuring) return;

    if (state.hasMoreBefore && !refs.isLoadingBefore.current) {
      if (virtualizerRange.startIndex <= config.loadMoreThreshold - config.overscan) {
        refs.isLoadingBefore.current = true;
        loadMoreBefore()
          .catch((error) => {
            // Log error but don't throw - allow the UI to continue functioning
            console.error('Failed to load more records before:', error);
          })
          .finally(() => {
            requestAnimationFrame(() => {
              refs.isLoadingBefore.current = false;
            });
          });
      }
    }

    if (state.hasMoreAfter && !refs.isLoadingAfter.current) {
      if (virtualizerRange.endIndex >= countRef.current - 1 - config.loadMoreThreshold + config.overscan) {
        refs.isLoadingAfter.current = true;
        loadMoreAfter()
          .catch((error) => {
            // Log error and allow the UI to continue functioning
            console.error('Failed to load more records after:', error);
          })
          .finally(() => {
            requestAnimationFrame(() => {
              refs.isLoadingAfter.current = false;
            });
          });
      }
    }
  }, [
    virtualizerRange?.startIndex,
    virtualizerRange?.endIndex,
    state.hasMoreBefore,
    state.hasMoreAfter,
    state.isLoading,
    state.isRemeasuring,
    config.overscan,
    config.loadMoreThreshold,
  ]);
};

/**
 * useFollowFromEnd - Implement follow-from-end behavior
 */

export const useFollowFromEnd = ({ client, config, state, refs, services }: Runtime) => {
  useEffect(() => {
    if (!config.follow || state.isLoading || state.hasMoreAfter) return;

    const scrollElement = refs.scrollEl.current;

    let rafID: number | null = null;
    let pendingRecords: LogRecord[] = [];

    const flush = () => {
      rafID = null;
      if (pendingRecords.length === 0) return;

      const records = pendingRecords;
      pendingRecords = [];

      // Hack to get around https://github.com/TanStack/virtual/issues/1094
      services.virtualizer.isScrolling = false;

      // Scroll to bottom if auto-scroll enabled
      if (refs.isAutoScrollEnabled.current) {
        services.beforePaint(() => {
          if (scrollElement) scrollElement.scrollTop = scrollElement.scrollHeight;
        });
      }

      // Append all pending records at once
      services.recordStore.append(records);
    };

    const cb = (record: LogRecord) => {
      pendingRecords.push(record);
      if (rafID === null) rafID = requestAnimationFrame(flush);
    };

    const after = services.recordStore.last()?.cursor ?? 'BEGINNING';
    const opts = { after };

    const unsubscribe = client.subscribe(cb, opts);

    return () => {
      unsubscribe();

      // Prevent next flush
      if (rafID !== null) cancelAnimationFrame(rafID);
      pendingRecords = [];
    };
  }, [client, config.follow, state.isLoading, state.hasMoreAfter]);
};

/**
 * useAutoScroll - Implement auto-scroll
 */

export const useAutoScroll = ({ config, state, refs }: Runtime) => {
  const lastScrollTopRef = useRef(0);

  useEffect(() => {
    const scrollElement = refs.scrollEl.current;
    if (!scrollElement) return;

    if (state.isLoading || state.hasMoreAfter) return;

    const handleScroll = () => {
      const lastScrollTop = lastScrollTopRef.current;

      const { scrollTop } = scrollElement;

      // Update scroll position tracker
      lastScrollTopRef.current = scrollTop;

      // Noop if scroll was programmatic
      if (refs.isProgrammaticScroll.current) return;

      // If scrolled to bottom, turn on auto-scroll
      if (isAtBottom(scrollElement, config.pinToBottomTolerance)) {
        refs.isAutoScrollEnabled.current = true;
        return;
      }

      // If scrolling up, turn off auto-scroll and exit
      if (scrollTop < lastScrollTop) {
        refs.isAutoScrollEnabled.current = false;
      }
    };

    scrollElement.addEventListener('scroll', handleScroll);

    return () => {
      scrollElement.removeEventListener('scroll', handleScroll);
    };
  }, [config.pinToBottomTolerance, config.follow, state.isLoading, state.hasMoreAfter]);
};

/**
 * usePullToRefresh - Implement pull-to-refresh feature
 */

export const usePullToRefresh = (runtime: Runtime) => {
  const loadMoreAfter = useLoadMoreAfter(runtime);

  const { config, refs, state, actions } = runtime;
  const wheelRafRef = useRef<number | null>(null);

  // Handle pull-to-refresh at the end when follow is disabled
  useEffect(() => {
    const scrollElement = refs.scrollEl.current;
    if (!scrollElement) return;

    if (config.follow || state.isLoading || state.hasMoreAfter) return;

    const handleWheel = (event: WheelEvent) => {
      if (wheelRafRef.current !== null) return;

      const { deltaY } = event;

      wheelRafRef.current = requestAnimationFrame(() => {
        wheelRafRef.current = null;

        if (deltaY <= 0) return;
        if (!isAtBottom(scrollElement, config.pinToBottomTolerance)) return;
        if (refs.isLoadingAfter.current) return;

        refs.isLoadingAfter.current = true;
        actions.setIsRefreshing(true);

        loadMoreAfter()
          .catch((error) => {
            // Log error and allow the UI to continue functioning
            console.error('Failed to refresh records:', error);
          })
          .finally(() => {
            requestAnimationFrame(() => {
              refs.isLoadingAfter.current = false;
              actions.setIsRefreshing(false);
            });
          });
      });
    };

    scrollElement.addEventListener('wheel', handleWheel, { passive: true });

    return () => {
      if (wheelRafRef.current !== null) {
        cancelAnimationFrame(wheelRafRef.current);
        wheelRafRef.current = null;
      }
      scrollElement.removeEventListener('wheel', handleWheel);
    };
  }, [config.follow, config.loadMoreThreshold, config.pinToBottomTolerance, state.isLoading, state.hasMoreAfter]);
};

/**
 * LogViewerInner - Inner component that renders virtualized list of log records
 */

type LogViewerInnerProps = {
  className?: string;
  client: Client;
  config: RuntimeConfig;
  isLoading: boolean;
  setIsLoading: React.Dispatch<React.SetStateAction<boolean>>;
  isRemeasuring: boolean;
  virtualizerRef: React.RefObject<Virtualizer<HTMLDivElement, Element> | null>;
  scrollElRef?: React.RefObject<HTMLDivElement | null>;
  children: (virtualizer: LogViewerVirtualizer) => React.ReactNode;
};

export const LogViewerInner = ({
  className = '',
  client,
  config,
  isLoading,
  setIsLoading,
  isRemeasuring,
  virtualizerRef,
  scrollElRef: externalScrollElRef,
  children,
  ...other
}: LogViewerInnerProps) => {
  const internalScrollElRef = useRef<HTMLDivElement>(null);
  const scrollElRef = externalScrollElRef || internalScrollElRef;

  const [count, setCount] = useState(0);

  // RecordsRef will never be null so this assertion is safe
  const recordsRef = useRef(null) as unknown as React.RefObject<DoubleTailedArray<LogRecordInternal>>;

  const [hasMoreBefore, setHasMoreBefore] = useState(false);
  const [hasMoreAfter, setHasMoreAfter] = useState(false);
  const [isRefreshing, setIsRefreshing] = useState(false);

  const isLoadingBeforeRef = useRef(false);
  const isLoadingAfterRef = useRef(false);

  const beforePaint = useBeforePaint(count);
  const recordStore = useRecordStore({ recordsRef, setCount });
  const isAutoScrollEnabledRef = useRef(false);
  const isProgrammaticScrollRef = useRef(false);

  const estimateSize = useCallback(
    (index: number) => config.estimateRowHeight(recordsRef.current.at(index)),
    [config.estimateRowHeight],
  );

  const getItemKey = useCallback((index: number) => recordsRef.current.at(index).key, [count]);

  const scrollToRafID = useRef<number>(null);
  const scrollToFn = useCallback<typeof elementScroll>((offset, options, instance) => {
    if (scrollToRafID.current) cancelAnimationFrame(scrollToRafID.current);
    isProgrammaticScrollRef.current = true;
    elementScroll(offset, options, instance);
    scrollToRafID.current = requestAnimationFrame(() => {
      isProgrammaticScrollRef.current = false;
      scrollToRafID.current = null;
    });
  }, []);

  const virtualizer = useVirtualizer({
    count,
    getScrollElement: () => scrollElRef.current,
    estimateSize,
    getItemKey,
    measureElement: config.measureElement,
    scrollToFn,
    overscan: config.overscan,
    scrollMargin: hasMoreBefore ? config.hasMoreBeforeRowHeight : 0,
    useScrollendEvent: true,
  });

  // Store virtualizer in ref for parent access
  virtualizerRef.current = virtualizer;

  const runtime = {
    client,
    config,
    state: { count, hasMoreBefore, hasMoreAfter, isRefreshing, isLoading, isRemeasuring },
    refs: {
      scrollEl: scrollElRef,
      isAutoScrollEnabled: isAutoScrollEnabledRef,
      isLoadingBefore: isLoadingBeforeRef,
      isLoadingAfter: isLoadingAfterRef,
      isProgrammaticScroll: isProgrammaticScrollRef,
    },
    actions: { setCount, setHasMoreBefore, setHasMoreAfter, setIsRefreshing, setIsLoading },
    services: { beforePaint, virtualizer, recordStore },
  } satisfies Runtime;

  useInit(runtime);
  useLoadMore(runtime);
  useFollowFromEnd(runtime);
  useAutoScroll(runtime);
  usePullToRefresh(runtime);

  const v = {
    isLoading,
    isRemeasuring,
    isRefreshing,
    hasMoreBefore,
    hasMoreAfter,
    hasMoreBeforeRowHeight: config.hasMoreBeforeRowHeight,
    hasMoreAfterRowHeight: config.hasMoreAfterRowHeight,
    isRefreshingRowHeight: config.isRefreshingRowHeight,
    range: virtualizer.range,
    getTotalSize: () => {
      let h = count ? virtualizer.getTotalSize() : 0;
      if (hasMoreBefore) h += config.hasMoreBeforeRowHeight;
      if (hasMoreAfter) h += config.hasMoreAfterRowHeight;
      if (runtime.state.isRefreshing) h += config.isRefreshingRowHeight;
      return h;
    },
    getVirtualRows: () => {
      if (count === 0) return [];
      return virtualizer.getVirtualItems().map((item) => {
        const { key, index, size, start } = item;
        return {
          key,
          index,
          size,
          start,
          record: recordsRef.current.at(index),
        };
      });
    },
    measureElement: virtualizer.measureElement,
  } satisfies LogViewerVirtualizer;

  return (
    <div ref={scrollElRef} className={cn('overflow-auto', className)} {...other}>
      {children(v)}
    </div>
  );
};

/**
 * LogViewer - Component to render virtualized list of log records
 */

export type LogViewerProps = {
  className?: string;
  client: Client;
  estimateRowHeight: RuntimeConfig['estimateRowHeight'];
  initialPosition?: LogViewerInitialPosition;
  follow?: boolean;
  overscan?: number;
  batchSizeInitial?: number;
  batchSizeRegular?: number;
  loadMoreThreshold?: number;
  pinToBottomTolerance?: number;
  hasMoreBeforeRowHeight?: number;
  hasMoreAfterRowHeight?: number;
  isRefreshingRowHeight?: number;
  measureElement?: RuntimeConfig['measureElement'];
  scrollElRef?: React.RefObject<HTMLDivElement | null>;
  children: (virtualizer: LogViewerVirtualizer) => React.ReactNode;
};

export type LogViewerHandle = {
  jumpToBeginning: () => Promise<void>;
  jumpToEnd: () => Promise<void>;
  jumpToCursor: (cursor: Cursor) => Promise<void>;
  measure: () => void;
  subscribe: (callback: () => void) => () => void;
  getSnapshot: () => LogViewerState;
};

export const LogViewer = forwardRef<LogViewerHandle, LogViewerProps>(
  (
    {
      client,
      estimateRowHeight,
      initialPosition: defaultInitialPosition = DEFAULT_INITIAL_POSITION,
      follow = DEFAULT_FOLLOW,
      overscan = DEFAULT_OVERSCAN,
      batchSizeInitial = DEFAULT_BATCH_SIZE_INITIAL,
      batchSizeRegular = DEFAULT_BATCH_SIZE_REGULAR,
      loadMoreThreshold = DEFAULT_LOAD_MORE_THRESHOLD,
      pinToBottomTolerance = DEFAULT_PIN_TO_BOTTOM_TOLERANCE,
      hasMoreBeforeRowHeight = DEFAULT_HAS_MORE_BEFORE_ROW_HEIGHT,
      hasMoreAfterRowHeight = DEFAULT_HAS_MORE_AFTER_ROW_HEIGHT,
      isRefreshingRowHeight = DEFAULT_IS_REFRESHING_ROW_HEIGHT,
      measureElement,
      scrollElRef: externalScrollElRef,
      children,
      ...other
    },
    ref,
  ) => {
    const [keyID, setKeyID] = useState(0);
    const incrementKeyID = useCallback(() => setKeyID((id) => id + 1), []);

    const [initialPosition, setInitialPosition] = useState<LogViewerInitialPosition>(defaultInitialPosition);
    const [isLoading, setIsLoading] = useState<boolean>(LOG_VIEWER_INITIAL_STATE.isLoading);

    const [isRemeasuring, setIsRemeasuring] = useState(false);
    const isRemeasuringRef = useRef(false);

    // Create ref to hold virtualizer instance from child
    const virtualizerRef = useRef<Virtualizer<HTMLDivElement, Element> | null>(null);

    // Support structures for subscribe() and getSnapshot()
    const stateRef = useRef<LogViewerState>(LOG_VIEWER_INITIAL_STATE);
    const listenerQueueRef = useRef(null) as unknown as React.RefObject<Set<() => void>>;
    if (!listenerQueueRef.current) listenerQueueRef.current = new Set<() => void>();
    useEffect(() => {
      stateRef.current = { isLoading };
      listenerQueueRef.current.forEach((callback) => callback());
    }, [isLoading]);

    // Add handle
    useImperativeHandle(
      ref,
      () => ({
        jumpToBeginning: async () => {
          setIsLoading(true);
          setInitialPosition({ type: 'head' });
          incrementKeyID();
          // TODO: wait for isLoading to resolve
        },
        jumpToEnd: async () => {
          setIsLoading(true);
          setInitialPosition({ type: 'tail' });
          incrementKeyID();
          // TODO: wait for isLoading to resolve
        },
        jumpToCursor: async (cursor: Cursor) => {
          setIsLoading(true);
          setInitialPosition({ type: 'cursor', cursor });
          incrementKeyID();
          // TODO: wait for isLoading to resolve
        },
        measure: () => {
          const virtualizer = virtualizerRef.current;
          if (!virtualizer) return;

          const { scrollElement, range } = virtualizer;
          if (!scrollElement || !range) return;

          // Check guard
          if (isRemeasuringRef.current) return;
          isRemeasuringRef.current = true;

          // Set flag
          setIsRemeasuring(true);

          // Calculate scrollTo position
          const scrollTo: [number, { align: 'start' | 'end' }] =
            range.endIndex === virtualizer.options.count - 1
              ? [range.endIndex, { align: 'end' }]
              : [range.startIndex, { align: 'start' }];

          // Re-measure
          virtualizer.measure();

          requestAnimationFrame(() => {
            // Adjust scroll position
            virtualizer.scrollToIndex(scrollTo[0], scrollTo[1]);

            requestAnimationFrame(() => {
              // Reset flag
              setIsRemeasuring(false);

              // Reset guard
              isRemeasuringRef.current = false;
            });
          });
        },
        subscribe: (callback: () => void) => {
          listenerQueueRef.current.add(callback);
          return () => {
            listenerQueueRef.current.delete(callback);
          };
        },
        getSnapshot: () => stateRef.current,
      }),
      [],
    );

    // Reset completely when client changes
    const prevClientRef = useRef<Client>(null);
    useEffect(() => {
      if (prevClientRef.current && prevClientRef.current !== client) {
        setIsLoading(true);
        incrementKeyID();
      }
      prevClientRef.current = client;
    }, [client]);

    const config = useMemo(
      () => ({
        initialPosition,
        follow,
        estimateRowHeight,
        overscan,
        batchSizeInitial,
        batchSizeRegular,
        loadMoreThreshold,
        pinToBottomTolerance,
        hasMoreBeforeRowHeight,
        hasMoreAfterRowHeight,
        isRefreshingRowHeight,
        measureElement,
      }),
      [
        initialPosition,
        follow,
        estimateRowHeight,
        overscan,
        batchSizeInitial,
        batchSizeRegular,
        loadMoreThreshold,
        pinToBottomTolerance,
        hasMoreBeforeRowHeight,
        hasMoreAfterRowHeight,
        isRefreshingRowHeight,
        measureElement,
      ],
    );

    return (
      <LogViewerInner
        key={keyID}
        client={client}
        config={config}
        isLoading={isLoading}
        setIsLoading={setIsLoading}
        isRemeasuring={isRemeasuring}
        virtualizerRef={virtualizerRef}
        scrollElRef={externalScrollElRef}
        {...other}
      >
        {children}
      </LogViewerInner>
    );
  },
);

/**
 * useLogViewerState - Hook to subscribe to LogViewer external state reactively
 */

function createLogViewerStore(handle: LogViewerHandle | null) {
  if (handle) return { subscribe: handle.subscribe, getSnapshot: handle.getSnapshot };
  return {
    subscribe: (_: () => void) => () => {},
    getSnapshot: () => LOG_VIEWER_INITIAL_STATE,
  };
}

export function useLogViewerState(
  logViewerRef: React.RefObject<LogViewerHandle | null>,
  dependencies: any[],
): LogViewerState {
  // Initialize store
  const [store, setStore] = useState(() => createLogViewerStore(logViewerRef.current));

  // Update based on user-provided dependencies
  useEffect(() => {
    setStore(createLogViewerStore(logViewerRef.current));
  }, [...dependencies]);

  // Return sync external store instance
  return useSyncExternalStore(store.subscribe, store.getSnapshot);
}
