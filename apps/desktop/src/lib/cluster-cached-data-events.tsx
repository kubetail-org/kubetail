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

// The active cluster's cached Kubernetes Events (the synced `events.k8s.io`/core Event
// rows, not kubetail's own control-plane `Event` timeline) as a live list.
// `clusterCachedDataEventCollectionWatch` is a per-cache collection watch (a re-firing
// event bumps count/lastSeen → `Modified`). Reduced via `useCacheCollectionWatch`, keyed
// by `uid` with cacheID provenance so two caches' events never mix.
import { useMemo } from 'react';

import { graphql } from '@/gql';
import type { ClusterCachedDataEventCollectionWatchSubscription } from '@/gql/graphql';
import { useActiveCluster } from '@/lib/active-cluster';
import { useCacheCollectionWatch, joinProvenance } from '@/lib/graphql/use-cache-collection-watch';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';

const ClusterCachedDataEventCollectionWatchDocument = graphql(`
  subscription ClusterCachedDataEventCollectionWatch($id: ClusterID!, $cacheID: ObjectID!) {
    clusterCachedDataEventCollectionWatch(id: $id, cacheID: $cacheID) {
      type
      cacheID
      event {
        uid
        type
        reason
        message
        count
        firstSeen
        lastSeen
        involvedKind
        involvedNamespace
        involvedName
      }
    }
  }
`);

// One cached event (the `event` payload of a change), as the table renders it.
export type ClusterCachedDataEvent = NonNullable<
  ClusterCachedDataEventCollectionWatchSubscription['clusterCachedDataEventCollectionWatch']['event']
>;

// An event's repetitions: `×3`, or `1` for one seen once.
export function countText(count: number): string {
  return count > 1 ? `×${count}` : '1';
}

// When an event was last seen, in milliseconds: NaN when absent or unparsable.
export function lastSeenMs(e: ClusterCachedDataEvent): number {
  return e.lastSeen ? Date.parse(e.lastSeen) : Number.NaN;
}

// Newest first, an event never seen last.
export function lastSeenSortValue(e: ClusterCachedDataEvent): number | undefined {
  const ms = lastSeenMs(e);
  return Number.isNaN(ms) ? undefined : -ms;
}

// The active context's cached events, newest first (by lastSeen), live. Paused
// (empty) without an active cache. Provenance is just the cacheID — the variables
// carry no kind. `unshared` is for a watch that can mount into a running one, which would
// never see its snapshot.
export function useClusterCachedDataEvents({ unshared = false }: { unshared?: boolean } = {}): {
  events: ClusterCachedDataEvent[];
  active: boolean;
  phase: SubscriptionPhase;
} {
  const { clusterID, cacheID, active } = useActiveCluster();

  const { items, phase } = useCacheCollectionWatch<
    ClusterCachedDataEventCollectionWatchSubscription,
    ClusterCachedDataEvent
  >(
    {
      query: ClusterCachedDataEventCollectionWatchDocument,
      variables: { id: clusterID ?? '', cacheID: cacheID ?? '' },
      pause: !active,
    },
    {
      select: (d) => {
        const f = d.clusterCachedDataEventCollectionWatch;
        return { type: f.type, item: f.event, provenance: joinProvenance(f.cacheID) };
      },
      keyOf: (e) => e.uid,
      currentProvenance: joinProvenance(cacheID ?? ''),
      unshared,
    },
  );

  // Newest first by lastSeen; decorate–sort–undecorate so Date.parse runs once per row.
  // A lexicographic string sort is unsafe (RFC3339 mixes fractional and whole-second
  // forms). Null lastSeen sorts oldest.
  const events = useMemo(
    () =>
      items
        .map((e) => ({ e, ms: e.lastSeen ? Date.parse(e.lastSeen) : 0 }))
        .toSorted((a, b) => b.ms - a.ms)
        .map((x) => x.e),
    [items],
  );

  return { events, active, phase };
}
