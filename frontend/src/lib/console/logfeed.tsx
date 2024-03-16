// Copyright 2024 Andres Morey
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
import { AnsiUp } from 'ansi_up';
import { format, utcToZonedTime } from 'date-fns-tz';
import makeAnsiRegex from 'ansi-regex';
import { createRef, forwardRef, memo, useEffect, useImperativeHandle, useRef, useState } from 'react';
import AutoSizer from 'react-virtualized-auto-sizer';
import { FixedSizeList } from 'react-window';
import InfiniteLoader from 'react-window-infinite-loader';
import { atom, useRecoilState, useRecoilValue, useSetRecoilState } from 'recoil';

import Spinner from 'kubetail-ui/elements/Spinner';

import { Counter, MapSet, intersectSets } from '@/lib/helpers';
import type {
  LogRecord as GraphQLLogRecord,
  PageInfo,
  PodLogQueryResponse as GraphQLPodLogQueryResponse,
} from '@/lib/graphql/__generated__/graphql';
import * as ops from '@/lib/graphql/ops';
import { cn } from '@/lib/utils';

import { cssID } from './helpers';
import { useNodes, usePods } from './logging-resources';
import type { Node, Pod } from './logging-resources';

const ansiUp = new AnsiUp();
const ansiRegex = makeAnsiRegex({ onlyFirst: true });

/**
 * Shared types
 */

