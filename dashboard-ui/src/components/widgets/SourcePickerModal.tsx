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

import { createContext, useContext, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import TimeAgo from 'react-timeago';

import Button from '@kubetail/ui/elements/Button';
import DataTable from '@kubetail/ui/elements/DataTable';
import type { SortBy } from '@kubetail/ui/elements/DataTable/Header';
import Form from '@kubetail/ui/elements/Form';
import Spinner from '@kubetail/ui/elements/Spinner';

import Modal from '@/components/elements/Modal';
import * as dashboardOps from '@/lib/graphql/dashboard/ops';
import { useCounterQueryWithSubscription, useListQueryWithSubscription } from '@/lib/hooks';
import { Counter, cn } from '@/lib/util';
import { Workload, allWorkloads, iconMap, labelsPMap, typenameMap } from '@/lib/workload';

type ContextType = {
  kubeContext: string;
  namespace: string;
  setNamespace: React.Dispatch<string>;
  selectedSources: Set<string>;
  setSelectedSources: React.Dispatch<Set<string>>;
};

const Context = createContext({} as ContextType);

/**
 * Workload counter hook
 */

function useWorkloadCounter(kubeContext: string, namespace: string = '') {
  const cronjobs = useCounterQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_CRONJOBS_COUNT_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_CRONJOBS_COUNT_WATCH,
    queryDataKey: 'batchV1CronJobsList',
    subscriptionDataKey: 'batchV1CronJobsWatch',
    variables: { kubeContext, namespace },
  });

  const daemonsets = useCounterQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_DAEMONSETS_COUNT_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_DAEMONSETS_COUNT_WATCH,
    queryDataKey: 'appsV1DaemonSetsList',
    subscriptionDataKey: 'appsV1DaemonSetsWatch',
    variables: { kubeContext, namespace },
  });

  const deployments = useCounterQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_DEPLOYMENTS_COUNT_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_DEPLOYMENTS_COUNT_WATCH,
    queryDataKey: 'appsV1DeploymentsList',
    subscriptionDataKey: 'appsV1DeploymentsWatch',
    variables: { kubeContext, namespace },
  });

  const jobs = useCounterQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_JOBS_COUNT_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_JOBS_COUNT_WATCH,
    queryDataKey: 'batchV1JobsList',
    subscriptionDataKey: 'batchV1JobsWatch',
    variables: { kubeContext, namespace },
  });

  const pods = useCounterQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_PODS_COUNT_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_PODS_COUNT_WATCH,
    queryDataKey: 'coreV1PodsList',
    subscriptionDataKey: 'coreV1PodsWatch',
    variables: { kubeContext, namespace },
  });

  const replicasets = useCounterQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_REPLICASETS_COUNT_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_REPLICASETS_COUNT_WATCH,
    queryDataKey: 'appsV1ReplicaSetsList',
    subscriptionDataKey: 'appsV1ReplicaSetsWatch',
    variables: { kubeContext, namespace },
  });

  const statefulsets = useCounterQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_STATEFULSETS_COUNT_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_STATEFULSETS_COUNT_WATCH,
    queryDataKey: 'appsV1StatefulSetsList',
    subscriptionDataKey: 'appsV1StatefulSetsWatch',
    variables: { kubeContext, namespace },
  });

  const reqs = [cronjobs, daemonsets, deployments, jobs, pods, replicasets, statefulsets];
  const loading = reqs.some((req) => req.loading);
  const error = reqs.find((req) => Boolean(req.error));

  const counter = new Counter<Workload>();

  function updateCounter(key: Workload, count: number | undefined) {
    if (count !== undefined) counter.set(key, count);
  }

  if (!loading && !error) {
    updateCounter(Workload.CRONJOBS, cronjobs.count);
    updateCounter(Workload.DAEMONSETS, daemonsets.count);
    updateCounter(Workload.DEPLOYMENTS, deployments.count);
    updateCounter(Workload.JOBS, jobs.count);
    updateCounter(Workload.PODS, pods.count);
    updateCounter(Workload.REPLICASETS, replicasets.count);
    updateCounter(Workload.STATEFULSETS, statefulsets.count);
  }

  return { loading, error, counter };
}

/**
 * Namespaces component
 */

