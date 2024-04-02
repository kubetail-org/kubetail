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

import { ArrowTopRightOnSquareIcon } from '@heroicons/react/24/outline';
import { useState } from 'react';

import TimeAgo from 'react-timeago';

import Button from '@kubetail/ui/elements/Button';
import DataTable from '@kubetail/ui/elements/DataTable';
import type { SortBy } from '@kubetail/ui/elements/DataTable/Header';
import Form from '@kubetail/ui/elements/Form';
import Spinner from '@kubetail/ui/elements/Spinner';

import logo from '@/assets/logo.svg';
import AuthRequired from '@/components/utils/AuthRequired';
import Footer from '@/components/widgets/Footer';
import ProfilePicDropdown from '@/components/widgets/ProfilePicDropdown';
import * as ops from '@/lib/graphql/ops';
import { getBasename, joinPaths } from '@/lib/helpers';
import { useListQueryWithSubscription } from '@/lib/hooks';
import { Workload, iconMap, labelsPMap } from '@/lib/workload';


const Namespaces = ({
  value,
  setValue,
}: {
  value: string;
  setValue: (value: string) => void;
}) => {
  const { data } = useListQueryWithSubscription({
    query: ops.HOME_NAMESPACES_LIST_FETCH,
    subscription: ops.HOME_NAMESPACES_LIST_WATCH,
    queryDataKey: 'coreV1NamespacesList',
    subscriptionDataKey: 'coreV1NamespacesWatch',
  });

  return (
    <Form.Select
      onChange={(ev) => setValue(ev.target.value)}
      value={value}
    >
      <Form.Option value="">All namespaces</Form.Option>
      {data?.coreV1NamespacesList?.items.map(item => (
        <Form.Option key={item.id} value={item.metadata.name}>{item.metadata.name}</Form.Option>
      ))}
    </Form.Select>
  );
};

type DisplayItemsProps = {
  workload: Workload;
  namespace: string;
  fetching: boolean;
  items: {
    id: string;
    metadata: {
      uid: string;
      namespace: string;
      name: string;
      creationTimestamp: any;
      deletionTimestamp?: Date;
    };
  }[] | undefined | null;
};

