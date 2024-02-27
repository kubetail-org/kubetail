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

import React, { createContext, useContext, useEffect, useReducer } from 'react';

import * as ops from '@/lib/graphql/ops';
import { useGetQueryWithSubscription, useListQueryWithSubscription } from '@/lib/hooks';
import { Workload as WorkloadType } from '@/lib/workload';

import { LogFeedState, PodListResponse, WorkloadResponse } from './types';

type State = {
  sourceToWorkloadResponseMap: Map<string, WorkloadResponse>;
  sourceToPodListResponseMap: Map<string, PodListResponse>;
  logFeedState: LogFeedState;
  records: [number, number, number, number, number, number, number][];
};

type Context = {
  state: State;
  dispatch: React.Dispatch<Partial<State>>;
};

export const Context = createContext<Context>({} as Context);

function reducer(prevState: State, newState: Partial<State>): State {
  return Object.assign({}, { ...prevState }, { ...newState });
}

function initState(sourcePaths: string[]): State {
  const sourceToWorkloadResponseMap = new Map<string, WorkloadResponse>();
  const sourceToPodListResponseMap = new Map<string, PodListResponse>();

  sourcePaths.forEach(sourcePath => {
    sourceToWorkloadResponseMap.set(sourcePath, new WorkloadResponse());
    sourceToPodListResponseMap.set(sourcePath, new PodListResponse());
  });

  return {
    sourceToWorkloadResponseMap,
    sourceToPodListResponseMap,
    logFeedState: LogFeedState.Streaming,
    records: [],
  };
}

/**
 * Source map updater hook (for internal use)
 */

function useWorkloadMapUpdater(sourcePath: string, value: WorkloadResponse) {
  const { state, dispatch } = useContext(Context);
  const { sourceToWorkloadResponseMap } = state;

  useEffect(() => {
    sourceToWorkloadResponseMap.set(sourcePath, value);
    dispatch({ sourceToWorkloadResponseMap });
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
  if (matchLabels) labelSelector = Object.keys(matchLabels).map(k => `${k}=${matchLabels[k]}`).join(',');

  const { loading, error, data } = useListQueryWithSubscription({
    query: ops.CONSOLE_LOGGING_RESOURCES_PODS_FIND,
    subscription: ops.CONSOLE_LOGGING_RESOURCES_PODS_WATCH,
    queryDataKey: 'coreV1PodsList',
    subscriptionDataKey: 'coreV1PodsWatch',
    skip: !namespace || !labelSelector,
    variables: { namespace, labelSelector },
  });

  const { state, dispatch } = useContext(Context);
  const { sourceToPodListResponseMap } = state;

  useEffect(() => {
    const items = data?.coreV1PodsList?.items;
    sourceToPodListResponseMap.set(sourcePath, { loading, error, items });
    dispatch({ sourceToPodListResponseMap });
  }, [loading, error, data]);

  return <></>;
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
    <>
      {jobsReq.data?.batchV1JobsList?.items.map(job => {
        if (job.metadata.ownerReferences.some(ownerRef => ownerRef.uid === item?.metadata.uid)) {
          return (
            <LoadPodsForLabels
              key={job.metadata.uid}
              sourcePath={sourcePath}
              namespace={namespace}
              matchLabels={job.spec.selector?.matchLabels}
            />
          );
        }
      })}
    </>
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
  if (!item) return <></>;

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
  if (!item) return <></>;

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
  if (!item) return <></>;

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

  const { state, dispatch } = useContext(Context);
  const { sourceToPodListResponseMap } = state;

  useEffect(() => {
    const items = (item) ? [item] : undefined;
    sourceToPodListResponseMap.set(sourcePath, { loading, error, items });
    dispatch({ sourceToPodListResponseMap });
  }, [loading, error, data]);

  return <></>;
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
  if (!item) return <></>;

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
  if (!item) return <></>;

  return (
    <LoadPodsForLabels
      sourcePath={sourcePath}
      namespace={namespace}
      matchLabels={item.spec.selector?.matchLabels}
    />
  );
};

/**
 * Provider component
 */

interface LoggingResourcesProviderProps extends React.PropsWithChildren {
  sourcePaths: string[];
};

export const LoggingResourcesProvider = ({ sourcePaths, children }: LoggingResourcesProviderProps) => {
  // uniquify sourcePaths
  sourcePaths = Array.from(new Set(sourcePaths || []));
  sourcePaths.sort();

  // use state
  const [state, dispatch] = useReducer(reducer, sourcePaths, initState);

  // handle sourcePath deletions
  useEffect(() => {
    const { sourceToWorkloadResponseMap, sourceToPodListResponseMap } = state;
    const difference = Array.from(sourceToWorkloadResponseMap.keys()).filter(x => !sourcePaths.includes(x));
    if (difference.length) {
      difference.forEach(key => sourceToWorkloadResponseMap.delete(key));
      difference.forEach(key => sourceToPodListResponseMap.delete(key));
      dispatch({ sourceToWorkloadResponseMap, sourceToPodListResponseMap });
    }
  }, [JSON.stringify(sourcePaths)]);

  useEffect(() => {
    if (state.logFeedState === LogFeedState.Streaming) {
      const id = setInterval(() => {
        const records = state.records;
        records.push([
          Math.random(),
          Math.random(),
          Math.random(),
          Math.random(),
          Math.random(),
          Math.random(),
          Math.random(),
        ]);
        dispatch({ records });
      }, 1000);
      return () => clearInterval(id);
    }
  }, [state.logFeedState]);

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
    <Context.Provider value={{ state, dispatch }}>
      {sourcePaths.map(path => {
        const parts = path.split('/');
        if (!(parts[0] in resourceLoaders)) throw new Error(`not implemented: ${parts[0]}`);
        const Component = resourceLoaders[parts[0] as WorkloadType];
        return <Component key={path} sourcePath={path} />
      })}
      {children}
    </Context.Provider>
  );
};
