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

// Generic per-kind object table, over the rows its page reads from
// `useClusterCachedDataObjects`. Universal columns (Namespace?/Name/Age) plus kubectl-style
// extras from the `columns/` registry, then the row's actions, in a card with a footer.
// The states render distinctly — not-synced note, connecting spinner, empty snapshot, live
// table — mirroring `EventsTable`.
import { Spinner } from '@kubetail/ui/elements/spinner';
import { Tabs, TabsList, TabsTrigger } from '@kubetail/ui/elements/tabs';
import { cn } from '@kubetail/ui/lib/utils';
import { useEffect, useMemo, useState } from 'react';

import { TALL_ROW } from '@/components/dashboard/row-size';
import { VirtualTable } from '@/components/dashboard/virtual-table';
import type { TableSort, VirtualColumn } from '@/components/dashboard/virtual-table';
import { SearchBox } from '@/components/primitives/text-entry';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import { HEALTH_TONE, TONE } from '@/lib/cluster-health';
import type { StatusTone } from '@/lib/cluster-health';
import type { PrinterColumn } from '@/lib/dashboard-resources';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import type { ObjectAction } from '@/lib/object-actions';
import { matchesFilter } from '@/lib/object-filter';
import { ownerOf } from '@/lib/object-owner';
import { sortRows } from '@/lib/sort-rows';
import { arrange, useTableColumns } from '@/lib/table-columns';

import { actionsColumn } from './actions';
import { ColumnPicker } from './column-picker';
import { Age, ageSortValue, columnsForKind, toneForKind } from './columns';

type ObjectsTableProps = {
  apiVersion: string;
  resource: string;
  kind: string;
  // The kind's plural, for the footer.
  label: string;
  namespaced: boolean;
  // A CRD's declared columns, off the kinds watch. Empty for a built-in, and for a kind whose
  // hand-written registry entry wins anyway.
  printerColumns?: readonly PrinterColumn[];
  // The watch's rows, already filtered to `namespace`.
  objects: ClusterCachedDataObject[];
  active: boolean;
  phase: SubscriptionPhase;
  // The namespace the rows are filtered to, if any.
  namespace?: string;
  onAction: (action: ObjectAction, object: ClusterCachedDataObject) => void;
};

const NO_COLUMNS: readonly PrinterColumn[] = [];

const rowKey = (o: ClusterCachedDataObject) => o.uid;

const NAMESPACE_COLUMN: VirtualColumn<ClusterCachedDataObject> = {
  key: 'namespace',
  header: 'Namespace',
  className: 'truncate',
  width: 160,
  sortValue: (o) => o.namespace,
  cell: (o) => (
    <span className="text-xs text-muted-foreground" title={o.namespace}>
      <VisibleText text={o.namespace} />
    </span>
  ),
};

// Takes the width the sized columns leave: the name over the workload that owns it. Each
// line truncates, with the full one on hover.
const NAME_COLUMN: VirtualColumn<ClusterCachedDataObject> = {
  key: 'name',
  header: 'Name',
  className: 'truncate',
  minWidth: 240,
  sortValue: (o) => o.name,
  cell: (o) => {
    const owner = ownerOf(o.rawJSON);
    return (
      <>
        <div className="truncate font-mono text-xs text-primary" title={o.name}>
          <VisibleText text={o.name} />
        </div>
        {owner && (
          <div className="truncate text-xs text-muted-foreground" title={owner}>
            <VisibleText text={owner} />
          </div>
        )}
      </>
    );
  },
};

function createdMs(o: ClusterCachedDataObject): number {
  return o.creationTimestamp ? Date.parse(o.creationTimestamp) : Number.NaN;
}

const AGE_COLUMN: VirtualColumn<ClusterCachedDataObject> = {
  key: 'age',
  header: 'Age',
  className: 'tabular-nums',
  width: 64,
  sortValue: (o) => ageSortValue(createdMs(o)),
  cell: (o) => <Age ms={createdMs(o)} />,
};

