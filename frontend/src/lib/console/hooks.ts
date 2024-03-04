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

import { useRecoilValue } from 'recoil';

import { useListQueryWithSubscription } from '@/lib/hooks';
import * as ops from '@/lib/graphql/ops';
import { Workload as WorkloadType, typenameMap } from '@/lib/workload';

import {
  sourceToWorkloadResponseMapState,
  sourceToPodListResponseMapState,
} from './state';
import { Node, Pod, Workload } from './types';

/**
 * Nodes hook
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

/**
 * Workloads hook
 */

export function useWorkloads() {
  const sourceToWorkloadResponseMap = useRecoilValue(sourceToWorkloadResponseMapState);

  let loading = false;
  const workloads = new Map<WorkloadType, Workload[]>();

  // group sources by workload type
  sourceToWorkloadResponseMap.forEach((val) => {
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
 * Pods hoook
 */

export function usePods() {
  const sourceToPodListResponseMap = useRecoilValue(sourceToPodListResponseMapState);

  let loading = false;
  const pods: Pod[] = [];

  // uniquify
  const usedIDs = new Set<string>();
  sourceToPodListResponseMap.forEach((val) => {
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
