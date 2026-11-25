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

import { act, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { renderWithRouter } from '@/test-utils';

// Stands in for the layout route, so the chat route mounts without the provider
// stack `__root` builds.
vi.mock('@/routes/_app', async () => {
  const { createRootRoute, createRoute, Outlet } = await import('@tanstack/react-router');
  const { NotFound } = await import('@/routes/__root');
  const testRoot = createRootRoute({ notFoundComponent: NotFound });
  return {
    testRoot,
    Route: createRoute({ getParentRoute: () => testRoot, id: 'app', component: () => <Outlet /> }),
  };
});

// The pane has its own suite; here it only has to say what it was given.
const { paneProps } = vi.hoisted(() => ({ paneProps: { current: null as Record<string, unknown> | null } }));
vi.mock('@/components/chat/chat-pane', () => ({
  NewChatPane: (props: Record<string, unknown>) => {
    paneProps.current = props;
    return <div data-testid="pane" />;
  },
}));

const { testRoot, Route: appRoute } = (await import('@/routes/_app')) as unknown as {
  testRoot: import('@tanstack/react-router').AnyRoute;
  Route: import('@tanstack/react-router').AnyRoute;
};
const { Route: chatRoute } = await import('./chat');

const render = () => renderWithRouter(testRoot.addChildren([appRoute.addChildren([chatRoute])]), '/chat');

beforeEach(() => {
  paneProps.current = null;
});

describe('chat route', () => {
  it('opens a chat that has not started, so a stray visit writes nothing', async () => {
    await render();
    expect(screen.getByTestId('pane')).toBeInTheDocument();
    expect(paneProps.current).toMatchObject({ mode: 'chat' });
  });

  it('moves to the chat the first send created, leaving Back where it was', async () => {
    const { router } = await render();
    await act(async () => {
      (paneProps.current!.onCreated as (id: string) => void)('c9');
    });

    expect(router.state.location.pathname).toBe('/chat/c9');
  });
});
