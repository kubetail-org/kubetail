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

import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { afterEach, describe, expect, it, onTestFinished, vi } from 'vitest';

import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import { expectCodeText, stubLayout } from '@/test-utils';

import { ObjectsTable } from './table';
import type { ObjectsTableProps } from './table';

// The detail's Events card reads a watch of its own.
vi.mock('@/lib/cluster-cached-data-events', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/cluster-cached-data-events')>()),
  useClusterCachedDataEvents: () => ({ events: [], active: true, phase: 'live' }),
}));

// ObjectsTable is pure presentation over the rows its page hands it. `live` is a watch
// whose snapshot has landed with these objects.
const live = (...objects: unknown[]) => ({
  objects: objects as ClusterCachedDataObject[],
  active: true,
  phase: 'live' as const,
});

const noAction = () => {};
const PODS = {
  apiVersion: 'v1',
  resource: 'pods',
  kind: 'Pod',
  label: 'Pods',
  namespaced: true,
  onAction: noAction,
  onOpen: noAction,
};
// A CRD kind with no registered columns — exercises the universal-columns fallback.
const WIDGETS = {
  apiVersion: 'example.com/v1',
  resource: 'widgets',
  kind: 'Widget',
  label: 'Widgets',
  namespaced: true,
  onAction: noAction,
  onOpen: noAction,
};

function obj(over: Record<string, unknown> = {}) {
  return {
    uid: 'u1',
    apiVersion: 'v1',
    kind: 'Pod',
    namespace: 'default',
    name: 'my-pod',
    creationTimestamp: '2026-07-20T10:00:00Z',
    rawJSON: undefined as unknown,
    ...over,
  };
}

const restarts = (n: number) => ({ spec: { containers: [{}] }, status: { containerStatuses: [{ restartCount: n }] } });

// Rows render only inside a laid-out viewport (see VirtualTable).
stubLayout(400);

