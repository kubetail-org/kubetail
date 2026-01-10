// Copyright 2024-2025 Andres Morey
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
import { useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import { dashboardClient, getClusterAPIClient } from '@/apollo-client';
import AppLayout from '@/components/layouts/AppLayout';
import AuthRequired from '@/components/utils/AuthRequired';
// import { FakeClient } from '@/components/widgets/log-viewer';
import type { Client, LogViewerHandle } from '@/components/widgets/log-viewer';
import type { LogSourceFilter } from '@/lib/graphql/dashboard/__generated__/graphql';
import { useIsClusterAPIEnabled } from '@/lib/hooks';

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
  const sourceStringsRef = useRef<string[]>(null);

  const next = searchParams.getAll('source');

  if (!deepEqual(next, sourceStringsRef.current)) {
    sourceStringsRef.current = next;
  }

  return sourceStringsRef.current as string[];
}

/**
 * useStableSourceFilter - Custom hook that returns a stable reference to page source filter
 */

function useStableSourceFilter(searchParams: URLSearchParams) {
  const sourceFilterRef = useRef<LogSourceFilter>(null);

  const next = {
    region: searchParams.getAll('region'),
    zone: searchParams.getAll('zone'),
    os: searchParams.getAll('os'),
    arch: searchParams.getAll('arch'),
    node: searchParams.getAll('node'),
    container: searchParams.getAll('container'),
  } satisfies LogSourceFilter;

  if (!deepEqual(next, sourceFilterRef.current)) {
    sourceFilterRef.current = next;
  }

  return sourceFilterRef.current as LogSourceFilter;
}

/**
 * InnerLayout component
 */

type InnerLayoutProps = {
  sidebar: React.ReactElement;
  header: React.ReactElement;
  main: React.ReactElement;
};

const InnerLayout = ({ sidebar, header, main }: InnerLayoutProps) => {
  const { isSidebarOpen, setIsSidebarOpen } = useContext(PageContext);
  const [sidebarWidth, setSidebarWidth] = useState(300);

  const handleDrag = useCallback(() => {
    // change width when mouse moves
    const fn = (ev: MouseEvent) => {
      const newWidth = Math.max(ev.clientX, 180);
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
          <div className="absolute h-full bg-chrome-100 overflow-x-hidden" style={{ width: `${sidebarWidth}px` }}>
            {sidebar}
            <button
              type="button"
              onClick={handleCloseSidebar}
              title="Collapse sidebar"
              className="absolute cursor-pointer right-1.75 top-7.5 transform -translate-y-1/2"
            >
              <PanelLeftCloseIcon size={20} strokeWidth={2} className="text-chrome-500" />
            </button>
          </div>
          {/* eslint-disable-next-line jsx-a11y/no-static-element-interactions */}
          <div
            className="absolute bg-chrome-divider w-1 h-full border-l-2 border-chrome-100 cursor-ew-resize"
            style={{ left: `${sidebarWidth}px` }}
            onMouseDown={handleDrag}
          />
        </>
      )}
      <main
        className="h-full flex flex-col overflow-hidden"
        style={{ marginLeft: `${isSidebarOpen ? sidebarWidth + 4 : 0}px` }}
      >
        <div className="bg-chrome-100 border-b border-chrome-divider">{header}</div>
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
  const [logServerClient, setLogServerClient] = useState<Client>();

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
  useEffect(() => {
    if (shouldUseClusterAPI === undefined) return;

    let apolloClient: ApolloClient;
    if (shouldUseClusterAPI) {
      apolloClient = getClusterAPIClient({
        kubeContext: kubeContext ?? '',
        namespace: 'kubetail-system',
        serviceName: 'kubetail-cluster-api',
      });
    } else {
      apolloClient = dashboardClient;
    }

    setLogServerClient(
      new LogServerClient({
        apolloClient,
        kubeContext: kubeContext ?? '',
        sources: sourceStrings,
        sourceFilter,
        grep: grep ?? undefined,
      }),
    );
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
