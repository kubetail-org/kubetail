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
import { screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { renderWithRouter } from '@/test-utils';

// The real root mounts the whole provider stack, and the layout the window shell;
// this suite is about where `/` sends the window.
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
const { DEFAULT_ROUTE, Route: indexRoute } = await import('.');

// `/` carries no view of its own; it redirects to whichever route `DEFAULT_ROUTE`
// names. This pins that default to chat, so flipping the constant to make
// something else the landing view is a deliberate, test-visible change.
function buildTree() {
  const chat = createRoute({ getParentRoute: () => appRoute, path: '/chat', component: () => <div>chat-page</div> });
  const dashboard = createRoute({
    getParentRoute: () => appRoute,
    path: '/dashboard',
    component: () => <div>dashboard-page</div>,
  });
  return rootRoute.addChildren([appRoute.addChildren([indexRoute, chat, dashboard])]);
}

describe('default route', () => {
  it('sends / to the chat route', async () => {
    const { router } = await renderWithRouter(buildTree(), '/');
    expect(DEFAULT_ROUTE).toBe('/chat');
    expect(router.state.location.pathname).toBe('/chat');
    expect(screen.getByText('chat-page')).toBeInTheDocument();
    expect(screen.queryByText('dashboard-page')).not.toBeInTheDocument();
  });
});
