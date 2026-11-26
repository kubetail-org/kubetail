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

// One cluster's live diagnostics, each a stream the Clusters dialog and the status popover
// read: the cache's contents, the next connection check, and the connection's data rate.
import { graphql } from '@/gql';
import { sampleFeedReducer } from '@/lib/graphql/sample-feed';
import type { SampleFeedFrame } from '@/lib/graphql/sample-feed';
import { stateWatchReducer } from '@/lib/graphql/state-watch';
import { perFrame, useFoldedSubscription } from '@/lib/graphql/use-folded-subscription';

// The cache's contents as a state watch. NOT read off the ClusterCache record: that
// object stops changing once its sync settles, so a field there would freeze at
// subscribe time.
const ClusterCacheStatsStateWatchDocument = graphql(`
  subscription ClusterCacheStatsStateWatch($id: ClusterID!, $cacheID: ObjectID!) {
    clusterCacheStatsStateWatch(id: $id, cacheID: $cacheID) {
      exists
      bytes
      dbBytes
      walBytes
      shmBytes
      objectCount
      kindCount
    }
  }
`);

export type CacheContents = {
  exists: boolean;
  bytes: number;
  dbBytes: number;
  walBytes: number;
  shmBytes: number;
  objectCount: number;
  kindCount: number;
};

// A state watch: each frame replaces the last outright. null until the first frame.
export function useClusterCacheStatsStateWatch(
  clusterId: string,
  cacheId: string,
  pause: boolean,
): CacheContents | null {
  const { data } = useFoldedSubscription<{ clusterCacheStatsStateWatch: CacheContents }, CacheContents>(
    { query: ClusterCacheStatsStateWatchDocument, variables: { id: clusterId, cacheID: cacheId }, pause },
    stateWatchReducer((resp) => resp.clusterCacheStatsStateWatch),
  );
  return data ?? null;
}

// Next-reconcile time, streamed per-cluster (a scheduling change fires no list
// watch — the only way the countdown stays live for an idle disconnected cluster).
// Subscribed only while a row's diagnostics are open.
const ClusterScheduleStateWatchDocument = graphql(`
  subscription ClusterScheduleStateWatch($id: ClusterID!) {
    clusterScheduleStateWatch(id: $id) {
      nextRequeueAt
      probing
      failures
    }
  }
`);

// `failures` counts the connection attempts in a row that have failed.
export type NextCheckState = { atMs: number | null; probing: boolean; failures: number };

export function useClusterScheduleStateWatch(clusterId: string): NextCheckState {
  // Null `nextRequeueAt` = reconcile in flight (`probing`) or nothing scheduled.
  // Hold the last time only across the in-flight window; with `probing` false a
  // null must clear the countdown, not freeze a stale one.
  const { data } = useFoldedSubscription<
    { clusterScheduleStateWatch: { nextRequeueAt: string | null; probing: boolean; failures: number } },
    { nextRequeueAt: string | null; probing: boolean; failures: number }
  >(
    { query: ClusterScheduleStateWatchDocument, variables: { id: clusterId } },
    perFrame((prev, resp) => {
      const { nextRequeueAt, probing, failures } = resp.clusterScheduleStateWatch;
      return {
        nextRequeueAt: nextRequeueAt ?? (probing ? (prev?.nextRequeueAt ?? null) : null),
        probing,
        failures,
      };
    }),
  );
  return {
    atMs: parseTimeOrNull(data?.nextRequeueAt ?? null),
    probing: data?.probing ?? false,
    failures: data?.failures ?? 0,
  };
}

// The connection's data rate as a sample feed: the sidecar's window first, then each new sample.
// Unshared: a view mounting into a running shared stream would never see the window. Paused
// for a disabled cluster, which the sidecar refuses rather than reads as idle.
const ClusterDataRateSampleFeedDocument = graphql(`
  subscription ClusterDataRateSampleFeed($id: ClusterID!) {
    clusterDataRateSampleFeed(id: $id) {
      intervalSeconds
      windowSeconds
      samples {
        at
        receivedBytesPerSecond
        sentBytesPerSecond
      }
    }
  }
`);

type DataRateSample = { at: string; receivedBytesPerSecond: number; sentBytesPerSecond: number };
type DataRateSeries = SampleFeedFrame<DataRateSample>;

// Null until the window arrives.
export function useClusterDataRateSampleFeed(clusterId: string, pause: boolean): DataRateSeries | null {
  const { data } = useFoldedSubscription<{ clusterDataRateSampleFeed: DataRateSeries }, DataRateSeries>(
    { query: ClusterDataRateSampleFeedDocument, variables: { id: clusterId }, pause },
    sampleFeedReducer((resp) => resp.clusterDataRateSampleFeed),
    { unshared: true },
  );
  return data ?? null;
}

// An ISO time in epoch milliseconds; null for a missing or unparseable one.
export function parseTimeOrNull(iso: string | null | undefined): number | null {
  const ms = iso ? Date.parse(iso) : Number.NaN;
  return Number.isNaN(ms) ? null : ms;
}
