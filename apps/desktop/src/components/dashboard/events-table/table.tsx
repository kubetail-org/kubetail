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

// The Events page's body, over the rows its page reads from `useClusterCachedDataEvents`:
// type tabs, a search and a sortable table, or, with an event open, a list of the same rows
// beside that event's detail. Gates on `active`/`phase` as every whole-screen watch view does.
// See docs/read-contracts/README.md
import { Spinner } from '@kubetail/ui/elements/spinner';
import { cn } from '@kubetail/ui/lib/utils';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { chevronColumn } from '@/components/dashboard/chevron-column';
import { ColumnPicker } from '@/components/dashboard/column-picker';
import { TALL_ROW } from '@/components/dashboard/row-size';
import { TABLE_CELLS, TABLE_HEADER } from '@/components/dashboard/table-style';
import { VirtualTable } from '@/components/dashboard/virtual-table';
import type { TableSort } from '@/components/dashboard/virtual-table';
import { ResizeHandle } from '@/components/primitives/resize-handle';
import { SearchBox } from '@/components/primitives/text-entry';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import { usePersistedWidth } from '@/lib/persisted-width';
import { sortRows } from '@/lib/sort-rows';
import { arrange, useTableColumns } from '@/lib/table-columns';

import { DEFAULT_SORT, EVENT_COLUMNS, OBJECT_COLUMN, lastSeenSortValue, rowKey } from './columns';
import type { EventColumn } from './columns';
import { EventDetail } from './detail';
import { EventsList } from './list';
import { TypeTabs, inTab, tabCounts } from './type-tabs';
import type { EventTab } from './type-tabs';

export type EventsTableProps = {
  // The watch's rows, already filtered to `namespace`.
  events: ClusterCachedDataEvent[];
  active: boolean;
  phase: SubscriptionPhase;
  // The namespace the rows are filtered to, if any.
  namespace?: string;
  // The event open in the detail, and the press that opens one or, with none, closes it.
  openUid?: string;
  onOpen: (uid: string | undefined) => void;
};

// The list pane's width. The detail keeps at least DETAIL_MIN_PX beside it, so on a row too
// narrow for both the list goes under its minimum, to nothing if it must.
const LIST_WIDTH = { min: 240, max: Number.POSITIVE_INFINITY, initial: 320 };
const DETAIL_MIN_PX = 360;
// The 16px strip between the panes, its grip centred on the list pane's edge: the pane draws
// the strip's first half, the detail's margin the second, so the handle on that edge sits
// under the grip.
const HALF_STRIP_PX = 8;

// A case-insensitive substring of the reason, the message or the involved object's kind,
// name or namespace. Spaces at the ends are ignored.
function matchesEvent(e: ClusterCachedDataEvent, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return [e.reason, e.message, e.involvedKind, e.involvedName, e.involvedNamespace].some((text) =>
    text.toLowerCase().includes(q),
  );
}

// Object is never hidden: it is the row.
function isShown(c: EventColumn, choices: Readonly<Record<string, boolean>>): boolean {
  return c === OBJECT_COLUMN || (choices[c.key] ?? !c.hiddenByDefault);
}

