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

import { useApolloClient, useQuery } from '@apollo/client/react';
import type { TypedDocumentNode, OperationVariables } from '@apollo/client';
import { useCallback, useEffect, useRef } from 'react';

import appConfig from '@/app-config';
import { LOCAL_STORAGE_KEY } from '@/components/widgets/EnvironmentControl';
import {
  SOURCE_PICKER_CRONJOBS_COUNT_FETCH,
  SOURCE_PICKER_CRONJOBS_COUNT_WATCH,
  SOURCE_PICKER_DAEMONSETS_COUNT_FETCH,
  SOURCE_PICKER_DAEMONSETS_COUNT_WATCH,
  SOURCE_PICKER_DEPLOYMENTS_COUNT_FETCH,
  SOURCE_PICKER_DEPLOYMENTS_COUNT_WATCH,
  SOURCE_PICKER_JOBS_COUNT_FETCH,
  SOURCE_PICKER_JOBS_COUNT_WATCH,
  SOURCE_PICKER_PODS_COUNT_FETCH,
  SOURCE_PICKER_PODS_COUNT_WATCH,
  SOURCE_PICKER_REPLICASETS_COUNT_FETCH,
  SOURCE_PICKER_REPLICASETS_COUNT_WATCH,
  SOURCE_PICKER_STATEFULSETS_COUNT_FETCH,
  SOURCE_PICKER_STATEFULSETS_COUNT_WATCH,
} from '@/lib/graphql/dashboard/ops';
import { Counter } from './util';
import { WorkloadKind } from './workload';
import { Status, useClusterAPIServerStatus } from './server-status';

type GenericListFragment = {
  metadata: {
    continue: string;
    resourceVersion: string;
  };
  items: {
    metadata: {
      name: string;
      uid: string;
      resourceVersion: string;
      deletionTimestamp: null | string;
    };
  }[];
};

type GenericCounterFragment = {
  metadata: {
    remainingItemCount: bigint;
    resourceVersion: string;
  };
  items: {
    metadata: {
      resourceVersion: string;
    };
  }[];
};

type GenericWatchEventFragment = {
  type: string;
  object: any;
};

type CustomError = Error & {
  graphQLErrors?: {
    message: string;
    extensions: {
      code: string;
      reason: string;
      status: string;
    };
  }[];
};

const RETRY_TIMEOUT = 5000;

/**
 * Is watch expired error?
 */

function isWatchExpiredError(err: Error): boolean {
  const { graphQLErrors } = err as CustomError;
  if (graphQLErrors && graphQLErrors.length) {
    const gqlErr = graphQLErrors[0];
    return gqlErr.extensions?.code === 'KUBETAIL_WATCH_ERROR' && gqlErr.extensions?.reason === 'Expired';
  }
  return false;
}

/**
 * Runs queued callbacks on the next reactive cycle
 * (commit → paint), then clears the queue.
 *
 * @returns schedule – call with a callback to run next tick.
 *
 * Usage:
 * const nextTick = useNextTick();
 * nextTick(() => console.log('I run after the next render + paint'));
 */

export function useNextTick(): (fn: () => void) => void {
  const queueRef = useRef<(() => void)[]>([]);

  /** Enqueue a callback for the next tick */
  const schedule = useCallback((fn: () => void) => {
    queueRef.current.push(fn);
  }, []);

  // Flush the queue on every commit; actual execution is deferred
  // to the next paint using requestAnimationFrame.
  useEffect(() => {
    if (queueRef.current.length === 0) return;

    const id = requestAnimationFrame(() => {
      const queued = queueRef.current.splice(0);
      queued.forEach((cb) => {
        try {
          cb();
        } catch (err) {
          // Surface errors so they are not swallowed
          setTimeout(() => {
            throw err;
          });
        }
      });
    });

    // If the component unmounts before RAF fires, cancel it.
    return () => cancelAnimationFrame(id);
  });

  return schedule;
}

/**
 * Throttles a callback so it runs at most once per animation frame.
 * Cancels any pending frame on unmount to avoid orphan callbacks.
 */

export function useRafThrottle<T extends (...args: any[]) => void>(fn: T) {
  const frameRef = useRef<number | null>(null);

  const throttled = useCallback(
    (...args: Parameters<T>) => {
      if (frameRef.current) return;
      frameRef.current = requestAnimationFrame(() => {
        fn(...args);
        frameRef.current = null;
      });
    },
    [fn],
  );

  useEffect(
    () => () => {
      if (frameRef.current) cancelAnimationFrame(frameRef.current);
    },
    [],
  );

  return throttled;
}

/**
 * Get-style query with subscription hook
 */

