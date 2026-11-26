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

// The engine behind every per-cache collection watch (`useClusterDataKinds`/`Events`/
// `Objects`): `collectionWatchReducer`, plus the correctness-critical provenance guard. A
// frame whose provenance ≠ `currentProvenance` is dropped (a straggler from a superseded
// subscription), and the accumulator restarts when the current provenance changes. The
// provenance string must carry EVERY dimension the watch is keyed on (cacheID for
// kinds/events; cacheID + apiVersion + resource for objects) — miss one and that
// dimension's stragglers mis-attribute. Transport reconnects (same provenance, full replay)
// are handled underneath by `useFoldedSubscription`.
// See docs/read-contracts/README.md, *Collection watch*.
//
// Items are returned in insertion order; sorting stays with the caller.
import { useMemo } from 'react';
import type { AnyVariables, UseSubscriptionArgs } from 'urql';

import { collectionWatchReducer } from '@/lib/graphql/collection-watch';
import type { CollectionChange, CollectionState } from '@/lib/graphql/collection-watch';
import { subscriptionPhase, useFoldedSubscription } from '@/lib/graphql/use-folded-subscription';
import type { SubscriptionOptions, SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';

// The reduced set, tagged with the provenance its items were folded under.
type CollectionSnapshot<T> = CollectionState<T> & { provenance: string };

// One frame as `select`ed: its change, and the frame's own provenance (built off the same
// dimensions as `currentProvenance`).
export type CacheCollectionChange<T> = CollectionChange<T> & { provenance: string };

// Joins provenance dimensions into one opaque key. The separator is the unit
// separator rather than anything a dimension could contain — written as an escape,
// since a literal control byte would make git read this file as binary.
export function joinProvenance(...parts: string[]): string {
  return parts.join('\u001f');
}

// Runs a per-cache collection watch; returns reduced items (insertion order) plus `phase`.
// Liveness (`active`) stays with the caller, which derived `pause` from it.
export function useCacheCollectionWatch<Data, T>(
  args: UseSubscriptionArgs<AnyVariables, Data>,
  opts: {
    // Extract the change type, item, and frame provenance from a raw frame.
    select: (data: Data) => CacheCollectionChange<T>;
    // The item's stable map key (uid, or apiVersion/resource for kinds).
    keyOf: (item: T) => string;
    // The active subscription's provenance — built off the same dimensions `select` reads.
    currentProvenance: string;
  } & SubscriptionOptions,
): { items: T[]; phase: SubscriptionPhase } {
  const { select, keyOf, currentProvenance, unshared } = opts;

  const { data, connected } = useFoldedSubscription(
    args,
    (prev: CollectionSnapshot<T> | undefined, frames) => {
      // Restart when the current provenance changed under us (cache swap / kind switch).
      const base = prev && prev.provenance === currentProvenance ? prev : undefined;
      // Drop stragglers from a superseded subscription.
      const current = frames.map(select).filter((change) => change.provenance === currentProvenance);
      const reduce = collectionWatchReducer((change: CacheCollectionChange<T>) => change, keyOf);
      return { ...reduce(base, current), provenance: currentProvenance };
    },
    { unshared },
  );

  // Read only a set tagged for the current provenance: on a swap/switch the accumulator
  // still holds the previous one's set for the render in which `currentProvenance` flips,
  // before the resubscribe clears it. Feeds both items and phase.
  const active = data && data.provenance === currentProvenance ? data : undefined;
  const activeItems = active?.items;
  const items = useMemo(() => (activeItems ? [...activeItems.values()] : []), [activeItems]);

  return { items, phase: subscriptionPhase(!!active?.snapshotComplete, connected) };
}
