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

// The app's cluster registry: reduces two collection watches and a keyed state watch
// (`clusterCollectionWatch`, `clusterCacheCollectionWatch`,
// `clusterCacheHealthKeyedStateWatch`) into id-keyed maps, then joins client-side — caches
// onto clusters (active cache = `spec.serverUid` matches `status.server.uid`), the folded
// sync verdict onto that cache by `cacheID`. See docs/data-access.md, *Collection watch*.
//
// The verdict is the sidecar's rollup over every kind the cache syncs — the cache's
// own `Synced` condition is deliberately coarse and NOT what the UI reads. Only
// always-mounted reads live here: the per-kind `ClusterCachedKind` records are subscribed by
// the sync dialog (`cluster-sync-panel/panel.tsx`), not fleet-wide in every window.
import { createContext, useContext, useMemo } from 'react';

import { graphql } from '@/gql';
import type {
  ClusterCollectionWatchSubscription,
  ClusterCacheCollectionWatchSubscription,
  ClusterCacheHealthKeyedStateWatchSubscription,
} from '@/gql/graphql';
import { collectionWatchReducer } from '@/lib/graphql/collection-watch';
import type { Keyed } from '@/lib/graphql/collection-watch';
import { keyedStateWatchReducer } from '@/lib/graphql/keyed-state-watch';
import { useFoldedSubscription } from '@/lib/graphql/use-folded-subscription';

// One item on each collection watch (the `cluster` / `cache` payload of a change).
// NonNullable: the item is null only on a Bookmark, which the reducers fold away,
// so nothing downstream of them ever sees one.
type ClusterRow = NonNullable<ClusterCollectionWatchSubscription['clusterCollectionWatch']['cluster']>;
type CacheRow = NonNullable<ClusterCacheCollectionWatchSubscription['clusterCacheCollectionWatch']['cache']>;
export type ClusterCacheHealth = ClusterCacheHealthKeyedStateWatchSubscription['clusterCacheHealthKeyedStateWatch'];

// A cache joined with its sync verdict (null until that frame lands). The selection
// is deliberately thin — omitting the coarse `Synced` condition from the query
// enforces "the UI doesn't read it" at the schema level.
type JoinedCache = CacheRow & { health: ClusterCacheHealth | null };

// A cluster joined with its active cache (`spec.serverUid` matches `status.server.uid`);
// null when never probed or mid-migration.
export type Cluster = ClusterRow & { activeCache: JoinedCache | null };

const ClusterCollectionWatchDocument = graphql(`
  subscription ClusterCollectionWatch {
    clusterCollectionWatch {
      type
      cluster {
        id
        deletionRequestedAt
        spec {
          name
          syncEnabled
          enabled
          isDefault
          source {
            kubeconfig {
              context
            }
          }
        }
        status {
          source {
            kubeconfig {
              cluster {
                name
                entry {
                  server
                  insecureSkipTLSVerify
                }
              }
              user {
                name
              }
              isPresent
              isDefault
            }
          }
          server {
            uid
          }
        }
        conditions {
          type
          status
          reason
          message
          liveness
          unconfirmed
          transitionedAt
        }
      }
    }
  }
`);

const ClusterCacheCollectionWatchDocument = graphql(`
  subscription ClusterCacheCollectionWatch {
    clusterCacheCollectionWatch {
      type
      cache {
        id
        clusterID
        spec {
          serverUid
        }
        # On-disk stats ride clusterCacheStatsStateWatch, subscribed per expanded row.
      }
    }
  }
`);

const ClusterCacheHealthKeyedStateWatchDocument = graphql(`
  subscription ClusterCacheHealthKeyedStateWatch {
    clusterCacheHealthKeyedStateWatch {
      cacheID
      status
      reason
      unhealthyKindRefs {
        apiVersion
        resource
        reason
      }
      failure
      totalKinds
      unhealthyKinds
      pausedKinds
      lastUpdateAt
      lastLiveAt
    }
  }
`);

type ClustersContextValue = {
  // null until the cluster snapshot's Bookmark lands; [] = no known clusters. The
  // two are distinct on purpose — [] renders "no clusters", null renders a spinner.
  clusters: Cluster[] | null;
  // Transport up? Sourced from `clusterCollectionWatch`; pair with
  // `clusters` via `subscriptionPhase` to tell "reconnecting" from "live".
  connected: boolean;
};

const ClustersContext = createContext<ClustersContextValue | null>(null);

// The boundary reports the store as it is, tombstones included, and leaves the choice
// here. One place, at the edge: a per-view filter is one a view forgets. A record being
// torn down is folded as a removal rather than rendered mid-teardown — the window is
// microseconds unless children are draining.
const reduceClusters = collectionWatchReducer(
  ({ clusterCollectionWatch: { type, cluster } }: ClusterCollectionWatchSubscription) => ({
    type: cluster?.deletionRequestedAt ? 'Deleted' : type,
    item: cluster,
  }),
);