interface GetQueryWithSubscriptionTQVariables {
  kubeContext?: string;
  namespace: string;
  name: string;
}

interface GetQueryWithSubscriptionTSVariables {
  kubeContext?: string;
  namespace: string;
  fieldSelector: string;
}

interface GetQueryWithSubscriptionArgs<TQData, TQVariables, TSData, TSVariables> {
  query: TypedDocumentNode<TQData, TQVariables>;
  subscription: TypedDocumentNode<TSData, TSVariables>;
  queryDataKey: keyof TQData;
  subscriptionDataKey: keyof TSData;
  skip?: boolean;
  variables: TQVariables;
}

export function useGetQueryWithSubscription<
  TQData = any,
  TQVariables extends GetQueryWithSubscriptionTQVariables = GetQueryWithSubscriptionTQVariables,
  TSData = any,
  TSVariables extends GetQueryWithSubscriptionTSVariables = GetQueryWithSubscriptionTSVariables,
>(args: GetQueryWithSubscriptionArgs<TQData, TQVariables, TSData, TSVariables>) {
  const { kubeContext, name, namespace } = args.variables;

  // get workload object
  const { loading, error, data, subscribeToMore, refetch, startPolling, stopPolling } = useQuery(args.query, {
    skip: args.skip,
    variables: args.variables,
  });

  // Retry on errors
  useEffect(() => {
    if (error) startPolling(RETRY_TIMEOUT);
    else stopPolling();
  }, [error, startPolling, stopPolling]);

  // subscribe to changes
  useEffect(
    () =>
      subscribeToMore({
        document: args.subscription,
        variables: {
          kubeContext,
          namespace,
          fieldSelector: `metadata.name=${name}`,
        } as TSVariables,
        updateQuery: (prev, { subscriptionData }): TQData => {
          const ev = subscriptionData.data[args.subscriptionDataKey] as GenericWatchEventFragment;
          if (ev?.type === 'ADDED' && ev.object) return { [args.queryDataKey]: ev.object } as TQData;
          return prev as TQData;
        },
        onError: (err) => {
          if (isWatchExpiredError(err)) refetch();
        },
      }),
    [subscribeToMore],
  );

  return { loading, error, data };
}

/**
 * List-style query with subscription hook
 */

interface ListQueryWithSubscriptionArgs<TQData, TQVariables, TSData, TSVariables> {
  query: TypedDocumentNode<TQData, TQVariables>;
  subscription: TypedDocumentNode<TSData, TSVariables>;
  queryDataKey: keyof TQData;
  subscriptionDataKey: keyof TSData;
  skip?: boolean;
  variables?: TQVariables;
}

export function useListQueryWithSubscription<
  TQData = any,
  TQVariables extends OperationVariables = OperationVariables,
  TSData = any,
  TSVariables extends OperationVariables = OperationVariables,
>(args: ListQueryWithSubscriptionArgs<TQData, TQVariables, TSData, TSVariables>) {
  const client = useApolloClient();

  // initial query
  const { loading, error, data, fetchMore, subscribeToMore, refetch, startPolling, stopPolling } = useQuery(
    args.query,
    {
      skip: args.skip,
      variables: args.variables as TQVariables,
    },
  );

  // Retry on errors
  useEffect(() => {
    if (error) startPolling(RETRY_TIMEOUT);
    else stopPolling();
  }, [error, startPolling, stopPolling]);

  // TODO: tighten `any`
  const respData = data ? (data[args.queryDataKey] as GenericListFragment) : null;

  // fetch rest
  const fetchMoreRef = useRef(new Set<string>([]));
  const continueVal = respData?.metadata.continue;
  useEffect(() => {
    if (continueVal && !fetchMoreRef.current.has(continueVal)) {
      fetchMoreRef.current.add(continueVal);
      fetchMore({
        variables: {
          ...args.variables,
          continue: continueVal,
        } as unknown as Partial<TQVariables>,
      });
    }
  }, [continueVal]);

  // subscribe to changes
  useEffect(() => {
    // wait for all data to get fetched
    if (args.skip || loading || continueVal) return;

    const resourceVersion = respData?.metadata.resourceVersion || '';

    // add `resourceVersion`
    const variables = {
      ...args.variables,
      resourceVersion,
    } as unknown as TSVariables;

    return subscribeToMore({
      document: args.subscription,
      variables,
      updateQuery: (prev, { subscriptionData }): TQData => {
        const ev = subscriptionData.data[args.subscriptionDataKey] as GenericWatchEventFragment;

        if (!ev?.type || !ev?.object) return prev as TQData;
        if (!(prev as TQData)[args.queryDataKey]) return prev as TQData;

        const oldResult = (prev as TQData)[args.queryDataKey] as GenericListFragment;

        // initialize new result and update resourceVersion
        const newResult = {
          ...oldResult,
          metadata: {
            ...oldResult.metadata,
            resourceVersion: ev.object.metadata.resourceVersion,
          },
        };

        switch (ev.type) {
          case 'ADDED':
            // add and re-sort item if not already in list
            if (!newResult.items.some((item) => item.metadata.uid === ev.object.metadata.uid)) {
              const items = Array.from(newResult.items);
              items.push(ev.object);
              items.sort((a, b) => {
                if (!a.metadata.name) return 1;
                if (!b.metadata.name) return -1;
                return a.metadata.name.localeCompare(b.metadata.name);
              });
              newResult.items = items;
            }
            break;
          case 'MODIFIED':
            break;
          case 'DELETED':
            // handle forced deletions that don't set `deletionTimestamp`
            if (ev.object.metadata.deletionTimestamp === null) {
              client.cache.modify({
                id: client.cache.identify(ev.object),
                fields: {
                  metadata: (currMetadata) => ({
                    ...currMetadata,
                    deletionTimestamp: new Date().toISOString(),
                  }),
                },
              });
            }

            // remove deleted item
            newResult.items = oldResult.items.filter((item) => item.metadata.uid !== ev.object.metadata.uid);
            break;
          default:
            return prev as TQData;
        }

        return { [args.queryDataKey]: newResult } as TQData;
      },
      onError: (err) => {
        if (isWatchExpiredError(err)) refetch();
      },
    });
  }, [args.skip, subscribeToMore, loading, continueVal]);

  const fetching = Boolean(loading || continueVal);

  return {
    loading,
    fetching,
    error,
    data,
  };
}

