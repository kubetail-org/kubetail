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

import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

const { useEventsMock } = vi.hoisted(() => ({ useEventsMock: vi.fn() }));
vi.mock('@/lib/cluster-cached-data-events', () => ({ useClusterCachedDataEvents: useEventsMock }));

const { ObjectEventsView } = await import('./events-view');

const POD = { apiVersion: 'v1', resource: 'pods', kind: 'Pod', uid: 'u1', namespace: 'shop', name: 'web-1' };

const event = (over: Record<string, unknown> = {}) => ({
  uid: 'e1',
  type: 'Warning',
  reason: 'BackOff',
  message: 'Back-off restarting failed container',
  count: 1,
  firstSeen: null,
  lastSeen: new Date(Date.now() - 5 * 60_000).toISOString(),
  involvedKind: 'Pod',
  involvedNamespace: 'shop',
  involvedName: 'web-1',
  ...over,
});

const watch = (events: unknown[], phase = 'live', active = true) =>
  useEventsMock.mockReturnValue({ events, active, phase });

describe('ObjectEventsView', () => {
  it('watches the events on a connection of its own', () => {
    watch([]);
    render(<ObjectEventsView object={POD} />);
    expect(useEventsMock).toHaveBeenCalledWith({ unshared: true });
  });

  it('lists the events about the object alone, in the watch’s order', () => {
    watch([
      event({ uid: 'e2', reason: 'Pulled', type: 'Normal' }),
      event({ uid: 'x1', involvedName: 'web-2' }),
      event({ uid: 'x2', involvedNamespace: 'other' }),
      event({ uid: 'x3', involvedKind: 'Deployment' }),
      event(),
    ]);
    render(<ObjectEventsView object={POD} />);
    const items = screen.getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('Pulled');
    expect(items[1]).toHaveTextContent('Warning');
    expect(items[1]).toHaveTextContent('BackOff');
    expect(items[1]).toHaveTextContent('Back-off restarting failed container');
    expect(items[1]).toHaveTextContent('5 minutes ago');
  });

  it('counts a repeated event, and dashes a missing reason and time', () => {
    watch([event({ count: 4, reason: '', lastSeen: null, message: '' })]);
    render(<ObjectEventsView object={POD} />);
    const item = screen.getByRole('listitem');
    expect(item).toHaveTextContent('×4 · —');
    expect(item.querySelector('p')).toBeNull();
  });

  it('spells an invisible character in the cluster’s text', () => {
    watch([event({ message: 'a​b' })]);
    render(<ObjectEventsView object={POD} />);
    expect(screen.getByTitle('An invisible character')).toBeInTheDocument();
  });

  it('waits for the snapshot, then says when there are none', () => {
    watch([], 'connecting');
    const { rerender } = render(<ObjectEventsView object={POD} />);
    expect(screen.getByText('Loading…')).toBeInTheDocument();
    watch([event({ involvedName: 'web-2' })]);
    rerender(<ObjectEventsView object={POD} />);
    expect(screen.getByText('No events.')).toBeInTheDocument();
  });

  it('says so without an active cache', () => {
    watch([], 'connecting', false);
    render(<ObjectEventsView object={POD} />);
    expect(screen.getByText('No synced cache for this cluster.')).toBeInTheDocument();
  });
});
