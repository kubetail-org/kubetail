// Copyright 2024 The Kubetail Authors
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

import type { ApolloClient } from '@apollo/client';
import deepEqual from 'fast-deep-equal';
import { PanelLeftClose as PanelLeftCloseIcon } from 'lucide-react';
import { useCallback, useContext, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import { dashboardClient, getClusterAPIClient } from '@/apollo-client';
import AppLayout from '@/components/layouts/AppLayout';
import AuthRequired from '@/components/utils/AuthRequired';
// import { FakeClient } from '@/components/widgets/log-viewer';
import type { LogViewerHandle } from '@/components/widgets/log-viewer';
import type { LogSourceFilter } from '@/lib/graphql/dashboard/__generated__/graphql';
import { useIsClusterAPIEnabled } from '@/lib/hooks';
import { cn } from '@/lib/util';

import { Header } from './header';
import { LogServerClient } from './log-server-client';
import { Main } from './main';
import { PageContext } from './shared';
import { Sidebar } from './sidebar';
import { ConfigureContainerColors, SourcesFetcher } from './helpers';

/**
 * useStableSourceStrings - Custom hook that returns a stable reference to page source strings
 */

function useStableSourceStrings(searchParams: URLSearchParams) {
  const next = searchParams.getAll('source');
  const [value, setValue] = useState(next);

  if (!deepEqual(next, value)) {
    setValue(next);
  }

  return value;
}

/**
 * useStableSourceFilter - Custom hook that returns a stable reference to page source filter
 */

function useStableSourceFilter(searchParams: URLSearchParams) {
  const next = {
    region: searchParams.getAll('region'),
    zone: searchParams.getAll('zone'),
    os: searchParams.getAll('os'),
    arch: searchParams.getAll('arch'),
    node: searchParams.getAll('node'),
    container: searchParams.getAll('container'),
  } satisfies LogSourceFilter;

  const [value, setValue] = useState(next);

  if (!deepEqual(next, value)) {
    setValue(next);
  }

  return value;
}

/**
 * InnerLayout component
 */

type InnerLayoutProps = {
  sidebar: React.ReactElement;
  header: React.ReactElement;
  main: React.ReactElement;
};

// Starting width auto-fits the sidebar content (longest source/container name)
// between a floor that keeps the "Pods/Containers" header from overflowing and
// a ceiling that keeps it from getting too wide.
const SIDEBAR_MIN_WIDTH = 220;
const SIDEBAR_MAX_WIDTH = 300;

const InnerLayout = ({ sidebar, header, main }: InnerLayoutProps) => {
  const { isSidebarOpen, setIsSidebarOpen } = useContext(PageContext);
  // null until the user resizes: the panel auto-fits its content via CSS while
  // we mirror the resulting width here so <main> and the drag handle stay aligned.
  const [sidebarWidth, setSidebarWidth] = useState<number | null>(null);
  const [autoWidth, setAutoWidth] = useState(SIDEBAR_MAX_WIDTH);
  const sidebarRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    if (!isSidebarOpen || sidebarWidth !== null) return undefined;
    const el = sidebarRef.current;
    if (!el) return undefined;

    const update = () => setAutoWidth(el.offsetWidth);
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, [isSidebarOpen, sidebarWidth]);

  const width = sidebarWidth ?? autoWidth;

  const handleDrag = useCallback(() => {
    // change width when mouse moves
    const fn = (ev: MouseEvent) => {
      const newWidth = Math.max(ev.clientX, SIDEBAR_MIN_WIDTH);
      setSidebarWidth(newWidth);
    };
    document.addEventListener('mousemove', fn);

    // show resize cursor
    const bodyCursor = document.body.style.cursor;
    document.body.style.cursor = 'ew-resize';

    // disable text select
    const onSelectStart = document.body.onselectstart;
    document.body.onselectstart = () => false;

    // cleanup
    document.addEventListener('mouseup', function cleanup() {
      document.removeEventListener('mousemove', fn);
      document.body.style.cursor = bodyCursor;
      document.body.onselectstart = onSelectStart;
      document.removeEventListener('mouseup', cleanup);
    });
  }, []);

  const handleCloseSidebar = useCallback(() => {
    setIsSidebarOpen(false);
  }, [setIsSidebarOpen]);

  return (
    <div className="relative h-full">
      {isSidebarOpen && (
        <>
          <div
            ref={sidebarRef}
            className="absolute h-full bg-sidebar overflow-x-hidden"
            style={
              sidebarWidth === null
                ? { width: 'max-content', minWidth: SIDEBAR_MIN_WIDTH, maxWidth: SIDEBAR_MAX_WIDTH }
                : { width: `${sidebarWidth}px` }
            }
          >
            {sidebar}
            <button
              type="button"
              onClick={handleCloseSidebar}
              title="Collapse sidebar"
              className="absolute cursor-pointer right-1.75 top-7.5 transform -translate-y-1/2"
            >
              <PanelLeftCloseIcon size={20} strokeWidth={2} />
            </button>
          </div>
          {/*
            Wide, transparent drag affordance centered on the 1px divider
            (which is <main>'s border-l). Widening the hit area here keeps the
            grab target easy without thickening the visible edge.
          */}
          {/* eslint-disable-next-line jsx-a11y/no-static-element-interactions */}
          <div
            className="absolute top-0 z-10 h-full w-2 -translate-x-1/2 cursor-ew-resize"
            style={{ left: `${width}px` }}
            onMouseDown={handleDrag}
          />
        </>
      )}
      <main
        className={cn('h-full flex flex-col overflow-hidden', isSidebarOpen && 'border-l border-sidebar-border')}
        style={{ marginLeft: `${isSidebarOpen ? width : 0}px` }}
      >
        <div>{header}</div>
        <div className="grow min-h-0">{main}</div>
      </main>
    </div>
  );
};

