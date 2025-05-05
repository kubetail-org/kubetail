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
import { ArrowTopRightOnSquareIcon } from '@heroicons/react/24/outline';
import numeral from 'numeral';
import React, {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react';
import TimeAgo from 'react-timeago';
import type { Formatter, Suffix, Unit } from 'react-timeago';
import { RecoilRoot, atom, useRecoilValue, useSetRecoilState } from 'recoil';

import { Boxes, Layers3, PanelLeftClose, PanelLeftOpen, Search } from 'lucide-react';
import { useDebounceCallback } from 'usehooks-ts';

import FormControl from '@kubetail/ui/elements/FormControl';
import Button from '@kubetail/ui/elements/Button';
import DataTable from '@kubetail/ui/elements/DataTable';
import type { SortBy } from '@kubetail/ui/elements/DataTable/Header';
import Form from '@kubetail/ui/elements/Form';
import Spinner from '@kubetail/ui/elements/Spinner';

import appConfig from '@/app-config';
import logo from '@/assets/logo.svg';
import AppLayout from '@/components/layouts/AppLayout';
import AuthRequired from '@/components/utils/AuthRequired';
import SettingsDropdown from '@/components/widgets/SettingsDropdown';
import * as dashboardOps from '@/lib/graphql/dashboard/ops';
import { useListQueryWithSubscription, useLogMetadata, useWorkloadCounter } from '@/lib/hooks';
import { joinPaths, getBasename, cn } from '@/lib/util';
import { Workload, allWorkloads, iconMap, labelsPMap } from '@/lib/workload';
import type {
  HomeCronJobsListItemFragmentFragment,
  HomeDaemonSetsListItemFragmentFragment,
  HomePodsListItemFragmentFragment,
  HomeJobsListItemFragmentFragment,
  HomeDeploymentsListItemFragmentFragment,
  HomeReplicaSetsListItemFragmentFragment,
  HomeStatefulSetsListItemFragmentFragment,
} from '@/lib/graphql/dashboard/__generated__/graphql';

/**
 * Shared variables and helper methods
 */

const basename = getBasename();

const defaultKubeContext = appConfig.environment === 'cluster' ? '' : undefined;

const logMetadataMapState = atom({
  key: 'homeLogMetadataMap',
  default: new Map<string, FileInfo>(),
});

type ContextType = {
  kubeContext?: string;
  setKubeContext: React.Dispatch<React.SetStateAction<string | undefined>>;
  namespace: string;
  setNamespace: React.Dispatch<React.SetStateAction<string>>;
  workloadFilter?: Workload;
  setWorkloadFilter: React.Dispatch<React.SetStateAction<Workload | undefined>>;
  sidebarOpen: boolean;
  setSidebarOpen: React.Dispatch<React.SetStateAction<boolean>>;
  search: string;
  setSearch: React.Dispatch<React.SetStateAction<string>>;
};

export type WorkloadItem = HomeCronJobsListItemFragmentFragment | HomeJobsListItemFragmentFragment | HomeDeploymentsListItemFragmentFragment | HomePodsListItemFragmentFragment | HomeDaemonSetsListItemFragmentFragment | HomeReplicaSetsListItemFragmentFragment | HomeStatefulSetsListItemFragmentFragment;

const Context = createContext({} as ContextType);

type FileInfo = {
  size: string;
  lastModifiedAt?: Date;
};

export function getContainerIDs(
  parentID: string,
  ownershipMap: Map<string, string[]>,
  containerIDs: string[] = [],
): string[] {
  ownershipMap.get(parentID)?.forEach((childID) => {
    if (ownershipMap.has(childID)) getContainerIDs(childID, ownershipMap, containerIDs);
    else containerIDs.push(childID);
  });

  return containerIDs;
}

/**
 * Checks if all provided arrays are either undefined or empty
 */

export function noSearchResults(...arrays: (WorkloadItem[] | undefined)[]) {
  return arrays.every((array) => array === undefined || array.length === 0);
}

/**
 * Custom hooks
 */

function useCronJobs(kubeContext?: string) {
  return useListQueryWithSubscription({
    query: dashboardOps.HOME_CRONJOBS_LIST_FETCH,
    subscription: dashboardOps.HOME_CRONJOBS_LIST_WATCH,
    queryDataKey: 'batchV1CronJobsList',
    subscriptionDataKey: 'batchV1CronJobsWatch',
    variables: { kubeContext },
  });
}

function useDaemonSets(kubeContext?: string) {
  return useListQueryWithSubscription({
    query: dashboardOps.HOME_DAEMONSETS_LIST_FETCH,
    subscription: dashboardOps.HOME_DAEMONSETS_LIST_WATCH,
    queryDataKey: 'appsV1DaemonSetsList',
    subscriptionDataKey: 'appsV1DaemonSetsWatch',
    variables: { kubeContext },
  });
}

function useDeployments(kubeContext?: string) {
  return useListQueryWithSubscription({
    query: dashboardOps.HOME_DEPLOYMENTS_LIST_FETCH,
    subscription: dashboardOps.HOME_DEPLOYMENTS_LIST_WATCH,
    queryDataKey: 'appsV1DeploymentsList',
    subscriptionDataKey: 'appsV1DeploymentsWatch',
    variables: { kubeContext },
  });
}

function useJobs(kubeContext?: string) {
  return useListQueryWithSubscription({
    query: dashboardOps.HOME_JOBS_LIST_FETCH,
    subscription: dashboardOps.HOME_JOBS_LIST_WATCH,
    queryDataKey: 'batchV1JobsList',
    subscriptionDataKey: 'batchV1JobsWatch',
    variables: { kubeContext },
  });
}

function usePods(kubeContext?: string) {
  return useListQueryWithSubscription({
    query: dashboardOps.HOME_PODS_LIST_FETCH,
    subscription: dashboardOps.HOME_PODS_LIST_WATCH,
    queryDataKey: 'coreV1PodsList',
    subscriptionDataKey: 'coreV1PodsWatch',
    variables: { kubeContext },
  });
}

function useReplicaSets(kubeContext?: string) {
  return useListQueryWithSubscription({
    query: dashboardOps.HOME_REPLICASETS_LIST_FETCH,
    subscription: dashboardOps.HOME_REPLICASETS_LIST_WATCH,
    queryDataKey: 'appsV1ReplicaSetsList',
    subscriptionDataKey: 'appsV1ReplicaSetsWatch',
    variables: { kubeContext },
  });
}

function useStatefulSets(kubeContext?: string) {
  return useListQueryWithSubscription({
    query: dashboardOps.HOME_STATEFULSETS_LIST_FETCH,
    subscription: dashboardOps.HOME_STATEFULSETS_LIST_WATCH,
    queryDataKey: 'appsV1StatefulSetsList',
    subscriptionDataKey: 'appsV1StatefulSetsWatch',
    variables: { kubeContext },
  });
}

function useLogFileInfo(uids: string[], ownershipMap: Map<string, string[]>) {
  const logMetadataMap = useRecoilValue(logMetadataMapState);

  const logFileInfo = new Map<
    string,
    { size: number; lastModifiedAt: Date; containerIDs: string[] }
  >();
  uids.forEach((uid) => {
    const containerIDs = getContainerIDs(uid, ownershipMap);

    // combine fileInfo
    const fileInfo = {
      size: 0,
      lastModifiedAt: new Date(0),
      containerIDs,
    };

    containerIDs.forEach((containerID) => {
      const val = logMetadataMap.get(containerID);

      if (val?.size) {
        fileInfo.size += parseInt(val.size, 10);
      }

      if (val?.lastModifiedAt) {
        fileInfo.lastModifiedAt = new Date(
          Math.max(
            val.lastModifiedAt.getTime(),
            fileInfo.lastModifiedAt.getTime(),
          ),
        );
      }
    });

    // update map
    if (fileInfo.lastModifiedAt.getTime() > 0) logFileInfo.set(uid, fileInfo);
  });

  return logFileInfo;
}

/**
 * function to apply filters and search
 */

export function applySearchAndFilter(fetching: boolean, items: WorkloadItem[] | null | undefined, search: string, namespace: string): undefined | WorkloadItem[] {
  if (fetching) return undefined;

  // filter items
  const filteredItems = items?.filter((item) => {
    // remove deleted items
    if (item.metadata.deletionTimestamp) return false;

    // workloads withing namespace filter and search
    if (search !== '') {
      return ((namespace === '' || item.metadata.namespace === namespace) && item.metadata.name.includes(search));
    }

    // remove items not in filtered namespace
    return namespace === '' || item.metadata.namespace === namespace;
  });

  return filteredItems;
}

/**
 * LogMetadataMapProvider component
 */

const LogMetadataMapProvider = () => {
  const { kubeContext } = useContext(Context);
  const setLogMetadataMap = useSetRecoilState(logMetadataMapState);

  const logMetadata = useLogMetadata({
    enabled: appConfig.clusterAPIEnabled && kubeContext !== undefined,
    kubeContext: kubeContext || '',
    onUpdate: (containerID) => {
      document.querySelectorAll(`.last_event_${containerID}`).forEach((el) => {
        const k = 'animate-flash-bg-green';
        el.classList.remove(k);
        el.classList.add(k);
        setTimeout(() => el.classList.remove(k), 1000);
      });
    },
  });

  // TODO: This should be replaced with a more efficient implementation that updates
  //       the shared state using the hook's onUpdate() method
  useEffect(() => {
    const logMetadataMap = new Map<string, FileInfo>();
    logMetadata.data?.logMetadataList?.items.forEach((item) => {
      logMetadataMap.set(item.spec.containerID, item.fileInfo);
    });

    setLogMetadataMap(() => logMetadataMap);
  }, [JSON.stringify(logMetadata.data?.logMetadataList?.items)]);

  return null;
};

/**
 * KubeContextPicker component
 */

const KubeContextPicker = ({
  value,
  setValue,
}: {
  value?: string;
  setValue: (value: string) => void;
}) => {
  const { loading, data } = useSubscription(dashboardOps.KUBE_CONFIG_WATCH);
  const kubeConfig = data?.kubeConfigWatch?.object;

  // Set default value
  useEffect(() => {
    const defaultValue = kubeConfig?.currentContext;
    if (defaultValue) setValue(defaultValue);
  }, [loading]);

  return (
    <Form.Select
      value={value}
      className="m-0"
      onChange={(ev) => setValue(ev.target.value)}
      disabled={loading}
    >
      {loading ? (
        <Form.Option>Loading...</Form.Option>
      ) : (
        kubeConfig
        && kubeConfig.contexts.map((context) => (
          <Form.Option key={context.name} value={context.name}>
            {context.name}
          </Form.Option>
        ))
      )}
    </Form.Select>
  );
};

/**
 * SearchBox component
 */

const SearchBox = () => {
  const { setSearch } = useContext(Context);
  const deboucedSearch = useDebounceCallback((value: string) => setSearch(value), 300);

  return (
    <div className={cn('search-input relative')}>
      <FormControl
        id="search-box"
        onChange={(e) => deboucedSearch(e.target.value)}
        type="search"
        onKeyDown={(e) => e.key === 'Enter' && e.preventDefault()}
        placeholder="search workloads..."
        className="text-sm align-bottom pl-10 placeholder:text-chrome-400 w-64"
      />
      <Search className="absolute left-3 top-1/4 h-5 w-5 text-chrome-500" />
    </div>
  );
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

  return (
    <Form.Select
      className="mt-0"
      value={namespace}
      onChange={(ev) => setNamespace(ev.target.value)}
      disabled={loading}
    >
      {loading ? (
        <Form.Option>Loading...</Form.Option>
      ) : (
        <>
          <Form.Option value="">All namespaces</Form.Option>
          {data?.coreV1NamespacesList?.items.map((item) => (
            <Form.Option key={item.id} value={item.metadata.name}>
              {item.metadata.name}
            </Form.Option>
          ))}
        </>
      )}
    </Form.Select>
  );
};

/**
 * DisplayItems component
 */

const lastModifiedAtFormatter: Formatter = (value: number, unit: Unit, suffix: Suffix, epochMilliseconds: number, nextFormatter?: Formatter) => {
  if (suffix === 'from now' || unit === 'second') return 'just now';
  if (nextFormatter) return nextFormatter(value, unit, suffix, epochMilliseconds);
  return '';
};

type DisplayItemsProps = {
  workload: Workload;
  namespace: string;
  fetching: boolean;
  items:
  | WorkloadItem[]
  | undefined
  | null;
  ownershipMap: Map<string, string[]>;
};

const DisplayItems = ({
  workload,
  namespace,
  fetching,
  items,
  ownershipMap,
}: DisplayItemsProps) => {
  const { kubeContext, search, workloadFilter } = useContext(Context);

  // destructuring items array due to read-only array issue
  const filteredItems = items ? [...items] : [];

  const ids = filteredItems?.map((item) => item.metadata.uid) || [];
  const logFileInfo = useLogFileInfo(ids, ownershipMap);

  // handle sorting
  const [sortBy, setSortBy] = useState<SortBy>({
    field: 'name',
    direction: 'ASC',
  });
  const handleSortByChange = (newSortBy: SortBy) => setSortBy(newSortBy);

  if (filteredItems) {
    filteredItems.sort((a, b) => {
      let cmp = 0;
      switch (sortBy.field) {
        case 'name':
          cmp = a.metadata.name.localeCompare(b.metadata.name);
          break;
        case 'namespace':
          cmp = a.metadata.namespace.localeCompare(b.metadata.namespace);
          if (cmp === 0) cmp = a.metadata.name.localeCompare(b.metadata.name);
          break;
        case 'created':
          cmp = a.metadata.creationTimestamp - b.metadata.creationTimestamp;
          break;
        case 'size': {
          const sizeA = logFileInfo.get(a.metadata.uid)?.size || 0;
          const sizeB = logFileInfo.get(b.metadata.uid)?.size || 0;
          cmp = sizeA - sizeB;
          break;
        }
        case 'lastEvent': {
          const tsA = logFileInfo.get(a.metadata.uid)?.lastModifiedAt || new Date(0);
          const tsB = logFileInfo.get(b.metadata.uid)?.lastModifiedAt || new Date(0);
          cmp = tsA.getTime() - tsB.getTime();
          break;
        }
        default:
          throw new Error('sort field not implemented');
      }

      // sort alphabetically if same
      if (cmp === 0 && sortBy.field !== 'name') return a.metadata.name.localeCompare(b.metadata.name);

      // otherwise use original cmp
      return sortBy.direction === 'ASC' ? cmp : cmp * -1;
    });
  }

  // not showing all the rows at once when user selects all workloads filter
  const allWorkloadView = workloadFilter === undefined;

  // handle show some-or-all
  const [showAll, setShowAll] = useState(!allWorkloadView);
  const visibleItems = filteredItems && showAll ? filteredItems : filteredItems?.slice(0, 5);
  const hasMore = filteredItems && filteredItems.length > 5;

  // handle toggle-all
  const [selectAll, setSelectAll] = useState(false);
  const [isChecked, setIsChecked] = useState<Map<string, boolean>>(new Map());

  const handleSelectAllChange = () => {
    const newValue = !selectAll;
    setSelectAll(newValue);

    // update individual checkboxes
    filteredItems?.forEach((item) => isChecked.set(item.id, newValue));
    setIsChecked(new Map(isChecked));
  };

  const handleSingleCheckboxChange = (id: string) => {
    // update individual
    const newValue = !isChecked.get(id);
    isChecked.set(id, newValue);
    setIsChecked(new Map(isChecked));

    // update selectAll
    const values: boolean[] = [];
    filteredItems?.forEach((item) => values.push(isChecked.get(item.id) || false));

    // all-checked
    if (values.every((val) => val)) setSelectAll(true);

    // some-unchecked
    if (values.some((val) => !val)) setSelectAll(false);
  };

  // for label
  const Icon = iconMap[workload];
  const label = labelsPMap[workload];

  // updating showAll when we change workloadFilter,
  // the state was persistent when switching between workload filters earlier.
  useEffect(() => {
    setShowAll(!allWorkloadView);
  }, [workloadFilter]);

  // hides workloads that doesn't have any filtered items during search
  if (allWorkloadView && filteredItems?.length === 0 && search !== '') {
    return null;
  }

  return (
    <>
      <thead>
        <tr>
          <td colSpan={5} className="pb-[5px] text-[0.9rem]">
            <div className="flex items-center space-x-1">
              <Icon className="w-[22px] h-[22px]" />
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
          </td>
        </tr>
      </thead>
      <>
        <DataTable.Header
          className="rounded-thead bg-transparent"
          sortBy={sortBy}
          onSortByChange={handleSortByChange}
        >
          <DataTable.Row>
            <DataTable.HeaderCell>
              <Form.Check
                checked={selectAll}
                onChange={handleSelectAllChange}
              />
            </DataTable.HeaderCell>
            <DataTable.HeaderCell sortField="name" initialSortDirection="ASC">
              Name
            </DataTable.HeaderCell>
            {namespace === '' && (
              <DataTable.HeaderCell
                sortField="namespace"
                initialSortDirection="ASC"
              >
                Namespace
              </DataTable.HeaderCell>
            )}
            <DataTable.HeaderCell
              sortField="created"
              initialSortDirection="DESC"
            >
              Created
            </DataTable.HeaderCell>
            {appConfig.clusterAPIEnabled === true && (
              <>
                <DataTable.HeaderCell
                  sortField="size"
                  initialSortDirection="DESC"
                  className="text-right"
                >
                  Size
                </DataTable.HeaderCell>
                <DataTable.HeaderCell
                  sortField="lastEvent"
                  initialSortDirection="DESC"
                >
                  Last Event
                </DataTable.HeaderCell>
              </>
            )}
            <DataTable.HeaderCell>&nbsp;</DataTable.HeaderCell>
          </DataTable.Row>
        </DataTable.Header>

        {/* no resource found ui */}
        {!items?.length && (
          <DataTable.Body className="rounded-tbody">
            <DataTable.Row>
              <DataTable.DataCell colSpan={7}>
                <div className="flex flex-col items-center  py-1 ">
                  <Layers3 className="h-5 w-5 text-chrome-400" />
                  <span className="text-chrome-400 italic font-medium">
                    No resources found
                  </span>
                </div>
              </DataTable.DataCell>
            </DataTable.Row>
          </DataTable.Body>
        )}

        <DataTable.Body className="rounded-tbody">
          {visibleItems?.map((item) => {
            const sourceString = `${item.metadata.namespace}:${workload}/${item.metadata.name}`;
            const fileInfo = logFileInfo.get(item.metadata.uid);

            // for last event
            const lastEventCls = fileInfo?.containerIDs
              .map((id) => `last_event_${id}`)
              .join(' ');

            return (
              <DataTable.Row
                key={item.metadata.uid}
                className="text-chrome-700 max-w-full"
              >
                <DataTable.DataCell>
                  <Form.Check
                    name="source"
                    value={sourceString}
                    checked={isChecked.get(item.id) || false}
                    onChange={() => handleSingleCheckboxChange(item.id)}
                  />
                </DataTable.DataCell>
                <DataTable.DataCell>{item.metadata.name}</DataTable.DataCell>
                {namespace === '' && (
                  <DataTable.DataCell>
                    {item.metadata.namespace}
                  </DataTable.DataCell>
                )}
                <DataTable.DataCell>
                  <TimeAgo
                    key={Math.random()}
                    date={item.metadata.creationTimestamp}
                    title={item.metadata.creationTimestamp.toUTCString()}
                  />
                </DataTable.DataCell>
                {appConfig.clusterAPIEnabled === true && (
                  <>
                    <DataTable.DataCell className="text-right pr-[35px]">
                      {fileInfo?.size === undefined ? (
                        <span>--</span>
                      ) : (
                        numeral(fileInfo.size).format('0.0 b')
                      )}
                    </DataTable.DataCell>
                    <DataTable.DataCell className={lastEventCls}>
                      {fileInfo?.size === undefined ? (
                        <span>--</span>
                      ) : (
                        <TimeAgo
                          key={Math.random()}
                          date={fileInfo.lastModifiedAt}
                          formatter={lastModifiedAtFormatter}
                          minPeriod={60}
                          title={fileInfo.lastModifiedAt.toUTCString()}
                        />
                      )}
                    </DataTable.DataCell>
                  </>
                )}
                <DataTable.DataCell>
                  <a
                    target="_blank"
                    href={`${joinPaths(basename, '/console')}?kubeContext=${encodeURIComponent(kubeContext || '')}&source=${encodeURIComponent(sourceString)}`}
                    className="flex items-center underline text-primary"
                  >
                    <div>view</div>
                    <ArrowTopRightOnSquareIcon className="w-[18px] h-[18px] ml-1" />
                  </a>
                </DataTable.DataCell>
              </DataTable.Row>
            );
          })}
        </DataTable.Body>
        <tbody>
          <tr>
            <td colSpan={5} className="pb-[30px]">
              {/* showing this button only for all workloads view  */}
              {hasMore && allWorkloadView && (
                <button
                  type="button"
                  className="block underline cursor-pointer text-chrome-500"
                  onClick={() => setShowAll(!showAll)}
                >
                  {showAll ? 'Show less...' : 'Show more...'}
                </button>
              )}
            </td>
          </tr>
        </tbody>
      </>
    </>
  );
};

/**
 * DisplayWorkloads component
 */

const DisplayWorkloads = () => {
  const { search, kubeContext, workloadFilter, namespace } = useContext(Context);

  const cronjobs = useCronJobs(kubeContext);
  const daemonsets = useDaemonSets(kubeContext);
  const deployments = useDeployments(kubeContext);
  const jobs = useJobs(kubeContext);
  const pods = usePods(kubeContext);
  const replicasets = useReplicaSets(kubeContext);
  const statefulsets = useStatefulSets(kubeContext);

  // calculate ownership map
  const ownershipMap = useMemo(() => {
    const m = new Map<string, string[]>();

    // add workload ids
    [
      ...(daemonsets.data?.appsV1DaemonSetsList?.items || []),
      ...(jobs.data?.batchV1JobsList?.items || []),
      ...(pods.data?.coreV1PodsList?.items || []),
      ...(replicasets.data?.appsV1ReplicaSetsList?.items || []),
      ...(statefulsets.data?.appsV1StatefulSetsList?.items || []),
    ].forEach((item) => {
      item.metadata.ownerReferences.forEach((ref) => {
        const childUIDs = m.get(ref.uid) || [];
        childUIDs.push(item.metadata.uid);
        m.set(ref.uid, childUIDs);
      });
    });

    // add container ids
    pods.data?.coreV1PodsList?.items.forEach((pod) => {
      // strip out prefix (e.g. "containerd://")
      const containerIDs = pod.status.containerStatuses.map((status) => status.containerID.replace(/^[^:]+:\/\/(.*)/, '$1'));
      m.set(pod.metadata.uid, containerIDs);
    });

    return m;
  }, [
    daemonsets.data?.appsV1DaemonSetsList?.metadata.resourceVersion,
    jobs.data?.batchV1JobsList?.metadata.resourceVersion,
    pods.data?.coreV1PodsList?.metadata.resourceVersion,
    replicasets.data?.appsV1ReplicaSetsList?.metadata.resourceVersion,
    statefulsets.data?.appsV1StatefulSetsList?.metadata.resourceVersion,
  ]);

  const filterCronJobs = applySearchAndFilter(cronjobs.fetching, cronjobs.data?.batchV1CronJobsList?.items, search, namespace);
  const filterDaemonsets = applySearchAndFilter(daemonsets.fetching, daemonsets.data?.appsV1DaemonSetsList?.items, search, namespace);
  const filterPods = applySearchAndFilter(pods.fetching, pods.data?.coreV1PodsList?.items, search, namespace);
  const filterJobs = applySearchAndFilter(jobs.fetching, jobs.data?.batchV1JobsList?.items, search, namespace);
  const filterDeployments = applySearchAndFilter(deployments.fetching, deployments.data?.appsV1DeploymentsList?.items, search, namespace);
  const filterReplicasets = applySearchAndFilter(replicasets.fetching, replicasets.data?.appsV1ReplicaSetsList?.items, search, namespace);
  const filterStatefulsets = applySearchAndFilter(statefulsets.fetching, statefulsets.data?.appsV1StatefulSetsList?.items, search, namespace);

  // we want to show this only when user searches for a workload
  const noResultFound = search !== '' ? noSearchResults(filterCronJobs, filterDeployments, filterPods, filterJobs, filterDaemonsets, filterReplicasets, filterStatefulsets) : false;

  // Render data tables
  const tableEls: JSX.Element[] = [];

  if (!workloadFilter || workloadFilter === Workload.CRONJOBS) {
    tableEls.push(
      <DisplayItems
        key={Workload.CRONJOBS}
        workload={Workload.CRONJOBS}
        namespace={namespace}
        fetching={cronjobs.fetching}
        items={filterCronJobs}
        ownershipMap={ownershipMap}
      />,
    );
  }

  if (!workloadFilter || workloadFilter === Workload.DAEMONSETS) {
    tableEls.push(
      <DisplayItems
        key={Workload.DAEMONSETS}
        workload={Workload.DAEMONSETS}
        namespace={namespace}
        fetching={daemonsets.fetching}
        items={filterDaemonsets}
        ownershipMap={ownershipMap}
      />,
    );
  }

  if (!workloadFilter || workloadFilter === Workload.DEPLOYMENTS) {
    tableEls.push(
      <DisplayItems
        key={Workload.DEPLOYMENTS}
        workload={Workload.DEPLOYMENTS}
        namespace={namespace}
        fetching={deployments.fetching}
        items={filterDeployments}
        ownershipMap={ownershipMap}
      />,
    );
  }

  if (!workloadFilter || workloadFilter === Workload.JOBS) {
    tableEls.push(
      <DisplayItems
        key={Workload.JOBS}
        workload={Workload.JOBS}
        namespace={namespace}
        fetching={jobs.fetching}
        items={filterJobs}
        ownershipMap={ownershipMap}
      />,
    );
  }

  if (!workloadFilter || workloadFilter === Workload.PODS) {
    tableEls.push(
      <DisplayItems
        key={Workload.PODS}
        workload={Workload.PODS}
        namespace={namespace}
        fetching={pods.fetching}
        items={filterPods}
        ownershipMap={ownershipMap}
      />,
    );
  }

  if (!workloadFilter || workloadFilter === Workload.REPLICASETS) {
    tableEls.push(
      <DisplayItems
        key={Workload.REPLICASETS}
        workload={Workload.REPLICASETS}
        namespace={namespace}
        fetching={replicasets.fetching}
        items={filterReplicasets}
        ownershipMap={ownershipMap}
      />,
    );
  }

  if (!workloadFilter || workloadFilter === Workload.STATEFULSETS) {
    tableEls.push(
      <DisplayItems
        key={Workload.STATEFULSETS}
        workload={Workload.STATEFULSETS}
        namespace={namespace}
        fetching={statefulsets.fetching}
        items={filterStatefulsets}
        ownershipMap={ownershipMap}
      />,
    );
  }

  return (
    <>
      <DataTable className="rounded-table-wrapper overflow-visible w-full" size="sm">
        {tableEls}
      </DataTable>
      {/* showing this only on all workloads view */}
      {noResultFound && workloadFilter === undefined && (
        <div className="flex items-center border border-dashed border-secondary rounded-md justify-center h-32">
          <div className="text-center">
            <Search className="h-8 w-8 text-chrome-400 mx-auto mb-2" />
            <p className="text-base text-chrome-400">No matching workloads found</p>
          </div>
        </div>
      )}
    </>
  );
};

/**
 * Header component
 */

const Header = () => {
  const { kubeContext, setKubeContext } = useContext(Context);

  return (
    <div className="px-4 py-[5px] flex items-center justify-between">
      <div className="flex items-center space-x-4">
        <a href="/">
          <img
            src={joinPaths(basename, logo)}
            alt="logo"
            className="display-block h-[40px]"
          />
        </a>
      </div>
      <div className="flex flex-row items-center gap-3">
        {appConfig.environment === 'desktop' && (
          <KubeContextPicker value={kubeContext} setValue={setKubeContext} />
        )}
        <SettingsDropdown />
      </div>
    </div>
  );
};

/**
 * CountBadge component
 */

const CountBadge = ({ count, workload, workloadFilter }: { count: number, workload: Workload, workloadFilter: Workload | undefined }) => (
  <span className={cn('text-xs font-medium px-2 py-[1px]  rounded-full  group-hover:bg-blue-200', workload === workloadFilter ? 'bg-blue-200' : 'bg-gray-200')}>
    {count}
  </span>
);

/**
 * Sidebar component
 */

const Sidebar = () => {
  const { workloadFilter, setWorkloadFilter, kubeContext, namespace } = useContext(Context);

  // kubeContext sometimes is undefined
  const { loading, error, counter } = useWorkloadCounter(
    kubeContext ?? '',
    namespace,
  );

  // using some default sidebar values during data loading and error states
  const sidebarItems: [Workload, number][] = loading || error
    ? allWorkloads.map((w) => [w, 0])
    : Array.from(counter.entries());

  return (
    <div className="px-4">
      <ul className="space-y-1">
        {sidebarItems.map(([workload, count]) => (
          <li key={workload}>
            <button
              type="button"
              className={cn(
                'flex items-center justify-between py-2 px-4 rounded-lg group hover:bg-blue-100 w-full',
                workload === workloadFilter
                  ? 'bg-blue-100 text-primary font-medium'
                  : 'text-chrome-500 ',
              )}
              onClick={() => setWorkloadFilter(workload)}
            >
              <span>{labelsPMap[workload]}</span>
              <CountBadge
                count={count}
                workload={workload}
                workloadFilter={workloadFilter}
              />
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
};

/**
 * Content component
 */

const Content = () => {
  const { kubeContext, sidebarOpen, setSidebarOpen } = useContext(Context);

  const readyWait = useSubscription(dashboardOps.KUBERNETES_API_READY_WAIT, {
    variables: { kubeContext },
  });

  return (
    <div className="px-[20px] py-[10px] ">
      {readyWait.loading || kubeContext === undefined ? (
        <div>Connecting...</div>
      ) : (
        <form
          method="get"
          target="_blank"
          action={joinPaths(basename, '/console')}
          className="w-full"
        >
          <input type="hidden" name="kubeContext" value={kubeContext} />
          <div className="overflow-x-scroll">
            <div className="min-w-[810px] w-full">
              <div className="flex gap-4 py-4 justify-between flex-row w-full">
                <div className="flex gap-2 flex-row items-center">
                  {!sidebarOpen && (
                    <PanelLeftOpen
                      className="cursor-pointer text-chrome-400 hover:text-primary"
                      onClick={() => setSidebarOpen(true)}
                    />
                  )}
                  <h1 className="text-2xl font-semibold">Dashboard</h1>
                </div>
                <div className="flex gap-2">
                  <SearchBox />
                  <div className="block w-[200px]">
                    <NamespacesPicker />
                  </div>
                  <Button type="submit">
                    View in console
                    <ArrowTopRightOnSquareIcon className="w-[18px] h-[18px] ml-1" />
                  </Button>
                </div>
              </div>

              {/* Tables will be inside the same width-constrained container */}
              <DisplayWorkloads />
            </div>
          </div>
        </form>
      )}
    </div>
  );
};

/**
 * InnerLayout component
 */

type InnerLayoutProps = {
  header: JSX.Element;
  sidebar: JSX.Element;
  content: JSX.Element;
};

const InnerLayout = ({ sidebar, header, content }: InnerLayoutProps) => {
  const { setWorkloadFilter, sidebarOpen, setSidebarOpen } = useContext(Context);

  const sidebarWidth = sidebarOpen ? 200 : 0;

  return (
    <div className="h-full flex flex-col">
      <div className="bg-chrome-100 border-b border-chrome-divider">
        {header}
      </div>
      <div className="flex-1 h-0">
        <div className="flex h-full">
          <aside
            className="flex-shrink-0  bg-chrome-100 transition-all duration-100 ease-in relative overflow-y-auto"
            style={{ width: `${sidebarWidth}px` }}
          >
            <header className="flex flex-row px-4 pt-8 py-4 justify-between items-center gap-2">
              <button
                type="button"
                className="flex items-center gap-2"
                onClick={() => setWorkloadFilter(undefined)}
              >
                <Boxes className="h-6 w-6 text-chrome-600" />
                <span className="font-semibold text-lg"> Workloads</span>
              </button>
              <PanelLeftClose className="cursor-pointer text-chrome-400 hover:text-primary " onClick={() => setSidebarOpen(false)} />
            </header>
            {sidebarOpen && sidebar}
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
  const [workloadFilter, setWorkloadFilter] = useState<Workload>();
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [search, setSearch] = useState('');

  const [namespace, setNamespace] = useState('');

  const context = useMemo(() => ({
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
  }), [kubeContext, setKubeContext, namespace, setNamespace, workloadFilter, setWorkloadFilter, sidebarOpen, setSidebarOpen, search, setSearch]);

  return (
    <AuthRequired>
      <Context.Provider value={context}>
        <RecoilRoot>
          <LogMetadataMapProvider />
          <AppLayout>
            <InnerLayout
              header={<Header />}
              sidebar={<Sidebar />}
              content={<Content />}
            />
          </AppLayout>
        </RecoilRoot>
      </Context.Provider>
    </AuthRequired>
  );
}
