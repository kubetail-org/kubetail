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

// One row of the dashboard's resource nav (the mockups' `K8s/NavItem`): a pinned row
// (`top`, with an icon) or a kind inside a group (`kind`). A router `Link` writing the
// `resource` search param, keeping the rest of the search.
import { Link } from '@tanstack/react-router';
import type { LucideIcon } from 'lucide-react';

import { VisibleText } from '@/components/primitives/visible-text';
import type { DashboardResource } from '@/lib/dashboard-resources';

// The current page's look; a closed group holding it borrows it.
export const SELECTED = 'data-[active=true]:bg-sidebar-accent data-[active=true]:text-sidebar-accent-foreground';

const ITEM = `flex w-full items-center gap-2 rounded-md text-sidebar-foreground transition-colors hover:bg-sidebar-accent/50 ${SELECTED}`;

const LEVEL = {
  top: 'h-8 px-2 text-sm font-medium',
  // The size of the group row above it.
  kind: 'h-6 pr-2 pl-6 text-[13px]',
};

// Right-aligned and tabular, so digits line up down the column.
export const COUNT = 'ml-auto min-w-7 shrink-0 text-right text-xs tabular-nums text-muted-foreground';

type NavItemProps = {
  id: DashboardResource;
  label: string;
  level: keyof typeof LEVEL;
  active: boolean;
  count?: number;
  icon?: LucideIcon;
};

export function NavItem({ id, label, level, active, count, icon: Icon }: NavItemProps) {
  return (
    <Link
      to="/dashboard"
      search={(prev) => ({ ...prev, resource: id })}
      data-active={active}
      aria-current={active ? 'page' : undefined}
      className={`${ITEM} ${LEVEL[level]}`}
    >
      {Icon && <Icon className="size-4 shrink-0" aria-hidden />}
      {/* A discovered kind's label is the cluster's text. */}
      <span className="truncate">
        <VisibleText text={label} />
      </span>
      {count !== undefined && <span className={COUNT}>{count.toLocaleString()}</span>}
    </Link>
  );
}
