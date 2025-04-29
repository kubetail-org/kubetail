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

import { useQuery, useSubscription } from '@apollo/client';
import { format, toZonedTime } from 'date-fns-tz';
import { stripAnsi } from 'fancy-ansi';
import { AnsiHtml } from 'fancy-ansi/react';
import { RecoilRoot, atom, useRecoilState, useRecoilValue, useSetRecoilState } from 'recoil';
import { createContext, forwardRef, memo, useContext, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react';
import AutoSizer from 'react-virtualized-auto-sizer';
import { VariableSizeList, areEqual } from 'react-window';
import InfiniteLoader from 'react-window-infinite-loader';
import { useDebounceCallback } from 'usehooks-ts';

import Spinner from '@kubetail/ui/elements/Spinner';

import { ConsoleNodesListItemFragmentFragment, LogRecordsFragmentFragment as LogRecord, LogRecordsQueryMode, LogSourceFilter, LogSourceFragmentFragment } from '@/lib/graphql/dashboard/__generated__/graphql';
import * as dashboardOps from '@/lib/graphql/dashboard/ops';
import { useIsClusterAPIEnabled, useListQueryWithSubscription } from '@/lib/hooks';
import { Counter, MapSet, cn, cssEncode } from '@/lib/util';
import { getClusterAPIClient } from '@/apollo-client';

type ContextType = {
  kubeContext: string | null;
  sources: string[];
  sourceFilter: LogSourceFilter;
  grep: string | null;
};

const Context = createContext<ContextType>({} as ContextType);

/**
 * Shared types
 */

export enum ViewerColumn {
  Timestamp = 'Timestamp',
  ColorDot = 'Color Dot',
  PodContainer = 'Pod/Container',
  Region = 'Region',
  Zone = 'Zone',
  OS = 'OS',
  Arch = 'Arch',
  Node = 'Node',
  Message = 'Message',
}

export const allViewerColumns = [
  ViewerColumn.Timestamp,
  ViewerColumn.ColorDot,
  ViewerColumn.PodContainer,
  ViewerColumn.Region,
  ViewerColumn.Zone,
  ViewerColumn.OS,
  ViewerColumn.Arch,
  ViewerColumn.Node,
  ViewerColumn.Message,
];

/**
 * State
 */

const logRecordsState = atom({
  key: 'logFeedLogRecords',
  default: new Array<LogRecord>(),
});

const isReadyState = atom({
  key: 'logFeedIsReady',
  default: false,
});

const isLoadingState = atom({
  key: 'logFeedIsLoading',
  default: true,
});

const isFollowState = atom({
  key: 'logFeedIsFollow',
  default: true,
});

const visibleColsState = atom({
  key: 'logFeedVisibleCols',
  default: new Set([ViewerColumn.Timestamp, ViewerColumn.ColorDot, ViewerColumn.Message]),
});

const isWrapState = atom({
  key: 'logFeedIsWrap',
  default: false,
});

const colWidthsState = atom({
  key: 'logFeedColWidths',
  default: new Map<ViewerColumn, number>(),
});

const maxRowWidthState = atom({
  key: 'logFeedMaxRowWidth',
  default: 0,
});

const filtersState = atom({
  key: 'logFeedFilters',
  default: new MapSet<string, string>(),
});

/**
 * Hooks
 */

export function useNodes() {
  const { kubeContext } = useContext(Context);

  const { fetching, data } = useListQueryWithSubscription({
    query: dashboardOps.CONSOLE_NODES_LIST_FETCH,
    subscription: dashboardOps.CONSOLE_NODES_LIST_WATCH,
    queryDataKey: 'coreV1NodesList',
    subscriptionDataKey: 'coreV1NodesWatch',
    variables: { kubeContext: kubeContext || '' },
  });

  const loading = fetching; // treat still-fetching as still-loading
  const nodes = (data?.coreV1NodesList?.items) ? data.coreV1NodesList.items : [] as ConsoleNodesListItemFragmentFragment[];

  return { loading, nodes };
}

export const useSources = () => {
  const { kubeContext, sources } = useContext(Context);
  const [sourceMap, setSourceMap] = useState(new Map<string, LogSourceFragmentFragment>());

  const { loading } = useSubscription(dashboardOps.LOG_SOURCES_WATCH, {
    variables: { kubeContext, sources },
    onData: ({ data }) => {
      const ev = data.data?.logSourcesWatch;
      if (!ev) return;

      const source = ev?.object;
      if (!source) return;

      const k = `${source.namespace}/${source.podName}/${source.containerName}`;
      setSourceMap((prevMap) => {
        const newMap = new Map(prevMap);
        if (ev?.type === 'ADDED') newMap.set(k, source);
        else if (ev?.type === 'DELETED') newMap.delete(k);
        return newMap;
      });
    },
  });

  return { loading, sources: Array.from(sourceMap.values()) };
};

export const useViewerIsWrap = () => useRecoilState(isWrapState);

export const useViewerMetadata = () => {
  const isReady = useRecoilValue(isReadyState);
  const isLoading = useRecoilValue(isLoadingState);
  const isFollow = useRecoilValue(isFollowState);
  return { isReady, isLoading, isFollow };
};

export const useViewerVisibleCols = () => useRecoilState(visibleColsState);

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

export const useViewerFilters = () => useRecoilState(filtersState);

/**
 * Loading overlay
 */

const LoadingOverlay = ({ height, width }: { height: number; width: number; }) => (
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
      const el = (
        <div
          className="inline-block w-[8px] h-[8px] rounded-full"
          style={{ backgroundColor: `var(--${k}-color)` }}
        />
      );
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

const Row = memo(
  ({ index, style, data }: RowProps) => {
    const { items, hasMoreBefore, visibleCols, isWrap } = data;

    const rowElRef = useRef<HTMLDivElement>(null);
    const [colWidths, setColWidths] = useRecoilState(colWidthsState);
    const setMaxRowWidth = useSetRecoilState(maxRowWidthState);

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
    }, [visibleCols]);

    // first row
    if (index === 0) {
      const msg = (hasMoreBefore) ? 'Loading...' : 'Beginning of feed';
      return <div className="px-[8px] leading-[24px]" style={style}>{msg}</div>;
    }

    // last row (only present when hasMoreAter === true)
    if (index === (items.length + 1)) {
      return <div className="px-[8px] leading-[24px]" style={style}>Loading...</div>;
    }

    const record = items[index - 1];

    const els: JSX.Element[] = [];
    allViewerColumns.forEach((col) => {
      if (visibleCols.has(col)) {
        els.push((
          <div
            key={col}
            className={cn(
              index % 2 !== 0 && 'bg-chrome-100',
              'px-[8px]',
              (isWrap) ? '' : 'whitespace-nowrap',
              (col === ViewerColumn.Timestamp) ? 'bg-chrome-200' : '',
              (col === ViewerColumn.Message) ? 'flex-grow' : 'shrink-0',
            )}
            style={(col !== ViewerColumn.Message) ? { minWidth: `${(colWidths.get(col) || 0)}px` } : {}}
            data-col-id={col}
          >
            {getAttribute(record, col)}
          </div>
        ));
      }
    });

    const { width, ...otherStyles } = style;
    return (
      <div
        ref={rowElRef}
        className="flex leading-[24px]"
        style={{ width: 'inherit', ...otherStyles }}
      >
        {els}
      </div>
    );
  },
  areEqual,
);

