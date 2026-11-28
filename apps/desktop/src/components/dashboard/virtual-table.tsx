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

// A table that renders only the rows on screen. Every row is one height, so the
// visible window is arithmetic over `scrollTop`, and the rows are real `<tr>`s between
// two spacer rows that stand in for everything above and below.
//
// The rows are a live watch: they shift index under the scroll offset on every frame.
// After each commit the table remembers which rows were on screen and, on the next,
// moves `scrollTop` by the smallest shift any of them made, so what the user is reading
// stays put. Native scroll anchoring is off (`overflow-anchor: none`) — Chromium's would
// stack on top of this correction; WebKit has none.
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@kubetail/ui/elements/table';
import { cn } from '@kubetail/ui/lib/utils';
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';

import type { SortValue } from '@/lib/sort-rows';

import { ROW, type RowSize } from './row-size';

// A pinned cell covers what scrolls under it, so it paints the row's hover wash over the
// page's colour itself. Its `right` is the width of the pinned columns after it.
const PINNED_CELL =
  'sticky bg-background group-hover/row:bg-[color-mix(in_oklab,var(--muted)_50%,var(--background))] group-has-aria-expanded/row:bg-[color-mix(in_oklab,var(--muted)_50%,var(--background))]';

// Rows rendered beyond each edge of the viewport. Also covers the one frame between a
// scroll correction below and the `scroll` event that recomputes the window from it.
const OVERSCAN = 10;

export type VirtualColumn<T> = {
  key: string;
  header: string;
  cell: (row: T) => ReactNode;
  // Applied to the header and every cell: alignment, truncation, tabular-nums.
  className?: string;
  minWidth?: number;
  // Left out until the user picks it in the column picker.
  hiddenByDefault?: boolean;
  // Present when the column sorts: its header becomes a button.
  sortValue?: (row: T) => SortValue;
  // Sort largest first on the first press, as for a count.
  descFirst?: boolean;
  // Its sort value reads the clock, so its order goes stale with no new rows.
  sortsByClock?: boolean;
} & (
  | {
      // The column's width in pixels. The layout is fixed, so the one column without a width
      // takes the slack; it keeps `minWidth`, and the table scrolls sideways before it would
      // go under it.
      width?: number;
      pinned?: false;
    }
  | {
      // Stays at the right edge while the rows scroll sideways. Pinned columns come after every
      // other. Their header cells sit under a patch of the header's colour, so they have no
      // header text and do not sort. Their widths are what the patch covers and what a focused
      // header button scrolls clear of.
      pinned: true;
      width: number;
      header: '';
      sortValue?: never;
      descFirst?: never;
      sortsByClock?: never;
    }
);

export type TableSort = { key: string; desc: boolean };

// A sort triangle: muted and darkening with its button's hover, or lit in the header's colour.
const triangle = (lit: boolean) =>
  lit ? 'fill-current' : 'fill-muted-foreground/40 group-hover:fill-muted-foreground';

// A sortable column's header: its name over two triangles. `desc` is the column's direction
// while the table is sorted by it, whose triangle is lit.
function SortButton({ label, desc, onClick }: { label: string; desc: boolean | undefined; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'group inline-flex items-center gap-1.5 hover:text-foreground',
        desc !== undefined && 'text-foreground',
      )}
    >
      {label}
      <svg aria-hidden viewBox="0 0 8 12" className="h-3 w-2 shrink-0">
        <path d="M4 1 7.5 5h-7z" className={triangle(desc === false)} />
        <path d="M4 11 .5 7h7z" className={triangle(desc === true)} />
      </svg>
    </button>
  );
}

function ariaSort(desc: boolean | undefined) {
  if (desc === undefined) return undefined;
  return desc ? 'descending' : 'ascending';
}

