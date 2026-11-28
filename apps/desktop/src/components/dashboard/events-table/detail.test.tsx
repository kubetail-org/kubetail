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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';

import { EventDetail } from './detail';

const NOW = new Date(2026, 6, 20, 10, 0, 0).getTime();

function evt(over: Partial<ClusterCachedDataEvent> = {}): ClusterCachedDataEvent {
  return {
    uid: 'e1',
    type: 'Warning',
    reason: 'BackOff',
    message: 'Back-off restarting\nfailed container',
    count: 3,
    firstSeen: new Date(2026, 6, 20, 9, 4).toISOString(),
    lastSeen: new Date(2026, 6, 20, 9, 59, 48).toISOString(),
    involvedKind: 'Pod',
    involvedNamespace: 'default',
    involvedName: 'my-pod',
    ...over,
  };
}

function show(event: ClusterCachedDataEvent, events: ClusterCachedDataEvent[] = [event], onOpen = vi.fn()) {
  render(<EventDetail event={event} events={events} settled onOpen={onOpen} onClose={vi.fn()} />);
  return onOpen;
}

const heading = () => screen.getByRole('heading', { level: 2 });
const card = (title: RegExp) => screen.getByRole('heading', { level: 3, name: title }).closest('section')!;
const detailValue = (label: string) => within(card(/^Details$/)).getByText(label).nextElementSibling as HTMLElement;

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
});
afterEach(() => vi.useRealTimers());