/**
 * Content component
 */

type ContentHandle = {
  scrollTo: (pos: 'first' | 'last', callback?: () => void) => void;
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

  const [isLoading, setIsLoading] = useRecoilState(isLoadingState);
  const visibleCols = useRecoilValue(visibleColsState);
  const colWidths = useRecoilValue(colWidthsState);
  const maxRowWidth = useRecoilValue(maxRowWidthState);
  const isWrap = useRecoilValue(isWrapState);

  const headerOuterElRef = useRef<HTMLDivElement>(null);
  const headerInnerElRef = useRef<HTMLDivElement>(null);

  const listRef = useRef<VariableSizeList<LogRecord>>(null);
  const listOuterRef = useRef<HTMLDivElement>(null);
  const listInnerRef = useRef<HTMLDivElement>(null);
  const infiniteLoaderRef = useRef<InfiniteLoader>(null);
  const msgHeaderColElRef = useRef<HTMLDivElement>(null);
  const sizerElRef = useRef<HTMLDivElement>(null);

  const [isListReady, setIsListReady] = useState(false);

  const scrollToRef = useRef<'first' | 'last' | null>(null);
  const scrollToCallbackRef = useRef<(() => void)>();
  const [scrollToTrigger, setScrollToTrigger] = useState(0);

  const isAutoScrollRef = useRef(true);
  const isProgrammaticScrollRef = useRef(false);

  const [msgColWidth, setMsgColWidth] = useState(0);

  let itemCount = items.length + 1; // always add extra row before
  if (hasMoreAfter) itemCount += 1; // only add extra row if more are hidden

  // define handler api
  useImperativeHandle(ref, () => {
    const scrollTo = (pos: 'first' | 'last', callback?: () => void) => {
      // update autoscroll
      if (pos === 'last') isAutoScrollRef.current = true;
      else isAutoScrollRef.current = false;

      scrollToRef.current = pos;
      scrollToCallbackRef.current = callback;
      setScrollToTrigger(Math.random());
    };

    return {
      scrollTo,
      autoScroll: () => {
        if (isAutoScrollRef.current) scrollTo('last');
      },
    };
  }, [isListReady]);

  // -------------------------------------------------------------------------------------
  // Loading logic
  // -------------------------------------------------------------------------------------

  // loaded-item cache logic
  const isItemLoaded = (index: number) => {
    if (index === 0 && hasMoreBefore) return false;
    if (index === (itemCount - 1) && hasMoreAfter) return false;
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

    setTimeout(() => {
      // maintain scroll position
      if (startIndex === 0 && listOuterRef.current) {
        const { scrollTop, scrollHeight } = listOuterRef.current;
        listOuterRef.current.scrollTo({ top: scrollTop + (scrollHeight - origScrollHeight), behavior: 'instant' });
      }

      // reset load cache for loadMoreBefore()
      if (startIndex === 0) infiniteLoaderRef.current?.resetloadMoreItemsCache(true);

      // stop loading
      setIsLoading(false);
    }, 0);
  };

  // -------------------------------------------------------------------------------------
  // Sizing logic
  // -------------------------------------------------------------------------------------

  const handleItemSize = (index: number) => {
    const sizerEl = sizerElRef.current;
    if (!isWrap || !sizerEl) return 24;

    // placeholder rows
    if (index === 0 || index === (items.length + 1)) return 24;

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
    return () => resizeObserver.unobserve(listOuterEl);
  }, [isWrap, maxRowWidth]);

  // update width of inner wrapper element when `maxRowWidth` changes
  useEffect(() => {
    const listInnerEl = listInnerRef.current;
    if (!listInnerEl) return;
    listInnerEl.style.width = (isWrap || !maxRowWidth) ? '100%' : `${maxRowWidth}px`;
  }, [isWrap, maxRowWidth]);

  // -------------------------------------------------------------------------------------
  // Scrolling logic
  // -------------------------------------------------------------------------------------

  // handle horizontal scroll on header
  const handleHeaderScrollX = (ev: React.UIEvent<HTMLDivElement>) => {
    const headerOuterEl = ev.target as HTMLDivElement;
    const listOuterEl = listOuterRef.current;
    if (!listOuterEl) return;
    listOuterEl.scrollTo({ left: headerOuterEl.scrollLeft, behavior: 'instant' });
  };

  // handle horizontal scroll on content
  const handleContentScrollX = (ev: React.UIEvent<HTMLDivElement>) => {
    const listOuterEl = ev.target as HTMLDivElement;
    const headerOuterEl = headerOuterElRef.current;
    if (!headerOuterEl) return;
    headerOuterEl.scrollTo({ left: listOuterEl.scrollLeft, behavior: 'instant' });
  };

  // handle vertical scroll on content
  const handleContentScrollY = () => {
    const el = listOuterRef.current;
    if (el && !isProgrammaticScrollRef.current) {
      const tolerance = 20;
      const { scrollTop, clientHeight, scrollHeight } = el;
      if (Math.abs((scrollTop + clientHeight) - scrollHeight) <= tolerance) {
        isAutoScrollRef.current = true;
      } else {
        isAutoScrollRef.current = false;
      }
    }
  };

  // attach scroll event listeners
  useEffect(() => {
    const listOuterEl = listOuterRef.current;
    if (!listOuterEl) return;
    listOuterEl.addEventListener('scroll', handleContentScrollX as any);
    return () => listOuterEl.removeEventListener('scroll', handleContentScrollX as any);
  }, [isListReady]);

  // -------------------------------------------------------------------------------------
  // Miscellaneous
  // -------------------------------------------------------------------------------------

  useEffect(() => {
    if (scrollToRef.current) {
      isProgrammaticScrollRef.current = true;

      // perform scroll and reset
      const index = (scrollToRef.current === 'last') ? items.length : 1;
      listRef.current?.scrollToItem(index);
      scrollToRef.current = null;

      const callback = scrollToCallbackRef.current;
      scrollToCallbackRef.current = undefined;

      setTimeout(() => {
        // reset load cache
        infiniteLoaderRef.current?.resetloadMoreItemsCache(true);

        // execute callback
        if (callback) callback();

        isProgrammaticScrollRef.current = false;
      }, 0);
    }
  }, [scrollToTrigger]);

  // ------------------------------------------------------------------------------------
  // Renderer
  // ------------------------------------------------------------------------------------

  return (
    <div className="h-full flex flex-col text-xs">
      <div
        ref={sizerElRef}
        className="absolute invisible font-mono leading-[24px] px-[8px]"
        style={{ width: msgColWidth }}
      />
      <div
        ref={headerOuterElRef}
        className="overflow-x-scroll no-scrollbar cursor-default"
        onScroll={handleHeaderScrollX}
      >
        <div
          ref={headerInnerElRef}
          className="flex leading-[18px] border-b border-chrome-divider bg-chrome-200 [&>*]:border-r [&>*:not(:last-child)]:border-chrome-divider"
          style={{ minWidth: (isWrap) ? '100%' : `${maxRowWidth}px` }}
        >
          {allViewerColumns.map((col) => {
            if (visibleCols.has(col)) {
              return (
                <div
                  key={col}
                  ref={(col === ViewerColumn.Message) ? msgHeaderColElRef : null}
                  className={cn(
                    'whitespace-nowrap uppercase px-[8px]',
                    (col === ViewerColumn.Message) ? 'flex-grow' : 'shrink-0',
                  )}
                  style={(col !== ViewerColumn.Message) ? { minWidth: `${colWidths.get(col) || 0}px` } : {}}
                  data-col-id={col}
                >
                  {(col !== ViewerColumn.ColorDot) && col}
                </div>
              );
            }
            return null;
          })}
        </div>
      </div>
      <div className="flex-grow relative">
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
                    }}
                    onScroll={handleContentScrollY}
                    height={height}
                    width={width}
                    itemCount={itemCount}
                    estimatedItemSize={24}
                    itemSize={handleItemSize}
                    outerRef={listOuterRef}
                    innerRef={listInnerRef}
                    overscanCount={20}
                    itemData={{ items, hasMoreBefore, hasMoreAfter, visibleCols, isWrap }}
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
 * LogRecordsFetcher component
 */

