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

import { useQuery } from '@apollo/client';
import type { ApolloError } from '@apollo/client';
import { createContext, createRef, forwardRef, useContext, useEffect, useImperativeHandle, useRef, useState } from 'react';

import type { ExtractQueryType } from '@/app-env';
import * as fragments from '@/lib/graphql/fragments';
import * as ops from '@/lib/graphql/ops';
import type { LogRecord as GraphQLLogRecord } from '@/lib/graphql/__generated__/graphql';
import { useGetQueryWithSubscription, useListQueryWithSubscription } from '@/lib/hooks';
import { Workload, typenameMap } from '@/lib/workload';

export type LRNode = ExtractQueryType<typeof fragments.CONSOLE_NODES_LIST_ITEM_FRAGMENT>;
export type LRWorkload = ExtractQueryType<typeof fragments.CONSOLE_LOGGING_RESOURCES_GENERIC_OBJECT_FRAGMENT>;
export type LRPod = ExtractQueryType<typeof fragments.CONSOLE_LOGGING_RESOURCES_POD_FRAGMENT>;

export interface LogRecord extends GraphQLLogRecord {
  node: LRNode;
  pod: LRPod;
  container: string;
};

type OnRecordCallbackFunction = (record: LogRecord) => void;

export enum LogFeedState {
  Playing = 'PLAYING',
  Paused = 'PAUSED',
  InQuery = 'IN_QUERY',
}

export type LogFeedQueryOptions = {
  since?: string;
  until?: string;
};

/**
 * Context object
 */

type WorkloadMapValue = {
  loading: boolean;
  error: ApolloError | undefined;
  item: LRWorkload | null | undefined;
};

type WorkloadMap = Map<string, WorkloadMapValue>;

type PodMapValue = {
  loading: boolean;
  error: ApolloError | undefined;
  items: LRPod[] | null | undefined;
};

type PodMap = Map<string, PodMapValue>;

type Context = {
  workloadMap: WorkloadMap;
  setWorkloadMap: React.Dispatch<WorkloadMap>;
  podMap: PodMap;
  setPodMap: React.Dispatch<PodMap>;
  logFeedState: LogFeedState;
  setLogFeedState: React.Dispatch<LogFeedState>;
  logFeedLoaderRef: React.RefObject<HTMLDivElement>;
};

const Context = createContext<Context>({} as Context);

/**
 * Source map updater hook (for internal use)
 */

