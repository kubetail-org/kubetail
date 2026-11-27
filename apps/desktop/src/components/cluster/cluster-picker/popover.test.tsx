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

import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

// Mocks ---------------------------------------------------------------

// The scope the picker switches, and the dialog host its footer asks.
const { useActiveKubeContextMock, setContext, openDialog } = vi.hoisted(() => ({
  useActiveKubeContextMock: vi.fn(),
  setContext: vi.fn(),
  openDialog: vi.fn(),
}));
vi.mock('@/lib/active-kube-context', () => ({ useActiveKubeContext: useActiveKubeContextMock }));
vi.mock('@/lib/dialog', () => ({ useDialog: () => ({ openDialog }) }));
// The records only; the lookups over them are the real ones.
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: () => ({ clusters: [] }),
}));

// The focused log view, which a switch clears.
const { clearView } = vi.hoisted(() => ({ clearView: vi.fn() }));
vi.mock('@/lib/logs-view', () => ({ useLogsView: () => ({ clear: clearView }) }));

// The picker navigates itself rather than going through `setContext`, so the router
// is the seam for what it writes — and for where the window is, which decides
// whether the switch also has a chat route to leave.
const { navigate, location } = vi.hoisted(() => ({ navigate: vi.fn(), location: { pathname: '/dashboard' } }));
vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => navigate,
  useLocation: ({ select }: { select: (loc: { pathname: string }) => unknown }) => select(location),
}));

const { ClusterPicker } = await import('./popover');

// Helpers -------------------------------------------------------------

function serveContexts(names: string[], over: { context?: string; phase?: string } = {}) {
  useActiveKubeContextMock.mockReturnValue({
    context: over.context ?? names[0] ?? '',
    contexts: names.map((name) => ({ name, cluster: name, user: name, clusterEntry: null })),
    active: undefined,
    phase: over.phase ?? 'live',
    setContext,
  });
}

async function openPicker() {
  const user = userEvent.setup();
  render(
    <ClusterPicker anchorRef={{ current: null }}>
      <button type="button">Open picker</button>
    </ClusterPicker>,
  );
  await user.click(screen.getByRole('button', { name: 'Open picker' }));
  await screen.findByText('Clusters');
  return user;
}

const row = (name: string) => screen.getByText(name, { selector: '[data-testid="cluster-name"]' }).closest('button')!;

beforeEach(() => {
  vi.clearAllMocks();
  location.pathname = '/dashboard';
  serveContexts(['prod', 'staging']);
});

// Tests ---------------------------------------------------------------

describe('ClusterPicker', () => {
  // A chat and a namespace belong to a cluster, so neither follows a cluster switch —
  // and the rest of the window's state does.
  it('switches cluster from a row, closing the panel’s chat and the namespace scope', async () => {
    const user = await openPicker();
    await user.click(row('staging'));

    expect(navigate).toHaveBeenCalledTimes(1);
    const { to, search } = navigate.mock.calls[0][0];
    expect(to).toBe('.');
    expect(search({ kubeContext: 'prod', resource: 'pods', namespace: 'checkout', chat: 'c1' })).toEqual({
      kubeContext: 'staging',
      resource: 'pods',
      namespace: undefined,
      chat: undefined,
    });
    expect(setContext).not.toHaveBeenCalled();
    expect(screen.queryByText('Clusters')).not.toBeInTheDocument();
  });

  it('clears the focused log view on a switch', async () => {
    const user = await openPicker();
    await user.click(row('staging'));

    expect(clearView).toHaveBeenCalledTimes(1);
  });

  // In chat mode the open chat is the route, so the switch has to leave it: a chat of
  // the cluster the window just left would otherwise greet it with the out-of-scope
  // notice, which is for arriving at such a chat rather than for stepping off one.
  it('leaves an open chat for the new cluster’s unstarted one', async () => {
    location.pathname = '/chat/c1';
    const user = await openPicker();
    await user.click(row('staging'));

    const { to, search } = navigate.mock.calls[0][0];
    expect(to).toBe('/chat');
    expect(search({ kubeContext: 'prod' })).toEqual({ kubeContext: 'staging', namespace: undefined, chat: undefined });
  });

  it('closes without navigating when the window’s own cluster is picked', async () => {
    const user = await openPicker();
    await user.click(row('prod'));

    expect(navigate).not.toHaveBeenCalled();
    expect(clearView).not.toHaveBeenCalled();
    expect(screen.queryByText('Clusters')).not.toBeInTheDocument();
  });

  it('opens the Clusters dialog from its footer, and closes', async () => {
    const user = await openPicker();
    await user.click(screen.getByRole('button', { name: 'Manage clusters…' }));

    expect(openDialog).toHaveBeenCalledWith('clusters');
    expect(screen.queryByText('Clusters')).not.toBeInTheDocument();
  });

  it('says it is still connecting, its footer still there', async () => {
    serveContexts([], { phase: 'connecting' });
    await openPicker();

    expect(screen.getByText('Connecting…')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Manage clusters…' })).toBeInTheDocument();
  });

  // The footer is how a user with no kubeconfig reaches the dialog's orphaned caches.
  it('says there is no kubeconfig once connected with none, its footer still there', async () => {
    serveContexts([], { phase: 'live' });
    await openPicker();

    expect(screen.getByText('No kubeconfig')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Manage clusters…' })).toBeInTheDocument();
  });

  it('focuses the filter on open', async () => {
    serveContexts(['a', 'b', 'c', 'd', 'e', 'f']);
    await openPicker();

    // The popup moves focus once it has opened.
    await waitFor(() => expect(screen.getByRole('textbox', { name: 'Filter clusters' })).toHaveFocus());
  });

  // jsdom lays nothing out, so this checks the style that sizes it.
  it('is at least 640px wide, wider than the omnibox it hangs from, and capped to the frame', async () => {
    serveContexts(['a']);
    await openPicker();
    const popup = screen.getByTestId('cluster-picker');
    expect(popup.style.width).toBe('max(var(--anchor-width), 640px)');
    // The room `OmniboxPopover`'s positioner leaves inside the window frame.
    expect(popup.style.maxWidth).toBe('var(--available-width)');
  });
});
