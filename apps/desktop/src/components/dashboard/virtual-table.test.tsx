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

import { fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { stubLayout } from '@/test-utils';

import { ROW_H, ROW_H_PX, TALL_ROW } from './row-size';
import { VirtualTable } from './virtual-table';
import type { VirtualColumn } from './virtual-table';

// A viewport ten rows tall.
const VIEW_H = 10 * ROW_H_PX;
stubLayout(VIEW_H);

type Row = { id: string };
const rowKey = (r: Row) => r.id;
const columns: VirtualColumn<Row>[] = [{ key: 'id', header: 'ID', className: 'w-32', cell: (r) => r.id }];
const rowsOf = (n: number, from = 0): Row[] => Array.from({ length: n }, (_, i) => ({ id: `r${from + i}` }));

function renderTable(rows: Row[]) {
  const view = render(<VirtualTable rows={rows} rowKey={rowKey} columns={columns} />);
  const scroller = view.container.querySelector<HTMLDivElement>('[tabindex="0"]')!;
  const rerender = (next: Row[]) => view.rerender(<VirtualTable rows={next} rowKey={rowKey} columns={columns} />);
  const scrollTo = (top: number) => {
    scroller.scrollTop = top;
    fireEvent.scroll(scroller);
  };
  return { scroller, rerender, scrollTo };
}

// Data rows only: the spacer rows have no cells.
const bodyRows = () => screen.getAllByRole('row').filter((r) => r.querySelector('td'));
const spacers = () => Array.from(document.querySelectorAll<HTMLTableRowElement>('tbody > tr:not(:has(td))'));

describe('VirtualTable', () => {
  it('renders only the rows in the window, between two spacer rows', () => {
    renderTable(rowsOf(10_000));
    const rows = bodyRows();
    expect(rows.length).toBeGreaterThan(10);
    expect(rows.length).toBeLessThan(100);
    expect(rows[0]).toHaveTextContent('r0');
    rows.forEach((r) => expect(r).toHaveClass(ROW_H));
    const [top, bottom] = spacers();
    expect(top.style.height).toBe('0px');
    expect(bottom.style.height).toBe(`${(10_000 - rows.length) * ROW_H_PX}px`);
  });

  it('lays the table out fixed, with each column carrying its class on header and cells', () => {
    renderTable(rowsOf(3));
    document.querySelectorAll('table').forEach((t) => expect(t).toHaveClass('table-fixed'));
    expect(screen.getByRole('columnheader', { name: 'ID' })).toHaveClass('w-32');
    expect(screen.getByText('r1')).toHaveClass('w-32');
  });

  it('sizes each column of both tables from its width, and holds the table to the widths and the slack column’s minimum', () => {
    const sized: VirtualColumn<Row>[] = [
      { key: 'a', header: 'A', width: 100, cell: (r) => r.id },
      { key: 'b', header: 'B', minWidth: 240, cell: (r) => r.id },
      { key: 'c', header: 'C', width: 60, cell: (r) => r.id },
      { key: 'd', header: 'D', cell: (r) => r.id },
    ];
    render(<VirtualTable rows={rowsOf(1)} rowKey={rowKey} columns={sized} />);
    // Header and body share the widths: the body's first row is a spacer with no cells.
    document.querySelectorAll('table').forEach((t) => {
      expect(Array.from(t.querySelectorAll('col')).map((c) => c.style.width)).toEqual(['100px', '', '60px', '']);
      expect(t.style.minWidth).toBe('400px');
    });
  });

  // Home/End/PageUp/PageDown are the browser's on a focused overflow container.
  it('pins a pinned column’s cells to the right edge, and no other', () => {
    const pinned: VirtualColumn<Row>[] = [
      ...columns,
      { key: 'more', header: '', width: 40, pinned: true, cell: () => 'more' },
    ];
    render(<VirtualTable rows={rowsOf(1)} rowKey={rowKey} columns={pinned} />);
    const [id, more] = within(bodyRows()[0]).getAllByRole('cell');
    expect(more).toHaveClass('sticky', 'bg-background');
    expect(more).toHaveStyle({ right: '0px' });
    expect(id).not.toHaveClass('sticky');
    expect(screen.getByTestId('pinned-header-patch')).toHaveStyle({ width: '40px' });
  });

  it('stacks pinned columns from the right edge, each beside the ones after it', () => {
    const pinned: VirtualColumn<Row>[] = [
      ...columns,
      { key: 'open', header: '', width: 30, pinned: true, cell: () => 'open' },
      { key: 'more', header: '', width: 40, pinned: true, cell: () => 'more' },
    ];
    render(<VirtualTable rows={rowsOf(1)} rowKey={rowKey} columns={pinned} />);
    const cells = within(bodyRows()[0]).getAllByRole('cell');
    expect(cells.at(-2)).toHaveStyle({ right: '40px' });
    expect(cells.at(-1)).toHaveStyle({ right: '0px' });
    expect(screen.getByTestId('pinned-header-patch')).toHaveStyle({ width: '70px' });
  });

  it('draws no header patch without a pinned column or a scrollbar', () => {
    renderTable(rowsOf(1));
    expect(screen.queryByTestId('pinned-header-patch')).toBeNull();
  });

  // The patch and a focused header button's scroll are measured by the pinned columns' widths.
  it('takes a pinned column only with a width', () => {
    // @ts-expect-error -- a pinned column without a width
    const unsized: VirtualColumn<Row> = { key: 'more', header: '', pinned: true, cell: () => 'more' };
    const sized: VirtualColumn<Row> = { ...unsized, width: 40 };
    expect(sized.width).toBe(40);
  });

  // Its header sits under the patch, where neither text nor a sort button would show.
  it('takes a pinned column only without header text or a sort', () => {
    // @ts-expect-error -- a pinned column with header text
    const titled: VirtualColumn<Row> = { key: 'more', header: 'More', width: 40, pinned: true, cell: () => 'more' };
    // @ts-expect-error -- a pinned column that sorts
    const sorted: VirtualColumn<Row> = {
      key: 'more',
      header: '',
      width: 40,
      pinned: true,
      cell: () => 'more',
      sortValue: () => 1,
    };
    expect([titled.pinned, sorted.pinned]).toEqual([true, true]);
  });

  it('refuses a pinned column before an ordinary one', () => {
    // React logs the render's error before it rethrows.
    const logged = vi.spyOn(console, 'error').mockImplementation(() => {});
    const misordered: VirtualColumn<Row>[] = [
      { key: 'more', header: '', width: 40, pinned: true, cell: () => 'more' },
      ...columns,
    ];
    expect(() => render(<VirtualTable rows={rowsOf(1)} rowKey={rowKey} columns={misordered} />)).toThrow(
      'pinned columns must come after every other column',
    );
    logged.mockRestore();
  });

  it('makes the scroller focusable', () => {
    const { scroller } = renderTable(rowsOf(3));
    expect(scroller).toHaveAttribute('tabindex', '0');
  });

  it('moves the window with the scroll offset', () => {
    const { scrollTo } = renderTable(rowsOf(10_000));
    scrollTo(5_000 * ROW_H_PX);
    const rows = bodyRows();
    expect(rows[0]).toHaveTextContent('r4990'); // 10 rows of overscan above the viewport
    expect(rows.at(-1)).toHaveTextContent('r5019');
    const [top, bottom] = spacers();
    expect(top.style.height).toBe(`${4_990 * ROW_H_PX}px`);
    expect(bottom.style.height).toBe(`${(10_000 - 5_020) * ROW_H_PX}px`);
  });

  // A filter or a tab, with the scroller far down the longer list.
  it('draws the rows a list cut short still has', () => {
    const { scrollTo, rerender } = renderTable(rowsOf(10_000));
    scrollTo(5_000 * ROW_H_PX);
    rerender(rowsOf(3));
    expect(bodyRows().map((r) => r.textContent)).toEqual(['r0', 'r1', 'r2']);
    expect(spacers().map((s) => s.style.height)).toEqual(['0px', '0px']);
  });

  describe('when the rows change under the viewport', () => {
    // The user is reading r500 at the top of a 1,000-row list.
    const parked = () => {
      const view = renderTable(rowsOf(1_000));
      view.scrollTo(500 * ROW_H_PX);
      return view;
    };

    it('holds the view when rows are inserted above it', () => {
      const { scroller, rerender } = parked();
      rerender([...rowsOf(50, 10_000), ...rowsOf(1_000)]);
      expect(scroller.scrollTop).toBe(550 * ROW_H_PX);
    });

    it('holds the view when rows are deleted above it', () => {
      const { scroller, rerender } = parked();
      rerender(rowsOf(1_000).slice(100));
      expect(scroller.scrollTop).toBe(400 * ROW_H_PX);
    });

    // The rows below it stay where they are; a row from above fills the slot it left.
    it('holds the rows below a deleted first visible row', () => {
      const { scroller, rerender } = parked();
      rerender(rowsOf(1_000).filter((r) => r.id !== 'r500'));
      expect(scroller.scrollTop).toBe(499 * ROW_H_PX);
    });

    // A re-fired event moves from wherever it is to the top of a newest-first list.
    it('holds the view when the first visible row moves to the top', () => {
      const { scroller, rerender } = parked();
      const rows = rowsOf(1_000);
      rerender([rows[500], ...rows.filter((r) => r.id !== 'r500')]);
      expect(scroller.scrollTop).toBe(500 * ROW_H_PX);
    });

    it('follows new rows when parked at the top', () => {
      const { scroller, rerender } = renderTable(rowsOf(1_000));
      rerender([{ id: 'fresh' }, ...rowsOf(1_000)]);
      expect(scroller.scrollTop).toBe(0);
      expect(screen.getByText('fresh')).toBeInTheDocument();
    });

    // Chromium would stack its own anchoring on the correction above; WebKit has none.
    it('turns native scroll anchoring off', () => {
      const { scroller } = renderTable(rowsOf(3));
      expect(scroller).toHaveClass('[overflow-anchor:none]');
    });
  });

  it('draws each row at the height it is given, and its spacers to match', () => {
    render(<VirtualTable rows={rowsOf(1_000)} rowKey={rowKey} columns={columns} rowSize={TALL_ROW} />);
    const rows = bodyRows();
    rows.forEach((r) => expect(r).toHaveClass(TALL_ROW.className));
    expect(spacers()[1].style.height).toBe(`${(1_000 - rows.length) * TALL_ROW.px}px`);
  });

  it('makes a sortable header a button, and marks the sorted one', () => {
    const onSort = vi.fn();
    const sortable: VirtualColumn<Row>[] = [
      { key: 'id', header: 'ID', cell: (r) => r.id, sortValue: (r) => r.id },
      { key: 'plain', header: 'Plain', cell: () => '' },
    ];
    render(
      <VirtualTable
        rows={rowsOf(2)}
        rowKey={rowKey}
        columns={sortable}
        sort={{ key: 'id', desc: true }}
        onSort={onSort}
      />,
    );
    const [id, plain] = screen.getAllByRole('columnheader');
    expect(id).toHaveAttribute('aria-sort', 'descending');
    expect(plain).not.toHaveAttribute('aria-sort');
    expect(within(plain).queryByRole('button')).not.toBeInTheDocument();

    // Down lit, up muted.
    const lit = (header: HTMLElement) =>
      Array.from(header.querySelectorAll('path')).map((p) => p.classList.contains('fill-current'));
    expect(lit(id)).toEqual([false, true]);

    fireEvent.click(within(id).getByRole('button', { name: 'ID' }));
    expect(onSort).toHaveBeenCalledWith('id');
  });

  it('marks an ascending sort', () => {
    const sortable: VirtualColumn<Row>[] = [{ key: 'id', header: 'ID', cell: (r) => r.id, sortValue: (r) => r.id }];
    render(
      <VirtualTable
        rows={rowsOf(2)}
        rowKey={rowKey}
        columns={sortable}
        sort={{ key: 'id', desc: false }}
        onSort={vi.fn()}
      />,
    );
    expect(screen.getByRole('columnheader')).toHaveAttribute('aria-sort', 'ascending');
    const paths = screen.getByRole('columnheader').querySelectorAll('path');
    expect(Array.from(paths).map((p) => p.classList.contains('fill-current'))).toEqual([true, false]);
  });

  it('draws both triangles muted on a sortable header not sorted', () => {
    const sortable: VirtualColumn<Row>[] = [{ key: 'id', header: 'ID', cell: (r) => r.id, sortValue: (r) => r.id }];
    render(<VirtualTable rows={rowsOf(2)} rowKey={rowKey} columns={sortable} onSort={vi.fn()} />);
    const paths = screen.getByRole('columnheader').querySelectorAll('path');
    expect(Array.from(paths).map((p) => p.classList.contains('fill-current'))).toEqual([false, false]);
  });

  describe('header', () => {
    // A screen reader ties a cell to its column's header only within one table.
    it('shares one table with the rows', () => {
      renderTable(rowsOf(3));
      const table = screen.getByRole('table');
      expect(within(table).getByRole('columnheader', { name: 'ID' })).toBeInTheDocument();
      expect(
        within(table)
          .getAllByRole('cell')
          .map((c) => c.textContent),
      ).toEqual(['r0', 'r1', 'r2']);
      document.querySelectorAll('table').forEach((t) => expect(t).toHaveAttribute('role', 'none'));
    });

    it('sits above the scroller, with the scrollbar styled', () => {
      const { scroller } = renderTable(rowsOf(3));
      expect(scroller).toHaveClass('table-scrollbar');
      expect(within(scroller).queryByRole('columnheader')).not.toBeInTheDocument();
      expect(screen.getByRole('columnheader', { name: 'ID' })).toBeInTheDocument();
    });

    it('leaves room for the scroller’s bar', () => {
      // The scroller is 10px wider than its content: a classic bar's width.
      const offset = vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(410);
      const client = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(400);
      renderTable(rowsOf(3));
      const header = screen.getByRole('columnheader').closest('table')!.parentElement!.parentElement!.parentElement!;
      expect(header.style.paddingRight).toBe('10px');
      // The header scrolled sideways still draws in that padding, so a patch covers it.
      expect(screen.getByTestId('pinned-header-patch')).toHaveStyle({ right: '0px', width: '10px' });
      offset.mockRestore();
      client.mockRestore();
    });

    // A focused button the pinned column covers would hide its focus ring and still sort.
    describe('a focused header button', () => {
      // Beside the 40px pinned column, 150px of the 190px view is free.
      function renderWide() {
        vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(190);
        const wide: VirtualColumn<Row>[] = [
          { key: 'name', header: 'Name', width: 240, sortValue: (r) => r.id, cell: (r) => r.id },
          { key: 'age', header: 'Age', width: 100, sortValue: (r) => r.id, cell: (r) => r.id },
          { key: 'more', header: '', width: 40, pinned: true, cell: () => 'more' },
        ];
        render(<VirtualTable rows={rowsOf(1)} rowKey={rowKey} columns={wide} onSort={vi.fn()} />);
        const scroller = screen.getByRole('table').lastElementChild as HTMLElement;
        scroller.scrollLeft = 100;
        // Puts a button at `left` px from the view's left edge, `width` wide, and focuses it.
        const focusAt = (name: string, left: number, width: number) => {
          const button = screen.getByRole('button', { name: new RegExp(`^${name}`) });
          vi.spyOn(button, 'getBoundingClientRect').mockReturnValue(new DOMRect(left, 0, width, 20));
          button.focus();
        };
        return { scroller, focusAt };
      }

      afterEach(() => vi.restoreAllMocks());

      // Name's column is wider than the room, but its button is in view.
      it('moves nothing while the button shows, though its column is wider than the room', () => {
        const { scroller, focusAt } = renderWide();
        focusAt('Name', 8, 60);
        expect(scroller.scrollLeft).toBe(100);
      });

      it('scrolls by the least that brings a button under the pinned column into view', () => {
        const { scroller, focusAt } = renderWide();
        focusAt('Age', 130, 40);
        expect(scroller.scrollLeft).toBe(120);
      });

      it('scrolls back to a button left of the view', () => {
        const { scroller, focusAt } = renderWide();
        focusAt('Name', -60, 60);
        expect(scroller.scrollLeft).toBe(40);
      });

      it('shows a button wider than the room from its left edge', () => {
        const { scroller, focusAt } = renderWide();
        focusAt('Age', 20, 200);
        expect(scroller.scrollLeft).toBe(120);
      });

      // Focus scrolls the header's clipped box too; it goes back, as the header follows by transform.
      it('puts the header’s own box back', () => {
        const { focusAt } = renderWide();
        const headerBox = screen.getByRole('table').firstElementChild as HTMLElement;
        headerBox.scrollLeft = 50;
        focusAt('Age', 130, 40);
        expect(headerBox.scrollLeft).toBe(0);
      });
    });

    // The patch over the scroller's bar inherits the strip's background, so it must be opaque.
    it('paints the header strip opaque, unless the caller restyles it', () => {
      const strip = () => screen.getByTestId('pinned-header-patch').parentElement!;
      const offset = vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(410);
      const client = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(400);
      const { unmount } = render(<VirtualTable rows={rowsOf(3)} rowKey={rowKey} columns={columns} />);
      expect(strip()).toHaveClass('bg-background');
      unmount();

      render(<VirtualTable rows={rowsOf(3)} rowKey={rowKey} columns={columns} headerClassName="bg-muted" />);
      expect(strip()).toHaveClass('bg-muted');
      expect(strip()).not.toHaveClass('bg-background');
      offset.mockRestore();
      client.mockRestore();
    });

    it('follows the scroller sideways', () => {
      const { scroller } = renderTable(rowsOf(3));
      const moved = screen.getByRole('columnheader').closest('table')!.parentElement!.parentElement!;
      scroller.scrollLeft = 120;
      fireEvent.scroll(scroller);
      expect(moved.style.transform).toBe('translateX(-120px)');
    });
  });
});
