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

// A small table inside a detail card: a header row over one grid row per item, the
// columns laid out by the caller's `grid-cols-*`.
import { cn } from '@kubetail/ui/lib/utils';
import type { ReactNode } from 'react';

export type SmallColumn<T> = {
  header: string;
  cell: (row: T) => ReactNode;
  // The cell's classes beside `truncate`.
  className?: string;
};

type SmallTableProps<T> = {
  // A `grid-cols-*` class, one track per column.
  grid: string;
  columns: readonly SmallColumn<T>[];
  rows: readonly T[];
  rowKey: (row: T, index: number) => string;
  // A last row of the caller's, under the items.
  footer?: ReactNode;
};

export function SmallTable<T>({ grid, columns, rows, rowKey, footer }: SmallTableProps<T>) {
  return (
    <div role="table">
      <div role="row" className={cn('grid gap-x-3 px-3 py-2 text-xs font-medium text-muted-foreground', grid)}>
        {columns.map((c) => (
          <span key={c.header} role="columnheader" className="truncate">
            {c.header}
          </span>
        ))}
      </div>
      {rows.map((row, i) => (
        <div key={rowKey(row, i)} role="row" className={cn('grid gap-x-3 border-t px-3 py-2.5 text-xs', grid)}>
          {columns.map((c) => (
            <span key={c.header} role="cell" className={cn('min-w-0 truncate', c.className)}>
              {c.cell(row)}
            </span>
          ))}
        </div>
      ))}
      {footer}
    </div>
  );
}
