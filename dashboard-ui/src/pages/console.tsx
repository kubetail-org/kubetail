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
import {
  History as HistoryIcon,
  Pause as PauseIcon,
  Play as PlayIcon,
  Settings as SettingsIcon,
  SkipBack as SkipBackIcon,
  SkipForward as SkipForwardIcon,
  PanelLeftClose as PanelLeftCloseIcon,
  PanelLeftOpen as PanelLeftOpenIcon,
} from 'lucide-react';
import { createContext, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import Form from '@kubetail/ui/elements/Form';
import { Popover, PopoverTrigger, PopoverContent } from '@kubetail/ui/elements/Popover';

import logo from '@/assets/logo.svg';
import AppLayout from '@/components/layouts/AppLayout';
import AuthRequired from '@/components/utils/AuthRequired';
import SourcePickerModal from '@/components/widgets/SourcePickerModal';
import { DateRangeDropdown, DateRangeDropdownOnChangeArgs } from '@/components/widgets/DateRangeDropdown';
import {
  Provider as LogFeedProvider,
  Viewer as LogFeedViewer,
  ViewerHandle as LogFeedViewerHandle,
  ViewerColumn,
  allViewerColumns,
  useSources,
  useViewerFacets,
  useViewerFilters,
  useViewerIsWrap,
  useViewerMetadata,
  useViewerVisibleCols,
} from '@/lib/logfeed';
import { Counter, cn, getBasename, joinPaths, MapSet } from '@/lib/util';
import { LogSourceFragmentFragment } from '@/lib/graphql/dashboard/__generated__/graphql';
import { Workload, allWorkloads, iconMap, labelsPMap } from '@/lib/workload';
import { useContainerColor } from '@/lib/color';

/**
 * Shared
 */

type ContextType = {
  isSidebarOpen: boolean;
  setIsSidebarOpen: React.Dispatch<React.SetStateAction<boolean>>;
};

const Context = createContext({} as ContextType);

/**
 * Settings button
 */

const SettingsButton = () => {
  const [visibleCols, setVisibleCols] = useViewerVisibleCols();
  const [isWrap, setIsWrap] = useViewerIsWrap();

  const handleOnChange = (col: ViewerColumn, ev: React.ChangeEvent<HTMLInputElement>) => {
    const newSet = new Set(visibleCols);
    if (ev.target.checked) newSet.add(col);
    else newSet.delete(col);
    setVisibleCols(newSet);
  };

  const checkboxEls: JSX.Element[] = [];

  allViewerColumns.forEach((col) => {
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
 * Header
 */

const Header = ({ viewerRef }: { viewerRef: React.RefObject<LogFeedViewerHandle> }) => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { isSidebarOpen, setIsSidebarOpen } = useContext(Context);
  const feed = useViewerMetadata();

  const buttonCN = 'rounded-lg h-[40px] w-[40px] flex items-center justify-center enabled:hover:bg-chrome-200 disabled:opacity-30';

  const handleDateRangeDropdownChange = (args: DateRangeDropdownOnChangeArgs) => {
    if (args.since) {
      // Update location
      const since = args.since.toISOString();
      searchParams.set('mode', 'head');
      searchParams.set('since', since);
      setSearchParams(new URLSearchParams(searchParams), { replace: true });

      // Execute command
      viewerRef.current?.seekTime(since);
    }
  };

  const handleJumpToBeginningPress = () => {
    // Update location
    searchParams.set('mode', 'head');
    searchParams.delete('since');
    setSearchParams(new URLSearchParams(searchParams), { replace: true });

    // Execute command
    viewerRef.current?.seekHead();
  };

  const handleJumpToEndPress = () => {
    // Update location
    searchParams.set('mode', 'tail');
    searchParams.delete('since');
    setSearchParams(new URLSearchParams(searchParams), { replace: true });

    // Execute command
    viewerRef.current?.seekTail();
  };

  const handleSubmit = (ev: React.FormEvent<HTMLFormElement>) => {
    ev.preventDefault();

    const formData = new FormData(ev.currentTarget);
    const grep = (formData.get('grep') as string || '').trim();

    // Update location
    if (grep === '') searchParams.delete('grep');
    else searchParams.set('grep', grep);
    setSearchParams(new URLSearchParams(searchParams), { replace: true });
  };

  return (
    <div className="flex justify-between items-end p-1">
      <div className="flex items-center">
        {!isSidebarOpen && (
          <button
            type="button"
            onClick={() => setIsSidebarOpen(true)}
            title="Collapse sidebar"
            className="pr-2"
          >
            <PanelLeftOpenIcon size={20} strokeWidth={2} className="text-chrome-500" />
          </button>
        )}
        <div className={cn('flex', isSidebarOpen ? 'px-4' : 'px-2')}>
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
            onClick={handleJumpToBeginningPress}
          >
            <SkipBackIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
          </button>
          {feed.isFollow ? (
            <button
              type="button"
              className={buttonCN}
              title="Pause"
              aria-label="Pause"
              onClick={() => viewerRef.current?.pause()}
            >
              <PauseIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
            </button>
          ) : (
            <button
              type="button"
              className={buttonCN}
              title="Play"
              aria-label="Play"
              onClick={() => viewerRef.current?.play()}
            >
              <PlayIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
            </button>
          )}
          <button
            type="button"
            className={buttonCN}
            title="Jump to end"
            aria-label="Jump to end"
            onClick={handleJumpToEndPress}
          >
            <SkipForwardIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
          </button>
        </div>
        <div>
          {feed.isSearchEnabled && (
            <Form onSubmit={handleSubmit}>
              <Form.Control
                name="grep"
                className="w-[400px]"
                placeholder="Match string or /regex/..."
                defaultValue={searchParams.get('grep') || ''}
              />
            </Form>
          )}
        </div>
      </div>
      <div className="h-full flex flex-col justify-end items-end">
        <SettingsButton />
      </div>
    </div>
  );
};

/**
 * Sidebar workloads component
 */

const workloadTypestrMap: Record<string, Workload> = {
  daemonsets: Workload.DAEMONSETS,
  deployments: Workload.DEPLOYMENTS,
  replicasets: Workload.REPLICASETS,
  statefulsets: Workload.STATEFULSETS,
  cronjobs: Workload.CRONJOBS,
  jobs: Workload.JOBS,
  pods: Workload.PODS,
};

function parseSourceArg(input: string) {
  const regex = /^([^:]+):([^/]+)\/(.+)$/;
  const match = input.match(regex);

  if (!match) {
    throw new Error(`Invalid input format. Expected format is "<namespace>:<workload-type>/<workload-name>", got "${input}"`);
  }

  // Destructure the match array. The first element is the full match, so we skip it.
  const [, namespace, workloadTypeStr, workloadName] = match;

  const workloadType = workloadTypestrMap[workloadTypeStr];

  if (!workloadType) {
    throw new Error(`Invalid workload type: ${workloadTypeStr}`);
  }

  return { namespace, workloadType, workloadName };
}

const SidebarWorkloads = () => {
  const [isPickerOpen, setIsPickerOpen] = useState(false);
  const [searchParams] = useSearchParams();

  const kubeContext = searchParams.get('kubeContext') || '';

  // Build workload map
  const workloadMap = new MapSet<Workload, { namespace: string, name: string }>();
  searchParams.getAll('source').forEach((source) => {
    const { namespace, workloadType, workloadName } = parseSourceArg(source);
    workloadMap.add(workloadType, { namespace, name: workloadName });
  });

  const deleteSource = (sourcePath: string) => {
    searchParams.delete('source', sourcePath);

    // TODO: instead of navigating to new url can we use react-router?
    const currentUrl = new URL(window.location.href);
    currentUrl.search = (new URLSearchParams(searchParams)).toString();
    window.location.href = currentUrl.toString();
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
          const objs = workloadMap.get(workload);
          if (!objs) return;

          const vals = Array.from(objs.values());
          vals.sort((a, b) => a.name.localeCompare(b.name));

          const Icon = iconMap[workload];
          return (
            <div key={workload}>
              <div className="flex items-center space-x-1">
                <div><Icon className="h-[18px] w-[18px]" /></div>
                <div className="font-semibold text-chrome-500">{labelsPMap[workload]}</div>
              </div>
              <ul className="pl-[23px]">
                {vals.map((val) => (
                  <li key={val.name} className="flex items-center justify-between">
                    <span className="whitespace-nowrap overflow-hidden text-ellipsis">{val.name.replace(/\/\*$/, '')}</span>
                    <button
                      type="button"
                      onClick={() => deleteSource(`${val.namespace}:${workload}/${val.name}`)}
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

type ContainersProps = {
  namespace: string;
  podName: string;
  containerNames?: string[];
};

const ContainerItem = ({
  namespace,
  podName,
  containerName,
}: {
  namespace: string;
  podName: string;
  containerName: string;
}) => {
  const [searchParams, setSearchParams] = useSearchParams();
  const color = useContainerColor(namespace, podName, containerName);
  const urlKey = 'container';
  const urlVal = `${namespace}:${podName}/${containerName}`;

  const handleToggle = (ev: React.ChangeEvent<HTMLInputElement>) => {
    const { name, value, checked } = ev.currentTarget;
    if (checked) searchParams.append(name, value);
    else searchParams.delete(name, value);
    setSearchParams(new URLSearchParams(searchParams));
  };

  return (
    <div className="flex item-center justify-between">
      <div className="flex items-center space-x-1">
        <div
          className="w-[13px] h-[13px] rounded-full"
          style={{ backgroundColor: color }}
        />
        <div>{containerName}</div>
      </div>
      <Form.Check
        checked={searchParams.has(urlKey, urlVal)}
        name={urlKey}
        value={urlVal}
        onChange={handleToggle}
      />
    </div>
  );
};

const Containers = ({
  namespace,
  podName,
  containerNames = [],
}: ContainersProps) => {
  containerNames.sort();

  return (
    <>
      {containerNames.map((containerName) => (
        <ContainerItem
          key={containerName}
          namespace={namespace}
          podName={podName}
          containerName={containerName}
        />
      ))}
    </>
  );
};

class ContainerGroup {
  namespace: string;

  podName: string;

  containers: string[];

  constructor(namespace: string, podName: string, containers: string[] = []) {
    this.namespace = namespace;
    this.podName = podName;
    this.containers = containers;
  }

  addContainer(containerName: string): void {
    if (!this.containers.includes(containerName)) {
      this.containers.push(containerName);
    }
  }
}

const SidebarPodsAndContainers = () => {
  const { sources } = useSources();
  const [searchParams] = useSearchParams();

  // Create synthetic sources from search params
  searchParams.getAll('container').forEach((key) => {
    const match = key.match(/^([^:]+):([^/]+)\/(.+)$/);
    if (!match) return; // Skip if pattern doesn't match

    const synthetic = {
      namespace: match[1],
      podName: match[2],
      containerName: match[3],
    } as LogSourceFragmentFragment;

    if (!sources.some((s) => s.namespace === synthetic.namespace && s.podName === synthetic.podName && s.containerName === synthetic.containerName)) {
      sources.push(synthetic);
    }
  });
  sources.sort((a, b) => a.podName.localeCompare(b.podName));

  const generateMapKey = (namespace: string, podName: string) => `${namespace}/${podName}`;

  // Group containers by pod
  const groupMap = new Map<string, ContainerGroup>();
  sources.forEach((source) => {
    const k = generateMapKey(source.namespace, source.podName);
    if (!groupMap.has(k)) groupMap.set(k, new ContainerGroup(source.namespace, source.podName));
    groupMap.get(k)?.addContainer(source.containerName);
  });

  const containerGroups = Array.from(groupMap.values()).sort((a, b) => {
    const keyA = `${a.namespace}/${a.podName}`;
    const keyB = `${b.namespace}/${b.podName}`;
    return keyA.localeCompare(keyB);
  });

  return (
    <>
      <div className="border-t border-chrome-divider mt-[10px]" />
      <div className="py-[10px] font-bold text-chrome-500">Pods/Containers</div>
      <div className="space-y-3">
        {containerGroups.map((group) => (
          <div key={`${group.namespace}/${group.podName}`}>
            <div className="flex items-center justify-between">
              <div className="whitespace-nowrap overflow-hidden text-ellipsis">{group.podName}</div>
            </div>
            <Containers
              namespace={group.namespace}
              podName={group.podName}
              containerNames={group.containers}
            />
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

  const entries = counter.orderedEntries();

  // If there are no entries, or only one empty entry, return null
  if (entries.length === 0 || (entries.length === 1 && entries[0][0] === '')) {
    return null;
  }

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
      {entries.map(([facet, count]) => (
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
  const facets = useViewerFacets();

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
 * Sidebar
 */

const Sidebar = () => {
  const [searchParams] = useSearchParams();
  const [, setFilters] = useViewerFilters();

  // sync filters with search params
  useEffect(() => {
    const filters = new MapSet<string, string>();
    [
      'region',
      'zone',
      'os',
      'arch',
      'node',
      'container',
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
 * Inner Layout component
 */

type InnerLayoutProps = {
  sidebar: JSX.Element;
  header: JSX.Element;
  content: JSX.Element;
};

const InnerLayout = ({ sidebar, header, content }: InnerLayoutProps) => {
  const { isSidebarOpen, setIsSidebarOpen } = useContext(Context);
  const [sidebarWidth, setSidebarWidth] = useState(300);

  const handleDrag = () => {
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
  };

  return (
    <div className="relative h-full">
      {isSidebarOpen && (
        <>
          <div
            className="absolute h-full bg-chrome-100 overflow-x-hidden"
            style={{ width: `${sidebarWidth}px` }}
          >
            {sidebar}
            <button
              type="button"
              onClick={() => setIsSidebarOpen(false)}
              title="Collapse sidebar"
              className="absolute right-0 top-[30px] transform -translate-y-1/2"
            >
              <PanelLeftCloseIcon size={20} strokeWidth={2} className="text-chrome-500" />
            </button>
          </div>
          {/* eslint-disable-next-line jsx-a11y/no-static-element-interactions */}
          <div
            className="absolute bg-chrome-divider w-[4px] h-full border-l-2 border-chrome-100 cursor-ew-resize"
            style={{ left: `${sidebarWidth}px` }}
            onMouseDown={handleDrag}
          />
        </>
      )}
      <main
        className="h-full flex flex-col overflow-hidden"
        style={{ marginLeft: `${isSidebarOpen ? sidebarWidth + 4 : 0}px` }}
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
  const viewerRef = useRef<LogFeedViewerHandle>(null);
  const [isSidebarOpen, setIsSidebarOpen] = useState(true);

  const sourceFilter = {
    region: searchParams.getAll('region'),
    zone: searchParams.getAll('zone'),
    os: searchParams.getAll('os'),
    arch: searchParams.getAll('arch'),
    node: searchParams.getAll('node'),
    container: searchParams.getAll('container'),
  };

  const context = useMemo(() => ({
    isSidebarOpen,
    setIsSidebarOpen,
  }), [isSidebarOpen, setIsSidebarOpen]);

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
      <Context.Provider value={context}>
        <LogFeedProvider
          kubeContext={searchParams.get('kubeContext')}
          sources={searchParams.getAll('source')}
          sourceFilter={sourceFilter}
          grep={processedGrep}
        >
          <AppLayout>
            <InnerLayout
              sidebar={<Sidebar />}
              header={<Header viewerRef={viewerRef} />}
              content={(
                <LogFeedViewer
                  ref={viewerRef}
                  defaultMode={searchParams.get('mode')}
                  defaultSince={searchParams.get('since')}
                />
              )}
            />
          </AppLayout>
        </LogFeedProvider>
      </Context.Provider>
    </AuthRequired>
  );
}
