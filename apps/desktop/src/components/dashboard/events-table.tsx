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

// The dashboard's cached Kubernetes Events table (newest first), over the rows its page
// reads from `useClusterCachedDataEvents`. Gates on `active`/`phase` — the pattern every
// whole-screen watch view follows.
// See docs/read-contracts/README.md
import { Spinner } from '@kubetail/ui/elements/spinner';
import ReactTimeAgo from 'react-timeago';

import { VirtualTable } from '@/components/dashboard/virtual-table';
import type { VirtualColumn } from '@/components/dashboard/virtual-table';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';

// kubectl-style "Kind namespace/name"; any part may be absent (cluster-scoped or
// name-only reference).
function involvedRef(e: ClusterCachedDataEvent): string {
  const path = e.involvedNamespace ? `${e.involvedNamespace}/${e.involvedName}` : e.involvedName;
  return [e.involvedKind, path].filter(Boolean).join(' ');
}

// `type` is an open Kubernetes string — a cluster may omit it or supply a custom
// value, so everything but Warning stays muted (empty renders "Unknown").
export function TypeBadge({ type }: { type: ClusterCachedDataEvent['type'] }) {
  const warning = type === 'Warning';
  return (
    <span
      className={`inline-block rounded px-1.5 py-0.5 text-xs font-medium ${
        warning ? 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300' : 'bg-muted text-muted-foreground'
      }`}
    >
      {type || 'Unknown'}
    </span>
  );
}

const rowKey = (e: ClusterCachedDataEvent) => e.uid;

// Object and Message truncate under the fixed layout, with the full text on hover; Message
// takes the width the sized columns leave.
const COLUMNS: VirtualColumn<ClusterCachedDataEvent>[] = [
  { key: 'type', header: 'Type', width: 96, cell: (e) => <TypeBadge type={e.type} /> },
  {
    key: 'reason',
    header: 'Reason',
    width: 192,
    cell: (e) => <span className="font-medium">{e.reason || '—'}</span>,
  },
  {
    key: 'object',
    header: 'Object',
    className: 'truncate',
    width: 256,
    cell: (e) => {
      const ref = involvedRef(e);
      return <span title={ref}>{ref || '—'}</span>;
    },
  },
  {
    key: 'message',
    header: 'Message',
    className: 'truncate',
    minWidth: 240,
    cell: (e) => (
      <span className="text-muted-foreground" title={e.message}>
        {e.message || '—'}
      </span>
    ),
  },
  { key: 'count', header: 'Count', className: 'text-right tabular-nums', width: 64, cell: (e) => e.count },
  {
    key: 'lastSeen',
    header: 'Last Seen',
    className: 'tabular-nums',
    width: 128,
    cell: (e) => {
      // lastSeen is null when the source Event carried no timestamp.
      const lastSeenMs = e.lastSeen ? Date.parse(e.lastSeen) : Number.NaN;
      return Number.isNaN(lastSeenMs) ? '—' : <ReactTimeAgo date={lastSeenMs} component="span" maxPeriod={60} />;
    },
  },
];

type EventsTableProps = {
  // The watch's rows, already filtered to `namespace`.
  events: ClusterCachedDataEvent[];
  active: boolean;
  phase: SubscriptionPhase;
  // The namespace the rows are filtered to, if any.
  namespace?: string;
};

export function EventsTable({ events, active, phase, namespace }: EventsTableProps) {
  // No active cache: an unsynced/paused cluster streams nothing — say so rather
  // than spin forever.
  if (!active) {
    return (
      <p className="text-sm text-muted-foreground">
        No synced cache for this cluster yet — events appear once syncing has started.
      </p>
    );
  }

  // Distinct from a genuinely empty snapshot (below).
  if (phase === 'connecting') {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Spinner size="sm" />
        Loading events…
      </div>
    );
  }

  if (events.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No events
        {namespace !== undefined && (
          <>
            {' in '}
            <VisibleText text={namespace} />
          </>
        )}
        .
      </p>
    );
  }

  return <VirtualTable rows={events} rowKey={rowKey} columns={COLUMNS} />;
}
