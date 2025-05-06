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

import type {
  HomeCronJobsListItemFragmentFragment,
  HomeDaemonSetsListItemFragmentFragment,
  HomePodsListItemFragmentFragment,
  HomeJobsListItemFragmentFragment,
  HomeDeploymentsListItemFragmentFragment,
  HomeReplicaSetsListItemFragmentFragment,
  HomeStatefulSetsListItemFragmentFragment,
} from '@/lib/graphql/dashboard/__generated__/graphql';

export type WorkloadItem = HomeCronJobsListItemFragmentFragment | HomeJobsListItemFragmentFragment | HomeDeploymentsListItemFragmentFragment | HomePodsListItemFragmentFragment | HomeDaemonSetsListItemFragmentFragment | HomeReplicaSetsListItemFragmentFragment | HomeStatefulSetsListItemFragmentFragment;

/**
 * gets all the leaf node IDs of parent node
 */

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
