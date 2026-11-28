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

import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Cluster } from '@/lib/clusters';
import type {
  MonitoringTriageModel,
  MonitorSignal,
  MonitorTarget,
  MonitorTargetEnabledSet,
  NotificationPermission,
} from '@/lib/incidents';
import { clusterRecord, monitorTargetRecord } from '@/test-utils';

const { state, set, turnOff } = vi.hoisted(() => ({
  state: {} as {
    targets: MonitorTarget[] | null;
    signals: Map<string, MonitorSignal>;
    switch: Omit<MonitorTargetEnabledSet, 'set'>;
    clusters: Cluster[] | null;
    permission: NotificationPermission | undefined;
    setting: MonitoringTriageModel | null | undefined;
    offError: string | null;
  },
  set: vi.fn(),
  turnOff: vi.fn(),
}));
vi.mock('@/lib/incidents', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/incidents')>()),
  useMonitorTargets: () => state.targets,
  useMonitorSignals: () => state.signals,
  useMonitorTargetEnabledSet: () => ({ ...state.switch, set }),
  useNotificationPermission: () => state.permission,
  useMonitoringTriageModel: () => state.setting,
  useMonitoringTriageModelOff: () => ({ sending: false, error: state.offError, turnOff }),
}));
vi.mock('@/lib/models', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/models')>()),
  useModels: () => ({
    models: [
      {
        provider: { id: 'anthropic', label: 'Anthropic' },
        id: 'claude',
        label: 'Claude',
        efforts: [],
        defaultEffort: '',
      },
    ],
    snapshotComplete: true,
  }),
}));
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: () => ({ clusters: state.clusters, connected: true }),
}));
vi.mock('@/lib/dialog', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/dialog')>()),
  useDialog: () => ({ openDialog: vi.fn() }),
}));

const { MonitoringSettings } = await import('./monitoring');

const signal = (name: string, label: string): [string, MonitorSignal] => [
  name,
  { name, label, integration: 'Kubernetes', severity: 'Warning' },
];

beforeEach(() => {
  vi.clearAllMocks();
  state.targets = [];
  state.signals = new Map([
    signal('kubernetes.crash_loop', 'Crash loop'),
    signal('kubernetes.image_pull', 'Image pull failing'),
  ]);
  state.switch = { changing: new Set(), errors: new Map() };
  state.clusters = [];
  state.permission = 'Granted';
  state.setting = null;
  state.offError = null;
});

const row = (label: string) => screen.getByRole('switch', { name: label }).closest('li')!;