const DisplayItems = ({ workload, namespace, fetching, items }: DisplayItemsProps) => {
  // filter items
  const filteredItems = items?.filter((item) => {
    // remove deleted items
    if (item.metadata.deletionTimestamp) return false;

    // remove items not in filtered namespace
    return (namespace === '' || item.metadata.namespace === namespace);
  });

  // handle sorting
  const [sortBy, setSortBy] = useState<SortBy>({ field: 'name', direction: 'ASC' });
  const handleSortByChange = (newSortBy: SortBy) => setSortBy(newSortBy);

  if (filteredItems) {
    filteredItems.sort((a, b) => {
      let cmp = 0;
      switch (sortBy.field) {
        case 'name':
          cmp = a.metadata.name.localeCompare(b.metadata.name);
          break
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
    })
  }

  // handle show some-or-all
  const [showAll, setShowAll] = useState<Boolean>(false);
  const visibleItems = (filteredItems && showAll) ? filteredItems : filteredItems?.slice(0, 5);
  const hasMore = filteredItems && filteredItems.length > 5;

  // handle toggle-all
  const [selectAll, setSelectAll] = useState(false);
  const [isChecked, setIsChecked] = useState<Map<string, boolean>>(new Map());

  const handleSelectAllChange = () => {
    const newValue = !selectAll;
    setSelectAll(newValue);

    // update individual checkboxes
    filteredItems?.forEach(item => isChecked.set(item.id, newValue));
    setIsChecked(new Map(isChecked));
  };

  const handleSingleCheckboxChange = (id: string) => {
    // update individual
    const newValue = !isChecked.get(id);
    isChecked.set(id, newValue);
    setIsChecked(new Map(isChecked));

    // update selectAll
    const values: Boolean[] = [];
    filteredItems?.forEach(item => values.push(isChecked.get(item.id) || false));

    // all-checked
    if (values.every(val => val)) setSelectAll(true);

    // some-unchecked
    if (values.some(val => !val)) setSelectAll(false);
  };

  // for label
  const Icon = iconMap[workload];
  const label = labelsPMap[workload]

  return (
    <>
      <thead>
        <tr>
          <td colSpan={5} className="pb-[5px] text-[0.9rem]">
            <div className="flex items-center space-x-1">
              <Icon className="w-[22px] h-[22px]" />
              <div>{label}</div>
              <div>({filteredItems?.length})</div>
              {fetching && <div><Spinner size="xs" /></div>}
            </div>
          </td>
        </tr>
      </thead>
      {!filteredItems?.length && (
        <tbody>
          <tr>
            <td colSpan={5} className="pb-[30px] italic text-chrome-400">
              No results
            </td>
          </tr>
        </tbody>
      )}
      {filteredItems && filteredItems.length > 0 && (
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
              <DataTable.HeaderCell
                sortField="name"
                initialSortDirection="ASC"
              >
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
              <DataTable.HeaderCell>&nbsp;</DataTable.HeaderCell>
            </DataTable.Row>
          </DataTable.Header>
          <DataTable.Body className="rounded-tbody">
            {visibleItems?.map(item => {
              const sourceString = `${workload}/${item.metadata.namespace}/${item.metadata.name}`;
              return (
                <DataTable.Row key={item.metadata.uid} className="text-chrome-700">
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
                    <DataTable.DataCell>{item.metadata.namespace}</DataTable.DataCell>
                  )}
                  <DataTable.DataCell>
                    <TimeAgo date={item.metadata.creationTimestamp} title={item.metadata.creationTimestamp.toUTCString()} />
                  </DataTable.DataCell>
                  <DataTable.DataCell>
                    <a
                      target="_blank"
                      href={`${joinPaths(getBasename(), '/console')}?source=${encodeURIComponent(sourceString)}`}
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
                {hasMore && (
                  <a className="block underline cursor-pointer text-chrome-500" onClick={() => setShowAll(!showAll)}>
                    {showAll && 'Show less...' || 'Show more...'}
                  </a>
                )}
              </td>
            </tr>
          </tbody>
        </>
      )}
    </>
  )
};

const DisplayWorkloads = ({ namespace }: { namespace: string; }) => {
  const cronjobs = useListQueryWithSubscription({
    query: ops.HOME_CRONJOBS_LIST_FETCH,
    subscription: ops.HOME_CRONJOBS_LIST_WATCH,
    queryDataKey: 'batchV1CronJobsList',
    subscriptionDataKey: 'batchV1CronJobsWatch',
  });

  const daemonsets = useListQueryWithSubscription({
    query: ops.HOME_DAEMONSETS_LIST_FETCH,
    subscription: ops.HOME_DAEMONSETS_LIST_WATCH,
    queryDataKey: 'appsV1DaemonSetsList',
    subscriptionDataKey: 'appsV1DaemonSetsWatch',
  });

  const deployments = useListQueryWithSubscription({
    query: ops.HOME_DEPLOYMENTS_LIST_FETCH,
    subscription: ops.HOME_DEPLOYMENTS_LIST_WATCH,
    queryDataKey: 'appsV1DeploymentsList',
    subscriptionDataKey: 'appsV1DeploymentsWatch',
  });

  const jobs = useListQueryWithSubscription({
    query: ops.HOME_JOBS_LIST_FETCH,
    subscription: ops.HOME_JOBS_LIST_WATCH,
    queryDataKey: 'batchV1JobsList',
    subscriptionDataKey: 'batchV1JobsWatch',
  });

  const pods = useListQueryWithSubscription({
    query: ops.HOME_PODS_LIST_FETCH,
    subscription: ops.HOME_PODS_LIST_WATCH,
    queryDataKey: 'coreV1PodsList',
    subscriptionDataKey: 'coreV1PodsWatch',
  });

  const replicasets = useListQueryWithSubscription({
    query: ops.HOME_REPLICASETS_LIST_FETCH,
    subscription: ops.HOME_REPLICASETS_LIST_WATCH,
    queryDataKey: 'appsV1ReplicaSetsList',
    subscriptionDataKey: 'appsV1ReplicaSetsWatch',
  });

  const statefulsets = useListQueryWithSubscription({
    query: ops.HOME_STATEFULSETS_LIST_FETCH,
    subscription: ops.HOME_STATEFULSETS_LIST_WATCH,
    queryDataKey: 'appsV1StatefulSetsList',
    subscriptionDataKey: 'appsV1StatefulSetsWatch',
  });

  const LoadingModal = () => (
    <div className="relative z-10" role="dialog">
      <div className="fixed inset-0 bg-chrome-500 bg-opacity-75"></div>
      <div className="fixed inset-0 z-10 w-screen">
        <div className="flex min-h-full items-center justify-center p-0 text-center">
          <div className="relative transform overflow-hidden rounded-lg bg-background my-8 p-6 text-left shadow-xl">
            <div className="flex items-center space-x-2">
              <div>Loading Workloads</div>
              <Spinner size="sm" />
            </div>
          </div>
        </div>
      </div>
    </div>
  );

  const loading = cronjobs.loading || daemonsets.loading || deployments.loading || jobs.loading || pods.loading || replicasets.loading || statefulsets.loading;

  return (
    <>
      {loading && <LoadingModal />}
      <DataTable className="rounded-table-wrapper min-w-[600px]" size="sm">
        <DisplayItems
          workload={Workload.CRONJOBS}
          namespace={namespace}
          fetching={cronjobs.fetching}
          items={cronjobs.data?.batchV1CronJobsList?.items}
        />
        <DisplayItems
          workload={Workload.DAEMONSETS}
          namespace={namespace}
          fetching={daemonsets.fetching}
          items={daemonsets.data?.appsV1DaemonSetsList?.items}
        />
        <DisplayItems
          workload={Workload.DEPLOYMENTS}
          namespace={namespace}
          fetching={deployments.fetching}
          items={deployments.data?.appsV1DeploymentsList?.items}
        />
        <DisplayItems
          workload={Workload.JOBS}
          namespace={namespace}
          fetching={jobs.fetching}
          items={jobs.data?.batchV1JobsList?.items}
        />
        <DisplayItems
          workload={Workload.PODS}
          namespace={namespace}
          fetching={pods.fetching}
          items={pods.data?.coreV1PodsList?.items}
        />
        <DisplayItems
          workload={Workload.REPLICASETS}
          namespace={namespace}
          fetching={replicasets.fetching}
          items={replicasets.data?.appsV1ReplicaSetsList?.items}
        />
        <DisplayItems
          workload={Workload.STATEFULSETS}
          namespace={namespace}
          fetching={statefulsets.fetching}
          items={statefulsets.data?.appsV1StatefulSetsList?.items}
        />
      </DataTable>
    </>
  );
};

const Home = () => {
  const [namespace, setNamespace] = useState('');

  return (
    <>
      <div className="px-[10px] py-[5px] flex items-center justify-between border-b-[1px] border-chrome-300 bg-chrome-100">
        <a href="/">
          <img src={joinPaths(getBasename(), logo)} alt="logo" className="display-block h-[31.4167px]" />
        </a>
        <ProfilePicDropdown />
      </div>
      <main className="px-[10px]">
        <form
          method="get"
          target="_blank"
          action={joinPaths(getBasename(), '/console')}
        >
          <div className="flex items-start justify-between mt-[10px] mb-[20px]">
            <div className="block w-[200px]">
              <Namespaces value={namespace} setValue={setNamespace} />
            </div>
            <Button type="submit">
              View in console
              <ArrowTopRightOnSquareIcon className="w-[18px] h-[18px] ml-1" />
            </Button>
          </div>
          <DisplayWorkloads namespace={namespace} />
        </form>
      </main>
    </>
  );
};

/**
 * Default component
 */

export default function Page() {
  return (
    <AuthRequired>
      <Home />
      <div className="sticky bottom-0">
        <Footer />
      </div>
    </AuthRequired>
  );
}
