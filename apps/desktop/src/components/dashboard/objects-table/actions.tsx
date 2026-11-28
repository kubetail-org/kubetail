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

// The object table's last column: a menu of the row's actions behind a ⋯ on every row, then the
// chevron that opens the object.
import { ActionsMenu } from '@/components/dashboard/actions-menu';
import { chevronColumn } from '@/components/dashboard/chevron-column';
import type { VirtualColumn } from '@/components/dashboard/virtual-table';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import type { GVR } from '@/lib/gvr';
import type { ObjectAction } from '@/lib/object-actions';

export function actionsColumn(
  gvr: GVR,
  onAction: (action: ObjectAction, object: ClusterCachedDataObject) => void,
): VirtualColumn<ClusterCachedDataObject> {
  return chevronColumn({
    nameOf: (o) => o.name,
    onOpen: (o) => onAction('yaml', o),
    before: {
      buttons: 1,
      cell: (o) => <ActionsMenu gvr={gvr} name={o.name} onAction={(action) => onAction(action, o)} />,
    },
  });
}
