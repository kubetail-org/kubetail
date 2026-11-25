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

import { createRootRoute, createRoute, Link, Outlet, retainSearchParams } from '@tanstack/react-router';
import { act, screen, waitFor } from '@testing-library/react';
import { Provider as UrqlProvider } from 'urql';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import {
  flushSubscriptionsSynchronously,
  mockTauriCore,
  pushClusterModified,
  pushClusters,
  renderWithRouter,
} from '@/test-utils';

// Mocks ---------------------------------------------------------------

// Frames pushed here are asserted on immediately.
flushSubscriptionsSynchronously();

const { invokeMock, channels, channelFor, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { createGraphqlClient } = await import('@/lib/graphql/client');
const { ClustersProvider } = await import('./clusters');
const { KubeConfigProvider } = await import('./kube-config');
const { useActiveKubeContext, useFixWindowContext } = await import('./active-kube-context');

// Helpers -------------------------------------------------------------

// A probe that surfaces the resolved active context and a way to change it, so
// tests can drive and observe the hook. Rendered on both pages.
function Probe() {
  const { context, noneSelected, setContext } = useActiveKubeContext();
  return (
    <div>
      <div data-testid="active">{context}</div>
      {noneSelected && <div>none-selected</div>}
      <button type="button" onClick={() => setContext('staging')}>
        pick-staging
      </button>
    </div>
  );
}

// Fixes the window's cluster in the param, as `AppLayout` does.
function Fixer() {
  useFixWindowContext();
  return null;
}

// A faithful stand-in for the real `_app` route: same `kubeContext` search param and
// the retainSearchParams middleware that carries it across the mode switch. The
// real route tree is exercised end-to-end in the app-layout test.
function buildTree({ fix = false } = {}) {
  const root = createRootRoute({
    component: () => (
      <UrqlProvider value={createGraphqlClient()}>
        <ClustersProvider>
          <KubeConfigProvider>
            {fix && <Fixer />}
            <Outlet />
          </KubeConfigProvider>
        </ClustersProvider>
      </UrqlProvider>
    ),
  });
  const app = createRoute({
    getParentRoute: () => root,
    id: 'app',
    validateSearch: (s: Record<string, unknown>): { kubeContext?: string } =>
      typeof s.kubeContext === 'string' ? { kubeContext: s.kubeContext } : {},
    search: { middlewares: [retainSearchParams(['kubeContext'])] },
    component: () => <Outlet />,
  });
  const chat = createRoute({
    getParentRoute: () => app,
    path: '/chat',
    component: () => (
      <>
        <Probe />
        <Link to="/dashboard">to-dashboard</Link>
      </>
    ),
  });
  const dashboard = createRoute({
    getParentRoute: () => app,
    path: '/dashboard',
    component: () => (
      <>
        <div>dashboard-page</div>
        <Probe />
      </>
    ),
  });
  return root.addChildren([app.addChildren([chat, dashboard])]);
}

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
    throw new Error(`unexpected ${cmd}`);
  });
});