const HEADER = 'bg-muted [&_th]:h-9 [&_th]:text-xs [&_th]:text-muted-foreground';
// Tighter than the library's cells: 12px between columns, 12px at the card's edges.
const CELLS =
  '[&_td]:px-1.5 [&_th]:px-1.5 [&_td:first-child]:pl-3 [&_th:first-child]:pl-3 [&_td:last-child]:pr-3 [&_th:last-child]:pr-3';

// Name is never hidden: it is the row.
function isShown(c: VirtualColumn<ClusterCachedDataObject>, choices: Readonly<Record<string, boolean>>): boolean {
  return c === NAME_COLUMN || (choices[c.key] ?? !c.hiddenByDefault);
}

// The status tabs, in order, for a kind whose objects have a tone.
type Bucket = 'all' | StatusTone;
const BUCKETS: readonly Bucket[] = ['all', 'ok', 'failing', 'pending'];
const BUCKET_LABEL: Record<Bucket, string> = { all: 'All', ok: 'Healthy', failing: 'Failing', pending: 'Pending' };

export function ObjectsTable({
  apiVersion,
  resource,
  kind,
  label,
  namespaced,
  printerColumns = NO_COLUMNS,
  objects,
  active,
  phase,
  namespace,
  onAction,
}: ObjectsTableProps) {
  // Under a namespace every row would repeat it.
  const showNamespace = namespaced && namespace === undefined;
  // Kind-specific columns between Name and Age; [] when neither the registry nor the kind
  // itself declares any. Memoized so the sort below reruns only on new rows or a new sort.
  const defaultColumns = useMemo(
    () => [
      ...(showNamespace ? [NAMESPACE_COLUMN] : []),
      NAME_COLUMN,
      ...columnsForKind({ apiVersion, resource }, printerColumns),
      AGE_COLUMN,
    ],
    [showNamespace, apiVersion, resource, printerColumns],
  );
  // The user's order and choices, else each column's default.
  const { choices, changed, setShown, setOrder, reset } = useTableColumns({ apiVersion, resource });
  const allColumns = useMemo(() => arrange(defaultColumns, choices.order), [defaultColumns, choices.order]);
  // The actions stay out of the picker: always drawn, always last.
  const columns = useMemo(
    () => [...allColumns.filter((c) => isShown(c, choices.shown)), actionsColumn({ apiVersion, resource }, onAction)],
    [allColumns, choices.shown, apiVersion, resource, onAction],
  );
  const namespaceShown = showNamespace && isShown(NAMESPACE_COLUMN, choices.shown);

  // The text filter narrows the rows first; the tabs count and bucket what it leaves.
  const [query, setQuery] = useState('');
  // It matches the namespace only while the column shows it: under a scope every row
  // shares one, and matching it would keep them all.
  const matched = useMemo(
    () => objects.filter((o) => matchesFilter(o, query, { namespace: namespaceShown })),
    [objects, query, namespaceShown],
  );

  const toneOf = toneForKind({ apiVersion, resource });
  const [bucket, setBucket] = useState<Bucket>('all');
  // Each row's tone is read once, for both the counts and the tab's rows.
  const tones = useMemo(() => (toneOf ? matched.map(toneOf) : undefined), [matched, toneOf]);
  const counts = useMemo(() => {
    const n: Record<Bucket, number> = { all: matched.length, ok: 0, failing: 0, pending: 0 };
    if (tones) for (const t of tones) n[t] += 1;
    return n;
  }, [matched, tones]);
  const inBucket = useMemo(
    () => (tones && bucket !== 'all' ? matched.filter((_, i) => tones[i] === bucket) : matched),
    [matched, tones, bucket],
  );

  // Unsorted is the watch's order: namespace, then name.
  const [sort, setSort] = useState<TableSort>();
  // A sort by a column the table no longer draws, such as Namespace once a namespace is
  // picked, reads as unsorted.
  const sortColumn = columns.find((c) => c.key === sort?.key);
  // A column sorted by the clock is sorted again each second, by a fresh copy of the same
  // sort: a running job overtakes a finished one with no new frame.
  const sortsByClock = Boolean(sortColumn?.sortsByClock);
  useEffect(() => {
    if (!sortsByClock) return undefined;
    const id = setInterval(() => setSort((s) => s && { ...s }), 1000);
    return () => clearInterval(id);
  }, [sortsByClock]);
  const rows = useMemo(
    () => (sort && sortColumn?.sortValue ? sortRows(inBucket, sortColumn.sortValue, sort.desc) : inBucket),
    [inBucket, sort, sortColumn],
  );
  // A press sorts by the column, the next reverses it, the next unsorts.
  const onSort = (key: string) => {
    const first = Boolean(columns.find((c) => c.key === key)?.descFirst);
    if (sort?.key !== key) setSort({ key, desc: first });
    else if (sort.desc === first) setSort({ key, desc: !first });
    else setSort(undefined);
  };

  // No active cache streams nothing — say so rather than spin forever.
  if (!active) {
    return (
      <p className="text-sm text-muted-foreground">
        No synced cache for this cluster yet — {kind.toLowerCase()} objects appear once syncing has started.
      </p>
    );
  }

  // Held until the snapshot's Bookmark lands, so the empty state below can only be
  // reached once the kind is genuinely known to have no objects.
  if (phase === 'connecting') {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Spinner size="sm" />
        Loading {kind.toLowerCase()}…
      </div>
    );
  }

  if (objects.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No {kind.toLowerCase()} objects
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

  const sortedBy = sortColumn?.header ?? (namespaceShown ? 'Namespace' : 'Name');
  const plural = label.toLowerCase();
  return (
    <>
      {toneOf && (
        <Tabs value={bucket} onValueChange={(value) => setBucket(value as Bucket)} className="mb-3">
          <TabsList aria-label={`${label} by status`}>
            {BUCKETS.map((b) => (
              <TabsTrigger key={b} value={b} className="gap-1.5 px-2.5">
                {BUCKET_LABEL[b]}
                <span
                  className={cn(
                    'text-xs font-normal tabular-nums',
                    // A count above zero takes its tone's colour; All and a zero stay muted.
                    b !== 'all' && counts[b] > 0 ? TONE[HEALTH_TONE[b]].text : 'text-muted-foreground',
                  )}
                >
                  {counts[b]}
                </span>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      )}
      {/* The table's own controls, on the row over it: the filter left, the picker right. */}
      <div className="mb-3 flex items-center gap-2">
        <SearchBox
          aria-label={`Filter ${plural}`}
          placeholder="Search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Escape') setQuery('');
          }}
        />
        <div className="ml-auto">
          <ColumnPicker
            columns={allColumns.map((c) => ({
              key: c.key,
              header: c.header,
              shown: isShown(c, choices.shown),
              hideable: c !== NAME_COLUMN,
            }))}
            onToggle={setShown}
            onReorder={setOrder}
            onReset={reset}
            changed={changed}
          />
        </div>
      </div>
      {/* A tab or a query that leaves no row draws the table empty, its footer counting zero. */}
      <div
        className={cn('flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border bg-background shadow-sm', CELLS)}
      >
        <VirtualTable
          rows={rows}
          rowKey={rowKey}
          columns={columns}
          rowSize={TALL_ROW}
          headerClassName={HEADER}
          sort={sortColumn && sort}
          onSort={onSort}
        />
        <div className="flex justify-between gap-4 border-t bg-muted px-4 py-2.5 text-xs text-muted-foreground">
          <span>
            Showing {rows.length} of {objects.length} {plural}
          </span>
          <span>
            Sorted by <VisibleText text={sortedBy.toLowerCase()} />
          </span>
        </div>
      </div>
    </>
  );
}
