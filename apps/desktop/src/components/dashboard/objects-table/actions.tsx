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

// The object table's last column: a menu of the row's actions behind a ⋯ on every row, and a
// chevron that opens the object.
import { Button } from '@kubetail/ui/elements/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@kubetail/ui/elements/dropdown-menu';
import { ChevronRight, FileCode, List, MoreHorizontal, ScrollText } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

import type { VirtualColumn } from '@/components/dashboard/virtual-table';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import type { GVR } from '@/lib/gvr';
import { actionsForKind } from '@/lib/object-actions';
import type { ObjectAction } from '@/lib/object-actions';
import { visibleTitle } from '@/lib/visible-text';

const ACTION: Record<ObjectAction, { label: string; icon: LucideIcon }> = {
  yaml: { label: 'View YAML', icon: FileCode },
  events: { label: 'View events', icon: List },
  logs: { label: 'View logs', icon: ScrollText },
};

export function actionsColumn(
  gvr: GVR,
  onAction: (action: ObjectAction, object: ClusterCachedDataObject) => void,
): VirtualColumn<ClusterCachedDataObject> {
  const actions = actionsForKind(gvr);
  return {
    key: 'actions',
    // The trigger names itself; a header would only crowd the narrow column.
    header: '',
    className: 'text-right',
    pinned: true,
    // Two `icon-sm` buttons (28px each) and the last cell's padding (6px left, 12px right).
    width: 74,
    cell: (o) => (
      <div className="flex justify-end">
        <DropdownMenu>
          <DropdownMenuTrigger
            render={<Button type="button" variant="ghost" size="icon-sm" />}
            aria-label={`Actions for ${visibleTitle(o.name)}`}
          >
            <MoreHorizontal className="size-4" aria-hidden />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" sideOffset={4}>
            {actions.map((action) => {
              const { label, icon: Icon } = ACTION[action];
              return (
                <DropdownMenuItem key={action} onClick={() => onAction(action, o)}>
                  <Icon className="size-4" aria-hidden />
                  {label}
                </DropdownMenuItem>
              );
            })}
          </DropdownMenuContent>
        </DropdownMenu>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={`Open ${visibleTitle(o.name)}`}
          onClick={() => onAction('yaml', o)}
        >
          <ChevronRight className="size-4 text-muted-foreground" aria-hidden />
        </Button>
      </div>
    ),
  };
}
