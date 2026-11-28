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
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import type { ProblemPod } from '@/lib/overview';

const { focusObject } = vi.hoisted(() => ({ focusObject: vi.fn() }));
vi.mock('@/lib/object-view', () => ({ useObjectView: () => ({ set: focusObject }) }));

const { NeedsAttention } = await import('./needs-attention');
const { bodyRows, renderOnDashboard } = await import('./testutil');

function problem(name: string, label = 'CrashLoopBackOff', tone: 'failing' | 'pending' = 'failing'): ProblemPod {
  const object = {
    uid: `u-${name}`,
    namespace: 'shop',
    name,
    rawJSON: { metadata: { ownerReferences: [{ kind: 'StatefulSet', name: 'db', controller: true }] } },
  } as ClusterCachedDataObject;
  return { object, status: { label, tone }, sinceMs: Date.now() - 4 * 60 * 60 * 1000 };
}

const rows = () => bodyRows('Needs attention');

describe('NeedsAttention', () => {
  it('draws each pod with its owner, namespace, problem and since', async () => {
    await renderOnDashboard(
      <NeedsAttention problems={[problem('db-0'), problem('db-1', 'ContainerCreating', 'pending')]} />,
    );
    expect(screen.getByRole('region', { name: 'Needs attention' })).toHaveTextContent(/^Needs attention2/);
    const cells = within(rows()[0]).getAllByRole('cell');
    // jsdom draws both layouts: the narrow card's namespace on the owner line, and the wide
    // card's Namespace and Since columns.
    expect(cells.map((c) => c.textContent)).toEqual([
      'db-0shop · Pod · StatefulSet db',
      'shop',
      'CrashLoopBackOff',
      '4h',
      '',
    ]);
    expect(within(rows()[1]).getByText('ContainerCreating').closest('[data-tone]')).toHaveAttribute(
      'data-tone',
      'pending',
    );
  });

  // A 320px page leaves the card too narrow for the fixed columns.
  it('folds Namespace and Since away in a narrow card, keeping the problem and the menu', async () => {
    await renderOnDashboard(<NeedsAttention problems={[problem('db-0')]} />);
    const headers = screen.getAllByRole('columnheader');
    const wideOnly = headers.filter((h) => h.classList.contains('hidden')).map((h) => h.textContent);
    expect(wideOnly).toEqual(['Namespace', 'Since']);
    expect(within(rows()[0]).getByText('shop ·', { exact: false })).toHaveClass('@xl:hidden');
  });

  it('draws a dash for a pod with no time to count from', async () => {
    await renderOnDashboard(<NeedsAttention problems={[{ ...problem('db-0'), sinceMs: Number.NaN }]} />);
    expect(within(rows()[0]).getAllByRole('cell')[3]).toHaveTextContent('—');
  });

  it('opens a row’s actions menu onto the pod', async () => {
    const user = userEvent.setup();
    const pod = problem('db-0');
    await renderOnDashboard(<NeedsAttention problems={[pod]} />);
    await user.click(screen.getByRole('button', { name: 'Actions for db-0' }));
    await user.click(await screen.findByRole('menuitem', { name: 'View logs' }));
    expect(focusObject).toHaveBeenCalledWith(
      { apiVersion: 'v1', resource: 'pods', kind: 'Pod', uid: 'u-db-0', namespace: 'shop', name: 'db-0' },
      'logs',
    );
  });

  it('opens the Pods page when it has more than it shows', async () => {
    await renderOnDashboard(<NeedsAttention problems={['a', 'b', 'c', 'd'].map((n) => problem(n))} />);
    expect(rows()).toHaveLength(3);
    expect(screen.getByRole('link', { name: 'Open Pods' })).toHaveAttribute('href', '/dashboard?resource=pods');
  });

  it('says when nothing needs attention', async () => {
    await renderOnDashboard(<NeedsAttention problems={[]} />);
    expect(screen.getByRole('region', { name: 'Needs attention' })).toHaveTextContent(/^Needs attention0/);
    expect(rows().map((r) => r.textContent)).toEqual(['Nothing needs attention']);
  });

  it('claims nothing while the cache is still syncing', async () => {
    await renderOnDashboard(<NeedsAttention problems={[]} syncing />);
    expect(rows().map((r) => r.textContent)).toEqual(['No problem pods found yet']);
  });

  it('waits for the pods behind a spinner', async () => {
    await renderOnDashboard(<NeedsAttention />);
    expect(screen.getByText('Loading…')).toBeInTheDocument();
  });
});
