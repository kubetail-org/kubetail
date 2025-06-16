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

import { PanelLeftClose as PanelLeftCloseIcon } from 'lucide-react';
import { useCallback, useContext, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import AppLayout from '@/components/layouts/AppLayout';
import AuthRequired from '@/components/utils/AuthRequired';
import { safeDigest } from '@/lib/util';

import { Header } from './header';
import { PageContext } from './shared';
import { Sidebar } from './sidebar';
import { cssID } from './util';
import { Viewer, ViewerProvider, useSources } from './viewer';
import type { ViewerHandle } from './viewer';

/**
 * Configure container colors component
 */

const palette = [
  '#3B6EDC', // Muted Blue
  '#2F9E5F', // Muted Green
  '#D14343', // Muted Red
  '#D38B2A', // Muted Amber
  '#8456D8', // Muted Purple
  '#2C9CB3', // Muted Cyan
  '#7A6BD1', // Muted Violet
  '#D14D8A', // Muted Pink
  '#7FA83A', // Muted Lime
  '#E06C3A', // Muted Orange
  '#2F9A8A', // Muted Teal
  '#5C63D6', // Muted Indigo
  '#A46A3D', // Muted Brown
  '#C24A77', // Muted Rose
  '#6B8F3A', // Muted Forest Green
  '#4B4FCF', // Muted Deep Blue
  '#9A4EB3', // Muted Magenta
  '#BFA23A', // Muted Gold
  '#4A84C8', // Muted Sky Blue
  '#247A8A', // Muted Blue-Green
];

const ConfigureContainerColors = () => {
  const { sources } = useSources();
  const containerKeysRef = useRef(new Set<string>());

  sources.forEach((source) => {
    const k = cssID(source.namespace, source.podName, source.containerName);

    // skip if previously defined
    if (containerKeysRef.current.has(k)) return;
    containerKeysRef.current.add(k);

    (async () => {
      // set css var
      const colorIDX = (await safeDigest(k)).getUint32(0) % palette.length;
      document.documentElement.style.setProperty(`--${k}-color`, palette[colorIDX]);
    })();
  });

  return null;
};

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
              className="absolute cursor-pointer right-[7px] top-[30px] transform -translate-y-1/2"
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
        <div className="grow">{main}</div>
      </main>
    </div>
  );
};

/**
 * Page component
 */

export default function Page() {
  const [searchParams] = useSearchParams();
  const [isSidebarOpen, setIsSidebarOpen] = useState(true);
  const viewerRef = useRef<ViewerHandle>(null);

  // Memoize list args
  const [sources, sourceFilter] = useMemo(
    () => [
      searchParams.getAll('source'),
      {
        region: searchParams.getAll('region'),
        zone: searchParams.getAll('zone'),
        os: searchParams.getAll('os'),
        arch: searchParams.getAll('arch'),
        node: searchParams.getAll('node'),
        container: searchParams.getAll('container'),
      },
    ],
    [searchParams],
  );

  const context = useMemo(
    () => ({
      isSidebarOpen,
      setIsSidebarOpen,
    }),
    [isSidebarOpen, setIsSidebarOpen],
  );

  const grepVal = searchParams.get('grep');

  // Process the grep parameter
  const processedGrep = useMemo(() => {
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

  return (
    <AuthRequired>
      <PageContext.Provider value={context}>
        <ViewerProvider
          kubeContext={searchParams.get('kubeContext')}
          sources={sources}
          sourceFilter={sourceFilter}
          grep={processedGrep}
        >
          <ConfigureContainerColors />
          <AppLayout>
            <InnerLayout
              sidebar={<Sidebar />}
              header={<Header viewerRef={viewerRef} />}
              main={
                <Viewer
                  ref={viewerRef}
                  defaultMode={searchParams.get('mode')}
                  defaultSince={searchParams.get('since')}
                />
              }
            />
          </AppLayout>
        </ViewerProvider>
      </PageContext.Provider>
    </AuthRequired>
  );
}
