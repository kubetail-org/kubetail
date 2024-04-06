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

import { ApolloError } from '@apollo/client';
import { useEffect } from 'react';
import {
  RecoilRoot, atom, useRecoilState, useRecoilValue, useSetRecoilState,
} from 'recoil';

import type { ExtractQueryType } from '@/app-env';
import * as fragments from '@/lib/graphql/fragments';
import * as ops from '@/lib/graphql/ops';
import { useGetQueryWithSubscription, useListQueryWithSubscription } from '@/lib/hooks';
import { Workload as WorkloadType, typenameMap } from '@/lib/workload';

/**
 * Shared types
 */

export type Node = ExtractQueryType<typeof fragments.CONSOLE_NODES_LIST_ITEM_FRAGMENT>;
export type Workload = ExtractQueryType<typeof fragments.CONSOLE_LOGGING_RESOURCES_GENERIC_OBJECT_FRAGMENT>;
export type Pod = ExtractQueryType<typeof fragments.CONSOLE_LOGGING_RESOURCES_POD_FRAGMENT>;

class WorkloadResponse {
  loading: boolean = false;

  error?: ApolloError = undefined;

  item?: Workload | null = undefined;
}

class PodListResponse {
  loading: boolean = false;

  error?: ApolloError = undefined;

  items?: Pod[] | null = undefined;
}

/**
 * State
 */

const sourceToWorkloadResponseMapState = atom({
  key: 'sourceToWorkloadResponseMap',
  default: new Map<string, WorkloadResponse>(),
});

const sourceToPodListResponseMapState = atom({
  key: 'sourceToPodListResponseMap',
  default: new Map<string, PodListResponse>(),
});

/**
 * Hooks
 */

export function useNodes() {
  const { fetching, data } = useListQueryWithSubscription({
    query: ops.CONSOLE_NODES_LIST_FETCH,
    subscription: ops.CONSOLE_NODES_LIST_WATCH,
    queryDataKey: 'coreV1NodesList',
    subscriptionDataKey: 'coreV1NodesWatch',
  });

  const loading = fetching; // treat still-fetching as still-loading
  const nodes = (data?.coreV1NodesList?.items) ? data.coreV1NodesList.items : [] as Node[];

  return { loading, nodes };
}

export function useWorkloads() {
  const sourceToWorkloadResponseMap = useRecoilValue(sourceToWorkloadResponseMapState);

  let loading = false;
  const workloads = new Map<WorkloadType, Workload[]>();

  // group sources by workload type
  sourceToWorkloadResponseMap.forEach((val) => {
    loading = loading || val.loading;
    const { item } = val;
    if (!item?.__typename) return;
    const workload = typenameMap[item.__typename];
    const items = workloads.get(workload) || [];
    items.push(item);
    workloads.set(workload, items);
  });

  return { loading, workloads };
}

export function usePods() {
  const sourceToPodListResponseMap = useRecoilValue(sourceToPodListResponseMapState);

  let loading = false;
  const pods: Pod[] = [];

  // uniquify
  const usedIDs = new Set<string>();
  sourceToPodListResponseMap.forEach((val) => {
    loading = loading || val.loading;
    val.items?.forEach((item) => {
      if (usedIDs.has(item.metadata.uid)) return;
      pods.push(item);
      usedIDs.add(item.metadata.uid);
    });
  });

  // sort
  pods.sort((a, b) => a.metadata.name.localeCompare(b.metadata.name));

  return { loading, pods };
}

/**
 * Source map updater hook (for internal use)
 */

function useWorkloadMapUpdater(sourcePath: string, value: WorkloadResponse) {
  const setSourceToWorkloadResponseMap = useSetRecoilState(sourceToWorkloadResponseMapState);

  useEffect(() => {
    setSourceToWorkloadResponseMap((oldVal) => {
      const newVal = new Map(oldVal);
      newVal.set(sourcePath, value);
      return newVal;
    });
  }, [JSON.stringify(value)]);
}

/**
 * Load pods matching label selector
 */

