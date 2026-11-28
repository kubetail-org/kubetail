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

// The frame each of the Overview's tables sits in: a titled header, a row of column headers,
// at most `ROW_LIMIT` rows, and a link to the page with the rest.
import { cn } from '@kubetail/ui/lib/utils';
import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';

import type { DashboardResource } from '@/lib/dashboard-resources';

import { Loading } from './loading';

const ROW_LIMIT = 3;

// `className` goes on the column's header and cells, such as a container variant hiding it
// in a narrow card.
export type CardColumn<T> = { key: string; header: string; className?: string; cell: (row: T) => ReactNode };

type OverviewCardProps<T> = {
  title: string;
  // The header's right-hand note, drawn once the rows have loaded.
  count: ReactNode;
  // The columns' `grid-template-columns`, as literal classes so Tailwind sees them. The card
  // is a container, so a `@<size>:` variant reflows the columns by the card's own width.
  grid: string;
  columns: CardColumn<T>[];
  // Undefined while loading.
  rows: readonly T[] | undefined;
  rowKey: (row: T) => string;
  empty: string;
  // The footer's link, shown when rows are cut, or `always`.
  link: { label: string; resource: DashboardResource; always?: boolean };
};

export function OverviewCard<T>({ title, count, grid, columns, rows, rowKey, empty, link }: OverviewCardProps<T>) {
  const row = cn('grid items-center gap-3 border-t border-border px-3 py-2.5', grid);
  return (
    <section
      aria-label={title}
      className="@container flex min-w-0 flex-col overflow-hidden rounded-lg border border-border bg-background text-xs/4.5"
    >
      <header className="flex h-10 items-center justify-between gap-3 border-b border-border bg-card px-3 py-2.5">
        <h2 className="truncate text-[13px]/[19px] font-semibold text-foreground">{title}</h2>
        {rows !== undefined && <span className="shrink-0 truncate text-muted-foreground">{count}</span>}
      </header>
      {rows === undefined ? (
        <div className="px-3 py-2.5">
          <Loading />
        </div>
      ) : (
        <div role="table" aria-label={title}>
          <div
            role="row"
            className={cn('grid h-8.5 items-center gap-3 px-3 py-2 font-medium text-muted-foreground', grid)}
          >
            {columns.map((c) => (
              <div key={c.key} role="columnheader" className={cn('truncate', c.className)}>
                {c.header}
              </div>
            ))}
          </div>
          {rows.length === 0 ? (
            <div role="row" className="border-t border-border px-3 py-2.5">
              <div role="cell" className="truncate text-muted-foreground">
                {empty}
              </div>
            </div>
          ) : (
            rows.slice(0, ROW_LIMIT).map((r) => (
              <div key={rowKey(r)} role="row" className={row}>
                {columns.map((c) => (
                  <div key={c.key} role="cell" className={cn('min-w-0 truncate', c.className)}>
                    {c.cell(r)}
                  </div>
                ))}
              </div>
            ))
          )}
        </div>
      )}
      {rows !== undefined && (link.always || rows.length > ROW_LIMIT) && (
        <Link
          to="/dashboard"
          search={(prev) => ({ ...prev, resource: link.resource })}
          className="truncate px-3 py-2.5 font-medium text-foreground hover:bg-muted/50"
        >
          {link.label}
        </Link>
      )}
    </section>
  );
}
