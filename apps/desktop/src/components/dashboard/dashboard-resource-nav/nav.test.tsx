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

import { createRootRoute, createRoute, Outlet } from '@tanstack/react-router';
import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { buildDashboardNav } from '@/lib/dashboard-resources';
import type { ServerKind } from '@/lib/dashboard-resources';
import { expectCodeText, renderWithRouter } from '@/test-utils';

// The nav reads its tree from `useDashboardNav`, which reaches into urql and the
// providers. Stub it with a tree the real builder makes, and the watch state beside it.
const { navState } = vi.hoisted(() => ({
  navState: { kinds: undefined as unknown[] | undefined, active: true, phase: 'live' },
}));

vi.mock('@/lib/dashboard-nav', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/dashboard-nav')>()),
  useDashboardNav: () => ({
    nav: buildDashboardNav(navState.kinds as ServerKind[] | undefined),
    active: navState.active,
    phase: navState.phase,
  }),
}));

const { DashboardResourceNav } = await import('./nav');

const served = (apiVersion: string, kind: string, resource: string, count: number, scope = 'Namespaced') => ({
  apiVersion,
  kind,
  resource,
  scope,
  isCRD: false,
  count,
  printerColumns: [],
});

const CATALOG = [
  served('v1', 'Event', 'events', 128),
  served('v1', 'Node', 'nodes', 3, 'Cluster'),
  served('v1', 'Pod', 'pods', 42),
  served('apps/v1', 'Deployment', 'deployments', 5),
  served('apps/v1', 'StatefulSet', 'statefulsets', 0),
  served('v1', 'ConfigMap', 'configmaps', 7),
];

// A `/dashboard` route owning the `resource` and `namespace` search params, as `_app` does.
function buildTree() {
  const root = createRootRoute({ component: () => <Outlet /> });
  const dashboard = createRoute({
    getParentRoute: () => root,
    path: '/dashboard',
    validateSearch: (search: Record<string, unknown>) => ({
      ...(typeof search.resource === 'string' && search.resource ? { resource: search.resource } : {}),
      ...(typeof search.namespace === 'string' ? { namespace: search.namespace } : {}),
    }),
    component: DashboardResourceNav,
  });
  return root.addChildren([dashboard]);
}

const render = (path = '/dashboard') => renderWithRouter(buildTree(), path);

beforeEach(() => {
  localStorage.clear();
  navState.kinds = CATALOG;
  navState.active = true;
  navState.phase = 'live';
});

