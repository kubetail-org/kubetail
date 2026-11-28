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

// The objects beside an open object's detail: one row each, the whole row the button that
// opens it, in the table's sort.
import { cn } from '@kubetail/ui/lib/utils';
import { ChevronRight } from 'lucide-react';
import { useMemo } from 'react';

import { Age, timestampMs } from '@/components/dashboard/age';
import { LIST_ROW } from '@/components/dashboard/row-size';
import { VirtualTable } from '@/components/dashboard/virtual-table';
import type { VirtualColumn } from '@/components/dashboard/virtual-table';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import { HEALTH_TONE, TONE } from '@/lib/cluster-health';
import type { StatusTone } from '@/lib/cluster-health';
import { ownerOf } from '@/lib/object-owner';
import { visibleTitle } from '@/lib/visible-text';

const rowKey = (o: ClusterCachedDataObject) => o.uid;

export type ObjectsListProps = {
  // The rows the tab and the search leave, sorted.
  rows: readonly ClusterCachedDataObject[];
  // How many objects the namespace scope holds.
  inScope: number;
  // The kind's plural, lowercase, for the footer.
  plural: string;
  // Whether a row names its namespace: while the table would show the Namespace column.
  showNamespace: boolean;
  // The kind's tone, for a kind with a status.
  toneOf?: (o: ClusterCachedDataObject) => StatusTone;
  openUid: string;
  onOpen: (uid: string) => void;
};

type ListColumnOptions = Pick<ObjectsListProps, 'showNamespace' | 'toneOf' | 'openUid' | 'onOpen'>;

// One column, the row's whole width, that names no header.
function listColumns({
  showNamespace,
  toneOf,
  openUid,
  onOpen,
}: ListColumnOptions): VirtualColumn<ClusterCachedDataObject>[] {
  return [
    {
      key: 'object',
      header: 'Object',
      cell: (o) => {
        const open = o.uid === openUid;
        const where = [showNamespace ? o.namespace : '', ownerOf(o.rawJSON) ?? ''].filter(Boolean).join(' · ');
        return (
          <button
            type="button"
            aria-current={open || undefined}
            onClick={() => onOpen(o.uid)}
            className={cn('flex w-full items-center gap-2 px-3 py-2 text-left', open && 'bg-sidebar-accent')}
          >
            <span
              aria-hidden
              className={cn(
                'size-1.75 shrink-0 rounded-full',
                toneOf ? TONE[HEALTH_TONE[toneOf(o)]].dot : 'bg-muted-foreground',
              )}
            />
            <span className="min-w-0 flex-1 text-xs/4">
              <span className="block truncate font-mono text-primary" title={visibleTitle(o.name)}>
                <VisibleText text={o.name} />
              </span>
              {where && (
                <span className="block truncate text-muted-foreground" title={visibleTitle(where)}>
                  <VisibleText text={where} />
                </span>
              )}
            </span>
            <Age
              ms={timestampMs(o.creationTimestamp)}
              className="shrink-0 text-xs text-muted-foreground tabular-nums"
            />
            <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          </button>
        );
      },
    },
  ];
}

export function ObjectsList({ rows, inScope, plural, showNamespace, toneOf, openUid, onOpen }: ObjectsListProps) {
  const columns = useMemo(
    () => listColumns({ showNamespace, toneOf, openUid, onOpen }),
    [showNamespace, toneOf, openUid, onOpen],
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border bg-background">
      <VirtualTable rows={rows} rowKey={rowKey} columns={columns} rowSize={LIST_ROW} headless />
      <div className="border-t bg-card px-3 py-2.25 text-xs text-muted-foreground">
        Showing {rows.length} of {inScope} {plural}
      </div>
    </div>
  );
}