const Namespaces = () => {
  const { kubeContext, namespace, setNamespace } = useContext(Context);

  const { data } = useListQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_NAMESPACES_LIST_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_NAMESPACES_LIST_WATCH,
    queryDataKey: 'coreV1NamespacesList',
    subscriptionDataKey: 'coreV1NamespacesWatch',
    variables: { kubeContext },
  });

  return (
    <Form.Select
      className="h-[35px] bg-chrome-50 border border-chrome-30 text-sm rounded-lg !mt-0"
      onChange={(ev) => setNamespace(ev.target.value)}
      value={namespace}
    >
      <Form.Option value="">All namespaces</Form.Option>
      {data?.coreV1NamespacesList?.items.map((item) => (
        <Form.Option key={item.id} value={item.metadata.name}>{item.metadata.name}</Form.Option>
      ))}
    </Form.Select>
  );
};

/**
 * Sidebar component
 */

const Sidebar = ({
  workloadState,
}: {
  workloadState: [Workload | null, React.Dispatch<React.SetStateAction<Workload | null>>];
}) => {
  const { kubeContext, namespace } = useContext(Context);
  const { counter } = useWorkloadCounter(kubeContext, namespace);
  const [currWorkload, setCurrWorkload] = workloadState;

  return (
    <ul className="text-[.85rem]">
      <li>
        <div className="font-bold text-chrome-600 mt-[5px] mb-[12px]">Workloads</div>
        <div>
          <ul className="inline-grid space-y-0">
            {allWorkloads.map((workload) => {
              const Icon = iconMap[workload];
              return (
                <li
                  key={workload}
                  className="ml-[-8px]"
                >
                  <button
                    type="button"
                    className={cn(
                      'w-full px-[8px] py-[5px] cursor-pointer rounded-sm flex items-center',
                      currWorkload === workload ? 'bg-chrome-300' : 'hover:bg-chrome-200',
                    )}
                    onClick={() => setCurrWorkload(workload)}
                  >
                    <Icon className="h-[18px] w-[18px]" />
                    <div className="ml-1 text-chrome-700">
                      {labelsPMap[workload]}
                      {' '}
                      {counter.has(workload) && `(${counter.get(workload)})`}
                    </div>
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      </li>
    </ul>
  );
};

/**
 * Workload items display component
 */

type DisplayItemsProps = {
  items: {
    __typename?: string;
    metadata: {
      name: string;
      namespace: string;
      uid: string;
      creationTimestamp: any;
    }
  }[];
};

function isSuperset<T>(set: Set<T>, subset: Set<T>) {
  for (const elem of subset) {
    if (!set.has(elem)) {
      return false;
    }
  }
  return true;
}

const DisplayItems = ({ items }: DisplayItemsProps) => {
  const { namespace, selectedSources, setSelectedSources } = useContext(Context);

  // filter items
  const filteredItems = items?.filter((item) => namespace === '' || item.metadata.namespace === namespace);

  // handle sorting
  const [sortBy, setSortBy] = useState<SortBy>({ field: 'name', direction: 'ASC' });
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
        default:
          throw new Error('sort field not implemented');
      }

      // sort alphabetically if same
      if (cmp === 0 && sortBy.field !== 'name') return a.metadata.name.localeCompare(b.metadata.name);

      // otherwise use original cmp
      return sortBy.direction === 'ASC' ? cmp : cmp * -1;
    });
  }

  const genSourcePath = (item: any) => `${item.metadata.namespace}/${typenameMap[item.__typename || '']}/${item.metadata.name}`;

  // source toggler
  const filteredSourcePaths = new Set(filteredItems.map((item) => genSourcePath(item)));

  const [allSourcesChecked, setAllSourcesChecked] = useState(isSuperset(selectedSources, filteredSourcePaths));

  const handleSourceToggle = (sourcePath: string) => {
    // update individual checkbox
    if (selectedSources.has(sourcePath)) selectedSources.delete(sourcePath);
    else selectedSources.add(sourcePath);
    setSelectedSources(new Set(selectedSources));

    // update allSources checkbox
    if (isSuperset(selectedSources, filteredSourcePaths)) setAllSourcesChecked(true);
    else setAllSourcesChecked(false);
  };

  const handleAllSourcesToggle = () => {
    const isChecked = !allSourcesChecked;
    if (isChecked) filteredSourcePaths.forEach((path) => selectedSources.add(path));
    else filteredSourcePaths.forEach((path) => selectedSources.delete(path));
    setSelectedSources(new Set(selectedSources));
    setAllSourcesChecked(isChecked);
  };

  return (
    <DataTable size="sm">
      <DataTable.Header
        sortBy={sortBy}
        onSortByChange={handleSortByChange}
      >
        <DataTable.Row>
          <DataTable.HeaderCell>
            <Form.Check
              checked={allSourcesChecked}
              onChange={handleAllSourcesToggle}
            />
          </DataTable.HeaderCell>
          <DataTable.HeaderCell sortField="name" initialSortDirection="ASC">Name</DataTable.HeaderCell>
          {namespace === '' && <DataTable.HeaderCell sortField="namespace" initialSortDirection="ASC">Namespace</DataTable.HeaderCell>}
          <DataTable.HeaderCell sortField="created" initialSortDirection="DESC" className="min-w-[140px]">Created</DataTable.HeaderCell>
        </DataTable.Row>
      </DataTable.Header>
      <DataTable.Body>
        {filteredItems.map((item) => {
          const sourcePath = genSourcePath(item);
          return (
            <DataTable.Row key={item.metadata.uid}>
              <DataTable.DataCell>
                <Form.Check
                  checked={selectedSources.has(sourcePath)}
                  onChange={() => handleSourceToggle(sourcePath)}
                />
              </DataTable.DataCell>
              <DataTable.DataCell>{item.metadata.name}</DataTable.DataCell>
              {namespace === '' && <DataTable.DataCell>{item.metadata.namespace}</DataTable.DataCell>}
              <DataTable.DataCell>
                <TimeAgo
                  date={item.metadata.creationTimestamp}
                  title={item.metadata.creationTimestamp.toUTCString()}
                />
              </DataTable.DataCell>
            </DataTable.Row>
          );
        })}
      </DataTable.Body>
    </DataTable>
  );
};

