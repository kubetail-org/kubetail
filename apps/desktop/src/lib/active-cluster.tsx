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

// The one home for the "active kube-context → cluster → active cache" join used by
// every cluster-data hook. Only kubeconfig-sourced records carry a context, so the
// match is on `spec.source.kubeconfig.context`. `clusterID`/`cacheID` are stable
// primitives (safe subscription keys); `active` is true only when both resolve — a
// never-synced/paused cluster has no active cache, so its data watches stay paused.
// `phase` is the clusters watch's: it says whether an undefined `clusterID` means no
// cluster or no snapshot yet.
import { useMemo } from 'react';

import { useActiveKubeContext } from '@/lib/active-kube-context';
import { clusterForContext, useClusters } from '@/lib/clusters';
import type { Cluster } from '@/lib/clusters';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';

export function useActiveCluster(): {
  cluster: Cluster | undefined;
  clusterID: string | undefined;
  cacheID: string | undefined;
  active: boolean;
  phase: SubscriptionPhase;
} {
  const { context, phase } = useActiveKubeContext();
  const { clusters } = useClusters();

  const cluster = useMemo(() => clusterForContext(clusters, context), [clusters, context]);
  const clusterID = cluster?.id;
  const cacheID = cluster?.activeCache?.id;
  return { cluster, clusterID, cacheID, active: !!(clusterID && cacheID), phase };
}
