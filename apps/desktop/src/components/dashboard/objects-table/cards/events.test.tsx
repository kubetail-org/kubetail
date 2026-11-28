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

import { EventsCard } from './events';

const NOW = Date.parse('2026-11-22T11:14:00Z');

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
});
afterEach(() => vi.useRealTimers());

function evt(n: number, over: Partial<ClusterCachedDataEvent> = {}): ClusterCachedDataEvent {
  return {
    uid: `e${n}`,
    type: 'Normal',
    reason: `R${n}`,
    message: `message ${n}`,
    count: 1,
    firstSeen: null,
    lastSeen: new Date(NOW - n * 60_000).toISOString(),
    involvedKind: 'Pod',
    involvedNamespace: 'default',
    involvedName: 'web',
    ...over,
  };
}

function show(events: ClusterCachedDataEvent[], phase: 'live' | 'connecting' = 'live') {
  render(<EventsCard kind="Pod" namespace="default" name="web" events={events} phase={phase} />);
}

const card = () => screen.getByRole('heading', { level: 3, name: 'Events' }).closest('section')!;
const reasons = () =>
  within(card())
    .getAllByRole('row')
    .slice(1)
    .map((r) => r.children[0].textContent);

describe('EventsCard', () => {
  it('draws the events about the object alone, newest first', () => {
    show([
      evt(3),
      evt(1),
      evt(2, { involvedName: 'db' }),
      evt(4, { involvedNamespace: 'other' }),
      evt(5, { involvedKind: 'Service' }),
    ]);
    expect(reasons()).toEqual(['R1', 'R3']);
  });

  it('draws each column, a Warning’s reason in red', () => {
    show([evt(1, { type: 'Warning', count: 3, reason: 'BackOff', message: 'a​b' })]);
    const [header, row] = within(card()).getAllByRole('row');
    expect(Array.from(header.children).map((c) => c.textContent)).toEqual(['Reason', 'Message', 'Count', 'Last seen']);
    expect(Array.from(row.children).map((c) => c.textContent)).toEqual(['BackOff', 'a\\u{200B}b', '×3', '1m']);
    expect(within(row).getByText('BackOff')).toHaveClass('text-destructive');
    expect(row.children[1].firstElementChild).toHaveAttribute('title', 'a\\u{200B}b');
  });

  it('dashes a missing reason and message', () => {
    show([evt(1, { reason: '', message: '' })]);
    const [, row] = within(card()).getAllByRole('row');
    expect(row.children[0]).toHaveTextContent('—');
    expect(row.children[1]).toHaveTextContent('—');
  });

  it('counts the warnings in red before the total', () => {
    show([evt(1, { type: 'Warning' }), evt(2, { type: 'Warning' }), evt(3)]);
    const note = within(card()).getByText('2 warnings ·');
    expect(note).toHaveClass('text-destructive');
    expect(note.parentElement).toHaveTextContent('2 warnings · 3');
  });

  it('names one warning in the singular', () => {
    show([evt(1, { type: 'Warning' })]);
    expect(within(card()).getByText('1 warning ·')).toBeInTheDocument();
  });

  it('draws the total alone without a warning', () => {
    show([evt(1)]);
    expect(within(card()).queryByText(/warning/)).not.toBeInTheDocument();
    expect(card().querySelector('header')).toHaveTextContent('Events1');
  });

  it('draws the five newest, then shows them all on a press', () => {
    show([1, 2, 3, 4, 5, 6, 7].map((n) => evt(n)));
    expect(reasons()).toEqual(['R1', 'R2', 'R3', 'R4', 'R5']);
    fireEvent.click(within(card()).getByRole('button', { name: 'Show all 7 events' }));
    expect(reasons()).toEqual(['R1', 'R2', 'R3', 'R4', 'R5', 'R6', 'R7']);
    expect(within(card()).queryByRole('button')).not.toBeInTheDocument();
  });

  it('offers no Show all with five or fewer', () => {
    show([1, 2, 3, 4, 5].map((n) => evt(n)));
    expect(within(card()).queryByRole('button')).not.toBeInTheDocument();
  });

  it('spins while the watch connects', () => {
    show([], 'connecting');
    expect(within(card()).getByText('Loading events…')).toBeInTheDocument();
  });

  it('says when there are none', () => {
    show([evt(1, { involvedName: 'db' })]);
    expect(within(card()).getByText('No events.')).toBeInTheDocument();
  });
});