describe('useActiveKubeContext', () => {
  it('resolves to the default context when no ?kubeContext= param is present', async () => {
    await renderWithRouter(buildTree(), '/chat');
    await act(async () => {
      pushClusters(channelFor, [
        { id: 'a', name: 'prod', current: true },
        { id: 'b', name: 'staging' },
      ]);
    });
    expect(screen.getByTestId('active')).toHaveTextContent('prod');
  });

  it("resolves to the user's default cluster ahead of the kubeconfig's current context", async () => {
    await renderWithRouter(buildTree(), '/chat');
    await act(async () => {
      pushClusters(channelFor, [
        { id: 'a', name: 'prod', current: true },
        { id: 'b', name: 'staging', isDefault: true },
      ]);
    });
    expect(screen.getByTestId('active')).toHaveTextContent('staging');
  });

  it('resolves to the ?kubeContext= param when it names a present context', async () => {
    await renderWithRouter(buildTree(), '/chat?kubeContext=staging');
    await act(async () => {
      pushClusters(channelFor, [
        { id: 'a', name: 'prod', current: true },
        { id: 'b', name: 'staging' },
      ]);
    });
    expect(screen.getByTestId('active')).toHaveTextContent('staging');
  });

  it('falls back to the default when the ?kubeContext= param names a gone context', async () => {
    await renderWithRouter(buildTree(), '/chat?kubeContext=ghost');
    await act(async () => {
      pushClusters(channelFor, [
        { id: 'a', name: 'prod', current: true },
        { id: 'b', name: 'staging' },
      ]);
    });
    expect(screen.getByTestId('active')).toHaveTextContent('prod');
  });

  it('setContext writes the choice to the URL search param', async () => {
    const { router } = await renderWithRouter(buildTree(), '/chat');
    await act(async () => {
      pushClusters(channelFor, [
        { id: 'a', name: 'prod', current: true },
        { id: 'b', name: 'staging' },
      ]);
    });

    act(() => {
      screen.getByRole('button', { name: 'pick-staging' }).click();
    });

    await waitFor(() => {
      expect((router.state.location.search as { kubeContext?: string }).kubeContext).toBe('staging');
    });
    expect(screen.getByTestId('active')).toHaveTextContent('staging');
  });

  it('retains the picked context across a chat -> dashboard navigation', async () => {
    const { router } = await renderWithRouter(buildTree(), '/chat');
    await act(async () => {
      pushClusters(channelFor, [
        { id: 'a', name: 'prod', current: true },
        { id: 'b', name: 'staging' },
      ]);
    });

    act(() => {
      screen.getByRole('button', { name: 'pick-staging' }).click();
    });
    await waitFor(() => expect(screen.getByTestId('active')).toHaveTextContent('staging'));

    act(() => {
      screen.getByRole('link', { name: 'to-dashboard' }).click();
    });

    await waitFor(() => expect(screen.getByText('dashboard-page')).toBeInTheDocument());
    expect((router.state.location.search as { kubeContext?: string }).kubeContext).toBe('staging');
    expect(screen.getByTestId('active')).toHaveTextContent('staging');
  });
});

describe('useFixWindowContext', () => {
  const param = (router: { state: { location: { search: unknown } } }) =>
    (router.state.location.search as { kubeContext?: string }).kubeContext;

  it('writes nothing before the clusters snapshot is complete', async () => {
    const { router } = await renderWithRouter(buildTree({ fix: true }), '/chat');
    expect(param(router)).toBeUndefined();
  });

  it('writes the resolved context into the param, replacing the history entry', async () => {
    const { router } = await renderWithRouter(buildTree({ fix: true }), '/chat');
    const entries = router.history.length;
    await act(async () => {
      pushClusters(channelFor, [
        { id: 'a', name: 'prod', current: true },
        { id: 'b', name: 'staging' },
      ]);
    });

    await waitFor(() => expect(param(router)).toBe('prod'));
    expect(router.history.length).toBe(entries);
  });

  it('keeps the window where it is when the default changes', async () => {
    const { router } = await renderWithRouter(buildTree({ fix: true }), '/chat');
    await act(async () => {
      pushClusters(channelFor, [
        { id: 'a', name: 'prod', current: true },
        { id: 'b', name: 'staging' },
      ]);
    });
    await waitFor(() => expect(param(router)).toBe('prod'));

    await act(async () => {
      pushClusterModified(channelFor, { id: 'b', name: 'staging', isDefault: true });
    });

    expect(param(router)).toBe('prod');
    expect(screen.getByTestId('active')).toHaveTextContent('prod');
  });

  it('writes nothing while no cluster resolves, then fixes the one that does', async () => {
    const { router } = await renderWithRouter(buildTree({ fix: true }), '/chat');
    await act(async () => {
      pushClusters(channelFor, [{ id: 'a', name: 'prod' }]);
    });
    expect(param(router)).toBeUndefined();
    expect(screen.getByTestId('active')).toHaveTextContent('');
    expect(screen.getByText('none-selected')).toBeInTheDocument();

    await act(async () => {
      pushClusterModified(channelFor, { id: 'a', name: 'prod', isDefault: true });
    });

    await waitFor(() => expect(param(router)).toBe('prod'));
  });

  it('rewrites a param naming a gone context to the default', async () => {
    const { router } = await renderWithRouter(buildTree({ fix: true }), '/chat?kubeContext=ghost');
    await act(async () => {
      pushClusters(channelFor, [{ id: 'a', name: 'prod', current: true }]);
    });

    await waitFor(() => expect(param(router)).toBe('prod'));
  });
});
