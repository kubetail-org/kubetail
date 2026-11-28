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

import { act, fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import { expectCodeText, stubLayout } from '@/test-utils';

import { EventsTable } from './table';
import type { EventsTableProps } from './table';

// The table never focuses an object: the right sidebar is the kind pages' alone.
const { focusObject } = vi.hoisted(() => ({ focusObject: vi.fn() }));
vi.mock('@/lib/object-view', () => ({ useObjectView: () => ({ set: focusObject }) }));

const NOW = Date.parse('2026-07-20T10:00:00Z');

function evt(over: Partial<ClusterCachedDataEvent> = {}): ClusterCachedDataEvent {
  return {
    uid: 'e1',
    type: 'Warning',
    reason: 'BackOff',
    message: 'Back-off restarting failed container',
    count: 3,
    firstSeen: '2026-07-20T09:00:00Z',
    lastSeen: '2026-07-20T09:59:48Z',
    involvedKind: 'Pod',
    involvedNamespace: 'default',
    involvedName: 'my-pod',
    ...over,
  };
}

// The page's part: it holds the open event, as `EventsPage` does.
function Page({ initialOpen, ...props }: Omit<EventsTableProps, 'openUid' | 'onOpen'> & { initialOpen?: string }) {
  const [openUid, setOpenUid] = useState(initialOpen);
  return <EventsTable {...props} openUid={openUid} onOpen={setOpenUid} />;
}

// A namespace holding an invisible character.
const SPELLED = 'a\u200bb';

const live = (...events: ClusterCachedDataEvent[]) => ({ events, active: true, phase: 'live' as const });

const headers = () => screen.getAllByRole('columnheader').map((h) => h.textContent);
const bodyRows = () => screen.getAllByRole('row').filter((r) => r.closest('tbody'));
const footer = () => screen.getByText(/^Showing/);
const sortedBy = () => screen.getByText(/^Sorted by/);

// Rows render only inside a laid-out viewport (see VirtualTable).
stubLayout(400);

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  localStorage.clear();
  focusObject.mockClear();
});

