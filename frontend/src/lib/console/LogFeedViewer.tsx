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

import { useEffect, useRef, useState } from 'react';
import AutoSizer from 'react-virtualized-auto-sizer';
import InfiniteLoader from 'react-window-infinite-loader';
import { FixedSizeList } from 'react-window';

import { cn } from '@/lib/utils';

import { useLogFeed } from './hooks';

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
  items: [number, number, number, number, number, number, number][];
  hasMore: boolean;
  fetchMore: () => Promise<void>;
  visibleCols: Set<LogFeedColumn>;
}

const LogFeedContent = ({ items, fetchMore, hasMore, visibleCols }: LogFeedContentProps) => {
  const [colWidths ] = useState([300, 300, 300, 300, 300, 300, 300]);

  const headerElRef = useRef<HTMLDivElement>(null);

  const listRef = useRef<FixedSizeList<string> | null>(null);
  const listOuterRef = useRef<HTMLDivElement | null>(null);
  const infiniteLoaderRef = useRef<InfiniteLoader | null>(null);

  const [isLoading, setIsLoading] = useState(false);
  const [isListReady, setIsListReady] = useState(false);

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
  };

  const handleHeaderScrollX = (ev: React.UIEvent<HTMLDivElement>) => {
    const headerEl = ev.target as HTMLDivElement;
    const contentEl = listOuterRef.current;
    if (!contentEl) return;
    contentEl.scrollTo({ left: headerEl.scrollLeft, behavior: 'instant' });
  };

  const handleContentScrollX = (ev: React.UIEvent<HTMLDivElement>) => {
    const contentEl = ev.target as HTMLDivElement;
    const headerEl = headerElRef.current;
    if (!headerEl) return;
    headerEl.scrollTo({ left: contentEl.scrollLeft, behavior: 'instant' });
  };

  useEffect(() => {
    const listOuterEl = listOuterRef.current;
    if (!listOuterEl) return;
    listOuterEl.addEventListener('scroll', handleContentScrollX as any);
    return () => listOuterEl.removeEventListener('scroll', handleContentScrollX as any);
  }, [isListReady, handleContentScrollX]);

  const Row = ({ index, style }: { index: any; style: any; }) => {
    if (index === 0) {
      if (hasMore) return <div>Loading...</div>;
      else return <div>no more data</div>;
    }
    const content = items[hasMore ? index - 1 : index];

    const els: JSX.Element[] = [];
    allLogFeedColumns.forEach(col => {
      if (visibleCols.has(col)) {
        els.push((
          <div
            key={col}
            className={cn(
              'shrink-0',
              col === LogFeedColumn.Message && 'flex-grow',
              index % 2 !== 0 && 'bg-chrome-50',
            )}
            style={{ width: `300px` }}
          >
            {content[0]}
          </div>
        ));
      }
    })

    return (
      <div className="flex" style={style}>
        {els}
      </div>
    );
  };

  return (
    <div className="h-full flex flex-col">
      <div
        ref={headerElRef}
        className="w-full overflow-auto no-scrollbar cursor-default flex"
        onScroll={handleHeaderScrollX}
      >
        {allLogFeedColumns.map(col => {
          if (visibleCols.has(col)) {
            return (
              <div
                key={col}
                className={cn('shrink-0 bg-chrome-100 uppercase', col === LogFeedColumn.Message && 'flex-grow')}
                style={{ width: `300px` }}
              >
                {col}
              </div>
            );
          }
        })}
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
                  initialScrollOffset={itemCount * 18}
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
      fetchMore={async () => {}}
      visibleCols={visibleCols}
    />
  );
};
