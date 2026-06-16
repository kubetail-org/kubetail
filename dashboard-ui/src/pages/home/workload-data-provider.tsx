// Copyright 2024 The Kubetail Authors
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

import type { TypedDocumentNode } from '@apollo/client';
import { useSubscription } from '@apollo/client/react';
import { useSetAtom } from 'jotai';
import { useEffect } from 'react';

import type {
  HomeCronJobsListFetchQuery,
  HomeDaemonSetsListFetchQuery,
  HomeDeploymentsListFetchQuery,
  HomeJobsListFetchQuery,
  HomePodsListFetchQuery,
  HomeReplicaSetsListFetchQuery,
  HomeStatefulSetsListFetchQuery,
} from '@/lib/graphql/dashboard/__generated__/graphql';
import {
  HOME_CRONJOBS_LIST_FETCH,
  HOME_CRONJOBS_LIST_WATCH,
  HOME_DAEMONSETS_LIST_FETCH,
  HOME_DAEMONSETS_LIST_WATCH,
  HOME_DEPLOYMENTS_LIST_FETCH,
  HOME_DEPLOYMENTS_LIST_WATCH,
  HOME_JOBS_LIST_FETCH,
  HOME_JOBS_LIST_WATCH,
  HOME_PODS_LIST_FETCH,
  HOME_PODS_LIST_WATCH,
  HOME_REPLICASETS_LIST_FETCH,
  HOME_REPLICASETS_LIST_WATCH,
  HOME_STATEFULSETS_LIST_FETCH,
  HOME_STATEFULSETS_LIST_WATCH,
  KUBERNETES_API_READY_WAIT,
} from '@/lib/graphql/dashboard/ops';
import { useListQueryWithSubscription } from '@/lib/hooks';
import { WorkloadKind, ALL_WORKLOAD_KINDS } from '@/lib/workload';

import type { KubeContext, WorkloadItem } from './shared';
import { workloadQueryAtomFamilies } from './state';

/**
 * WorkloadDataFetcher component
 */

// Each workload kind selects a different top-level field, so the per-kind query
// result types are mutually incompatible. This shared entry type lets a single
// dynamic dispatch site consume any of them without a union over documents.
type WorkloadQueryConfigEntry = {
  query: TypedDocumentNode<any, any>;
  subscription: TypedDocumentNode<any, any>;
  queryDataKey: string;
  subscriptionDataKey: string;
  getItems: (data: any) => WorkloadItem[] | undefined;
};

const workloadQueryConfig: Record<WorkloadKind, WorkloadQueryConfigEntry> = {
  [WorkloadKind.CRONJOBS]: {
    query: HOME_CRONJOBS_LIST_FETCH,
    subscription: HOME_CRONJOBS_LIST_WATCH,
    queryDataKey: 'batchV1CronJobsList',
    subscriptionDataKey: 'batchV1CronJobsWatch',
    getItems: (data: HomeCronJobsListFetchQuery) => data?.batchV1CronJobsList?.items,
  },
  [WorkloadKind.DAEMONSETS]: {
    query: HOME_DAEMONSETS_LIST_FETCH,
    subscription: HOME_DAEMONSETS_LIST_WATCH,
    queryDataKey: 'appsV1DaemonSetsList',
    subscriptionDataKey: 'appsV1DaemonSetsWatch',
    getItems: (data: HomeDaemonSetsListFetchQuery) => data?.appsV1DaemonSetsList?.items,
  },
  [WorkloadKind.DEPLOYMENTS]: {
    query: HOME_DEPLOYMENTS_LIST_FETCH,
    subscription: HOME_DEPLOYMENTS_LIST_WATCH,
    queryDataKey: 'appsV1DeploymentsList',
    subscriptionDataKey: 'appsV1DeploymentsWatch',
    getItems: (data: HomeDeploymentsListFetchQuery) => data?.appsV1DeploymentsList?.items,
  },
  [WorkloadKind.JOBS]: {
    query: HOME_JOBS_LIST_FETCH,
    subscription: HOME_JOBS_LIST_WATCH,
    queryDataKey: 'batchV1JobsList',
    subscriptionDataKey: 'batchV1JobsWatch',
    getItems: (data: HomeJobsListFetchQuery) => data?.batchV1JobsList?.items,
  },
  [WorkloadKind.PODS]: {
    query: HOME_PODS_LIST_FETCH,
    subscription: HOME_PODS_LIST_WATCH,
    queryDataKey: 'coreV1PodsList',
    subscriptionDataKey: 'coreV1PodsWatch',
    getItems: (data: HomePodsListFetchQuery) => data?.coreV1PodsList?.items,
  },
  [WorkloadKind.REPLICASETS]: {
    query: HOME_REPLICASETS_LIST_FETCH,
    subscription: HOME_REPLICASETS_LIST_WATCH,
    queryDataKey: 'appsV1ReplicaSetsList',
    subscriptionDataKey: 'appsV1ReplicaSetsWatch',
    getItems: (data: HomeReplicaSetsListFetchQuery) => data?.appsV1ReplicaSetsList?.items,
  },
  [WorkloadKind.STATEFULSETS]: {
    query: HOME_STATEFULSETS_LIST_FETCH,
    subscription: HOME_STATEFULSETS_LIST_WATCH,
    queryDataKey: 'appsV1StatefulSetsList',
    subscriptionDataKey: 'appsV1StatefulSetsWatch',
    getItems: (data: HomeStatefulSetsListFetchQuery) => data?.appsV1StatefulSetsList?.items,
  },
};

type WorkloadDataFetcherProps = {
  kind: WorkloadKind;
  kubeContext: KubeContext;
};

const WorkloadDataFetcher = ({ kind, kubeContext }: WorkloadDataFetcherProps) => {
  const setAtom = useSetAtom(workloadQueryAtomFamilies[kind](kubeContext));

  const readyWait = useSubscription(KUBERNETES_API_READY_WAIT, {
    skip: kubeContext === null,
    variables: { kubeContext },
  });

  const isReady = readyWait.data?.kubernetesAPIReadyWait ?? false;

  const cfg = workloadQueryConfig[kind];
  const { loading, fetching, data, error } = useListQueryWithSubscription({
    skip: !isReady,
    query: cfg.query,
    subscription: cfg.subscription,
    queryDataKey: cfg.queryDataKey,
    subscriptionDataKey: cfg.subscriptionDataKey,
    variables: { kubeContext },
  });

  // Update data atom
  useEffect(() => {
    setAtom({
      loading,
      fetching,
      error,
      items: (data && cfg.getItems(data)) ?? [],
    });
  }, [loading, fetching, error, data, setAtom]);

  return null;
};

/**
 * WorkloadDataProvider component
 */

type WorkloadDataProviderProps = {
  kubeContext: KubeContext;
};

export const WorkloadDataProvider = ({ kubeContext }: WorkloadDataProviderProps) => (
  <>
    {ALL_WORKLOAD_KINDS.map((kind) => (
      <WorkloadDataFetcher key={kind} kind={kind} kubeContext={kubeContext} />
    ))}
  </>
);
