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

import React, { createContext, forwardRef, useContext, useEffect, useRef, useState } from 'react';
import AutoSizer from 'react-virtualized-auto-sizer';
import InfiniteLoader from 'react-window-infinite-loader';
import { FixedSizeList, FixedSizeGrid, type ListOnItemsRenderedProps, type FixedSizeGridProps } from 'react-window';

import { cn } from '@/lib/utils';

type Context = {};

const Context = createContext<Context>({} as Context);

/**
 * Log feed hook
 */

export function useLogFeed() {
  return {
    play: () => console.log('play'),
    pause: () => console.log('pause'),
    skipForward: () => console.log('skipForward'),
    query: () => console.log('query'),
  };
}

/**
 * Log feed content component
 */

type OnItemsRenderedCallbackFunction = (args: ListOnItemsRenderedProps) => void;

export const LogFeedContent = () => {
  const [colWidths, setColWidths] = useState([100, 300, 300, 300, 300, 300, 300]);

  const headerElRef = useRef<HTMLDivElement>(null);

  const listRef = useRef<FixedSizeList<string> | null>(null);
  const listOuterRef = useRef<HTMLDivElement | null>(null);
  const listInnerRef = useRef<HTMLDivElement | null>(null);
  const infiniteLoaderRef = useRef<InfiniteLoader | null>(null);

  const [hasMore, setHasMore] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  const [isListReady, setIsListReady] = useState(false);

  const [onNextRenderCallback, setOnNextRenderCallback] = useState<OnItemsRenderedCallbackFunction | undefined>();

  const [items, setItems] = useState(() => {
    // init list
    let items: [number, number, number, number, number, number, number][] = [];
    for (let i = 0; i < 50; i++) {
      items.push([
        i,
        Math.random(),
        Math.random(),
        Math.random(),
        Math.random(),
        Math.random(),
        Math.random(),
      ]);
    }
    return items;
  });

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

    setTimeout(() => {
      const startNum = items[0][0];
      for (let i = 1; i <= 30; i++) {
        items.unshift([
          startNum - i,
          Math.random(),
          Math.random(),
          Math.random(),
          Math.random(),
          Math.random(),
          Math.random(),
        ]);
      }

      // current scrollPos
      const scrollPos = listOuterRef.current?.scrollTop || 0;

      // update state
      setItems(Array.from(items));
      setIsLoading(false);

      if (items[1][0] < -100) setHasMore(false);

      // reset cache and keep scrollPos in place
      setOnNextRenderCallback(() => {
        infiniteLoaderRef.current?.resetloadMoreItemsCache();
        setTimeout(() => listRef.current?.scrollTo(scrollPos + (30 * 18)), 0);
      });
    }, 1000);
  }

  const handleItemsRendered = (args: ListOnItemsRenderedProps) => {
    // set isListReady
    if (!isListReady) setIsListReady(true);

    // execute callback if available
    if (onNextRenderCallback) {
      onNextRenderCallback(args);
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

    // remove `width` from styles
    const { width, ...customStyle } = style;

    return (
      <div className="flex" style={customStyle}>
        <div className="shrink-0" style={{ width: `${colWidths[0]}px` }}>{content[0]}</div>
        <div className="shrink-0" style={{ width: `${colWidths[1]}px` }}>{content[1]}</div>
        <div className="shrink-0" style={{ width: `${colWidths[2]}px` }}>{content[2]}</div>
        <div className="shrink-0" style={{ width: `${colWidths[3]}px` }}>{content[3]}</div>
        <div className="shrink-0" style={{ width: `${colWidths[4]}px` }}>{content[4]}</div>
        <div className="shrink-0" style={{ width: `${colWidths[5]}px` }}>{content[5]}</div>
        <div className="shrink-0" style={{ width: `${colWidths[6]}px` }}>{content[6]}</div>
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
        <div className="bg-chrome-100 shrink-0" style={{ width: `${colWidths[0]}px` }}>index</div>
        <div className="bg-chrome-100 shrink-0" style={{ width: `${colWidths[1]}px` }}>col-1</div>
        <div className="bg-chrome-100 shrink-0" style={{ width: `${colWidths[2]}px` }}>col-2</div>
        <div className="bg-chrome-100 shrink-0" style={{ width: `${colWidths[3]}px` }}>col-3</div>
        <div className="bg-chrome-100 shrink-0" style={{ width: `${colWidths[4]}px` }}>col-4</div>
        <div className="bg-chrome-100 shrink-0" style={{ width: `${colWidths[5]}px` }}>col-5</div>
        <div className="bg-chrome-100 shrink-0" style={{ width: `${colWidths[6]}px` }}>col-6</div>
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
                    handleItemsRendered(args);
                  }}
                  height={height}
                  width={width}
                  itemCount={itemCount}
                  itemSize={18}
                  outerRef={listOuterRef}
                  innerRef={listInnerRef}
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

/*

type OnItemsRenderedCallbackFunction = (args: ListOnItemsRenderedProps) => void;

export const LogFeedContent = () => {
  const listRef = useRef<FixedSizeList<string> | null>(null);
  const listOuterRef = useRef<HTMLDivElement | null>(null);
  const listInnerRef = useRef<HTMLDivElement | null>(null);
  const infiniteLoaderRef = useRef<InfiniteLoader | null>(null);

  const [hasMore, setHasMore] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  const [isListReady, setIsListReady] = useState(false);

  const [onItemsRenderedCallback, setOnItemsRenderedCallback] = useState<OnItemsRenderedCallbackFunction | undefined>(() => {
    return () => setIsListReady(true);
  });

  const [items, setItems] = useState(() => {
    // init list
    let items: number[] = [];
    for (let i = 0; i < 50; i++) items.push(i);
    return items;
  });

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

    setTimeout(() => {
      const startNum = items[0];
      for (let i = 1; i <= 30; i++) items.unshift(startNum - i);

      // current scrollPos
      const scrollPos = listOuterRef.current?.scrollTop || 0;

      // update state
      setItems(Array.from(items));
      setIsLoading(false);

      if (items[1] < -100) setHasMore(false);

      // go back to scrollPos
      setOnItemsRenderedCallback(() => {
        setTimeout(() => {
          listRef.current?.scrollTo(scrollPos + (30 * 18));
          infiniteLoaderRef.current?.resetloadMoreItemsCache();
        }, 0);
      });
    }, 1000);
  }

  const handleItemsRendered = (args: ListOnItemsRenderedProps) => {
    onItemsRenderedCallback && onItemsRenderedCallback(args);
    setOnItemsRenderedCallback(undefined);
  };

  const Row = ({ index, style }: { index: any; style: any; }) => {
    if (index === 0) {
      if (hasMore) return <div>Loading...</div>;
      else return <div>no more data</div>;
    }
    const content = items[hasMore ? index - 1 : index];
    return <div style={style}>{content}</div>;
  };

  return (
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
                handleItemsRendered(args);
              }}
              height={height}
              width={width}
              itemCount={itemCount}
              itemSize={18}
              outerRef={listOuterRef}
              innerRef={listInnerRef}
              initialScrollOffset={itemCount * 18}
            >
              {Row}
            </FixedSizeList>
          )}
        </InfiniteLoader>
      )}
    </AutoSizer>
  );
};
*/

/**
 * Provider component
 */

interface LoggingResourcesProviderProps extends React.PropsWithChildren {
  sourcePaths: string[];
};

export const LoggingResourcesProvider = ({ sourcePaths, children }: LoggingResourcesProviderProps) => {
  return (
    <Context.Provider value={{}}>
      {children}
    </Context.Provider>
  );
};
