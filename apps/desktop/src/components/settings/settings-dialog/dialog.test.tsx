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
import userEvent from '@testing-library/user-event';
import { useEffect } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { mockTauriCore } from '@/test-utils';

const { invokeMock, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

// The tab's own behavior is its test's; here it only says when it is mounted.
const { clustersMounted } = vi.hoisted(() => ({ clustersMounted: vi.fn() }));
vi.mock('./clusters', () => ({
  ClustersSettings: () => {
    useEffect(() => {
      clustersMounted(true);
      return () => clustersMounted(false);
    }, []);
    return <p>The clusters</p>;
  },
}));

vi.mock('./sandbox', () => ({ SandboxSettings: () => <p>The sandbox</p> }));

// The Permissions pane draws over these hooks; its own test covers the rest.
const { sandboxAvailable } = vi.hoisted(() => ({ sandboxAvailable: { current: true } }));
vi.mock('@/lib/sandbox', () => ({ useSandbox: () => ({ available: sandboxAvailable.current }) }));
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: () => ({ clusters: [], connected: true }),
}));
vi.mock('@/lib/permission-settings', () => ({
  usePermissionSettings: () => ({
    settings: {
      targets: [{ target: 'Kubernetes', defaultMode: 'Ask', accounts: [] }],
      rules: [],
      alwaysAsks: [],
      accessControl: [],
    },
    error: null,
  }),
}));
vi.mock('@/lib/sandbox-cloud-access', () => ({
  useSandboxCloudAccess: () => ({ platforms: [], readFailed: false }),
}));

vi.mock('./connectors', () => ({ ConnectorsSettings: () => <p>The connectors</p> }));

vi.mock('./providers', async () => {
  const { useSelectSettingsTab } = await import('@/lib/settings-tab');
  return {
    ProvidersSettings: () => {
      const selectTab = useSelectSettingsTab();
      return (
        <button type="button" onClick={() => selectTab('Connectors')}>
          The providers
        </button>
      );
    },
  };
});

vi.mock('./memory', () => ({ MemorySettings: () => <p>The memory</p> }));
vi.mock('./environment', () => ({ EnvironmentSettings: () => <p>The environment</p> }));
vi.mock('./tools', () => ({ ToolsSettings: () => <p>The tools</p> }));

const { ThemeProvider } = await import('@/lib/theme');
const { SettingsDialog } = await import('./dialog');

function renderDialog(onOpenChange = vi.fn()) {
  render(
    <ThemeProvider>
      <SettingsDialog open onOpenChange={onOpenChange} />
    </ThemeProvider>,
  );
  return onOpenChange;
}

const NAMES = [
  'General',
  'Clusters',
  'Connectors',
  'Providers',
  'Memory',
  'Environment',
  'Tools',
  'Sandbox',
  'Permissions',
  'Monitoring',
];

beforeEach(() => {
  sandboxAvailable.current = true;
  invokeMock.mockReset();
  invokeMock.mockResolvedValue(undefined);
});

describe('SettingsDialog', () => {
  it('lists its tabs in a vertical tab list, General selected on open', async () => {
    renderDialog();
    await screen.findByRole('dialog', { name: 'Settings' });

    const list = screen.getByRole('tablist');
    expect(list).toHaveAttribute('aria-orientation', 'vertical');
    expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual(NAMES);
    expect(screen.getByRole('tab', { name: 'General', selected: true })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'General' })).toBeInTheDocument();
    expect(screen.getByRole('group', { name: 'Appearance' })).toBeInTheDocument();
  });

  it('draws the Sandbox tab', async () => {
    const user = userEvent.setup();
    renderDialog();
    await user.click(await screen.findByRole('tab', { name: 'Sandbox' }));
    expect(screen.getByRole('heading', { name: 'Sandbox' })).toBeInTheDocument();
    expect(screen.getByRole('tabpanel')).toHaveTextContent('The sandbox');
  });

  it('draws the Permissions pane under its one heading', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByRole('tab', { name: 'Permissions' }));
    expect(screen.getAllByRole('heading', { name: 'Permissions' })).toHaveLength(1);
    expect(screen.getByRole('tabpanel')).toHaveTextContent(
      'Modes and rules decide what sandboxed commands may change.',
    );
  });

  it('says why Permissions has nothing to set on a machine with no sandbox', async () => {
    const user = userEvent.setup();
    sandboxAvailable.current = false;
    renderDialog();

    await user.click(await screen.findByRole('tab', { name: 'Permissions' }));
    expect(screen.getByRole('tabpanel')).toHaveTextContent(
      'This machine has no sandbox, so every command asks before it runs. Modes and rules apply only to sandboxed commands.',
    );
  });

  it('draws the Connectors tab', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByRole('tab', { name: 'Connectors' }));

    const panel = screen.getByRole('tabpanel');
    expect(panel).toHaveTextContent('The connectors');
  });

  it('draws the Providers tab', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByRole('tab', { name: 'Providers' }));

    const panel = screen.getByRole('tabpanel');
    expect(panel).toHaveTextContent('The providers');
  });

  it('lets a pane select another tab', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByRole('tab', { name: 'Providers' }));
    await user.click(screen.getByRole('button', { name: 'The providers' }));

    expect(screen.getByRole('tab', { name: 'Connectors', selected: true })).toBeInTheDocument();
    expect(screen.getByRole('tabpanel')).toHaveTextContent('The connectors');
  });

  it('draws the Tools tab', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByRole('tab', { name: 'Tools' }));

    expect(screen.getByRole('tabpanel')).toHaveTextContent('The tools');
  });

  it('draws the Memory tab', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByRole('tab', { name: 'Memory' }));

    const panel = screen.getByRole('tabpanel');
    expect(panel).toHaveTextContent('The memory');
  });

  it('draws the Monitoring tab', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByRole('tab', { name: 'Monitoring' }));

    expect(screen.getByRole('heading', { name: 'Monitoring' })).toBeInTheDocument();
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Coming soon');
  });

  // Each row's cache size is a live watch; it must not run behind another tab.
  it('draws the Clusters tab while it is selected, and lets it go after', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByRole('tab', { name: 'Clusters' }));
    expect(screen.getByRole('tabpanel')).toHaveTextContent('The clusters');
    expect(clustersMounted).toHaveBeenLastCalledWith(true);

    await user.click(screen.getByRole('tab', { name: 'General' }));
    expect(clustersMounted).toHaveBeenLastCalledWith(false);
    expect(screen.queryByText('The clusters')).toBeNull();
  });

  it('moves between tabs with the arrow keys', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(await screen.findByRole('tab', { name: 'General' }));
    await user.keyboard('{ArrowDown}');

    expect(screen.getByRole('tab', { name: 'Clusters' })).toHaveFocus();
    expect(screen.getByRole('heading', { name: 'Clusters' })).toBeInTheDocument();

    await user.keyboard('{ArrowUp}');
    expect(screen.getByRole('tab', { name: 'General', selected: true })).toHaveFocus();
  });

  it('closes from the Close button in the pane header', async () => {
    const user = userEvent.setup();
    const onOpenChange = renderDialog();

    await user.click(await screen.findByRole('button', { name: 'Close' }));

    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