type LogRecordsFetchOptions = {
  mode: LogRecordsQueryMode;
  since?: string;
  after?: string | null;
  before?: string | null;
};

type LogRecordsFetchResponse = {
  records: LogRecord[];
  nextCursor: string | null;
};

type LogRecordsFetcherHandle = {
  fetch: (opts: LogRecordsFetchOptions) => Promise<LogRecordsFetchResponse>;
  reset: () => void;
};

type LogRecordsFetcherProps = {
  onFollowData: (record: LogRecord) => void;
};

const LogRecordsFetcherImpl: React.ForwardRefRenderFunction<LogRecordsFetcherHandle, LogRecordsFetcherProps> = (
  { onFollowData }: LogRecordsFetcherProps,
  ref: React.ForwardedRef<LogRecordsFetcherHandle>,
) => {
  const { kubeContext, sources, sourceFilter, grep } = useContext(Context);
  const isFollow = useRecoilValue(isFollowState);

  const [isReachedEnd, setIsReachedEnd] = useState(false);
  const lastTS = useRef<string>();

  const batchSize = 300;

  const isClusterAPIEnabled = useIsClusterAPIEnabled();
  const connectArgs = {
    kubeContext: kubeContext || '',
    namespace: 'kubetail-system',
    serviceName: 'kubetail-cluster-api',
  };
  const client = isClusterAPIEnabled ? getClusterAPIClient(connectArgs) : undefined;

  // Initialize query
  const query = useQuery(dashboardOps.LOG_RECORDS_FETCH, {
    client,
    skip: true,
    variables: { kubeContext, sources, sourceFilter, grep, limit: batchSize + 1 },
  });

  // Expose handler
  useImperativeHandle(ref, () => ({
    fetch: async (opts: LogRecordsFetchOptions) => {
      // Reset previous refetch() args
      const newOpts = { after: undefined, before: undefined, since: undefined, ...opts };

      // Execute query
      const response = (await query.refetch(newOpts)).data.logRecordsFetch;
      if (!response) throw new Error('query response is null');

      let records: LogRecord[] = [];
      let nextCursor: string | null = null;

      // Handle response
      switch (opts.mode) {
        case LogRecordsQueryMode.Head:
          records = response.records.slice(0, batchSize);
          if (response.records.length > batchSize) nextCursor = records[records.length - 1].timestamp;
          setIsReachedEnd(!nextCursor);
          break;
        case LogRecordsQueryMode.Tail:
          records = response.records.slice(Math.max(response.records.length - batchSize, 0));
          if (response.records.length > batchSize) nextCursor = records[0].timestamp;
          setIsReachedEnd(true);
          break;
        default:
          throw new Error('not implemented');
      }

      // Update last TS
      if (records.length) lastTS.current = records[records.length - 1].timestamp;

      return { records, nextCursor };
    },
    reset: () => {
      lastTS.current = undefined;
      setIsReachedEnd(false);
    },
  }), [kubeContext, JSON.stringify(sources)]);

  // Follow
  useEffect(() => {
    if (!isReachedEnd || !isFollow) return;

    return query.subscribeToMore({
      document: dashboardOps.LOG_RECORDS_FOLLOW,
      variables: { kubeContext, sources, sourceFilter, grep, after: lastTS.current },
      updateQuery: (_, { subscriptionData }) => {
        const { data: { logRecordsFollow: record } } = subscriptionData;
        if (record) {
          // Update last TS
          lastTS.current = record.timestamp;

          // Execute callback
          onFollowData(record);
        }
        return { logRecordsFetch: null };
      },
    });
  }, [kubeContext, JSON.stringify(sources), isReachedEnd, isFollow, query.subscribeToMore]);

  return null;
};