type VirtualTableProps<T> = {
  rows: readonly T[];
  // Stable across frames for the same object (`uid`); the scroll correction is keyed on it.
  // Pass a module-level function — it keys a per-frame index map.
  rowKey: (row: T) => string;
  columns: VirtualColumn<T>[];
  rowSize?: RowSize;
  // Restyles the header strip. Its background must stay opaque: the patch over the pinned
  // columns and the scroller's bar inherits it.
  headerClassName?: string;
  // Draws no header strip, for a list whose one column needs no name.
  headless?: boolean;
  // The column the caller sorted `rows` by, and the press on a sortable header.
  sort?: TableSort;
  onSort?: (key: string) => void;
};

type Seen = { key: string; index: number };

export function VirtualTable<T>({
  rows,
  rowKey,
  columns,
  rowSize = ROW,
  headerClassName,
  headless = false,
  sort,
  onSort,
}: VirtualTableProps<T>) {
  const scrollerRef = useRef<HTMLDivElement>(null);
  const headerBoxRef = useRef<HTMLDivElement>(null);
  const headerRef = useRef<HTMLDivElement>(null);
  // The scroller's vertical bar, which the header leaves room for.
  const [gutter, setGutter] = useState(0);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewHeight, setViewHeight] = useState(0);

  useEffect(() => {
    const el = scrollerRef.current!;
    const observer = new ResizeObserver(() => {
      setViewHeight(el.clientHeight);
      setGutter(el.offsetWidth - el.clientWidth);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // No further than the rows reach. A list cut short under a deep scroll would otherwise draw
  // no row, under a spacer tall enough to keep the browser from pulling the offset back.
  const top = Math.min(scrollTop, Math.max(0, rows.length * rowSize.px - viewHeight));
  const first = Math.max(0, Math.floor(top / rowSize.px) - OVERSCAN);
  const last = Math.min(rows.length, Math.ceil((top + viewHeight) / rowSize.px) + OVERSCAN);

  const indexByKey = useMemo(() => new Map(rows.map((row, i) => [rowKey(row), i])), [rows, rowKey]);

  // The rows on screen at the last commit, from the first visible one down.
  const seenRef = useRef<Seen[]>([]);
  useLayoutEffect(() => {
    const el = scrollerRef.current!;
    // Within a row of the top the user is parked there: let new rows arrive in view.
    if (el.scrollTop >= rowSize.px) {
      // Rows on screen move together when something is inserted or removed above them;
      // a row that moved on its own (an event re-fired to the top) is the outlier.
      let shift = 0;
      let best = Infinity;
      seenRef.current.forEach(({ key, index }) => {
        const now = indexByKey.get(key);
        if (now === undefined || Math.abs(now - index) >= best) return;
        shift = now - index;
        best = Math.abs(shift);
      });
      el.scrollTop += shift * rowSize.px;
    }
    const firstVisible = Math.max(first, Math.floor(el.scrollTop / rowSize.px));
    seenRef.current = [];
    for (let i = firstVisible; i < last; i += 1) seenRef.current.push({ key: rowKey(rows[i]), index: i });
  });

  const firstPinned = columns.findIndex((c) => c.pinned);
  if (firstPinned >= 0 && columns.slice(firstPinned).some((c) => !c.pinned)) {
    throw new Error('VirtualTable: pinned columns must come after every other column');
  }
  // Each pinned column's offset from the right edge: the widths of the pinned columns after it.
  const pinnedRight = new Map<string, number>();
  let pinnedWidth = 0;
  for (const c of columns.toReversed()) {
    if (!c.pinned) break;
    pinnedRight.set(c.key, pinnedWidth);
    pinnedWidth += c.width;
  }
  // Tab can land on a header button scrolled out of view or under the pinned columns, where
  // its focus ring cannot show: scroll the rows, which the header follows, by the least that
  // shows the button beside them, its left edge first when it is wider than the room. Focus
  // also scrolls the header's own clipped box, so that goes back first.
  const revealHeaderButton = (button: HTMLElement) => {
    const el = scrollerRef.current!;
    headerBoxRef.current!.scrollLeft = 0;
    const view = el.clientWidth - pinnedWidth;
    const origin = el.getBoundingClientRect().left;
    const { left, right } = button.getBoundingClientRect();
    let shift = Math.max(0, right - origin - view);
    if (left - origin - shift < 0) shift = left - origin;
    el.scrollLeft += shift;
  };
  const minWidth = columns.reduce((sum, c) => sum + (c.width ?? c.minWidth ?? 0), 0);
  // Both tables lay out from these: a fixed table takes its widths from its first row, which
  // in the body is a spacer.
  const cols = (
    <colgroup>
      {columns.map((c) => (
        <col key={c.key} style={{ width: c.width }} />
      ))}
    </colgroup>
  );
  // Two native tables would read as a table of headers with no rows and a table of rows with
  // no headers, so both give up their roles to this one, and their parts name their own.
  return (
    <div role="table" className="flex min-h-0 flex-1 flex-col">
      {/* The header is a table of its own above the scroller, so the scrollbar starts under it.
      It leaves room for the scroller's bar, so its box is as wide as the rows' view, and
      follows the scroller sideways by a transform: WebKit leaves the padding out of a box's
      scroll range, so a header scrolled to match would stop short at the far right. */}
      {!headless && (
        <div
          ref={headerBoxRef}
          className={cn('relative shrink-0 overflow-hidden bg-background', headerClassName)}
          style={{ paddingRight: gutter }}
        >
          <div ref={headerRef}>
            <Table role="none" className="table-fixed" containerClassName="overflow-x-visible" style={{ minWidth }}>
              {cols}
              <TableHeader role="rowgroup">
                <TableRow role="row">
                  {columns.map((c) => {
                    const desc = sort?.key === c.key ? sort.desc : undefined;
                    return (
                      <TableHead
                        key={c.key}
                        role="columnheader"
                        className={c.className}
                        aria-sort={ariaSort(desc)}
                        onFocus={c.pinned ? undefined : (e) => revealHeaderButton(e.target)}
                      >
                        {c.sortValue && onSort ? (
                          <SortButton label={c.header} desc={desc} onClick={() => onSort(c.key)} />
                        ) : (
                          c.header
                        )}
                      </TableHead>
                    );
                  })}
                </TableRow>
              </TableHeader>
            </Table>
          </div>
          {/* The scroller's bar sits in the header's padding, where the scrolled header still draws. */}
          {pinnedWidth + gutter > 0 && (
            <div
              data-testid="pinned-header-patch"
              aria-hidden
              className="absolute inset-y-0 bg-inherit"
              style={{ right: 0, width: pinnedWidth + gutter }}
            />
          )}
        </div>
      )}
      <div
        ref={scrollerRef}
        // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- focusable so Home/End/PageUp/PageDown reach the scroll region
        tabIndex={0}
        onScroll={(e) => {
          setScrollTop(e.currentTarget.scrollTop);
          if (headerRef.current) headerRef.current.style.transform = `translateX(${-e.currentTarget.scrollLeft}px)`;
        }}
        className="table-scrollbar flex-1 overflow-auto [overflow-anchor:none] focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      >
        {/* The library's wrapper is a scroll container of its own; this one is the scroller. */}
        <Table role="none" className="table-fixed" containerClassName="overflow-x-visible" style={{ minWidth }}>
          {cols}
          <TableBody role="rowgroup">
            <tr aria-hidden style={{ height: first * rowSize.px }} />
            {rows.slice(first, last).map((row) => (
              <TableRow key={rowKey(row)} role="row" className={cn('group/row', rowSize.className)}>
                {columns.map((c) => (
                  <TableCell
                    key={c.key}
                    role="cell"
                    className={cn(c.className, c.pinned && PINNED_CELL)}
                    style={c.pinned ? { right: pinnedRight.get(c.key) } : undefined}
                  >
                    {c.cell(row)}
                  </TableCell>
                ))}
              </TableRow>
            ))}
            <tr aria-hidden style={{ height: (rows.length - last) * rowSize.px }} />
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
