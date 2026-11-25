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

// The active cluster's namespace names, for the dashboard's namespace picker.
import { createContext, useContext, useMemo } from 'react';

import { useClusterCachedDataObjects } from '@/lib/cluster-cached-data-objects';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';

type ClusterNamespaces = { namespaces: string[]; active: boolean; phase: SubscriptionPhase };

const NAMESPACES = { apiVersion: 'v1', resource: 'namespaces' };

const ClusterNamespacesContext = createContext<ClusterNamespaces | null>(null);

// Holds the window's one namespace-list watch, so the picker's menu opens on a list
// already loaded. `unshared`: the Namespaces page watches the same kind, and whichever
// mounted second would never see a shared connection's snapshot.
export function ClusterNamespacesProvider({ enabled, children }: { enabled: boolean; children: React.ReactNode }) {
  const { objects, active, phase } = useClusterCachedDataObjects(NAMESPACES, { enabled, unshared: true });
  // The objects come sorted by namespace, then name; a namespace has no namespace.
  const value = useMemo(() => ({ namespaces: objects.map((o) => o.name), active, phase }), [objects, active, phase]);
  return <ClusterNamespacesContext.Provider value={value}>{children}</ClusterNamespacesContext.Provider>;
}

export function useClusterNamespaces(): ClusterNamespaces {
  const ctx = useContext(ClusterNamespacesContext);
  if (!ctx) throw new Error('useClusterNamespaces must be used inside <ClusterNamespacesProvider>');
  return ctx;
}