describe('MonitoringSettings', () => {
  it('states that automatic triage spends model calls and its cap, off and on', () => {
    const view = render(<MonitoringSettings />);
    const section = () => screen.getByRole('region', { name: 'Automatic triage' });
    expect(section()).toHaveTextContent('Off. Turn it on beside an incident’s Triage with AI.');
    expect(section()).toHaveTextContent(
      'It spends model calls while nobody is watching. At most 6 triages start per target per hour',
    );
    expect(within(section()).queryByRole('button')).not.toBeInTheDocument();

    state.setting = { providerID: 'anthropic', modelID: 'claude', effort: 'high' };
    view.rerender(<MonitoringSettings />);
    expect(section()).toHaveTextContent('Each new incident is triaged with Claude, effort high.');

    state.setting = { providerID: 'anthropic', modelID: 'claude', effort: 'hi\u202egh' };
    view.rerender(<MonitoringSettings />);
    expect(within(section()).getByTitle('An invisible character')).toBeInTheDocument();
    expect(section().textContent).not.toContain('\u202e');
  });

  it('turns automatic triage off, and draws nothing of it before the setting is read', async () => {
    state.setting = { providerID: 'other', modelID: 'm-1', effort: null };
    const view = render(<MonitoringSettings />);
    expect(screen.getByRole('region', { name: 'Automatic triage' })).toHaveTextContent('triaged with m-1.');
    await userEvent.click(screen.getByRole('button', { name: 'Turn off' }));
    expect(turnOff).toHaveBeenCalled();

    state.setting = undefined;
    view.rerender(<MonitoringSettings />);
    expect(screen.queryByRole('region', { name: 'Automatic triage' })).not.toBeInTheDocument();
  });

  it('says why turning automatic triage off was refused', () => {
    state.setting = { providerID: 'anthropic', modelID: 'claude', effort: null };
    state.offError = 'Monitoring is stopping';
    render(<MonitoringSettings />);
    expect(screen.getByRole('alert')).toHaveTextContent('Monitoring is stopping');
  });

  it('draws nothing before the targets land', () => {
    state.targets = null;
    const { container } = render(<MonitoringSettings />);
    expect(container).toBeEmptyDOMElement();
  });

  it('says while notifications are denied how to turn them on, and nothing otherwise', () => {
    state.permission = 'Denied';
    const { rerender } = render(<MonitoringSettings />);
    expect(screen.getByRole('status')).toHaveTextContent(
      'Notifications are off for Kubetail, so a new incident raises none. Turn them on in System Settings › Notifications › Kubetail.',
    );
    for (const permission of ['Granted', 'NotDetermined', undefined] as const) {
      state.permission = permission;
      rerender(<MonitoringSettings />);
      expect(screen.queryByRole('status')).not.toBeInTheDocument();
    }
  });

  it('says what is watched, by each signal label in order, and when', () => {
    render(<MonitoringSettings />);
    expect(screen.getByText(/watches each enabled target/)).toHaveTextContent(
      'While Kubetail is running, the tray included, it watches each enabled target for: Crash loop and Image pull failing. A quit app watches nothing.',
    );
  });

  it('names no signal before the signal list answers', () => {
    state.signals = new Map();
    render(<MonitoringSettings />);
    expect(screen.getByText(/watches each enabled target/)).toHaveTextContent(
      'While Kubetail is running, the tray included, it watches each enabled target for failures. A quit app watches nothing.',
    );
  });

  it('says so when there is no target', () => {
    render(<MonitoringSettings />);
    expect(screen.getByText('No targets to watch.')).toBeInTheDocument();
    expect(screen.queryByRole('switch')).toBeNull();
  });

  it("draws a row per target in the hook's order, each switch at the watch's enabled", () => {
    state.targets = [
      monitorTargetRecord({ id: 't1', label: 'dev', enabled: false }),
      monitorTargetRecord({ id: 't2', label: 'prod' }),
    ];
    render(<MonitoringSettings />);
    expect(screen.getAllByRole('switch').map((s) => s.getAttribute('aria-label'))).toEqual(['dev', 'prod']);
    expect(screen.getByRole('switch', { name: 'dev' })).not.toBeChecked();
    expect(screen.getByRole('switch', { name: 'prod' })).toBeChecked();
  });

  it('adds the kube context to rows that share a label, and to their switches', () => {
    state.clusters = [clusterRecord({ context: 'east' }), clusterRecord({ context: 'west' })];
    state.targets = [
      monitorTargetRecord({ id: 't1', label: 'prod', clusterID: 'id-east' }),
      monitorTargetRecord({ id: 't2', label: 'prod', clusterID: 'id-west' }),
      monitorTargetRecord({ id: 't3', label: 'dev', clusterID: 'id-east' }),
    ];
    render(<MonitoringSettings />);
    expect(screen.getAllByRole('switch').map((s) => s.getAttribute('aria-label'))).toEqual([
      'prod (east)',
      'prod (west)',
      'dev',
    ]);
    expect(within(row('prod (west)')).getByText('west')).toBeInTheDocument();
    expect(within(row('dev')).queryByText('east')).not.toBeInTheDocument();
  });

  it('sends a press and leaves the switch where the watch has it', async () => {
    state.targets = [monitorTargetRecord({ id: 't1', label: 'prod' })];
    render(<MonitoringSettings />);
    await userEvent.click(screen.getByRole('switch', { name: 'prod' }));
    expect(set).toHaveBeenCalledWith('t1', false);
    expect(screen.getByRole('switch', { name: 'prod' })).toBeChecked();
  });

  it("moves the switch when the target's Modified arrives, and draws a target added later on", () => {
    state.targets = [monitorTargetRecord({ id: 't1', label: 'prod' })];
    const { rerender } = render(<MonitoringSettings />);

    state.targets = [
      monitorTargetRecord({ id: 't2', label: 'new' }),
      monitorTargetRecord({ id: 't1', label: 'prod', enabled: false }),
    ];
    rerender(<MonitoringSettings />);
    expect(screen.getByRole('switch', { name: 'prod' })).not.toBeChecked();
    expect(screen.getByRole('switch', { name: 'new' })).toBeChecked();
  });

  it('disables the switch of a target in flight alone', () => {
    state.targets = [monitorTargetRecord({ id: 't1', label: 'prod' }), monitorTargetRecord({ id: 't2', label: 'dev' })];
    state.switch.changing = new Set(['t1']);
    render(<MonitoringSettings />);
    expect(screen.getByRole('switch', { name: 'prod' })).toHaveAttribute('aria-disabled', 'true');
    expect(screen.getByRole('switch', { name: 'dev' })).not.toHaveAttribute('aria-disabled', 'true');
  });

  it('shows a refused switch on its own row', () => {
    state.targets = [monitorTargetRecord({ id: 't1', label: 'prod' }), monitorTargetRecord({ id: 't2', label: 'dev' })];
    state.switch.errors = new Map([['t1', 'Record not found']]);
    render(<MonitoringSettings />);
    expect(within(row('prod')).getByRole('alert')).toHaveTextContent('Record not found');
    expect(within(row('dev')).queryByRole('alert')).toBeNull();
  });

  it('says why a target that is on is not read, and nothing of the read for one that is off', () => {
    const failed = {
      readState: 'Failed' as const,
      unknownSignals: [{ name: 'kubernetes.crash_loop', reason: 'PermissionDenied' as const }],
    };
    state.targets = [
      monitorTargetRecord({ id: 't1', label: 'prod', ...failed }),
      monitorTargetRecord({ id: 't2', label: 'dev', enabled: false, ...failed }),
    ];
    render(<MonitoringSettings />);
    expect(within(row('prod')).getByText(/cannot be read/)).toHaveTextContent('prod cannot be read: access denied.');
    expect(row('dev')).not.toHaveTextContent(/cannot be read|Monitoring is off/);
  });

  it("draws a target's label, its switch's name and its refusal as visible text", () => {
    state.targets = [monitorTargetRecord({ id: 't1', label: 'pr\u202Eod' })];
    state.switch.errors = new Map([['t1', 'refused\u202E']]);
    render(<MonitoringSettings />);
    expect(screen.getAllByText('\\u{202E}')).toHaveLength(2);
    expect(screen.getByRole('switch')).toHaveAttribute('aria-label', 'pr\\u{202E}od');
  });
});
