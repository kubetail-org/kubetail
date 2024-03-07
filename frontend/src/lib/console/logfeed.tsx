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

import { useQuery } from '@apollo/client';
import { AnsiUp } from 'ansi_up';
import { addMinutes, parseISO } from 'date-fns';
import { format, utcToZonedTime } from 'date-fns-tz';
import makeAnsiRegex from 'ansi-regex';
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react';
import { createRef, memo } from 'react';
import AutoSizer from 'react-virtualized-auto-sizer';
import { FixedSizeList } from 'react-window';
import InfiniteLoader from 'react-window-infinite-loader';
import { atom, useRecoilState, useRecoilValue, useResetRecoilState, useSetRecoilState } from 'recoil';

import { cn } from '@/lib/utils';

import type { LogRecord as GraphQLLogRecord } from '@/lib/graphql/__generated__/graphql';
import * as ops from '@/lib/graphql/ops';

import { cssID } from './helpers';
import { useNodes, usePods } from './logging-resources2';
import type { Node, Pod } from './logging-resources2';

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

type LogFeedQueryOptions = {
  after?: string;
  before?: string;
  since?: string;
  until?: string;
  limit?: number;
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
  time: Date;
}

interface LoadMoreAfterCommand extends BaseCommand {
  type: 'loadMoreAfter';
}

type Command = HeadCommand | TailCommand | SeekCommand | LoadMoreAfterCommand;

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

const feedStateState = atom({
  key: 'logFeedFeedState',
  default: LogFeedState.Paused,
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

const controlChannelIDState = atom<string | undefined>({
  key: 'logFeedControlChannelID',
  default: undefined,
});

/**
 * Hooks
 */

export const useLogFeedControls = () => {
  const setIsFollow = useSetRecoilState(isFollowState);
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
    seek: (time: Date) => {
      postMessage({ type: 'seek', time });
    },
    setFollow: (follow: boolean) => {
      setIsFollow(follow);
    },
    loadMoreAfter: () => {
      postMessage({ type: 'loadMoreAfter' });
    },
  };
};

export const useLogFeedMetadata = () => {
  const isReady = useRecoilValue(isReadyState);
  const isLoading = useRecoilValue(isLoadingState);
  const isFollow = useRecoilValue(isFollowState);
  return { isReady, isLoading, isFollow };
};

export function useLogFeedVisibleCols(): [Set<LogFeedColumn>, (arg: Set<LogFeedColumn>) => void] {
  return useRecoilState(visibleColsState);
}

/**
 * LogFeedViewer component
 */

type LogFeedContentHandle = {
  resetloadMoreItemsCache: () => void;
};

type LogFeedContentProps = {
  items: LogRecord[];
  hasMoreBefore: boolean;
  hasMoreAfter: boolean;
  loadMoreBefore: () => Promise<void>;
  loadMoreAfter: () => Promise<void>;
  initialPos: string;
}

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

const CheckForMoreLink = () => {
  const controls = useLogFeedControls();

  const handleClick = () => {
    controls.loadMoreAfter();
  };

  return (
    <a
      className="text-primary underline font-normal cursor-pointer"
      onClick={handleClick}
    >
      Check for new records
    </a>
  );
}

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
    const { items, hasMoreBefore, hasMoreAfter, visibleCols, minColWidths } = data;

    // first row
    if (index === 0) {
      const msg = (hasMoreBefore) ? 'Loading...' : 'Beginning of feed';
      return <div className="px-[8px] leading-[24px]" style={style}>{msg}</div>;
    }

    // last row
    if (index === (items.length + 1)) {
      if (hasMoreAfter) {
        return <div className="px-[8px] leading-[24px]" style={style}>Loading...</div>;
      }
      return (
        <div className="px-[8px] leading-[24px]" style={style}>
          <CheckForMoreLink />
        </div>
      );
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

const LogFeedContentImpl: React.ForwardRefRenderFunction<LogFeedContentHandle, LogFeedContentProps> = (props, ref) => {
  const minColWidths = new Map<LogFeedColumn, number>();
  const visibleCols = useRecoilValue(visibleColsState);

  const { items, hasMoreBefore, hasMoreAfter, loadMoreBefore, loadMoreAfter, initialPos } = props;
  const [isLoading, setIsLoading] = useState(false);
  const infiniteLoaderRef = useRef<InfiniteLoader | null>(null);

  const itemCount = items.length + 2;

  const isItemLoaded = (index: number) => {
    if (index === 0 && hasMoreBefore) return false;
    if (index === (itemCount - 1) && hasMoreAfter) return false;
    return true;
  };

  const loadMoreItems = async (startIndex: number) => {
    if (isLoading) return;
    setIsLoading(true);

    if (startIndex === 0) await loadMoreBefore();
    else await loadMoreAfter();

    setIsLoading(false);
    infiniteLoaderRef.current?.resetloadMoreItemsCache();
  };

  // define handler api
  useImperativeHandle(ref, () => ({
    resetloadMoreItemsCache: () => {
      infiniteLoaderRef.current?.resetloadMoreItemsCache();
    },
  }));

  return (
    <div className="h-full flex flex-col text-xs">
      <div className="flex-grow">
        <AutoSizer>
          {({ height, width }) => (
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
                    //listRef.current = list;
                  }}
                  className="font-mono"
                  onItemsRendered={(args) => {
                    onItemsRendered(args);
                    //handleItemsRendered();
                  }}
                  //onScroll={handleContentScroll}
                  height={height}
                  width={width}
                  itemCount={itemCount}
                  itemSize={24}
                  //outerRef={listOuterRef}
                  //innerRef={listInnerRef}
                  initialScrollOffset={itemCount * 24}
                  overscanCount={20}
                  itemData={{
                    items,
                    hasMoreBefore,
                    hasMoreAfter,
                    minColWidths,
                    visibleCols,
                    resetloadMoreItemsCache: infiniteLoaderRef.current?.resetloadMoreItemsCache,
                  }}
                >
                  {Row}
                </FixedSizeList>
              )}
            </InfiniteLoader>
          )}
        </AutoSizer>
      </div>
    </div>
  );
};