const LogRecordsFetcher = forwardRef(LogRecordsFetcherImpl);

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
  {
    defaultMode,
    defaultSince,
  }: ViewerProps,
  ref: React.ForwardedRef<ViewerHandle>,
) => {
  const { kubeContext, grep, sources, sourceFilter } = useContext(Context);

  const [items, setItems] = useRecoilState(logRecordsState);
  const [hasMoreBefore, setHasMoreBefore] = useState(false);
  const [hasMoreAfter, setHasMoreAfter] = useState(false);

  const setIsLoading = useSetRecoilState(isLoadingState);
  const setIsFollow = useSetRecoilState(isFollowState);
  const nextCursorRef = useRef<any>(null);

  const fetcherRef = useRef<LogRecordsFetcherHandle>(null);
  const contentRef = useRef<ContentHandle>(null);

  const handleOnFollowData = (record: LogRecord) => {
    setItems((currItems) => [...currItems, record]);
    contentRef.current?.autoScroll();
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
  const handle = useMemo(() => ({
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

      contentRef.current?.scrollTo('first', () => setIsLoading(false));
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

      contentRef.current?.scrollTo('last', () => setIsLoading(false));
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

      contentRef.current?.scrollTo('first', () => setIsLoading(false));
    },
    play: () => {
      setIsFollow(true);
    },
    pause: () => {
      setIsFollow(false);
    },
  }), []);

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
  }, [kubeContext, grep, JSON.stringify(sources), JSON.stringify(sourceFilter)]);

  return (
    <>
      <LogRecordsFetcher
        ref={fetcherRef}
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
 * Provider component
 */

type ProviderProps = {
  kubeContext: string | null;
  sources: string[];
  sourceFilter: LogSourceFilter;
  grep: string | null;
};

export const Provider = ({
  kubeContext,
  sources,
  sourceFilter,
  grep,
  children,
}: React.PropsWithChildren<ProviderProps>) => {
  const context = useMemo(() => ({
    kubeContext,
    sources,
    sourceFilter,
    grep,
  }), [kubeContext, grep, JSON.stringify(sources), JSON.stringify(sourceFilter)]);

  return (
    <Context.Provider value={context}>
      <RecoilRoot>
        {children}
      </RecoilRoot>
    </Context.Provider>
  );
};