const LoadPodsForLabels = ({
  sourcePath,
  namespace,
  matchLabels,
}: {
  sourcePath: string;
  namespace: string;
  matchLabels: Record<string, string> | null | undefined;
}) => {
  let labelSelector = '';
  if (matchLabels) labelSelector = Object.keys(matchLabels).map((k) => `${k}=${matchLabels[k]}`).join(',');

  const { loading, error, data } = useListQueryWithSubscription({
    query: ops.CONSOLE_LOGGING_RESOURCES_PODS_FIND,
    subscription: ops.CONSOLE_LOGGING_RESOURCES_PODS_WATCH,
    queryDataKey: 'coreV1PodsList',
    subscriptionDataKey: 'coreV1PodsWatch',
    skip: !namespace || !labelSelector,
    variables: { namespace, labelSelector },
  });

  const setSourceToPodListResponseMap = useSetRecoilState(sourceToPodListResponseMapState);

  useEffect(() => {
    setSourceToPodListResponseMap((oldVal) => {
      const items = data?.coreV1PodsList?.items;
      const newVal = new Map(oldVal);
      newVal.set(sourcePath, { loading, error, items });
      return newVal;
    });
  }, [loading, error, data]);

  return null;
};

/**
 * Fetch a CronJob workload and associated streams
 */

const LoadCronJobWorkload = ({ sourcePath }: { sourcePath: string }) => {
  const parts = sourcePath.split('/');
  const [namespace, name] = [parts[1], parts[2]];

  const { loading, error, data } = useGetQueryWithSubscription({
    query: ops.CONSOLE_LOGGING_RESOURCES_CRONJOB_GET,
    subscription: ops.CONSOLE_LOGGING_RESOURCES_CRONJOB_WATCH,
    queryDataKey: 'batchV1CronJobsGet',
    subscriptionDataKey: 'batchV1CronJobsWatch',
    skip: parts.length < 2,
    variables: { namespace, name },
  });

  // get all jobs in namespace
  const jobsReq = useListQueryWithSubscription({
    query: ops.CONSOLE_LOGGING_RESOURCES_JOBS_FIND,
    subscription: ops.CONSOLE_LOGGING_RESOURCES_JOBS_WATCH,
    queryDataKey: 'batchV1JobsList',
    subscriptionDataKey: 'batchV1JobsWatch',
    skip: !namespace,
    variables: { namespace },
  });

  const item = data?.batchV1CronJobsGet;

  // update source map
  useWorkloadMapUpdater(sourcePath, { loading, error, item });

  // load streams
  return (
    <div>
      {jobsReq.data?.batchV1JobsList?.items.map((job) => {
        if (job.metadata.ownerReferences.some((ownerRef) => ownerRef.uid === item?.metadata.uid)) {
          return (
            <LoadPodsForLabels
              key={job.metadata.uid}
              sourcePath={sourcePath}
              namespace={namespace}
              matchLabels={job.spec.selector?.matchLabels}
            />
          );
        }
        return null;
      })}
    </div>
  );
};

/**
 * Fetch a DaemonSet workload and associated streams
 */

const LoadDaemonSetWorkload = ({ sourcePath }: { sourcePath: string }) => {
  const parts = sourcePath.split('/');
  const [namespace, name] = [parts[1], parts[2]];

  const { loading, error, data } = useGetQueryWithSubscription({
    query: ops.CONSOLE_LOGGING_RESOURCES_DAEMONSET_GET,
    subscription: ops.CONSOLE_LOGGING_RESOURCES_DAEMONSET_WATCH,
    skip: parts.length < 2,
    variables: { namespace, name },
    queryDataKey: 'appsV1DaemonSetsGet',
    subscriptionDataKey: 'appsV1DaemonSetsWatch',
  });

  const item = data?.appsV1DaemonSetsGet;

  // update source map
  useWorkloadMapUpdater(sourcePath, { loading, error, item });

  // load streams
  if (!item) return null;

  return (
    <LoadPodsForLabels
      sourcePath={sourcePath}
      namespace={namespace}
      matchLabels={item.spec.selector?.matchLabels}
    />
  );
};