describe('ObjectsTable', () => {
  it('explains the unsynced state when there is no active cache', () => {
    render(<ObjectsTable {...PODS} objects={[]} active={false} phase="connecting" />);
    expect(screen.getByText(/No synced cache/i)).toBeInTheDocument();
  });

  it('shows a spinner while connecting', () => {
    render(<ObjectsTable {...PODS} objects={[]} active phase="connecting" />);
    expect(screen.getByText(/Loading pod/i)).toBeInTheDocument();
  });

  it('shows an empty state on a connected empty snapshot', () => {
    render(<ObjectsTable {...PODS} {...live()} />);
    expect(screen.getByText('No pod objects.')).toBeInTheDocument();
  });

  it('falls back to the universal Name/Namespace/Age columns for a kind with no registry entry', () => {
    render(<ObjectsTable {...WIDGETS} {...live(obj({ kind: 'Widget' }))} />);
    const headers = screen.getAllByRole('columnheader').map((h) => h.textContent);
    expect(headers).toEqual(['Namespace', 'Name', 'Age', '']);
    expect(screen.getByText('my-pod')).toBeInTheDocument();
    expect(screen.getByText('default')).toBeInTheDocument();
  });

  it("renders a CRD's declared columns between Name and Age", () => {
    render(
      <ObjectsTable
        {...WIDGETS}
        printerColumns={[{ name: 'Replicas', type: 'integer', jsonPath: '.spec.replicas', priority: 0 }]}
        {...live(obj({ kind: 'Widget', rawJSON: { spec: { replicas: 3 } } }))}
      />,
    );

    expect(screen.getAllByRole('columnheader').map((h) => h.textContent)).toEqual([
      'Namespace',
      'Name',
      'Replicas',
      'Age',
      '',
    ]);
    expect(screen.getByText('3')).toBeInTheDocument();
  });

  // The page filters the rows; the table drops the column every row would repeat.
  it('omits the Namespace column under a namespace scope', () => {
    render(<ObjectsTable {...PODS} {...live(obj())} namespace="default" />);
    expect(screen.getAllByRole('columnheader').map((h) => h.textContent)).not.toContain('Namespace');
  });

  it('names the namespace in a scoped empty state', () => {
    render(<ObjectsTable {...PODS} {...live()} namespace={'a\u200bb'} />);
    expect(screen.getByText(/No pod objects in/)).toHaveTextContent('No pod objects in a\\u{200B}b.');
  });

  it('omits the Namespace column for a cluster-scoped kind', () => {
    render(
      <ObjectsTable
        apiVersion="v1"
        resource="nodes"
        kind="Node"
        label="Nodes"
        namespaced={false}
        onAction={noAction}
        onOpen={noAction}
        {...live(obj({ namespace: '', name: 'node-1' }))}
      />,
    );
    const headers = screen.getAllByRole('columnheader').map((h) => h.textContent);
    expect(headers).toEqual(['Name', 'Status', 'Roles', 'Version', 'Age', '']);
  });

  it('renders Pod-specific columns (Status/Ready/Restarts/Node) computed from the body', () => {
    const pod = obj({
      rawJSON: {
        spec: { nodeName: 'pool-a-2', containers: [{}, {}] },
        status: {
          phase: 'Running',
          containerStatuses: [
            { ready: true, restartCount: 1 },
            { ready: false, restartCount: 2 },
          ],
        },
      },
    });
    render(<ObjectsTable {...PODS} {...live(pod)} />);
    const headers = screen.getAllByRole('columnheader').map((h) => h.textContent);
    expect(headers).toEqual(['Namespace', 'Name', 'Status', 'Ready', 'Restarts', 'Node', 'Age', '']);
    const row = screen.getByText('my-pod').closest('tr')!;
    expect(within(row).getByText('1/2')).toBeInTheDocument(); // ready / total containers
    // Running, but not ready: pending until it is.
    expect(within(row).getByText('Running').closest('[data-tone]')).toHaveAttribute('data-tone', 'pending');
    expect(within(row).getByText('3')).toHaveClass('text-destructive'); // summed restarts
    expect(within(row).getByText('pool-a-2')).toBeInTheDocument();
  });

  it('mutes a pod with no restarts, and dashes one with no node or status yet', () => {
    const pod = obj({ rawJSON: { spec: { containers: [{}] }, status: { containerStatuses: [{ restartCount: 0 }] } } });
    render(<ObjectsTable {...PODS} {...live(pod)} />);
    const row = screen.getByText('my-pod').closest('tr')!;
    expect(within(row).getByText('0')).toHaveClass('text-muted-foreground');
    // Status and Node.
    expect(within(row).getAllByText('—')).toHaveLength(2);
  });

  it('draws the name over the workload that owns it, through VisibleText', () => {
    const pod = obj({
      name: 'web\u200b-1',
      rawJSON: {
        metadata: {
          labels: { 'pod-template-hash': '7d9f' },
          ownerReferences: [{ kind: 'ReplicaSet', name: 'web-7d9f', controller: true }],
        },
      },
    });
    render(<ObjectsTable {...PODS} {...live(pod)} />);
    expect(screen.getByText(/^web/, { selector: '.font-mono' })).toHaveTextContent('web\\u{200B}-1');
    expect(screen.getByText('deploy/web')).toBeInTheDocument();
  });

  it('prints an age as kubectl does, largest unit only', () => {
    const ago = (ms: number) => new Date(Date.now() - ms).toISOString();
    render(
      <ObjectsTable
        {...PODS}
        {...live(
          obj({ uid: 'a', name: 'a', creationTimestamp: ago(22 * 60_000 + 5_000) }),
          obj({ uid: 'b', name: 'b', creationTimestamp: ago(3 * 86_400_000) }),
        )}
      />,
    );
    // Age is the last column before the actions.
    const age = (name: string) => screen.getByText(name).closest('tr')!.lastElementChild!.previousElementSibling;
    expect(age('a')).toHaveTextContent(/^22m$/);
    expect(age('b')).toHaveTextContent(/^3d$/);
  });

  it('counts the rows it shows in its footer', () => {
    render(<ObjectsTable {...PODS} {...live(obj(), obj({ uid: 'u2', name: 'other' }))} />);
    expect(screen.getByText('Showing 2 of 2 pods')).toBeInTheDocument();
  });

  // kubectl derives a pod's Status from container state, not `status.phase` — a crash-looping
  // pod is phase=Running, so trusting the phase would report "Running" for a broken pod.
  it('reports a waiting container reason over the coarse phase', () => {
    const pod = obj({
      rawJSON: {
        spec: { containers: [{}] },
        status: {
          phase: 'Running',
          containerStatuses: [{ ready: false, restartCount: 5, state: { waiting: { reason: 'CrashLoopBackOff' } } }],
        },
      },
    });
    render(<ObjectsTable {...PODS} {...live(pod)} />);
    const row = screen.getByText('my-pod').closest('tr')!;
    expect(within(row).getByText('CrashLoopBackOff').closest('[data-tone]')).toHaveAttribute('data-tone', 'failing');
    expect(within(row).queryByText('Running')).not.toBeInTheDocument();
  });

  it('reports init-container progress while a pod is initializing', () => {
    const pod = obj({
      rawJSON: {
        spec: { containers: [{}], initContainers: [{ name: 'setup' }, { name: 'migrate' }] },
        status: {
          phase: 'Pending',
          initContainerStatuses: [{ name: 'setup', state: { waiting: { reason: 'PodInitializing' } } }],
        },
      },
    });
    render(<ObjectsTable {...PODS} {...live(pod)} />);
    const row = screen.getByText('my-pod').closest('tr')!;
    expect(within(row).getByText('Init:0/2')).toBeInTheDocument();
  });

  it('reports Terminating for a pod with a deletion timestamp', () => {
    const pod = obj({
      rawJSON: {
        metadata: { deletionTimestamp: '2026-07-21T10:00:00Z' },
        spec: { containers: [{}] },
        status: { phase: 'Running', containerStatuses: [{ ready: true, state: { running: {} } }] },
      },
    });
    render(<ObjectsTable {...PODS} {...live(pod)} />);
    const row = screen.getByText('my-pod').closest('tr')!;
    expect(within(row).getByText('Terminating')).toBeInTheDocument();
  });

  // Retries during init land in initContainerStatuses while containerStatuses is still empty —
  // reading only the latter would show "—" despite a nonzero restart count.
  it('counts init-container restarts in the Restarts total', () => {
    const pod = obj({
      rawJSON: {
        spec: { containers: [{}], initContainers: [{ name: 'setup' }] },
        status: {
          phase: 'Pending',
          initContainerStatuses: [
            { name: 'setup', restartCount: 3, state: { waiting: { reason: 'CrashLoopBackOff' } } },
          ],
        },
      },
    });
    render(<ObjectsTable {...PODS} {...live(pod)} />);
    const row = screen.getByText('my-pod').closest('tr')!;
    expect(within(row).getByText('3')).toBeInTheDocument();
  });

  it('sums init and regular container restarts', () => {
    const pod = obj({
      rawJSON: {
        spec: { containers: [{}], initContainers: [{ name: 'setup' }] },
        status: {
          phase: 'Running',
          initContainerStatuses: [{ name: 'setup', restartCount: 2, state: { terminated: { exitCode: 0 } } }],
          containerStatuses: [{ ready: true, restartCount: 4, state: { running: {} } }],
        },
      },
    });
    render(<ObjectsTable {...PODS} {...live(pod)} />);
    const row = screen.getByText('my-pod').closest('tr')!;
    expect(within(row).getByText('6')).toBeInTheDocument();
  });

  it('degrades kind-specific cells to "—" when the body is missing', () => {
    // A registered kind (Pod) still shows its columns, but with no body every cell is "—".
    render(<ObjectsTable {...PODS} {...live(obj())} />);
    const row = screen.getByText('my-pod').closest('tr')!;
    // Status, Ready, Restarts and Node all fall back to "—" (Age has a real timestamp here).
    expect(within(row).getAllByText('—')).toHaveLength(4);
  });

  it('renders "—" for a null creationTimestamp instead of an ancient date', () => {
    render(<ObjectsTable {...WIDGETS} {...live(obj({ kind: 'Widget', creationTimestamp: null }))} />);
    const row = screen.getByText('my-pod').closest('tr')!;
    expect(within(row).getByText('—')).toBeInTheDocument();
    expect(screen.queryByText(/0001/)).not.toBeInTheDocument();
  });

  it('renders the live rows through the virtualized, fixed-layout table', () => {
    render(<ObjectsTable {...PODS} {...live(obj())} />);
    document.querySelectorAll('table').forEach((t) => expect(t).toHaveClass('table-fixed'));
  });

  describe('sorting', () => {
    const pods = [
      obj({ uid: 'a', name: 'a', creationTimestamp: '2026-07-20T10:00:00Z', rawJSON: restarts(0) }),
      obj({ uid: 'b', name: 'b', creationTimestamp: '2026-07-22T10:00:00Z', rawJSON: restarts(7) }),
      obj({ uid: 'c', name: 'c', creationTimestamp: '2026-07-21T10:00:00Z', rawJSON: restarts(2) }),
    ];
    const names = () =>
      screen
        .getAllByRole('row')
        .filter((r) => r.querySelector('td'))
        .map((r) => r.querySelector('.font-mono')?.textContent);
    const press = (header: string) => fireEvent.click(screen.getByRole('button', { name: header }));

    it('keeps the watch’s order until a header is pressed, and says so', () => {
      render(<ObjectsTable {...PODS} {...live(...pods)} />);
      expect(names()).toEqual(['a', 'b', 'c']);
      expect(screen.getByText(/^Sorted by/)).toHaveTextContent('Sorted by namespace');
    });

    it('sorts a count largest first, then reverses, then unsorts', () => {
      render(<ObjectsTable {...PODS} {...live(...pods)} />);
      press('Restarts');
      expect(names()).toEqual(['b', 'c', 'a']);
      expect(screen.getByText(/^Sorted by/)).toHaveTextContent('Sorted by restarts');
      press('Restarts');
      expect(names()).toEqual(['a', 'c', 'b']);
      press('Restarts');
      expect(names()).toEqual(['a', 'b', 'c']);
    });

    it('sorts by a running job’s duration again as it grows, with no new rows', () => {
      onTestFinished(() => {
        vi.useRealTimers();
      });
      vi.useFakeTimers();
      vi.setSystemTime(Date.parse('2026-01-01T01:00:00Z'));
      const JOBS = { apiVersion: 'batch/v1', resource: 'jobs', kind: 'Job', label: 'Jobs', namespaced: true };
      const jobs = [
        obj({ uid: 'r', name: 'running', rawJSON: { status: { startTime: '2026-01-01T00:59:30Z' } } }),
        obj({
          uid: 'd',
          name: 'done',
          rawJSON: { status: { startTime: '2026-01-01T00:00:00Z', completionTime: '2026-01-01T00:01:00Z' } },
        }),
      ];
      render(<ObjectsTable {...JOBS} {...live(...jobs)} onAction={noAction} onOpen={noAction} />);
      press('Duration');
      expect(names()).toEqual(['done', 'running']);
      act(() => {
        vi.advanceTimersByTime(60_000);
      });
      expect(names()).toEqual(['running', 'done']);
    });

    it('sorts text and age smallest first', () => {
      render(<ObjectsTable {...PODS} {...live(...pods)} />);
      press('Name');
      press('Name');
      expect(names()).toEqual(['c', 'b', 'a']);
      press('Age');
      expect(names()).toEqual(['b', 'c', 'a']);
    });

    it('reads a sort by a column it no longer draws as unsorted', () => {
      const { rerender } = render(<ObjectsTable {...PODS} {...live(...pods)} />);
      press('Namespace');
      rerender(<ObjectsTable {...PODS} {...live(...pods)} namespace="default" />);
      expect(screen.getByText(/^Sorted by/)).toHaveTextContent('Sorted by name');
      expect(screen.getAllByRole('columnheader').every((h) => !h.hasAttribute('aria-sort'))).toBe(true);
    });

    it('keeps an object without the value last', () => {
      const ageless = obj({ uid: 'z', name: 'z', creationTimestamp: null });
      render(<ObjectsTable {...PODS} {...live(ageless, ...pods)} />);
      press('Age');
      expect(names()).toEqual(['b', 'c', 'a', 'z']);
    });
  });

  describe('status tabs', () => {
    const pod = (uid: string, rawJSON: unknown) => obj({ uid, name: uid, rawJSON });
    const ready = { status: { phase: 'Running', conditions: [{ type: 'Ready', status: 'True' }] } };
    const crashing = {
      status: { phase: 'Running', containerStatuses: [{ state: { waiting: { reason: 'CrashLoopBackOff' } } }] },
    };
    const starting = { status: { phase: 'Pending' } };
    const pods = [pod('a', ready), pod('b', crashing), pod('c', ready), pod('d', starting)];
    const tab = (name: RegExp) => screen.getByRole('tab', { name });

    it('counts each status in its colour', () => {
      render(<ObjectsTable {...PODS} {...live(...pods)} />);
      expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual([
        'All4',
        'Healthy2',
        'Failing1',
        'Pending1',
      ]);
      expect(within(tab(/^All/)).getByText('4')).toHaveClass('text-muted-foreground');
      expect(within(tab(/^Healthy/)).getByText('2')).toHaveClass('text-emerald-600');
      expect(within(tab(/^Failing/)).getByText('1')).toHaveClass('text-red-600');
      expect(within(tab(/^Pending/)).getByText('1')).toHaveClass('text-amber-600');
    });

    it('shows a tab’s pods alone, and counts them in the footer', async () => {
      render(<ObjectsTable {...PODS} {...live(...pods)} />);
      fireEvent.click(tab(/^Healthy/));
      await waitFor(() => expect(screen.getByText('Showing 2 of 4 pods')).toBeInTheDocument());
      expect(screen.queryByText('b')).not.toBeInTheDocument();
    });

    it('draws an empty table for a tab with nothing in it, and keeps the tabs', async () => {
      render(<ObjectsTable {...PODS} {...live(pod('a', ready))} />);
      fireEvent.click(tab(/^Failing/));
      await waitFor(() => expect(screen.getByText('Showing 0 of 1 pods')).toBeInTheDocument());
      expect(screen.getAllByRole('columnheader').length).toBeGreaterThan(0);
      expect(screen.queryByText(/^No /)).not.toBeInTheDocument();
      expect(within(tab(/^Failing/)).getByText('0')).toHaveClass('text-muted-foreground');
    });

    it('draws no tabs for a kind without a status', () => {
      render(<ObjectsTable {...WIDGETS} {...live(obj({ kind: 'Widget' }))} />);
      expect(screen.queryByRole('tablist')).not.toBeInTheDocument();
    });
  });

  describe('text filter', () => {
    const labelled = (uid: string, labels: Record<string, string>, nodeName: string) =>
      obj({ uid, name: uid, rawJSON: { metadata: { labels }, spec: { nodeName }, status: { phase: 'Pending' } } });
    const pods = [
      labelled('web-1', { app: 'web' }, 'pool-a'),
      labelled('web-2', { app: 'web' }, 'pool-b'),
      labelled('db-0', { app: 'db' }, 'pool-a'),
    ];
    const filter = (text: string) =>
      fireEvent.change(screen.getByRole('searchbox', { name: /^Filter/ }), { target: { value: text } });

    it('is a code-text box', () => {
      render(<ObjectsTable {...PODS} {...live(...pods)} />);
      expectCodeText(screen.getByRole('searchbox', { name: 'Filter pods' }));
    });

    it('narrows the rows by name, label or node, and the footer counts them', () => {
      render(<ObjectsTable {...PODS} {...live(...pods)} />);
      filter('WEB-');
      expect(screen.getByText('Showing 2 of 3 pods')).toBeInTheDocument();
      filter('app=db');
      expect(screen.getByText('Showing 1 of 3 pods')).toBeInTheDocument();
      filter('pool-a');
      expect(screen.getByText('Showing 2 of 3 pods')).toBeInTheDocument();
    });

    it('counts the tabs over what it leaves', () => {
      render(<ObjectsTable {...PODS} {...live(...pods)} />);
      filter('db');
      expect(screen.getByRole('tab', { name: /^All/ })).toHaveTextContent('All1');
      expect(screen.getByRole('tab', { name: /^Pending/ })).toHaveTextContent('Pending1');
    });

    it('draws an empty table when nothing matches', () => {
      render(<ObjectsTable {...PODS} {...live(...pods)} />);
      filter('nothing');
      expect(screen.getByText('Showing 0 of 3 pods')).toBeInTheDocument();
      expect(screen.queryByText(/^No /)).not.toBeInTheDocument();
    });

    it('clears on Escape', () => {
      render(<ObjectsTable {...PODS} {...live(...pods)} />);
      filter('db');
      fireEvent.keyDown(screen.getByRole('searchbox'), { key: 'Escape' });
      expect(screen.getByRole('searchbox')).toHaveValue('');
      expect(screen.getByText('Showing 3 of 3 pods')).toBeInTheDocument();
      fireEvent.keyDown(screen.getByRole('searchbox'), { key: 'a' });
      expect(screen.getByText('Showing 3 of 3 pods')).toBeInTheDocument();
    });

    it('sits under the tabs', () => {
      render(<ObjectsTable {...PODS} {...live(...pods)} />);
      const box = screen.getByRole('searchbox');
      expect(box.compareDocumentPosition(screen.getByRole('tablist')) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
    });

    it('matches the namespace under all namespaces, and not under a scope', () => {
      const shop = [obj({ uid: 'a', name: 'a', namespace: 'shop' }), obj({ uid: 'b', name: 'b', namespace: 'blog' })];
      const { rerender } = render(<ObjectsTable {...PODS} {...live(...shop)} />);
      filter('shop');
      expect(screen.getByText('Showing 1 of 2 pods')).toBeInTheDocument();
      rerender(<ObjectsTable {...PODS} {...live(shop[0])} namespace="shop" />);
      expect(screen.getByText('Showing 0 of 1 pods')).toBeInTheDocument();
    });

    it('filters a kind without tabs', () => {
      render(<ObjectsTable {...WIDGETS} {...live(obj({ uid: 'a', name: 'a' }), obj({ uid: 'b', name: 'b' }))} />);
      filter('b');
      expect(screen.getByText('Showing 1 of 2 widgets')).toBeInTheDocument();
    });
  });

  describe('actions', () => {
    afterEach(() => localStorage.clear());

    it('hands a row’s action and object to the page', async () => {
      const onAction = vi.fn();
      const pod = obj();
      render(<ObjectsTable {...PODS} {...live(pod)} onAction={onAction} />);
      await userEvent.click(screen.getByRole('button', { name: 'Actions for my-pod' }));
      await userEvent.click(await screen.findByRole('menuitem', { name: 'View logs' }));
      expect(onAction).toHaveBeenCalledWith('logs', pod);
    });

    it('stays last, out of the column picker', async () => {
      localStorage.setItem('kubetail:table-columns:v1/pods', JSON.stringify({ order: ['age', 'name'] }));
      const user = userEvent.setup();
      render(<ObjectsTable {...PODS} {...live(obj())} />);
      const row = screen.getByText('my-pod').closest('tr')!;
      expect(
        within(row.lastElementChild as HTMLElement).getByRole('button', { name: 'Actions for my-pod' }),
      ).toBeInTheDocument();
      await user.click(screen.getByRole('button', { name: 'Columns' }));
      // The pod's 16 columns, and no box for the actions.
      expect(await screen.findAllByRole('checkbox')).toHaveLength(16);
    });
  });

  describe('column picker', () => {
    afterEach(() => localStorage.clear());
    const headers = () => screen.getAllByRole('columnheader').map((h) => h.textContent);
    const toggle = async (user: ReturnType<typeof userEvent.setup>, name: string) => {
      await user.click(screen.getByRole('button', { name: 'Columns' }));
      await user.click(await screen.findByRole('checkbox', { name }));
      await user.keyboard('{Escape}');
    };

    it('offers every column in the table’s order, and hides a picked one for the kind', async () => {
      const user = userEvent.setup();
      const { unmount } = render(<ObjectsTable {...PODS} {...live(obj())} />);
      await user.click(screen.getByRole('button', { name: 'Columns' }));
      const boxes = await screen.findAllByRole('checkbox');
      expect(boxes.map((c) => c.closest('label')?.textContent)).toEqual([
        'Namespace',
        'Name',
        'Status',
        'Ready',
        'Restarts',
        'Node',
        'IP',
        'Host IP',
        'Containers',
        'Images',
        'QoS class',
        'Service account',
        'Priority class',
        'Controlled by',
        'Started',
        'Age',
      ]);
      // The extra columns start unticked.
      expect(boxes.filter((b) => b.getAttribute('aria-checked') === 'true')).toHaveLength(7);
      await user.keyboard('{Escape}');
      await toggle(user, 'Node');
      expect(headers()).not.toContain('Node');
      unmount();

      // Kept for the kind, and for that kind alone.
      render(<ObjectsTable {...PODS} {...live(obj())} />);
      expect(headers()).not.toContain('Node');
    });

    it('draws the columns in the kept order, and Reset brings back the defaults', async () => {
      localStorage.setItem(
        'kubetail:table-columns:v1/pods',
        JSON.stringify({ shown: { IP: true }, order: ['age', 'name', 'namespace'] }),
      );
      const user = userEvent.setup();
      render(<ObjectsTable {...PODS} {...live(obj())} />);
      expect(headers()).toEqual(['Age', 'Name', 'Status', 'Ready', 'Restarts', 'Node', 'IP', 'Namespace', '']);

      await user.click(screen.getByRole('button', { name: 'Columns' }));
      await user.click(await screen.findByRole('button', { name: 'Reset' }));
      expect(headers()).toEqual(['Namespace', 'Name', 'Status', 'Ready', 'Restarts', 'Node', 'Age', '']);
      expect(screen.getByRole('button', { name: 'Reset' })).toBeDisabled();
    });

    it('shows a column hidden by default once picked', async () => {
      const user = userEvent.setup();
      render(<ObjectsTable {...PODS} {...live(obj({ rawJSON: { status: { podIP: '10.0.0.5' } } }))} />);
      expect(headers()).not.toContain('IP');
      await toggle(user, 'IP');
      expect(headers()).toContain('IP');
      expect(screen.getByText('10.0.0.5')).toBeInTheDocument();
    });

    it('sits on the search box’s row, under the status tabs', () => {
      render(<ObjectsTable {...PODS} {...live(obj())} />);
      const row = screen.getByRole('searchbox').closest('div.mb-3')!;
      expect(within(row as HTMLElement).getByRole('button', { name: 'Columns' })).toBeInTheDocument();
      expect(within(row as HTMLElement).queryByRole('tablist')).not.toBeInTheDocument();
    });

    it('matches the namespace only while its column shows', async () => {
      const user = userEvent.setup();
      render(<ObjectsTable {...PODS} {...live(obj({ namespace: 'shop' }))} />);
      await toggle(user, 'Namespace');
      fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'shop' } });
      expect(screen.getByText('Showing 0 of 1 pods')).toBeInTheDocument();
    });

    it('reads a sort by a hidden column as unsorted', async () => {
      const user = userEvent.setup();
      render(<ObjectsTable {...PODS} {...live(obj())} />);
      fireEvent.click(screen.getByRole('button', { name: 'Age' }));
      await toggle(user, 'Age');
      expect(screen.getByText(/^Sorted by/)).toHaveTextContent('Sorted by namespace');
    });
  });

  describe('detail', () => {
    // The page's part: it holds the open object, as `KindPage` does.
    function Page({ initialOpen, ...props }: Omit<ObjectsTableProps, 'openUid' | 'onOpen'> & { initialOpen?: string }) {
      const [openUid, setOpenUid] = useState(initialOpen);
      return <ObjectsTable {...props} openUid={openUid} onOpen={setOpenUid} />;
    }

    const detail = () => screen.queryByRole('region', { name: 'Pod' });
    const heading = () => within(detail()!).getByRole('heading', { level: 2 });
    const footer = () => screen.getByText(/^Showing/);
    const listRow = (name: string) => screen.getByText(name, { selector: 'button .font-mono' }).closest('button')!;
    const running = (ready: boolean) => ({
      status: { phase: 'Running', conditions: [{ type: 'Ready', status: ready ? 'True' : 'False' }] },
    });

    afterEach(() => localStorage.clear());

    it('opens from a row’s chevron, never the right sidebar, and the list replaces the table', () => {
      const onAction = vi.fn();
      render(
        <Page {...PODS} onAction={onAction} {...live(obj({ uid: 'a', name: 'web' }), obj({ uid: 'b', name: 'db' }))} />,
      );
      fireEvent.click(screen.getByRole('button', { name: 'Open web' }));
      expect(heading()).toHaveTextContent('web');
      expect(onAction).not.toHaveBeenCalled();
      // No header, no picker, no sort footer beside the detail.
      expect(screen.queryByRole('button', { name: 'Columns' })).not.toBeInTheDocument();
      expect(screen.queryByText(/^Sorted by/)).not.toBeInTheDocument();
      expect(footer()).toHaveTextContent('Showing 2 of 2 pods');
      expect(listRow('web')).toHaveAttribute('aria-current', 'true');
    });

    it('opens another object from its list row', () => {
      render(
        <Page {...PODS} initialOpen="a" {...live(obj({ uid: 'a', name: 'web' }), obj({ uid: 'b', name: 'db' }))} />,
      );
      fireEvent.click(listRow('db'));
      expect(heading()).toHaveTextContent('db');
      expect(listRow('db')).toHaveAttribute('aria-current', 'true');
    });

    it('keeps the status tabs and the search over the list', () => {
      render(<Page {...PODS} initialOpen="u1" {...live(obj({ rawJSON: running(true) }))} />);
      expect(screen.getByRole('tablist', { name: 'Pods by status' }).closest('.overflow-x-auto')).toBeInTheDocument();
      expectCodeText(screen.getByRole('searchbox'));
    });

    it('draws no tabs over the list of a kind without a status', () => {
      render(<Page {...WIDGETS} initialOpen="u1" {...live(obj({ kind: 'Widget' }))} />);
      expect(screen.queryByRole('tablist')).not.toBeInTheDocument();
      expect(screen.getByRole('region', { name: 'Widget' })).toBeInTheDocument();
    });

    it('stays open across a tab, a search and a sort that leave it out of the list', async () => {
      const user = userEvent.setup();
      render(
        <Page
          {...PODS}
          {...live(
            obj({ uid: 'w', name: 'web', rawJSON: running(true) }),
            obj({ uid: 'd', name: 'db', rawJSON: running(false) }),
          )}
        />,
      );
      await user.click(screen.getByRole('button', { name: 'Name' }));
      await user.click(screen.getByRole('button', { name: 'Open web' }));
      await user.click(screen.getByRole('tab', { name: /^Pending/ }));
      fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'db' } });
      expect(footer()).toHaveTextContent('Showing 1 of 2 pods');
      expect(heading()).toHaveTextContent('web');
    });

    it('keeps the table’s sort in the list', () => {
      render(<Page {...PODS} {...live(obj({ uid: 'a', name: 'a' }), obj({ uid: 'b', name: 'b' }))} />);
      fireEvent.click(screen.getByRole('button', { name: 'Name' }));
      fireEvent.click(screen.getByRole('button', { name: 'Name' }));
      fireEvent.click(screen.getByRole('button', { name: 'Open a' }));
      const names = Array.from(document.querySelectorAll('button .font-mono')).map((n) => n.textContent);
      expect(names).toEqual(['b', 'a']);
    });

    it('closes with its X, and with Escape from inside it, focus moving to the search', async () => {
      const user = userEvent.setup();
      render(<Page {...PODS} {...live(obj())} />);
      await user.click(screen.getByRole('button', { name: 'Open my-pod' }));
      expect(detail()).toHaveFocus();
      await user.keyboard('{Escape}');
      expect(detail()).not.toBeInTheDocument();
      expect(screen.getByRole('searchbox')).toHaveFocus();
      await user.click(screen.getByRole('button', { name: 'Open my-pod' }));
      await user.click(screen.getByRole('button', { name: 'Close my-pod' }));
      expect(detail()).not.toBeInTheDocument();
      expect(screen.getByRole('searchbox')).toHaveFocus();
    });

    it('says the object no longer exists once a settled snapshot lacks it, holding it through a reconnect', () => {
      const a = obj({ uid: 'a', name: 'web' });
      const b = obj({ uid: 'b', name: 'db' });
      const { rerender } = render(<Page {...PODS} initialOpen="a" {...live(a, b)} />);
      rerender(
        <Page {...PODS} initialOpen="a" objects={[b] as ClusterCachedDataObject[]} active phase="reconnecting" />,
      );
      expect(screen.queryByText('This object no longer exists.')).not.toBeInTheDocument();
      rerender(<Page {...PODS} initialOpen="a" {...live(b)} />);
      expect(screen.getByText('This object no longer exists.')).toBeInTheDocument();
      expect(heading()).toHaveTextContent('web');
    });

    it('says the last object in scope is gone rather than that there are none', () => {
      const { rerender } = render(<Page {...PODS} initialOpen="u1" {...live(obj())} />);
      rerender(<Page {...PODS} initialOpen="u1" {...live()} />);
      expect(screen.getByText('This object no longer exists.')).toBeInTheDocument();
      fireEvent.click(screen.getByRole('button', { name: 'Close my-pod' }));
      expect(screen.getByText('No pod objects.')).toBeInTheDocument();
    });

    it('hands the detail’s Logs to the page as the menu’s action', () => {
      const onAction = vi.fn();
      const pod = obj();
      render(<Page {...PODS} initialOpen="u1" onAction={onAction} {...live(pod)} />);
      fireEvent.click(screen.getByRole('button', { name: 'Logs for my-pod' }));
      expect(onAction).toHaveBeenCalledWith('logs', pod);
    });

    describe('list width', () => {
      const KEY = 'kubetail:dashboard-objects-list-width';
      const handle = () => screen.getByRole('separator', { name: 'Resize pods list' });
      const pane = () => handle().parentElement!;
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

      it('opens at 320px, the pane drawing half the strip beside it', () => {
        render(<Page {...PODS} initialOpen="u1" {...live(obj())} />);
        expect(pane().style.width).toBe('328px');
        expect(pane().style.maxWidth).toBe('calc(100% - 368px)');
      });

      it('resizes within 240px and the width that leaves the detail 360px, and keeps it for every kind', () => {
        render(<Page {...PODS} initialOpen="u1" {...live(obj())} />);
        layOut(1000);
        press('ArrowRight');
        expect(localStorage.getItem(KEY)).toBe('336');
        for (let i = 0; i < 30; i += 1) press('ArrowRight');
        expect(pane().style.width).toBe('632px');
        for (let i = 0; i < 40; i += 1) press('ArrowLeft');
        expect(pane().style.width).toBe('248px');
      });

      it('reads the kept width back', () => {
        localStorage.setItem(KEY, '400');
        render(<Page {...WIDGETS} initialOpen="u1" {...live(obj({ kind: 'Widget' }))} />);
        expect(screen.getByRole('separator', { name: 'Resize widgets list' }).parentElement!.style.width).toBe('408px');
      });
    });
  });
});
