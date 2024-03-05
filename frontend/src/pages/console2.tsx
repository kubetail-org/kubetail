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

import { PlusCircleIcon, TrashIcon } from '@heroicons/react/24/solid';
import { addMinutes, addHours, addDays, addWeeks, addMonths } from 'date-fns';
import { format, utcToZonedTime } from 'date-fns-tz';
import type { OptionsWithTZ } from 'date-fns-tz';
import distinctColors from 'distinct-colors';
import {
  Pause as PauseIcon,
  Play as PlayIcon,
  Settings as SettingsIcon,
  SkipForward as SkipForwardIcon,
} from 'lucide-react';
import {
  createContext,
  useContext,
  useReducer,
  useRef,
  useState,
} from 'react';
import { useSearchParams } from 'react-router-dom';

import Form from 'kubetail-ui/elements/Form';
import { Popover, PopoverTrigger, PopoverContent } from 'kubetail-ui/elements/Popover';
import Spinner from 'kubetail-ui/elements/Spinner';

import logo from '@/assets/logo.svg';
import AppLayout from '@/components/layouts/AppLayout';
import AuthRequired from '@/components/utils/AuthRequired';
import { DateRangeDropdown, DateRangeDropdownOnChangeArgs, Duration, DurationUnit } from '@/components/widgets/DateRangeDropdown';
import SourcePickerModal from '@/components/widgets/SourcePickerModal';
import { cssID } from '@/lib/console/helpers';
import { LoggingResourcesProvider, useNodes, usePods, useWorkloads } from '@/lib/console/logging-resources2';
import type { Pod } from '@/lib/console/logging-resources2';
import {
  LogFeedColumn,
  LogFeedProvider,
  LogFeedState,
  LogFeedViewer,
  allLogFeedColumns,
  useLogFeedControls,
  useLogFeedMetadata,
  useLogFeedVisibleCols,
} from '@/lib/console/logfeed';
import { Counter, MapSet, getBasename, joinPaths } from '@/lib/helpers';
import { allWorkloads, iconMap, labelsPMap } from '@/lib/workload';