/**
 * Fetch a Deployment workload and associated streams
 */

const LoadDeploymentWorkload = ({ sourcePath }: { sourcePath: string }) => {
  const parts = sourcePath.split('/');
  const [namespace, name] = [parts[1], parts[2]];

  const { loading, error, data } = useGetQueryWithSubscription({
    query: ops.CONSOLE_LOGGING_RESOURCES_DEPLOYMENT_GET,
    subscription: ops.CONSOLE_LOGGING_RESOURCES_DEPLOYMENT_WATCH,
    skip: parts.length < 2,
    variables: { namespace, name },
    queryDataKey: 'appsV1DeploymentsGet',
    subscriptionDataKey: 'appsV1DeploymentsWatch',
  });

  const item = data?.appsV1DeploymentsGet;

  // update source map
  useWorkloadMapUpdater(sourcePath, { loading, error, item });

  // load streams
  if (!item) return null;

  return (
    <LoadPodsForLabels
      sourcePath={sourcePath}
      namespace={namespace}
      matchLabels={item.spec.selector?.matchLabels}
    />
  );
};

/**
 * Fetch a Job workload and associated streams
 */

const LoadJobWorkload = ({ sourcePath }: { sourcePath: string }) => {
  const parts = sourcePath.split('/');
  const [namespace, name] = [parts[1], parts[2]];

  const { loading, error, data } = useGetQueryWithSubscription({
    query: ops.CONSOLE_LOGGING_RESOURCES_JOB_GET,
    subscription: ops.CONSOLE_LOGGING_RESOURCES_JOB_WATCH,
    skip: parts.length < 2,
    variables: { namespace, name },
    queryDataKey: 'batchV1JobsGet',
    subscriptionDataKey: 'batchV1JobsWatch',
  });

  const item = data?.batchV1JobsGet;

  // update source map
  useWorkloadMapUpdater(sourcePath, { loading, error, item });

  // load streams
  if (!item) return null;

  return (
    <LoadPodsForLabels
      sourcePath={sourcePath}
      namespace={namespace}
      matchLabels={item.spec.selector?.matchLabels}
    />
  );
};

/**
 * Fetch a Pod workload and associated streams
 */

const LoadPodWorkload = ({ sourcePath }: { sourcePath: string }) => {
  const parts = sourcePath.split('/');
  const [namespace, name] = [parts[1], parts[2]];

  const { loading, error, data } = useGetQueryWithSubscription({
    query: ops.CONSOLE_LOGGING_RESOURCES_POD_GET,
    subscription: ops.CONSOLE_LOGGING_RESOURCES_POD_WATCH,
    skip: parts.length < 3,
    variables: { namespace, name },
    queryDataKey: 'coreV1PodsGet',
    subscriptionDataKey: 'coreV1PodsWatch',
  });

  const item = data?.coreV1PodsGet;

  // update workload map
  useWorkloadMapUpdater(sourcePath, { loading, error, item });

  const [sourceToPodListResponseMap, setSourceToPodListResponseMap] = useRecoilState(sourceToPodListResponseMapState);

  useEffect(() => {
    const items = (item) ? [item] : undefined;
    const newMap = new Map(sourceToPodListResponseMap);
    newMap.set(sourcePath, { loading, error, items });
    setSourceToPodListResponseMap(newMap);
  }, [loading, error, data]);

  return null;
};

/**
 * Fetch a ReplicaSet workload and associated streams
 */

const LoadReplicaSetWorkload = ({ sourcePath }: { sourcePath: string }) => {
  const parts = sourcePath.split('/');
  const [namespace, name] = [parts[1], parts[2]];

  const { loading, error, data } = useGetQueryWithSubscription({
    query: ops.CONSOLE_LOGGING_RESOURCES_REPLICASET_GET,
    subscription: ops.CONSOLE_LOGGING_RESOURCES_REPLICASET_WATCH,
    skip: parts.length < 2,
    variables: { namespace, name },
    queryDataKey: 'appsV1ReplicaSetsGet',
    subscriptionDataKey: 'appsV1ReplicaSetsWatch',
  });

  const item = data?.appsV1ReplicaSetsGet;

  // update source map
  useWorkloadMapUpdater(sourcePath, { loading, error, item });

  // load streams
  if (!item) return null;

  return (
    <LoadPodsForLabels
      sourcePath={sourcePath}
      namespace={namespace}
      matchLabels={item.spec.selector?.matchLabels}
    />
  );
};