function useWorkloadMapUpdater(sourcePath: string, value: WorkloadMapValue) {
  const { workloadMap, setWorkloadMap } = useContext(Context);

  useEffect(() => {
    workloadMap.set(sourcePath, value);
    setWorkloadMap(new Map(workloadMap));
  }, [value.loading, value.error, value.item]);
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

  const { podMap, setPodMap } = useContext(Context);

  useEffect(() => {
    const items = data?.coreV1PodsList?.items;
    podMap.set(sourcePath, { loading, error, items });
    setPodMap(new Map(podMap));
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

  // update pod map
  const { podMap, setPodMap } = useContext(Context);

  useEffect(() => {
    const items = (item) ? [item] : undefined;
    podMap.set(sourcePath, { loading, error, items });
    setPodMap(new Map(podMap));
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
 * Log feed hook
 */


export function useLogFeed() {
  const { logFeedState, logFeedLoaderRef } = useContext(Context);

  const play = () => {
    const ev = new CustomEvent('play');
    logFeedLoaderRef.current?.dispatchEvent(ev);
  };

  const pause = () => {
    const ev = new CustomEvent('pause');
    logFeedLoaderRef.current?.dispatchEvent(ev);
  };

  const skipForward = () => {
    const ev = new CustomEvent('skipForward');
    logFeedLoaderRef.current?.dispatchEvent(ev);
  }

  const query = (args: LogFeedQueryOptions) => {
    const ev = new CustomEvent<LogFeedQueryOptions>('query', { detail: args });
    logFeedLoaderRef.current?.dispatchEvent(ev);
  }

  return { state: logFeedState, play, pause, skipForward, query };
}

/**
 * Log feed data fetcher component
 */

type LogFeedRecordFetcherProps = {
  node: LRNode;
  pod: LRPod;
  container: string;
  onLoad: (records: LogRecord[]) => void;
  onUpdate: (record: LogRecord) => void;
};

type LogFeedRecordFetcherHandle = {
  skipForward: () => Promise<LogRecord[]>;
  query: (opts: LogFeedQueryOptions) => Promise<LogRecord[]>;
};

const LogFeedDataFetcherImpl: React.ForwardRefRenderFunction<LogFeedRecordFetcherHandle, LogFeedRecordFetcherProps> = (props, ref) => {
  const { node, pod, container, onLoad, onUpdate } = props;
  const { namespace, name } = pod.metadata;
  const { logFeedState } = useContext(Context);
  const lastTSRef = useRef<string>();

  const upgradeRecord = (record: GraphQLLogRecord) => {
    return { ...record, node, pod, container };
  };

  // get logs
  const { loading, data, subscribeToMore, refetch } = useQuery(ops.QUERY_CONTAINER_LOG, {
    variables: { namespace, name, container },
    fetchPolicy: 'no-cache',
    skip: true,  // we'll use refetch() and subscribeToMmore() instead
    onCompleted: (data) => {
      if (!data?.podLogQuery) return;
      // execute callback
      onLoad(data.podLogQuery.map(record => upgradeRecord(record)));
    },
    onError: (err) => {
      console.log(err);
    },
  });

  // update lastTS
  if (!lastTSRef.current) lastTSRef.current = data?.podLogQuery?.length ? data.podLogQuery[data.podLogQuery.length - 1].timestamp : undefined;

  // tail
  useEffect(() => {
    // wait for initial query to complete
    if (!(loading === false)) return;

    // only execute when playing
    if (!(logFeedState === LogFeedState.Playing)) return;

    const variables = { namespace, name, container } as any;

    // implement `after`
    if (lastTSRef.current) variables.after = lastTSRef.current;
    else variables.since = 'NOW';

    return subscribeToMore({
      document: ops.TAIL_CONTAINER_LOG,
      variables: variables,
      updateQuery: (_, { subscriptionData }) => {
        const record = subscriptionData.data.podLogTail;
        if (record) {
          // update lastTS
          lastTSRef.current = record.timestamp;

          // execute callback
          onUpdate(upgradeRecord(record));
        }
        return { podLogQuery: [] };
      },
      onError: (err) => {
        console.log(err)
      },
    });
  }, [subscribeToMore, loading, logFeedState]);

  // define handler api
  useImperativeHandle(ref, () => ({
    skipForward: async () => {
      const variables = {} as any;
      if (lastTSRef.current) variables.after = lastTSRef.current;

      const result = await refetch(variables);
      if (!result.data.podLogQuery) return [];

      // upgrade records
      const records = result.data.podLogQuery.map(record => upgradeRecord(record));

      // update lastTS
      if (records.length) lastTSRef.current = records[records.length - 1].timestamp;

      // return records
      return records;
    },
    query: async (opts: LogFeedQueryOptions) => {
      const result = await refetch(opts);
      if (!result.data.podLogQuery) return [];

      // upgrade records
      const records = result.data.podLogQuery.map(record => upgradeRecord(record));

      // update lastTS
      if (!opts.until) {
        if (records.length) lastTSRef.current = records[records.length - 1].timestamp;
        else lastTSRef.current = undefined;
      }

      // return records
      return records;
    }
  }));

  return <></>;
};

const LogFeedDataFetcher = forwardRef(LogFeedDataFetcherImpl);

/**
 * Log feed loader component
 */

type LogFeedLoaderProps = {
  onRecord?: OnRecordCallbackFunction;
};

const LogFeedLoader = forwardRef((
  {
    onRecord,
  }: LogFeedLoaderProps,
  ref: React.Ref<HTMLDivElement | null>,
) => {
  const nodes = useNodes();
  const pods = usePods();
  const { setLogFeedState } = useContext(Context);
  const childRefs = useRef(new Array<React.RefObject<LogFeedRecordFetcherHandle>>());

  const wrapperElRef = useRef<HTMLDivElement>(null);
  useImperativeHandle(ref, () => wrapperElRef.current);

  // attach event listeners
  useEffect(() => {
    const wrapperEl = wrapperElRef.current;
    if (!wrapperEl) return;

    // play
    const playFn = () => setLogFeedState(LogFeedState.Playing);
    wrapperEl.addEventListener('play', playFn);

    // pause
    const pauseFn = () => setLogFeedState(LogFeedState.Paused);
    wrapperEl.addEventListener('pause', pauseFn);

    // skip-forward
    const skipForwardFn = async () => {
      const promises = Array<Promise<LogRecord[]>>();
      const records = Array<LogRecord>();

      // trigger skipForward in children
      childRefs.current.forEach(childRef => {
        childRef.current && promises.push(childRef.current.skipForward());
      });

      // gather and sort results
      (await Promise.all(promises)).forEach(result => records.push(...result));
      records.sort((a, b) => a.timestamp.localeCompare(b.timestamp));

      // execute callback
      onRecord && records.forEach(record => onRecord(record));
    };
    wrapperEl.addEventListener('skipForward', skipForwardFn);

    // query
    const queryFn = async (ev: Event) => {
      const opts = (ev as CustomEvent<LogFeedQueryOptions>).detail;

      if (opts.until) {
        setLogFeedState(LogFeedState.InQuery);
      } else {
        setLogFeedState(LogFeedState.Paused);
      }

      const promises = Array<Promise<LogRecord[]>>();
      const records = Array<LogRecord>();

      // trigger query in children
      childRefs.current.forEach(childRef => {
        childRef.current && promises.push(childRef.current.query(opts));
      });

      // gather and sort results
      (await Promise.all(promises)).forEach(result => records.push(...result));
      records.sort((a, b) => a.timestamp.localeCompare(b.timestamp));

      // execute callback
      onRecord && records.forEach(record => onRecord(record));
    };
    wrapperEl.addEventListener('query', queryFn);

    // cleanup
    return () => {
      wrapperEl.removeEventListener('play', playFn);
      wrapperEl.removeEventListener('pause', pauseFn);
      wrapperEl.removeEventListener('skipForward', skipForwardFn);
      wrapperEl.removeEventListener('query', queryFn);
    };
  }, []);

  // wait until resources are loaded
  if (nodes.fetching || pods.loading) return <div ref={wrapperElRef} />;

  const handleOnUpdate = (record: LogRecord) => {
    onRecord && onRecord(record);
  };

  // only load containers from nodes that we have a record of
  const nodeMap = new Map(nodes.items?.map(node => [node.metadata.name, node]));

  const els: JSX.Element[] = [];
  const refs: React.RefObject<LogFeedRecordFetcherHandle>[] = [];

  pods.pods.forEach(pod => {
    pod.status.containerStatuses.forEach(status => {
      const node = nodeMap.get(pod.spec.nodeName);
      if (status.started && node) {
        const k = `${pod.metadata.namespace}/${pod.metadata.name}/${status.name}`;

        const ref = createRef<LogFeedRecordFetcherHandle>();
        refs.push(ref);

        els.push(
          <LogFeedDataFetcher
            key={k}
            ref={ref}
            node={node}
            pod={pod}
            container={status.name}
            onLoad={(records) => console.log(records)}
            onUpdate={handleOnUpdate}
          />
        );
      }
    });
  });

  childRefs.current = refs;

  return (
    <div ref={wrapperElRef}>
      {els}
    </div>
  );
});

/**
 * Nodes hook
 */

export function useNodes() {
  const { loading, fetching, error, data } = useListQueryWithSubscription({
    query: ops.CONSOLE_NODES_LIST_FETCH,
    subscription: ops.CONSOLE_NODES_LIST_WATCH,
    queryDataKey: 'coreV1NodesList',
    subscriptionDataKey: 'coreV1NodesWatch',
  });

  const items = (data?.coreV1NodesList?.items) ? data.coreV1NodesList.items : undefined;

  return { loading, fetching, error, items };
}

/**
 * Workloads hook
 */

export function useWorkloads() {
  const { workloadMap } = useContext(Context);

  let loading = false;
  const workloads = new Map<Workload, LRWorkload[]>();

  // group sources by workload type
  workloadMap.forEach((val) => {
    loading = loading || val.loading;
    const item = val.item;
    if (!item?.__typename) return;
    const workload = typenameMap[item.__typename];
    const items = workloads.get(workload) || [];
    items.push(item);
    workloads.set(workload, items);
  });

  return { loading, workloads };
}

/**
 * Pods hook
 */

export function usePods() {
  const { podMap } = useContext(Context);

  let loading = false;
  const pods: LRPod[] = [];

  // uniquify
  const usedIDs = new Set<string>();
  podMap?.forEach((val) => {
    loading = loading || val.loading;
    val.items?.forEach(item => {
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
 * Provider component
 */

interface LoggingResourcesProviderProps extends React.PropsWithChildren {
  sourcePaths: string[];
  onRecord?: OnRecordCallbackFunction;
};

export const LoggingResourcesProvider = ({
  sourcePaths,
  onRecord,
  children,
}: LoggingResourcesProviderProps) => {
  const [workloadMap, setWorkloadMap] = useState<WorkloadMap>(new Map());
  const [podMap, setPodMap] = useState<PodMap>(new Map());
  const [logFeedState, setLogFeedState] = useState<LogFeedState>(LogFeedState.Playing);
  const logFeedLoaderRef = useRef<HTMLDivElement>(null);

  // uniquify sourcePaths
  sourcePaths = Array.from(new Set(sourcePaths || []));
  sourcePaths.sort();

  // handle sourcePath deletions
  useEffect(() => {
    const difference = Array.from(workloadMap.keys()).filter(x => !sourcePaths.includes(x));
    if (difference.length) {
      // update workload map
      difference.forEach(key => workloadMap.delete(key));
      setWorkloadMap(new Map(workloadMap));

      // update pod map
      difference.forEach(key => podMap.delete(key));
      setPodMap(new Map(podMap));
    }
  }, [JSON.stringify(sourcePaths)]);

  const resourceLoaders = {
    [Workload.CRONJOBS]: LoadCronJobWorkload,
    [Workload.DAEMONSETS]: LoadDaemonSetWorkload,
    [Workload.DEPLOYMENTS]: LoadDeploymentWorkload,
    [Workload.JOBS]: LoadJobWorkload,
    [Workload.PODS]: LoadPodWorkload,
    [Workload.REPLICASETS]: LoadReplicaSetWorkload,
    [Workload.STATEFULSETS]: LoadStatefulSetWorkload,
  };

  const contextValue = {
    workloadMap,
    setWorkloadMap,
    podMap,
    setPodMap,
    logFeedState,
    setLogFeedState,
    logFeedLoaderRef,
  };

  return (
    <Context.Provider value={contextValue}>
      {sourcePaths.map(path => {
        const parts = path.split('/');
        if (!(parts[0] in resourceLoaders)) throw new Error(`not implemented: ${parts[0]}`);
        const Component = resourceLoaders[parts[0] as Workload];
        return <Component key={path} sourcePath={path} />
      })}
      <LogFeedLoader
        ref={logFeedLoaderRef}
        onRecord={onRecord}
      />
      {children}
    </Context.Provider>
  );
};
