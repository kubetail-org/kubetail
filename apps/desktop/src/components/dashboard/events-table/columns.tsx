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

// The Events table's columns, and the readings of an event the list and the detail share.
import { Age, ageSortValue, timestampMs } from '@/components/dashboard/age';
import { StatusPill } from '@/components/dashboard/status-pill';
import type { TableSort, VirtualColumn } from '@/components/dashboard/virtual-table';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import { visibleTitle } from '@/lib/visible-text';

export type EventColumn = VirtualColumn<ClusterCachedDataEvent>;

export const rowKey = (e: ClusterCachedDataEvent) => e.uid;

// An event's repetitions: `×3`, or `1` for one seen once.
export function countText(count: number): string {
  return count > 1 ? `×${count}` : '1';
}

export function lastSeenMs(e: ClusterCachedDataEvent): number {
  return timestampMs(e.lastSeen);
}

// Newest first.
export function lastSeenSortValue(e: ClusterCachedDataEvent): number | undefined {
  return ageSortValue(lastSeenMs(e));
}

// The parts of a line joined by middle dots, an empty one left out with its separator.
export function dotted(...parts: string[]): string {
  return parts.filter(Boolean).join(' · ');
}

// `type` is an open Kubernetes string: a cluster may omit it or write its own. Only Warning
// takes a pill; the rest read as muted text, "Unknown" when empty.
function EventType({ type }: { type: string }) {
  if (type === 'Warning') return <StatusPill label="Warning" tone="pending" />;
  return (
    <span className="text-xs text-muted-foreground">
      <VisibleText text={type || 'Unknown'} />
    </span>
  );
}

const LAST_SEEN_COLUMN: EventColumn = {
  key: 'lastSeen',
  header: 'Last seen',
  className: 'tabular-nums',
  width: 76,
  sortValue: lastSeenSortValue,
  cell: (e) => <Age ms={lastSeenMs(e)} className="text-sm text-foreground" />,
};

// The involved object's name over its kind and namespace, as a kind page's Name cell draws a
// name over its owner. It is the row, so it is never hidden.
export const OBJECT_COLUMN: EventColumn = {
  key: 'object',
  header: 'Object',
  className: 'truncate',
  width: 210,
  sortValue: (e) => e.involvedName || undefined,
  cell: (e) => {
    const where = dotted(e.involvedKind, e.involvedNamespace);
    return (
      <>
        <div className="truncate font-mono text-xs text-primary" title={visibleTitle(e.involvedName)}>
          <VisibleText text={e.involvedName} />
        </div>
        {where && (
          <div className="truncate text-xs text-muted-foreground" title={visibleTitle(where)}>
            <VisibleText text={where} />
          </div>
        )}
      </>
    );
  },
};

// The table's columns in their default order. Message takes the width the others leave.
export const EVENT_COLUMNS: readonly EventColumn[] = [
  LAST_SEEN_COLUMN,
  OBJECT_COLUMN,
  {
    key: 'type',
    header: 'Type',
    width: 100,
    sortValue: (e) => e.type || undefined,
    cell: (e) => <EventType type={e.type} />,
  },
  {
    key: 'reason',
    header: 'Reason',
    className: 'truncate',
    width: 140,
    sortValue: (e) => e.reason || undefined,
    cell: (e) => (
      <span className="text-xs text-foreground" title={visibleTitle(e.reason)}>
        <VisibleText text={e.reason || '—'} />
      </span>
    ),
  },
  {
    key: 'message',
    header: 'Message',
    className: 'truncate',
    minWidth: 240,
    sortValue: (e) => e.message || undefined,
    cell: (e) => (
      <span className="text-xs text-muted-foreground" title={visibleTitle(e.message)}>
        <VisibleText text={e.message || '—'} />
      </span>
    ),
  },
  {
    key: 'count',
    header: 'Count',
    className: 'tabular-nums',
    width: 56,
    sortValue: (e) => e.count,
    descFirst: true,
    cell: (e) => <span className="text-sm text-muted-foreground">{countText(e.count)}</span>,
  },
];

// Newest first, as the watch hands them.
export const DEFAULT_SORT: TableSort = { key: LAST_SEEN_COLUMN.key, desc: false };
