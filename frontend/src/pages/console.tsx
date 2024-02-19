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
import { addMinutes, addHours, addDays, addWeeks, addMonths, parse, isValid } from 'date-fns';
import { format, utcToZonedTime } from 'date-fns-tz';
import type { OptionsWithTZ } from 'date-fns-tz';
import distinctColors from 'distinct-colors';
import {
  Pause as PauseIcon,
  Play as PlayIcon,
  Settings as SettingsIcon,
  SkipForward as SkipForwardIcon,
} from 'lucide-react';
import { useSearchParams } from 'react-router-dom';
import { createContext, forwardRef, useImperativeHandle, useRef, useState, Fragment } from 'react';
import { DateRange } from 'react-day-picker';

import Button from 'kubetail-ui/elements/Button';
import { Calendar } from 'kubetail-ui/elements/Calendar';
import Form from 'kubetail-ui/elements/Form';
import { Popover, PopoverClose, PopoverTrigger, PopoverContent } from 'kubetail-ui/elements/Popover';
import Spinner from 'kubetail-ui/elements/Spinner';
import { Tabs, TabsContent, TabsList, TabsTrigger } from 'kubetail-ui/elements/Tabs';

import logo from '@/assets/logo.svg';
import AuthRequired from '@/components/utils/AuthRequired';
import ServerStatus from '@/components/widgets/ServerStatus';
import SourcePickerModal from '@/components/widgets/SourcePickerModal.tsx';
import {
  LoggingResourcesProvider,
  LogFeedState,
  useLogFeed,
  useNodes,
  usePods,
  useWorkloads,
} from '@/lib/console/logging-resources';
import type { LogFeedQueryOptions, LogRecord, LRPod } from '@/lib/console/logging-resources';
import { Counter, MapSet, cssEncode, intersectSets, getBasename, joinPaths } from '@/lib/helpers';
import { cn } from '@/lib/utils';
import { allWorkloads, iconMap, labelsPMap } from '@/lib/workload';

enum DurationUnit {
  Minutes = 'minutes',
  Hours = 'hours',
  Days = 'days',
  Weeks = 'weeks',
  Months = 'moths',
}

class Duration {
  value: number;
  unit: DurationUnit;

  constructor(value: number, unit: DurationUnit) {
    this.value = value;
    this.unit = unit;
  }

  toISOString() {
    switch (this.unit) {
      case DurationUnit.Minutes:
        return `PT${this.value}M`;
      case DurationUnit.Hours:
        return `PT${this.value}H`;
      case DurationUnit.Days:
        return `P${this.value}D`;
      case DurationUnit.Weeks:
        return `P${this.value}W`;
      case DurationUnit.Months:
        return `P${this.value}M`;
    }
  }
}

/**
 * Context object
 */

type Context = {
  timezone: string;
  setTimezone: React.Dispatch<string>;
};

const Context = createContext<Context>({} as Context);

/**
 * Color helpers
 */

const palette = distinctColors({
  count: 20,
  chromaMin: 40,
  chromaMax: 100,
  lightMin: 20,
  lightMax: 80,
});

function cssID(pod: LRPod, container: string) {
  return cssEncode(`${pod.metadata.namespace}/${pod.metadata.name}/${container}`);
}

function useContainerColorVars() {
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
}

/**
 * Allowed containers after filters
 */

