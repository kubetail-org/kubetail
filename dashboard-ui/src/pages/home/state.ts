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

import type { ApolloError } from '@apollo/client';
import fastDeepEqualES6 from 'fast-deep-equal/es6';
import { atom } from 'jotai';
import { atomFamily, selectAtom } from 'jotai/utils';

import { HomePodsListItemFragmentFragment } from '@/lib/graphql/dashboard/__generated__/graphql';
import { WorkloadKind, ALL_WORKLOAD_KINDS } from '@/lib/workload';

import type { FileInfo, KubeContext, WorkloadItem } from './shared';

/**
 * UI state
 */

export const namespaceFilterAtom = atom('');

export const searchQueryAtom = atom('');

/**
 * Workload query state
 */

type WorkloadQueryResponse = {
  loading: boolean;
  fetching: boolean;
  items: WorkloadItem[] | undefined;
  error: ApolloError | undefined;
};

function makeWorkloadQueryAtomFamily() {
  return atomFamily((_kubeContext: KubeContext) =>
    atom<WorkloadQueryResponse>({
      loading: true,
      fetching: true,
      items: undefined,
      error: undefined,
    }),
  );
}

export const workloadQueryAtomFamilies = Object.fromEntries(
  ALL_WORKLOAD_KINDS.map((kind) => [kind, makeWorkloadQueryAtomFamily()]),
) as Record<WorkloadKind, ReturnType<typeof makeWorkloadQueryAtomFamily>>;

/**
 * Per-workload query isLoading state
 */

function makeWorkloadIsLoadingAtomFamily(kind: WorkloadKind) {
  return atomFamily((kubeContext: KubeContext) =>
    selectAtom(workloadQueryAtomFamilies[kind](kubeContext), (s) => s.loading),
  );
}

export const workloadIsLoadingAtomFamilies = Object.fromEntries(
  ALL_WORKLOAD_KINDS.map((kind) => [kind, makeWorkloadIsLoadingAtomFamily(kind)]),
) as Record<WorkloadKind, ReturnType<typeof makeWorkloadIsLoadingAtomFamily>>;

/**
 * Cross-workload query isLoading state
 */

export const isLoadingAtomFamily = atomFamily((kubeContext: KubeContext) =>
  atom((get) => Object.values(workloadIsLoadingAtomFamilies).some((family) => get(family(kubeContext)))),
);

/**
 * Per-workload query isFetching state
 */

function makeWorkloadIsFetchingAtomFamily(kind: WorkloadKind) {
  return atomFamily((kubeContext: KubeContext) =>
    selectAtom(workloadQueryAtomFamilies[kind](kubeContext), (s) => s.fetching),
  );
}

export const workloadIsFetchingAtomFamilies = Object.fromEntries(
  ALL_WORKLOAD_KINDS.map((kind) => [kind, makeWorkloadIsFetchingAtomFamily(kind)]),
) as Record<WorkloadKind, ReturnType<typeof makeWorkloadIsFetchingAtomFamily>>;

/**
 * Stable workload items state
 */

function makeStableWorkloadItemsAtomFamily(kind: WorkloadKind) {
  return atomFamily((kubeContext: KubeContext) =>
    selectAtom(
      workloadQueryAtomFamilies[kind](kubeContext),
      (s) => s.items ?? [],
      (itemsA, itemsB) => {
        // Check equality
        if (Object.is(itemsA, itemsB)) return true;

        // Check length
        if (itemsA.length !== itemsB.length) return false;

        // Compare item data
        for (let i = 0; i < itemsA.length; i += 1) {
          const mda = itemsA[i].metadata;
          const mdb = itemsB[i].metadata;

          // Check uid
          if (mda.uid !== mdb.uid) return false;

          // Check deletionTimestamp
          if (mda.deletionTimestamp !== mdb.deletionTimestamp) return false;
        }

        return true;
      },
    ),
  );
}

const stableWorkloadItemsAtomFamilies = Object.fromEntries(
  ALL_WORKLOAD_KINDS.map((kind) => [kind, makeStableWorkloadItemsAtomFamily(kind)]),
) as Record<WorkloadKind, ReturnType<typeof makeStableWorkloadItemsAtomFamily>>;

/**
 * Filtered workload items state
 */

function makeFilteredWorkloadItemsAtomFamily(kind: WorkloadKind) {
  return atomFamily((kubeContext: KubeContext) =>
    atom((get) => {
      const searchQuery = get(searchQueryAtom);
      const namespaceFilter = get(namespaceFilterAtom);
      const items = get(stableWorkloadItemsAtomFamilies[kind](kubeContext));

      // Return filtered items
      return items.filter((item) => {
        // Remove deleted items
        if (item.metadata.deletionTimestamp) return false;

        // Apply namespace filter
        if (namespaceFilter !== '' && item.metadata.namespace !== namespaceFilter) return false;

        // Apply search filter
        if (searchQuery !== '' && !item.metadata.name.toLowerCase().includes(searchQuery.toLowerCase())) return false;

        return true;
      });
    }),
  );
}

export const filteredWorkloadItemsAtomFamilies = Object.fromEntries(
  ALL_WORKLOAD_KINDS.map((kind) => [kind, makeFilteredWorkloadItemsAtomFamily(kind)]),
) as Record<WorkloadKind, ReturnType<typeof makeFilteredWorkloadItemsAtomFamily>>;

/**
 * Per-workload filtered count state
 */

function makeFilteredWorkloadCountAtomFamily(kind: WorkloadKind) {
  return atomFamily((kubeContext: KubeContext) =>
    selectAtom(filteredWorkloadItemsAtomFamilies[kind](kubeContext), (s) => s.length ?? 0),
  );
}

export const filteredWorkloadCountAtomFamilies = Object.fromEntries(
  ALL_WORKLOAD_KINDS.map((kind) => [kind, makeFilteredWorkloadCountAtomFamily(kind)]),
) as Record<WorkloadKind, ReturnType<typeof makeFilteredWorkloadCountAtomFamily>>;

/**
 * Cross-workload filtered count state
 */

export const filteredTotalCountAtomFamily = atomFamily((kubeContext: KubeContext) =>
  atom((get) =>
    Object.values(filteredWorkloadCountAtomFamilies).reduce((total, family) => total + get(family(kubeContext)), 0),
  ),
);

/**
 * Ownership map
 */

export const ownershipMapAtomFamily = atomFamily((kubeContext: KubeContext) =>
  atom((get) => {
    const m = new Map<string, string[]>();

    ALL_WORKLOAD_KINDS.forEach((kind) => {
      const items = get(stableWorkloadItemsAtomFamilies[kind](kubeContext));

      items.forEach((item) => {
        const itemID = item.metadata.uid;

        // Update parent-child relationships
        item.metadata.ownerReferences.forEach((ref) => {
          const parentID = ref.uid;
          const childrenIDs = m.get(parentID) ?? [];
          childrenIDs.push(itemID);
          m.set(parentID, childrenIDs);
        });

        // Add container ids from pods
        if (kind === WorkloadKind.PODS) {
          const pod = item as HomePodsListItemFragmentFragment;
          // strip out prefix (e.g. "containerd://")
          const containerIDs = pod.status.containerStatuses.map(
            (status) => status.containerID.split('://')[1] ?? status.containerID,
          );
          m.set(itemID, containerIDs);
        }
      });
    });

    return m;
  }),
);

/**
 * LogMetadataMap state
 */

export const logMetadataMapAtomFamily = atomFamily((_kubeContext: KubeContext) =>
  atom({ inner: new Map<string, FileInfo>() }),
);