/**
 * Workload display components
 */

const DisplayCronJobs = () => {
  const { kubeContext } = useContext(Context);

  const { loading, data } = useListQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_CRONJOBS_LIST_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_CRONJOBS_LIST_WATCH,
    queryDataKey: 'batchV1CronJobsList',
    subscriptionDataKey: 'batchV1CronJobsWatch',
    variables: { kubeContext },
  });

  if (loading) return <Spinner size="sm" />;
  return (
    <DisplayItems items={data?.batchV1CronJobsList?.items || []} />
  );
};

const DisplayDaemonSets = () => {
  const { kubeContext } = useContext(Context);

  const { loading, data } = useListQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_DAEMONSETS_LIST_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_DAEMONSETS_LIST_WATCH,
    queryDataKey: 'appsV1DaemonSetsList',
    subscriptionDataKey: 'appsV1DaemonSetsWatch',
    variables: { kubeContext },
  });

  if (loading) return <Spinner size="sm" />;
  return (
    <DisplayItems items={data?.appsV1DaemonSetsList?.items || []} />
  );
};

const DisplayDeployments = () => {
  const { kubeContext } = useContext(Context);

  const { loading, data } = useListQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_DEPLOYMENTS_LIST_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_DEPLOYMENTS_LIST_WATCH,
    queryDataKey: 'appsV1DeploymentsList',
    subscriptionDataKey: 'appsV1DeploymentsWatch',
    variables: { kubeContext },
  });

  if (loading) return <Spinner size="sm" />;
  return (
    <DisplayItems items={data?.appsV1DeploymentsList?.items || []} />
  );
};

const DisplayJobs = () => {
  const { kubeContext } = useContext(Context);

  const { loading, data } = useListQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_JOBS_LIST_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_JOBS_LIST_WATCH,
    queryDataKey: 'batchV1JobsList',
    subscriptionDataKey: 'batchV1JobsWatch',
    variables: { kubeContext },
  });

  if (loading) return <Spinner size="sm" />;
  return (
    <DisplayItems items={data?.batchV1JobsList?.items || []} />
  );
};

const DisplayPods = () => {
  const { kubeContext } = useContext(Context);

  const { loading, data } = useListQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_PODS_LIST_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_PODS_LIST_WATCH,
    queryDataKey: 'coreV1PodsList',
    subscriptionDataKey: 'coreV1PodsWatch',
    variables: { kubeContext },
  });

  if (loading) return <Spinner size="sm" />;
  return (
    <DisplayItems items={data?.coreV1PodsList?.items || []} />
  );
};

