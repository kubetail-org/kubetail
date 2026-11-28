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

// The events beside an open event's detail: one row each, the whole row the button that opens
// it, in the table's sort.
import { cn } from '@kubetail/ui/lib/utils';
import { ChevronRight } from 'lucide-react';
import { useMemo } from 'react';

import { Age } from '@/components/dashboard/age';
import { LIST_ROW } from '@/components/dashboard/row-size';
import { VirtualTable } from '@/components/dashboard/virtual-table';
import type { VirtualColumn } from '@/components/dashboard/virtual-table';
import { VisibleText } from '@/components/primitives/visible-text';
import { lastSeenMs } from '@/lib/cluster-cached-data-events';
import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import { TONE } from '@/lib/cluster-health';
import { visibleTitle } from '@/lib/visible-text';

import { dotted, rowKey } from './columns';

type EventsListProps = {
  // The rows the tab and the search leave, sorted.
  rows: readonly ClusterCachedDataEvent[];
  // How many events the namespace scope holds.
  inScope: number;
  openUid: string;
  onOpen: (uid: string) => void;
};

// One column, the row's whole width, that names no header.
function listColumns(openUid: string, onOpen: (uid: string) => void): VirtualColumn<ClusterCachedDataEvent>[] {
  return [
    {
      key: 'event',
      header: 'Event',
      cell: (e) => {
        const open = e.uid === openUid;
        const where = dotted(e.involvedName, e.involvedKind, e.involvedNamespace);
        return (
          <button
            type="button"
            aria-current={open || undefined}
            onClick={() => onOpen(e.uid)}
            className={cn('flex w-full items-center gap-2 px-3 py-2 text-left', open && 'bg-sidebar-accent')}
          >
            <span
              aria-hidden
              className={cn(
                'size-1.75 shrink-0 rounded-full',
                e.type === 'Warning' ? TONE.attention.dot : 'bg-muted-foreground',
              )}
            />
            <span className="min-w-0 flex-1 text-xs/4">
              <span className="block truncate text-foreground" title={visibleTitle(e.reason)}>
                <VisibleText text={e.reason || '—'} />
              </span>
              <span className="block truncate text-muted-foreground" title={visibleTitle(where)}>
                <VisibleText text={where} />
              </span>
            </span>
            <Age ms={lastSeenMs(e)} className="shrink-0 text-xs text-muted-foreground tabular-nums" />
            <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
          </button>
        );
      },
    },
  ];
}

export function EventsList({ rows, inScope, openUid, onOpen }: EventsListProps) {
  const columns = useMemo(() => listColumns(openUid, onOpen), [openUid, onOpen]);

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border bg-background">
      <VirtualTable rows={rows} rowKey={rowKey} columns={columns} rowSize={LIST_ROW} headless />
      <div className="border-t bg-card px-3 py-2.25 text-xs text-muted-foreground">
        Showing {rows.length} of {inScope} events
      </div>
    </div>
  );
}
