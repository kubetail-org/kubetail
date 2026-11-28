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

import { act, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useEffect, useSyncExternalStore } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Incident, IncidentOccurrence, MonitorTarget } from '@/lib/incidents';
import { incidentRecord, monitorTargetRecord, occurrenceRecord } from '@/test-utils';

// The provider's hooks are its own test's; here they answer from `state`, and the
// feed records which incident it was asked for.
const { state, feedFor } = vi.hoisted(() => ({
  state: {
    targets: null as MonitorTarget[] | null,
    incidents: null as Incident[] | null,
    occurrences: [] as IncidentOccurrence[],
    snapshotComplete: true,
    windowClusterID: undefined as string | undefined,
    clusterIDs: ['id-prod', 'id-dev'] as string[] | null,
  },
  feedFor: vi.fn(),
}));

// The hooks read `state` through a store, so a change re-renders its readers as the
// provider's would.
const { listeners } = vi.hoisted(() => ({ listeners: new Set<() => void>() }));
let version = 0;
function useStateVersion() {
  return useSyncExternalStore(
    (onChange) => {
      listeners.add(onChange);
      return () => listeners.delete(onChange);
    },
    () => version,
  );
}
function update(change: Partial<typeof state>) {
  Object.assign(state, change);
  act(() => {
    version += 1;
    listeners.forEach((notify) => notify());
  });
}
vi.mock('@/lib/incidents', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/incidents')>()),
  useMonitorTargets: () => {
    useStateVersion();
    return state.targets;
  },
  useIncidents: (targetID?: string) => {
    useStateVersion();
    return targetID === undefined ? state.incidents : (state.incidents?.filter((i) => i.targetID === targetID) ?? null);
  },
  useMonitorSignals: () => new Map(),
  useIncidentOccurrenceEventFeed: (incidentID: string | undefined) => {
    feedFor(incidentID);
    return { occurrences: incidentID ? state.occurrences : [], snapshotComplete: state.snapshotComplete };
  },
}));
vi.mock('@/lib/active-cluster', () => ({ useActiveCluster: () => ({ clusterID: state.windowClusterID }) }));
vi.mock('@/lib/clusters', () => ({
  useClusters: () => {
    useStateVersion();
    return { clusters: state.clusterIDs?.map((id) => ({ id })) ?? null };
  },
}));

const { DialogProvider, useDialog } = await import('@/lib/dialog');
const { IncidentsDialog } = await import('./dialog');

const prod = monitorTargetRecord({ id: 't-prod', clusterID: 'id-prod', label: 'prod' });
const dev = monitorTargetRecord({ id: 't-dev', clusterID: 'id-dev', label: 'dev' });
const onProd = incidentRecord({ id: 'i-prod', targetID: 't-prod', subjectLabel: 'Deployment default/web' });
const onDev = incidentRecord({ id: 'i-dev', targetID: 't-dev', subjectLabel: 'Deployment default/api' });

function Harness({ on }: { on?: string }) {
  const { openDialog, activeDialog, closeDialog } = useDialog();
  useEffect(() => openDialog('incidents', on), [openDialog, on]);
  return activeDialog === 'incidents' && <IncidentsDialog open onOpenChange={() => closeDialog()} />;
}

const tree = (on?: string) => (
  <DialogProvider>
    <Harness on={on} />
  </DialogProvider>
);

function renderDialog(on?: string) {
  const view = render(tree(on));
  return {
    ...view,
    rerenderOn: (next?: string) => view.rerender(tree(next)),
  };
}

// Renders the dialog and waits for its open, which the harness makes in an effect.
async function openIncidentsDialog(on?: string) {
  const view = renderDialog(on);
  await screen.findByRole('dialog', { name: 'Incidents' });
  return view;
}

const rows = () => within(screen.getByRole('list', { name: 'Incidents' })).queryAllByRole('button');
const filter = () => screen.getByRole('combobox', { name: 'Target' });

beforeEach(() => {
  vi.clearAllMocks();
  state.targets = [dev, prod];
  state.incidents = [onProd, onDev];
  state.occurrences = [];
  state.snapshotComplete = true;
  state.windowClusterID = 'id-prod';
  state.clusterIDs = ['id-prod', 'id-dev'];
});