/**
 * Page component
 */

export default function Page() {
  const [isSidebarOpen, setIsSidebarOpen] = useState(true);
  const logViewerRef = useRef<LogViewerHandle>(null);

  const [searchParams] = useSearchParams();
  const kubeContext = searchParams.get('kubeContext');

  const shouldUseClusterAPI = useIsClusterAPIEnabled(kubeContext);

  const sourceStrings = useStableSourceStrings(searchParams);
  const sourceFilter = useStableSourceFilter(searchParams);

  // Process the grep parameter
  const grepVal = searchParams.get('grep');
  const grep = useMemo(() => {
    if (!grepVal) return null;

    // If the input is in the format /regex/, extract the regex pattern
    const regexMatch = /^\/(.+)\/$/.exec(grepVal);
    if (regexMatch) {
      // Return the regex pattern without the slashes
      return regexMatch[1];
    }

    // Otherwise, escape special regex characters to make it a literal string search
    return grepVal.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  }, [grepVal]);

  // Configure log server client
  const logServerClient = useMemo(() => {
    if (shouldUseClusterAPI === undefined) return undefined;

    let apolloClient: ApolloClient;
    if (shouldUseClusterAPI) {
      apolloClient = getClusterAPIClient(kubeContext ?? '');
    } else {
      apolloClient = dashboardClient;
    }

    return new LogServerClient({
      apolloClient,
      kubeContext: kubeContext ?? '',
      sources: sourceStrings,
      sourceFilter,
      grep: grep ?? undefined,
    });
  }, [shouldUseClusterAPI, kubeContext, sourceStrings, sourceFilter, grep]);

  const context = useMemo(
    () => ({
      kubeContext,
      shouldUseClusterAPI,
      logServerClient,
      grep,
      logViewerRef,
      isSidebarOpen,
      setIsSidebarOpen,
    }),
    [kubeContext, shouldUseClusterAPI, logServerClient, grep, isSidebarOpen],
  );

  return (
    <AuthRequired>
      <PageContext.Provider value={context}>
        <AppLayout>
          <InnerLayout sidebar={<Sidebar />} header={<Header />} main={<Main />} />
        </AppLayout>
        <SourcesFetcher />
        <ConfigureContainerColors />
      </PageContext.Provider>
    </AuthRequired>
  );
}