const DisplayReplicaSets = () => {
  const { kubeContext } = useContext(Context);

  const { loading, data } = useListQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_REPLICASETS_LIST_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_REPLICASETS_LIST_WATCH,
    queryDataKey: 'appsV1ReplicaSetsList',
    subscriptionDataKey: 'appsV1ReplicaSetsWatch',
    variables: { kubeContext },
  });

  if (loading) return <Spinner size="sm" />;
  return (
    <DisplayItems items={data?.appsV1ReplicaSetsList?.items || []} />
  );
};

const DisplayStatefulSets = () => {
  const { kubeContext } = useContext(Context);

  const { loading, data } = useListQueryWithSubscription({
    query: dashboardOps.SOURCE_PICKER_STATEFULSETS_LIST_FETCH,
    subscription: dashboardOps.SOURCE_PICKER_STATEFULSETS_LIST_WATCH,
    queryDataKey: 'appsV1StatefulSetsList',
    subscriptionDataKey: 'appsV1StatefulSetsWatch',
    variables: { kubeContext },
  });

  if (loading) return <Spinner size="sm" />;
  return (
    <DisplayItems items={data?.appsV1StatefulSetsList?.items || []} />
  );
};

const displayWorkloadComponents = {
  [Workload.CRONJOBS]: DisplayCronJobs,
  [Workload.DAEMONSETS]: DisplayDaemonSets,
  [Workload.DEPLOYMENTS]: DisplayDeployments,
  [Workload.JOBS]: DisplayJobs,
  [Workload.PODS]: DisplayPods,
  [Workload.REPLICASETS]: DisplayReplicaSets,
  [Workload.STATEFULSETS]: DisplayStatefulSets,
};

/**
 * Main component
 */
const Main = ({
  workloadState,
}: {
  workloadState: [Workload | null, React.Dispatch<React.SetStateAction<Workload | null>>];
}) => {
  const [currWorkload] = workloadState;
  if (!currWorkload) return <div />;
  const DisplayWorkloadComponent = displayWorkloadComponents[currWorkload];

  return (
    <DisplayWorkloadComponent />
  );
};

/**
 * Explorer component
 */

const Explorer = () => {
  const workloadState = useState<Workload | null>(null);

  return (
    <div className="flex space-x-2">
      <Sidebar workloadState={workloadState} />
      <div className="flex-grow">
        <Main workloadState={workloadState} />
      </div>
    </div>
  );
};

/**
 * Default component
 */
const SourcePickerModal = ({ onClose }: { onClose: (value?: boolean) => void; }) => {
  const [searchParams] = useSearchParams();
  const [namespace, setNamespace] = useState('');
  const [selectedSources, setSelectedSources] = useState(new Set(searchParams.getAll('source')));

  const kubeContext = searchParams.get('kubeContext') || '';

  const handleUpdate = () => {
    const sourcePaths = Array.from(selectedSources);
    sourcePaths.sort();

    searchParams.delete('source');
    sourcePaths.forEach((sourcePath) => searchParams.append('source', sourcePath));

    // TODO: instead of navigating to new url can we use react-router?
    const currentUrl = new URL(window.location.href);
    currentUrl.search = (new URLSearchParams(searchParams)).toString();
    window.location.href = currentUrl.toString();

    onClose();
  };

  const context = useMemo(() => ({
    kubeContext,
    namespace,
    setNamespace,
    selectedSources,
    setSelectedSources,
  }), [kubeContext, namespace, setNamespace, selectedSources, setSelectedSources]);

  return (
    <Context.Provider value={context}>
      <Modal open onClose={() => onClose()} className="!max-w-[1000px]">
        <div className="flex items-center justify-between mb-[15px]">
          <div className="font-semibold">Choose logging sources</div>
          <div className="max-w-[200px]">
            <Namespaces />
          </div>
        </div>
        <Explorer />
        <div className="flex justify-end space-x-2 mt-[15px]">
          <Button intent="secondary" onClick={() => onClose()}>Cancel</Button>
          <Button intent="primary" onClick={() => handleUpdate()}>Update</Button>
        </div>
      </Modal>
    </Context.Provider>
  );
};

export default SourcePickerModal;