describe('EventDetail', () => {
  it('heads a Warning with the object’s name, the pill and when it was last seen', () => {
    show(evt());
    expect(heading()).toHaveTextContent('my-pod');
    expect(heading()).toHaveClass('font-mono');
    expect(screen.getByText('Warning', { selector: '[data-tone] *' }).closest('[data-tone]')).toHaveAttribute(
      'data-tone',
      'pending',
    );
    expect(screen.getByText(/^Event/)).toHaveTextContent('Event · BackOff · last seen 12s ago');
  });

  it('leaves out the pill, a missing reason and a missing time', () => {
    show(evt({ type: 'Normal', reason: '', lastSeen: null }));
    expect(document.querySelector('[data-tone]')).toBeNull();
    expect(screen.getByText(/^Event/)).toHaveTextContent(/^Event$/);
  });

  it('draws the whole message, wrapping, with invisible characters spelled', () => {
    show(evt({ message: 'line one\nline​two' }));
    const message = within(card(/^Message$/)).getByText(/line one/);
    expect(message).toHaveClass('whitespace-pre-wrap', 'wrap-break-word');
    expect(message.querySelector('mark')).toBeInTheDocument();
  });

  it('lists the details', () => {
    show(evt());
    expect(detailValue('Type')).toHaveTextContent('Warning');
    expect(detailValue('Type')).toHaveClass('text-amber-600');
    expect(detailValue('Reason')).toHaveTextContent('BackOff');
    expect(detailValue('Object')).toHaveTextContent('Pod my-pod');
    expect(detailValue('Object')).toHaveClass('font-mono', 'text-primary');
    expect(detailValue('Object').querySelector('a')).toBeNull();
    expect(detailValue('Namespace')).toHaveTextContent('default');
    expect(detailValue('Count')).toHaveTextContent('3');
    expect(detailValue('First seen')).toHaveTextContent('2026-07-20 09:04 · 56m ago');
    expect(detailValue('First seen')).toHaveAttribute('title', '2026-07-20 09:04');
    expect(detailValue('Last seen')).toHaveTextContent('2026-07-20 09:59 · 12s ago');
  });

  it('fills in what an event leaves out', () => {
    show(
      evt({
        type: '',
        reason: '',
        message: '',
        involvedKind: '',
        involvedName: '',
        involvedNamespace: '',
        firstSeen: null,
        lastSeen: null,
      }),
    );
    expect(detailValue('Type')).toHaveTextContent('Unknown');
    expect(detailValue('Type')).toHaveClass('text-foreground');
    for (const label of ['Reason', 'Object', 'Namespace', 'First seen', 'Last seen']) {
      expect(detailValue(label)).toHaveTextContent('—');
    }
    expect(within(card(/^Message$/)).getByText('—')).toBeInTheDocument();
    expect(detailValue('First seen')).not.toHaveAttribute('title');
  });

  it('titles a detail with its value spelled', () => {
    show(evt({ involvedNamespace: 'de​fault' }));
    expect(detailValue('Namespace')).toHaveAttribute('title', 'de\\u{200B}fault');
  });

  it('lists the other events on the same object, newest first, and opens one', () => {
    const open = evt({ uid: 'open' });
    const older = evt({
      uid: 'older',
      reason: 'Pulled',
      message: 'm1',
      count: 1,
      lastSeen: new Date(2026, 6, 20, 9, 0).toISOString(),
    });
    const newer = evt({
      uid: 'newer',
      reason: 'Started',
      message: 'm2',
      count: 4,
      lastSeen: new Date(2026, 6, 20, 9, 58).toISOString(),
    });
    const elsewhere = [
      evt({ uid: 'kind', involvedKind: 'Deployment' }),
      evt({ uid: 'ns', involvedNamespace: 'shop' }),
      evt({ uid: 'name', involvedName: 'other' }),
    ];
    const onOpen = show(open, [older, open, ...elsewhere, newer]);
    const others = card(/^Other events on my-pod/);
    expect(within(others).getByText('2')).toBeInTheDocument();
    const rows = within(others).getAllByRole('button');
    expect(rows.map((r) => r.textContent)).toEqual(['Startedm2×42m', 'Pulledm111h']);
    fireEvent.click(rows[1]);
    expect(onOpen).toHaveBeenCalledWith('older');
  });

  it('dashes another event’s missing reason and message', () => {
    show(evt(), [evt(), evt({ uid: 'other', reason: '', message: '' })]);
    const [reason, message] = within(card(/^Other events/)).getByRole('button').children;
    expect(reason).toHaveTextContent('—');
    expect(message).toHaveTextContent('—');
  });

  it('draws the open event’s newest body', () => {
    const props = { events: [], settled: true, onOpen: vi.fn(), onClose: vi.fn() };
    const { rerender } = render(<EventDetail event={evt({ count: 3 })} {...props} />);
    rerender(<EventDetail event={evt({ count: 4 })} {...props} />);
    expect(detailValue('Count')).toHaveTextContent('4');
    rerender(<EventDetail event={undefined} {...props} settled={false} />);
    expect(detailValue('Count')).toHaveTextContent('4');
  });

  it('says when no other event is on the object', () => {
    show(evt());
    const others = card(/^Other events on my-pod/);
    expect(within(others).getByText('0')).toBeInTheDocument();
    expect(within(others).getByText('No other events on this object.')).toBeInTheDocument();
  });

  it('holds the event while unsettled rows lack it, and says it is gone once they settle', () => {
    const e = evt();
    const props = { events: [], onOpen: vi.fn(), onClose: vi.fn() };
    const { rerender } = render(<EventDetail event={e} settled {...props} />);
    rerender(<EventDetail event={undefined} settled={false} {...props} />);
    expect(screen.getByRole('heading', { level: 3, name: 'Details' })).toBeInTheDocument();
    rerender(<EventDetail event={undefined} settled {...props} />);
    expect(screen.getByText('This event no longer exists.')).toBeInTheDocument();
    expect(heading()).toHaveTextContent('my-pod');
  });

  it('stays gone through a reconnect once settled rows lack it, until it comes back', () => {
    const e = evt();
    const props = { events: [], onOpen: vi.fn(), onClose: vi.fn() };
    const { rerender } = render(<EventDetail event={e} settled {...props} />);
    rerender(<EventDetail event={undefined} settled {...props} />);
    rerender(<EventDetail event={undefined} settled={false} {...props} />);
    expect(screen.getByText('This event no longer exists.')).toBeInTheDocument();
    rerender(<EventDetail event={e} settled={false} {...props} />);
    expect(screen.queryByText('This event no longer exists.')).not.toBeInTheDocument();
  });

  it('takes focus as it opens', () => {
    show(evt());
    expect(screen.getByRole('region', { name: 'Event' })).toHaveFocus();
  });

  it('titles a time with its age as the pointer arrives', () => {
    show(evt());
    expect(detailValue('First seen')).toHaveAttribute('title', '2026-07-20 09:04');
    fireEvent.pointerEnter(detailValue('First seen'));
    expect(detailValue('First seen')).toHaveAttribute('title', '2026-07-20 09:04 · 56m ago');
  });

  it('closes on Escape from inside it, and on its X', () => {
    const onClose = vi.fn();
    render(<EventDetail event={evt()} events={[]} settled onOpen={vi.fn()} onClose={onClose} />);
    const close = screen.getByRole('button', { name: 'Close event' });
    fireEvent.keyDown(close, { key: 'Escape' });
    fireEvent.keyDown(close, { key: 'Enter' });
    fireEvent.click(close);
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});
