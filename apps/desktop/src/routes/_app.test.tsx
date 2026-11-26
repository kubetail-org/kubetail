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

import { createRoute } from '@tanstack/react-router';
import { act, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { renderWithRouter } from '@/test-utils';

// Mocks ---------------------------------------------------------------

// The real root mounts the whole provider stack, and the layout the window shell;
// this suite is about the search params the layout route owns.
vi.mock('@/routes/__root', async () => {
  const { createRootRoute, Outlet } = await import('@tanstack/react-router');
  return { Route: createRootRoute({ component: () => <Outlet /> }) };
});
vi.mock('@/layouts/app-layout', async () => {
  const { Outlet } = await import('@tanstack/react-router');
  return { AppLayout: () => <Outlet /> };
});

const { Route: rootRoute } = (await import('@/routes/__root')) as unknown as {
  Route: import('@tanstack/react-router').AnyRoute;
};
const { Route: appRoute } = await import('./_app');

// Helpers -------------------------------------------------------------

function buildTree() {
  const chat = createRoute({ getParentRoute: () => appRoute, path: '/chat', component: () => <div>chat-page</div> });
  const dashboard = createRoute({
    getParentRoute: () => appRoute,
    path: '/dashboard',
    component: () => <div>dashboard-page</div>,
  });
  return rootRoute.addChildren([appRoute.addChildren([chat, dashboard])]);
}

// Tests ---------------------------------------------------------------

describe('the app layout route', () => {
  it('carries the window’s scope across a mode switch', async () => {
    const { router } = await renderWithRouter(buildTree(), '/dashboard?kubeContext=prod&resource=pods');

    await act(async () => {
      await router.navigate({ to: '/chat' });
    });
    expect(screen.getByText('chat-page')).toBeInTheDocument();
    // The window is still pointed at the same cluster and resource — going to chat
    // is a change of view, not of scope.
    expect(router.state.location.search).toEqual({
      kubeContext: 'prod',
      resource: 'pods',
    });

    await act(async () => {
      await router.navigate({ to: '/dashboard' });
    });
    // ...so coming back lands on the resource that was left, not the default.
    expect(router.state.location.search).toMatchObject({ resource: 'pods' });
  });

  // The panel's chat is window state like the rest, so it survives a mode switch. It
  // rides into /chat URLs too, where nothing reads it — a different key from the
  // route's own $chatId, and dropping it would forget the panel on every switch.
  it('carries the dashboard panel’s chat across a mode switch', async () => {
    const { router } = await renderWithRouter(buildTree(), '/dashboard?chat=c1');

    await act(async () => {
      await router.navigate({ to: '/chat' });
    });
    expect(router.state.location.search).toEqual({ chat: 'c1' });

    await act(async () => {
      await router.navigate({ to: '/dashboard' });
    });
    expect(router.state.location.search).toMatchObject({ chat: 'c1' });
  });

  // Absent is the recent list, so a selection of nothing carries nothing — the rule
  // `resource` already follows.
  it('drops an empty chat param rather than carrying a selection of nothing', () => {
    const validate = appRoute.options.validateSearch as (s: Record<string, unknown>) => Record<string, unknown>;

    expect(validate({ chat: 'c1' })).toMatchObject({ chat: 'c1' });
    expect(validate({ chat: '' })).toEqual({});
    expect(validate({ chat: 7 })).toEqual({});
  });

  // The namespace scope is window state like the cluster, so it survives a mode switch.
  it('carries the namespace scope across a mode switch', async () => {
    const { router } = await renderWithRouter(buildTree(), '/dashboard?kubeContext=prod&namespace=checkout');

    await act(async () => {
      await router.navigate({ to: '/chat' });
    });
    expect(router.state.location.search).toEqual({ kubeContext: 'prod', namespace: 'checkout' });

    await act(async () => {
      await router.navigate({ to: '/dashboard' });
    });
    expect(router.state.location.search).toMatchObject({ namespace: 'checkout' });
  });

  // Absent is every namespace, so an empty one carries nothing.
  it('drops an empty namespace rather than carrying a scope of nothing', () => {
    const validate = appRoute.options.validateSearch as (s: Record<string, unknown>) => Record<string, unknown>;

    expect(validate({ namespace: 'kube-system' })).toEqual({ namespace: 'kube-system' });
    expect(validate({ namespace: '' })).toEqual({});
    expect(validate({ namespace: 7 })).toEqual({});
  });

  it('carries nothing it was not given', async () => {
    const { router } = await renderWithRouter(buildTree(), '/dashboard');

    await act(async () => {
      await router.navigate({ to: '/chat' });
    });
    expect(router.state.location.search).toEqual({});
  });
});
