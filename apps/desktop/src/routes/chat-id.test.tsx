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

vi.mock('@/routes/_app', async () => {
  const { createRootRoute, createRoute, Outlet } = await import('@tanstack/react-router');
  const { NotFound } = await import('@/routes/__root');
  const testRoot = createRootRoute({ notFoundComponent: NotFound });
  return {
    testRoot,
    Route: createRoute({ getParentRoute: () => testRoot, id: 'app', component: () => <Outlet /> }),
  };
});

// The pane has a suite of its own; here it only reports what it was handed, and its
// callback is what the route exists to answer.
const { paneProps } = vi.hoisted(() => ({ paneProps: { current: null as Record<string, unknown> | null } }));
vi.mock('@/components/chat/chat-pane', () => ({
  ChatPane: (props: Record<string, unknown>) => {
    paneProps.current = props;
    return <div data-testid="pane" />;
  },
}));

const { testRoot, Route: appRoute } = (await import('@/routes/_app')) as unknown as {
  testRoot: import('@tanstack/react-router').AnyRoute;
  Route: import('@tanstack/react-router').AnyRoute;
};
const { Route: chatIdRoute } = await import('./chat-id');

const render = () => renderWithRouter(testRoot.addChildren([appRoute.addChildren([chatIdRoute])]), '/chat/c1');

beforeEach(() => {
  paneProps.current = null;
  vi.clearAllMocks();
});

describe('chat route with an id', () => {
  it('opens the chat in the path, in chat mode', async () => {
    await render();
    expect(screen.getByTestId('pane')).toBeInTheDocument();
    expect(paneProps.current).toMatchObject({ chatID: 'c1', mode: 'chat' });
  });

  it('leaves a chat that is gone for the empty one', async () => {
    const { router } = await render();
    await act(async () => {
      (paneProps.current!.onGone as () => void)();
    });

    expect(router.state.location.pathname).toBe('/chat');
  });
});