const LogFeedContent = forwardRef(LogFeedContentImpl);

/*
const Row = memo(({ index, style, data }: { index: any; style: any; data: any; }) => {
  const { hasMore, visibleCols, items, minColWidths } = data;

  if (index === 0) {
    if (hasMore) return <div>Loading...</div>;
    else return <div>no more data</div>;
  }
  const record = items[hasMore ? index - 1 : index];

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
});

const LogFeedContent = ({ items, fetchMore, hasMore }: LogFeedContentProps) => {
  const visibleCols = useRecoilValue(visibleColsState);

  const headerOuterElRef = useRef<HTMLDivElement>(null);
  const headerInnerElRef = useRef<HTMLDivElement>(null);

  const listRef = useRef<FixedSizeList<LogRecord> | null>(null);
  const listOuterRef = useRef<HTMLDivElement | null>(null);
  const listInnerRef = useRef<HTMLDivElement | null>(null);
  const infiniteLoaderRef = useRef<InfiniteLoader | null>(null);

  const [isLoading, setIsLoading] = useState(false);
  const [isListReady, setIsListReady] = useState(false);

  const [maxWidth, setMaxWidth] = useState<number | string>('100%');
  const [minColWidths, setMinColWidths] = useState<Map<LogFeedColumn, number>>(new Map());

  const [onNextRenderCallback, setOnNextRenderCallback] = useState<() => void>();

  const isAutoScrollRef = useRef(true);
  const isProgrammaticScrollRef = useRef(false);

  // initialize minimum column widths
  useEffect(() => {
    // iterate through header columns
    Array.from(headerInnerElRef.current?.children || []).forEach(colEl => {
      const colId = (colEl as HTMLElement).dataset.colId as LogFeedColumn;
      if (!colId) return;
      const currVal = minColWidths.get(colId) || 0;
      minColWidths.set(colId, Math.max(currVal, colEl.scrollWidth));
    });

    // iterate through data columns
    Array.from(listInnerRef.current?.children || []).forEach(rowEl => {
      Array.from(rowEl.children || []).forEach(colEl => {
        const colId = (colEl as HTMLElement).dataset.colId as LogFeedColumn;
        if (!colId) return;
        const currVal = minColWidths.get(colId) || 0;
        minColWidths.set(colId, Math.max(currVal, colEl.scrollWidth));
      });
    });

    setMinColWidths(new Map(minColWidths));
  }, [JSON.stringify(Array.from(visibleCols))]);

  // scroll to bottom on new data
  useEffect(() => {
    const listOuterEl = listOuterRef.current;
    if (isAutoScrollRef.current && listOuterEl) {
      isProgrammaticScrollRef.current = true;
      listOuterEl.scrollTo({ top: listOuterEl.scrollHeight, behavior: 'instant' });
      const timeout = setTimeout(() => {
        isProgrammaticScrollRef.current = false;
        clearTimeout(timeout);
      }, 0);
    }
  }, [isListReady, items.length]);

  // handle auto-scroll
  const handleContentScroll = () => {
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

  // leave extra space if there are more results
  const itemCount = (hasMore) ? items.length + 1 : items.length;

  // use first item as loading placeholder
  const isItemLoaded = (index: number) => {
    if (index === 0 && isListReady && hasMore) return false;
    return true;
  };

  const loadMoreItems = async () => {
    if (isLoading) return;
    setIsLoading(true);

    await fetchMore();

    // current scrollPos
    const scrollPos = listOuterRef.current?.scrollTop || 0;

    // update state
    setIsLoading(false);

    // reset cache and keep scrollPos in place
    setOnNextRenderCallback(() => {
      infiniteLoaderRef.current?.resetloadMoreItemsCache();
      setTimeout(() => listRef.current?.scrollTo(scrollPos + (30 * 18)), 0);
    });
  }

  const handleItemsRendered = () => {
    // set isListReady
    if (!isListReady) setIsListReady(true);

    // execute callback if available
    if (onNextRenderCallback) {
      onNextRenderCallback();
      setOnNextRenderCallback(undefined);
    }

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

  const handleHeaderScrollX = (ev: React.UIEvent<HTMLDivElement>) => {
    const headerOuterEl = ev.target as HTMLDivElement;
    const listOuterEl = listOuterRef.current;
    if (!listOuterEl) return;
    listOuterEl.scrollTo({ left: headerOuterEl.scrollLeft, behavior: 'instant' });
  };

  const handleContentScrollX = (ev: React.UIEvent<HTMLDivElement>) => {
    const listOuterEl = ev.target as HTMLDivElement;
    const headerOuterEl = headerOuterElRef.current;
    if (!headerOuterEl) return;
    headerOuterEl.scrollTo({ left: listOuterEl.scrollLeft, behavior: 'instant' });
  };

  useEffect(() => {
    const listOuterEl = listOuterRef.current;
    if (!listOuterEl) return;
    listOuterEl.addEventListener('scroll', handleContentScrollX as any);
    return () => listOuterEl.removeEventListener('scroll', handleContentScrollX as any);
  }, [isListReady, handleContentScrollX]);

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
      <div className="flex-grow">
        <AutoSizer>
          {({ height, width }) => (
            <InfiniteLoader
              ref={infiniteLoaderRef}
              isItemLoaded={isItemLoaded}
              itemCount={itemCount}
              loadMoreItems={loadMoreItems}
              threshold={0}
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
                  onScroll={handleContentScroll}
                  height={height}
                  width={width}
                  itemCount={itemCount}
                  itemSize={24}
                  outerRef={listOuterRef}
                  innerRef={listInnerRef}
                  initialScrollOffset={itemCount * 24}
                  overscanCount={10}
                  itemData={{ hasMore, minColWidths, items, visibleCols }}
                >
                  {Row}
                </FixedSizeList>
              )}
            </InfiniteLoader>
          )}
        </AutoSizer>
      </div>
    </div>
  );
};
*/

