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

// The active cluster's cached objects of one kind as a live table feed.
// `clusterCachedDataObjectCollectionWatch` is keyed by the active cache plus the kind
// (apiVersion + resource); each object carries its typed universal identity plus
// `rawJSON`, the full native body (kind-specific columns derive from it — see
// ObjectsTable). Provenance is the FULL triple cacheID + apiVersion + resource —
// this watch is keyed by kind too, so cacheID alone would let the previous kind's
// stragglers leak across a resource switch within one cache.
import { useMemo } from 'react';

import { graphql } from '@/gql';
import type { ClusterCachedDataObjectCollectionWatchSubscription } from '@/gql/graphql';
import { useActiveCluster } from '@/lib/active-cluster';
import { useCacheCollectionWatch, joinProvenance } from '@/lib/graphql/use-cache-collection-watch';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import { gvrKey } from '@/lib/gvr';
import type { GVR } from '@/lib/gvr';

const ClusterCachedDataObjectCollectionWatchDocument = graphql(`
  subscription ClusterCachedDataObjectCollectionWatch(
    $id: ClusterID!
    $cacheID: ObjectID!
    $apiVersion: String!
    $resource: String!
  ) {
    clusterCachedDataObjectCollectionWatch(id: $id, cacheID: $cacheID, apiVersion: $apiVersion, resource: $resource) {
      type
      cacheID
      apiVersion
      resource
      object {
        uid
        apiVersion
        kind
        namespace
        name
        creationTimestamp
        rawJSON
      }
    }
  }
`);

// One cached object (the `object` payload of a change), as the table renders a row from.
export type ClusterCachedDataObject = NonNullable<
  ClusterCachedDataObjectCollectionWatchSubscription['clusterCachedDataObjectCollectionWatch']['object']
>;

// Plain code-unit order. Namespaces and names are DNS labels (lowercase ASCII, digits,
// `-`, `.`), for which this is the collated order too, and it is the order kubectl
// prints — at a fraction of a collator compare, which the list pays N log N times per flush.
function byNamespaceThenName(a: ClusterCachedDataObject, b: ClusterCachedDataObject): number {
  if (a.namespace !== b.namespace) return a.namespace < b.namespace ? -1 : 1;
  if (a.name === b.name) return 0;
  return a.name < b.name ? -1 : 1;
}

// The active context's cached objects of `kind`, live. Paused (empty) without an
// active cache, or while not `enabled`. Provenance must carry the full cacheID +
// apiVersion + resource key (see file header). `unshared` is for a watch that can mount
// into a running one of the same kind, which would never see its snapshot.
export function useClusterCachedDataObjects(
  kind: GVR,
  { enabled = true, unshared = false }: { enabled?: boolean; unshared?: boolean } = {},
): {
  objects: ClusterCachedDataObject[];
  active: boolean;
  phase: SubscriptionPhase;
} {
  const { clusterID, cacheID, active: cacheActive } = useActiveCluster();
  const active = enabled && cacheActive;

  const { items, phase } = useCacheCollectionWatch<
    ClusterCachedDataObjectCollectionWatchSubscription,
    ClusterCachedDataObject
  >(
    {
      query: ClusterCachedDataObjectCollectionWatchDocument,
      variables: { id: clusterID ?? '', cacheID: cacheID ?? '', apiVersion: kind.apiVersion, resource: kind.resource },
      pause: !active,
    },
    {
      select: (d) => {
        const f = d.clusterCachedDataObjectCollectionWatch;
        return { type: f.type, item: f.object, provenance: joinProvenance(f.cacheID, gvrKey(f)) };
      },
      keyOf: (o) => o.uid,
      currentProvenance: joinProvenance(cacheID ?? '', gvrKey(kind)),
      unshared,
    },
  );

  // Sort by (namespace, name) so the table order is stable across delta churn.
  const objects = useMemo(() => items.toSorted(byNamespaceThenName), [items]);

  return { objects, active, phase };
}
