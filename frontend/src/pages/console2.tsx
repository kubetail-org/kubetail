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

import {
  Clock as ClockIcon,
  Pause as PauseIcon,
  Play as PlayIcon,
  Settings as SettingsIcon,
  SkipForward as SkipForwardIcon,
} from 'lucide-react';
import { createContext, useContext, useReducer, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import AppLayout from '@/components/layouts/AppLayout';
import AuthRequired from '@/components/utils/AuthRequired';
import { 
  LoggingResourcesProvider,
  LogFeedContent,
  useLogFeed,
  Duration,
  StreamingState,
} from '@/lib/console';

type State = {
  since: Date | Duration | string;
  until: Date | string;
  streamingState: StreamingState;
};

type Context = {
  state: State;
  dispatch: React.Dispatch<Partial<State>>;
};

const Context = createContext<Context>({} as Context);

function reducer(prevState: State, newState: Partial<State>): State {
  return Object.assign({}, { ...prevState }, { ...newState });
}

/**
 * Sidebar component
 */

const Sidebar = () => {
  return (
    <div>sidebar</div>
  );
};

/**
 * Header component
 */

const Header = () => {
  const { state, dispatch } = useContext(Context);
  const feed = useLogFeed();

  const buttonCN = 'rounded-lg h-[40px] w-[40px] flex items-center justify-center enabled:hover:bg-chrome-200 disabled:opacity-30';

  return (
    <div className="grid grid-cols-2 p-1">
      <div className="flex px-2 justify-left">
        {state.streamingState === StreamingState.Streaming ? (
          <button
            className={buttonCN}
            title="Pause"
            onClick={() => dispatch({ streamingState: StreamingState.Paused })}
          >
            <PauseIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
          </button>
        ) : (
          <button
            className={buttonCN}
            title="Play"
            onClick={() => dispatch({ streamingState: StreamingState.Streaming })}
          >
            <PlayIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
          </button>
        )}
        <button
          className={buttonCN}
          title="Skip Forward"
        >
          <SkipForwardIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
        </button>
        <button className="cursor-pointer bg-chrome-200 hover:bg-chrome-300 py-1 px-2 rounded ml-1">
          <div className="w-[390px] flex text-primary font-medium text-sm items-center space-x-1">
            <ClockIcon size={15} strokeWidth={2} />
            <div>Feb 26, 2024 07:13:39 UTC</div>
            <div className="px-1">-</div>
            <div>Streaming</div>
            <div>{state.streamingState}</div>
          </div>
        </button>
      </div>
      <div className="h-full flex flex-col justify-end items-end">
        settings
      </div>
    </div>
  );
};

/**
 * Content component
 */

const Content = () => {
  const { state } = useContext(Context);
  //const { state, records, controls } = useLogFeed();

  return (
    <LogFeedContent 
      since={state.since}
      until={state.until}
      follow={state.streamingState === StreamingState.Streaming}
    />
  );
};

/**
 * Layout component
 */

type InnerLayoutProps = {
  sidebar: JSX.Element;
  header: JSX.Element;
  content: JSX.Element;
}

const InnerLayout = ({ sidebar, header, content }: InnerLayoutProps) => {
  const [sidebarWidth, setSidebarWidth] = useState(300);

  const handleDrag = () => {
    // change width when mouse moves
    const fn = (ev: MouseEvent) => {
      const newWidth = Math.max(ev.clientX, 100);
      setSidebarWidth(newWidth);
    };
    document.addEventListener('mousemove', fn);

    // show resize cursor
    const bodyCursor = document.body.style.cursor;
    document.body.style.cursor = 'ew-resize';

    // disable text select
    const onSelectStart = document.body.onselectstart;
    document.body.onselectstart = () => { return false; };

    // cleanup
    document.addEventListener('mouseup', function cleanup() {
      document.removeEventListener('mousemove', fn);
      document.body.style.cursor = bodyCursor;
      document.body.onselectstart = onSelectStart;
      document.removeEventListener('mouseup', cleanup);
    });
  }

  return (
    <div className="relative h-full">
      <div
        className="absolute h-full bg-chrome-100 overflow-x-hidden"
        style={{ width: `${sidebarWidth}px` }}
      >
        {sidebar}
      </div>
      <div
        className="absolute bg-chrome-divider w-[4px] h-full border-l-2 border-chrome-100 cursor-ew-resize"
        style={{ left: `${sidebarWidth}px` }}
        onMouseDown={handleDrag}
      />
      <main
        className="h-full flex flex-col overflow-hidden"
        style={{ marginLeft: `${sidebarWidth + 4}px` }}
      >
        <div className="bg-chrome-100 border-b border-chrome-divider">
          {header}
        </div>
        <div className="flex-grow">
          {content}
        </div>
      </main>
    </div>
  );
};

/**
 * Default component
 */

export default function Page() {
  const [searchParams] = useSearchParams();

  const [state, dispatch] = useReducer(reducer, {
    since: '-100',
    until: 'FOREVER',
    streamingState: StreamingState.Streaming,
  });

  return (
    <AuthRequired>
      <Context.Provider value={{ state, dispatch }}>
        <LoggingResourcesProvider sourcePaths={searchParams.getAll('source')}>
          <AppLayout>
            <InnerLayout
              sidebar={<Sidebar />}
              header={<Header />}
              content={<Content />}
            />
          </AppLayout>
        </LoggingResourcesProvider>
      </Context.Provider>
    </AuthRequired>
  );
}