export function EventsTable({ events, active, phase, namespace, openUid, onOpen }: EventsTableProps) {
  const { choices, changed, setShown, setOrder, reset } = useTableColumns({ apiVersion: 'v1', resource: 'events' });
  const allColumns = useMemo(() => arrange(EVENT_COLUMNS, choices.order), [choices.order]);
  const open = useCallback((e: ClusterCachedDataEvent) => onOpen(e.uid), [onOpen]);
  // The chevron stays out of the picker: always drawn, always last.
  const columns = useMemo(
    () => [
      ...allColumns.filter((c) => isShown(c, choices.shown)),
      chevronColumn({ nameOf: (e: ClusterCachedDataEvent) => e.involvedName, onOpen: open }),
    ],
    [allColumns, choices.shown, open],
  );

  // The search narrows the rows first; the tabs count and bucket what it leaves.
  const [query, setQuery] = useState('');
  const matched = useMemo(() => events.filter((e) => matchesEvent(e, query)), [events, query]);
  const counts = useMemo(() => tabCounts(matched), [matched]);
  const [tab, setTab] = useState<EventTab>('all');
  const tabbed = useMemo(() => matched.filter((e) => inTab(e, tab)), [matched, tab]);

  const [sort, setSort] = useState<TableSort>(DEFAULT_SORT);
  // A sort by a column the user has hidden reads as the default.
  const sortValue = columns.find((c) => c.key === sort.key)?.sortValue;
  const shownSort = sortValue ? sort : DEFAULT_SORT;
  const rows = useMemo(
    () => sortRows(tabbed, sortValue ?? lastSeenSortValue, shownSort.desc),
    [tabbed, sortValue, shownSort.desc],
  );
  // A press sorts by the column, the next reverses it.
  const onSort = (key: string) => {
    const first = Boolean(columns.find((c) => c.key === key)?.descFirst);
    setSort(shownSort.key === key ? { key, desc: !shownSort.desc } : { key, desc: first });
  };

  const [listWidth, setListWidth] = usePersistedWidth('dashboard-events-list-width', LIST_WIDTH);
  const listRef = useRef<HTMLDivElement>(null);
  const listBounds = useCallback(() => {
    const max = Math.max(0, listRef.current!.parentElement!.clientWidth - DETAIL_MIN_PX - HALF_STRIP_PX);
    return { min: Math.min(LIST_WIDTH.min + HALF_STRIP_PX, max), max };
  }, []);
  const onListResize = useCallback((px: number) => setListWidth(px - HALF_STRIP_PX), [setListWidth]);

  // Closing the detail removes the control that had focus, so the search takes it.
  const searchRef = useRef<HTMLInputElement>(null);
  const focusSearch = useRef(false);
  const onClose = useCallback(() => {
    focusSearch.current = true;
    onOpen(undefined);
  }, [onOpen]);
  useEffect(() => {
    if (!focusSearch.current || openUid !== undefined) return;
    focusSearch.current = false;
    searchRef.current?.focus();
  });

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

  // An open event stays on screen to say it is gone, even when it was the last.
  if (events.length === 0 && openUid === undefined) {
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

  const tabs = <TypeTabs value={tab} onChange={setTab} counts={counts} />;
  const search = (
    <SearchBox
      ref={searchRef}
      aria-label="Filter events"
      placeholder="Search"
      value={query}
      onChange={(e) => setQuery(e.target.value)}
      onClear={() => setQuery('')}
    />
  );

  if (openUid !== undefined) {
    return (
      <div className="-mr-2.5 flex min-h-0 flex-1">
        <div
          ref={listRef}
          className="relative flex min-w-0 shrink-0 flex-col gap-3"
          style={{
            width: listWidth + HALF_STRIP_PX,
            maxWidth: `calc(100% - ${DETAIL_MIN_PX + HALF_STRIP_PX}px)`,
            paddingRight: HALF_STRIP_PX,
          }}
        >
          {/* The tabs scroll sideways in a list narrower than they are. */}
          <div className="shrink-0 overflow-x-auto">{tabs}</div>
          {search}
          <EventsList rows={rows} inScope={events.length} openUid={openUid} onOpen={onOpen} />
          <div
            aria-hidden
            className="pointer-events-none absolute inset-y-0 -right-2 flex w-4 items-center justify-center"
          >
            <div className="h-8 w-1 rounded-full bg-muted-foreground" />
          </div>
          <ResizeHandle
            edge="right"
            label="Resize events list"
            bounds={listBounds}
            onResize={onListResize}
            className="-right-2 w-4"
          />
        </div>
        <EventDetail
          key={openUid}
          className="ml-2 min-w-0 flex-1"
          event={events.find((e) => e.uid === openUid)}
          events={events}
          settled={phase === 'live'}
          onOpen={onOpen}
          onClose={onClose}
        />
      </div>
    );
  }

  // The default sort's column can be hidden, so its header is read off every column.
  const sortedBy = EVENT_COLUMNS.find((c) => c.key === shownSort.key)!.header;
  return (
    <>
      <div className="mb-3">{tabs}</div>
      {/* The table's own controls, on the row over it: the search left, the picker right. */}
      <div className="mb-3 flex items-center gap-2">
        {search}
        <div className="ml-auto">
          <ColumnPicker
            columns={allColumns.map((c) => ({
              key: c.key,
              header: c.header,
              shown: isShown(c, choices.shown),
              hideable: c !== OBJECT_COLUMN,
            }))}
            onToggle={setShown}
            onReorder={setOrder}
            onReset={reset}
            changed={changed}
          />
        </div>
      </div>
      {/* A tab or a search that leaves no row draws the table empty, its footer counting zero. */}
      <div
        className={cn(
          'flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border bg-background shadow-sm',
          TABLE_CELLS,
        )}
      >
        <VirtualTable
          rows={rows}
          rowKey={rowKey}
          columns={columns}
          rowSize={TALL_ROW}
          headerClassName={TABLE_HEADER}
          sort={shownSort}
          onSort={onSort}
        />
        <div className="flex justify-between gap-4 border-t bg-muted px-4 py-2.5 text-xs text-muted-foreground">
          <span>
            Showing {rows.length} of {events.length} events
          </span>
          <span>Sorted by {sortedBy.toLowerCase()}</span>
        </div>
      </div>
    </>
  );
}
