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

// The bar at the top of the dashboard's main panel: where the page sits in the resource
// nav, and the namespace scope every page reads.
import { cn } from '@kubetail/ui/lib/utils';
import { ChevronRight } from 'lucide-react';

import { VisibleText } from '@/components/primitives/visible-text';
import { PINNED_ICON, SECTION_ICON, useDashboardNavPlace } from '@/lib/dashboard-nav';
import type { DashboardResource, ServerKind } from '@/lib/dashboard-resources';

import { NamespacePicker } from './namespace-picker';

const CRUMB = 'flex min-w-0 items-center gap-1.5';

type DashboardBreadcrumbBarProps = {
  resource: DashboardResource;
  label: string;
  // The kind the page shows, held through a replay of the catalog; placed in its stead.
  kind?: ServerKind;
};

export function DashboardBreadcrumbBar({ resource, label, kind }: DashboardBreadcrumbBarProps) {
  const place = useDashboardNavPlace(resource, kind);
  // A pinned page's icon is its nav row's; a kind's is its section's. The resource comes
  // from the URL, so only an own key counts: `__proto__` is on every object.
  const pinnedIcon = Object.hasOwn(PINNED_ICON, resource) ? PINNED_ICON[resource] : undefined;
  const Icon = place ? SECTION_ICON[place.section] : pinnedIcon;

  return (
    <div
      data-testid="breadcrumb-bar"
      className="flex h-10 shrink-0 items-center justify-between gap-4 border-b border-border px-4"
    >
      <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1.5">
        {Icon && <Icon aria-hidden className="size-4 shrink-0 text-muted-foreground" />}
        {/* The icon alone tells a cluster kind from a namespaced one, and a reader skips it. */}
        {place && <span className="sr-only">{place.sectionLabel}</span>}
        <ol className="flex min-w-0 items-center gap-1.5 text-sm font-medium">
          {place && (
            <li className={cn(CRUMB, 'text-muted-foreground')}>
              <span className="truncate">
                <VisibleText text={place.group} />
              </span>
            </li>
          )}
          <li aria-current="page" className={cn(CRUMB, 'text-foreground')}>
            {place && <ChevronRight aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />}
            {/* The current page is the page's one heading, for heading navigation. */}
            <h1 className="truncate">
              <VisibleText text={label} />
            </h1>
          </li>
        </ol>
      </nav>
      <NamespacePicker />
    </div>
  );
}
