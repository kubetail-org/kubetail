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

// The scope a Settings pane shows: every cluster, or one picked from the
// kubeconfig's clusters, then those no longer in it.
import { type Cluster, clusterForContext, displayName, useClusters } from '@/lib/clusters';
import { useKubeConfig } from '@/lib/kube-config';

/** The picker's value for every cluster; a cluster's is its id. */
export const EVERY = '';

export type Scope = {
  present: Cluster[];
  /** By name, none of them present, though the two watches can disagree for a moment. */
  archived: Cluster[];
  /** The picked cluster; undefined for every cluster, and once the picked record is gone. */
  cluster: Cluster | undefined;
};

export function useScope(picked: string): Scope {
  const { clusters } = useClusters();
  const { kubeConfig } = useKubeConfig();
  const present = (kubeConfig?.contexts ?? []).flatMap((ctx) => clusterForContext(clusters, ctx.name) ?? []);
  const archived = (clusters ?? [])
    .filter((c) => c.status.source.kubeconfig?.isPresent === false && !present.includes(c))
    .toSorted((a, b) => displayName(a).localeCompare(displayName(b)));
  return { present, archived, cluster: [...present, ...archived].find((c) => c.id === picked) };
}
