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

// An object's row menu: its kind's actions behind a ghost icon button.
import { Button } from '@kubetail/ui/elements/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@kubetail/ui/elements/dropdown-menu';
import { FileCode, List, MoreHorizontal, ScrollText } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

import type { GVR } from '@/lib/gvr';
import { actionsForKind } from '@/lib/object-actions';
import type { ObjectAction } from '@/lib/object-actions';
import { visibleTitle } from '@/lib/visible-text';

const ACTION: Record<ObjectAction, { label: string; icon: LucideIcon }> = {
  yaml: { label: 'View YAML', icon: FileCode },
  events: { label: 'View events', icon: List },
  logs: { label: 'View logs', icon: ScrollText },
};

type ActionsMenuProps = {
  gvr: GVR;
  // The object's name, for the trigger's label.
  name: string;
  onAction: (action: ObjectAction) => void;
};

export function ActionsMenu({ gvr, name, onAction }: ActionsMenuProps) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button type="button" variant="ghost" size="icon-sm" />}
        aria-label={`Actions for ${visibleTitle(name)}`}
      >
        <MoreHorizontal className="size-4" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={4}>
        {actionsForKind(gvr).map((action) => {
          const { label, icon: Icon } = ACTION[action];
          return (
            <DropdownMenuItem key={action} onClick={() => onAction(action)}>
              <Icon className="size-4" aria-hidden />
              {label}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
