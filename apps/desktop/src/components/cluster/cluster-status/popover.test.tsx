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

import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Provider as UrqlProvider } from 'urql';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Cluster } from '@/lib/clusters';
import { clusterRecord, flushSubscriptionsSynchronously, mockTauriCore } from '@/test-utils';

// Frames pushed here are asserted on immediately.
flushSubscriptionsSynchronously();

const { invokeMock, channels, channelFor, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { createGraphqlClient } = await import('@/lib/graphql/client');
const { DialogProvider, useDialog } = await import('@/lib/dialog');
const { ClusterStatusPopover } = await import('./popover');

function withHealth(cluster: Cluster, over: Record<string, unknown>): Cluster {
  const cache = cluster.activeCache!;
  return { ...cluster, activeCache: { ...cache, health: { ...cache.health!, ...over } } };
}

function retryCalls() {
  return invokeMock.mock.calls.filter(
    ([cmd, arg]) => cmd === 'graphql_query' && (arg as { body: string }).body.includes('clusterConnectionRetry'),
  );
}

// What the popover asked the dialog host for.
function DialogProbe() {
  const { activeDialog, clusterDetail } = useDialog();
  return <output data-testid="dialog">{JSON.stringify({ activeDialog, clusterDetail })}</output>;
}

function dialogState() {
  return JSON.parse(screen.getByTestId('dialog').textContent);
}

function withConnected(
  cluster: Cluster,
  over: { reason?: string; message?: string; transitionedAt?: string; unconfirmed?: boolean },
): Cluster {
  return { ...cluster, conditions: cluster.conditions.map((c) => (c.type === 'Connected' ? { ...c, ...over } : c)) };
}

// One of the popup's sections, by its heading.
function section(popup: HTMLElement, title: string) {
  return within(popup).getByRole('heading', { name: title }).closest('section')!;
}

// An alert sits first in the sections' scroller, out of the fixed header, so a long one
// scrolls rather than pushing the footer out of the popup.
function expectOverTheSections(popup: HTMLElement, alert: HTMLElement) {
  const scroller = section(popup, 'Kubeconfig').parentElement!;
  expect(alert.closest('header')).toBeNull();
  expect(scroller.firstElementChild).toContainElement(alert);
}

function push(field: string, data: unknown) {
  channelFor(field).onmessage!(JSON.stringify({ type: 'next', payload: { data: { [field]: data } } }));
}

function pushSchedule(nextRequeueAt: string | null, probing = false, failures = 0) {
  push('clusterScheduleStateWatch', { nextRequeueAt, probing, failures });
}

const rate = (received: number, sent: number) => ({
  at: '2026-11-02T12:00:00Z',
  receivedBytesPerSecond: received,
  sentBytesPerSecond: sent,
});

// The popover as the omnibox mounts it, under the providers it reads.
function tree(cluster: Cluster | undefined) {
  return (
    <UrqlProvider value={createGraphqlClient()}>
      <DialogProvider>
        <ClusterStatusPopover anchorRef={{ current: null }} context="prod" cluster={cluster}>
          <button type="button">Open status</button>
        </ClusterStatusPopover>
        <DialogProbe />
      </DialogProvider>
    </UrqlProvider>
  );
}

async function openStatus(cluster: Cluster | undefined) {
  const user = userEvent.setup();
  const view = render(tree(cluster));
  await user.click(screen.getByRole('button', { name: 'Open status' }));
  const popup = await screen.findByTestId('cluster-status');
  return { user, popup, rerender: (next: Cluster) => view.rerender(tree(next)) };
}

describe('ClusterStatusPopover', () => {
  beforeEach(() => {
    invokeMock.mockReset();
    channels.length = 0;
    let id = 0;
    invokeMock.mockImplementation(async (cmd: string) => {
      if (cmd === 'graphql_subscribe') {
        id += 1;
        return id;
      }
      if (cmd === 'graphql_unsubscribe') return undefined;
      if (cmd === 'graphql_query') {
        return { status: 200, body: JSON.stringify({ data: { clusterConnectionRetry: true } }) };
      }
      throw new Error(`unexpected ${cmd}`);
    });
  });

  afterEach(cleanup);

  // jsdom lays nothing out, so this checks the style that sizes it.
  it('is at least 320px wide, as wide as the omnibox it hangs from, and capped to the frame', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod' }));
    expect(popup.style.width).toBe('max(var(--anchor-width), 320px)');
    // The room `OmniboxPopover`'s positioner leaves inside the window frame.
    expect(popup.style.maxWidth).toBe('var(--available-width)');
    expect(popup.style.maxHeight).toBe('var(--available-height)');
  });

  it('names the window’s cluster', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod' }));
    expect(popup).toHaveTextContent('prod');
  });

  it('gives each axis its verdict and no history', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod', connected: 'False' }));

    expect(within(popup).getByText('Disconnected')).toHaveAttribute('data-tone', 'error');
    expect(within(popup).getByText('Stalled')).toHaveAttribute('data-tone', 'muted');
    expect(popup).not.toHaveTextContent(/recent|sync verified/i);
    const queries = invokeMock.mock.calls
      .filter(([cmd]) => cmd === 'graphql_subscribe')
      .map(([, arg]) => (arg as { query: string }).query);
    expect(queries.some((q) => q.includes('objectEventFeed'))).toBe(false);
  });

  it('marks a stale sync verdict as needing attention', async () => {
    const stale = withHealth(clusterRecord({ context: 'prod', reason: 'Stale' }), {
      unhealthyKindRefs: [
        { apiVersion: 'v1', resource: 'pods', reason: 'Stale' },
        { apiVersion: 'v1', resource: 'secrets', reason: 'Syncing' },
      ],
      unhealthyKinds: 2,
    });
    const { popup } = await openStatus(stale);

    expect(within(section(popup, 'Cache')).getByText('Stale')).toHaveAttribute('data-tone', 'attention');
  });

  it('names a stale sync’s stale kinds in an alert over the sections', async () => {
    const stale = withHealth(clusterRecord({ context: 'prod', reason: 'Stale' }), {
      unhealthyKindRefs: [
        { apiVersion: 'v1', resource: 'pods', reason: 'Stale' },
        { apiVersion: 'v1', resource: 'secrets', reason: 'Syncing' },
      ],
      unhealthyKinds: 2,
    });
    const { popup } = await openStatus(stale);

    const alert = within(popup).getByRole('alert');
    expectOverTheSections(popup, alert);
    expect(alert).toHaveTextContent(
      /^pods not receiving updatesData shown may be out of date\. Kubetail will restart watches automatically until updates resume\.$/,
    );
    expect(section(popup, 'Cache')).not.toHaveTextContent('not receiving updates');
    expect(popup).not.toHaveTextContent('secrets');
  });

  it('titles a stale sync that names no kinds', async () => {
    const stale = withHealth(clusterRecord({ context: 'prod', reason: 'Stale' }), { unhealthyKindRefs: [] });
    const { popup } = await openStatus(stale);
    expect(within(popup).getByRole('alert')).toHaveTextContent('Some kinds aren’t receiving updates');
  });

  it('counts a stale sync’s kinds past three', async () => {
    const refs = ['configmaps', 'pods', 'secrets', 'services'].map((resource) => ({
      apiVersion: 'v1',
      resource,
      reason: 'Stale',
    }));
    const stale = withHealth(clusterRecord({ context: 'prod', reason: 'Stale' }), {
      unhealthyKindRefs: [...refs, { apiVersion: 'v1', resource: 'nodes', reason: 'Syncing' }],
      unhealthyKinds: 5,
      totalKinds: 10,
    });
    const { popup } = await openStatus(stale);
    expect(within(popup).getByRole('alert')).toHaveTextContent('4 kinds not receiving updates');
  });

  it('says no kind is receiving updates when every unpaused kind is stale', async () => {
    const refs = ['configmaps', 'pods', 'secrets', 'services'].map((resource) => ({
      apiVersion: 'v1',
      resource,
      reason: 'Stale',
    }));
    const stale = withHealth(clusterRecord({ context: 'prod', reason: 'Stale' }), {
      unhealthyKindRefs: refs,
      unhealthyKinds: 4,
      totalKinds: 5,
      pausedKinds: 1,
    });
    const { popup } = await openStatus(stale);
    expect(within(popup).getByRole('alert')).toHaveTextContent('No kinds are receiving updates');
  });

  it('keeps an unknown verdict’s reason line in the Cache section', async () => {
    const degraded = withHealth(clusterRecord({ context: 'prod', reason: 'SomethingNew' }), {
      unhealthyKindRefs: [{ apiVersion: 'v1', resource: 'pods', reason: 'SomethingNew' }],
      unhealthyKinds: 1,
      totalKinds: 3,
    });
    const { popup } = await openStatus(degraded);
    expect(within(popup).queryByRole('alert')).not.toBeInTheDocument();
    expect(section(popup, 'Cache')).toHaveTextContent('pods');
  });

  it('puts a failed sync’s reason in an alert over the sections', async () => {
    const failed = withHealth(clusterRecord({ context: 'prod', reason: 'SyncFailed' }), {
      unhealthyKindRefs: [
        { apiVersion: 'v1', resource: 'pods', reason: 'SyncFailed' },
        { apiVersion: 'v1', resource: 'secrets', reason: 'Syncing' },
      ],
      unhealthyKinds: 2,
      totalKinds: 3,
    });
    const { popup } = await openStatus(failed);

    const alert = within(popup).getByRole('alert');
    expectOverTheSections(popup, alert);
    expect(alert).toHaveTextContent('Some kinds aren’t syncing');
    expect(alert).toHaveTextContent('pods not syncing');
    expect(alert).not.toHaveTextContent('secrets');
    expect(section(popup, 'Cache')).not.toHaveTextContent('not syncing');
  });

  it('names a failed sync by its class of failure', async () => {
    const failed = withHealth(clusterRecord({ context: 'prod', reason: 'SyncFailed' }), {
      unhealthyKindRefs: [{ apiVersion: 'v1', resource: 'secrets', reason: 'SyncFailed' }],
      unhealthyKinds: 1,
      totalKinds: 3,
      failure: 'Forbidden',
    });
    const { popup } = await openStatus(failed);
    const alert = within(popup).getByRole('alert');
    expect(alert).toHaveTextContent('Permission denied');
    expect(alert).toHaveTextContent('secrets not syncing');
  });

  it('puts a failed store in an alert in the header', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod', reason: 'StoreFailed' }));
    const alert = within(popup).getByRole('alert');
    expect(alert).toHaveTextContent('Cache storage failed');
    expect(alert).toHaveTextContent('The cache file will not open.');
  });

  it('gives a cache no alert while the connection is down', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod', connected: 'False', reason: 'SyncFailed' }));
    expect(within(popup).getAllByRole('alert')).toHaveLength(1);
    expect(within(popup).getByRole('alert')).not.toHaveTextContent('Some kinds aren’t syncing');
  });

  it('gives a healthy sync verdict no reason line', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod' }));
    expect(within(section(popup, 'Cache')).getByText('Current')).toHaveAttribute('data-tone', 'ok');
    expect(popup).not.toHaveTextContent(/not receiving|not syncing|will not open/);
  });

  it('retries the connection, held until the probe it asked for is done', async () => {
    let finish!: () => void;
    const probing = new Promise<void>((resolve) => {
      finish = resolve;
    });
    const respond = invokeMock.getMockImplementation()!;
    invokeMock.mockImplementation(async (cmd: string, arg?: unknown) => {
      if (cmd === 'graphql_query') await probing;
      return respond(cmd, arg);
    });
    const { user, popup } = await openStatus(clusterRecord({ context: 'prod', connected: 'False' }));

    await user.click(within(popup).getByRole('button', { name: 'Retry now' }));
    expect(retryCalls()).toHaveLength(1);
    expect(JSON.parse((retryCalls()[0][1] as { body: string }).body).variables).toEqual({ id: 'id-prod' });
    expect(within(popup).getByRole('button', { name: 'Retrying…' })).toBeDisabled();

    await act(async () => finish());
    expect(await within(popup).findByRole('button', { name: 'Retry now' })).toBeEnabled();
  });

  it('counts a down connection’s attempts and down to its retry', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod', connected: 'False' }));
    const alert = within(popup).getByRole('alert');
    const in90s = () => new Date(Date.now() + 90_000).toISOString();

    act(() => pushSchedule(in90s(), false, 1));
    expect(alert).toHaveTextContent(/1 attempt · Retrying in \d+s$/);

    act(() => pushSchedule(in90s(), false, 3));
    expect(alert).toHaveTextContent(/3 attempts · Retrying in \d+s$/);

    // While a probe runs the countdown gives way to the retry itself.
    act(() => pushSchedule(null, true, 3));
    expect(alert).toHaveTextContent(/3 attempts · Retrying now…$/);
  });

  it('counts a down connection’s attempts alone while no retry is scheduled', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod', connected: 'False' }));
    act(() => pushSchedule(null, false, 2));
    expect(within(popup).getByRole('alert')).toHaveTextContent(/2 attempts$/);
  });

  it('draws no retry line for a probe before any attempt has failed', async () => {
    const failed = withConnected(clusterRecord({ context: 'prod', connected: 'False' }), {
      reason: 'Unreachable',
      message: 'connection refused',
    });
    const { popup } = await openStatus(failed);
    act(() => pushSchedule(null, true, 0));
    expect(within(popup).getByRole('alert')).not.toHaveTextContent(/attempt|Retrying/);
  });

  it('reconnects a live connection, held as reconnecting until its probe is done', async () => {
    let finish!: () => void;
    const probing = new Promise<void>((resolve) => {
      finish = resolve;
    });
    const respond = invokeMock.getMockImplementation()!;
    invokeMock.mockImplementation(async (cmd: string, arg?: unknown) => {
      if (cmd === 'graphql_query') await probing;
      return respond(cmd, arg);
    });
    const { user, popup } = await openStatus(clusterRecord({ context: 'prod' }));

    await user.click(within(popup).getByRole('button', { name: 'Reconnect' }));
    expect(retryCalls()).toHaveLength(1);
    // The probe the press asked for shows on the state watch too; the press's own words win.
    act(() => pushSchedule(null, true));
    expect(within(popup).getByRole('button', { name: 'Reconnecting…' })).toBeDisabled();

    await act(async () => finish());
    act(() => pushSchedule(new Date(Date.now() + 90_000).toISOString()));
    expect(await within(popup).findByRole('button', { name: 'Reconnect' })).toBeEnabled();
  });

  it('holds a down connection’s button as retrying while a probe runs', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod', connected: 'False' }));

    act(() => pushSchedule(null, true, 1));
    expect(within(popup).getByRole('button', { name: 'Retrying…' })).toBeDisabled();

    act(() => pushSchedule(new Date(Date.now() + 90_000).toISOString(), false, 1));
    expect(within(popup).getByRole('button', { name: 'Retry now' })).toBeEnabled();
  });

  it('holds a live connection’s button as checking while a probe runs', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod' }));

    act(() => pushSchedule(null, true));
    expect(within(popup).getByRole('button', { name: 'Checking…' })).toBeDisabled();

    act(() => pushSchedule(new Date(Date.now() + 90_000).toISOString()));
    expect(within(popup).getByRole('button', { name: 'Reconnect' })).toBeEnabled();
  });

  it('puts a failed connection’s reason and retry in an alert over the sections', async () => {
    const failed = withConnected(clusterRecord({ context: 'prod', connected: 'False' }), {
      reason: 'Unreachable',
      message: 'connection refused',
    });
    const { popup } = await openStatus(failed);
    act(() => pushSchedule(new Date(Date.now() + 90_000).toISOString(), false, 2));

    const alert = within(popup).getByRole('alert');
    expectOverTheSections(popup, alert);
    expect(alert).toHaveTextContent('Can’t reach the server');
    expect(alert).toHaveTextContent('connection refused');
    expect(alert).toHaveTextContent('2 attempts · Retrying in');
  });

  it('gives a connecting cluster no alert and no retry line', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod', connected: 'Unknown' }));
    act(() => pushSchedule(new Date(Date.now() + 90_000).toISOString()));

    expect(within(popup).queryByRole('alert')).not.toBeInTheDocument();
    expect(popup).not.toHaveTextContent(/Next check|Retrying in/);
  });

  it('gives a live connection no next check', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod' }));
    act(() => pushSchedule(new Date(Date.now() + 90_000).toISOString()));
    expect(popup).not.toHaveTextContent('Retrying in');
  });

  it('names the kube context first', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod' }));
    const label = within(popup).getByText('Context');
    expect(label.nextElementSibling).toHaveTextContent('prod');
    expect(popup.querySelector('dt')).toBe(label);
  });

  it('names the server the context points at, or a dash without one', async () => {
    const { popup, rerender } = await openStatus(clusterRecord({ context: 'prod' }));
    const row = () => within(popup).getByText('Server').nextElementSibling!;
    expect(row()).toHaveTextContent('https://example:6443');

    const nameless = clusterRecord({ context: 'prod' });
    nameless.status.source.kubeconfig!.cluster.entry = null;
    rerender(nameless);
    expect(row()).toHaveTextContent('—');
  });

  it('names the kubeconfig user, or a dash without a kubeconfig entry', async () => {
    const { popup, rerender } = await openStatus(clusterRecord({ context: 'prod' }));
    const row = () => within(popup).getByText('User').nextElementSibling!;
    expect(row()).toHaveTextContent('prod');
    expect(row().querySelector('[title="prod"]')).not.toBeNull();

    const unlisted = clusterRecord({ context: 'prod' });
    unlisted.status.source.kubeconfig = null;
    rerender(unlisted);
    expect(row()).toHaveTextContent('—');
  });

  it('says why a failed connection failed, drawing the cluster’s words visibly', async () => {
    const failed = withConnected(clusterRecord({ context: 'prod', connected: 'False' }), {
      reason: 'ProbeFailed',
      message: 'dial tcp\u200b: connection refused',
    });
    const { popup } = await openStatus(failed);
    const row = within(popup).getByRole('alert');

    expect(row).toHaveTextContent(/dial tcp.*: connection refused/);
    expect(row.querySelector('mark')).not.toBeNull();
  });

  it('falls back to the condition’s reason when it carries no message', async () => {
    const failed = withConnected(clusterRecord({ context: 'prod', connected: 'False' }), { reason: 'ProbeFailed' });
    const { popup } = await openStatus(failed);
    const alert = within(popup).getByRole('alert');
    expect(alert).toHaveTextContent('Connection failed');
    expect(alert).toHaveTextContent('ProbeFailed');
  });

  it('gives a live connection no reason line', async () => {
    const live = withConnected(clusterRecord({ context: 'prod' }), { reason: 'ProbeSucceeded', message: 'probe ok' });
    const { popup } = await openStatus(live);
    expect(section(popup, 'Connection')).toHaveTextContent('Connected');
    expect(within(popup).queryByRole('alert')).not.toBeInTheDocument();
    expect(popup).not.toHaveTextContent(/probe ok|ProbeSucceeded/);
  });

  it('keeps the verdicts when the omnibox draws it again', async () => {
    const cluster = clusterRecord({ context: 'prod', connected: 'False' });
    const { rerender } = await openStatus(cluster);
    rerender(cluster);

    const popup = await screen.findByTestId('cluster-status');
    expect(within(popup).getByText('Disconnected')).toHaveAttribute('data-tone', 'error');
  });

  it('says when the cluster has no record yet', async () => {
    const { popup } = await openStatus(undefined);
    expect(popup).toHaveTextContent('Kubetail has not reached this cluster yet.');
    expect(within(popup).getByText('Inactive')).toHaveAttribute('data-tone', 'muted');
    expect(within(popup).queryByRole('button', { name: /retry/i })).not.toBeInTheDocument();
  });

  it('opens the Clusters dialog on this cluster’s connection pane, and closes', async () => {
    const { user, popup } = await openStatus(clusterRecord({ context: 'prod', connected: 'False' }));

    await user.click(within(popup).getByRole('button', { name: 'View details' }));
    expect(dialogState()).toEqual({
      activeDialog: 'clusters',
      clusterDetail: { clusterId: 'id-prod', pane: 'connection' },
    });
    await waitFor(() => expect(screen.queryByTestId('cluster-status')).not.toBeInTheDocument());
  });

  it('offers no View details for a cluster with no record', async () => {
    const { popup } = await openStatus(undefined);
    expect(within(popup).queryByRole('button', { name: 'View details' })).not.toBeInTheDocument();
  });

  it('heads the popup with the cluster’s overall verdict and a phrase per axis', async () => {
    const { popup, rerender } = await openStatus(clusterRecord({ context: 'prod' }));
    expect(within(popup).getByText('Healthy')).toHaveAttribute('data-tone', 'ok');
    expect(popup).toHaveTextContent('Connection stable · cache in sync');

    rerender(clusterRecord({ context: 'prod', reason: 'Stale' }));
    expect(within(popup).getByText('Degraded')).toHaveAttribute('data-tone', 'attention');
    expect(popup).toHaveTextContent('Connection stable · cache stale');

    rerender(clusterRecord({ context: 'prod', connected: 'False' }));
    expect(within(popup).getByText('Unhealthy')).toHaveAttribute('data-tone', 'error');
    expect(popup).toHaveTextContent('Disconnected · cache stalled');
  });

  const since = (ms: number) => new Date(Date.now() - ms).toISOString();
  const upFor = (ms: number) => withConnected(clusterRecord({ context: 'prod' }), { transitionedAt: since(ms) });

  it.each<[string, () => Cluster, string]>([
    ['under a minute', () => upFor(10_000), '<1m'],
    ['minutes', () => upFor(5 * 60_000 + 10_000), '5m'],
    ['hours', () => upFor(3 * 3_600_000 + 20 * 60_000 + 10_000), '3h20m'],
    ['days', () => upFor(((3 * 24 + 4) * 60 + 12) * 60_000 + 10_000), '3d'],
    ['none while unconfirmed', () => withConnected(clusterRecord({ context: 'prod' }), { unconfirmed: true }), '—'],
    ['none while connecting', () => clusterRecord({ context: 'prod', connected: 'Unknown' }), '—'],
  ])('counts the uptime: %s', async (_name, cluster, want) => {
    const { popup } = await openStatus(cluster());
    expect(within(popup).getByText('Uptime').nextElementSibling).toHaveTextContent(want);
  });

  const downFor = (ms: number, over: { unconfirmed?: boolean } = {}) =>
    withConnected(clusterRecord({ context: 'prod', connected: 'False' }), { transitionedAt: since(ms), ...over });

  it.each<[string, () => Cluster, string]>([
    ['minutes', () => downFor(5 * 60_000 + 10_000), '5m'],
    ['none while unconfirmed', () => downFor(60_000, { unconfirmed: true }), '—'],
  ])('counts the downtime in place of the uptime: %s', async (_name, cluster, want) => {
    const { popup } = await openStatus(cluster());
    expect(within(popup).queryByText('Uptime')).not.toBeInTheDocument();
    expect(within(popup).getByText('Downtime').nextElementSibling).toHaveTextContent(want);
  });

  it('shows the download rate beside its sparkline of it alone, a dash before the window', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod' }));
    const download = () => within(popup).getByText('Download').nextElementSibling!;
    expect(download()).toHaveTextContent('—');

    await act(async () =>
      push('clusterDataRateSampleFeed', {
        intervalSeconds: 2,
        windowSeconds: 120,
        samples: [rate(512, 64), rate(2048, 0)],
      }),
    );
    expect(download()).toHaveTextContent('2.0 KB/s');
    // The download alone: the sent rate has no label here to explain a second line.
    expect(section(popup, 'Connection').querySelectorAll('svg polyline')).toHaveLength(1);
  });

  it('shows a hovered sample’s rate and age in a tooltip, leaving the download figure alone', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod' }));
    // Past a minute, still in seconds.
    const at = new Date(Date.now() - 95_000).toISOString();
    await act(async () =>
      push('clusterDataRateSampleFeed', {
        intervalSeconds: 2,
        windowSeconds: 4,
        samples: [{ ...rate(1024, 0), at }, rate(2048, 0)],
      }),
    );
    // The verdict's check is an SVG too; the sparkline is the one that stretches.
    const svg = section(popup, 'Connection').querySelector<SVGSVGElement>('svg[preserveAspectRatio="none"]')!;
    svg.getBoundingClientRect = () => ({ left: 0, width: 100 }) as DOMRect;

    fireEvent.pointerMove(svg, { clientX: 0 });
    expect(within(popup).getByRole('tooltip')).toHaveTextContent('1.0 KB/s · 95s ago');
    expect(within(popup).getByText('Download').nextElementSibling).toHaveTextContent('2.0 KB/s');
  });

  it('counts the cache’s kinds and objects, and when data last arrived', async () => {
    const cluster = withHealth(clusterRecord({ context: 'prod' }), {
      lastUpdateAt: new Date(Date.now() - 2_000).toISOString(),
    });
    const { popup } = await openStatus(cluster);
    const stat = (label: string) => within(popup).getByText(label).nextElementSibling!;
    expect(stat('Populated kinds')).toHaveTextContent('—');
    expect(stat('Objects')).toHaveTextContent('—');
    expect(stat('Last event')).toHaveTextContent(/^\ds ago$/);

    await act(async () =>
      push('clusterCacheStatsStateWatch', {
        exists: true,
        bytes: 0,
        dbBytes: 0,
        walBytes: 0,
        shmBytes: 0,
        objectCount: 12384,
        kindCount: 29,
      }),
    );
    expect(stat('Populated kinds')).toHaveTextContent('29');
    expect(stat('Objects')).toHaveTextContent('12,384');
  });

  it('asks nothing of a cluster with no cache yet, and dashes its counts', async () => {
    const { popup } = await openStatus({ ...clusterRecord({ context: 'prod' }), activeCache: null });
    const stat = (label: string) => within(popup).getByText(label).nextElementSibling!;
    expect(stat('Populated kinds')).toHaveTextContent('—');
    expect(stat('Objects')).toHaveTextContent('—');
    expect(stat('Last event')).toHaveTextContent('—');
    const queries = invokeMock.mock.calls
      .filter(([cmd]) => cmd === 'graphql_subscribe')
      .map(([, arg]) => (arg as { query: string }).query);
    expect(queries.some((q) => q.includes('clusterCacheStatsStateWatch'))).toBe(false);
  });

  it('gives a cache with no write yet no last event', async () => {
    const { popup } = await openStatus(clusterRecord({ context: 'prod' }));
    expect(within(popup).getByText('Last event').nextElementSibling).toHaveTextContent('—');
  });

  it('counts the paused kinds in a current verdict', async () => {
    const cluster = withHealth(clusterRecord({ context: 'prod' }), { totalKinds: 48, pausedKinds: 2 });
    const { popup } = await openStatus(cluster);
    expect(within(section(popup, 'Cache')).getByText('Current · 2 paused')).toHaveAttribute('data-tone', 'ok');
  });

  it('counts the caught-up kinds in a syncing verdict, leaving out the paused ones', async () => {
    const cluster = withHealth(clusterRecord({ context: 'prod', reason: 'Syncing' }), {
      totalKinds: 48,
      unhealthyKinds: 5,
      pausedKinds: 2,
    });
    const { popup } = await openStatus(cluster);
    expect(within(section(popup, 'Cache')).getByText('Syncing (41/46)…')).toHaveAttribute('data-tone', 'ok');
  });

  it('names a syncing cache by its label before discovery lands', async () => {
    const cluster = withHealth(clusterRecord({ context: 'prod', reason: 'Unknown' }), { totalKinds: 0 });
    const { popup } = await openStatus(cluster);
    expect(within(section(popup, 'Cache')).getByText('Syncing')).toBeInTheDocument();
  });
});
