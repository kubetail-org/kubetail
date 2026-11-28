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
import { Button } from '@kubetail/ui/elements/button';
import { cn } from '@kubetail/ui/lib/utils';
import { X } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';

import { Age, timestampMs } from '@/components/dashboard/age';
import { StatusPill } from '@/components/dashboard/status-pill';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import { TONE } from '@/lib/cluster-health';
import { humanDuration } from '@/lib/human-duration';
import { sortRows } from '@/lib/sort-rows';
import { visibleTitle } from '@/lib/visible-text';

import { countText, lastSeenMs, lastSeenSortValue } from './columns';

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

// The caller keys the detail on the open event's uid, so what it holds is that event's, and
// it mounts on each event opened.
export function EventDetail({ event, events, settled, onOpen, onClose, className }: EventDetailProps) {
  // The last body seen, drawn through a reconnect and once the event is gone.
  const [held, setHeld] = useState(event);
  if (event && event !== held) setHeld(event);
  const shown = event ?? held;
  // Once settled rows lack the event it stays gone through a reconnect, until it comes back.
  const [gone, setGone] = useState(false);
  if (event && gone) setGone(false);
  if (!event && settled && !gone) setGone(true);

  // The control that opened it is gone or elsewhere, so focus moves here, where Escape closes it.
  const ref = useRef<HTMLElement>(null);
  useEffect(() => ref.current!.focus(), []);

  return (
    // oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions -- it hears Escape from the controls inside it, which keep their own keys
    <section
      ref={ref}
      aria-label="Event"
      tabIndex={-1}
      className={cn('flex min-h-0 flex-col gap-3 outline-none', className)}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose();
      }}
    >
      <header className="flex shrink-0 items-start gap-2 pr-2.5">
        <div className="min-w-0 flex-1">{shown && <Title event={shown} />}</div>
        <Button type="button" variant="ghost" size="icon-sm" aria-label="Close event" onClick={onClose}>
          <X className="size-4" aria-hidden />
        </Button>
      </header>
      {gone && <p className="pr-2.5 text-xs text-muted-foreground">This event no longer exists.</p>}
      {shown && !gone && (
        <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto pr-2.5">
          <Card title="Message">
            <p className="p-3 text-xs/4.5 wrap-break-word whitespace-pre-wrap text-foreground">
              <VisibleText text={shown.message || '—'} trailing="lines" />
            </p>
          </Card>
          <Details event={shown} />
          <OtherEvents event={shown} events={events} onOpen={onOpen} />
        </div>
      )}
    </section>
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

function Card({ title, count, children }: { title: ReactNode; count?: number; children: ReactNode }) {
  return (
    <section className="shrink-0 overflow-hidden rounded-lg border">
      <header className="flex h-10 items-center justify-between gap-2 border-b bg-card px-3 py-2.5">
        <h3 className="truncate text-[13px]/4.75 font-semibold">{title}</h3>
        {count !== undefined && <span className="text-xs text-muted-foreground tabular-nums">{count}</span>}
      </header>
      {children}
    </section>
  );
}

const pad = (n: number) => String(n).padStart(2, '0');

// A time as the user's clock reads it, to the minute: 2026-07-20 10:04.
function localMinute(ms: number): string {
  const d = new Date(ms);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function Row({
  label,
  title,
  className,
  onPointerEnter,
  children,
}: {
  label: string;
  title?: string;
  className?: string;
  onPointerEnter?: () => void;
  children: ReactNode;
}) {
  return (
    <div className="flex gap-2 text-xs/4.5">
      <dt className="w-30 shrink-0 text-muted-foreground">{label}</dt>
      <dd
        className={cn('min-w-0 flex-1 truncate', className ?? 'text-foreground')}
        title={title}
        onPointerEnter={onPointerEnter}
      >
        {children}
      </dd>
    </div>
  );
}

// A moment, and how long ago it was, ticking. A title cannot tick, so it reads the clock as
// the pointer arrives.
function SeenRow({ label, ms }: { label: string; ms: number }) {
  const [hoveredAt, setHoveredAt] = useState<number>();
  if (Number.isNaN(ms)) return <Row label={label}>—</Row>;
  const at = localMinute(ms);
  const title = hoveredAt === undefined ? at : `${at} · ${humanDuration(hoveredAt - ms)} ago`;
  return (
    <Row label={label} title={title} onPointerEnter={() => setHoveredAt(Date.now())}>
      {at} · <Age ms={ms} className="" /> ago
    </Row>
  );
}

function Details({ event }: { event: ClusterCachedDataEvent }) {
  const type = event.type || 'Unknown';
  const object = [event.involvedKind, event.involvedName].filter(Boolean).join(' ');
  return (
    <Card title="Details">
      <dl className="flex flex-col gap-2 p-3">
        <Row
          label="Type"
          title={visibleTitle(type)}
          className={event.type === 'Warning' ? TONE.attention.text : undefined}
        >
          <VisibleText text={type} />
        </Row>
        <Row label="Reason" title={visibleTitle(event.reason)}>
          <VisibleText text={event.reason || '—'} />
        </Row>
        {/* Plain text: opening the object needs its uid and apiVersion, which events do not carry. */}
        <Row label="Object" title={visibleTitle(object)} className="font-mono text-primary">
          <VisibleText text={object || '—'} />
        </Row>
        <Row label="Namespace" title={visibleTitle(event.involvedNamespace)}>
          <VisibleText text={event.involvedNamespace || '—'} />
        </Row>
        <Row label="Count">{event.count}</Row>
        <SeenRow label="First seen" ms={timestampMs(event.firstSeen)} />
        <SeenRow label="Last seen" ms={lastSeenMs(event)} />
      </dl>
    </Card>
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
    <Card
      title={
        <>
          Other events on <VisibleText text={event.involvedName} />
        </>
      }
      count={others.length}
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
    </Card>
  );
}