function useAllowedContainers(): Set<string> | undefined {
  const [searchParams] = useSearchParams();
  const { loading, pods } = usePods();
  const nodes = useNodes();

  // exit early if still loading resources
  if (loading || nodes.loading) return undefined;

  // gather filters
  const filters = new Map<string, string[]>();
  ['container', 'node', 'region', 'zone', 'os', 'arch'].forEach(k => {
    const v = searchParams.getAll(k);
    if (v.length) filters.set(k, v);
  });

  // exit early if no filters specified
  if (!filters.size) return undefined;

  // map nodes to containers
  const nodesToContainersIDX = new MapSet();
  pods?.forEach(pod => {
    pod.spec.containers.forEach(container => {
      nodesToContainersIDX.add(pod.spec.nodeName, `${pod.metadata.namespace}/${pod.metadata.name}/${container.name}`);
    });
  });

  // map facets to nodes
  const facetsToNodesIDX = new MapSet();
  nodes.items?.forEach(node => {
    const { name, labels } = node.metadata;

    // skip if no pods on node
    if (!nodesToContainersIDX.has(name)) return;

    facetsToNodesIDX.add(`node:${name}`, name);

    const region = labels['topology.kubernetes.io/region'];
    if (region) facetsToNodesIDX.add(`region:${region}`, name);

    const zone = labels['topology.kubernetes.io/zone'];
    if (zone) facetsToNodesIDX.add(`zone:${zone}`, name);

    const os = labels['kubernetes.io/os'];
    if (os) facetsToNodesIDX.add(`os:${os}`, name);

    const arch = labels['kubernetes.io/arch'];
    if (arch) facetsToNodesIDX.add(`arch:${arch}`, name);
  });

  // get allowed containers from each filter
  const allowedContainerSets = new Array<Set<string>>();

  if (filters.has('container')) allowedContainerSets.push(new Set(filters.get('container')));

  ['node', 'region', 'zone', 'os', 'arch'].forEach(key => {
    const containers = new Array<string>();
    filters.get(key)?.forEach(val => {
      facetsToNodesIDX.get(`${key}:${val}`)?.forEach(node => {
        Array.prototype.push.apply(containers, Array.from(nodesToContainersIDX.get(node) || []))
      });
    });
    if (containers.length) allowedContainerSets.push(new Set(containers));
  });

  return intersectSets(allowedContainerSets);
}

/**
 * Sidebar stylesheet
 */

