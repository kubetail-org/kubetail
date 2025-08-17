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

import { useSubscription } from '@apollo/client';
import type { ApolloError } from '@apollo/client';
import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  getPaginationRowModel,
  useReactTable,
} from '@tanstack/react-table';
import type { ColumnDef, SortDirection, SortingState, TableMeta, TableOptions } from '@tanstack/react-table';
import { atom, useAtomValue, useSetAtom } from 'jotai';
import { atomFamily, selectAtom } from 'jotai/utils';
import React, { ReactElement, createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { ChevronDown, ChevronUp, ExternalLink, Layers3, PanelLeftClose, PanelLeftOpen, Search } from 'lucide-react';
import numeral from 'numeral';
import TimeAgo from 'react-timeago';
import type { Formatter, Suffix, Unit } from 'react-timeago';
import { useDebounceCallback } from 'usehooks-ts';

import { Button } from '@kubetail/ui/elements/button';
import { Checkbox } from '@kubetail/ui/elements/checkbox';
import { SearchBox } from '@kubetail/ui/elements/search-box';
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@kubetail/ui/elements/select';
import { Spinner } from '@kubetail/ui/elements/spinner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@kubetail/ui/elements/table';

import appConfig from '@/app-config';
import KubetailLogo from '@/assets/logo.svg?react';
import AppLayout from '@/components/layouts/AppLayout';
import AuthRequired from '@/components/utils/AuthRequired';
import * as dashboardOps from '@/lib/graphql/dashboard/ops';
import {
  HomeCronJobsListFetchQuery,
  HomeCronJobsListItemFragmentFragment,
  HomeDaemonSetsListFetchQuery,
  HomeDaemonSetsListItemFragmentFragment,
  HomeDeploymentsListFetchQuery,
  HomeDeploymentsListItemFragmentFragment,
  HomeJobsListFetchQuery,
  HomeJobsListItemFragmentFragment,
  HomePodsListFetchQuery,
  HomePodsListItemFragmentFragment,
  HomeReplicaSetsListFetchQuery,
  HomeReplicaSetsListItemFragmentFragment,
  HomeStatefulSetsListFetchQuery,
  HomeStatefulSetsListItemFragmentFragment,
} from '@/lib/graphql/dashboard/__generated__/graphql';
import { getContainerIDs } from '@/lib/home';
import { useIsClusterAPIEnabled, useListQueryWithSubscription, useLogMetadata } from '@/lib/hooks';
import { joinPaths, getBasename, cn } from '@/lib/util';
import { Workload, allWorkloads, glyphIconMap, knockoutIconMap, labelsPMap } from '@/lib/workload';

/**
 * Shared variables and helper methods
 */

const basename = getBasename();

const defaultKubeContext = appConfig.environment === 'cluster' ? '' : null;

type ContextType = {
  kubeContext: string | null;
  setKubeContext: React.Dispatch<React.SetStateAction<string | null>>;
  namespace: string;
  setNamespace: React.Dispatch<React.SetStateAction<string>>;
  workloadFilter?: Workload;
  setWorkloadFilter: React.Dispatch<React.SetStateAction<Workload | undefined>>;
  sidebarOpen: boolean;
  setSidebarOpen: React.Dispatch<React.SetStateAction<boolean>>;
  search: string;
  setSearch: React.Dispatch<React.SetStateAction<string>>;
};

const Context = createContext({} as ContextType);

const workloadQueryConfig = {
  [Workload.CRONJOBS]: {
    query: dashboardOps.HOME_CRONJOBS_LIST_FETCH,
    subscription: dashboardOps.HOME_CRONJOBS_LIST_WATCH,
    queryDataKey: 'batchV1CronJobsList',
    subscriptionDataKey: 'batchV1CronJobsWatch',
    getItems: (data: HomeCronJobsListFetchQuery) => data?.batchV1CronJobsList?.items,
  },
  [Workload.DAEMONSETS]: {
    query: dashboardOps.HOME_DAEMONSETS_LIST_FETCH,
    subscription: dashboardOps.HOME_DAEMONSETS_LIST_WATCH,
    queryDataKey: 'appsV1DaemonSetsList',
    subscriptionDataKey: 'appsV1DaemonSetsWatch',
    getItems: (data: HomeDaemonSetsListFetchQuery) => data?.appsV1DaemonSetsList?.items,
  },
  [Workload.DEPLOYMENTS]: {
    query: dashboardOps.HOME_DEPLOYMENTS_LIST_FETCH,
    subscription: dashboardOps.HOME_DEPLOYMENTS_LIST_WATCH,
    queryDataKey: 'appsV1DeploymentsList',
    subscriptionDataKey: 'appsV1DeploymentsWatch',
    getItems: (data: HomeDeploymentsListFetchQuery) => data?.appsV1DeploymentsList?.items,
  },
  [Workload.JOBS]: {
    query: dashboardOps.HOME_JOBS_LIST_FETCH,
    subscription: dashboardOps.HOME_JOBS_LIST_WATCH,
    queryDataKey: 'batchV1JobsList',
    subscriptionDataKey: 'batchV1JobsWatch',
    getItems: (data: HomeJobsListFetchQuery) => data?.batchV1JobsList?.items,
  },
  [Workload.PODS]: {
    query: dashboardOps.HOME_PODS_LIST_FETCH,
    subscription: dashboardOps.HOME_PODS_LIST_WATCH,
    queryDataKey: 'coreV1PodsList',
    subscriptionDataKey: 'coreV1PodsWatch',
    getItems: (data: HomePodsListFetchQuery) => data?.coreV1PodsList?.items,
  },
  [Workload.REPLICASETS]: {
    query: dashboardOps.HOME_REPLICASETS_LIST_FETCH,
    subscription: dashboardOps.HOME_REPLICASETS_LIST_WATCH,
    queryDataKey: 'appsV1ReplicaSetsList',
    subscriptionDataKey: 'appsV1ReplicaSetsWatch',
    getItems: (data: HomeReplicaSetsListFetchQuery) => data?.appsV1ReplicaSetsList?.items,
  },
  [Workload.STATEFULSETS]: {
    query: dashboardOps.HOME_STATEFULSETS_LIST_FETCH,
    subscription: dashboardOps.HOME_STATEFULSETS_LIST_WATCH,
    queryDataKey: 'appsV1StatefulSetsList',
    subscriptionDataKey: 'appsV1StatefulSetsWatch',
    getItems: (data: HomeStatefulSetsListFetchQuery) => data?.appsV1StatefulSetsList?.items,
  },
};

type WorkloadItem =
  | HomeCronJobsListItemFragmentFragment
  | HomeDaemonSetsListItemFragmentFragment
  | HomeDeploymentsListItemFragmentFragment
  | HomeJobsListItemFragmentFragment
  | HomePodsListItemFragmentFragment
  | HomeReplicaSetsListItemFragmentFragment
  | HomeStatefulSetsListItemFragmentFragment;

type WorkloadQueryResponse = {
  loading: boolean;
  fetching: boolean;
  items: WorkloadItem[] | undefined;
  error: ApolloError | undefined;
};

function makeAtom() {
  return atom<WorkloadQueryResponse>({
    loading: false,
    fetching: false,
    items: undefined,
    error: undefined,
  });
}

const workloadQueryAtoms = {
  [Workload.CRONJOBS]: makeAtom(),
  [Workload.DAEMONSETS]: makeAtom(),
  [Workload.DEPLOYMENTS]: makeAtom(),
  [Workload.JOBS]: makeAtom(),
  [Workload.PODS]: makeAtom(),
  [Workload.REPLICASETS]: makeAtom(),
  [Workload.STATEFULSETS]: makeAtom(),
};

const loadingByWorkloadAtom = atomFamily((w: Workload) => selectAtom(workloadQueryAtoms[w], (v) => v.loading));
const fetchingByWorkloadAtom = atomFamily((w: Workload) => selectAtom(workloadQueryAtoms[w], (v) => v.fetching));
const numItemsByWorkloadAtom = atomFamily((w: Workload) =>
  selectAtom(workloadQueryAtoms[w], (v) => v.items?.length ?? 0),
);

const isLoadingAtom = atom((get) => allWorkloads.some((w) => get(loadingByWorkloadAtom(w))));
const isFetchingAtom = atom((get) => allWorkloads.some((w) => get(fetchingByWorkloadAtom(w))));
const numItemsAtom = atom((get) => allWorkloads.reduce((total, w) => total + get(numItemsByWorkloadAtom(w)), 0));

type FileInfo = {
  size: string;
  lastModifiedAt?: Date;
};

const logMetadataMapAtom = atom(new Map<string, FileInfo>());

const ownershipMapAtom = atom(new Map<string, string[]>());

/**
 * useLogFileInfo hook
 */

function useLogFileInfo(uids: string[]) {
  const logMetadataMap = useAtomValue(logMetadataMapAtom);
  const ownershipMap = useAtomValue(ownershipMapAtom);

  const [val, setVal] = useState<Map<string, { size: number; lastModifiedAt: Date; containerIDs: string[] }>>(
    new Map(),
  );

  useEffect(() => {
    const newVal = new Map<string, { size: number; lastModifiedAt: Date; containerIDs: string[] }>();

    uids.forEach((uid) => {
      const containerIDs = getContainerIDs(uid, ownershipMap);

      // combine fileInfo
      const fileInfo = {
        size: 0,
        lastModifiedAt: new Date(0),
        containerIDs,
      };

      containerIDs.forEach((containerID) => {
        const v = logMetadataMap.get(containerID);

        if (v?.size) {
          fileInfo.size += parseInt(v.size, 10);
        }

        if (v?.lastModifiedAt) {
          fileInfo.lastModifiedAt = new Date(Math.max(v.lastModifiedAt.getTime(), fileInfo.lastModifiedAt.getTime()));
        }
      });

      // update map
      if (fileInfo.lastModifiedAt.getTime() > 0) newVal.set(uid, fileInfo);
    });

    setVal(newVal);
  }, [uids, ownershipMap, logMetadataMap]);

  return val;
}

/**
 * WorkloadDataFetcher component
 */

const WorkloadDataFetcher = ({ workload }: { workload: Workload }) => {
  const { kubeContext, namespace, search } = useContext(Context);
  const setAtom = useSetAtom(workloadQueryAtoms[workload]);

  const cfg = workloadQueryConfig[workload];
  const { loading, fetching, data, error } = useListQueryWithSubscription({
    query: cfg.query,
    subscription: cfg.subscription,
    // @ts-expect-error
    queryDataKey: cfg.queryDataKey,
    // @ts-expect-error
    subscriptionDataKey: cfg.subscriptionDataKey,
    variables: { kubeContext },
  });

  const filterFn = useCallback(
    (item: WorkloadItem) => {
      // Remove deleted items
      if (item.metadata.deletionTimestamp) return false;

      // Apply namespace filter
      if (namespace !== '' && item.metadata.namespace !== namespace) return false;

      // Apply search filter
      if (search !== '' && !item.metadata.name.toLowerCase().includes(search.toLowerCase())) return false;

      return true;
    },
    [namespace, search],
  );

  useEffect(() => {
    const items = data ? cfg.getItems(data)?.filter(filterFn) : undefined;
    setAtom({ loading, fetching, error, items: items as any });
  }, [loading, fetching, data, error, filterFn]);

  return null;
};

/**
 * WorkloadDataProvider component
 */

const WorkloadDataProvider = () => (
  <>
    {allWorkloads.map((workload) => (
      <WorkloadDataFetcher key={workload} workload={workload} />
    ))}
  </>
);

/**
 * OwnershipMapUpdater component
 */

function orderedEqual<T>(a?: T[], b?: T[]): boolean {
  if (a === b) return true;
  if (!a || !b) return false;
  if (a.length !== b.length) return false;

  for (let i = 0; i < a.length; i += 1) {
    if (a[i] !== b[i]) return false;
  }

  return true;
}

const OwnershipMapUpdater = ({ workload }: { workload: Workload }) => {
  const setOwnershipMap = useSetAtom(ownershipMapAtom);

  // Get data from workload atoms
  const { items } = useAtomValue(workloadQueryAtoms[workload]);

  useEffect(() => {
    const m = new Map<string, string[]>();

    // Add workload ids from all workload types
    items?.forEach((item) => {
      item.metadata.ownerReferences.forEach((ref) => {
        const childUIDs = m.get(ref.uid) || [];
        childUIDs.push(item.metadata.uid);
        m.set(ref.uid, childUIDs);
      });
    });

    // Add container ids from pods
    if (workload === Workload.PODS) {
      items?.forEach((item) => {
        const pod = item as HomePodsListItemFragmentFragment;
        // strip out prefix (e.g. "containerd://")
        const containerIDs = pod.status.containerStatuses.map((status) =>
          status.containerID.replace(/^[^:]+:\/\/(.*)/, '$1'),
        );
        m.set(pod.metadata.uid, containerIDs);
      });
    }

    setOwnershipMap((prevMap) => {
      let hasChanges = false;
      const newMap = new Map(prevMap);

      m.forEach((value, key) => {
        const prevValue = newMap.get(key);
        if (!prevValue || !orderedEqual(prevValue, value)) {
          hasChanges = true;
          newMap.set(key, value);
        }
      });

      return hasChanges ? newMap : prevMap;
    });
  }, [items]);

  return null;
};

/**
 * OwnershipMapProvider component
 */

const OwnershipMapProvider = () => (
  <>
    {allWorkloads.map((workload) => (
      <OwnershipMapUpdater key={workload} workload={workload} />
    ))}
  </>
);

/**
 * LogMetadataMapProvider component
 */

const LogMetadataMapProvider = () => {
  const { kubeContext } = useContext(Context);
  const setLogMetadataMap = useSetAtom(logMetadataMapAtom);
  const isClusterAPIEnabled = useIsClusterAPIEnabled(kubeContext);

  const { data } = useLogMetadata({
    enabled: isClusterAPIEnabled && kubeContext !== null,
    kubeContext: kubeContext || '',
    onUpdate: (containerID, fileInfo) => {
      // Update state
      setLogMetadataMap((currMap) => {
        const newMap = new Map(currMap);
        newMap.set(containerID, fileInfo);
        return newMap;
      });

      // Flash data
      document.querySelectorAll(`.last_event_${containerID}`).forEach((el) => {
        const k = 'animate-flash-bg-green';
        el.classList.remove(k);
        el.classList.add(k);
        setTimeout(() => el.classList.remove(k), 1000);
      });
    },
  });

  // Initial data
  useEffect(() => {
    setLogMetadataMap((currMap) => {
      const newMap = new Map(currMap);
      data?.logMetadataList?.items.forEach((item) => {
        newMap.set(item.spec.containerID, item.fileInfo);
      });
      return newMap;
    });
  }, [data !== undefined]);

  return null;
};

/**
 * NamespacesPicker component
 */

const NamespacesPicker = () => {
  const { kubeContext, namespace, setNamespace } = useContext(Context);

  const { loading, data } = useListQueryWithSubscription({
    query: dashboardOps.HOME_NAMESPACES_LIST_FETCH,
    subscription: dashboardOps.HOME_NAMESPACES_LIST_WATCH,
    queryDataKey: 'coreV1NamespacesList',
    subscriptionDataKey: 'coreV1NamespacesWatch',
    variables: { kubeContext },
  });

  const ALL_NAMESPACES = '*';

  // Reset namespaces when kubeContext changes
  useEffect(() => {
    setNamespace('');
  }, [kubeContext]);

  return (
    <Select
      value={namespace === '' ? ALL_NAMESPACES : namespace}
      onValueChange={(v) => setNamespace(v === ALL_NAMESPACES ? '' : v)}
      disabled={loading}
    >
      <SelectTrigger className="w-[200px]">
        <SelectValue placeholder="Loading..." />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectLabel>Namespaces</SelectLabel>
          <SelectItem value={ALL_NAMESPACES}>All namespaces</SelectItem>
          {data?.coreV1NamespacesList?.items.map((item) => (
            <SelectItem key={item.id} value={item.metadata.name}>
              {item.metadata.name}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
};

/**
 * KubeContextPicker component
 */

type KubeContextPickerProps = {
  value: string | null;
  setValue: (value: string) => void;
};

const KubeContextPicker = ({ value, setValue }: KubeContextPickerProps) => {
  const { loading, data } = useSubscription(dashboardOps.KUBE_CONFIG_WATCH);
  const kubeConfig = data?.kubeConfigWatch?.object;

  // Set default value
  useEffect(() => {
    const kubeContext = kubeConfig?.currentContext;
    if (kubeContext !== undefined) setValue(kubeContext);
  }, [kubeConfig !== undefined]);

  return (
    <Select value={value || ''} onValueChange={(v) => setValue(v)} disabled={loading}>
      <SelectTrigger className="my-[24px] w-full">
        <SelectValue placeholder="Loading..." />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          <SelectLabel>Clusters</SelectLabel>
          {kubeConfig &&
            kubeConfig.contexts.map((context) => (
              <SelectItem key={context.name} value={context.name}>
                {context.name}
              </SelectItem>
            ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
};

/**
 * WorkloadCount component
 */

const WorkloadCount = ({ workload }: { workload: Workload }) => {
  const { items } = useAtomValue(workloadQueryAtoms[workload]);
  return <span>{items?.length}</span>;
};

/**
 * SidebarContent component
 */

const SidebarContent = () => {
  const { workloadFilter, setWorkloadFilter } = useContext(Context);

  return (
    <>
      <button type="button" className="cursor-pointer" onClick={() => setWorkloadFilter(undefined)}>
        <span className="text-md text-chrome-500">Workloads</span>
      </button>
      <ul className="space-y-1">
        {allWorkloads.map((workload) => {
          const Icon = glyphIconMap[workload];
          return (
            <li className="group" key={workload}>
              <button
                type="button"
                className={cn(
                  'group flex items-center justify-between py-2 px-1 rounded-sm hover:bg-accent w-full',
                  workload === workloadFilter && 'bg-blue-100',
                )}
                onClick={() => setWorkloadFilter((w) => (w === workload ? undefined : workload))}
              >
                <div className="flex items-center gap-2">
                  <Icon className="w-[20px] h-[20px] text-chrome-950" />
                  <span className="text-md">{labelsPMap[workload]}</span>
                </div>
                <div
                  className={cn(
                    'text-xs font-medium border not-dark:group-has-hover:border-chrome-300 min-w-[24px] h-[24px] px-[4px] rounded-sm flex items-center justify-center',
                    workload === workloadFilter && 'border-chrome-300',
                  )}
                >
                  <WorkloadCount workload={workload} />
                </div>
              </button>
            </li>
          );
        })}
      </ul>
    </>
  );
};

/**
 * Sidebar component
 */

const Sidebar = () => {
  const { kubeContext } = useContext(Context);

  const readyWait = useSubscription(dashboardOps.KUBERNETES_API_READY_WAIT, {
    variables: { kubeContext },
  });

  if (readyWait.loading || kubeContext === null) {
    return null;
  }

  return <SidebarContent />;
};

/**
 * AdaptiveTimeAgo
 */

const useAdaptiveMinPeriod = (date: Date) => {
  const [minPeriod, setMinPeriod] = useState(10);

  useEffect(() => {
    const updateMinPeriod = () => {
      const ageMs = Date.now() - date.getTime();

      const ageMinutes = ageMs / (1000 * 60);
      if (ageMinutes < 1) {
        setMinPeriod(1); // 1 seconds for first minute
        return 60 * 1000 - ageMs;
      }

      const ageHours = ageMinutes / 60;
      if (ageHours < 1) {
        setMinPeriod(60); // 1 minute until 1 hour
        return 60 * 60 * 1000 - ageMs;
      }

      setMinPeriod(3600); // 1 hour after that
      return Infinity;
    };

    const checkMs = updateMinPeriod();

    // Schedule next check, if necessary
    if (checkMs !== Infinity) {
      const interval = setInterval(updateMinPeriod, checkMs);
      return () => clearInterval(interval);
    }
  }, [date]);

  return minPeriod;
};

const AdaptiveTimeAgo = ({ date }: { date: Date }) => {
  const minPeriod = useAdaptiveMinPeriod(date);
  return <TimeAgo date={date} minPeriod={minPeriod} title={date.toUTCString()} />;
};

/**
 * DisplayItems component
 */

const lastModifiedAtFormatter: Formatter = (
  value: number,
  unit: Unit,
  suffix: Suffix,
  epochMilliseconds: number,
  nextFormatter: Formatter,
  now: () => number,
) => {
  if (suffix === 'from now' || unit === 'second') return 'just now';
  if (nextFormatter) return nextFormatter(value, unit, suffix, epochMilliseconds, nextFormatter, now);
  return '';
};

type WorkloadTableData = {
  id: string;
  name: string;
  namespace: string;
  createdAt: Date;
  size?: number;
  lastModifiedAt?: Date;
  sourceString: string;
  containerIDs: string[];
};

interface WorkloadTableMeta extends TableMeta<WorkloadTableData> {
  kubeContext: string | null;
  selectAll: boolean;
  isChecked: Map<string, boolean>;
  handleSelectAllChange: () => void;
  handleSingleCheckboxChange: (id: string) => void;
}

const WORKLOAD_TABLE_COLUMNS = [
  {
    id: 'checkbox',
    header: ({ table }) => {
      const meta = table.options.meta as WorkloadTableMeta;
      return (
        <div className="flex items-center">
          <Checkbox checked={meta.selectAll} onCheckedChange={meta.handleSelectAllChange} />
        </div>
      );
    },
    cell: ({ table, row }) => {
      const meta = table.options.meta as WorkloadTableMeta;
      const { id, sourceString } = row.original;
      return (
        <div className="flex items-center">
          <Checkbox
            name="source"
            value={sourceString}
            checked={meta.isChecked.get(id) || false}
            onCheckedChange={() => meta.handleSingleCheckboxChange(id)}
          />
        </div>
      );
    },
  },
  {
    accessorKey: 'name',
    header: 'Name',
    enableSorting: true,
  },
  {
    accessorKey: 'namespace',
    header: 'Namespace',
    enableSorting: true,
  },
  {
    accessorKey: 'createdAt',
    enableSorting: true,
    sortDescFirst: true,
    header: 'Created',
    cell: ({ row }) => {
      const { createdAt } = row.original;
      return <AdaptiveTimeAgo date={createdAt} />;
    },
  },
  {
    accessorKey: 'size',
    enableSorting: true,
    sortDescFirst: true,
    sortingFn: (rowA, rowB) => {
      const sizeA = rowA.original.size ?? 0;
      const sizeB = rowB.original.size ?? 0;
      return sizeA - sizeB;
    },
    header: 'Size',
    cell: ({ row }) => {
      const { size } = row.original;
      return size === undefined ? <span>--</span> : numeral(size).format('0.0 b');
    },
  },
  {
    accessorKey: 'lastModifiedAt',
    enableSorting: true,
    sortDescFirst: true,
    header: 'Last Event',
    cell: ({ row }) => {
      const { lastModifiedAt } = row.original;
      if (lastModifiedAt === undefined) return <span>--</span>;

      return (
        <TimeAgo
          date={lastModifiedAt}
          formatter={lastModifiedAtFormatter}
          minPeriod={60}
          title={lastModifiedAt.toUTCString()}
        />
      );
    },
  },
  {
    id: 'viewlink',
    cell: ({ table, row }) => {
      const { kubeContext } = table.options.meta as WorkloadTableMeta;
      const { sourceString } = row.original;
      return (
        <a
          target="_blank"
          href={`${joinPaths(basename, '/console')}?kubeContext=${encodeURIComponent(kubeContext || '')}&source=${encodeURIComponent(sourceString)}`}
          className="flex items-center underline text-primary"
        >
          <div>view</div>
          <ExternalLink className="w-[18px] h-[18px] ml-1" />
        </a>
      );
    },
  },
] satisfies ColumnDef<WorkloadTableData>[];

type SortIconProps = {
  dir: SortDirection | false;
  descFirst: boolean | undefined;
};

const SortIcon = ({ dir, descFirst }: SortIconProps) => {
  const iconCN = 'h-5 w-5 ml-2 flex-none text-chrome-400 ';

  switch (dir) {
    case 'asc':
      return <ChevronUp className={iconCN} />;
    case 'desc':
      return <ChevronDown className={iconCN} />;
    default: {
      const Icon = descFirst ? ChevronDown : ChevronUp;
      return <Icon className={cn(iconCN, 'invisible group-hover:visible group-focus:visible')} />;
    }
  }
};

const DisplayItems = ({ workload }: { workload: Workload }) => {
  const { kubeContext, workloadFilter } = useContext(Context);
  const isClusterAPIEnabled = useIsClusterAPIEnabled(kubeContext);

  const { fetching, items } = useAtomValue(workloadQueryAtoms[workload]);

  const ids = useMemo(() => items?.map((item) => item.metadata.uid) || [], [items]);
  const logFileInfo = useLogFileInfo(ids);

  const data = useMemo(
    () =>
      !items
        ? []
        : items.map((item) => {
            const fileInfo = logFileInfo.get(item.metadata.uid);
            return {
              id: item.id,
              name: item.metadata.name,
              namespace: item.metadata.namespace,
              createdAt: item.metadata.creationTimestamp,
              size: fileInfo?.size,
              lastModifiedAt: fileInfo?.lastModifiedAt,
              sourceString: `${item.metadata.namespace}:${workload}/${item.metadata.name}/*`,
              containerIDs: fileInfo?.containerIDs || [],
            };
          }),
    [items, logFileInfo],
  );

  const numItems = data.length;
  const maxDisplayRows = workloadFilter === workload ? numItems : 5;
  const [showAll, setShowAll] = useState(false);

  const [sorting, setSorting] = useState<SortingState>([{ id: 'name', desc: false }]);

  const [selectAll, setSelectAll] = useState(false);
  const [isChecked, setIsChecked] = useState<Map<string, boolean>>(new Map());

  const handleSelectAllChange = useCallback(() => {
    const newValue = !selectAll;
    setSelectAll(newValue);

    // update individual checkboxes
    const newIsChecked = new Map(isChecked);
    data.forEach((item) => newIsChecked.set(item.id, newValue));
    setIsChecked(newIsChecked);
  }, [data, selectAll, setSelectAll, isChecked, setIsChecked]);

  const handleSingleCheckboxChange = useCallback(
    (id: string) => {
      // update individual
      const newValue = !isChecked.get(id);
      const newIsChecked = new Map(isChecked);
      newIsChecked.set(id, newValue);
      setIsChecked(newIsChecked);

      // update selectAll based on all current items
      const allItemsChecked = data.every((item) => newIsChecked.get(item.id) || false);
      const someItemsUnchecked = data.some((item) => !newIsChecked.get(item.id));

      if (allItemsChecked) setSelectAll(true);
      if (someItemsUnchecked) setSelectAll(false);
    },
    [data, isChecked, setSelectAll, setIsChecked],
  );

  const meta = useMemo(
    () =>
      ({
        kubeContext,
        selectAll,
        isChecked,
        handleSelectAllChange,
        handleSingleCheckboxChange,
      }) satisfies WorkloadTableMeta,
    [kubeContext, selectAll, isChecked, handleSelectAllChange, handleSingleCheckboxChange],
  );

  const tableCfg = useMemo(
    () =>
      ({
        data,
        columns: WORKLOAD_TABLE_COLUMNS,
        meta,
        state: {
          sorting,
          columnVisibility: {
            size: isClusterAPIEnabled,
            lastEventAt: isClusterAPIEnabled,
          },
          pagination: {
            pageIndex: 0,
            pageSize: showAll ? numItems : maxDisplayRows,
          },
        },
        onSortingChange: setSorting,
        getCoreRowModel: getCoreRowModel(),
        getSortedRowModel: getSortedRowModel(),
        getPaginationRowModel: getPaginationRowModel(),
        manualPagination: false,
      }) satisfies TableOptions<WorkloadTableData>,
    [data, meta, sorting, isClusterAPIEnabled, showAll, numItems, maxDisplayRows, setSorting],
  );

  const table = useReactTable(tableCfg as TableOptions<WorkloadTableData>);

  // For label
  const Icon = knockoutIconMap[workload];
  const label = labelsPMap[workload];

  return (
    <>
      <TableHeader>
        <TableRow>
          <TableHead colSpan={5} className="pb-[5px] text-[0.9rem]">
            <div className="flex items-center space-x-1">
              <Icon className="w-[22px] h-[22px] text-primary" />
              <div className="font-medium">{label}</div>
              {fetching ? (
                <div>
                  <Spinner size="xs" />
                </div>
              ) : (
                <div className="px-[10px] py-[2px] bg-chrome-100 font-semibold rounded-full text-xs text-chrome-foreground">
                  {items && `${items?.length}`}
                </div>
              )}
            </div>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableHeader className="rounded-thead">
        {table.getHeaderGroups().map((headerGroup) => (
          <TableRow key={headerGroup.id}>
            {headerGroup.headers.map((header) => {
              const canSort = header.column.getCanSort();
              return (
                <TableHead key={header.id} onClick={canSort ? header.column.getToggleSortingHandler() : undefined}>
                  <div className={cn('flex group', canSort && 'cursor-pointer')}>
                    {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                    {canSort && (
                      <SortIcon dir={header.column.getIsSorted()} descFirst={header.column.columnDef.sortDescFirst} />
                    )}
                  </div>
                </TableHead>
              );
            })}
          </TableRow>
        ))}
      </TableHeader>
      <TableBody className="rounded-tbody">
        {numItems === 0 ? (
          <TableRow>
            <TableCell colSpan={table.getVisibleLeafColumns().length}>
              <div className="flex flex-col items-center  py-1 ">
                <Layers3 className="h-5 w-5 text-chrome-400" />
                <span className="text-chrome-400 italic font-medium">No resources found</span>
              </div>
            </TableCell>
          </TableRow>
        ) : (
          <>
            {table.getRowModel().rows.map((row) => (
              <TableRow key={row.id}>
                {row.getVisibleCells().map((cell) => {
                  let cls = '';
                  if (cell.column.id === 'lastModifiedAt') {
                    cls = row.original.containerIDs.map((id) => `last_event_${id}`).join(' ');
                  }
                  return (
                    <TableCell key={cell.id} className={cls}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  );
                })}
              </TableRow>
            ))}
          </>
        )}
      </TableBody>
      <TableBody>
        <TableRow>
          <TableCell colSpan={table.getVisibleLeafColumns().length} className="pb-[30px]">
            {numItems > maxDisplayRows && (
              <button
                type="button"
                className="text-chrome-600 hover:text-chrome-700 text-sm font-medium cursor-pointer"
                onClick={() => setShowAll(!showAll)}
              >
                {showAll ? 'Show less...' : `Show ${numItems - maxDisplayRows} more...`}
              </button>
            )}
          </TableCell>
        </TableRow>
      </TableBody>
    </>
  );
};

/**
 * DisplayWorkloads component
 */

const DisplayWorkloads = () => {
  const { search, workloadFilter } = useContext(Context);
  const isLoading = useAtomValue(isLoadingAtom);
  const isFetching = useAtomValue(isFetchingAtom);
  const numItems = useAtomValue(numItemsAtom);

  if (isLoading) return <div>Loading...</div>;
  if (isFetching) return <div>Fetching workloads...</div>;

  // If loading & fetching is finished and there are no search results, display "No Results" UI
  if (search.trim() !== '' && numItems === 0) {
    return (
      <div className="flex items-center border border-dashed border-secondary rounded-md justify-center h-32">
        <div className="text-center">
          <Search className="h-8 w-8 text-chrome-400 mx-auto mb-2" />
          <p className="text-base text-chrome-400">No matching workloads found</p>
        </div>
      </div>
    );
  }

  return (
    <div className="rounded-table-wrapper">
      <Table>
        {allWorkloads.map((workload) => {
          if (!workloadFilter || workloadFilter === workload) {
            return <DisplayItems key={workload} workload={workload} />;
          }
          return null;
        })}
      </Table>
    </div>
  );
};

/**
 * Content component
 */

const Content = () => {
  const { kubeContext, setSearch } = useContext(Context);
  const [inputValue, setInputValue] = useState('');

  const readyWait = useSubscription(dashboardOps.KUBERNETES_API_READY_WAIT, {
    variables: { kubeContext },
  });

  const debouncedSearch = useDebounceCallback((value: string) => setSearch(value), 100);

  const isLoading = useAtomValue(isLoadingAtom);
  const isFetching = useAtomValue(isFetchingAtom);

  return (
    <div className="inline-block min-w-full px-[20px] py-[10px]">
      {readyWait.loading || kubeContext === null ? (
        <div>Connecting...</div>
      ) : (
        <form method="get" target="_blank" action={joinPaths(basename, '/console')}>
          <input type="hidden" name="kubeContext" value={kubeContext} />
          <div className="flex gap-4 pt-[14px] pb-[24px] justify-between flex-row">
            <div className="text-heading-2xl">Dashboard</div>
            <div className="flex gap-2">
              <SearchBox
                className="w-64"
                value={inputValue}
                placeholder="Search workloads..."
                onChange={(e) => {
                  setInputValue(e.target.value);
                  debouncedSearch(e.target.value);
                }}
                onKeyDown={(e) => e.key === 'Enter' && e.preventDefault()}
                disabled={isLoading || isFetching}
              />
              <div className="block w-[200px]">
                <NamespacesPicker />
              </div>
              <Button type="submit">
                View in Console
                <ExternalLink className="w-[18px] h-[18px] ml-1" />
              </Button>
            </div>
          </div>
          <DisplayWorkloads />
        </form>
      )}
    </div>
  );
};

/**
 * InnerLayout component
 */

type InnerLayoutProps = {
  sidebar: ReactElement;
  content: ReactElement;
};

const InnerLayout = ({ sidebar, content }: InnerLayoutProps) => {
  const { sidebarOpen, setSidebarOpen, kubeContext, setKubeContext } = useContext(Context);

  const sidebarWidth = sidebarOpen ? 210 : 30;

  return (
    <div className="h-full flex flex-col">
      <div className="flex-1 h-0">
        <div className="flex h-full">
          <aside
            className={cn(
              'shrink-0 bg-chrome-100 border-r-1 transition-all duration-100 ease-in relative overflow-y-auto',
              sidebarOpen ? 'px-[12px]' : 'px-[2px]',
            )}
            style={{ width: `${sidebarWidth}px` }}
          >
            <header className="my-[24px] h-[36px] flex flex-row justify-between items-center gap-2">
              {sidebarOpen ? (
                <>
                  <KubetailLogo className="h-full w-auto" />
                  <PanelLeftClose
                    className="h-[20px] cursor-pointer text-chrome-500 hover:text-primary "
                    onClick={() => setSidebarOpen(false)}
                  />
                </>
              ) : (
                <PanelLeftOpen
                  className="h-[20px] cursor-pointer text-chrome-500 hover:text-primary "
                  onClick={() => setSidebarOpen(true)}
                />
              )}
            </header>
            {sidebarOpen && (
              <>
                <div className="my-[12px]">
                  {appConfig.environment === 'desktop' && (
                    <KubeContextPicker value={kubeContext} setValue={setKubeContext} />
                  )}
                </div>
                {sidebar}
              </>
            )}
          </aside>
          <main className="flex-1 overflow-auto">{content}</main>
        </div>
      </div>
    </div>
  );
};

/**
 * Default component
 */

export default function Page() {
  const [kubeContext, setKubeContext] = useState(defaultKubeContext);
  const [namespace, setNamespace] = useState('');
  const [workloadFilter, setWorkloadFilter] = useState<Workload>();
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [search, setSearch] = useState('');

  const context = useMemo(
    () => ({
      kubeContext,
      setKubeContext,
      namespace,
      setNamespace,
      workloadFilter,
      setWorkloadFilter,
      sidebarOpen,
      setSidebarOpen,
      search,
      setSearch,
    }),
    [
      kubeContext,
      setKubeContext,
      namespace,
      setNamespace,
      workloadFilter,
      setWorkloadFilter,
      sidebarOpen,
      setSidebarOpen,
      search,
      setSearch,
    ],
  );

  return (
    <AuthRequired>
      <Context.Provider value={context}>
        <WorkloadDataProvider />
        <OwnershipMapProvider />
        <LogMetadataMapProvider />
        <AppLayout>
          <InnerLayout sidebar={<Sidebar />} content={<Content />} />
        </AppLayout>
      </Context.Provider>
    </AuthRequired>
  );
}