/**
 * Fetch a StatefulSet workload and associated streams
 */

const LoadStatefulSetWorkload = ({ sourcePath }: { sourcePath: string }) => {
  const parts = sourcePath.split('/');
  const [namespace, name] = [parts[1], parts[2]];

  const { loading, error, data } = useGetQueryWithSubscription({
    query: ops.CONSOLE_LOGGING_RESOURCES_STATEFULSET_GET,
    subscription: ops.CONSOLE_LOGGING_RESOURCES_STATEFULSET_WATCH,
    skip: parts.length < 2,
    variables: { namespace, name },
    queryDataKey: 'appsV1StatefulSetsGet',
    subscriptionDataKey: 'appsV1StatefulSetsWatch',
  });

  const item = data?.appsV1StatefulSetsGet;

  // update source map
  useWorkloadMapUpdater(sourcePath, { loading, error, item });

  // load streams
  if (!item) return null;

  return (
    <LoadPodsForLabels
      sourcePath={sourcePath}
      namespace={namespace}
      matchLabels={item.spec.selector?.matchLabels}
    />
  );
};

/**
 * Source deletion handler component
 */

type SourceDeletionHandlerProps = {
  sourcePaths: string[];
};

const removeUnusedKeys = <K, V>(origMap: Map<K, V>, usedKeys: K[]): Map<K, V> => {
  const newMap = new Map(origMap);
  Array.from(newMap.keys()).forEach((key) => {
    if (!usedKeys.includes(key)) newMap.delete(key);
  });
  return newMap;
};

const SourceDeletionHandler = ({ sourcePaths }: SourceDeletionHandlerProps) => {
  const setSourceToWorkloadResponseMap = useSetRecoilState(sourceToWorkloadResponseMapState);
  const setSourceToPodListResponseMap = useSetRecoilState(sourceToPodListResponseMapState);

  // handle sourcePath deletions
  useEffect(() => {
    setSourceToWorkloadResponseMap((oldVal) => removeUnusedKeys(oldVal, sourcePaths));
    setSourceToPodListResponseMap((oldVal) => removeUnusedKeys(oldVal, sourcePaths));
  }, [JSON.stringify(sourcePaths)]);

  return null;
};

/**
 * Provider component
 */

interface LoggingResourcesProviderProps extends React.PropsWithChildren {
  sourcePaths: string[];
}

export const LoggingResourcesProvider = ({ sourcePaths, children }: LoggingResourcesProviderProps) => {
  // uniquify sourcePaths
  const sourcePathsSorted = Array.from(new Set(sourcePaths || []));
  sourcePathsSorted.sort();

  const resourceLoaders = {
    [WorkloadType.CRONJOBS]: LoadCronJobWorkload,
    [WorkloadType.DAEMONSETS]: LoadDaemonSetWorkload,
    [WorkloadType.DEPLOYMENTS]: LoadDeploymentWorkload,
    [WorkloadType.JOBS]: LoadJobWorkload,
    [WorkloadType.PODS]: LoadPodWorkload,
    [WorkloadType.REPLICASETS]: LoadReplicaSetWorkload,
    [WorkloadType.STATEFULSETS]: LoadStatefulSetWorkload,
  };

  return (
    <RecoilRoot>
      <SourceDeletionHandler sourcePaths={sourcePathsSorted} />
      {sourcePathsSorted.map((path) => {
        const parts = path.split('/');
        if (!(parts[0] in resourceLoaders)) throw new Error(`not implemented: ${parts[0]}`);
        const Component = resourceLoaders[parts[0] as WorkloadType];
        return <Component key={path} sourcePath={path} />;
      })}
      {children}
    </RecoilRoot>
  );
};