/**
 * LogFeedRecordFetcher component
 */

type LogFeedRecordFetcherProps = {
  //defaultSince: string;
  //defaultUntil: string;
  node: Node;
  pod: Pod;
  container: string;
  //onLoad?: (records: LogRecord[]) => void;
  onUpdate?: (record: LogRecord) => void;
};

type LogFeedRecordFetcherHandle = {
  skipForward: () => Promise<LogRecord[]>;
  query: (opts: LogFeedQueryOptions) => Promise<LogRecord[]>;
};

const LogFeedRecordFetcherImpl: React.ForwardRefRenderFunction<LogFeedRecordFetcherHandle, LogFeedRecordFetcherProps> = (props, ref) => {
  const { node, pod, container, onUpdate } = props;
  const { namespace, name } = pod.metadata;
  const feedState = useRecoilValue(feedStateState);

  const lastTSRef = useRef<string>();
  const startTSRef = useRef<string>();

  const upgradeRecord = (record: GraphQLLogRecord) => {
    return { ...record, node, pod, container };
  };

  // get logs
  const { loading, data, subscribeToMore, refetch } = useQuery(ops.QUERY_CONTAINER_LOG, {
    variables: { namespace, name, container },
    fetchPolicy: 'no-cache',
    skip: true,  // we'll use refetch() and subscribeToMmore() instead
    onError: (err) => {
      console.log(err);
    },
  });

  // update lastTS
  if (!lastTSRef.current) lastTSRef.current = data?.podLogQuery?.length ? data.podLogQuery[data.podLogQuery.length - 1].timestamp : undefined;

  // tail
  useEffect(() => {
    // wait for initial query to complete
    if (!(loading === false)) return;

    // only execute when playing
    if (!(feedState === LogFeedState.Streaming)) return;

    // update startTS
    startTSRef.current = (new Date()).toISOString();

    const variables = { namespace, name, container } as any;

    // implement `after`
    if (lastTSRef.current) variables.after = lastTSRef.current;
    else variables.since = 'NOW';

    return subscribeToMore({
      document: ops.TAIL_CONTAINER_LOG,
      variables: variables,
      updateQuery: (_, { subscriptionData }) => {
        const record = subscriptionData.data.podLogTail;
        if (record) {
          // update lastTS
          lastTSRef.current = record.timestamp;

          // execute callback
          onUpdate && onUpdate(upgradeRecord(record));
        }
        return { podLogQuery: [] };
      },
      onError: (err) => {
        console.log(err)
      },
    });
  }, [subscribeToMore, loading, feedState]);

  // define handler api
  useImperativeHandle(ref, () => ({
    skipForward: async () => {
      const variables = {} as any;
      if (lastTSRef.current) variables.after = lastTSRef.current;
      else variables.after = startTSRef.current;

      const result = await refetch(variables);
      if (!result.data.podLogQuery) return [];

      // upgrade records
      const records = result.data.podLogQuery.map(record => upgradeRecord(record));

      // update lastTS
      if (records.length) lastTSRef.current = records[records.length - 1].timestamp;

      // return records
      return records;
    },
    query: async (opts: LogFeedQueryOptions) => {
      const result = await refetch(opts);
      if (!result.data.podLogQuery) return [];

      // upgrade records
      const records = result.data.podLogQuery.map(record => upgradeRecord(record));

      // update lastTS
      if (!opts.until) {
        if (records.length) lastTSRef.current = records[records.length - 1].timestamp;
        else lastTSRef.current = undefined;
      }

      // return records
      return records;
    }
  }));

  return <></>;
};

