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
import { ChevronRight } from 'lucide-react';

import { ActionsMenu } from '@/components/dashboard/actions-menu';
import type { VirtualColumn } from '@/components/dashboard/virtual-table';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import type { GVR } from '@/lib/gvr';
import type { ObjectAction } from '@/lib/object-actions';
import { visibleTitle } from '@/lib/visible-text';

export function actionsColumn(
  gvr: GVR,
  onAction: (action: ObjectAction, object: ClusterCachedDataObject) => void,
): VirtualColumn<ClusterCachedDataObject> {
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
        <ActionsMenu gvr={gvr} name={o.name} onAction={(action) => onAction(action, o)} />
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