const SidebarStylesheet = () => {
  const allowedContainers = useAllowedContainers();

  return (
    <style>
      {`.logline { display: ${allowedContainers === undefined ? 'table-row' : 'none'}; }`}

      {Array.from(allowedContainers || []).map(container => (
        <Fragment key={container}>
          {`.container_${cssEncode(container)} { display: table-row !important; }`}
        </Fragment>
      ))}
    </style>
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
          <span className="font-bold text-gray-500">Sources</span>
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
                <div className="font-semibold text-gray-500">{labelsPMap[workload]}</div>
              </div>
              <ul className="pl-[23px]">
                {objs.map(obj => (
                  <li key={obj.id} className="flex items-center justify-between">
                    <span className="whitespace-nowrap overflow-hidden text-ellipsis">{obj.metadata.name}</span>
                    <a onClick={() => deleteSource(`${workload}/${obj.metadata.namespace}/${obj.metadata.name}`)}>
                      <TrashIcon className="h-[18px] w-[18px] text-gray-300 hover:text-gray-400 cursor-pointer" />
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
 * Sidebar streams component
 */

const SidebarPodsAndContainers = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { pods } = usePods();

  const Containers = ({ pod }: { pod: LRPod }) => {
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
      <div className="border-t border-gray-300 mt-[10px]"></div>
      <div className="py-[10px] font-bold text-gray-500">Pods/Containers</div>
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
  const nodes = useNodes();

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

  nodes.items?.forEach(node => {
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
        <div className="border-t border-gray-300 mt-[10px] py-[10px] font-bold text-gray-500">
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
  useContainerColorVars();

  return (
    <div className="text-sm px-[7px] pt-[10px]">
      <SidebarStylesheet />
      <a href="/">
        <img src={joinPaths(getBasename(), logo)} alt="logo" className="display-block h-[31.4167px] mb-[10px]" />
      </a>
      <SidebarWorkloads />
      <SidebarPodsAndContainers />
      <SidebarFacets />
    </div>
  );
};

/**
 * Settings button
 */

type SettingsButtonProps = {
  className?: string;
};

const SettingsButton = (props: SettingsButtonProps) => {
  const [checkedCols, setCheckedCols] = useState(new Map<string, boolean>([
    ['region', false],
    ['zone', false],
    ['os', false],
    ['arch', false],
    ['node', false],
  ]));

  const handleOnChange = (key: string, ev: React.ChangeEvent<HTMLInputElement>) => {
    checkedCols.set(key, ev.target.checked);
    setCheckedCols(new Map(checkedCols));
  };

  const checkboxEls: JSX.Element[] = [];

  [
    'Timestamp',
    'Pod/Container',
    'Region',
    'Zone',
    'OS',
    'Arch',
    'Node',
    'Message',
  ].forEach(label => {
    const k = label.replace(/[^a-z]/gi, '').toLowerCase();
    checkboxEls.push(
      <Form.Check
        key={k}
        label={label}
        checked={checkedCols.has(k) ? checkedCols.get(k) : true}
        onChange={(ev) => handleOnChange(k, ev)}
      />
    );
  });

  const StyleEl = () => (
    <style>
      {Array.from(checkedCols.entries()).map(([key, isChecked]) => {
        if (isChecked) {
          return <Fragment key={key} />;
        } else {
          return (
            <Fragment key={key}>
              {`.col_${key} { display: none; }`}
            </Fragment>
          );
        }
      })}
    </style>
  );

  return (
    <>
      <StyleEl />
      <Popover>
        <PopoverTrigger>
          <SettingsIcon className={cn(props.className)} size={18} strokeWidth={1.5} />
        </PopoverTrigger>
        <PopoverContent
          className="bg-white w-auto mr-1"
          onOpenAutoFocus={(ev) => ev.preventDefault()}
          sideOffset={-1}
        >
          {checkboxEls}
        </PopoverContent>
      </Popover>
    </>
  );
}

/**
 * Relative time picker component
 */

type RelativeTimePickerHandle = {
  reset: () => void;
  getValue: () => Duration | undefined;
};

const RelativeTimePicker = forwardRef<RelativeTimePickerHandle, {}>((_, ref) => {
  const [durationValue, setDurationValue] = useState('5');
  const [durationUnit, setDurationUnit] = useState(DurationUnit.Minutes);
  const [errorMsg, setErrorMsg] = useState('');

  const validate = () => {
    if (durationValue.trim() === '') {
      setErrorMsg('Please choose a number');
      return undefined;
    }
    return new Duration(Number(durationValue), durationUnit);
  }

  // define handler api
  useImperativeHandle(ref, () => ({
    reset: () => {
      setDurationValue('5');
      setDurationUnit(DurationUnit.Minutes);
    },
    getValue: validate,
  }));

  const DurationButton = ({ value, unit }: { value: number; unit: DurationUnit }) => (
    <Button
      intent="outline"
      size="xs"
      onClick={() => {
        setDurationValue(value.toString());
        setDurationUnit(unit);
      }}
    >
      {value}
    </Button>
  );

  return (
    <>
      <div className="grid grid-cols-6 gap-2 text-sm pt-3 pl-3 pr-3">
        <div className="flex items-center">Minutes</div>
        {[5, 10, 15, 30, 45].map(val => (<DurationButton key={val} value={val} unit={DurationUnit.Minutes} />))}
        <div className="flex items-center">Hours</div>
        {[1, 2, 3, 6, 12].map(val => (<DurationButton key={val} value={val} unit={DurationUnit.Hours} />))}
        <div className="flex items-center">Days</div>
        {[1, 2, 3, 4, 5].map(val => (<DurationButton key={val} value={val} unit={DurationUnit.Days} />))}
        <div className="flex items-center">Weeks</div>
        {[1, 2, 3, 4, 5].map(val => (<DurationButton key={val} value={val} unit={DurationUnit.Weeks} />))}
      </div>
      <div className="grid grid-cols-2 w-full gap-5 mt-5">
        <div>
          <Form.Label>Duration</Form.Label>
          <Form.Control
            type="number"
            min="1"
            value={durationValue}
            onChange={ev => setDurationValue(ev.target.value)}
          />
          {errorMsg && <Form.Control.Feedback>{errorMsg}</Form.Control.Feedback>}
        </div>
        <div>
          <Form.Label>Unit of time</Form.Label>
          <Form.Select
            className="mt-0"
            value={durationUnit}
            onChange={ev => setDurationUnit(ev.target.value as DurationUnit)}
          >
            <Form.Option value={DurationUnit.Minutes}>Minutes</Form.Option>
            <Form.Option value={DurationUnit.Hours}>Hours</Form.Option>
            <Form.Option value={DurationUnit.Days}>Days</Form.Option>
            <Form.Option value={DurationUnit.Weeks}>Weeks</Form.Option>
            <Form.Option value={DurationUnit.Months}>Months</Form.Option>
          </Form.Select>
        </div>
      </div>
    </>
  );
});

/**
 * Absolute time picker component
 */

type AbsoluteTimePickerHandle = {
  reset: () => void;
  getValue: () => DateRange | undefined;
};

const AbsoluteTimePicker = forwardRef<AbsoluteTimePickerHandle, {}>((_, ref) => {
  const today = new Date;
  const dateFmt = Intl.DateTimeFormat().resolvedOptions().locale === 'en-US' ? 'MM/dd/yyyy' : 'dd/MM/yyyy';

  const [calendarDateRange, setCalendarDateRange] = useState<DateRange | undefined>({ from: today, to: today });

  const [manualStartDate, setManualStartDate] = useState(format(today, dateFmt));
  const [manualStartTime, setManualStartTime] = useState('00:00:00');

  const [manualEndDate, setManualEndDate] = useState(format(today, dateFmt));
  const [manualEndTime, setManualEndTime] = useState('23:59:59');

  const [errorMsgs, setErrorMsgs] = useState(new Map<string, string>());

  const validate = () => {
    if (!isValid(parse(manualStartDate, dateFmt, new Date()))) errorMsgs.set('startDate', dateFmt)
    else errorMsgs.delete('startDate');

    if (!isValid(parse(manualStartTime, 'HH:mm:ss', new Date()))) errorMsgs.set('startTime', 'HH:mm:ss')
    else errorMsgs.delete('startTime');

    if (!isValid(parse(manualEndDate, dateFmt, new Date()))) errorMsgs.set('endDate', dateFmt)
    else errorMsgs.delete('endDate');

    if (!isValid(parse(manualEndTime, 'HH:mm:ss', new Date()))) errorMsgs.set('endTime', 'HH:mm:ss')
    else errorMsgs.delete('endTime');

    setErrorMsgs(new Map(errorMsgs));

    // return undefined if validation failed
    if (errorMsgs.size) return undefined;

    // return parsed DateRange
    return {
      from: parse(`${manualStartDate} ${manualStartTime}`, `${dateFmt} HH:mm:ss`, new Date()),
      to: parse(`${manualEndDate} ${manualEndTime}`, `${dateFmt} HH:mm:ss`, new Date()),
    };
  }

  // define handler api
  useImperativeHandle(ref, () => ({
    reset: () => {
      setCalendarDateRange({ from: today, to: today });
      setManualStartDate(format(today, dateFmt));
      setManualStartTime('00:00:00');
      setManualEndDate(format(today, dateFmt));
      setManualEndTime('23:59:59');
      setErrorMsgs(new Map<string, string>());
    },
    getValue: validate
  }));

  const handleCalendarSelect = (value: DateRange | undefined) => {
    if (!value) return;
    setCalendarDateRange(value);
    if (value.from) {
      setManualStartDate(format(value.from, dateFmt))
      setManualStartTime('00:00:00');
    }
    if (value.to) {
      setManualEndDate(format(value.to, dateFmt))
      setManualEndTime('23:59:59');
    }
    setErrorMsgs(new Map<string, string>());
  }

  return (
    <>
      <Calendar
        initialFocus
        mode="range"
        disabled={{ after: today }}
        defaultMonth={today && addMonths(today, -1)}
        selected={calendarDateRange}
        onSelect={handleCalendarSelect}
        numberOfMonths={2}
      />
      <div className="mt-1 flex px-3 justify-between">
        <div className="flex space-x-4">
          <div>
            <Form.Label>Start date</Form.Label>
            <Form.Control
              className="w-[100px]"
              value={manualStartDate}
              onChange={ev => setManualStartDate(ev.target.value)}
            />
            {errorMsgs.has('startDate') && <Form.Control.Feedback>{errorMsgs.get('startDate')}</Form.Control.Feedback>}
          </div>
          <div>
            <Form.Label>Start time</Form.Label>
            <Form.Control
              className="w-[100px]"
              value={manualStartTime}
              onChange={ev => setManualStartTime(ev.target.value)}
            />
            {errorMsgs.has('startTime') && <Form.Control.Feedback>{errorMsgs.get('startTime')}</Form.Control.Feedback>}
          </div>
        </div>
        <div className="flex space-x-4">
          <div>
            <Form.Label>End date</Form.Label>
            <Form.Control
              className="w-[100px]"
              value={manualEndDate}
              onChange={ev => setManualEndDate(ev.target.value)}
            />
            {errorMsgs.has('endDate') && <Form.Control.Feedback>{errorMsgs.get('endDate')}</Form.Control.Feedback>}
          </div>
          <div>
            <Form.Label>End time</Form.Label>
            <Form.Control
              className="w-[100px]"
              value={manualEndTime}
              onChange={ev => setManualEndTime(ev.target.value)}
            />
            {errorMsgs.has('endTime') && <Form.Control.Feedback>{errorMsgs.get('endTime')}</Form.Control.Feedback>}
          </div>
        </div>
      </div>
    </>
  );
});

/**
 * Date range dropdown component
 */

type DateRangeDropdownOnChangeArgs = {
  since?: Date | Duration;
  until?: Date;
}

interface DateRangeDropdownProps extends React.PropsWithChildren {
  onChange: (args: DateRangeDropdownOnChangeArgs) => void;
}

const DateRangeDropdown = ({ children, onChange }: DateRangeDropdownProps) => {
  const [tabValue, setTabValue] = useState('relative');

  const cancelButtonRef = useRef<HTMLButtonElement>();
  const relativePickerRef = useRef<RelativeTimePickerHandle>(null);
  const absolutePickerRef = useRef<AbsoluteTimePickerHandle>(null);

  const closePopover = () => {
    cancelButtonRef.current?.click();
  };

  const handleClear = () => {
    if (tabValue === 'relative') relativePickerRef.current?.reset();
    else if (tabValue === 'absolute') absolutePickerRef.current?.reset();
  };

  const handleApply = () => {
    const args: DateRangeDropdownOnChangeArgs = {};

    if (tabValue === 'relative') {
      const val = relativePickerRef.current?.getValue();
      if (!val) return;
      args.since = val;
    } else {
      const val = absolutePickerRef.current?.getValue();
      if (!val) return;
      if (val.from) args.since = val.from;
      if (val.to) args.until = val.to;
    }

    // close popover and call onChange handler
    closePopover();
    onChange(args);
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        {children}
      </PopoverTrigger>
      <PopoverContent
        className="w-auto p-0 bg-white"
        align="center"
      >
        <Tabs
          className="w-[565px] p-3"
          defaultValue={tabValue}
          onValueChange={(value) => setTabValue(value)}
        >
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="relative">Relative</TabsTrigger>
            <TabsTrigger value="absolute">Absolute</TabsTrigger>
          </TabsList>
          <TabsContent value="relative">
            <RelativeTimePicker ref={relativePickerRef} />
          </TabsContent>
          <TabsContent value="absolute">
            <AbsoluteTimePicker ref={absolutePickerRef} />
          </TabsContent>
        </Tabs>
        <div className="flex justify-between mt-4 p-3 border-t">
          <Button intent="outline" size="sm" onClick={handleClear}>Clear</Button>
          <div className="flex space-x-2">
            <PopoverClose asChild>
              <Button ref={cancelButtonRef} intent="ghost" size="sm">Cancel</Button>
            </PopoverClose>
            <Button intent="primary" size="sm" onClick={handleApply}>Apply</Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
};

/**
 * Feed title
 */

type FeedTitleProps = {
  since: Date | Duration;
  until: Date | null;
}

const FeedTitle = ({ since, until }: FeedTitleProps) => {
  const feed = useLogFeed();
  const dateFmt = 'LLL dd, y HH:mm:ss';
  const dateOpts: OptionsWithTZ = { timeZone: 'UTC' };

  const now = utcToZonedTime(new Date(), 'UTC');

  let sinceMsg = '';
  let untilMsg = '';

  if (since instanceof Date) {
    since = utcToZonedTime(since, 'UTC');
    sinceMsg = format(since, dateFmt, dateOpts) + ' UTC';
  } else if (since instanceof Duration) {
    let ts = utcToZonedTime(new Date(now), 'UTC');
    if (since.unit === DurationUnit.Minutes) ts = addMinutes(now, -1 * since.value);
    else if (since.unit === DurationUnit.Hours) ts = addHours(now, -1 * since.value);
    else if (since.unit === DurationUnit.Days) ts = addDays(now, -1 * since.value);
    else if (since.unit === DurationUnit.Weeks) ts = addWeeks(now, -1 * since.value);
    else if (since.unit === DurationUnit.Months) ts = addMonths(now, -1 * since.value);
    sinceMsg = format(ts, dateFmt) + ' UTC';
  }
  
  if (feed.state === LogFeedState.Playing) {
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

type HeaderProps = {
  contentElRef: React.RefObject<HTMLTableSectionElement>;
}

const Header = (props: HeaderProps) => {
  const [since, setSince] = useState<Date | Duration>(new Date());
  const [until, setUntil] = useState<Date | null>(null);

  const feed = useLogFeed();

  const clearConsole = () => {
    const el = props.contentElRef.current;
    while (el?.firstChild) el.removeChild(el.firstChild);
  };

  const handleDateRangeDropdownChange = (args: DateRangeDropdownOnChangeArgs) => {
    clearConsole();

    const opts: LogFeedQueryOptions = {};
    opts.since = (args.since) ? args.since.toISOString() : undefined;
    opts.until = (args.until) ? args.until.toISOString() : undefined;
    feed.query(opts);

    const now = new Date();
    setSince(args.since || now);
    setUntil(args.until || now);
  };

  const handlePlayPress = () => {
    if (feed.state === LogFeedState.InQuery) {
      clearConsole();
      setSince(new Date());
    }
    feed.play();
    setUntil(null);
  };

  const handlePausePress = () => {
    feed.pause();
    setUntil(new Date());
  }

  const handleSkipForwardPress = () => {
    feed.skipForward();
    setUntil(new Date());
  }

  const buttonCN = 'rounded-lg h-[40px] w-[40px] flex items-center justify-center enabled:hover:bg-gray-200 disabled:opacity-30';

  return (
    <div className="grid grid-cols-3 p-1">
      <div className="flex px-2 justify-left">
        {/**/}
        {feed.state === LogFeedState.Playing ? (
          <button
            className={buttonCN}
            title="Pause"
            onClick={handlePausePress}
          >
            <PauseIcon size={24} strokeWidth={1.5} />
          </button>
        ) : (
          <button
            className={buttonCN}
            title="Play"
            onClick={handlePlayPress}
          >
            <PlayIcon size={24} strokeWidth={1.5} />
          </button>
        )}
        <button
          className={cn(buttonCN)}
          title="Update feed"
          onClick={handleSkipForwardPress}
          disabled={feed.state !== LogFeedState.Paused}
        >
          <SkipForwardIcon size={26} strokeWidth={1.5} />
        </button>
      </div>
      <div className="flex justify-center items-center">
        <DateRangeDropdown
          onChange={handleDateRangeDropdownChange}
        >
          <button className="cursor-pointer bg-gray-200 hover:bg-gray-300 py-1 px-2 rounded">
            <FeedTitle since={since} until={until} />
          </button>
        </DateRangeDropdown>
      </div>
      <div className="h-full flex flex-col justify-end items-end">
        {/*
        <Form.Select
          className="text-xs h-[20px] py-0 mt-0 w-auto"
          value={timezone}
          onChange={(ev) => setTimezone(ev.target.value)}
        >
          <Form.Option value="utc">UTC</Form.Option>
          <Form.Option value="local">Local</Form.Option>
        </Form.Select>
        */}
        <SettingsButton />
      </div>
    </div>
  );
};

/**
 * Loading message component
 */

const LoadingMessage = () => {
  /*
  const nodes = useNodes();
  const workloads = useWorkloads();
  const pods = usePods();

  if (nodes.fetching || workloads.loading || pods.loading) {
    return (
      <div className="relative z-10" role="dialog">
        <div className="fixed inset-0 bg-gray-500 bg-opacity-75"></div>
        <div className="fixed inset-0 z-10 w-screen">
          <div className="flex min-h-full items-center justify-center p-0 text-center">
            <div className="relative transform overflow-hidden rounded-lg bg-white my-8 p-6 text-left shadow-xl">
              <div className="flex items-center space-x-2">
                <div>Loading Resources</div>
                <Spinner size="sm" />
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  }*/
  return <></>;
};

/**
 * Default component
 */

const Console = () => {
  const [searchParams] = useSearchParams();
  const contentWrapperElRef = useRef<HTMLDivElement | null>(null);
  const contentElRef = useRef<HTMLTableSectionElement | null>(null);
  const [sidebarWidth, setSidebarWidth] = useState(300);
  const isAutoScrollRef = useRef(true);
  const isProgrammaticScrollRef = useRef(false);

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

  const handleOnRecord = (record: LogRecord) => {
    const k = cssID(record.pod, record.container);

    const tdCN = 'align-top w-1 whitespace-nowrap';

    const rowEl = document.createElement('tr');
    rowEl.className = `logline container_${k}`;

    const tsEl = document.createElement('td');
    tsEl.className = cn(tdCN, 'bg-gray-200 col_timestamp');

    const tsWithTZ = utcToZonedTime(record.timestamp, 'UTC');
    tsEl.innerHTML = format(tsWithTZ, 'LLL dd, y HH:mm:ss.SSS', { timeZone: 'UTC' });
    rowEl.appendChild(tsEl);

    [
      ['col_podcontainer', `${record.pod.metadata.name}/${record.container}`],
      ['col_region', record.node.metadata.labels['topology.kubernetes.io/region']],
      ['col_zone', record.node.metadata.labels['topology.kubernetes.io/zone']],
      ['col_os', record.node.metadata.labels['kubernetes.io/os']],
      ['col_arch', record.node.metadata.labels['kubernetes.io/arch']],
      ['col_node', record.pod.spec.nodeName],
    ].forEach(([colname, val]) => {
      const tdEl = document.createElement('td');
      tdEl.className = cn(tdCN, colname);
      tdEl.innerHTML = val || '-';
      rowEl.appendChild(tdEl);
    });

    const msgEl = document.createElement('td');
    msgEl.className = 'w-auto font-medium whitespace-nowrap col_message';
    msgEl.style['color'] = `var(--${k}-color)`;
    msgEl.innerHTML = record.message;
    rowEl.appendChild(msgEl);

    contentElRef.current?.appendChild(rowEl);

    // scroll to bottom
    const contentWrapperEl = contentWrapperElRef.current;
    if (isAutoScrollRef.current && contentWrapperEl) {
      isProgrammaticScrollRef.current = true;
      contentWrapperEl.scrollTo(0, contentWrapperEl.scrollHeight);
      const timeout = setTimeout(() => {
        isProgrammaticScrollRef.current = false;
        clearTimeout(timeout);
      }, 0);
    }
  };

  // handle auto-scroll
  const handleContentScroll = () => {
    const el = contentWrapperElRef.current;
    if (el && !isProgrammaticScrollRef.current) {
      const tolerance = 10;
      const { scrollTop, clientHeight, scrollHeight } = el;
      if (Math.abs((scrollTop + clientHeight) - scrollHeight) <= tolerance) {
        isAutoScrollRef.current = true;
      } else {
        isAutoScrollRef.current = false;
      }
    }
  };

  const tdCN = 'sticky top-0 bg-gray-200 pl-2 outline outline-[1px] outline-offset-0 outline-gray-300';

  return (
    <AuthRequired>
      <LoggingResourcesProvider
        sourcePaths={searchParams.getAll('source')}
        onRecord={handleOnRecord}
      >
        <div className="relative h-full border">
          <LoadingMessage />
          <div
            className="absolute bg-gray-100 h-full overflow-x-hidden"
            style={{ width: `${sidebarWidth}px` }}
          >
            <Sidebar />
          </div>
          <div
            className="absolute bg-gray-300 w-[4px] h-full border-l-2 border-gray-100 cursor-ew-resize"
            style={{ left: `${sidebarWidth}px` }}
            onMouseDown={handleDrag}
          />
          <main className="h-full overflow-auto" style={{ marginLeft: `${sidebarWidth + 4}px` }}>
            <div className="flex flex-col h-full">
              <div className="bg-gray-100 border-b border-gray-300">
                <Header contentElRef={contentElRef} />
              </div>
              <div
                ref={contentWrapperElRef}
                className="flex-grow overflow-auto"
                onScroll={handleContentScroll}
              >
                <table className="w-full">
                  <thead className="text-xs uppercase">
                    <tr>
                      <td className={cn(tdCN, 'col_timestamp')}>Timestamp</td>
                      <td className={cn(tdCN, 'col_podcontainer')}>Pod/Container</td>
                      <td className={cn(tdCN, 'col_region')}>Region</td>
                      <td className={cn(tdCN, 'col_zone')}>Zone</td>
                      <td className={cn(tdCN, 'col_os')}>OS</td>
                      <td className={cn(tdCN, 'col_arch')}>Arch</td>
                      <td className={cn(tdCN, 'col_node')}>Node</td>
                      <td className={cn(tdCN, 'col_message')}>Message</td>
                    </tr>
                  </thead>
                  <tbody
                    ref={contentElRef}
                    id="log-records"
                    className="text-xs font-mono [&>tr:nth-child(even)]:bg-gray-100 [&_td]:px-2 [&_td]:py-1 text-gray-600"
                  />
                </table>
              </div>
            </div>
          </main>
        </div>
      </LoggingResourcesProvider>
    </AuthRequired>
  );
};

/**
 * Default component
 */

export default function Page() {
  const [timezone, setTimezone] = useState('utc');

  return (
    <Context.Provider value={{ timezone, setTimezone }}>
      <div className="h-[calc(100vh-23px)] overflow-auto">
        <Console />
      </div>
      <div className="h-[22px] bg-gray-100 border-t border-gray-300 text-sm text-right">
        <ServerStatus />
      </div>
    </Context.Provider>
  );
}
