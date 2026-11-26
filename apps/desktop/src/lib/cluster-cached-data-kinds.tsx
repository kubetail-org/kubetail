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

// The active cluster's discovered kind catalog as a live `ServerKind[]`, read by the
// dashboard nav and the object tables (which resolve a curated nav id like "pods"
// back to its full apiVersion/resource/scope here).
// `clusterCachedDataKindCollectionWatch` is a per-cache collection watch; per-kind `count`
// is live (an object write re-emits as `Modified`). Reduced via `useCacheCollectionWatch`,
// whose cacheID provenance guard keeps two caches' kinds from mixing.
import { createContext, useContext, useMemo } from 'react';

import { graphql } from '@/gql';
import type { ClusterCachedDataKindCollectionWatchSubscription } from '@/gql/graphql';
import { useActiveCluster } from '@/lib/active-cluster';
import type { ServerKind } from '@/lib/dashboard-resources';
import { useCacheCollectionWatch, joinProvenance } from '@/lib/graphql/use-cache-collection-watch';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import { gvrKey } from '@/lib/gvr';
import { useNamespaceScope } from '@/lib/namespace-scope';

const ClusterCachedDataKindCollectionWatchDocument = graphql(`
  subscription ClusterCachedDataKindCollectionWatch($id: ClusterID!, $cacheID: ObjectID!, $namespace: String!) {
    clusterCachedDataKindCollectionWatch(id: $id, cacheID: $cacheID, namespace: $namespace) {
      type
      cacheID
      namespace
      kind {
        apiVersion
        kind
        resource
        scope
        isCRD
        count
        printerColumns {
          name
          type
          jsonPath
          priority
        }
      }
    }
  }
`);

// The `kind` payload of a change — structurally a `ServerKind`.
type KindRow = NonNullable<
  ClusterCachedDataKindCollectionWatchSubscription['clusterCachedDataKindCollectionWatch']['kind']
>;

// `cacheID` is the cache the kinds came from, undefined whenever `active` is false.
type ClusterCachedDataKindsContextValue = {
  kinds: ServerKind[];
  active: boolean;
  cacheID: string | undefined;
  phase: SubscriptionPhase;
};

const ClusterCachedDataKindsContext = createContext<ClusterCachedDataKindsContextValue | null>(null);

// Holds the window's one catalog subscription, above both consumers, so it outlives the
// nav's card. `enabled` is false off the dashboard, where nothing reads the catalog, and
// the value reports it: a paused watch with no kinds, never one still connecting.
export function ClusterCachedDataKindsProvider({ enabled, children }: { enabled: boolean; children: React.ReactNode }) {
  const { clusterID, cacheID, active } = useActiveCluster();
  const { namespace } = useNamespaceScope();

  // The sidecar counts each namespaced kind in the namespace scope ('' for the whole
  // cache), so the scope is a variable and, with the cacheID, the provenance: a cache
  // swap or a namespace switch re-subscribes.
  const { items, phase } = useCacheCollectionWatch<ClusterCachedDataKindCollectionWatchSubscription, KindRow>(
    {
      query: ClusterCachedDataKindCollectionWatchDocument,
      variables: { id: clusterID ?? '', cacheID: cacheID ?? '', namespace: namespace ?? '' },
      pause: !enabled || !active,
    },
    {
      select: (d) => {
        const f = d.clusterCachedDataKindCollectionWatch;
        return { type: f.type, item: f.kind, provenance: joinProvenance(f.cacheID, f.namespace) };
      },
      keyOf: gvrKey,
      currentProvenance: joinProvenance(cacheID ?? '', namespace ?? ''),
    },
  );

  const value = useMemo<ClusterCachedDataKindsContextValue>(
    () => ({
      kinds: enabled ? items : [],
      active: enabled && active,
      cacheID: enabled && active ? cacheID : undefined,
      phase,
    }),
    [items, enabled, active, cacheID, phase],
  );
  return <ClusterCachedDataKindsContext.Provider value={value}>{children}</ClusterCachedDataKindsContext.Provider>;
}

// The active context's discovered kinds, live. Empty while paused: no active cache,
// or the provider disabled. Insertion order (no sort).
export function useClusterCachedDataKinds(): ClusterCachedDataKindsContextValue {
  const ctx = useContext(ClusterCachedDataKindsContext);
  if (!ctx) throw new Error('useClusterCachedDataKinds must be used inside <ClusterCachedDataKindsProvider>');
  return ctx;
}
