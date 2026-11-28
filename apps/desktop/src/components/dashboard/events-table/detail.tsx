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

// An open event in full, in the column beside the list: a header that stays put over its
// message, its details and the other events on its object, which scroll.
import { cn } from '@kubetail/ui/lib/utils';
import { useMemo } from 'react';

import { Age, timestampMs } from '@/components/dashboard/age';
import { DetailCard } from '@/components/dashboard/detail-card';
import { DetailPane } from '@/components/dashboard/detail-pane';
import { DetailRow } from '@/components/dashboard/detail-row';
import { SeenRow } from '@/components/dashboard/seen-row';
import { StatusPill } from '@/components/dashboard/status-pill';
import { VisibleText } from '@/components/primitives/visible-text';
import { countText, lastSeenMs, lastSeenSortValue } from '@/lib/cluster-cached-data-events';
import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import { TONE } from '@/lib/cluster-health';
import { useHeldRow } from '@/lib/graphql/use-held-row';
import { sortRows } from '@/lib/sort-rows';
import { visibleTitle } from '@/lib/visible-text';

type EventDetailProps = {
  // The open event as the rows hold it now: absent once it is gone, or while a reconnect
  // masks the rows.
  event: ClusterCachedDataEvent | undefined;
  // Every event in the namespace scope, whatever the tab and the search leave.
  events: readonly ClusterCachedDataEvent[];
  // The rows are the cache's whole present, after its snapshot's Bookmark: an absent event is
  // gone.
  settled: boolean;
  onOpen: (uid: string) => void;
  onClose: () => void;
  className?: string;
};

// The caller keys the detail on the open event's uid, so what it holds is that event's.
export function EventDetail({ event, events, settled, onOpen, onClose, className }: EventDetailProps) {
  const { shown, gone } = useHeldRow(event, settled);
  return (
    <DetailPane
      label="Event"
      title={shown && <Title event={shown} />}
      closeLabel="Close event"
      note={gone ? 'This event no longer exists.' : undefined}
      onClose={onClose}
      className={className}
    >
      {shown && !gone && (
        <>
          <DetailCard title="Message">
            <p className="p-3 text-xs/4.5 wrap-break-word whitespace-pre-wrap text-foreground">
              <VisibleText text={shown.message || '—'} trailing="lines" />
            </p>
          </DetailCard>
          <Details event={shown} />
          <OtherEvents event={shown} events={events} onOpen={onOpen} />
        </>
      )}
    </DetailPane>
  );
}

// The involved object's name, until the event's own is served.
function Title({ event }: { event: ClusterCachedDataEvent }) {
  const lastSeen = lastSeenMs(event);
  return (
    <>
      <div className="flex min-w-0 items-center gap-2">
        <h2
          className="truncate font-mono text-base/5.5 font-medium text-foreground"
          title={visibleTitle(event.involvedName)}
        >
          <VisibleText text={event.involvedName} />
        </h2>
        {event.type === 'Warning' && <StatusPill label="Warning" tone="pending" />}
      </div>
      <p className="mt-0.5 truncate text-xs/4.5 text-muted-foreground">
        Event
        {event.reason && (
          <>
            {' · '}
            <VisibleText text={event.reason} />
          </>
        )}
        {!Number.isNaN(lastSeen) && (
          <>
            {' · last seen '}
            <Age ms={lastSeen} className="" /> ago
          </>
        )}
      </p>
    </>
  );
}

function Details({ event }: { event: ClusterCachedDataEvent }) {
  const type = event.type || 'Unknown';
  const object = [event.involvedKind, event.involvedName].filter(Boolean).join(' ');
  return (
    <DetailCard title="Details">
      <dl className="flex flex-col gap-2 p-3">
        <DetailRow
          label="Type"
          title={visibleTitle(type)}
          className={event.type === 'Warning' ? TONE.attention.text : undefined}
        >
          <VisibleText text={type} />
        </DetailRow>
        <DetailRow label="Reason" title={visibleTitle(event.reason)}>
          <VisibleText text={event.reason || '—'} />
        </DetailRow>
        {/* Plain text: opening the object needs its uid and apiVersion, which events do not carry. */}
        <DetailRow label="Object" title={visibleTitle(object)} className="font-mono text-primary">
          <VisibleText text={object || '—'} />
        </DetailRow>
        <DetailRow label="Namespace" title={visibleTitle(event.involvedNamespace)}>
          <VisibleText text={event.involvedNamespace || '—'} />
        </DetailRow>
        <DetailRow label="Count">{event.count}</DetailRow>
        <SeenRow label="First seen" ms={timestampMs(event.firstSeen)} />
        <SeenRow label="Last seen" ms={lastSeenMs(event)} />
      </dl>
    </DetailCard>
  );
}

const OTHER_GRID = 'grid grid-cols-[120px_minmax(0,1fr)_40px_56px] gap-x-3 px-3';

// The events about the same object, newest first. An event names its object by kind,
// namespace and name alone, so an earlier object of the same name shares them.
function OtherEvents({
  event,
  events,
  onOpen,
}: {
  event: ClusterCachedDataEvent;
  events: readonly ClusterCachedDataEvent[];
  onOpen: (uid: string) => void;
}) {
  const others = useMemo(
    () =>
      sortRows(
        events.filter(
          (e) =>
            e.uid !== event.uid &&
            e.involvedKind === event.involvedKind &&
            e.involvedNamespace === event.involvedNamespace &&
            e.involvedName === event.involvedName,
        ),
        lastSeenSortValue,
        false,
      ),
    [events, event],
  );
  return (
    <DetailCard
      title={
        <>
          Other events on <VisibleText text={event.involvedName} />
        </>
      }
      note={others.length}
    >
      {others.length === 0 ? (
        <p className="px-3 py-2.5 text-xs text-muted-foreground">No other events on this object.</p>
      ) : (
        <>
          <div className={cn(OTHER_GRID, 'h-8.5 items-center border-b text-xs font-medium text-muted-foreground')}>
            <span>Reason</span>
            <span>Message</span>
            <span>Count</span>
            <span>Last seen</span>
          </div>
          <ul>
            {others.map((e) => (
              <li key={e.uid} className="border-b last:border-b-0">
                <button
                  type="button"
                  onClick={() => onOpen(e.uid)}
                  className={cn(OTHER_GRID, 'h-9.5 w-full items-center text-left text-xs')}
                >
                  <span className="truncate text-foreground" title={visibleTitle(e.reason)}>
                    <VisibleText text={e.reason || '—'} />
                  </span>
                  <span className="truncate text-muted-foreground" title={visibleTitle(e.message)}>
                    <VisibleText text={e.message || '—'} />
                  </span>
                  <span className="text-muted-foreground tabular-nums">{countText(e.count)}</span>
                  <Age ms={lastSeenMs(e)} className="text-foreground tabular-nums" />
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
    </DetailCard>
  );
}
