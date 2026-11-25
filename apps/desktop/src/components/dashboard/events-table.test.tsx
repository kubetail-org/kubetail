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
import { describe, expect, it } from 'vitest';

import { stubLayout } from '@/test-utils';

import { EventsTable } from './events-table';

// EventsTable is pure presentation over the rows its page hands it: each test draws one
// of the four states (no active cache, connecting, empty, live rows) directly.

function evt(over: Record<string, unknown> = {}) {
  return {
    uid: 'e1',
    type: 'Warning',
    reason: 'BackOff',
    message: 'Back-off restarting failed container',
    count: 3,
    firstSeen: '2026-07-20T10:00:00Z',
    lastSeen: '2026-07-20T10:00:00Z',
    involvedKind: 'Pod',
    involvedNamespace: 'default',
    involvedName: 'my-pod',
    ...over,
  };
}

// Rows render only inside a laid-out viewport (see VirtualTable).
stubLayout(400);

describe('EventsTable', () => {
  it('explains the unsynced state when there is no active cache', () => {
    render(<EventsTable events={[]} active={false} phase="connecting" />);
    expect(screen.getByText(/No synced cache/i)).toBeInTheDocument();
  });

  it('shows a spinner while connecting', () => {
    render(<EventsTable events={[]} active phase="connecting" />);
    expect(screen.getByText(/Loading events/i)).toBeInTheDocument();
  });

  it('shows an empty state on a connected empty snapshot', () => {
    render(<EventsTable events={[]} active phase="live" />);
    expect(screen.getByText('No events.')).toBeInTheDocument();
  });

  it('names the namespace in a scoped empty state', () => {
    render(<EventsTable events={[]} active phase="live" namespace={'a\u200bb'} />);
    expect(screen.getByText(/No events in/)).toHaveTextContent('No events in a\\u{200B}b.');
  });

  it('renders a row with the event fields and a kubectl-style involved reference', () => {
    render(<EventsTable events={[evt()]} active phase="live" />);
    expect(screen.getByText('Warning')).toBeInTheDocument();
    expect(screen.getByText('BackOff')).toBeInTheDocument();
    expect(screen.getByText('Pod default/my-pod')).toBeInTheDocument();
    expect(screen.getByText('Back-off restarting failed container')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
  });

  it('omits the namespace from the involved reference for a name-only object', () => {
    render(
      <EventsTable
        events={[evt({ involvedKind: 'Node', involvedNamespace: '', involvedName: 'node-1' })]}
        active
        phase="live"
      />,
    );
    expect(screen.getByText('Node node-1')).toBeInTheDocument();
  });

  it('labels an event with an empty type (open Kubernetes field) as Unknown', () => {
    render(<EventsTable events={[evt({ type: '' })]} active phase="live" />);
    expect(screen.getByText('Unknown')).toBeInTheDocument();
  });

  it('renders "—" for a null lastSeen instead of an ancient date', () => {
    render(<EventsTable events={[evt({ lastSeen: null })]} active phase="live" />);
    // The Last Seen cell falls back to the em dash; no year-0001 date leaks through.
    expect(screen.getByText('—')).toBeInTheDocument();
    expect(screen.queryByText(/0001/)).not.toBeInTheDocument();
  });

  it('renders the live rows through the virtualized, fixed-layout table', () => {
    render(<EventsTable events={[evt()]} active phase="live" />);
    document.querySelectorAll('table').forEach((t) => expect(t).toHaveClass('table-fixed'));
  });
});