type State = {
  since: Date | Duration | null;
  until: Date | null;
  isMsgWrap: boolean;
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

  pods.forEach(pod => {
    pod.spec.containers.forEach(container => {
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

  return <></>;
};

/**
 * Settings button
 */

const SettingsButton = () => {
  const { state, dispatch } = useContext(Context);
  const { isMsgWrap } = state;
  const [visibleCols, setVisibleCols] = useLogFeedVisibleCols();

  const handleOnChange = (col: LogFeedColumn, ev: React.ChangeEvent<HTMLInputElement>) => {
    const newSet = new Set(visibleCols);
    if (ev.target.checked) newSet.add(col);
    else newSet.delete(col);
    setVisibleCols(newSet);
  };

  const checkboxEls: JSX.Element[] = [];

  allLogFeedColumns.forEach(col => {
    checkboxEls.push(
      <Form.Check
        key={col}
        label={col}
        checked={visibleCols.has(col) ? true : false}
        onChange={(ev) => handleOnChange(col, ev)}
      />
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
          checked={isMsgWrap}
          onChange={() => dispatch({ isMsgWrap: !isMsgWrap })}
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

  const deleteSource = (sourcePath: string) => {
    searchParams.delete('source', sourcePath);
    setSearchParams(new URLSearchParams(searchParams), { replace: true });
  };

  return (
    <>
      {isPickerOpen && <SourcePickerModal onClose={() => setIsPickerOpen(false)} />}
      <div className="flex items-center justify-between mb-1">
        <div className="flex items-center space-x-1">
          <span className="font-bold text-chrome-500">Sources</span>
          {loading && <Spinner className="h-[15px] w-[15px]" />}
        </div>
        <a onClick={() => setIsPickerOpen(true)} className="cursor-pointer">
          <PlusCircleIcon className="h-[24px] w-[24px] text-primary" />
        </a>
      </div>
      <div className="space-y-2">
        {allWorkloads.map(workload => {
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
                {objs.map(obj => (
                  <li key={obj.id} className="flex items-center justify-between">
                    <span className="whitespace-nowrap overflow-hidden text-ellipsis">{obj.metadata.name}</span>
                    <a onClick={() => deleteSource(`${workload}/${obj.metadata.namespace}/${obj.metadata.name}`)}>
                      <TrashIcon className="h-[18px] w-[18px] text-chrome-300 hover:text-chrome-500 cursor-pointer" />
                    </a>
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

const SidebarPodsAndContainers = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { pods } = usePods();

  const Containers = ({ pod }: { pod: Pod }) => {
    const containers = Array.from(pod.spec.containers);
    containers.sort((a, b) => a.name.localeCompare(b.name));

    const handleToggle = (ev: React.ChangeEvent<HTMLInputElement>) => {
      const { name, value, checked } = ev.currentTarget;
      if (checked) searchParams.append(name, value);
      else searchParams.delete(name, value);
      setSearchParams(new URLSearchParams(searchParams));
    }

    return (
      <>
        {containers.map(container => {
          const k = cssID(pod, container.name);
          const urlKey = "container";
          const urlVal = `${pod.metadata.namespace}/${pod.metadata.name}/${container.name}`;
          return (
            <div key={container.name} className="flex item-center justify-between">
              <div className="flex items-center space-x-1">
                <div className="w-[13px] h-[13px]" style={{ backgroundColor: `var(--${k}-color)` }}></div>
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

  return (
    <>
      <div className="border-t border-chrome-divider mt-[10px]"></div>
      <div className="py-[10px] font-bold text-chrome-500">Pods/Containers</div>
      <div className="space-y-3">
        {pods.map(pod => (
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

const SidebarFacets = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { pods } = usePods();
  const { nodes } = useNodes();

  // count pods per node
  const nodeVals: string[] = [];
  pods?.forEach(pod => nodeVals.push(pod.spec.nodeName));
  const nodeCounts = new Counter(nodeVals);

  // count pods per node facets
  const regionCounts = new Counter();
  const zoneCounts = new Counter();
  const archCounts = new Counter();
  const osCounts = new Counter();

  // track nodes per facet
  const nodeMapSet = new MapSet();

  nodes?.forEach(node => {
    const count = nodeCounts.get(node.metadata.name) || 0;
    if (!count) return;

    const labels = node.metadata.labels;
    const nodeName = node.metadata.name;

    const region = labels['topology.kubernetes.io/region'];
    if (region) {
      regionCounts.update(region, count);
      nodeMapSet.add(`region:${region}`, nodeName);
    }

    const zone = labels['topology.kubernetes.io/zone'];
    if (zone) {
      zoneCounts.update(zone, count);
      nodeMapSet.add(`zone:${zone}`, nodeName);
    }

    const os = labels['kubernetes.io/os'];
    if (os) {
      osCounts.update(os, count);
      nodeMapSet.add(`os:${os}`, nodeName)
    }

    const arch = labels['kubernetes.io/arch'];
    if (arch) {
      archCounts.update(arch, count)
      nodeMapSet.add(`arch:${arch}`, nodeName)
    }
  });

  const handleToggle = (ev: React.ChangeEvent<HTMLInputElement>) => {
    const { name, value, checked } = ev.currentTarget;
    if (checked) searchParams.append(name, value);
    else searchParams.delete(name, value);
    setSearchParams(new URLSearchParams(searchParams));
  };

  const Facets = ({ label, counter }: { label: string, counter: Counter }) => {
    const urlKey = label.toLocaleLowerCase();

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
              <div>({count})</div>
            </div>
          </div>
        ))}
      </>
    );
  };

  return (
    <div>
      <Facets label="Region" counter={regionCounts} />
      <Facets label="Zone" counter={zoneCounts} />
      <Facets label="OS" counter={osCounts} />
      <Facets label="Arch" counter={archCounts} />
      <Facets label="Node" counter={nodeCounts} />
    </div>
  );
};

/**
 * Sidebar component
 */

const Sidebar = () => {
  return (
    <div className="text-sm px-[7px] pt-[10px]">
      <a href={joinPaths(getBasename(), '/')}>
        <img src={joinPaths(getBasename(), logo)} alt="logo" className="display-block h-[31.4167px] mb-[10px]" />
      </a>
      <SidebarWorkloads />
      <SidebarPodsAndContainers />
      <SidebarFacets />
    </div>
  );
};

/**
 * Feed title component
 */

type FeedTitleProps = {
  since: Date | Duration | null;
  until: Date | null;
}

const FeedTitle = ({ since, until }: FeedTitleProps) => {
  const [initTime] = useState(utcToZonedTime(new Date(), 'UTC'));
  const feed = useLogFeedMetadata();
  const dateFmt = 'LLL dd, y HH:mm:ss';
  const dateOpts: OptionsWithTZ = { timeZone: 'UTC' };

  const now = utcToZonedTime(new Date(), 'UTC');

  let sinceMsg = '';
  let untilMsg = '';

  if (since instanceof Date) {
    since = utcToZonedTime(since, 'UTC');
    sinceMsg = format(since, dateFmt, dateOpts) + ' UTC';
  } else if (since instanceof Duration) {
    let ts = now;
    if (since.unit === DurationUnit.Minutes) ts = addMinutes(now, -1 * since.value);
    else if (since.unit === DurationUnit.Hours) ts = addHours(now, -1 * since.value);
    else if (since.unit === DurationUnit.Days) ts = addDays(now, -1 * since.value);
    else if (since.unit === DurationUnit.Weeks) ts = addWeeks(now, -1 * since.value);
    else if (since.unit === DurationUnit.Months) ts = addMonths(now, -1 * since.value);
    sinceMsg = format(ts, dateFmt) + ' UTC';
  } else {
    sinceMsg = format(initTime, dateFmt) + ' UTC';
  }

  if (feed.state === LogFeedState.Streaming) {
    untilMsg = 'Streaming'
  } else if (feed.state === LogFeedState.Paused) {
    untilMsg = `${format(now, dateFmt)} UTC`;
  } else if (until) {
    until = utcToZonedTime(until, 'UTC');
    untilMsg = format(until, dateFmt) + ' UTC';
  }

  return (
    <div className="flex text-xs text-primary font-medium">
      <div className="w-[150px] text-right">{sinceMsg}</div>
      <div className="px-2">-</div>
      <div className="w-[150px] text-left">{untilMsg}</div>
    </div>
  );
};

/**
 * Header component
 */

const Header = () => {
  const controls = useLogFeedControls();
  const feed = useLogFeedMetadata();

  const { state, dispatch } = useContext(Context);

  const buttonCN = 'rounded-lg h-[40px] w-[40px] flex items-center justify-center enabled:hover:bg-chrome-200 disabled:opacity-30';

  const handleDateRangeDropdownChange = (args: DateRangeDropdownOnChangeArgs) => {
    dispatch({ since: args.since, until: args.until });
  };

  return (
    <div className="flex justify-between items-end p-1">
      <div className="flex px-2">
        {feed.state === LogFeedState.Streaming ? (
          <button
            className={buttonCN}
            title="Pause"
            onClick={() => controls.stopStreaming()}
          >
            <PauseIcon size={24} strokeWidth={1.5} className="text-chrome-foreground" />
          </button>
        ) : (
          <button
            className={buttonCN}
            title="Play"
            onClick={() => controls.startStreaming()}
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
      </div>
      <div className="flex justify-center">
        <DateRangeDropdown onChange={handleDateRangeDropdownChange}>
          <button className="h-[40px] cursor-pointer bg-chrome-200 hover:bg-chrome-300 py-1 px-2 rounded">
            <FeedTitle since={state.since} until={state.until} />
          </button>
        </DateRangeDropdown>
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
    since: null,
    until: null,
    isMsgWrap: false,
  });

  return (
    <AuthRequired>
      <Context.Provider value={{ state, dispatch }}>
        <LoggingResourcesProvider sourcePaths={searchParams.getAll('source')}>
          <LogFeedProvider
            defaultSince={state.since === null ? '-100' : state.since.toISOString()}
            defaultUntil={state.until === null ? 'forever' : state.until.toISOString()}
          >
            <AppLayout>
              <ConfigureContainerColors />
              <InnerLayout
                sidebar={<Sidebar />}
                header={<Header />}
                content={<LogFeedViewer />}
              />
            </AppLayout>
          </LogFeedProvider>
        </LoggingResourcesProvider>
      </Context.Provider>
    </AuthRequired>
  );
}