const LogFeedRecordFetcher = forwardRef(LogFeedRecordFetcherImpl);

/**
 * LogFeedLoader component
 */

type LogFeedLoaderHandle = {
  query: (opts: LogFeedQueryOptions) => Promise<LogRecord[]>;
  tail: (startLine: number, limit: number) => Promise<LogRecord[]>;
};

const LogFeedLoaderImpl: React.ForwardRefRenderFunction<LogFeedLoaderHandle, {}> = (_, ref) => {
  const nodes = useNodes();
  const pods = usePods();
  const setIsReady = useSetRecoilState(isReadyState);
  const setLogRecords = useSetRecoilState(logRecordsState);
  const childRefs = useRef(new Array<React.RefObject<LogFeedRecordFetcherHandle>>());
  const bufferRef = useRef(new Array<LogRecord>());
  const isSendToBuffer = useRef(true);

  // set isReady after component and children are mounted
  useEffect(() => {
    if (nodes.loading || pods.loading) return;
    setIsReady(true);
  }, [nodes.loading, pods.loading]);

  const handleOnUpdate = (record: LogRecord) => {
    //if (isSendToBuffer.current) bufferRef.current.push(record);
    //else setLogRecords((currRecords) => [...currRecords, record]);
  };

  // only load containers from nodes that we have a record of
  const nodeMap = new Map(nodes.nodes.map(node => [node.metadata.name, node]));

  const els: JSX.Element[] = [];
  const refs: React.RefObject<LogFeedRecordFetcherHandle>[] = [];
  const elKeys: string[] = [];

  pods.pods.forEach(pod => {
    pod.status.containerStatuses.forEach(status => {
      const node = nodeMap.get(pod.spec.nodeName);
      if (status.started && node) {
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
            onUpdate={handleOnUpdate}
          />
        );
      }
    });
  });

  childRefs.current = refs;
  elKeys.sort();

  // define api
  useImperativeHandle(ref, () => ({
    query: async (opts: LogFeedQueryOptions = {}) => {
      const promises = Array<Promise<LogRecord[]>>();
      const records = Array<LogRecord>();

      // trigger query in children
      childRefs.current.forEach(childRef => {
        childRef.current && promises.push(childRef.current.query(opts));
      });

      // gather and sort results
      (await Promise.all(promises)).forEach(result => records.push(...result));
      records.sort((a, b) => a.timestamp.localeCompare(b.timestamp));

      // handle limit
      if (opts.limit) return records.slice(0, opts.limit);

      return records;
    },
    tail: async (startLine: number, limit: number) => {
      const promises = Array<Promise<LogRecord[]>>();
      const records = Array<LogRecord>();

      // trigger query in children
      childRefs.current.forEach(childRef => {
        childRef.current && promises.push(childRef.current.query({ since: `${startLine}`, limit }));
      });

      // gather and sort results
      (await Promise.all(promises)).forEach(result => records.push(...result));
      records.sort((a, b) => a.timestamp.localeCompare(b.timestamp));

      // return all
      return records;
    },
  }), [JSON.stringify(elKeys)]);

  return <>{els}</>;
};