/**
 * Counter-style query with subscription hook
 */

interface CounterQueryWithSubscriptionArgs<TQData, TQVariables, TSData, TSVariables> {
  query: TypedDocumentNode<TQData, TQVariables>;
  subscription: TypedDocumentNode<TSData, TSVariables>;
  queryDataKey: keyof TQData;
  subscriptionDataKey: keyof TSData;
  skip?: boolean;
  variables?: TQVariables;
}

export function useCounterQueryWithSubscription<
  TQData = any,
  TQVariables extends OperationVariables = OperationVariables,
  TSData = any,
  TSVariables extends OperationVariables = OperationVariables,
>(args: CounterQueryWithSubscriptionArgs<TQData, TQVariables, TSData, TSVariables>) {
  // initial query
  const { loading, error, data, subscribeToMore, refetch, startPolling, stopPolling } = useQuery(args.query, {
    skip: args.skip,
    variables: args.variables as TQVariables,
  });

  // Retry on error
  useEffect(() => {
    if (error) startPolling(RETRY_TIMEOUT);
    else stopPolling();
  }, [error, startPolling, stopPolling]);

  const respData = data ? (data[args.queryDataKey] as GenericCounterFragment) : null;

  // subscribe to changes
  useEffect(() => {
    // wait for all data to get fetched
    if (loading || error) return;

    const resourceVersion = respData?.metadata.resourceVersion || '';

    // add `resourceVersion`
    const variables = {
      ...args.variables,
      resourceVersion,
    } as unknown as TSVariables;

    return subscribeToMore({
      document: args.subscription,
      variables,
      updateQuery: (prev, { subscriptionData }): TQData => {
        const ev = subscriptionData.data[args.subscriptionDataKey] as GenericWatchEventFragment;

        if (!ev?.type || !ev?.object) return prev as TQData;
        if (!(prev as TQData)[args.queryDataKey]) return prev as TQData;

        // Only handle additions and deletions
        if (!['ADDED', 'DELETED'].includes(ev.type)) return prev as TQData;

        const oldResult = (prev as TQData)[args.queryDataKey] as GenericCounterFragment;

        const oldCount = oldResult.metadata.remainingItemCount;
        const newCount = oldCount + (ev.type === 'ADDED' ? BigInt(1) : BigInt(-1));

        // Initialize new result and update resourceVersion
        const newResult = {
          ...oldResult,
          metadata: {
            ...oldResult.metadata,
            resourceVersion: ev.object.metadata.resourceVersion,
            remainingItemCount: newCount,
          },
        };

        return { [args.queryDataKey]: newResult } as TQData;
      },
      onError: (err) => {
        if (isWatchExpiredError(err)) refetch();
      },
    });
  }, [subscribeToMore, loading, error]);

  let count: number | undefined;
  if (respData) count = respData.items.length + Number(respData.metadata.remainingItemCount);

  return {
    loading,
    error,
    count,
  };
}

