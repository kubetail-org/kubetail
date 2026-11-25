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

// The live dashboard resource tree over the active cluster's kinds catalog. Until the
// catalog's Bookmark lands, or with no active cache, the tree is the curated kinds without
// counts. See docs/adr/2026-11-12-the-dashboard-nav-groups-kinds-by-scope-and-api-group.md
import { Compass, Globe, Layers, List } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { useMemo } from 'react';

import { useClusterCachedDataKinds } from '@/lib/cluster-cached-data-kinds';
import { buildDashboardNav, navPlaceOf } from '@/lib/dashboard-resources';
import type { DashboardNav, DashboardResource, NavPlace, NavSection, ServerKind } from '@/lib/dashboard-resources';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';

// A pinned page's icon, by its id; a kind has none of its own.
export const PINNED_ICON: Partial<Record<DashboardResource, LucideIcon>> = { overview: Compass, events: List };

// A section's icon, which its kinds wear in the breadcrumb bar.
export const SECTION_ICON: Record<NavSection['id'], LucideIcon> = { cluster: Globe, namespace: Layers };

// While paused (`active` false), `phase`'s `connected` is meaninglessly false — a
// "reconnecting/loading" affordance must gate on `active`, not `phase` alone, to
// stay silent on the curated-only fallback.
export function useDashboardNav(): { nav: DashboardNav; active: boolean; phase: SubscriptionPhase } {
  const { kinds, active, phase } = useClusterCachedDataKinds();
  // Undefined stands for the curated kinds.
  const landed = catalogLanded(active, phase) ? kinds : undefined;
  const nav = useMemo(() => buildDashboardNav(landed), [landed]);
  return { nav, active, phase };
}

// Where a kind sits in that tree, without building it. `kind` is the caller's search of the
// same catalog, or the kind it holds through a replay: without one, a landed catalog lacks
// the id, and before it lands the curated kinds place it.
export function useDashboardNavPlace(id: DashboardResource, kind?: ServerKind): NavPlace | undefined {
  const { active, phase } = useClusterCachedDataKinds();
  if (kind) return navPlaceOf([kind], id);
  return navPlaceOf(catalogLanded(active, phase) ? [] : undefined, id);
}

function catalogLanded(active: boolean, phase: SubscriptionPhase): boolean {
  return active && phase !== 'connecting';
}
