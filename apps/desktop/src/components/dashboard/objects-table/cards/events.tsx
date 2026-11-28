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

// The events about an open object, newest first, five until the user asks for all, filtered
// from the detail's events watch as the object panel's Events tab filters its own.
import { Spinner } from '@kubetail/ui/elements/spinner';
import { useMemo, useState } from 'react';

import { Age } from '@/components/dashboard/age';
import { DetailCard } from '@/components/dashboard/detail-card';
import { VisibleText } from '@/components/primitives/visible-text';
import { countText, lastSeenMs, lastSeenSortValue } from '@/lib/cluster-cached-data-events';
import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import { sortRows } from '@/lib/sort-rows';
import { visibleTitle } from '@/lib/visible-text';

import { SmallTable } from './small-table';
import type { SmallColumn } from './small-table';

const FIRST = 5;

const COLUMNS: SmallColumn<ClusterCachedDataEvent>[] = [
  {
    header: 'Reason',
    cell: (e) => (
      <span className={e.type === 'Warning' ? 'text-destructive' : 'text-foreground'}>
        <VisibleText text={e.reason || '—'} />
      </span>
    ),
  },
  {
    header: 'Message',
    className: 'text-muted-foreground',
    cell: (e) => (
      <span title={visibleTitle(e.message)}>
        <VisibleText text={e.message || '—'} />
      </span>
    ),
  },
  { header: 'Count', className: 'text-muted-foreground tabular-nums', cell: (e) => countText(e.count) },
  {
    header: 'Last seen',
    className: 'text-muted-foreground tabular-nums',
    cell: (e) => <Age ms={lastSeenMs(e)} className="" />,
  },
];

export type EventsWatch = { events: readonly ClusterCachedDataEvent[]; phase: SubscriptionPhase };

type EventsCardProps = EventsWatch & { kind: string; namespace: string; name: string };

// An event names its object by kind, namespace and name alone, so an earlier object of the
// same name shares them.
export function EventsCard({ kind, namespace, name, events, phase }: EventsCardProps) {
  const about = useMemo(
    () =>
      sortRows(
        events.filter((e) => e.involvedKind === kind && e.involvedNamespace === namespace && e.involvedName === name),
        lastSeenSortValue,
        false,
      ),
    [events, kind, namespace, name],
  );
  const warnings = about.filter((e) => e.type === 'Warning').length;
  const [all, setAll] = useState(false);
  const shown = all ? about : about.slice(0, FIRST);

  let body;
  if (phase === 'connecting') {
    body = (
      <div className="flex items-center gap-2 px-3 py-2.5 text-xs text-muted-foreground">
        <Spinner size="sm" />
        Loading events…
      </div>
    );
  } else if (about.length === 0) {
    body = <p className="px-3 py-2.5 text-xs text-muted-foreground">No events.</p>;
  } else {
    body = (
      <SmallTable
        grid="grid-cols-[minmax(0,120px)_minmax(80px,1fr)_40px_56px]"
        columns={COLUMNS}
        rows={shown}
        rowKey={(e) => e.uid}
        footer={
          shown.length < about.length && (
            <button
              type="button"
              onClick={() => setAll(true)}
              className="w-full border-t px-3 py-2.5 text-left text-xs font-medium"
            >
              Show all {about.length} events
            </button>
          )
        }
      />
    );
  }

  return (
    <DetailCard
      title="Events"
      note={
        phase === 'connecting' ? undefined : (
          <>
            {warnings > 0 && (
              <span className="text-destructive">
                {warnings} {warnings === 1 ? 'warning' : 'warnings'} ·{' '}
              </span>
            )}
            {about.length}
          </>
        )
      }
    >
      {body}
    </DetailCard>
  );
}
