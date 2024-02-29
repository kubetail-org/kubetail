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

import { format, utcToZonedTime } from 'date-fns-tz';
import { forwardRef, useEffect, useRef, useState } from 'react';
import AutoSizer from 'react-virtualized-auto-sizer';
import InfiniteLoader from 'react-window-infinite-loader';
import { FixedSizeList } from 'react-window';

import { cn } from '@/lib/utils';

import { useLogFeed } from './hooks';
import type { LogRecord } from './types';
import { ConsoleNodesListFetchDocument } from '../graphql/__generated__/graphql';

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

type LogFeedContentProps = {
  items: LogRecord[];
  hasMore: boolean;
  fetchMore: () => Promise<void>;
  visibleCols: Set<LogFeedColumn>;
}

const getAttribute = (record: LogRecord, col: LogFeedColumn) => {
  switch (col) {
    case LogFeedColumn.Timestamp:
      const tsWithTZ = utcToZonedTime(record.timestamp, 'UTC');
      return format(tsWithTZ, 'LLL dd, y HH:mm:ss.SSS', { timeZone: 'UTC' });
    case LogFeedColumn.ColorDot:
      return '.';
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
      return record.message;
    default:
      throw new Error('not implemented');
  }
};

const LogFeedContent = ({ items, fetchMore, hasMore, visibleCols }: LogFeedContentProps) => {
  const [colWidths] = useState([300, 300, 300, 300, 300, 300, 300]);

  const headerOuterElRef = useRef<HTMLDivElement>(null);
  const headerInnerElRef = useRef<HTMLDivElement>(null);

  const listRef = useRef<FixedSizeList<string> | null>(null);
  const listOuterRef = useRef<HTMLDivElement | null>(null);
  const listInnerRef = useRef<HTMLDivElement | null>(null);
  const infiniteLoaderRef = useRef<InfiniteLoader | null>(null);

  const [isLoading, setIsLoading] = useState(false);
  const [isListReady, setIsListReady] = useState(false);

  const [maxWidth, setMaxWidth] = useState<number | string>('100%');

  const [onNextRenderCallback, setOnNextRenderCallback] = useState<() => void>();

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

    // get max row width
    let maxWidth = 0;
    Array.from(listInnerRef.current?.children || []).forEach(rowEl => {
      maxWidth = Math.max(maxWidth, rowEl.scrollWidth);
    });

    // adjust list inner
    if (listInnerRef.current) listInnerRef.current.style.width = `${maxWidth}px`;

    setMaxWidth(maxWidth);
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

  const Row = ({ index, style, data }: { index: any; style: any; data: any }) => {
    if (index === 0) {
      if (hasMore) return <div>Loading...</div>;
      else return <div>no more data</div>;
    }
    console.log(data);
    const record = items[hasMore ? index - 1 : index];
    const { visibleCols } = data;

    const els: JSX.Element[] = [];
    allLogFeedColumns.forEach(col => {
      if (visibleCols.has(col)) {
        els.push((
          <div
            key={col}
            className={cn(
              index % 2 !== 0 && 'bg-chrome-50',
              'whitespace-nowrap',
              (col === LogFeedColumn.Message) ? 'flex-grow' : 'shrink-0',
            )}
            style={(col === LogFeedColumn.Message) ? {} : { width: `300px` }}
          >
            {getAttribute(record, col)}
          </div>
        ));
      }
    })

    const { width, ...otherStyles } = style;
    return (
      <div className="flex" style={{ width: 'inherit', ...otherStyles }}>
        {els}
      </div>
    );
  };

  return (
    <div className="h-full flex flex-col">
      <div
        ref={headerOuterElRef}
        className="overflow-x-scroll no-scrollbar cursor-default"
        onScroll={handleHeaderScrollX}
      >
        <div
          ref={headerInnerElRef}
          className="flex"
          style={{ width: `${maxWidth}px` }}
        >
          {allLogFeedColumns.map(col => {
            if (visibleCols.has(col)) {
              return (
                <div
                  key={col}
                  className={cn(
                    'bg-chrome-100 uppercase',
                    (col === LogFeedColumn.Message) ? 'flex-grow' : 'shrink-0',
                  )}
                  style={(col === LogFeedColumn.Message) ? {} : { width: `300px` }}
                >
                  {col}
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
                    listRef.current = list;
                  }}
                  onItemsRendered={(args) => {
                    onItemsRendered(args);
                    handleItemsRendered();
                  }}
                  height={height}
                  width={width}
                  itemCount={itemCount}
                  itemSize={18}
                  outerRef={listOuterRef}
                  innerRef={listInnerRef}
                  initialScrollOffset={itemCount * 18}
                  itemData={{ visibleCols }}
                  overscanCount={5}
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

export type LogFeedViewerProps = {
  visibleCols: Set<LogFeedColumn>;
};

export const LogFeedViewer = ({ visibleCols }: LogFeedViewerProps) => {
  const { records } = useLogFeed();

  return (
    <LogFeedContent
      items={records}
      hasMore={false}
      fetchMore={async () => { }}
      visibleCols={visibleCols}
    />
  );
};
