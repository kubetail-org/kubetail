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

import { act, fireEvent, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { DEFAULT_DASHBOARD_RESOURCE } from '@/lib/dashboard-resources';
import type { ServerKind } from '@/lib/dashboard-resources';
import { renderWithRouter } from '@/test-utils';

// Mocks ---------------------------------------------------------------

// Stand in for the layout route, so the real dashboard route mounts without the
// provider stack `__root` builds. The test root comes back with it, to hang the
// tree from.
vi.mock('@/routes/_app', async () => {
  const { createRootRoute, createRoute, Outlet } = await import('@tanstack/react-router');
  const testRoot = createRootRoute();
  return {
    testRoot,
    // Stands in for the real layout route, which owns the `resource` and `namespace` params.
    Route: createRoute({
      getParentRoute: () => testRoot,
      id: 'app',
      validateSearch: (search: Record<string, unknown>) => ({
        ...(typeof search.resource === 'string' && search.resource.length > 0 ? { resource: search.resource } : {}),
        ...(typeof search.namespace === 'string' ? { namespace: search.namespace } : {}),
      }),
      component: () => <Outlet />,
    }),
  };
});

// The bar is covered by its own suite; this one checks what it is handed.
vi.mock('@/components/dashboard/breadcrumb-bar', () => ({
  DashboardBreadcrumbBar: ({ resource, label, kind }: { resource: string; label: string; kind?: ServerKind }) => (
    <div data-testid="breadcrumb-bar" data-resource={resource} data-kind={kind?.kind}>
      {label}
    </div>
  ),
}));
// Both panels are covered by their own suites; this one is about which is picked and
// what each is handed.
vi.mock('@/components/dashboard/events-table', () => ({
  EventsTable: (props: Record<string, unknown>) => <div data-testid="events-table">{JSON.stringify(props)}</div>,
}));
// The table's rows press their actions through `onAction`; one button per action stands in.
vi.mock('@/components/dashboard/objects-table/table', () => ({
  ObjectsTable: ({ onAction, ...props }: { onAction: (action: string, o: unknown) => void }) => (
    <>
      <div data-testid="objects-table">{JSON.stringify(props)}</div>
      {['yaml', 'events', 'logs'].map((action) => (
        <button
          key={action}
          type="button"
          onClick={() => onAction(action, { uid: 'u1', namespace: 'shop', name: 'web', rawJSON: {} })}
        >
          {action}
        </button>
      ))}
    </>
  ),
}));

const { focusObject } = vi.hoisted(() => ({ focusObject: vi.fn() }));
vi.mock('@/lib/object-view', () => ({ useObjectView: () => ({ set: focusObject }) }));
// Whether the window is on no cluster while contexts are listed.
const { kubeContext } = vi.hoisted(() => ({ kubeContext: { noneSelected: false } }));
vi.mock('@/lib/active-kube-context', () => ({ useActiveKubeContext: () => kubeContext }));

const { useClusterCachedDataObjectsMock, useClusterCachedDataEventsMock } = vi.hoisted(() => ({
  useClusterCachedDataObjectsMock: vi.fn(),
  useClusterCachedDataEventsMock: vi.fn(),
}));
vi.mock('@/lib/cluster-cached-data-objects', () => ({ useClusterCachedDataObjects: useClusterCachedDataObjectsMock }));
vi.mock('@/lib/cluster-cached-data-events', () => ({ useClusterCachedDataEvents: useClusterCachedDataEventsMock }));

// The catalog draws the page again on `catalog.emit()`, as its watch does on each frame.
const { useClusterCachedDataKindsMock, catalog } = vi.hoisted(() => {
  const listeners = new Set<() => void>();
  const state = { version: 0 };
  return {
    useClusterCachedDataKindsMock: vi.fn(),
    catalog: {
      state,
      subscribe: (listener: () => void) => {
        listeners.add(listener);
        return () => listeners.delete(listener);
      },
      emit: () => {
        state.version += 1;
        listeners.forEach((l) => l());
      },
    },
  };
});
vi.mock('@/lib/cluster-cached-data-kinds', async () => {
  const { useSyncExternalStore } = await import('react');
  return {
    useClusterCachedDataKinds: () => {
      useSyncExternalStore(catalog.subscribe, () => catalog.state.version);
      return useClusterCachedDataKindsMock();
    },
  };
});

const { testRoot, Route: appRoute } = (await import('@/routes/_app')) as unknown as {
  testRoot: import('@tanstack/react-router').AnyRoute;
  Route: import('@tanstack/react-router').AnyRoute;
};
const { Route: dashboardRoute } = await import('./dashboard');

// Helpers -------------------------------------------------------------

const KIND: ServerKind = {
  apiVersion: 'apps/v1',
  kind: 'Deployment',
  resource: 'deployments',
  scope: 'Namespaced',
  isCRD: false,
  count: 3,
  printerColumns: [],
};

const pod = (uid: string, namespace: string) => ({ uid, namespace, name: uid });
const event = (uid: string, involvedNamespace: string) => ({ uid, involvedNamespace });

beforeEach(() => {
  kubeContext.noneSelected = false;
  focusObject.mockClear();
  useClusterCachedDataObjectsMock.mockReturnValue({ objects: [], active: true, phase: 'live' });
  useClusterCachedDataEventsMock.mockReturnValue({ events: [], active: true, phase: 'live' });
});

// What the mocked component was last handed.
const propsOf = (testId: string) => JSON.parse(screen.getByTestId(testId).textContent ?? '{}');
const crumb = () => screen.getByTestId('breadcrumb-bar');
// The Events page's note, whatever it holds.
const eventsNote = () => screen.queryByText((_, el) => el?.tagName === 'P' && /event/i.test(el.textContent ?? ''));

function render(path: string, kinds: ServerKind[] = []) {
  useClusterCachedDataKindsMock.mockReturnValue({ kinds, active: true, cacheID: 'cache-1', phase: 'live' });
  return renderWithRouter(testRoot.addChildren([appRoute.addChildren([dashboardRoute])]), path);
}

// The catalog's next state, drawn as its watch would draw it.
function emit(value: { kinds: ServerKind[]; active: boolean; cacheID?: string; phase: string }) {
  useClusterCachedDataKindsMock.mockReturnValue(value);
  act(() => catalog.emit());
}

// Tests ---------------------------------------------------------------

describe('dashboard route', () => {
  it('shows the events timeline for the events view', async () => {
    await render('/dashboard?resource=events');
    expect(screen.getByTestId('events-table')).toBeInTheDocument();
    expect(screen.queryByTestId('objects-table')).not.toBeInTheDocument();
  });

  it.each(['yaml', 'events', 'logs'])('focuses a row’s object on its %s tab', async (action) => {
    await render('/dashboard?resource=deployments', [KIND]);
    fireEvent.click(screen.getByRole('button', { name: action }));
    expect(focusObject).toHaveBeenCalledWith(
      { apiVersion: 'apps/v1', resource: 'deployments', kind: 'Deployment', uid: 'u1', namespace: 'shop', name: 'web' },
      action,
    );
  });

  it('shows the generic table for a discovered kind, told what it is listing', async () => {
    await render('/dashboard?resource=deployments', [KIND]);
    expect(screen.getByTestId('objects-table')).toHaveTextContent('"apiVersion":"apps/v1"');
    expect(screen.getByTestId('objects-table')).toHaveTextContent('"resource":"deployments"');
    // `scope` is the table's namespaced flag.
    expect(screen.getByTestId('objects-table')).toHaveTextContent('"namespaced":true');
  });

  it('waits for the catalog before calling a deep-linked kind unknown', async () => {
    useClusterCachedDataKindsMock.mockReturnValue({ kinds: [], active: true, cacheID: 'cache-1', phase: 'connecting' });
    await renderWithRouter(testRoot.addChildren([appRoute.addChildren([dashboardRoute])]), '/dashboard?resource=pods');
    expect(screen.getByText(/loading resources/i)).toBeInTheDocument();
    expect(screen.queryByText(/coming soon/i)).not.toBeInTheDocument();
  });

  it('shows Overview its placeholder at once, with no catalog to wait for', async () => {
    useClusterCachedDataKindsMock.mockReturnValue({ kinds: [], active: true, cacheID: 'cache-1', phase: 'connecting' });
    await renderWithRouter(
      testRoot.addChildren([appRoute.addChildren([dashboardRoute])]),
      '/dashboard?resource=overview',
    );
    expect(screen.getByText(/coming soon/i)).toBeInTheDocument();
    expect(screen.queryByText(/loading resources/i)).not.toBeInTheDocument();
  });

  it('reads a group id as a kind this cluster does not serve', async () => {
    await render('/dashboard?resource=workloads');
    expect(screen.queryByTestId('objects-table')).not.toBeInTheDocument();
    expect(screen.getByText(/this cluster does not serve workloads/i)).toBeInTheDocument();
  });

  it('names a discovered kind by the kind the catalog gives it', async () => {
    const hpa = {
      ...KIND,
      apiVersion: 'autoscaling/v2',
      kind: 'HorizontalPodAutoscaler',
      resource: 'horizontalpodautoscalers',
    };
    await render('/dashboard?resource=autoscaling/horizontalpodautoscalers', [hpa]);
    expect(crumb()).toHaveTextContent(/^HorizontalPodAutoscaler$/);
  });

  it('says a kind the synced catalog lacks is not served by this cluster', async () => {
    await render('/dashboard?resource=ingresses', [KIND]);
    expect(screen.getByText(/this cluster does not serve ingresses/i)).toBeInTheDocument();
    expect(screen.queryByText(/coming soon/i)).not.toBeInTheDocument();
  });

  // Contexts are listed but none resolves: every page gives way to the one note.
  it.each(['events', 'deployments', 'overview'])('says no cluster is selected in place of %s', async (resource) => {
    kubeContext.noneSelected = true;
    await render(`/dashboard?resource=${resource}`, [KIND]);
    expect(screen.getByText(/no cluster selected/i)).toBeInTheDocument();
    expect(screen.queryByTestId('events-table')).not.toBeInTheDocument();
    expect(screen.queryByTestId('objects-table')).not.toBeInTheDocument();
  });

  it('says a kind is not synced while the cluster has no active cache', async () => {
    useClusterCachedDataKindsMock.mockReturnValue({
      kinds: [],
      active: false,
      cacheID: undefined,
      phase: 'connecting',
    });
    await renderWithRouter(testRoot.addChildren([appRoute.addChildren([dashboardRoute])]), '/dashboard?resource=pods');
    expect(screen.getByText(/no synced cache for this cluster yet/i)).toBeInTheDocument();
    expect(screen.queryByText(/coming soon/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/loading resources/i)).not.toBeInTheDocument();
  });

  it('keeps Overview’s placeholder while the cluster has no active cache', async () => {
    useClusterCachedDataKindsMock.mockReturnValue({
      kinds: [],
      active: false,
      cacheID: undefined,
      phase: 'connecting',
    });
    await renderWithRouter(
      testRoot.addChildren([appRoute.addChildren([dashboardRoute])]),
      '/dashboard?resource=overview',
    );
    expect(screen.getByText(/coming soon/i)).toBeInTheDocument();
  });

  it('keeps the table it shows as the catalog’s counts churn', async () => {
    await render('/dashboard?resource=deployments', [KIND]);
    emit({ kinds: [{ ...KIND, count: 4 }], active: true, cacheID: 'cache-1', phase: 'live' });
    expect(screen.getByTestId('objects-table')).toHaveTextContent('"resource":"deployments"');
  });

  it('keeps the table mounted while a reconnect replays the catalog', async () => {
    await render('/dashboard?resource=deployments', [KIND]);
    const table = screen.getByTestId('objects-table');

    // The new connection's snapshot starts on a clean slate.
    emit({ kinds: [], active: true, cacheID: 'cache-1', phase: 'connecting' });
    expect(screen.getByTestId('objects-table')).toBe(table);
    expect(screen.queryByText(/loading resources/i)).not.toBeInTheDocument();

    emit({ kinds: [KIND], active: true, cacheID: 'cache-1', phase: 'live' });
    expect(screen.getByTestId('objects-table')).toBe(table);
  });

  it('lets the held kind go when the replayed catalog no longer serves it', async () => {
    await render('/dashboard?resource=deployments', [KIND]);
    emit({ kinds: [], active: true, cacheID: 'cache-1', phase: 'connecting' });
    emit({ kinds: [], active: true, cacheID: 'cache-1', phase: 'live' });
    expect(screen.queryByTestId('objects-table')).not.toBeInTheDocument();
    expect(screen.getByText(/this cluster does not serve deployments/i)).toBeInTheDocument();
  });

  // A namespace switch re-subscribes the catalog, which replays from empty.
  it('keeps a discovered kind’s breadcrumb through a namespace switch', async () => {
    const widget: ServerKind = {
      apiVersion: 'example.com/v1',
      kind: 'Widget',
      resource: 'widgets',
      scope: 'Namespaced',
      isCRD: true,
      count: 2,
      printerColumns: [],
    };
    await render('/dashboard?resource=example.com/widgets', [widget]);
    expect(crumb()).toHaveTextContent('Widget');
    expect(crumb()).toHaveAttribute('data-kind', 'Widget');

    emit({ kinds: [], active: true, cacheID: 'cache-1', phase: 'connecting' });
    expect(crumb()).toHaveTextContent('Widget');
    expect(crumb()).toHaveAttribute('data-kind', 'Widget');
    expect(propsOf('objects-table')).toMatchObject({ label: 'Widget' });
  });

  it('holds no kind across a swap to another cache', async () => {
    await render('/dashboard?resource=deployments', [KIND]);
    emit({ kinds: [], active: true, cacheID: 'cache-2', phase: 'connecting' });
    expect(screen.queryByTestId('objects-table')).not.toBeInTheDocument();
    expect(screen.getByText(/loading resources/i)).toBeInTheDocument();
  });

  it('names the view it is showing in the breadcrumb bar', async () => {
    await render('/dashboard?resource=deployments', [KIND]);
    expect(crumb()).toHaveTextContent('Deployments');
    expect(crumb()).toHaveAttribute('data-resource', 'deployments');
    expect(screen.queryByRole('heading', { level: 1 })).not.toBeInTheDocument();
  });

  it('holds the breadcrumb bar above the page, out of the page’s padding', async () => {
    await render('/dashboard?resource=deployments', [KIND]);
    const section = crumb().parentElement!;
    expect(section.firstElementChild).toBe(crumb());
    expect(section).not.toHaveClass('p-6');
    const body = crumb().nextElementSibling!;
    expect(body).toHaveClass('px-4', 'py-3', 'min-h-0', 'flex-1');
    expect(body.contains(screen.getByTestId('objects-table'))).toBe(true);
  });

  it('lands on the default view when the link carries no resource', async () => {
    const explicit = await render(`/dashboard?resource=${DEFAULT_DASHBOARD_RESOURCE}`);
    const expected = crumb().textContent ?? '';
    explicit.unmount();

    // A bare `/dashboard` resolves to the default without the link rewriting itself.
    await render('/dashboard');
    expect(crumb()).toHaveTextContent(expected);
  });

  it('reads an empty resource param as absent, not as a view named nothing', async () => {
    const explicit = await render(`/dashboard?resource=${DEFAULT_DASHBOARD_RESOURCE}`);
    const expected = crumb().textContent ?? '';
    explicit.unmount();

    await render('/dashboard?resource=');
    expect(crumb()).toHaveTextContent(expected);
  });

  it('names a kind’s page by its label alone, and no count', async () => {
    useClusterCachedDataObjectsMock.mockReturnValue({
      objects: [pod('a', 'x'), pod('b', 'y')],
      active: true,
      phase: 'live',
    });
    await render('/dashboard?resource=deployments', [KIND]);
    expect(crumb()).toHaveTextContent(/^Deployments$/);
  });

  it('shows a namespaced kind’s objects in the namespace alone', async () => {
    useClusterCachedDataObjectsMock.mockReturnValue({
      objects: [pod('a', 'checkout'), pod('b', 'default'), pod('c', 'checkout')],
      active: true,
      phase: 'live',
    });
    await render('/dashboard?resource=deployments&namespace=checkout', [KIND]);
    const table = propsOf('objects-table');
    expect(table.objects.map((o: { uid: string }) => o.uid)).toEqual(['a', 'c']);
    expect(table.namespace).toBe('checkout');
  });

  it('shows every object of a cluster-scoped kind under a namespace, and says so', async () => {
    const pv = { ...KIND, apiVersion: 'v1', kind: 'PersistentVolume', resource: 'persistentvolumes', scope: 'Cluster' };
    useClusterCachedDataObjectsMock.mockReturnValue({
      objects: [pod('a', ''), pod('b', '')],
      active: true,
      phase: 'live',
    });
    await render('/dashboard?resource=persistentvolumes&namespace=checkout', [pv]);
    expect(propsOf('objects-table').objects).toHaveLength(2);
    expect(propsOf('objects-table').namespace).toBeUndefined();
    expect(screen.getByText(/Cluster-scoped, so the/)).toHaveTextContent(
      "Cluster-scoped, so the checkout filter isn't applied.",
    );
  });

  it('shows Events scoped to the namespace, and says so', async () => {
    useClusterCachedDataEventsMock.mockReturnValue({
      events: [event('a', 'checkout'), event('b', 'default')],
      active: true,
      phase: 'live',
    });
    await render('/dashboard?resource=events&namespace=checkout');
    expect(crumb()).toHaveTextContent('Events');
    expect(eventsNote()).toHaveTextContent(/^1 event in checkout\.$/);
    const table = propsOf('events-table');
    expect(table.events.map((e: { uid: string }) => e.uid)).toEqual(['a']);
    expect(table.namespace).toBe('checkout');
  });

  it('shows Events with every event and no note without a namespace', async () => {
    useClusterCachedDataEventsMock.mockReturnValue({
      events: [event('a', 'x'), event('b', 'y')],
      active: true,
      phase: 'live',
    });
    await render('/dashboard?resource=events');
    expect(propsOf('events-table').events).toHaveLength(2);
    expect(eventsNote()).toHaveTextContent(/^2 events\.$/);
  });

  it('counts no events until the snapshot lands, naming the namespace meanwhile', async () => {
    useClusterCachedDataEventsMock.mockReturnValue({ events: [], active: true, phase: 'connecting' });
    await render('/dashboard?resource=events&namespace=checkout');
    expect(eventsNote()).toHaveTextContent(/^Events in checkout\.$/);
  });

  it('draws no note before the snapshot without a namespace, nor with no active cache', async () => {
    useClusterCachedDataEventsMock.mockReturnValue({ events: [], active: true, phase: 'connecting' });
    const { unmount } = await render('/dashboard?resource=events');
    expect(eventsNote()).not.toBeInTheDocument();
    // Nor an empty one, holding space above the spinner.
    expect(document.querySelector('p.mb-5')).toBeNull();
    unmount();
    useClusterCachedDataEventsMock.mockReturnValue({ events: [], active: false, phase: 'live' });
    await render('/dashboard?resource=events');
    expect(eventsNote()).not.toBeInTheDocument();
  });

  it('counts zero events once an empty snapshot lands', async () => {
    await render('/dashboard?resource=events');
    expect(eventsNote()).toHaveTextContent(/^0 events\.$/);
  });
});