// The caches join in as they arrive (the subscriptions carry no mutual ordering), so their
// Bookmark needs no gate of its own here.
const reduceCaches = collectionWatchReducer(
  ({ clusterCacheCollectionWatch: { type, cache } }: ClusterCacheCollectionWatchSubscription) => ({
    type,
    item: cache,
  }),
);

// Each frame replaces that cache's verdict outright. Which verdicts are LIVE is decided
// in the provider, against the cache collection.
const reduceHealth = keyedStateWatchReducer(
  ({ clusterCacheHealthKeyedStateWatch: health }: ClusterCacheHealthKeyedStateWatchSubscription) => health,
  (health) => health.cacheID,
);

export function ClustersProvider({ children }: { children: React.ReactNode }) {
  // Each subscription reduces into its own id-keyed map; useFoldedSubscription resets it
  // to `undefined` on a transport reconnect.
  const { data: clusterState, connected } = useFoldedSubscription(
    { query: ClusterCollectionWatchDocument },
    reduceClusters,
  );
  // Held back until the snapshot is complete, so a half-listed fleet never reads as
  // the whole fleet.
  const clusterMap = clusterState?.snapshotComplete ? clusterState.items : undefined;
  const { data: cacheState } = useFoldedSubscription({ query: ClusterCacheCollectionWatchDocument }, reduceCaches);
  const cacheMap = cacheState?.items;
  const { data: verdictMap } = useFoldedSubscription(
    { query: ClusterCacheHealthKeyedStateWatchDocument },
    reduceHealth,
  );
  // A keyed state watch has no removal frame, so a verdict's lifetime is its CACHE's.
  // Derive liveness here (against the cache collection) rather than sweeping in the
  // verdict reducer: a sweep gated on verdict traffic never runs once the fleet goes
  // quiet. And since the subscriptions carry no mutual ordering, an early verdict is
  // only HIDDEN until its cache lands — eviction would lose it for good.
  const healthMap = useMemo<Keyed<ClusterCacheHealth> | undefined>(() => {
    if (!verdictMap) return undefined;
    if (!cacheMap) return new Map();
    const live = new Map<string, ClusterCacheHealth>();
    verdictMap.forEach((h, cacheID) => {
      if (cacheMap.has(cacheID)) live.set(cacheID, h);
    });
    return live;
  }, [verdictMap, cacheMap]);

  // Join. No mutual ordering across subscriptions: a cluster can briefly lack its cache,
  // a cache its verdict — each resolves once its frame lands. null (no cluster
  // frame yet) is distinct from [] (no clusters).
  const clusters = useMemo<Cluster[] | null>(() => {
    if (!clusterMap) return null;
    const caches = cacheMap ? [...cacheMap.values()] : [];

    return [...clusterMap.values()].map((c) => {
      const active = caches.find((cache) => cache.clusterID === c.id && cache.spec.serverUid === c.status.server.uid);
      return {
        ...c,
        activeCache: active ? { ...active, health: healthMap?.get(active.id) ?? null } : null,
      };
    });
  }, [clusterMap, cacheMap, healthMap]);

  // No `error` branch — subscribe-exchange reconnects transport drops internally.
  const value = useMemo<ClustersContextValue>(() => ({ clusters, connected }), [clusters, connected]);
  return <ClustersContext.Provider value={value}>{children}</ClustersContext.Provider>;
}

export function useClusters(): ClustersContextValue {
  const ctx = useContext(ClustersContext);
  if (!ctx) throw new Error('useClusters must be used inside <ClustersProvider>');
  return ctx;
}

// The record the sidecar keeps for a kubeconfig context, if it has made one.
export function clusterForContext(clusters: Cluster[] | null, context: string): Cluster | undefined {
  return clusters?.find((c) => c.spec.source.kubeconfig?.context === context);
}

export function displayName(c: Cluster): string {
  return c.spec.name || c.spec.source.kubeconfig?.context || c.id;
}

const KB = 1024;
const MB = KB * 1024;
const GB = MB * 1024;

/**
 * Human-readable cache size. `bytes` is the on-disk size of a cluster's cache
 * (0 = not cached). Binary units (1 KB = 1024 B); raw bytes show no decimals,
 * KB and up show one. Zero renders as an em dash rather than "0 B" so an
 * uncached row reads as "nothing stored".
 */
export function formatBytes(bytes: number): string {
  if (bytes <= 0) return '—';
  if (bytes < KB) return `${bytes} B`;
  if (bytes < MB) return `${(bytes / KB).toFixed(1)} KB`;
  if (bytes < GB) return `${(bytes / MB).toFixed(1)} MB`;
  return `${(bytes / GB).toFixed(1)} GB`;
}

/**
 * A data rate in bytes per second, in formatBytes' units. Zero is "0 B/s": an idle
 * connection is an answer, not a missing one.
 */
export function formatRate(bytesPerSecond: number): string {
  if (bytesPerSecond < KB) return `${Math.round(bytesPerSecond)} B/s`;
  return `${formatBytes(bytesPerSecond)}/s`;
}
