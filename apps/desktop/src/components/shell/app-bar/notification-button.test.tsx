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

import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Incident, MonitorTarget } from '@/lib/incidents';
import { incidentRecord, monitorTargetRecord } from '@/test-utils';

const { state, openDialog } = vi.hoisted(() => ({
  state: { targets: null as MonitorTarget[] | null, incidents: null as Incident[] | null },
  openDialog: vi.fn(),
}));
vi.mock('@/lib/incidents', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/incidents')>()),
  useMonitorTargets: () => state.targets,
  useIncidents: () => state.incidents,
  useMonitorSignals: () => new Map(),
}));
vi.mock('@/lib/dialog', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/dialog')>()),
  useDialog: () => ({ openDialog }),
}));

const { NotificationButton } = await import('./notification-button');

// Helpers -------------------------------------------------------------

const button = () => screen.getByRole('button', { name: /^Notifications/ });

async function openBell() {
  render(<NotificationButton />);
  await userEvent.click(button());
  return screen.findByRole('dialog', { name: 'Open incidents' });
}

const prod = monitorTargetRecord({ id: 't-prod', label: 'prod' });
const dev = monitorTargetRecord({ id: 't-dev', clusterID: 'id-dev', label: 'dev' });

beforeEach(() => {
  vi.clearAllMocks();
  state.targets = [dev, prod];
  state.incidents = [
    incidentRecord({
      id: 'older',
      targetID: 't-prod',
      subjectLabel: 'Deployment default/web',
      lastStartedAt: '2026-11-01T00:00:00Z',
    }),
    incidentRecord({
      id: 'ended',
      targetID: 't-prod',
      open: false,
      lastSeenAt: '2026-11-02T00:00:00Z',
      lastEndReason: 'Resolved',
    }),
    incidentRecord({
      id: 'newer',
      targetID: 't-dev',
      subjectLabel: 'Deployment default/api',
      lastStartedAt: '2026-11-03T00:00:00Z',
    }),
  ];
});

// Tests ---------------------------------------------------------------

describe('NotificationButton', () => {
  it('counts every target’s open incidents on the button', () => {
    render(<NotificationButton />);
    expect(button()).toHaveAccessibleName('Notifications, 2 open incidents');
    expect(within(button()).getByText('2')).toBeInTheDocument();
  });

  it('leaves out a baseline incident, in the list and the count', async () => {
    state.incidents = [...state.incidents!, incidentRecord({ id: 'found', targetID: 't-dev', baseline: true })];
    const bell = await openBell();
    expect(button()).toHaveAccessibleName('Notifications, 2 open incidents');
    expect(within(within(bell).getByRole('list')).getAllByRole('button')).toHaveLength(2);
  });

  it('counts one open incident in the singular', () => {
    state.incidents = [incidentRecord({ id: 'one', targetID: 't-prod' })];
    render(<NotificationButton />);
    expect(button()).toHaveAccessibleName('Notifications, 1 open incident');
  });

  it('says Loading before the snapshots, and still offers View all', async () => {
    state.targets = null;
    state.incidents = null;
    const bell = await openBell();
    expect(within(bell).getByText('Loading…')).toBeInTheDocument();
    expect(within(bell).getByRole('button', { name: 'View all' })).toBeInTheDocument();
  });

  it('shows no count before the snapshots', () => {
    state.targets = null;
    state.incidents = null;
    render(<NotificationButton />);
    expect(button()).toHaveAccessibleName('Notifications');
  });

  it('fits the window, its list scrolling above View all', async () => {
    const bell = await openBell();
    expect(bell).toHaveClass('max-h-(--available-height)');
    expect(within(bell).getByRole('list').parentElement).toHaveClass('min-h-0', 'overflow-y-auto');
  });

  it('lists the open incidents newest first, each naming its target', async () => {
    const bell = await openBell();
    const items = within(within(bell).getByRole('list')).getAllByRole('button');
    expect(items.map((i) => i.textContent)).toEqual([
      expect.stringMatching(/default\/api.*dev/),
      expect.stringMatching(/default\/web.*prod/),
    ]);
  });

  it('opens the dialog on a pressed incident, and with none from View all', async () => {
    const bell = await openBell();
    await userEvent.click(within(bell).getAllByRole('button', { name: /default\/api/ })[0]);
    expect(openDialog).toHaveBeenCalledWith('incidents', 'newer');

    await userEvent.click(button());
    await userEvent.click(await screen.findByRole('button', { name: 'View all' }));
    expect(openDialog).toHaveBeenLastCalledWith('incidents', undefined);
  });

  it('closes when its Monitoring settings link opens Settings', async () => {
    state.incidents = [];
    state.targets = [{ ...dev, enabled: false }];
    const bell = await openBell();
    await userEvent.click(within(bell).getByRole('button', { name: 'Monitoring settings' }));
    expect(openDialog).toHaveBeenCalledWith('settings', 'Monitoring');
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Open incidents' })).not.toBeInTheDocument());
  });

  it('says No open incidents above the line of a target that is off, and still offers View all', async () => {
    state.incidents = [];
    state.targets = [
      { ...dev, enabled: false },
      { ...prod, readState: 'NeverRead' },
    ];
    const bell = await openBell();
    expect(within(bell).getByText('No open incidents.')).toBeInTheDocument();
    expect(within(bell).getByText(/Monitoring is off for/)).toHaveTextContent('dev');
    expect(within(bell).queryByText(/has not been read yet/)).not.toBeInTheDocument();
    expect(within(bell).getByRole('button', { name: 'View all' })).toBeInTheDocument();
  });
});