export enum LogFeedColumn {
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

export const allLogFeedColumns = [
  LogFeedColumn.Timestamp,
  LogFeedColumn.ColorDot,
  LogFeedColumn.PodContainer,
  LogFeedColumn.Region,
  LogFeedColumn.Zone,
  LogFeedColumn.OS,
  LogFeedColumn.Arch,
  LogFeedColumn.Node,
  LogFeedColumn.Message,
];

type LogFeedHeadOptions = {
  after?: string | null;
  since?: string;
  first?: number;
};

type LogFeedTailOptions = {
  before?: string;
  last?: number;
};

export enum LogFeedState {
  Streaming = 'STREAMING',
  Paused = 'PAUSED',
  InQuery = 'IN_QUERY',
}

interface LogRecord extends GraphQLLogRecord {
  node: Node;
  pod: Pod;
  container: string;
};

interface PodLogQueryResponse extends GraphQLPodLogQueryResponse {
  results: LogRecord[];
}

interface BaseCommand {
  type: string;
}

interface HeadCommand extends BaseCommand {
  type: 'head';
}

interface TailCommand extends BaseCommand {
  type: 'tail';
}

interface SeekCommand extends BaseCommand {
  type: 'seek';
  sinceTS: string;
}

interface PlayCommand extends BaseCommand {
  type: 'play';
}

interface PauseCommand extends BaseCommand {
  type: 'pause';
}

type Command = HeadCommand | TailCommand | SeekCommand | PlayCommand | PauseCommand;

/**
 * State
 */

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

const logRecordsState = atom({
  key: 'logFeedLogRecords',
  default: new Array<LogRecord>(),
});

const visibleColsState = atom({
  key: 'logFeedVisibleCols',
  default: new Set([LogFeedColumn.Timestamp, LogFeedColumn.ColorDot, LogFeedColumn.Message]),
});

const filtersState = atom({
  key: 'logFeedFilters',
  default: new MapSet<string, string>(),
});

const controlChannelIDState = atom<string | undefined>({
  key: 'logFeedControlChannelID',
  default: undefined,
});

/**
 * Hooks
 */

export const useLogFeedControls = () => {
  const channelID = useRecoilValue(controlChannelIDState);

  const postMessage = (command: Command) => {
    if (!channelID) return;
    const bc = new BroadcastChannel(channelID);
    bc.postMessage(command);
    bc.close();
  }

  return {
    tail: () => {
      postMessage({ type: 'tail' });
    },
    head: () => {
      postMessage({ type: 'head' });
    },
    seek: (sinceTS: string) => {
      postMessage({ type: 'seek', sinceTS });
    },
    play: () => {
      postMessage({ type: 'play' });
    },
    pause: () => {
      postMessage({ type: 'pause' });
    },
  };
};

export const useLogFeedMetadata = () => {
  const isReady = useRecoilValue(isReadyState);
  const isLoading = useRecoilValue(isLoadingState);
  const isFollow = useRecoilValue(isFollowState);
  return { isReady, isLoading, isFollow };
};

export function useLogFeedVisibleCols() {
  return useRecoilState(visibleColsState);
}

export function useLogFeedFacets() {
  const { pods } = usePods();
  const { nodes } = useNodes();

  // count pods per node
  const nodeVals: string[] = [];
  pods?.forEach(pod => nodeVals.push(pod.spec.nodeName));
  const nodeCounts = new Counter(nodeVals);

  // count pods per node facets
  const regionCounts = new Counter();
  const zoneCounts = new Counter();
  const archCounts = new Counter();
  const osCounts = new Counter();

  nodes?.forEach(node => {
    const count = nodeCounts.get(node.metadata.name) || 0;
    if (!count) return;

    const labels = node.metadata.labels;

    const region = labels['topology.kubernetes.io/region'];
    if (region) regionCounts.update(region, count);

    const zone = labels['topology.kubernetes.io/zone'];
    if (zone) zoneCounts.update(zone, count);

    const os = labels['kubernetes.io/os'];
    if (os) osCounts.update(os, count);

    const arch = labels['kubernetes.io/arch'];
    if (arch) archCounts.update(arch, count)
  });

  return {
    region: regionCounts,
    zone: zoneCounts,
    os: osCounts,
    arch: archCounts,
    node: nodeCounts,
  };
}

export function useLogFeedFilters() {
  return useRecoilState(filtersState);
}

function useAllowedContainers(): Set<string> | undefined {
  const pods = usePods();
  const nodes = useNodes();
  const filters = useRecoilValue(filtersState);

  // exit early if still loading resources
  if (pods.loading || nodes.loading) return undefined;

  // exit early if no filters specified
  if (!filters.size) return undefined;

  // map nodes to containers
  const nodesToContainersIDX = new MapSet();
  pods.pods?.forEach(pod => {
    pod.spec.containers.forEach(container => {
      nodesToContainersIDX.add(pod.spec.nodeName, `${pod.metadata.namespace}/${pod.metadata.name}/${container.name}`);
    });
  });

  // map facets to nodes
  const facetsToNodesIDX = new MapSet();
  nodes.nodes?.forEach(node => {
    const { name, labels } = node.metadata;

    // skip if no pods on node
    if (!nodesToContainersIDX.has(name)) return;

    facetsToNodesIDX.add(`node:${name}`, name);

    const region = labels['topology.kubernetes.io/region'];
    if (region) facetsToNodesIDX.add(`region:${region}`, name);

    const zone = labels['topology.kubernetes.io/zone'];
    if (zone) facetsToNodesIDX.add(`zone:${zone}`, name);

    const os = labels['kubernetes.io/os'];
    if (os) facetsToNodesIDX.add(`os:${os}`, name);

    const arch = labels['kubernetes.io/arch'];
    if (arch) facetsToNodesIDX.add(`arch:${arch}`, name);
  });

  // get allowed containers from each filter
  const allowedContainerSets = new Array<Set<string>>();

  // @ts-ignore
  if (filters.has('container')) allowedContainerSets.push(filters.get('container'));

  ['node', 'region', 'zone', 'os', 'arch'].forEach(key => {
    const containers = new Array<string>();
    filters.get(key)?.forEach(val => {
      facetsToNodesIDX.get(`${key}:${val}`)?.forEach(node => {
        Array.prototype.push.apply(containers, Array.from(nodesToContainersIDX.get(node) || []))
      });
    });
    if (containers.length) allowedContainerSets.push(new Set(containers));
  });

  return intersectSets(allowedContainerSets);
}

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
 * LogFeedViewer component
 */

const getAttribute = (record: LogRecord, col: LogFeedColumn) => {
  switch (col) {
    case LogFeedColumn.Timestamp:
      const tsWithTZ = utcToZonedTime(record.timestamp, 'UTC');
      return format(tsWithTZ, 'LLL dd, y HH:mm:ss.SSS', { timeZone: 'UTC' });
    case LogFeedColumn.ColorDot:
      const k = cssID(record.pod, record.container);
      const el = (
        <div
          className="inline-block w-[8px] h-[8px] rounded-full"
          style={{ backgroundColor: `var(--${k}-color)` }}
        />
      );
      return el;
    case LogFeedColumn.PodContainer:
      return `${record.pod.metadata.name}/${record.container}`;
    case LogFeedColumn.Region:
      return record.node.metadata.labels['topology.kubernetes.io/region'];
    case LogFeedColumn.Zone:
      return record.node.metadata.labels['topology.kubernetes.io/zone'];
    case LogFeedColumn.OS:
      return record.node.metadata.labels['kubernetes.io/os'];
    case LogFeedColumn.Arch:
      return record.node.metadata.labels['kubernetes.io/arch'];
    case LogFeedColumn.Node:
      return record.pod.spec.nodeName;
    case LogFeedColumn.Message:
      // apply ansi color coding
      if (ansiRegex.test(record.message)) {
        return (
          <span dangerouslySetInnerHTML={{ __html: ansiUp.ansi_to_html(record.message) }} />
        );
      } else {
        return record.message;
      }
    default:
      throw new Error('not implemented');
  }
};

type RowData = {
  items: LogRecord[];
  hasMoreBefore: boolean;
  hasMoreAfter: boolean;
  minColWidths: Map<LogFeedColumn, number>;
  visibleCols: Set<string>;
}

type RowProps = {
  index: any;
  style: any;
  data: RowData;
};

const Row = memo(
  ({ index, style, data }: RowProps) => {
    const { items, hasMoreBefore, visibleCols, minColWidths } = data;

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
    allLogFeedColumns.forEach(col => {
      if (visibleCols.has(col)) {
        els.push((
          <div
            key={col}
            className={cn(
              index % 2 !== 0 && 'bg-chrome-100',
              'whitespace-nowrap px-[8px]',
              (col === LogFeedColumn.Timestamp) ? 'bg-chrome-200' : '',
              (col === LogFeedColumn.Message) ? 'flex-grow' : 'shrink-0',
            )}
            style={(col !== LogFeedColumn.Message) ? { minWidth: `${(minColWidths.get(col) || 0)}px` } : {}}
            data-col-id={col}
          >
            {getAttribute(record, col)}
          </div>
        ));
      }
    })

    const { width, ...otherStyles } = style;
    return (
      <div className="flex leading-[24px]" style={{ width: 'inherit', ...otherStyles }}>
        {els}
      </div>
    );
  }
);

type LogFeedContentHandle = {
  scrollTo: (pos: 'first' | 'last') => void;
  autoScroll: () => void;
};

type LogFeedContentProps = {
  items: LogRecord[];
  hasMoreBefore: boolean;
  hasMoreAfter: boolean;
  loadMoreBefore: () => Promise<void>;
  loadMoreAfter: () => Promise<void>;
}

const LogFeedContentImpl: React.ForwardRefRenderFunction<LogFeedContentHandle, LogFeedContentProps> = (props, ref) => {
  const { hasMoreBefore, hasMoreAfter, loadMoreBefore, loadMoreAfter } = props;
  let { items } = props;

  const [isLoading, setIsLoading] = useRecoilState(isLoadingState);
  const visibleCols = useRecoilValue(visibleColsState);
  const allowedContainers = useAllowedContainers();

  const headerOuterElRef = useRef<HTMLDivElement>(null);
  const headerInnerElRef = useRef<HTMLDivElement>(null);

  const listRef = useRef<FixedSizeList<LogRecord> | null>(null);
  const listOuterRef = useRef<HTMLDivElement | null>(null);
  const listInnerRef = useRef<HTMLDivElement | null>(null);
  const infiniteLoaderRef = useRef<InfiniteLoader | null>(null);

  const [isListReady, setIsListReady] = useState(false);

  const scrollToRef = useRef<'first' | 'last' | null>(null);
  const [scrollToTrigger, setScrollToTrigger] = useState(0);

  const [maxWidth, setMaxWidth] = useState<number | string>('100%');
  const [minColWidths, setMinColWidths] = useState<Map<LogFeedColumn, number>>(new Map());

  const isAutoScrollRef = useRef(true);
  const isProgrammaticScrollRef = useRef(false);

  // apply filter
  items = items.filter(item => {
    const { pod } = item;
    const { namespace, name } = pod.metadata;
    const k = `${namespace}/${name}/${item.container}`;
    if (allowedContainers && !allowedContainers.has(k)) return false;
    return true;
  });

  let itemCount = items.length + 1; // always add extra row before
  if (hasMoreAfter) itemCount += 1; // only add extra row if more are hidden

  // define handler api
  useImperativeHandle(ref, () => {
    const scrollTo = (pos: 'first' | 'last') => {
      // update autoscroll
      if (pos === 'last') isAutoScrollRef.current = true;
      else isAutoScrollRef.current = false;

      scrollToRef.current = pos;
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
      const tolerance = 10;
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

  // handle items rendered
  const handleItemsRendered = () => {
    // set isListReady
    if (!isListReady) setIsListReady(true);

    // get max row and col widths
    let maxRowWidth = 0;
    let minColWidthsChanged = false;
    Array.from(listInnerRef.current?.children || []).forEach(rowEl => {
      maxRowWidth = Math.max(maxRowWidth, rowEl.scrollWidth);

      Array.from(rowEl.children || []).forEach(colEl => {
        const colId = (colEl as HTMLElement).dataset.colId as LogFeedColumn;
        if (!colId) return;
        const currVal = minColWidths.get(colId) || 0;
        const newVal = Math.max(currVal, colEl.scrollWidth);
        if (newVal !== currVal) minColWidths.set(colId, newVal);
      });
    });

    // adjust list inner
    if (listInnerRef.current) listInnerRef.current.style.width = `${maxRowWidth}px`;

    if (minColWidthsChanged) setMinColWidths(new Map(minColWidths));
    setMaxWidth(maxRowWidth);
  };

  useEffect(() => {
    if (scrollToRef.current) {
      isProgrammaticScrollRef.current = true;

      // perform scroll and reset
      const index = (scrollToRef.current === 'last') ? items.length : 1;
      listRef.current?.scrollToItem(index);
      scrollToRef.current = null;

      // reset load cache
      setTimeout(() => {
        isProgrammaticScrollRef.current = false;
        infiniteLoaderRef.current?.resetloadMoreItemsCache(true);
      }, 0);
    }
  }, [scrollToTrigger]);

  // ------------------------------------------------------------------------------------
  // Renderer
  // ------------------------------------------------------------------------------------

  return (
    <div className="h-full flex flex-col text-xs">
      <div
        ref={headerOuterElRef}
        className="overflow-x-scroll no-scrollbar cursor-default"
        onScroll={handleHeaderScrollX}
      >
        <div
          ref={headerInnerElRef}
          className="flex h-[18px] leading-[18px] border-b border-chrome-divider bg-chrome-200 [&>*]:border-r [&>*:not(:last-child)]:border-chrome-divider"
          style={{ width: `${maxWidth}px` }}
        >
          {allLogFeedColumns.map(col => {
            if (visibleCols.has(col)) {
              return (
                <div
                  key={col}
                  className={cn(
                    'whitespace-nowrap uppercase px-[8px]',
                    (col === LogFeedColumn.Message) ? 'flex-grow' : 'shrink-0',
                  )}
                  style={(col !== LogFeedColumn.Message) ? { minWidth: `${minColWidths.get(col) || 0}px` } : {}}
                  data-col-id={col}
                >
                  {(col !== LogFeedColumn.ColorDot) && col}
                </div>
              );
            }
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
                {({ onItemsRendered, ref }) => (
                  <FixedSizeList
                    ref={list => {
                      ref(list);
                      // @ts-ignore
                      listRef.current = list;
                    }}
                    className="font-mono"
                    onItemsRendered={(args) => {
                      onItemsRendered(args);
                      handleItemsRendered();
                    }}
                    onScroll={handleContentScrollY}
                    height={height}
                    width={width}
                    itemCount={itemCount}
                    itemSize={24}
                    outerRef={listOuterRef}
                    innerRef={listInnerRef}
                    overscanCount={20}
                    itemData={{ items, hasMoreBefore, hasMoreAfter, minColWidths, visibleCols }}
                  >
                    {Row}
                  </FixedSizeList>
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

const LogFeedContent = forwardRef(LogFeedContentImpl);

/**
 * LogFeedRecordFetcher component
 */

type LogFeedRecordFetcherProps = {
  node: Node;
  pod: Pod;
  container: string;
  defaultFollowAfter?: string;
  onFollowData: (record: LogRecord) => void;
};

type LogFeedRecordFetcherHandle = {
  key: string,
  head: (opts: LogFeedHeadOptions) => Promise<[string, PodLogQueryResponse]>;
  tail: (opts: LogFeedTailOptions) => Promise<[string, PodLogQueryResponse]>;
  skipForward: (batchSize: number, after: string | null | undefined) => Promise<[string, PodLogQueryResponse]>;
  reset: () => void;
};

const LogFeedRecordFetcherImpl: React.ForwardRefRenderFunction<LogFeedRecordFetcherHandle, LogFeedRecordFetcherProps> = (props, ref) => {
  const { node, pod, container, defaultFollowAfter, onFollowData } = props;
  const { namespace, name } = pod.metadata;

  const isFollow = useRecoilValue(isFollowState);
  const [followAfter, setFollowAfter] = useState<string | null | undefined>(defaultFollowAfter);

  const upgradeRecord = (record: GraphQLLogRecord) => {
    return { ...record, node, pod, container };
  };

  // head
  const head = useQuery(ops.HEAD_CONTAINER_LOG, {
    variables: { namespace, name, container },
    skip: true,
    fetchPolicy: 'no-cache',
    onError: console.log,
  });

  // tail
  const tail = useQuery(ops.TAIL_CONTAINER_LOG, {
    variables: { namespace, name, container },
    skip: true,
    fetchPolicy: 'no-cache',
    onError: console.log,
  });

  // follow
  useSubscription(ops.FOLLOW_CONTAINER_LOG, {
    variables: { namespace, name, container, after: followAfter },
    skip: !(isFollow && followAfter),
    fetchPolicy: 'no-cache',
    onError: console.log,
    onData: ({ data: { data } }) => {
      if (!data?.podLogFollow) return;
      const record = upgradeRecord(data.podLogFollow);

      // update folowAfter
      setFollowAfter(record.timestamp);

      // execute callback
      onFollowData(record);
    },
  })

  const key = `${namespace}/${name}/${container}`;

  // define handler api
  useImperativeHandle(ref, () => ({
    key,
    head: async (opts: LogFeedHeadOptions) => {
      // reset previous refetch() args
      opts = Object.assign({ after: undefined, since: undefined }, opts);

      // execute query
      const response = (await head.refetch(opts)).data.podLogHead;
      if (!response) throw new Error('query response is null');

      // update followAfter
      if (!response.pageInfo.hasNextPage) setFollowAfter(response.pageInfo.endCursor);

      // return with upgraded results
      return [
        key,
        {
          ...response,
          results: response.results.map(record => upgradeRecord(record))
        },
      ];
    },
    tail: async (opts: LogFeedTailOptions) => {
      // reset previous refetch() args
      opts = Object.assign({ before: undefined }, opts);

      // execute query
      const response = (await tail.refetch(opts)).data.podLogTail;
      if (!response) throw new Error('query response is null');

      // update followAfter
      if (!response.pageInfo.hasNextPage) setFollowAfter(response.pageInfo.endCursor);

      // return with upgraded results
      return [
        key,
        {
          ...response,
          results: response.results.map(record => upgradeRecord(record))
        },
      ];
    },
    skipForward: async (batchSize: number, after: string | null | undefined) => {
      // build args (including resetting `since`)
      const opts = { first: batchSize, since: undefined } as LogFeedHeadOptions;

      if (followAfter && followAfter.localeCompare(after || '')) opts.after = followAfter;
      else opts.after = after;

      // execute query
      const response = (await head.refetch(opts)).data.podLogHead;
      if (!response) throw new Error('query response is null');

      // update followAfter
      if (!response.pageInfo.hasNextPage) setFollowAfter(response.pageInfo.endCursor);
      else setFollowAfter(undefined);

      // return with upgraded results
      return [
        key,
        {
          ...response,
          results: response.results.map(record => upgradeRecord(record))
        },
      ];
    },
    reset: () => {
      setFollowAfter(undefined);
    },
  }));

  return <></>;
};

const LogFeedRecordFetcher = forwardRef(LogFeedRecordFetcherImpl);

/**
 * LogFeedLoader component
 */

type LogFeedLoaderProps = {
  onFollowData: (record: LogRecord) => void;
};

type LogFeedLoaderHandle = {
  head: (opts: LogFeedHeadOptions, cursorMap?: Map<string, PageInfo>) => Promise<[LogRecord[], Map<string, PageInfo>]>;
  tail: (opts: LogFeedTailOptions, cursorMap?: Map<string, PageInfo>) => Promise<[LogRecord[], Map<string, PageInfo>]>;
  skipForward: (batchSize: number, cursorMap: Map<string, PageInfo>) => Promise<[LogRecord[], Map<string, PageInfo>]>;
  reset: () => void;
};

const LogFeedLoaderImpl: React.ForwardRefRenderFunction<LogFeedLoaderHandle, LogFeedLoaderProps> = (
  {
    onFollowData,
  },
  ref,
) => {
  const nodes = useNodes();
  const pods = usePods();
  const setIsReady = useSetRecoilState(isReadyState);
  const childRefs = useRef(new Array<React.RefObject<LogFeedRecordFetcherHandle>>());
  const [defaultFollowAfter, setDefaultFollowAfter] = useState<string | undefined>();

  // set isReady after component and children are mounted
  useEffect(() => {
    if (nodes.loading || pods.loading) return;
    setIsReady(true);
  }, [nodes.loading, pods.loading]);

  // only load containers from nodes that we have a record of
  const nodeMap = new Map(nodes.nodes.map(node => [node.metadata.name, node]));

  const els: JSX.Element[] = [];
  const refs: React.RefObject<LogFeedRecordFetcherHandle>[] = [];
  const elKeys: string[] = [];

  pods.pods.forEach(pod => {
    pod.status.containerStatuses.forEach(status => {
      const node = nodeMap.get(pod.spec.nodeName);
      if (status.state.running?.startedAt && node) {
        const ref = createRef<LogFeedRecordFetcherHandle>();
        refs.push(ref);

        const k = `${pod.metadata.namespace}/${pod.metadata.name}/${status.name}`;
        elKeys.push(k);

        els.push(
          <LogFeedRecordFetcher
            key={k}
            ref={ref}
            node={node}
            pod={pod}
            container={status.name}
            defaultFollowAfter={defaultFollowAfter}
            onFollowData={onFollowData}
          />
        );
      }
    });
  });

  childRefs.current = refs;
  elKeys.sort();

  // define api
  useImperativeHandle(ref, () => ({
    head: async (opts: LogFeedHeadOptions = {}, oldCursorMap = new Map<string, PageInfo>()) => {
      const promises = Array<Promise<[string, PodLogQueryResponse]>>();
      const cursorMap = new Map(oldCursorMap);

      // build queries
      childRefs.current.forEach(childRef => {
        const fetcher = childRef.current;
        if (!fetcher) return;

        const pageInfo = cursorMap.get(fetcher.key)

        if (pageInfo === undefined) {
          // pass through query
          promises.push(fetcher.head(opts));
        } else if (pageInfo.hasNextPage) {
          // use end cursor from last time
          const newOpts = Object.assign({}, opts, { after: pageInfo.endCursor });
          promises.push(fetcher.head(newOpts));
        }
      });

      // execute quries
      const responses = await Promise.all(promises);

      // gather results and update cursor map
      const records = new Array<LogRecord>();
      responses.forEach(([key, response]) => {
        records.push(...response.results);

        // update cursor
        const cursor = Object.assign({}, cursorMap.get(key) || response.pageInfo);
        cursor.endCursor = response.pageInfo.endCursor;
        cursor.hasNextPage = response.pageInfo.hasNextPage;
        cursorMap.set(key, cursor);
      });

      // update defaultFollowAfter
      if (!hasNextPageSome(cursorMap)) setDefaultFollowAfter('BEGINNING');

      // sort records
      records.sort((a, b) => a.timestamp.localeCompare(b.timestamp));

      return [records, cursorMap];
    },
    tail: async (opts: LogFeedTailOptions = {}, oldCursorMap = new Map<string, PageInfo>()) => {
      const promises = Array<Promise<[string, PodLogQueryResponse]>>();
      const cursorMap = new Map(oldCursorMap);

      // build queries
      childRefs.current.forEach(childRef => {
        const fetcher = childRef.current;
        if (!fetcher) return;

        const pageInfo = cursorMap.get(fetcher.key)

        if (pageInfo === undefined) {
          // pass through query
          promises.push(fetcher.tail(opts));
        } else if (pageInfo.hasPreviousPage) {
          // use start cursor from last time
          const newOpts = Object.assign({}, opts, { before: pageInfo.startCursor });
          promises.push(fetcher.tail(newOpts));
        }
      });

      // update defaultFollowAfter
      setDefaultFollowAfter('BEGINNING');

      // execute quries
      const responses = await Promise.all(promises);

      // gather results and update cursor map
      const records = new Array<LogRecord>();
      responses.forEach(([key, response]) => {
        records.push(...response.results);

        // update cursor
        const cursor = Object.assign({}, cursorMap.get(key) || response.pageInfo);
        cursor.startCursor = response.pageInfo.startCursor;
        cursor.hasPreviousPage = response.pageInfo.hasPreviousPage;
        cursorMap.set(key, cursor);
      });

      // sort records
      records.sort((a, b) => a.timestamp.localeCompare(b.timestamp));

      return [records, cursorMap];
    },
    skipForward: async (batchSize: number, oldCursorMap = new Map<string, PageInfo>()) => {
      const promises = Array<Promise<[string, PodLogQueryResponse]>>();
      const cursorMap = new Map(oldCursorMap);

      // build queries
      childRefs.current.forEach(childRef => {
        const fetcher = childRef.current;
        if (!fetcher) return;

        const pageInfo = oldCursorMap.get(fetcher.key)

        if (pageInfo === undefined) {
          // pass through query
          promises.push(fetcher.head({ first: batchSize }));
        } else {
          // use end cursor from last time
          promises.push(fetcher.skipForward(batchSize, pageInfo.endCursor));
        }
      });

      // execute quries
      const responses = await Promise.all(promises);

      // gather results and update cursor map
      const records = new Array<LogRecord>();
      responses.forEach(([key, response]) => {
        records.push(...response.results);

        // update cursor
        const cursor = Object.assign({}, cursorMap.get(key) || response.pageInfo);
        cursor.endCursor = response.pageInfo.endCursor;
        cursor.hasNextPage = response.pageInfo.hasNextPage;
        cursorMap.set(key, cursor);
      });

      // update defaultFollowAfter
      if (!hasNextPageSome(cursorMap)) setDefaultFollowAfter('BEGINNING');

      // sort records
      records.sort((a, b) => a.timestamp.localeCompare(b.timestamp));

      return [records, cursorMap];
    },
    reset: () => {
      childRefs.current.forEach(childRef => childRef.current?.reset());
    },
  }), [JSON.stringify(elKeys)]);

  return <>{els}</>;
};

const LogFeedLoader = forwardRef(LogFeedLoaderImpl);

/**
 * LogFeedViewer component
 */

const hasPreviousPageSome = (cursorMap: Map<string, PageInfo>) => {
  for (const [, pageInfo] of cursorMap) {
    if (pageInfo.hasPreviousPage) return true;
  }
  return false;
};

const hasNextPageSome = (cursorMap: Map<string, PageInfo>) => {
  for (const [, pageInfo] of cursorMap) {
    if (pageInfo.hasNextPage) return true;
  }
  return false;
};


export const LogFeedViewer = () => {
  const [channelID, setChannelID] = useRecoilState(controlChannelIDState);
  const isReady = useRecoilValue(isReadyState);
  const setIsLoading = useSetRecoilState(isLoadingState);
  const setIsFollow = useSetRecoilState(isFollowState);
  const [logRecords, setLogRecords] = useRecoilState(logRecordsState);

  const loaderRef = useRef<LogFeedLoaderHandle>(null);
  const contentRef = useRef<LogFeedContentHandle>(null);

  const [hasMoreBefore, setHasMoreBefore] = useState(false);
  const [hasMoreAfter, setHasMoreAfter] = useState(false);

  const beforeBufferRef = useRef(new Array<LogRecord>());
  const afterBufferRef = useRef(new Array<LogRecord>());
  const cursorMapRef = useRef(new Map<string, PageInfo>);
  const isSendFollowToBufferRef = useRef(true);

  const batchSize = 300;

  const handleOnFollowData = (record: LogRecord) => {
    if (isSendFollowToBufferRef.current) {
      afterBufferRef.current.push(record);
    } else {
      setLogRecords(currRecords => [...currRecords, record]);
      contentRef.current?.autoScroll();
    }
  };

  const handleLoadMoreBefore = async () => {
    const client = loaderRef.current;
    if (!client) return;

    // build query
    const opts = { last: batchSize } as LogFeedTailOptions;

    // execute
    const [records, cursorMap] = await client.tail(opts, cursorMapRef.current);

    // add to buffer and resort
    beforeBufferRef.current.push(...records);
    beforeBufferRef.current.sort((a, b) => a.timestamp.localeCompare(b.timestamp));

    // update state
    cursorMapRef.current = cursorMap;

    // update content
    const newRecords = beforeBufferRef.current.splice(-1 * batchSize);

    setLogRecords(oldRecords => [...newRecords, ...oldRecords]);
    setHasMoreBefore(beforeBufferRef.current.length > 0 || hasPreviousPageSome(cursorMap));
  };

  const handleLoadMoreAfter = async () => {
    const client = loaderRef.current;
    if (!client) return;

    // build query
    const opts = { first: batchSize } as LogFeedHeadOptions;

    // execute
    const [records, cursorMap] = await client.head(opts, cursorMapRef.current);

    // add to buffer and resort
    afterBufferRef.current.push(...records);
    afterBufferRef.current.sort((a, b) => a.timestamp.localeCompare(b.timestamp));

    // update state
    cursorMapRef.current = cursorMap;

    // update content
    const newRecords = afterBufferRef.current.splice(0, batchSize);
    setLogRecords(oldRecords => [...oldRecords, ...newRecords]);

    const hasMoreAfter = afterBufferRef.current.length > 0 || hasNextPageSome(cursorMap);
    if (!hasMoreAfter) isSendFollowToBufferRef.current = false;
    setHasMoreAfter(hasMoreAfter);
  };

  // listen to control channel
  useEffect(() => {
    // initalize broadcast channel
    const channelID = Math.random().toString();
    const channel = new BroadcastChannel(channelID);

    const fn = async (ev: MessageEvent<Command>) => {
      const client = loaderRef.current;
      if (!client) return;

      let hasMoreAfter: boolean;

      const reset = () => {
        beforeBufferRef.current = [];
        afterBufferRef.current = [];
      }

      // handle commands
      switch (ev.data.type) {
        case 'head':
          setIsLoading(true);

          // reset
          reset();
          client.reset();
          setHasMoreBefore(false);
          isSendFollowToBufferRef.current = true;

          // execute query and reset state
          [afterBufferRef.current, cursorMapRef.current] = await client.head({ since: 'beginning', first: batchSize });

          // update content
          setLogRecords(afterBufferRef.current.splice(0, batchSize));

          hasMoreAfter = afterBufferRef.current.length > 0 || hasNextPageSome(cursorMapRef.current);
          if (!hasMoreAfter) isSendFollowToBufferRef.current = false;
          setHasMoreAfter(hasMoreAfter);

          contentRef.current?.scrollTo('first');

          setIsLoading(false);
          break;
        case 'tail':
          setIsLoading(true);

          // reset
          reset();
          client.reset();
          setHasMoreAfter(false);
          isSendFollowToBufferRef.current = false;

          // execute query and reset state
          [beforeBufferRef.current, cursorMapRef.current] = await client.tail({ last: batchSize });

          // update content
          setLogRecords(beforeBufferRef.current.splice(-1 * batchSize));
          setHasMoreBefore(beforeBufferRef.current.length > 0 || hasPreviousPageSome(cursorMapRef.current));

          contentRef.current?.scrollTo('last');

          setIsLoading(false);
          break;
        case 'seek':
          setIsLoading(true);

          // reset
          reset();
          client.reset();
          setHasMoreBefore(false);
          isSendFollowToBufferRef.current = true;

          // execute query and reset state
          [afterBufferRef.current, cursorMapRef.current] = await client.head({ since: ev.data.sinceTS, first: batchSize });

          // update content
          setLogRecords(afterBufferRef.current.splice(0, batchSize));

          hasMoreAfter = afterBufferRef.current.length > 0 || hasNextPageSome(cursorMapRef.current);
          if (!hasMoreAfter) isSendFollowToBufferRef.current = false;
          setHasMoreAfter(hasMoreAfter);

          contentRef.current?.scrollTo('first');

          setIsLoading(false);
          break;
        case 'play':
          // execute query
          const response = await client.skipForward(batchSize, cursorMapRef.current);

          // add to buffer and resort
          afterBufferRef.current.push(...response[0]);
          afterBufferRef.current.sort((a, b) => a.timestamp.localeCompare(b.timestamp));

          // update state
          cursorMapRef.current = response[1];

          hasMoreAfter = afterBufferRef.current.length > 0 || hasNextPageSome(cursorMapRef.current);

          if (!hasMoreAfter) isSendFollowToBufferRef.current = false;
          else isSendFollowToBufferRef.current = true;

          setHasMoreAfter(hasMoreAfter);
          setIsFollow(true);

          break;
        case 'pause':
          setIsFollow(false);
          break;
        default:
          throw new Error('not implemented');
      }

    };
    channel.addEventListener('message', fn);

    // update state
    setChannelID(channelID);

    return () => {
      setChannelID(undefined);
      channel.removeEventListener('message', fn);
      channel.close();
    };
  }, []);

  // tail by default
  useEffect(() => {
    if (!isReady || !channelID) return;
    const bc = new BroadcastChannel(channelID);
    bc.postMessage({ type: 'tail' });
    bc.close();
  }, [isReady, channelID]);

  return (
    <>
      <LogFeedLoader
        ref={loaderRef}
        onFollowData={handleOnFollowData}
      />
      <LogFeedContent
        ref={contentRef}
        items={logRecords}
        hasMoreBefore={hasMoreBefore}
        hasMoreAfter={hasMoreAfter}
        loadMoreBefore={handleLoadMoreBefore}
        loadMoreAfter={handleLoadMoreAfter}
      />
    </>
  );
};