/**
 * ClusterAPIEnabled hook
 */

export function useIsClusterAPIEnabled(kubeContext: string | null) {
  const status = useClusterAPIServerStatus(kubeContext || '');

  if (import.meta.env.MODE === 'development') {
    const overrideValue = localStorage.getItem(LOCAL_STORAGE_KEY);
    if (overrideValue !== null) return Boolean(JSON.parse(overrideValue));
  }

  // Return if running in cluster with ClusterAPI enabled
  if (appConfig.environment === 'cluster') {
    return appConfig.clusterAPIEnabled;
  }

  switch (status.status) {
    case Status.NotFound:
      return false;
    case Status.Unknown:
    case Status.Pending:
      return undefined;
    default:
      return true;
  }
}

/**
 * Workload counter hook
 */

export function useWorkloadCounter(kubeContext: string, namespace = '') {
  const cronjobs = useCounterQueryWithSubscription({
    query: SOURCE_PICKER_CRONJOBS_COUNT_FETCH,
    subscription: SOURCE_PICKER_CRONJOBS_COUNT_WATCH,
    queryDataKey: 'batchV1CronJobsList',
    subscriptionDataKey: 'batchV1CronJobsWatch',
    variables: { kubeContext, namespace },
  });

  const daemonsets = useCounterQueryWithSubscription({
    query: SOURCE_PICKER_DAEMONSETS_COUNT_FETCH,
    subscription: SOURCE_PICKER_DAEMONSETS_COUNT_WATCH,
    queryDataKey: 'appsV1DaemonSetsList',
    subscriptionDataKey: 'appsV1DaemonSetsWatch',
    variables: { kubeContext, namespace },
  });

  const deployments = useCounterQueryWithSubscription({
    query: SOURCE_PICKER_DEPLOYMENTS_COUNT_FETCH,
    subscription: SOURCE_PICKER_DEPLOYMENTS_COUNT_WATCH,
    queryDataKey: 'appsV1DeploymentsList',
    subscriptionDataKey: 'appsV1DeploymentsWatch',
    variables: { kubeContext, namespace },
  });

  const jobs = useCounterQueryWithSubscription({
    query: SOURCE_PICKER_JOBS_COUNT_FETCH,
    subscription: SOURCE_PICKER_JOBS_COUNT_WATCH,
    queryDataKey: 'batchV1JobsList',
    subscriptionDataKey: 'batchV1JobsWatch',
    variables: { kubeContext, namespace },
  });

  const pods = useCounterQueryWithSubscription({
    query: SOURCE_PICKER_PODS_COUNT_FETCH,
    subscription: SOURCE_PICKER_PODS_COUNT_WATCH,
    queryDataKey: 'coreV1PodsList',
    subscriptionDataKey: 'coreV1PodsWatch',
    variables: { kubeContext, namespace },
  });

  const replicasets = useCounterQueryWithSubscription({
    query: SOURCE_PICKER_REPLICASETS_COUNT_FETCH,
    subscription: SOURCE_PICKER_REPLICASETS_COUNT_WATCH,
    queryDataKey: 'appsV1ReplicaSetsList',
    subscriptionDataKey: 'appsV1ReplicaSetsWatch',
    variables: { kubeContext, namespace },
  });

  const statefulsets = useCounterQueryWithSubscription({
    query: SOURCE_PICKER_STATEFULSETS_COUNT_FETCH,
    subscription: SOURCE_PICKER_STATEFULSETS_COUNT_WATCH,
    queryDataKey: 'appsV1StatefulSetsList',
    subscriptionDataKey: 'appsV1StatefulSetsWatch',
    variables: { kubeContext, namespace },
  });

  const reqs = [cronjobs, daemonsets, deployments, jobs, pods, replicasets, statefulsets];
  const loading = reqs.some((req) => req.loading);
  const error = reqs.find((req) => Boolean(req.error));

  const counter = new Counter<WorkloadKind>();

  function updateCounter(key: WorkloadKind, count: number | undefined) {
    if (count !== undefined) counter.set(key, count);
  }

  if (!loading && !error) {
    updateCounter(WorkloadKind.CRONJOBS, cronjobs.count);
    updateCounter(WorkloadKind.DAEMONSETS, daemonsets.count);
    updateCounter(WorkloadKind.DEPLOYMENTS, deployments.count);
    updateCounter(WorkloadKind.JOBS, jobs.count);
    updateCounter(WorkloadKind.PODS, pods.count);
    updateCounter(WorkloadKind.REPLICASETS, replicasets.count);
    updateCounter(WorkloadKind.STATEFULSETS, statefulsets.count);
  }

  return { loading, error, counter };
}
