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

import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { ClusterCachedDataEvent } from '@/lib/cluster-cached-data-events';
import { TONE } from '@/lib/cluster-health';

import { RecentWarnings } from './recent-warnings';
import { bodyRows, renderOnDashboard } from './testutil';

function warning(uid: string, extra: Partial<ClusterCachedDataEvent> = {}): ClusterCachedDataEvent {
  return {
    uid,
    type: 'Warning',
    reason: 'BackOff',
    message: '',
    count: 1,
    firstSeen: null,
    lastSeen: new Date(Date.now() - 6 * 60 * 1000).toISOString(),
    involvedKind: 'Pod',
    involvedNamespace: 'shop',
    involvedName: uid,
    ...extra,
  };
}

const rows = () => bodyRows('Recent warnings');

describe('RecentWarnings', () => {
  it('draws each warning’s reason, object and last seen', async () => {
    await renderOnDashboard(
      <RecentWarnings
        warnings={[warning('web-1'), warning('node-1', { involvedKind: 'Node', involvedNamespace: '' })]}
      />,
    );
    expect(screen.getByRole('region', { name: 'Recent warnings' })).toHaveTextContent(/^Recent warningsLast hour · 2/);
    expect(rows().map((r) => r.textContent)).toEqual(['BackOffweb-16m', 'BackOffnode-16m']);
    expect(within(rows()[0]).getByText('BackOff').closest('span')).toHaveClass(TONE.error.text);
    expect(screen.getByText('web-1').closest('[title]')).toHaveAttribute('title', 'Pod shop/web-1');
    expect(screen.getByText('node-1').closest('[title]')).toHaveAttribute('title', 'Node node-1');
  });

  it('always links to the Events page', async () => {
    await renderOnDashboard(<RecentWarnings warnings={[]} />);
    expect(screen.getByRole('link', { name: 'Open Events' })).toHaveAttribute('href', '/dashboard?resource=events');
    expect(rows().map((r) => r.textContent)).toEqual(['No warnings in the last hour']);
  });

  it('claims no quiet hour while the cache is still syncing', async () => {
    await renderOnDashboard(<RecentWarnings warnings={[]} syncing />);
    expect(rows().map((r) => r.textContent)).toEqual(['No warnings found yet']);
  });

  it('draws a dash for a warning never seen', async () => {
    await renderOnDashboard(<RecentWarnings warnings={[warning('web-1', { lastSeen: null })]} />);
    expect(rows()[0]).toHaveTextContent('BackOffweb-1—');
  });

  it('waits behind a spinner', async () => {
    await renderOnDashboard(<RecentWarnings />);
    expect(screen.getByText('Loading…')).toBeInTheDocument();
  });
});
