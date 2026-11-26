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

import { createRootRoute, createRoute } from '@tanstack/react-router';
import { act, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { mockTauriWindow, renderWithRouter, stubResizeObserver } from '@/test-utils';

// The app bar measures its two ends.
stubResizeObserver();

// Mocks ---------------------------------------------------------------

// `WindowControls` (in the app bar off macOS) drives the native window via
// `getCurrentWindow`.
const { factory } = mockTauriWindow();
vi.mock('@tauri-apps/api/window', () => factory());

// Replace each chrome widget with an identifiable stub so the test asserts
// *placement* (title bar vs sidebar vs inset) without dragging in their
// providers.
vi.mock('@/components/shell/app-bar/menu', () => ({
  AppMenu: () => <div data-testid="app-menu" />,
}));
// The omnibox in the app bar needs the router search + kubeconfig providers; this
// test is about placement, so stub it.
vi.mock('@/components/shell/context-omnibox/omnibox', () => ({
  ContextOmnibox: () => <div data-testid="context-omnibox" />,
}));
// The dialogs host mounts the real overlay panels (which need the GraphQL/clusters
// provider stack); stub it — this test is about chrome placement, not dialogs.
vi.mock('@/components/shell/app-dialogs', () => ({
  AppDialogs: () => <div data-testid="app-dialogs" />,
}));
vi.mock('@/components/shell/app-bar/settings-button', () => ({
  SettingsButton: () => <div data-testid="settings-button" />,
}));
vi.mock('@/components/shell/connection-status', () => ({
  ConnectionStatus: () => <div data-testid="connection-status" />,
}));
// The log windows' provider talks to the host and reads the window's cluster; its own
// suite covers it. Here it serves an inert value, and says it was mounted.
const { logWindowsMounted } = vi.hoisted(() => ({ logWindowsMounted: { current: false } }));
vi.mock('@/lib/log-windows', async (importOriginal) => {
  const { LogWindowsContext, useLogWindows } = await importOriginal<typeof import('@/lib/log-windows')>();
  const value = { ready: true, windowOf: () => undefined, open: () => {}, focus: () => {}, shownWindowOf: () => null };
  return {
    useLogWindows,
    LogWindowsProvider: ({ children }: { children: import('react').ReactNode }) => {
      logWindowsMounted.current = true;
      return <LogWindowsContext.Provider value={value}>{children}</LogWindowsContext.Provider>;
    },
  };
});
// The real resource nav builds its tree from the active cluster's kinds (urql +
// clusters providers); this test only checks that it mounts on dashboard, so stub
// it with a nav that keeps the accessible "Resources" name the assertions match.
vi.mock('@/components/chat/chat-nav', () => ({
  ChatNav: () => <nav aria-label="Chats" />,
}));
// The layout mounts the chat outbox, whose send wants a client. Nothing here sends.
// The focused object is bound to the active cluster.
vi.mock('@/lib/active-cluster', () => ({ useActiveCluster: () => ({ clusterID: 'c1' }) }));
const { fixWindowContext } = vi.hoisted(() => ({ fixWindowContext: vi.fn() }));
vi.mock('@/lib/active-kube-context', () => ({ useFixWindowContext: fixWindowContext }));
vi.mock('urql', () => ({ useMutation: () => [{}, vi.fn()] }));
vi.mock('@/gql', () => ({ graphql: () => ({}) }));
// The kind catalog's provider subscribes; record what it was told instead, and mark
// its place in the tree.
const { catalogEnabled } = vi.hoisted(() => ({ catalogEnabled: vi.fn() }));
vi.mock('@/lib/cluster-cached-data-kinds', () => ({
  ClusterCachedDataKindsProvider: ({ enabled, children }: { enabled: boolean; children: React.ReactNode }) => {
    catalogEnabled(enabled);
    return <div data-testid="kind-catalog">{children}</div>;
  },
}));
// Likewise the namespace list's provider.
const { namespacesEnabled } = vi.hoisted(() => ({ namespacesEnabled: vi.fn() }));
vi.mock('@/lib/cluster-namespaces', () => ({
  ClusterNamespacesProvider: ({ enabled, children }: { enabled: boolean; children: React.ReactNode }) => {
    namespacesEnabled(enabled);
    return <div data-testid="namespace-list">{children}</div>;
  },
}));
vi.mock('@/components/dashboard/dashboard-resource-nav', () => ({
  DashboardResourceNav: () => <nav aria-label="Resources" />,
}));

const { AppLayout } = await import('./app-layout');

// Helpers -------------------------------------------------------------

// AppLayout renders an <Outlet/>, so it can only be exercised inside a router.
// Mount it as the root layout with one child page that drops a marker in the
// inset.
function buildTree() {
  const root = createRootRoute({ component: AppLayout });
  const index = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: () => <div data-testid="page-content" />,
  });
  // Chat and dashboard peers, so the layout's mode detection has real routes to
  // match against (the resource nav mounts only on dashboard).
  const chat = createRoute({
    getParentRoute: () => root,
    path: '/chat',
    component: () => <div data-testid="page-content" />,
  });
  const dashboard = createRoute({
    getParentRoute: () => root,
    path: '/dashboard',
    component: () => <div data-testid="page-content" />,
  });
  return root.addChildren([index, chat, dashboard]);
}