describe('DashboardResourceNav', () => {
  // The namespace picker lives in the breadcrumb bar, so Overview leads.
  it('leads with Overview, holding no namespace picker', async () => {
    await render();
    const nav = screen.getByRole('navigation', { name: 'Resources' });
    expect(nav.firstElementChild).toBe(screen.getByRole('link', { name: 'Overview' }));
    expect(screen.queryByRole('button', { name: /^Namespace:/ })).not.toBeInTheDocument();
  });

  it('draws Overview and Events above the sections, Events with its count', async () => {
    await render();
    expect(screen.getByRole('link', { name: 'Overview' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('link', { name: /Events/ })).toHaveTextContent('Events128');
    expect(screen.getByRole('heading', { name: 'Cluster' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Namespace' })).toBeInTheDocument();
  });

  // The catalog counts in the namespace scope, so the counts stay.
  // The breadcrumb bar's picker names the namespace; the heading does not repeat it.
  it('draws the counts the catalog took in the namespace scope, under a plain heading', async () => {
    await render('/dashboard?namespace=checkout&resource=pods');
    expect(screen.getByRole('heading', { name: 'Namespace' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Events/ })).toHaveTextContent('Events128');
    expect(screen.getByRole('button', { name: /Workloads/ })).toHaveTextContent('Workloads47');
    expect(screen.getByRole('link', { name: /Pods/ })).toHaveTextContent('Pods42');
  });

  it('draws each group with the sum of its kinds, and opens it on a click without navigating', async () => {
    const { router } = await render();
    expect(screen.getByRole('button', { name: /Workloads/ })).toHaveTextContent('Workloads47');
    const config = screen.getByRole('button', { name: /Config/ });
    expect(screen.queryByRole('link', { name: /ConfigMaps/ })).not.toBeInTheDocument();

    act(() => config.click());
    expect(screen.getByRole('link', { name: /ConfigMaps/ })).toHaveAttribute('href', '/dashboard?resource=configmaps');
    expect(router.state.location.search).toEqual({});
  });

  it('marks the kind in the URL as the current page', async () => {
    await render('/dashboard?resource=pods');
    expect(screen.getByRole('link', { name: /Pods/ })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('link', { name: 'Overview' })).not.toHaveAttribute('aria-current');
  });

  it('navigates to a kind, pushing a history entry', async () => {
    const { router } = await render();
    act(() => screen.getByRole('link', { name: /Deployments/ }).click());
    await waitFor(() => expect(router.state.location.search).toEqual({ resource: 'deployments' }));
    expect(router.history.length).toBeGreaterThan(1);
  });

  it('starts with each section’s first group open and the rest closed, and the headings are not toggles', async () => {
    await render();
    expect(screen.getByRole('button', { name: /Infrastructure/ })).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('button', { name: /Workloads/ })).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('button', { name: /Config/ })).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('button', { name: /^Cluster/ })).not.toBeInTheDocument();
  });

  it('keeps what was opened and closed for the next mount', async () => {
    const first = await render();
    act(() => screen.getByRole('button', { name: /Workloads/ }).click());
    act(() => screen.getByRole('button', { name: /Config/ }).click());
    first.unmount();

    await render();
    expect(screen.getByRole('button', { name: /Workloads/ })).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByRole('button', { name: /Config/ })).toHaveAttribute('aria-expanded', 'true');
  });

  it('opens the group of a kind it navigates to', async () => {
    const { router } = await render();
    expect(screen.queryByRole('link', { name: /ConfigMaps/ })).not.toBeInTheDocument();

    await act(() => router.navigate({ to: '/dashboard', search: { resource: 'configmaps' } }));
    expect(screen.getByRole('link', { name: /ConfigMaps/ })).toHaveAttribute('aria-current', 'page');
  });

  it('lets the group of the current kind be closed again, the group row then selected', async () => {
    await render('/dashboard?resource=pods');
    const workloads = screen.getByRole('button', { name: /Workloads/ });
    expect(workloads).toHaveAttribute('data-active', 'false');

    act(() => workloads.click());
    expect(screen.queryByRole('link', { name: /Pods/ })).not.toBeInTheDocument();
    expect(workloads).toHaveAttribute('data-active', 'true');

    act(() => workloads.click());
    expect(workloads).toHaveAttribute('data-active', 'false');
    expect(screen.getByRole('link', { name: /Pods/ })).toHaveAttribute('aria-current', 'page');
  });

  it('hides kinds with no objects, with nothing to show them', async () => {
    await render();
    expect(screen.queryByRole('link', { name: /StatefulSets/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /empty kind/ })).not.toBeInTheDocument();
  });

  it('shows an empty kind while it is the current page', async () => {
    await render('/dashboard?resource=statefulsets');
    expect(screen.getByRole('link', { name: /StatefulSets/ })).toHaveTextContent('StatefulSets0');
  });

  it('draws the curated kinds without counts before the catalog lands', async () => {
    navState.kinds = undefined;
    navState.phase = 'connecting';
    await render();
    expect(screen.getByRole('link', { name: 'StatefulSets' })).toBeInTheDocument();
    expect(screen.getByTestId('nav-status')).toHaveTextContent('Loading resources…');
  });

  it('flags reconnecting under the tree when the live watch drops', async () => {
    navState.phase = 'reconnecting';
    await render();
    expect(screen.getByTestId('nav-status')).toHaveTextContent('Reconnecting…');
  });

  it('stays silent when the watch is paused on an unsynced cluster', async () => {
    navState.kinds = undefined;
    navState.active = false;
    navState.phase = 'connecting';
    await render();
    expect(screen.queryByTestId('nav-status')).not.toBeInTheDocument();
  });
});

describe('DashboardResourceNav search', () => {
  const search = () => screen.getByRole('searchbox', { name: 'Search kinds' });
  const type = (value: string) => fireEvent.change(search(), { target: { value } });

  it('sits under Overview and Events, past a rule, above the sections it filters', async () => {
    await render();
    const follows = (a: Element, b: Element) =>
      Boolean(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);
    const rule = screen.getByRole('separator');
    expect(follows(screen.getByRole('link', { name: /Events/ }), rule)).toBe(true);
    expect(follows(rule, search())).toBe(true);
    expect(follows(search(), screen.getByRole('heading', { name: 'Namespace' }))).toBe(true);
    expect(
      follows(screen.getByRole('heading', { name: 'Namespace' }), screen.getByRole('heading', { name: 'Cluster' })),
    ).toBe(true);
  });

  it('spaces each section’s heading the same under whatever is above it, and the box as far under the rule', async () => {
    await render();
    expect(screen.getByRole('separator')).toHaveClass('mt-2', 'mb-4');
    expect(search().closest('.sticky')?.className).not.toMatch(/\bpb-/);
    for (const name of ['Namespace', 'Cluster']) {
      expect(screen.getByRole('heading', { name })).toHaveClass('mt-2');
      expect(screen.getByRole('heading', { name })).not.toHaveClass('mt-0');
    }
  });

  it('is a code-text box', async () => {
    await render();
    expectCodeText(search());
  });

  it('marks the Cluster section with a globe and the Namespace section with layers', async () => {
    await render();
    expect(screen.getByRole('heading', { name: 'Cluster' }).querySelector('svg')).toHaveClass('lucide-globe');
    expect(screen.getByRole('heading', { name: 'Namespace' }).querySelector('svg')).toHaveClass('lucide-layers');
  });

  it('keeps the kinds whose name starts with the query, empty ones too, their groups open', async () => {
    await render();
    type('st');
    expect(screen.getByRole('link', { name: /StatefulSets/ })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /Pods/ })).not.toBeInTheDocument();
    expect(screen.getByText('1 of 5 kinds')).toBeInTheDocument();
    // Pinned rows stay; a section with nothing to show says so.
    expect(screen.getByRole('link', { name: 'Overview' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Cluster · no matches' })).toBeInTheDocument();
  });

  it('opens a group the user closed while the query matches inside it', async () => {
    await render('/dashboard?resource=pods');
    act(() => screen.getByRole('button', { name: /Workloads/ }).click());
    type('pod');
    expect(screen.getByRole('link', { name: /Pods/ })).toBeInTheDocument();
  });

  it('clears on Clear', async () => {
    await render();
    type('st');
    act(() => screen.getByRole('button', { name: 'Clear' }).click());
    expect(search()).toHaveValue('');
    expect(screen.queryByText(/of 5 kinds/)).not.toBeInTheDocument();
  });

  it('clears on Escape', async () => {
    await render();
    type('st');
    fireEvent.keyDown(search(), { key: 'Escape' });
    expect(search()).toHaveValue('');
  });

  // In the narrow drawer, an Escape that reached `window` would close it too.
  it('keeps the Escape that clears its query from the window, and lets the next one through', async () => {
    const reached = vi.fn();
    window.addEventListener('keydown', reached);
    await render();
    type('st');
    fireEvent.keyDown(search(), { key: 'Escape' });
    expect(search()).toHaveValue('');
    expect(reached).not.toHaveBeenCalled();
    fireEvent.keyDown(search(), { key: 'Escape' });
    window.removeEventListener('keydown', reached);
    expect(reached).toHaveBeenCalledOnce();
  });

  it('leaves the query to an IME’s Escape', async () => {
    await render();
    type('st');
    fireEvent.keyDown(search(), { key: 'Escape', isComposing: true });
    expect(search()).toHaveValue('st');
  });

  it('leaves the query alone on any other key', async () => {
    await render();
    type('st');
    fireEvent.keyDown(search(), { key: 'Enter' });
    expect(search()).toHaveValue('st');
  });
});
