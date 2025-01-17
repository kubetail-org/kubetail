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

import { PlusCircleIcon, TrashIcon } from '@heroicons/react/24/solid';
import distinctColors from 'distinct-colors';
import {
  History as HistoryIcon,
  Pause as PauseIcon,
  Play as PlayIcon,
  Settings as SettingsIcon,
  SkipBack as SkipBackIcon,
  SkipForward as SkipForwardIcon,
} from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import Form from '@kubetail/ui/elements/Form';
import { Popover, PopoverTrigger, PopoverContent } from '@kubetail/ui/elements/Popover';
import Spinner from '@kubetail/ui/elements/Spinner';

import logo from '@/assets/logo.svg';
import AppLayout from '@/components/layouts/AppLayout';
import AuthRequired from '@/components/utils/AuthRequired';
import { DateRangeDropdown, DateRangeDropdownOnChangeArgs } from '@/components/widgets/DateRangeDropdown';
import SourcePickerModal from '@/components/widgets/SourcePickerModal';
import { cssID } from '@/lib/console/helpers';
import { LoggingResourcesProvider, usePods, useWorkloads } from '@/lib/console/logging-resources';
import type { Pod } from '@/lib/console/logging-resources';
import {
  LogFeedColumn,
  LogFeedViewer,
  allLogFeedColumns,
  useLogFeedControls,
  useLogFeedFacets,
  useLogFeedFilters,
  useLogFeedIsWrap,
  useLogFeedMetadata,
  useLogFeedVisibleCols,
} from '@/lib/console/logfeed';
import {
  Counter, MapSet, getBasename, joinPaths,
} from '@/lib/util';
import { allWorkloads, iconMap, labelsPMap } from '@/lib/workload';

/**
 * Configure container colors component
 */

const palette = distinctColors({
  count: 20,
  chromaMin: 40,
  chromaMax: 100,
  lightMin: 20,
  lightMax: 80,
});

const ConfigureContainerColors = () => {
  const { pods } = usePods();
  const containerKeysRef = useRef(new Set<string>());

  pods.forEach((pod) => {
    pod.spec.containers.forEach((container) => {
      const k = cssID(pod, container.name);

      // skip if previously defined
      if (containerKeysRef.current.has(k)) return;
      containerKeysRef.current.add(k);

      (async () => {
        // get color
        const streamUTF8 = new TextEncoder().encode(k);
        const buffer = await crypto.subtle.digest('SHA-256', streamUTF8);
        const view = new DataView(buffer);
        const colorIDX = view.getUint32(0) % 20;

        // set css var
        document.documentElement.style.setProperty(`--${k}-color`, palette[colorIDX].hex());
      })();
    });
  });

  return null;
};

/**
 * Settings button
 */

const SettingsButton = () => {
  const [visibleCols, setVisibleCols] = useLogFeedVisibleCols();
  const [isWrap, setIsWrap] = useLogFeedIsWrap();

  const handleOnChange = (col: LogFeedColumn, ev: React.ChangeEvent<HTMLInputElement>) => {
    const newSet = new Set(visibleCols);
    if (ev.target.checked) newSet.add(col);
    else newSet.delete(col);
    setVisibleCols(newSet);
  };

  const checkboxEls: JSX.Element[] = [];

  allLogFeedColumns.forEach((col) => {
    checkboxEls.push(
      <Form.Check
        key={col}
        label={col}
        checked={visibleCols.has(col)}
        onChange={(ev) => handleOnChange(col, ev)}
      />,
    );
  });

  return (
    <Popover>
      <PopoverTrigger>
        <SettingsIcon size={18} strokeWidth={1.5} />
      </PopoverTrigger>
      <PopoverContent
        className="bg-background w-auto mr-1 text-sm"
        onOpenAutoFocus={(ev) => ev.preventDefault()}
        sideOffset={-1}
      >
        <div className="border-b mb-1">Columns:</div>
        {checkboxEls}
        <div className="border-b mt-2 mb-1">Options:</div>
        <Form.Check
          label="Wrap"
          checked={isWrap}
          onChange={() => setIsWrap(!isWrap)}
        />
      </PopoverContent>
    </Popover>
  );
};

/**
 * Sidebar workloads component
 */

