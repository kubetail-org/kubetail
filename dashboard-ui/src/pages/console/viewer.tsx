// Copyright 2024-2025 Andres Morey
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

import { useSubscription } from '@apollo/client/react';
import { format, toZonedTime } from 'date-fns-tz';
import { useAtom, useAtomValue, useSetAtom } from 'jotai';
import {
  createContext,
  forwardRef,
  memo,
  useCallback,
  useContext,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from 'react';
import AutoSizer from 'react-virtualized-auto-sizer';
import { VariableSizeList, areEqual } from 'react-window';
import InfiniteLoader from 'react-window-infinite-loader';
import { useDebounceCallback } from 'usehooks-ts';

import { Spinner } from '@kubetail/ui/elements/spinner';
import { stripAnsi } from 'fancy-ansi';
import { AnsiHtml } from 'fancy-ansi/react';

import LoadingPage from '@/components/utils/LoadingPage';
import {
  ConsoleNodesListItemFragmentFragment,
  LogRecordsQueryMode,
  LogSourceFilter,
  LogSourceFragmentFragment,
  WatchEventType,
} from '@/lib/graphql/dashboard/__generated__/graphql';
import { CONSOLE_NODES_LIST_FETCH, CONSOLE_NODES_LIST_WATCH, LOG_SOURCES_WATCH } from '@/lib/graphql/dashboard/ops';
import { useIsClusterAPIEnabled, useListQueryWithSubscription, useNextTick } from '@/lib/hooks';
import { Counter, cn, cssEncode } from '@/lib/util';

import { LogRecordsFetcher } from './log-records-fetcher';
import type { LogRecordsFetcherHandle } from './log-records-fetcher';
import { ALL_VIEWER_COLUMNS, ViewerColumn } from './shared';
import type { LogRecord } from './shared';
import {
  logRecordsAtom,
  isReadyAtom,
  isLoadingAtom,
  isFollowAtom,
  visibleColsAtom,
  isWrapAtom,
  colWidthsAtom,
  maxRowWidthAtom,
} from './state';

/**
 * Shared variables and types
 */

type ContextType = {
  useClusterAPI: boolean | undefined;
  kubeContext: string | null;
  sources: string[];
  sourceFilter: LogSourceFilter;
  grep: string | null;
};

const Context = createContext<ContextType>({} as ContextType);

/**
 * Hooks
 */

export function useNodes() {
  const { kubeContext } = useContext(Context);

  const { fetching, data } = useListQueryWithSubscription({
    query: CONSOLE_NODES_LIST_FETCH,
    subscription: CONSOLE_NODES_LIST_WATCH,
    queryDataKey: 'coreV1NodesList',
    subscriptionDataKey: 'coreV1NodesWatch',
    variables: { kubeContext: kubeContext || '' },
  });

  const loading = fetching; // treat still-fetching as still-loading
  const nodes = data?.coreV1NodesList?.items
    ? data.coreV1NodesList.items
    : ([] as ConsoleNodesListItemFragmentFragment[]);

  return { loading, nodes };
}

export const useSources = () => {
  const { kubeContext, sources } = useContext(Context);
  const [sourceMap, setSourceMap] = useState(new Map<string, LogSourceFragmentFragment>());

  const { loading } = useSubscription(LOG_SOURCES_WATCH, {
    variables: { kubeContext, sources },
    onData: ({ data }) => {
      const ev = data.data?.logSourcesWatch;
      if (!ev) return;

      const source = ev?.object;
      if (!source) return;

      const k = `${source.namespace}/${source.podName}/${source.containerName}`;
      setSourceMap((prevMap) => {
        const newMap = new Map(prevMap);
        if (ev?.type === WatchEventType.Added) newMap.set(k, source);
        else if (ev?.type === WatchEventType.Deleted) newMap.delete(k);
        return newMap;
      });
    },
  });

  return { loading, sources: Array.from(sourceMap.values()) };
};

export const useViewerMetadata = () => {
  const isReady = useAtomValue(isReadyAtom);
  const isLoading = useAtomValue(isLoadingAtom);
  const isFollow = useAtomValue(isFollowAtom);

  const { kubeContext } = useContext(Context);
  const isUseClusterAPIEnabled = useIsClusterAPIEnabled(kubeContext);

  return {
    isReady,
    isLoading,
    isFollow,
    isSearchEnabled: isUseClusterAPIEnabled,
  };
};

export const useViewerFacets = () => {
  const { sources } = useSources();
  const { nodes } = useNodes();

  // Calculate facets
  const regionCounts = new Counter();
  const zoneCounts = new Counter();
  const archCounts = new Counter();
  const osCounts = new Counter();
  const nodeCounts = new Counter();

  // Update nodes facet
  nodes.forEach((node) => {
    nodeCounts.set(node.metadata.name, 0);
  });

  // Update facets
  sources.forEach((source) => {
    regionCounts.update(source.metadata.region);
    zoneCounts.update(source.metadata.zone);
    archCounts.update(source.metadata.arch);
    osCounts.update(source.metadata.os);
    nodeCounts.update(source.metadata.node);
  });

  return {
    region: regionCounts,
    zone: zoneCounts,
    os: osCounts,
    arch: archCounts,
    node: nodeCounts,
  };
};

/**
 * Loading overlay
 */

const LoadingOverlay = ({ height, width }: { height: number; width: number }) => (
  <>
    <div className="top-0 absolute bg-chrome-100 opacity-85" style={{ height, width }} />
    <div className="top-0 absolute" style={{ height, width }}>
      <div className="min-h-full flex items-center justify-center text-center">
        <div className="flex items-center space-x-4 bg-background p-3 border border-chrome-200 rounded-md">
          <div>Loading</div>
          <Spinner size="xs" />
        </div>
      </div>
    </div>
  </>
);

/**
 * Row component
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

type RowData = {
  items: LogRecord[];
  hasMoreBefore: boolean;
  hasMoreAfter: boolean;
  visibleCols: Set<string>;
  isWrap: boolean;
};

type RowProps = {
  index: any;
  style: any;
  data: RowData;
};

const Row = memo(({ index, style, data }: RowProps) => {
  const { items, hasMoreBefore, visibleCols, isWrap } = data;

  const rowElRef = useRef<HTMLDivElement>(null);
  const [colWidths, setColWidths] = useAtom(colWidthsAtom);
  const setMaxRowWidth = useSetAtom(maxRowWidthAtom);

  // update global colWidths
  useEffect(() => {
    const rowEl = rowElRef.current;
    if (!rowEl) return;

    // get current column widths
    const currColWidths = new Map<ViewerColumn, number>();
    Array.from(rowEl.children || []).forEach((colEl) => {
      const colId = (colEl as HTMLElement).dataset.colId as ViewerColumn;
      if (!colId || colId === ViewerColumn.Message) return;
      currColWidths.set(colId, colEl.scrollWidth);
    });

    // update colWidths state (if necessary)
    setColWidths((oldVals) => {
      const changedVals = new Map<ViewerColumn, number>();
      currColWidths.forEach((currWidth, colId) => {
        const oldWidth = oldVals.get(colId);
        const newWidth = Math.max(currWidth, oldWidth || 0);
        if (newWidth !== oldWidth) changedVals.set(colId, newWidth);
      });
      if (changedVals.size) return new Map([...oldVals, ...changedVals]);
      return oldVals;
    });

    // update maxRowWidth state
    setMaxRowWidth((currVal) => Math.max(currVal, rowEl.scrollWidth));
  }, [visibleCols, setColWidths, setMaxRowWidth]);

  // first row
  if (index === 0) {
    const msg = hasMoreBefore ? 'Loading...' : 'Beginning of feed';
    return (
      <div className="px-2 leading-6" style={style}>
        {msg}
      </div>
    );
  }

  // last row (only present when hasMoreAter === true)
  if (index === items.length + 1) {
    return (
      <div className="px-2 leading-6" style={style}>
        Loading...
      </div>
    );
  }

  const record = items[index - 1];

  const els: React.ReactElement[] = [];
  ALL_VIEWER_COLUMNS.forEach((col) => {
    if (visibleCols.has(col)) {
      els.push(
        <div
          key={col}
          className={cn(
            index % 2 !== 0 && 'bg-chrome-100',
            'px-2',
            isWrap ? '' : 'whitespace-nowrap',
            col === ViewerColumn.Timestamp ? 'bg-chrome-200' : '',
            col === ViewerColumn.Message ? 'grow' : 'shrink-0',
          )}
          style={col !== ViewerColumn.Message ? { minWidth: `${colWidths.get(col) || 0}px` } : {}}
          data-col-id={col}
        >
          {getAttribute(record, col)}
        </div>,
      );
    }
  });

  const { width, ...otherStyles } = style;
  return (
    <div ref={rowElRef} className="flex leading-6" style={{ width: 'inherit', ...otherStyles }}>
      {els}
    </div>
  );
}, areEqual);

/**
 * Content component
 */

type ContentHandle = {
  scrollTo: (pos: 'first' | 'last') => void;
  autoScroll: () => void;
};

type ContentProps = {
  items: LogRecord[];
  hasMoreBefore: boolean;
  hasMoreAfter: boolean;
  loadMoreBefore: () => Promise<void>;
  loadMoreAfter: () => Promise<void>;
};

const ContentImpl: React.ForwardRefRenderFunction<ContentHandle, ContentProps> = (
  props: ContentProps,
  ref: React.ForwardedRef<ContentHandle>,
) => {
  const { hasMoreBefore, hasMoreAfter, loadMoreBefore, loadMoreAfter } = props;
  const { items } = props;

  const [isLoading, setIsLoading] = useAtom(isLoadingAtom);
  const visibleCols = useAtomValue(visibleColsAtom);
  const colWidths = useAtomValue(colWidthsAtom);
  const maxRowWidth = useAtomValue(maxRowWidthAtom);
  const isWrap = useAtomValue(isWrapAtom);

  const headerOuterElRef = useRef<HTMLDivElement>(null);
  const headerInnerElRef = useRef<HTMLDivElement>(null);

  const listRef = useRef<VariableSizeList<LogRecord>>(null);
  const listOuterRef = useRef<HTMLDivElement>(null);
  const listInnerRef = useRef<HTMLDivElement>(null);
  const infiniteLoaderRef = useRef<InfiniteLoader>(null);
  const msgHeaderColElRef = useRef<HTMLDivElement>(null);
  const sizerElRef = useRef<HTMLDivElement>(null);

  const [isListReady, setIsListReady] = useState(false);

  const isAutoScrollEnabledRef = useRef(false);
  const lastScrollTopRef = useRef(0);

  const [msgColWidth, setMsgColWidth] = useState(0);

  const nextTick = useNextTick();

  let itemCount = items.length + 1; // always add extra row before
  if (hasMoreAfter) itemCount += 1; // only add extra row if more are hidden

  // define handler api
  useImperativeHandle(ref, () => {
    const scrollTo = (pos: 'first' | 'last') => {
      if (pos === 'last') {
        isAutoScrollEnabledRef.current = true;
        listRef.current?.scrollToItem(Infinity, 'end');
      } else {
        isAutoScrollEnabledRef.current = false;
        listRef.current?.scrollToItem(0, 'end');
      }

      // Reset load cache
      infiniteLoaderRef.current?.resetloadMoreItemsCache(true);
    };

    const autoScroll = () => {
      if (isAutoScrollEnabledRef.current) scrollTo('last');
    };

    return { scrollTo, autoScroll };
  }, [isListReady]);

  // -------------------------------------------------------------------------------------
  // Loading logic
  // -------------------------------------------------------------------------------------

  // loaded-item cache logic
  const isItemLoaded = (index: number) => {
    if (index === 0 && hasMoreBefore) return false;
    if (index === itemCount - 1 && hasMoreAfter) return false;
    return true;
  };

  // load more logic
  const loadMoreItems = async (startIndex: number) => {
    if (isLoading) return;
    setIsLoading(true);

    // get current scrollPos
    const origScrollHeight = listOuterRef.current?.scrollHeight || 0;

    // load data
    if (startIndex === 0) await loadMoreBefore();
    else await loadMoreAfter();

    nextTick(() => {
      // maintain scroll position
      if (startIndex === 0 && listOuterRef.current) {
        // Scroll
        const { scrollTop, scrollHeight } = listOuterRef.current;
        listOuterRef.current.scrollTo({ top: scrollTop + (scrollHeight - origScrollHeight), behavior: 'instant' });
      }

      // reset load cache for loadMoreBefore()
      if (startIndex === 0) infiniteLoaderRef.current?.resetloadMoreItemsCache(true);

      // stop loading
      setIsLoading(false);
    });
  };

  // -------------------------------------------------------------------------------------
  // Sizing logic
  // -------------------------------------------------------------------------------------

  const handleItemSize = (index: number) => {
    const sizerEl = sizerElRef.current;
    if (!isWrap || !sizerEl) return 24;

    // placeholder rows
    if (index === 0 || index === items.length + 1) return 24;

    const record = items[index - 1];
    sizerEl.textContent = stripAnsi(record.message); // strip out ansi
    return sizerEl.clientHeight;
  };

  // trigger resize on `msgColWidth` changes
  useEffect(() => {
    listRef.current?.resetAfterIndex(0);
  }, [msgColWidth]);

  // recalculate `msgColWidth` on `isWrap` and `visibleCols` changes
  useEffect(() => {
    const msgHeaderColEl = msgHeaderColElRef.current;
    if (!msgHeaderColEl) return;

    setMsgColWidth(isWrap ? msgHeaderColEl.clientWidth : 0);
  }, [isWrap, visibleCols]);

  // handle content window dimension changes
  const debouncedHandleResize = useDebounceCallback(() => {
    const listOuterEl = listOuterRef.current;
    const listInnerEl = listInnerRef.current;
    const msgHeaderColEl = msgHeaderColElRef.current;
    if (!listOuterEl || !listInnerEl || !msgHeaderColEl) return;

    if (isWrap) setMsgColWidth(msgHeaderColEl.clientWidth);
    else listInnerEl.style.width = `${Math.max(listOuterEl.clientWidth, maxRowWidth)}px`;
  }, 20);

  // listen to content window dimension changes
  useEffect(() => {
    const listOuterEl = listOuterRef.current;
    if (!listOuterEl) return;

    const resizeObserver = new ResizeObserver(debouncedHandleResize);
    resizeObserver.observe(listOuterEl);
    return () => {
      resizeObserver.unobserve(listOuterEl);
      resizeObserver.disconnect();
    };
  }, [isWrap, maxRowWidth]);

  // update width of inner wrapper element when `maxRowWidth` changes
  useEffect(() => {
    const listInnerEl = listInnerRef.current;
    if (!listInnerEl) return;
    listInnerEl.style.width = isWrap || !maxRowWidth ? '100%' : `${maxRowWidth}px`;
  }, [isWrap, maxRowWidth]);

  // -------------------------------------------------------------------------------------
  // Scrolling logic
  // -------------------------------------------------------------------------------------

  // handle horizontal scroll on header
  const handleHeaderScrollX = useCallback((ev: React.UIEvent<HTMLDivElement>) => {
    const headerOuterEl = ev.target as HTMLDivElement;
    const listOuterEl = listOuterRef.current;
    if (!listOuterEl) return;
    listOuterEl.scrollTo({ left: headerOuterEl.scrollLeft, behavior: 'instant' });
  }, []);

  const rafIdRefHeaderX = useRef<number | null>(null);

  const handleHeaderScrollXThrottled = useCallback(
    (ev: React.UIEvent<HTMLDivElement>) => {
      if (!rafIdRefHeaderX.current) {
        rafIdRefHeaderX.current = requestAnimationFrame(() => {
          handleHeaderScrollX(ev);
          rafIdRefHeaderX.current = null;
        });
      }
    },
    [handleHeaderScrollX],
  );

  // handle horizontal scroll on content
  const handleContentScrollX = useCallback((ev: React.UIEvent<HTMLDivElement>) => {
    const listOuterEl = ev.target as HTMLDivElement;
    const headerOuterEl = headerOuterElRef.current;
    if (!headerOuterEl) return;
    headerOuterEl.scrollTo({ left: listOuterEl.scrollLeft, behavior: 'instant' });
  }, []);

  const rafIdRefContentX = useRef<number | null>(null);

  const handleContentScrollXThrottled = useCallback(
    (ev: React.UIEvent<HTMLDivElement>) => {
      if (!rafIdRefContentX.current) {
        rafIdRefContentX.current = requestAnimationFrame(() => {
          handleContentScrollX(ev);
          rafIdRefContentX.current = null;
        });
      }
    },
    [handleContentScrollX],
  );

  // handle vertical scroll on content
  const handleContentScrollY = useCallback(() => {
    const lastScrollTop = lastScrollTopRef.current;

    // Update scroll position tracker
    const el = listOuterRef.current;
    if (el) lastScrollTopRef.current = el.scrollTop;
    else return; // Exit if element not available

    const { scrollTop, clientHeight, scrollHeight } = el;

    // If scrolling up, turn off auto-scroll and exit
    if (scrollTop < lastScrollTop) {
      isAutoScrollEnabledRef.current = false;
      return;
    }

    // If scrolled to bottom, turn on auto-scroll
    const tolerance = 10;
    if (!isAutoScrollEnabledRef.current && Math.abs(scrollTop + clientHeight - scrollHeight) <= tolerance)
      isAutoScrollEnabledRef.current = true;
  }, []);

  const rafIdRefY = useRef<number | null>(null);

  const handleContentScrollYThrottled = useCallback(() => {
    if (!rafIdRefY.current) {
      rafIdRefY.current = requestAnimationFrame(() => {
        handleContentScrollY();
        rafIdRefY.current = null;
      });
    }
  }, [handleContentScrollY]);

  // attach scroll event listeners
  useEffect(() => {
    const listOuterEl = listOuterRef.current;
    if (!listOuterEl) return;
    listOuterEl.addEventListener('scroll', handleContentScrollXThrottled as any);
    return () => listOuterEl.removeEventListener('scroll', handleContentScrollXThrottled as any);
  }, [isListReady]);

  // ------------------------------------------------------------------------------------
  // Renderer
  // ------------------------------------------------------------------------------------

  return (
    <div className="h-full flex flex-col text-xs">
      <div ref={sizerElRef} className="absolute invisible font-mono leading-6 px-2" style={{ width: msgColWidth }} />
      <div
        ref={headerOuterElRef}
        className="overflow-x-scroll no-scrollbar cursor-default"
        onScroll={handleHeaderScrollXThrottled}
      >
        <div
          ref={headerInnerElRef}
          className="flex leading-[18px] border-b border-chrome-divider bg-chrome-200 *:border-r [&>*:not(:last-child)]:border-chrome-divider"
          style={{ minWidth: isWrap ? '100%' : `${maxRowWidth}px` }}
        >
          {ALL_VIEWER_COLUMNS.map((col) => {
            if (visibleCols.has(col)) {
              return (
                <div
                  key={col}
                  ref={col === ViewerColumn.Message ? msgHeaderColElRef : null}
                  className={cn('whitespace-nowrap uppercase px-2', col === ViewerColumn.Message ? 'grow' : 'shrink-0')}
                  style={col !== ViewerColumn.Message ? { minWidth: `${colWidths.get(col) || 0}px` } : {}}
                  data-col-id={col}
                >
                  {col !== ViewerColumn.ColorDot && col}
                </div>
              );
            }
            return null;
          })}
        </div>
      </div>
      <div className="grow relative">
        <AutoSizer>
          {({ height, width }) => (
            <>
              <InfiniteLoader
                ref={infiniteLoaderRef}
                isItemLoaded={isItemLoaded}
                itemCount={itemCount}
                loadMoreItems={loadMoreItems}
                threshold={20}
              >
                {({ onItemsRendered, ref: thisRef }) => (
                  <VariableSizeList
                    ref={(list) => {
                      thisRef(list);
                      // @ts-expect-error Cannot assign to 'current' because it is a read-only property.
                      listRef.current = list;
                    }}
                    className="font-mono"
                    onItemsRendered={(args) => {
                      onItemsRendered(args);
                      if (!isListReady) setIsListReady(true);
                      if (isWrap) setTimeout(() => listRef.current?.resetAfterIndex(0), 0);
                    }}
                    onScroll={handleContentScrollYThrottled}
                    height={height}
                    width={width}
                    itemCount={itemCount}
                    estimatedItemSize={24}
                    itemSize={handleItemSize}
                    outerRef={listOuterRef}
                    innerRef={listInnerRef}
                    overscanCount={20}
                    itemData={{
                      items,
                      hasMoreBefore,
                      hasMoreAfter,
                      visibleCols,
                      isWrap,
                    }}
                  >
                    {Row}
                  </VariableSizeList>
                )}
              </InfiniteLoader>
              {isLoading && <LoadingOverlay height={height} width={width} />}
            </>
          )}
        </AutoSizer>
      </div>
    </div>
  );
};

const Content = forwardRef(ContentImpl);

/**
 * Viewer component
 */

export type ViewerHandle = {
  seekHead: () => Promise<void>;
  seekTail: () => Promise<void>;
  seekTime: (sinceTS: string) => Promise<void>;
  play: () => void;
  pause: () => void;
};

type ViewerProps = {
  defaultMode: string | null;
  defaultSince: string | null;
};

const ViewerImpl: React.ForwardRefRenderFunction<ViewerHandle, ViewerProps> = (
  { defaultMode, defaultSince }: ViewerProps,
  ref: React.ForwardedRef<ViewerHandle>,
) => {
  const { useClusterAPI, kubeContext, grep, sources, sourceFilter } = useContext(Context);

  const [items, setItems] = useAtom(logRecordsAtom);
  const [hasMoreBefore, setHasMoreBefore] = useState(false);
  const [hasMoreAfter, setHasMoreAfter] = useState(false);

  const setIsLoading = useSetAtom(isLoadingAtom);
  const setIsFollow = useSetAtom(isFollowAtom);
  const nextCursorRef = useRef<any>(null);

  const fetcherRef = useRef<LogRecordsFetcherHandle>(null);
  const contentRef = useRef<ContentHandle>(null);

  const nextTick = useNextTick();

  const handleOnFollowData = (record: LogRecord) => {
    setItems((currItems) => [...currItems, record]);
    nextTick(() => contentRef.current?.autoScroll());
  };

  const handleLoadMoreBefore = async () => {
    // Fetch
    const response = await fetcherRef.current?.fetch({
      mode: LogRecordsQueryMode.Tail,
      before: nextCursorRef.current,
    });
    if (!response) return;

    // Update
    nextCursorRef.current = response.nextCursor;
    setItems((currItems) => [...response.records, ...currItems]);
    setHasMoreBefore(Boolean(response.nextCursor));
  };

  const handleLoadMoreAfter = async () => {
    // Fetch
    const response = await fetcherRef.current?.fetch({
      mode: LogRecordsQueryMode.Head,
      after: nextCursorRef.current,
    });
    if (!response) return;

    // Update
    nextCursorRef.current = response.nextCursor;
    setItems((currItems) => [...currItems, ...response.records]);
    setHasMoreAfter(Boolean(response.nextCursor));
  };

  const reset = () => {
    setItems([]);
    setHasMoreBefore(false);
    setHasMoreAfter(false);
    nextCursorRef.current = null;
  };

  // Handler
  const handle = useMemo(
    () => ({
      seekHead: async () => {
        setIsLoading(true);

        // Reset
        reset();
        fetcherRef.current?.reset();

        // Fetch
        const response = await fetcherRef.current?.fetch({ mode: LogRecordsQueryMode.Head });
        if (!response) return;

        // Update
        nextCursorRef.current = response.nextCursor;
        setItems(response.records);
        setHasMoreAfter(Boolean(response.nextCursor));

        nextTick(() => {
          contentRef.current?.scrollTo('first');
          setIsLoading(false);
        });
      },
      seekTail: async () => {
        setIsLoading(true);

        // Reset
        reset();
        fetcherRef.current?.reset();

        // Fetch
        const response = await fetcherRef.current?.fetch({ mode: LogRecordsQueryMode.Tail });
        if (!response) return;

        // Update
        nextCursorRef.current = response.nextCursor;
        setItems(response.records);
        setHasMoreBefore(Boolean(response.nextCursor));

        nextTick(() => {
          contentRef.current?.scrollTo('last');
          setIsLoading(false);
        });
      },
      seekTime: async (sinceTS: string) => {
        setIsLoading(true);

        // Reset
        reset();
        fetcherRef.current?.reset();

        // Fetch
        const response = await fetcherRef.current?.fetch({
          mode: LogRecordsQueryMode.Head,
          since: sinceTS,
        });
        if (!response) return;

        // Update
        nextCursorRef.current = response.nextCursor;
        setItems(response.records);
        setHasMoreAfter(Boolean(response.nextCursor));

        nextTick(() => {
          contentRef.current?.scrollTo('first');
          setIsLoading(false);
        });
      },
      play: () => {
        setIsFollow(true);
      },
      pause: () => {
        setIsFollow(false);
      },
    }),
    [],
  );

  // Expose handler
  useImperativeHandle(ref, () => handle, [handle]);

  // Handle default
  useEffect(() => {
    switch (defaultMode) {
      case 'head':
        handle.seekHead();
        break;
      case 'time':
        handle.seekTime(defaultSince || '');
        break;
      default:
        handle.seekTail();
        break;
    }
  }, [kubeContext, grep, sources, sourceFilter]);

  return (
    <>
      <LogRecordsFetcher
        ref={fetcherRef}
        useClusterAPI={useClusterAPI}
        kubeContext={kubeContext}
        sources={sources}
        sourceFilter={sourceFilter}
        grep={grep}
        onFollowData={handleOnFollowData}
      />
      <Content
        ref={contentRef}
        items={items}
        hasMoreBefore={hasMoreBefore}
        hasMoreAfter={hasMoreAfter}
        loadMoreBefore={handleLoadMoreBefore}
        loadMoreAfter={handleLoadMoreAfter}
      />
    </>
  );
};

export const Viewer = forwardRef(ViewerImpl);

/**
 * ViewerProvider component
 */

type ViewerProviderProps = {
  kubeContext: string | null;
  sources: string[];
  sourceFilter: LogSourceFilter;
  grep: string | null;
};

export const ViewerProvider = ({
  kubeContext,
  sources,
  sourceFilter,
  grep,
  children,
}: React.PropsWithChildren<ViewerProviderProps>) => {
  const useClusterAPI = useIsClusterAPIEnabled(kubeContext);

  const context = useMemo(
    () => ({
      useClusterAPI,
      kubeContext,
      sources,
      sourceFilter,
      grep,
    }),
    [useClusterAPI, kubeContext, grep, sources, sourceFilter],
  );

  if (useClusterAPI === undefined) return <LoadingPage />;

  return <Context.Provider value={context}>{children}</Context.Provider>;
};