describe('IncidentsDialog', () => {
  it("opens filtered to its window's cluster's target, with nothing selected", async () => {
    await openIncidentsDialog();
    expect(filter()).toHaveTextContent('prod');
    expect(rows().map((r) => r.textContent)).toEqual([expect.stringContaining('Deployment default/web')]);
    expect(screen.getByText('Select an incident to see its occurrences.')).toBeInTheDocument();
  });

  it('opens on All targets in a window with no cluster target', async () => {
    state.windowClusterID = undefined;
    await openIncidentsDialog();
    expect(filter()).toHaveTextContent('All targets');
    expect(rows()).toHaveLength(2);
  });

  it('opens on an incident filtered to its target, whichever cluster the window is on', async () => {
    await openIncidentsDialog('i-dev');
    expect(filter()).toHaveTextContent('dev');
    expect(rows()[0]).toHaveAttribute('aria-pressed', 'true');
    expect(feedFor).toHaveBeenLastCalledWith('i-dev');
  });

  it("opens on its window's filter, with nothing selected, when no incident has the id it was opened on", async () => {
    await openIncidentsDialog('i-gone');
    expect(filter()).toHaveTextContent('prod');
    expect(rows()[0]).toHaveAttribute('aria-pressed', 'false');
  });

  it('says Loading, and lists nothing, before the snapshots', async () => {
    state.targets = null;
    state.incidents = null;
    renderDialog();
    expect(await screen.findByText('Loading…')).toBeInTheDocument();
    expect(screen.queryByRole('list', { name: 'Incidents' })).not.toBeInTheDocument();
  });

  it("says Nothing found under its target's line", async () => {
    state.incidents = [];
    state.targets = [{ ...prod, readState: 'Failed', unknownSignals: [{ name: 'x', reason: 'PermissionDenied' }] }];
    await openIncidentsDialog();
    expect(screen.getByText(/cannot be read: access denied/)).toBeInTheDocument();
    expect(screen.getByText('Nothing found')).toBeInTheDocument();
  });

  it('shows a line per target on All targets', async () => {
    state.windowClusterID = undefined;
    state.targets = [
      { ...dev, enabled: false },
      { ...prod, readState: 'NeverRead' },
    ];
    await openIncidentsDialog();
    expect(screen.getByText(/Monitoring is off for/)).toBeInTheDocument();
    expect(screen.getByText(/has not been read yet/)).toBeInTheDocument();
  });

  it('drops the filter and the selection at once when their target leaves, cancelling the feed', async () => {
    await openIncidentsDialog('i-prod');
    expect(feedFor).toHaveBeenLastCalledWith('i-prod');

    // The cluster's delete hides its target; the incident's own Deleted is held back.
    // The detail unmounts, which cancels its feed: nothing asks for it again.
    feedFor.mockClear();
    update({ targets: [dev], incidents: [onDev] });
    expect(filter()).toHaveTextContent('All targets');
    expect(rows()[0]).toHaveAttribute('aria-pressed', 'false');
    expect(screen.queryByRole('region', { name: 'Incident' })).not.toBeInTheDocument();
    expect(feedFor).not.toHaveBeenCalled();
  });

  it('keeps what the user picked through a reconnect', async () => {
    const user = userEvent.setup();
    await openIncidentsDialog('i-prod');
    await user.click(filter());
    await user.click(await screen.findByRole('option', { name: 'All targets' }));
    await user.click(rows()[1]);

    // A reconnect masks the provider's lists until its new snapshot lands.
    update({ targets: null, incidents: null });
    update({ targets: [dev, prod], incidents: [onProd, onDev] });
    expect(filter()).toHaveTextContent('All targets');
    expect(rows()[1]).toHaveAttribute('aria-pressed', 'true');
  });

  it('clears a selection the picked filter leaves out', async () => {
    const user = userEvent.setup();
    await openIncidentsDialog('i-prod');
    await user.click(filter());
    await user.click(await screen.findByRole('option', { name: 'dev' }));
    expect(screen.queryByRole('region', { name: 'Incident' })).not.toBeInTheDocument();
  });

  it('closes the detail when its incident is deleted', async () => {
    renderDialog('i-prod');
    await screen.findByRole('region', { name: 'Incident' });
    update({ incidents: [onDev] });
    expect(screen.queryByRole('region', { name: 'Incident' })).not.toBeInTheDocument();
  });

  it("lists every target's incidents on All targets, and selects the one pressed", async () => {
    const user = userEvent.setup();
    await openIncidentsDialog();
    await user.click(filter());
    await user.click(await screen.findByRole('option', { name: 'All targets' }));
    expect(rows()).toHaveLength(2);

    await user.click(rows()[1]);
    expect(rows()[1]).toHaveAttribute('aria-pressed', 'true');
    expect(feedFor).toHaveBeenLastCalledWith('i-dev');
  });

  it('marks an incident from a server its target no longer reaches, and reads a stale one as Last seen', async () => {
    state.incidents = [
      incidentRecord({
        id: 'old',
        targetID: 't-prod',
        targetUID: 'uid-old',
        open: false,
        lastSeenAt: '2026-11-01T00:00:00Z',
        lastEndReason: 'Stale',
      }),
    ];
    await openIncidentsDialog();
    expect(rows()[0]).toHaveTextContent('On a server this target no longer reaches');
    expect(rows()[0]).toHaveTextContent(/Last seen/);
    expect(rows()[0]).not.toHaveTextContent(/Resolved/);
  });

  it('draws a subject label, its attributes and an occurrence detail as visible text', async () => {
    state.incidents = [
      incidentRecord({
        id: 'i-prod',
        targetID: 't-prod',
        subjectLabel: 'Deployment default/‮web',
        subjectAttributes: [{ label: 'name', value: 'we\u001b[31mb' }],
      }),
    ];
    state.occurrences = [occurrenceRecord({ detail: [{ label: 'reason', value: 'Crash​Loop' }] })];
    renderDialog('i-prod');
    const detail = await screen.findByRole('region', { name: 'Incident' });
    expect(within(detail).getByText('\\u{202E}')).toBeInTheDocument();
    expect(within(detail).getByText('\\u{1B}')).toBeInTheDocument();
    expect(within(detail).getByText('\\u{200B}')).toBeInTheDocument();
  });

  it('lists occurrences by number, says ongoing or how one ended, and says when it shows the newest 100', async () => {
    state.incidents = [{ ...onProd, occurrenceCount: 140 }];
    state.occurrences = [
      occurrenceRecord({ id: 'o2', number: 2, startedAt: '2026-11-01T00:00:00Z' }),
      occurrenceRecord({
        id: 'o1',
        number: 1,
        startedAt: '2026-11-02T00:00:00Z',
        endedAt: '2026-11-02T00:05:00Z',
        endReason: 'Stopped',
      }),
    ];
    renderDialog('i-prod');
    const detail = await screen.findByRole('region', { name: 'Incident' });
    const items = within(within(detail).getByRole('list', { name: 'Occurrences' })).getAllByRole('listitem');
    expect(items.map((i) => i.textContent)).toEqual([
      expect.stringMatching(/#2.*ongoing/),
      expect.stringMatching(/#1.*5m.*Stopped/),
    ]);
    expect(detail).toHaveTextContent('Showing the newest 100 of 140 occurrences.');
  });

  it('says how long an ended occurrence lasted when it carries no end reason', async () => {
    state.occurrences = [occurrenceRecord({ endedAt: '2026-11-01T00:02:00Z', endReason: null })];
    renderDialog('i-prod');
    const detail = await screen.findByRole('region', { name: 'Incident' });
    expect(within(within(detail).getByRole('list', { name: 'Occurrences' })).getByRole('listitem')).toHaveTextContent(
      /#1.*2m$/,
    );
  });

  it('shows the detail loading until its Bookmark', async () => {
    state.snapshotComplete = false;
    renderDialog('i-prod');
    const detail = await screen.findByRole('region', { name: 'Incident' });
    expect(within(detail).getByText('Loading occurrences…')).toBeInTheDocument();
  });

  it('drops a removed cluster’s target and incidents while the incident watch reconnects', async () => {
    await openIncidentsDialog('i-dev');
    expect(filter()).toHaveTextContent('dev');
    update({ targets: null, incidents: null });
    update({ clusterIDs: ['id-prod'] });
    expect(filter()).toHaveTextContent('All targets');
    expect(rows().map((r) => r.textContent)).toEqual([expect.stringContaining('Deployment default/web')]);
    expect(screen.getByText('Select an incident to see its occurrences.')).toBeInTheDocument();
  });

  it('selects the incident of an open that arrives while it is open', async () => {
    const { rerenderOn } = await openIncidentsDialog('i-prod');
    expect(screen.getByRole('button', { name: /default\/web/, pressed: true })).toBeInTheDocument();
    rerenderOn('i-dev');
    await act(async () => {});
    expect(filter()).toHaveTextContent('dev');
    expect(screen.getByRole('button', { name: /default\/api/, pressed: true })).toBeInTheDocument();
  });

  it("reopens after a close on its window's filter, with nothing selected", async () => {
    const user = userEvent.setup();
    const { rerenderOn } = await openIncidentsDialog('i-dev');
    await user.keyboard('{Escape}');
    rerenderOn(undefined);
    await act(async () => {});
    expect(filter()).toHaveTextContent('prod');
    expect(rows()[0]).toHaveAttribute('aria-pressed', 'false');
  });
});