function sidebarOf(container: HTMLElement) {
  const el = container.querySelector('[data-testid="left-sidebar-panel"]');
  if (!el) throw new Error('sidebar not found');
  return el;
}

function pageOf(container: HTMLElement) {
  const el = container.querySelector('main');
  if (!el) throw new Error('page frame not found');
  return el;
}

// The row the sidebar and the page share, below the chrome.
function contentRowOf(container: HTMLElement) {
  const el = pageOf(container).parentElement;
  if (!el) throw new Error('content row not found');
  return el;
}

// The window's bands, top to bottom: chrome first, then the content row.
function bandsOf(container: HTMLElement) {
  const column = contentRowOf(container).parentElement;
  if (!column) throw new Error('layout column not found');
  return Array.from(column.children);
}

// Tests ---------------------------------------------------------------

// Both sidebars remember open/collapsed app-wide, so each case starts from a
// pristine store rather than from what the last one left.
beforeEach(() => {
  localStorage.clear();
});

describe('AppLayout', () => {
  it('puts the omnibox and both sidebar toggles in the app bar', async () => {
    const { container } = await renderWithRouter(buildTree(), '/');
    const bar = screen.getByTestId('app-bar');
    for (const control of [
      screen.getByTestId('context-omnibox'),
      screen.getByRole('button', { name: 'Toggle sidebar' }),
      screen.getByRole('button', { name: /toggle right sidebar/i }),
    ]) {
      expect(bar.contains(control)).toBe(true);
    }
    expect(sidebarOf(container).contains(screen.getByTestId('context-omnibox'))).toBe(false);
  });

  it('places the app menu in the app bar, not the sidebar', async () => {
    const { container } = await renderWithRouter(buildTree(), '/');
    const menu = screen.getByTestId('app-menu');
    expect(screen.getByTestId('app-bar').contains(menu)).toBe(true);
    expect(sidebarOf(container).contains(menu)).toBe(false);
  });

  // The window is a column of chrome over a row: app bar across the top, then the
  // sidebar beside the page. Nothing is fixed-positioned, so this nesting is what
  // keeps the page out from under the chrome.
  it('stacks the app bar above the row holding the sidebar and the page', async () => {
    const { container } = await renderWithRouter(buildTree(), '/');
    const bands = bandsOf(container);
    const row = contentRowOf(container);

    expect(row.contains(sidebarOf(container))).toBe(true);
    expect(row.contains(screen.getByTestId('app-bar'))).toBe(false);
    expect(bands.indexOf(screen.getByTestId('app-bar'))).toBeLessThan(bands.indexOf(row));
  });

  it('renders the routed page in the page area, not the sidebar', async () => {
    const { container } = await renderWithRouter(buildTree(), '/');
    const page = screen.getByTestId('page-content');
    expect(pageOf(container).contains(page)).toBe(true);
    expect(sidebarOf(container).contains(page)).toBe(false);
  });

  it('mounts the resource nav in the sidebar while in dashboard mode', async () => {
    const { container } = await renderWithRouter(buildTree(), '/dashboard');
    const nav = screen.getByRole('navigation', { name: 'Resources' });
    expect(sidebarOf(container).contains(nav)).toBe(true);
  });

  it('holds the kind catalog above the resource nav and the page, which both read it', async () => {
    await renderWithRouter(buildTree(), '/dashboard');
    const catalog = screen.getByTestId('kind-catalog');
    expect(catalog.contains(screen.getByRole('navigation', { name: 'Resources' }))).toBe(true);
    expect(catalog.contains(screen.getByTestId('page-content'))).toBe(true);
  });

  it('runs the kind catalog on the dashboard alone', async () => {
    await renderWithRouter(buildTree(), '/dashboard');
    expect(catalogEnabled).toHaveBeenLastCalledWith(true);
  });

  it('pauses the kind catalog in chat mode', async () => {
    await renderWithRouter(buildTree(), '/chat');
    expect(catalogEnabled).toHaveBeenLastCalledWith(false);
  });

  it('holds the namespace list above the page, and runs it on the dashboard alone', async () => {
    await renderWithRouter(buildTree(), '/dashboard');
    expect(screen.getByTestId('namespace-list').contains(screen.getByTestId('page-content'))).toBe(true);
    expect(namespacesEnabled).toHaveBeenLastCalledWith(true);
  });

  it('pauses the namespace list in chat mode', async () => {
    await renderWithRouter(buildTree(), '/chat');
    expect(namespacesEnabled).toHaveBeenLastCalledWith(false);
  });

  it('omits the resource nav outside dashboard mode', async () => {
    await renderWithRouter(buildTree(), '/chat');
    expect(screen.queryByRole('navigation', { name: 'Resources' })).not.toBeInTheDocument();
  });

  it('mounts the chat list in the sidebar while in chat mode', async () => {
    const { container } = await renderWithRouter(buildTree(), '/chat');
    expect(sidebarOf(container).contains(screen.getByRole('navigation', { name: 'Chats' }))).toBe(true);
  });

  it('omits the chat list in dashboard mode', async () => {
    await renderWithRouter(buildTree(), '/dashboard');
    expect(screen.queryByRole('navigation', { name: 'Chats' })).not.toBeInTheDocument();
  });

  it('mounts the resource nav when navigating into dashboard on the persistent layout', async () => {
    // The layout stays mounted across chat<->dashboard (only the Outlet swaps),
    // so this exercises a *client navigation* — not a fresh load — which is where
    // a non-reactive mode check would leave the nav stale until a reload.
    const { router } = await renderWithRouter(buildTree(), '/chat');
    expect(screen.queryByRole('navigation', { name: 'Resources' })).not.toBeInTheDocument();

    await act(async () => {
      await router.navigate({ to: '/dashboard' });
    });

    await waitFor(() => expect(screen.getByRole('navigation', { name: 'Resources' })).toBeInTheDocument());

    // ...and unmounts again on the way back to chat.
    await act(async () => {
      await router.navigate({ to: '/chat' });
    });
    await waitFor(() => expect(screen.queryByRole('navigation', { name: 'Resources' })).not.toBeInTheDocument());
  });

  it('slides the sidebar closed from the app bar’s toggle, and open again', async () => {
    const { container } = await renderWithRouter(buildTree(), '/chat');

    await act(async () => {
      screen.getByRole('button', { name: 'Toggle sidebar' }).click();
    });
    expect(sidebarOf(container)).toHaveAttribute('inert');
    expect(contentRowOf(container).contains(sidebarOf(container))).toBe(true);

    await act(async () => {
      screen.getByRole('button', { name: 'Toggle sidebar' }).click();
    });
    expect(sidebarOf(container)).not.toHaveAttribute('inert');
  });

  // Windows share the closed state, so a user who works with the sidebar out of
  // the way does not have to close it again in every window they open.
  it('remembers the sidebar closed for the next window', async () => {
    const first = await renderWithRouter(buildTree(), '/chat');
    await act(async () => {
      screen.getByRole('button', { name: 'Toggle sidebar' }).click();
    });
    first.unmount();

    const { container } = await renderWithRouter(buildTree(), '/chat');
    expect(sidebarOf(container)).toHaveAttribute('inert');
  });

  // A virtualized table scrolls inside a definite height; `min-h-*` alone would grow
  // with the rows and scroll the page instead. The column is the definite one, and
  // every flex item down to the page shrinks into it.
  it('bounds the page frame so a route can own its scroll', async () => {
    const { container } = await renderWithRouter(buildTree(), '/');
    const column = contentRowOf(container).parentElement!;
    expect(column).toHaveClass('h-(--app-min-h)', 'min-h-0', 'flex-col');
    expect(contentRowOf(container)).toHaveClass('min-h-0', 'flex-1', 'overflow-clip');
    expect(pageOf(container)).toHaveClass('min-h-0', 'flex-1', 'overflow-hidden');
  });

  // The bar's colour shows in the 8px around the box, so bar and gutter read as one surface,
  // and the box's corners clip the sidebars and the page.
  it('insets the content row in a rounded, bordered box on the bar’s colour', async () => {
    const { container } = await renderWithRouter(buildTree(), '/');
    expect(contentRowOf(container).parentElement).toHaveClass('bg-app-bar');
    expect(contentRowOf(container)).toHaveClass(
      'mx-2',
      'mb-2',
      'relative',
      '@container',
      'rounded-xl',
      'border',
      'border-border',
      'overflow-clip',
      'shadow-[0_1px_1px_rgb(0_0_0/0.1),0_1px_1.5px_rgb(0_0_0/0.1)]',
    );
  });

  it('puts the right sidebar after the page, flush to the window', async () => {
    const { container } = await renderWithRouter(buildTree(), '/');
    // Closed until asked for, and then after the page — the page keeps the middle,
    // the panel the right wall.
    expect(screen.queryByTestId('right-sidebar')).not.toBeInTheDocument();

    await act(async () => {
      screen.getByRole('button', { name: /toggle right sidebar/i }).click();
    });
    const row = contentRowOf(container);
    const panel = screen.getByTestId('right-sidebar');
    expect(Array.from(row.children).indexOf(panel)).toBe(row.children.length - 1);
    expect(pageOf(container).contains(panel)).toBe(false);
  });

  it("fixes the window's cluster in its param", async () => {
    await renderWithRouter(buildTree(), '/chat');
    expect(fixWindowContext).toHaveBeenCalled();
  });

  it('renders the connection-status banner', async () => {
    await renderWithRouter(buildTree(), '/');
    expect(screen.getByTestId('connection-status')).toBeInTheDocument();
  });

  it('mounts the log windows, which the panel and the cards reach', async () => {
    logWindowsMounted.current = false;
    await renderWithRouter(buildTree(), '/');
    expect(logWindowsMounted.current).toBe(true);
  });
});
