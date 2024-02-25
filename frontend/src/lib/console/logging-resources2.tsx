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

import React, { createContext, useContext, useEffect, useRef, useState } from 'react';
import AutoSizer from 'react-virtualized-auto-sizer';
import InfiniteLoader from 'react-window-infinite-loader';
import { FixedSizeList, type ListOnItemsRenderedProps} from 'react-window';

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

export const LogFeedContent = () => {
  const [initTS] = useState(new Date());

  const listRef = useRef<FixedSizeList<string> | null>(null);
  const infiniteLoaderRef = useRef<InfiniteLoader | null>(null);

  const [hasMore, setHasMore] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  const [isListReady, setIsListReady] = useState(false);
  const [onItemsRenderedCallback, setOnItemsRenderedCallback] = useState<() => void>();
  const [scrollTo, setScrollTo] = useState<number>();

  const [items, setItems] = useState(() => {
    // init list
    let timestamps: string[] = [];
    for (let i = 0; i < 50; i++) {
      const newDate = new Date(initTS.getTime() - i * 1000);
      timestamps.unshift(newDate.toISOString());
    }
    return timestamps;
  });

  // leave extra space if there are more results
  const itemCount = (hasMore) ? items.length + 1 : items.length;

  // use first item as loading placeholder
  const isItemLoaded = (index: number) => {
    if (index === 0 && isListReady && hasMore) return false;
    return true;
  };

  /*
  useEffect(() => {
    setTimeout(() => {
      listRef.current?.scrollToItem(items.length);
      infiniteLoaderRef.current?.resetloadMoreItemsCache();
    }, 1000);
  }, []);*/

  /*
  useEffect(() => {
    const id = setInterval(() => {
      items.push((new Date()).toISOString())
      setItems(Array.from(items));
      listRef.current?.scrollToItem(items.length);
    }, 3000);
    return () => clearInterval(id);
  }, []);*/

  const loadMoreItems = async (startIndex: number, stopIndex: number) => {
    if (isLoading) return;
    console.log(startIndex);
    console.log(stopIndex);
    setIsLoading(true);

    setTimeout(() => {
      const startDate = new Date(items[0]);
      for (let i = 0; i < 30; i++) {
        const newDate = new Date(startDate.getTime() - i * 1000);
        items.unshift(newDate.toISOString());
      }

      // update state
      setItems(Array.from(items));
      setIsLoading(false);
      infiniteLoaderRef.current?.resetloadMoreItemsCache();
      setOnItemsRenderedCallback(() => {
        listRef.current?.scrollToItem(31);
      });

      // scroll to bottom
      //listRef.current?.scrollToItem(itemCount);
    }, 1000);
  }

  const handleItemsRendered = (args: ListOnItemsRenderedProps) => {
    if (onItemsRenderedCallback) {
      onItemsRenderedCallback();
      setOnItemsRenderedCallback(undefined);
    }
  };

  const Row = ({ index, style }: { index: any; style: any; }) => {
    if (index === 0 && hasMore) return <div>Loading...</div>;
    const content = items[hasMore ? index - 1 : index];
    return <div style={style}>{content}</div>;
  };

  return (
    <div className="h-full">
      Loading state: {isLoading.toString()}
      <div className="h-full">
        <AutoSizer>
          {({ height, width }) => (
            <InfiniteLoader
              ref={infiniteLoaderRef}
              isItemLoaded={isItemLoaded}
              itemCount={itemCount}
              loadMoreItems={loadMoreItems}
            >
              {({ onItemsRendered, ref }) => (
                <FixedSizeList
                  ref={list => {
                    ref(list);
                    listRef.current = list;

                    // scroll to bottom and change ready state
                    if (!isListReady) {
                      list?.scrollToItem(items.length);
                      setIsListReady(true);
                    }
                    console.log('xxx');
                  }}
                  onItemsRendered={(args) => {
                    onItemsRendered(args);
                    handleItemsRendered(args);
                  }}
                  height={height}
                  width={width}
                  itemCount={itemCount}
                  itemSize={18}
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