const SidebarWorkloads = () => {
  const { loading, workloads } = useWorkloads();
  const [isPickerOpen, setIsPickerOpen] = useState(false);
  const [searchParams, setSearchParams] = useSearchParams();

  const kubeContext = searchParams.get('kubeContext') || '';

  const deleteSource = (sourcePath: string) => {
    searchParams.delete('source', sourcePath);
    setSearchParams(new URLSearchParams(searchParams), { replace: true });
  };

  return (
    <>
      {isPickerOpen && <SourcePickerModal onClose={() => setIsPickerOpen(false)} />}
      {kubeContext !== '' && (
        <div
          className="mb-2 font-bold text-primary overflow-hidden text-ellipsis whitespace-nowrap"
          title={kubeContext}
        >
          Cluster:
          {kubeContext}
        </div>
      )}
      <div className="flex items-center justify-between mb-1">
        <div className="flex items-center space-x-1">
          <span className="font-bold text-chrome-500">Sources</span>
          {loading && <Spinner className="h-[15px] w-[15px]" />}
        </div>
        <button
          type="button"
          onClick={() => setIsPickerOpen(true)}
          className="cursor-pointer"
          aria-label="Open source picker"
        >
          <PlusCircleIcon className="h-[24px] w-[24px] text-primary" />
        </button>
      </div>
      <div className="space-y-2">
        {allWorkloads.map((workload) => {
          const objs = workloads.get(workload);
          if (!objs) return;
          const Icon = iconMap[workload];
          return (
            <div key={workload}>
              <div className="flex items-center space-x-1">
                <div><Icon className="h-[18px] w-[18px]" /></div>
                <div className="font-semibold text-chrome-500">{labelsPMap[workload]}</div>
              </div>
              <ul className="pl-[23px]">
                {objs.map((obj) => (
                  <li key={obj.id} className="flex items-center justify-between">
                    <span className="whitespace-nowrap overflow-hidden text-ellipsis">{obj.metadata.name}</span>
                    <button
                      type="button"
                      onClick={() => deleteSource(`${obj.metadata.namespace}/${workload}/${obj.metadata.name}`)}
                      aria-label="Delete source"
                    >
                      <TrashIcon className="h-[18px] w-[18px] text-chrome-300 hover:text-chrome-500 cursor-pointer" />
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          );
        })}
      </div>
    </>
  );
};

/**
 * Sidebar pods and containers component
 */

const Containers = ({ pod }: { pod: Pod }) => {
  const [searchParams, setSearchParams] = useSearchParams();

  const containers = Array.from(pod.spec.containers);
  containers.sort((a, b) => a.name.localeCompare(b.name));

  const handleToggle = (ev: React.ChangeEvent<HTMLInputElement>) => {
    const { name, value, checked } = ev.currentTarget;
    if (checked) searchParams.append(name, value);
    else searchParams.delete(name, value);
    setSearchParams(new URLSearchParams(searchParams));
  };

  return (
    <>
      {containers.map((container) => {
        const k = cssID(pod, container.name);
        const urlKey = 'container';
        const urlVal = `${pod.metadata.namespace}/${pod.metadata.name}/${container.name}`;
        return (
          <div key={container.name} className="flex item-center justify-between">
            <div className="flex items-center space-x-1">
              <div className="w-[13px] h-[13px]" style={{ backgroundColor: `var(--${k}-color)` }} />
              <div>{container.name}</div>
            </div>
            <Form.Check
              checked={searchParams.has(urlKey, urlVal)}
              name={urlKey}
              value={urlVal}
              onChange={handleToggle}
            />
          </div>
        );
      })}
    </>
  );
};

const SidebarPodsAndContainers = () => {
  const { pods } = usePods();

  return (
    <>
      <div className="border-t border-chrome-divider mt-[10px]" />
      <div className="py-[10px] font-bold text-chrome-500">Pods/Containers</div>
      <div className="space-y-3">
        {pods.map((pod) => (
          <div key={pod.metadata.uid}>
            <div className="flex items-center justify-between">
              <div className="whitespace-nowrap overflow-hidden text-ellipsis">{pod.metadata.name}</div>
            </div>
            <Containers pod={pod} />
          </div>
        ))}
      </div>
    </>
  );
};

/**
 * Sidebar facets component
 */

const Facets = ({ label, counter }: { label: string, counter: Counter }) => {
  const [searchParams, setSearchParams] = useSearchParams();
  const urlKey = label.toLocaleLowerCase();

  const handleToggle = (ev: React.ChangeEvent<HTMLInputElement>) => {
    const { name, value, checked } = ev.currentTarget;
    if (checked) searchParams.append(name, value);
    else searchParams.delete(name, value);
    setSearchParams(new URLSearchParams(searchParams));
  };

  return (
    <>
      <div className="border-t border-chrome-300 mt-[10px] py-[10px] font-bold text-chrome-500">
        {label}
      </div>
      {counter.orderedEntries().map(([facet, count]) => (
        <div key={facet} className="flex items-center space-x-2">
          <div>
            <Form.Check
              checked={searchParams.has(urlKey, facet)}
              name={urlKey}
              value={facet}
              onChange={handleToggle}
            />
          </div>
          <div className="flex-grow flex justify-between">
            <div>{facet}</div>
            <div>{`(${count})`}</div>
          </div>
        </div>
      ))}
    </>
  );
};

const SidebarFacets = () => {
  const facets = useLogFeedFacets();

  return (
    <div>
      <Facets label="Region" counter={facets.region} />
      <Facets label="Zone" counter={facets.zone} />
      <Facets label="OS" counter={facets.os} />
      <Facets label="Arch" counter={facets.arch} />
      <Facets label="Node" counter={facets.node} />
    </div>
  );
};

/**
 * Sidebar component
 */

const Sidebar = () => {
  const [searchParams] = useSearchParams();
  const [, setFilters] = useLogFeedFilters();

  // sync filters with search params
  useEffect(() => {
    const filters = new MapSet<string, string>();
    [
      'container',
      'region',
      'zone',
      'os',
      'arch',
      'node',
    ].forEach((key) => {
      if (searchParams.has(key)) filters.set(key, new Set(searchParams.getAll(key)));
    });
    setFilters(filters);
  }, [searchParams]);

  return (
    <div className="text-sm px-[7px] pt-[10px]">
      <a href={joinPaths(getBasename(), '/')}>
        <img src={joinPaths(getBasename(), logo)} alt="logo" className="display-block h-[45px] mb-[10px]" />
      </a>
      <SidebarWorkloads />
      <SidebarPodsAndContainers />
      <SidebarFacets />
    </div>
  );
};

/**
 * Header component
 */

const Header = () => {
  const controls = useLogFeedControls();
  const feed = useLogFeedMetadata();

  const buttonCN = 'rounded-lg h-[40px] w-[40px] flex items-center justify-center enabled:hover:bg-chrome-200 disabled:opacity-30';

  const handleDateRangeDropdownChange = (args: DateRangeDropdownOnChangeArgs) => {
    if (args.since) controls.seek(args.since.toISOString());
  };

  return (
    <div className="flex justify-between items-end p-1">
      <div className="flex px-2">
        <DateRangeDropdown onChange={handleDateRangeDropdownChange}>
          <button
            type="button"
            className={buttonCN}
            title="Jump to time"
            aria-label="Jump to time"
          >
            <HistoryIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
          </button>
        </DateRangeDropdown>
        <button
          type="button"
          className={buttonCN}
          title="Jump to beginning"
          aria-label="Jump to beginning"
          onClick={() => controls.head()}
        >
          <SkipBackIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
        </button>
        {feed.isFollow ? (
          <button
            type="button"
            className={buttonCN}
            title="Pause"
            aria-label="Pause"
            onClick={() => controls.pause()}
          >
            <PauseIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
          </button>
        ) : (
          <button
            type="button"
            className={buttonCN}
            title="Play"
            aria-label="Play"
            onClick={() => controls.play()}
          >
            <PlayIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
          </button>
        )}
        <button
          type="button"
          className={buttonCN}
          title="Jump to end"
          aria-label="Jump to end"
          onClick={() => controls.tail()}
        >
          <SkipForwardIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
        </button>
      </div>
      <div className="h-full flex flex-col justify-end items-end">
        <SettingsButton />
      </div>
    </div>
  );
};

/**
 * Layout component
 */

type InnerLayoutProps = {
  sidebar: JSX.Element;
  header: JSX.Element;
  content: JSX.Element;
};

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
    document.body.onselectstart = () => false;

    // cleanup
    document.addEventListener('mouseup', function cleanup() {
      document.removeEventListener('mousemove', fn);
      document.body.style.cursor = bodyCursor;
      document.body.onselectstart = onSelectStart;
      document.removeEventListener('mouseup', cleanup);
    });
  };

  return (
    <div className="relative h-full">
      <div
        className="absolute h-full bg-chrome-100 overflow-x-hidden"
        style={{ width: `${sidebarWidth}px` }}
      >
        {sidebar}
      </div>
      {/* eslint-disable-next-line jsx-a11y/no-static-element-interactions */}
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

  return (
    <AuthRequired>
      <LoggingResourcesProvider kubeContext={searchParams.get('kubeContext') || ''} sourcePaths={searchParams.getAll('source')}>
        <AppLayout>
          <ConfigureContainerColors />
          <InnerLayout
            sidebar={<Sidebar />}
            header={<Header />}
            content={<LogFeedViewer />}
          />
        </AppLayout>
      </LoggingResourcesProvider>
    </AuthRequired>
  );
}