const LogFeedLoader = forwardRef(LogFeedLoaderImpl);

/**
 * LogFeedViewer component
 */

export const LogFeedViewer = () => {
  const [channelID, setChannelID] = useRecoilState(controlChannelIDState);
  const isReady = useRecoilValue(isReadyState);
  const resetIsLoading = useResetRecoilState(isLoadingState);
  const resetRecords = useResetRecoilState(logRecordsState);
  const [records, setRecords] = useRecoilState(logRecordsState);

  const loaderRef = useRef<LogFeedLoaderHandle>(null);
  const contentRef = useRef<LogFeedContentHandle>(null);

  const tailTrackerRef = useRef(0);
  const tailBufferRef = useRef(new Array<LogRecord>());

  const [hasMoreBefore, setHasMoreBefore] = useState(false);
  const [hasMoreAfter, setHasMoreAfter] = useState(false);
  const [initialPos, setInitialPos] = useState('first');

  const handleLoadMoreBefore = async () => {
    const client = loaderRef.current;
    if (!client) return;

    // for now only handle tail
    if (tailTrackerRef.current === 0) return;

    tailTrackerRef.current -= 100;
    const newTailRecords = await client.tail(tailTrackerRef.current, 100);

    // add to buffer and re-sort
    tailBufferRef.current.push(...newTailRecords);
    tailBufferRef.current.sort((a, b) => a.timestamp.localeCompare(b.timestamp));

    // send last 100 in buffer to content window
    if (tailBufferRef.current.length) setRecords(oldVal => [...tailBufferRef.current.splice(-100), ...oldVal]);

    if (!newTailRecords.length && !tailBufferRef.current.length) setHasMoreBefore(false);
  };

  const handleLoadMoreAfter = async () => {
    const client = loaderRef.current;
    if (!client) return;

    const opts = { limit: 100 } as LogFeedQueryOptions;
    if (records.length) opts.after = records[records.length - 1].timestamp;
    else opts.since = 'beginning';

    const newRecords = await client.query(opts);
    if (newRecords.length) setRecords(oldVal => [...oldVal, ...newRecords]);
    else setHasMoreAfter(false);
  };

  // listen to control channel
  useEffect(() => {
    // initalize broadcast channel
    const channelID = Math.random().toString();
    const channel = new BroadcastChannel(channelID);

    const resetAll = () => {
      // reset feed
      resetIsLoading();
      resetRecords();

      // reset tail tracker
      tailTrackerRef.current = 0;
      tailBufferRef.current = [];
    };

    const fn = async (ev: MessageEvent<Command>) => {
      const client = loaderRef.current;
      if (!client) return;

      // handle commands
      switch (ev.data.type) {
        case 'head':
          // reset
          resetAll();

          // fetch records
          setRecords(await client.query({ since: 'beginning', limit: 100 }));

          // update props
          setHasMoreBefore(false);
          setHasMoreAfter(true);
          setInitialPos('first');
          break;
        case 'tail':
          // reset
          resetAll();

          // fetch records and store in buffer
          tailBufferRef.current = await client.tail(-100, 100);

          // send last 100 in buffer to content window
          setRecords(tailBufferRef.current.splice(-100));

          // update tail tracker
          tailTrackerRef.current = -100;

          // update props
          setHasMoreBefore(true);
          setHasMoreAfter(false);
          setInitialPos('last');
          break;
        case 'seek':
          // reset
          resetAll();

          // fetch records
          setRecords(await client.query({ since: ev.data.time.toISOString(), limit: 100 }));

          // update props
          setHasMoreBefore(false);
          setHasMoreAfter(true);
          setInitialPos('first');
          break;
        case 'loadMoreAfter':
          setHasMoreAfter(true);
          handleLoadMoreAfter();
          break;
        default:
          throw new Error('not implemented');
      }

      // reset content cache
      contentRef.current?.resetloadMoreItemsCache();
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
      <LogFeedLoader ref={loaderRef} />
      <LogFeedContent
        ref={contentRef}
        items={records}
        hasMoreBefore={hasMoreBefore}
        hasMoreAfter={hasMoreAfter}
        loadMoreBefore={handleLoadMoreBefore}
        loadMoreAfter={handleLoadMoreAfter}
        initialPos={initialPos}
      />
    </>
  );
};
