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
  const [items, setItems] = useState(() => {
    const items: string[] = [];
    for (let i=0; i < 10; i++) items.push(`row ${i}`);
    return items;
  });

  useEffect(() => {
    const id = setInterval(() => {
      items.push(`row ${items.length + 1}`);
      setItems(Array.from(items));
    }, 1000);

    return () => clearInterval(id);
  }, []);

  const [isNextPageLoading, setIsNextPageLoading] = useState(false);

  const hasNextPage = true;
  const loadNextPage = () => {
    setIsNextPageLoading(true);
    for (let i=items.length; i<(items.length + 50); i++) items.push(`row ${i}`);
    setItems(Array.from(items));
    setTimeout(() => setIsNextPageLoading(false), 1000);
  };

  const itemCount = hasNextPage ? items.length + 1 : items.length;
  //const loadMoreItems = isNextPageLoading ? () => { } : loadNextPage;
  //const isItemLoaded = index => !hasNextPage || index < items.length;

  const isItemLoaded = (index: number) => {
    if (index > items.length) return false;
    return true;
  }

  const loadMoreItems = (startIndex: number, stopIndex: number) => {
    /*for (let i=items.length; i<(items.length + 20); i++) items.unshift(`row ${i}`);*/
    console.log(args);
  };

  const Row = ({ index, style }: { index: any; style: any; }) => {
    //let content;
    //if (!isItemLoaded(index)) content = "Loading...";
    //else content = items[index];
    const content = items[index];
    return <div style={style}>{content}</div>;
  };

  return (
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
