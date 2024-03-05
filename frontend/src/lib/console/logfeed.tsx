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
import { format, utcToZonedTime } from 'date-fns-tz';
import makeAnsiRegex from 'ansi-regex';
import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react';
import { createRef, memo } from 'react';
import AutoSizer from 'react-virtualized-auto-sizer';
import { FixedSizeList } from 'react-window';
import InfiniteLoader from 'react-window-infinite-loader';
import { RecoilRoot, atom, useRecoilState, useRecoilValue, useResetRecoilState, useSetRecoilState } from 'recoil';

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
  since?: string;
  until?: string;
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

type Command = HeadCommand | TailCommand | SeekCommand;

/**
 * State
 */

const isReadyState = atom({
  key: 'isReady',
  default: false,
});

const isLoadingState = atom({
  key: 'isLoading',
  default: true,
});

const feedStateState = atom({
  key: 'feedState',
  default: LogFeedState.Paused,
});

const logRecordsState = atom({
  key: 'logRecords',
  default: new Array<LogRecord>(),
});

const visibleColsState = atom({
  key: 'visibleCols',
  default: new Set([LogFeedColumn.Timestamp, LogFeedColumn.ColorDot, LogFeedColumn.Message]),
});

const channelIDState = atom<string | undefined>({
  key: 'channelID',
  default: undefined,
});

/**
 * Hooks
 */

export const useLogFeedControls = () => {
  const setFeedState = useSetRecoilState(feedStateState);
  const channelID = useRecoilValue(channelIDState);

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

    },
  };
};

export const useLogFeedMetadata = () => {
  const isReady = useRecoilValue(isReadyState);
  const isLoading = useRecoilValue(isLoadingState);
  const state = useRecoilValue(feedStateState);
  return { isReady, isLoading, state };
};

export function useLogFeedVisibleCols(): [Set<LogFeedColumn>, (arg: Set<LogFeedColumn>) => void] {
  return useRecoilState(visibleColsState);
}

/**
 * LogFeedViewer component
 */

type LogFeedContentProps = {
  items: LogRecord[];
  hasMore: boolean;
  fetchMore: () => Promise<void>;
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

export const LogFeedViewer = () => {
  const logRecords = useRecoilValue(logRecordsState);

  return (
    <LogFeedContent
      items={logRecords}
      hasMore={false}
      fetchMore={async () => { }}
    />
  );
};

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

type LogFeedLoaderProps = {
  defaultSince: string;
  defaultUntil: string;
}

const LogFeedLoader = ({ defaultSince, defaultUntil }: LogFeedLoaderProps) => {
  const nodes = useNodes();
  const pods = usePods();
  const setIsReady = useSetRecoilState(isReadyState);
  const setLogRecords = useSetRecoilState(logRecordsState);
  const setChannelID = useSetRecoilState(channelIDState);
  const childRefs = useRef(new Array<React.RefObject<LogFeedRecordFetcherHandle>>());
  const bufferRef = useRef(new Array<LogRecord>());
  const isSendToBuffer = useRef(true);

  // set isReady after component and children are mounted
  useEffect(() => {
    if (nodes.loading || pods.loading) return;
    setIsReady(true);

    // onload query
    (async () => {
      const promises = Array<Promise<LogRecord[]>>();
      const records = Array<LogRecord>();

      // trigger query in children
      childRefs.current.forEach(childRef => {
        childRef.current && promises.push(childRef.current.query({ since: defaultSince }));
      });

      // gather and sort results
      (await Promise.all(promises)).forEach(result => records.push(...result));
      records.sort((a, b) => a.timestamp.localeCompare(b.timestamp));

      // handle tailLines
      const tailLines = parseInt(defaultSince);
      if (!Number.isNaN(tailLines) && tailLines < 0) {
        const numToRemove = records.length + tailLines;
        if (numToRemove > 0) records.splice(0, numToRemove);
      }

      // update state
      setLogRecords([...records, ...bufferRef.current]);

      // update buffer and flag
      bufferRef.current = [];
      isSendToBuffer.current = false;
    })();
  }, [nodes.loading, pods.loading]);

  const handleOnUpdate = (record: LogRecord) => {
    if (isSendToBuffer.current) bufferRef.current.push(record);
    else setLogRecords((currRecords) => [...currRecords, record]);
  };

  // only load containers from nodes that we have a record of
  const nodeMap = new Map(nodes.nodes.map(node => [node.metadata.name, node]));

  const els: JSX.Element[] = [];
  const refs: React.RefObject<LogFeedRecordFetcherHandle>[] = [];

  pods.pods.forEach(pod => {
    pod.status.containerStatuses.forEach(status => {
      const node = nodeMap.get(pod.spec.nodeName);
      if (status.started && node) {
        const ref = createRef<LogFeedRecordFetcherHandle>();
        refs.push(ref);

        els.push(
          <LogFeedRecordFetcher
            key={`${pod.metadata.namespace}/${pod.metadata.name}/${status.name}`}
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

  return <>{els}</>;
};

/**
 * LogFeedProvider component
 */

interface LogFeedProvider extends React.PropsWithChildren {
  defaultSince: string;
  defaultUntil: string;
}

export const LogFeedProvider = ({ defaultSince, defaultUntil, children }: LogFeedProvider) => {
  const resetIsReady = useResetRecoilState(isReadyState);
  const resetIsLoading = useResetRecoilState(isLoadingState);
  const resetRecords = useResetRecoilState(logRecordsState);

  // reset on change in arguments
  useEffect(() => {
    resetIsReady();
    resetIsLoading();
    resetRecords();
  }, [defaultSince, defaultUntil]);

  useEffect(() => {
    // initalize broadcast channel
    const channelID = Math.random().toString();
    const channel = new BroadcastChannel(channelID);

    const fn = async (ev: MessageEvent<Command>) => {
      switch (ev.data.type) {
        case 'head':
          break;
        case 'tail':
          console.log('tail');
          break;
        case 'seek':
          console.log('seek');
          break;
        default:
          throw new Error('not implemented');
      }
      console.log(ev.data);
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

  return (
    <RecoilRoot override={false}>
      <LogFeedLoader
        key={`${defaultSince}_${defaultUntil}`}
        defaultSince={defaultSince}
        defaultUntil={defaultUntil}
      />
      {children}
    </RecoilRoot>
  );
};
