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

  const OuterElementType = forwardRef((props, ref) => {
    const { children, ...otherProps } = props;
    console.log(otherProps);
    return (
      <div
        ref={ref}
        className="relative"
        {...otherProps}
      >
        <div className="absolute top-0 z-10 w-auto">
          <div className="flex">
            <div className="w-[100px] bg-chrome-100">index</div>
            <div className="w-[300px] bg-chrome-100">col-1</div>
            <div className="w-[300px] bg-chrome-100">col-2</div>
            <div className="w-[300px] bg-chrome-100">col-3</div>
            <div className="w-[300px] bg-chrome-100">col-4</div>
            <div className="w-[300px] bg-chrome-100">col-5</div>
            <div className="w-[300px] bg-chrome-100">col-6</div>
            <div className="w-[300px] bg-chrome-100">col-7</div>
          </div>
        </div>

        {children}
      </div>
    );
  });

  const InnerElementType = forwardRef((props, ref) => {
    const { children, ...otherProps } = props;
    console.log(otherProps);
    return (
      <div
        ref={ref}
        {...otherProps}
      >
        {children}
      </div>
    );
  });

  const Row = ({ index, style }: { index: any; style: any; }) => {
    if (index === 0) {
      if (hasMore) return <div>Loading...</div>;
      else return <div>no more data</div>;
    }
    const content = items[hasMore ? index - 1 : index];

    // remove `width` from styles
    const { width, ...customStyle } = style;

    return <div className="flex" style={customStyle}>
      <div className="w-[100px]">{content}</div>
      <div className="w-[300px]">col-1</div>
      <div className="w-[300px]">col-2</div>
      <div className="w-[300px]">col-3</div>
      <div className="w-[300px]">col-4</div>
      <div className="w-[300px]">col-5</div>
      <div className="w-[300px]">col-6</div>
      <div className="w-[300px]">col-7</div>
    </div>;
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
              outerElementType={OuterElementType}
              innerElementType={InnerElementType}
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
