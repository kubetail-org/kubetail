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

// What one cluster's chats remember: its own memories and every cluster's, a collection
// watch folded into an id-keyed map. Stored records with no cache behind them, like
// the chats, so there is no provenance to guard.
import { useMemo } from 'react';

import { graphql } from '@/gql';
import type { MemoryCollectionWatchSubscription } from '@/gql/graphql';
import { collectionWatchReducer } from '@/lib/graphql/collection-watch';
import { useFoldedSubscription, subscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';

const MemoryCollectionWatchDocument = graphql(`
  subscription MemoryCollectionWatch($clusterID: ClusterID!) {
    memoryCollectionWatch(clusterID: $clusterID) {
      type
      memory {
        id
        clusterID
        name
        body
        writtenBy
        updatedAt
      }
    }
  }
`);

export type Memory = NonNullable<MemoryCollectionWatchSubscription['memoryCollectionWatch']['memory']>;

function clusterFirstByName(a: Memory, b: Memory): number {
  return Number(a.clusterID === null) - Number(b.clusterID === null) || a.name.localeCompare(b.name);
}

const reduceMemories = collectionWatchReducer(
  ({ memoryCollectionWatch: { type, memory } }: MemoryCollectionWatchSubscription) => ({ type, item: memory }),
);

/**
 * What `clusterID`'s chats see: its own memories, then every cluster's, each by
 * name — the order of the index the model reads. No watch without a cluster.
 */
export function useMemories(clusterID: string | undefined): {
  memories: Memory[];
  phase: SubscriptionPhase;
} {
  const { data, connected } = useFoldedSubscription(
    { query: MemoryCollectionWatchDocument, variables: { clusterID: clusterID ?? '' }, pause: !clusterID },
    reduceMemories,
  );

  const items = data?.items;
  const memories = useMemo(() => [...(items?.values() ?? [])].toSorted(clusterFirstByName), [items]);

  return { memories, phase: subscriptionPhase(!!data?.snapshotComplete, connected) };
}