describe('EventsTable', () => {
  describe('states', () => {
    it('explains the unsynced state when there is no active cache', () => {
      render(<Page events={[]} active={false} phase="connecting" />);
      expect(screen.getByText(/No synced cache/i)).toBeInTheDocument();
    });

    it('shows a spinner while connecting', () => {
      render(<Page events={[]} active phase="connecting" />);
      expect(screen.getByText(/Loading events/i)).toBeInTheDocument();
    });

    it('shows an empty state on a connected empty snapshot', () => {
      render(<Page {...live()} />);
      expect(screen.getByText('No events.')).toBeInTheDocument();
    });

    it('names the namespace in a scoped empty state', () => {
      render(<Page {...live()} namespace={SPELLED} />);
      expect(screen.getByText(/No events in/)).toHaveTextContent('No events in a\\u{200B}b.');
    });
  });

  describe('table', () => {
    it('draws the columns in order, then the chevron', () => {
      render(<Page {...live(evt())} />);
      expect(headers()).toEqual(['Last seen', 'Object', 'Type', 'Reason', 'Message', 'Count', '']);
      document.querySelectorAll('table').forEach((t) => expect(t).toHaveClass('table-fixed'));
    });

    it('draws each cell of a row', () => {
      render(<Page {...live(evt())} />);
      const cells = within(bodyRows()[0]).getAllByRole('cell');
      expect(cells.map((c) => c.textContent)).toEqual([
        '12s',
        'my-podPod · default',
        'Warning',
        'BackOff',
        'Back-off restarting failed container',
        '×3',
        '',
      ]);
      expect(within(cells[1]).getByText('my-pod')).toHaveClass('font-mono', 'text-primary');
      expect(within(cells[2]).getByText('Warning').closest('[data-tone]')).toHaveAttribute('data-tone', 'pending');
      expect(
        within(cells[4])
          .getByText(/Back-off/)
          .closest('[title]'),
      ).toHaveAttribute('title', 'Back-off restarting failed container');
    });

    it('draws a single count as 1, a missing time and reason as dashes, and a non-Warning type as text', () => {
      render(
        <Page
          {...live(
            evt({ uid: 'a', type: 'Normal', count: 1, lastSeen: null, reason: '' }),
            evt({ uid: 'b', type: '', involvedNamespace: '', involvedKind: 'Node', involvedName: 'node-1' }),
          )}
        />,
      );
      const [a, b] = bodyRows().map((r) => within(r).getAllByRole('cell'));
      // Newest first: the one with no time sorts last.
      expect(a[1]).toHaveTextContent('node-1Node');
      expect(within(a[2]).getByText('Unknown')).toHaveClass('text-muted-foreground');
      expect(b.map((c) => c.textContent)).toEqual([
        '—',
        'my-podPod · default',
        'Normal',
        '—',
        expect.any(String),
        '1',
        '',
      ]);
      expect(b[2].querySelector('[data-tone]')).toBeNull();
    });

    it('spells invisible characters in cluster text, in the cell and its title', () => {
      render(<Page {...live(evt({ involvedName: 'my​pod', message: 'm​sg' }))} />);
      const cells = within(bodyRows()[0]).getAllByRole('cell');
      expect(cells[1].querySelector('[title]')).toHaveAttribute('title', 'my\\u{200B}pod');
      expect(cells[4].querySelector('[title]')).toHaveAttribute('title', 'm\\u{200B}sg');
      expect(cells[4].querySelector('mark')).toBeInTheDocument();
    });

    it('sorts newest first by default, and by any column on a press', () => {
      render(
        <Page
          {...live(
            evt({ uid: 'old', involvedName: 'b', count: 9, lastSeen: '2026-07-20T09:00:00Z' }),
            evt({ uid: 'new', involvedName: 'a', count: 2, lastSeen: '2026-07-20T09:59:00Z' }),
          )}
        />,
      );
      const names = () => bodyRows().map((r) => within(r).getAllByRole('cell')[1].firstChild?.textContent);
      expect(names()).toEqual(['a', 'b']);
      expect(sortedBy()).toHaveTextContent('Sorted by last seen');

      fireEvent.click(screen.getByRole('button', { name: 'Last seen' }));
      expect(names()).toEqual(['b', 'a']);

      // A count sorts largest first.
      fireEvent.click(screen.getByRole('button', { name: 'Count' }));
      expect(names()).toEqual(['b', 'a']);
      expect(sortedBy()).toHaveTextContent('Sorted by count');
      fireEvent.click(screen.getByRole('button', { name: 'Count' }));
      expect(names()).toEqual(['a', 'b']);

      fireEvent.click(screen.getByRole('button', { name: 'Object' }));
      expect(names()).toEqual(['a', 'b']);
      expect(sortedBy()).toHaveTextContent('Sorted by object');
      for (const name of ['Type', 'Reason', 'Message']) {
        fireEvent.click(screen.getByRole('button', { name }));
        expect(sortedBy()).toHaveTextContent(`Sorted by ${name.toLowerCase()}`);
      }
    });

    it.each([
      ['Object', { involvedName: '' }],
      ['Type', { type: '' }],
      ['Reason', { reason: '' }],
      ['Message', { message: '' }],
    ])('sorts an event with no %s last', (name, over) => {
      render(<Page {...live(evt({ uid: 'empty', count: 7, ...over }), evt({ uid: 'full', count: 2 }))} />);
      fireEvent.click(screen.getByRole('button', { name }));
      expect(bodyRows().map((r) => within(r).getAllByRole('cell')[5].textContent)).toEqual(['×2', '×7']);
    });

    it('reads the default sort as last seen while that column is hidden', () => {
      localStorage.setItem('kubetail:table-columns:v1/events', JSON.stringify({ shown: { lastSeen: false } }));
      render(<Page {...live(evt())} />);
      expect(sortedBy()).toHaveTextContent('Sorted by last seen');
    });

    it('counts the tabs after the search, and shows a tab’s rows', () => {
      render(
        <Page
          {...live(
            evt({ uid: 'w1', type: 'Warning', reason: 'BackOff', message: '' }),
            evt({ uid: 'w2', type: 'Warning', reason: 'Failed', message: '' }),
            evt({ uid: 'n1', type: 'Normal', reason: 'Pulled', message: '' }),
            evt({ uid: 'x1', type: 'Custom', reason: 'Other', message: '' }),
          )}
        />,
      );
      const tab = (name: RegExp) => screen.getByRole('tab', { name });
      expect(tab(/^All/)).toHaveTextContent('All4');
      expect(tab(/^Warning/)).toHaveTextContent('Warning2');
      expect(tab(/^Normal/)).toHaveTextContent('Normal1');
      expect(within(tab(/^Warning/)).getByText('2')).toHaveClass('text-amber-600');
      expect(within(tab(/^All/)).getByText('4')).toHaveClass('text-muted-foreground');
      expect(within(tab(/^Normal/)).getByText('1')).toHaveClass('text-muted-foreground');
      expect(footer()).toHaveTextContent('Showing 4 of 4 events');

      fireEvent.click(tab(/^Warning/));
      expect(footer()).toHaveTextContent('Showing 2 of 4 events');
      fireEvent.click(tab(/^Normal/));
      expect(footer()).toHaveTextContent('Showing 1 of 4 events');

      fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'back' } });
      expect(tab(/^All/)).toHaveTextContent('All1');
      expect(tab(/^Normal/)).toHaveTextContent('Normal0');
      expect(within(tab(/^Warning/)).getByText('1')).toHaveClass('text-amber-600');
      fireEvent.click(tab(/^All/));
      fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'pulled' } });
      expect(within(tab(/^Warning/)).getByText('0')).toHaveClass('text-muted-foreground');
    });

    it.each([
      ['reason', { reason: 'NodeNotReady' }, 'nodenotready'],
      ['message', { message: 'disk pressure' }, 'DISK'],
      ['kind', { involvedKind: 'StatefulSet' }, 'statefulset'],
      ['name', { involvedName: 'checkout-api' }, 'checkout'],
      ['namespace', { involvedNamespace: 'payments' }, ' payments '],
    ])('matches the search on the %s', (_, over, query) => {
      render(
        <Page
          {...live(
            evt(over),
            evt({
              uid: 'other',
              reason: 'x',
              message: 'y',
              involvedKind: 'k',
              involvedName: 'n',
              involvedNamespace: 'ns',
            }),
          )}
        />,
      );
      fireEvent.change(screen.getByRole('searchbox'), { target: { value: query } });
      expect(footer()).toHaveTextContent('Showing 1 of 2 events');
    });

    it('clears the search on Escape', async () => {
      const user = userEvent.setup();
      render(<Page {...live(evt())} />);
      const search = screen.getByRole('searchbox');
      expectCodeText(search);
      await user.type(search, 'zzz');
      expect(footer()).toHaveTextContent('Showing 0 of 1 events');
      await user.keyboard('{Escape}');
      expect(search).toHaveValue('');
    });

    it('keeps the column picker’s choices under the events key, Object always shown', async () => {
      const user = userEvent.setup();
      const { unmount } = render(<Page {...live(evt())} />);
      await user.click(screen.getByRole('button', { name: 'Columns' }));
      const boxes = await screen.findAllByRole('checkbox');
      expect(boxes.map((c) => c.closest('label')?.textContent)).toEqual([
        'Last seen',
        'Object',
        'Type',
        'Reason',
        'Message',
        'Count',
      ]);
      expect(screen.getByRole('checkbox', { name: 'Object' })).toHaveAttribute('aria-disabled', 'true');
      await user.click(screen.getByRole('checkbox', { name: 'Message' }));
      await user.keyboard('{Escape}');
      expect(headers()).not.toContain('Message');
      expect(JSON.parse(localStorage.getItem('kubetail:table-columns:v1/events')!).shown).toEqual({ message: false });
      unmount();

      render(<Page {...live(evt())} />);
      expect(headers()).toEqual(['Last seen', 'Object', 'Type', 'Reason', 'Count', '']);
    });

    it('reads a sort by a hidden column as the default', async () => {
      localStorage.setItem('kubetail:table-columns:v1/events', JSON.stringify({ shown: { count: false } }));
      render(<Page {...live(evt())} />);
      fireEvent.click(screen.getByRole('button', { name: 'Reason' }));
      expect(sortedBy()).toHaveTextContent('Sorted by reason');
      const user = userEvent.setup();
      await user.click(screen.getByRole('button', { name: 'Columns' }));
      await user.click(await screen.findByRole('checkbox', { name: 'Reason' }));
      await user.keyboard('{Escape}');
      expect(sortedBy()).toHaveTextContent('Sorted by last seen');
    });
  });

  describe('detail', () => {
    const detail = () => screen.queryByRole('region', { name: 'Event' });
    const heading = () => within(detail()!).getByRole('heading', { level: 2 });

    it('opens from a row’s chevron, never the right sidebar, and the list replaces the table', () => {
      render(<Page {...live(evt({ uid: 'a', involvedName: 'web' }), evt({ uid: 'b', involvedName: 'db' }))} />);
      fireEvent.click(screen.getByRole('button', { name: 'Open web' }));
      expect(detail()).toBeInTheDocument();
      expect(heading()).toHaveTextContent('web');
      expect(focusObject).not.toHaveBeenCalled();
      // No header, no picker, no sort control beside the detail.
      expect(screen.queryByRole('button', { name: 'Columns' })).not.toBeInTheDocument();
      expect(screen.queryAllByRole('columnheader')).toHaveLength(0);
      expect(footer()).toHaveTextContent('Showing 2 of 2 events');
    });

    it('opens another event from its list row, which it marks', () => {
      render(
        <Page
          initialOpen="a"
          {...live(
            evt({ uid: 'a', involvedName: 'web', reason: 'A' }),
            evt({ uid: 'b', involvedName: 'db', reason: 'B' }),
          )}
        />,
      );
      const row = (reason: string) =>
        screen.getByText(reason, { selector: 'span.block *, span.block' }).closest('button')!;
      expect(row('A')).toHaveAttribute('aria-current', 'true');
      expect(row('B')).not.toHaveAttribute('aria-current');
      fireEvent.click(row('B'));
      expect(heading()).toHaveTextContent('db');
      expect(row('B')).toHaveAttribute('aria-current', 'true');
      expect(row('B')).toHaveClass('bg-sidebar-accent');
    });

    it('closes with its X, and with Escape from inside it', () => {
      render(<Page initialOpen="a" {...live(evt({ uid: 'a' }))} />);
      fireEvent.click(screen.getByRole('button', { name: 'Close event' }));
      expect(detail()).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole('button', { name: 'Open my-pod' }));
      fireEvent.keyDown(within(detail()!).getByText('Details'), { key: 'Escape' });
      expect(detail()).not.toBeInTheDocument();
    });

    it('stays open across a tab, a search and a sort that leave it out of the list', () => {
      render(
        <Page
          initialOpen="w"
          {...live(
            evt({ uid: 'w', type: 'Warning', involvedName: 'web' }),
            evt({ uid: 'n', type: 'Normal', involvedName: 'db' }),
          )}
        />,
      );
      fireEvent.click(screen.getByRole('tab', { name: /^Normal/ }));
      fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'db' } });
      expect(footer()).toHaveTextContent('Showing 1 of 2 events');
      expect(heading()).toHaveTextContent('web');
    });

    it('dashes a list row’s missing reason', () => {
      render(<Page initialOpen="e1" {...live(evt({ reason: '' }))} />);
      expect(bodyRows()[0].querySelector('span.block')).toHaveTextContent('—');
    });

    it('keeps the table’s sort in the list', () => {
      render(
        <Page
          {...live(
            evt({ uid: 'a', reason: 'Old', lastSeen: '2026-07-20T09:00:00Z' }),
            evt({ uid: 'b', reason: 'New', lastSeen: '2026-07-20T09:59:00Z' }),
          )}
        />,
      );
      fireEvent.click(screen.getByRole('button', { name: 'Last seen' }));
      fireEvent.click(screen.getAllByRole('button', { name: 'Open my-pod' })[0]);
      const reasons = bodyRows().map((r) => r.querySelector('span.block')?.textContent);
      expect(reasons).toEqual(['Old', 'New']);
    });

    it('says the event no longer exists once a settled snapshot lacks it, holding it through a reconnect', () => {
      const a = evt({ uid: 'a' });
      const b = evt({ uid: 'b', involvedName: 'other' });
      const { rerender } = render(<Page initialOpen="a" {...live(a, b)} />);
      rerender(<Page initialOpen="a" events={[b]} active phase="reconnecting" />);
      expect(within(detail()!).getByText('Details')).toBeInTheDocument();
      expect(screen.queryByText('This event no longer exists.')).not.toBeInTheDocument();
      rerender(<Page initialOpen="a" {...live(b)} />);
      expect(screen.getByText('This event no longer exists.')).toBeInTheDocument();
      expect(within(detail()!).queryByText('Details')).not.toBeInTheDocument();
      expect(heading()).toHaveTextContent('my-pod');
      expect(within(detail()!).getByRole('button', { name: 'Close event' })).toBeInTheDocument();
    });

    it('says the last event in scope is gone rather than that there are none', () => {
      const { rerender } = render(<Page initialOpen="a" {...live(evt({ uid: 'a' }))} />);
      rerender(<Page initialOpen="a" {...live()} />);
      expect(screen.getByText('This event no longer exists.')).toBeInTheDocument();
      expect(screen.queryByText('No events.')).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole('button', { name: 'Close event' }));
      expect(screen.getByText('No events.')).toBeInTheDocument();
    });

    describe('focus', () => {
      it('moves into the detail as it opens from a chevron, and to the search as it closes', async () => {
        const user = userEvent.setup();
        render(<Page {...live(evt())} />);
        await user.click(screen.getByRole('button', { name: 'Open my-pod' }));
        expect(detail()).toHaveFocus();
        await user.keyboard('{Escape}');
        expect(detail()).not.toBeInTheDocument();
        expect(screen.getByRole('searchbox')).toHaveFocus();
      });

      it('moves into the next event’s detail opened from Other events', async () => {
        const user = userEvent.setup();
        render(<Page initialOpen="a" {...live(evt({ uid: 'a', reason: 'A' }), evt({ uid: 'b', reason: 'B' }))} />);
        const others = screen.getByRole('heading', { level: 3, name: /^Other events/ }).closest('section')!;
        await user.click(within(others).getByRole('button'));
        expect(detail()).toHaveFocus();
        expect(within(detail()!).getByText(/^Event/)).toHaveTextContent('Event · B');
      });
    });

    describe('list width', () => {
      const KEY = 'kubetail:dashboard-events-list-width';
      const handle = () => screen.getByRole('separator', { name: 'Resize events list' });
      const pane = () => handle().parentElement!;
      // jsdom lays nothing out: the row is `rowWidth` wide and the pane as wide as it says.
      const layOut = (rowWidth: number) => {
        Object.defineProperty(pane().parentElement!, 'clientWidth', { configurable: true, value: rowWidth });
        vi.spyOn(pane(), 'getBoundingClientRect').mockImplementation(
          () => ({ width: Number.parseFloat(pane().style.width) }) as DOMRect,
        );
      };
      const press = (key: string) =>
        act(() => {
          fireEvent.keyDown(handle(), { key });
        });

      it.each([
        ['missing', null],
        ['corrupt', 'wide'],
      ])('opens at 320px with storage %s', (_, stored) => {
        if (stored !== null) localStorage.setItem(KEY, stored);
        render(<Page initialOpen="e1" {...live(evt())} />);
        // The pane draws the first half of the 16px strip beside it.
        expect(pane().style.width).toBe('328px');
      });

      it('opens at 320px when storage cannot be read', () => {
        vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
          throw new Error('denied');
        });
        render(<Page initialOpen="e1" {...live(evt())} />);
        expect(pane().style.width).toBe('328px');
      });

      it('resizes within 240px and the width that leaves the detail 360px, and keeps it', () => {
        render(<Page initialOpen="e1" {...live(evt())} />);
        layOut(1000);
        press('ArrowRight');
        expect(pane().style.width).toBe('344px');
        expect(localStorage.getItem(KEY)).toBe('336');

        // 1000 − 360 for the detail − 8 for its half of the strip.
        for (let i = 0; i < 30; i += 1) press('ArrowRight');
        expect(pane().style.width).toBe('632px');
        for (let i = 0; i < 40; i += 1) press('ArrowLeft');
        expect(pane().style.width).toBe('248px');
        expect(localStorage.getItem(KEY)).toBe('240');
        expect(pane().style.maxWidth).toBe('calc(100% - 368px)');
      });

      it('lets the detail win on a row too narrow for both panes', () => {
        render(<Page initialOpen="e1" {...live(evt())} />);
        // 500 − 368 leaves the list 132px, under its 240px minimum; the cap draws it so.
        layOut(500);
        expect(pane().style.maxWidth).toBe('calc(100% - 368px)');
        const separator = screen.getByRole('separator', { name: 'Resize events list' });
        fireEvent.focus(separator);
        expect(separator).toHaveAttribute('aria-valuemin', '132');
        expect(separator).toHaveAttribute('aria-valuemax', '132');
        // Narrower than the detail alone, the list has no room at all.
        layOut(300);
        fireEvent.focus(separator);
        expect(separator).toHaveAttribute('aria-valuemax', '0');
        expect(pane().firstElementChild).toHaveClass('overflow-x-auto');
      });

      it('drags from anywhere on the 16px strip', () => {
        render(<Page initialOpen="e1" {...live(evt())} />);
        expect(screen.getByRole('separator', { name: 'Resize events list' })).toHaveClass('-right-2', 'w-4');
      });

      it('reads the kept width back', () => {
        localStorage.setItem(KEY, '400');
        render(<Page initialOpen="e1" {...live(evt())} />);
        expect(pane().style.width).toBe('408px');
      });
    });
  });
});
