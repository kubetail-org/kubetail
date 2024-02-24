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

import { createContext, useContext, useEffect, useRef, useState } from 'react';
import AutoSizer from 'react-virtualized-auto-sizer';
import InfiniteLoader from 'react-window-infinite-loader';
import { FixedSizeList } from 'react-window';

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
  const [initialStartDate] = useState(new Date());

  const [isLoading, setIsLoading] = useState(false);

  const [items, setItems] = useState(() => {
    // init list
    let timestamps: string[] = [];
    for (let i = 0; i < 100; i++) {
      const newDate = new Date(initialStartDate.getTime() + i * 1000);
      timestamps.push(newDate.toISOString());
    }
    return timestamps;
  });

  const isItemLoaded = (index: number) => {
    if (index <= 0) return false;
    return true;
  };

  const loadMoreItems = async (startIndex: number, stopIndex: number) => {
    if (isLoading) return;
    console.log(startIndex);
    console.log(stopIndex);
    setIsLoading(true);

    setTimeout(() => {
      const startDate = new Date(items[0]);
      for (let i = 0; i < 100; i++) {
        const newDate = new Date(startDate.getTime() - i * 1000);
        items.unshift(newDate.toISOString());
      }
      setItems(Array.from(items));
      setIsLoading(false);
    }, 1000);
  }

  const Row = ({ index, style }: { index: any; style: any; }) => {
    if (index === 0) return <div>Loading...</div>;
    const content = items[index];
    return <div style={style}>{content}</div>;
  };

  return (
    <div className="h-full">
      Loading state: {isLoading.toString()}
      <div className="h-full">
        <AutoSizer>
          {({ height, width }) => (
            <InfiniteLoader
              isItemLoaded={isItemLoaded}
              itemCount={items.length}
              loadMoreItems={loadMoreItems}
            >
              {({ onItemsRendered, ref }: { onItemsRendered: any; ref: any; }) => (
                <FixedSizeList
                  height={height}
                  width={width}
                  itemCount={items.length}
                  onItemsRendered={onItemsRendered}
                  ref={ref}
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
