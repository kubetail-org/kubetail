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
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { renderWithRouter } from '@/test-utils';

// The layout route owns the `chat` param, so the real one is what the panel writes
// to; only the shell around it stands in.
vi.mock('@/routes/__root', async () => {
  const { createRootRoute, Outlet } = await import('@tanstack/react-router');
  return { Route: createRootRoute({ component: () => <Outlet /> }) };
});
vi.mock('@/layouts/app-layout', async () => {
  const { Outlet } = await import('@tanstack/react-router');
  return { AppLayout: () => <Outlet /> };
});

// Both panes have suites of their own; here they report what they were handed.
const { paneProps, navFocusFallback } = vi.hoisted(() => ({
  paneProps: { current: null as Record<string, unknown> | null },
  navFocusFallback: { current: undefined } as { current: unknown },
}));
vi.mock('@/components/chat/chat-pane', () => ({
  ChatPane: (props: Record<string, unknown>) => {
    paneProps.current = props;
    return <div data-testid="pane" />;
  },
  NewChatPane: (props: Record<string, unknown>) => {
    paneProps.current = props;
    const { empty } = props;
    return <div data-testid="pane">{empty as React.ReactNode}</div>;
  },
}));
vi.mock('@/components/chat/chat-nav', () => ({
  ChatNav: ({ mode, focusFallback }: { mode: string; focusFallback?: unknown }) => {
    navFocusFallback.current = focusFallback;
    return <div data-testid="chat-nav">{mode}</div>;
  },
}));

const { Route: rootRoute } = (await import('@/routes/__root')) as unknown as {
  Route: import('@tanstack/react-router').AnyRoute;
};
const { Route: appRoute } = await import('@/routes/_app');
const { DashboardChat } = await import('./dashboard-chat');

function render(path = '/dashboard') {
  const dashboard = createRoute({
    getParentRoute: () => appRoute,
    path: '/dashboard',
    component: () => <DashboardChat />,
  });
  return renderWithRouter(rootRoute.addChildren([appRoute.addChildren([dashboard])]), path);
}

const search = (router: { state: { location: { search: Record<string, unknown> } } }) => router.state.location.search;

beforeEach(() => {
  paneProps.current = null;
  vi.clearAllMocks();
});

describe('DashboardChat', () => {
  // The composer is what starts a chat, so it is there beside the list rather than
  // behind a button.
  it('shows the recent list over an unstarted chat while nothing is selected', async () => {
    await render();

    expect(screen.getByTestId('chat-nav')).toHaveTextContent('dashboard');
    expect(paneProps.current).toMatchObject({ mode: 'dashboard' });
    expect(paneProps.current).not.toHaveProperty('chatID');
  });

  // The list's last row going leaves it nothing to hold focus, so focus goes to the composer.
  it('hands the list the composer’s box to fall back on for focus', async () => {
    await render();
    expect(navFocusFallback.current).toBeDefined();
    expect(paneProps.current?.inputRef).toBe(navFocusFallback.current);
  });

  it('opens the chat the param names, and drops the list', async () => {
    await render('/dashboard?chat=c1');

    expect(paneProps.current).toMatchObject({ chatID: 'c1', mode: 'dashboard' });
    expect(screen.queryByTestId('chat-nav')).not.toBeInTheDocument();
  });

  it('returns to the list, keeping the rest of the window’s scope', async () => {
    const { router } = await render('/dashboard?chat=c1&resource=pods');
    await userEvent.click(screen.getByRole('button', { name: 'Chats' }));

    expect(search(router)).toEqual({ resource: 'pods' });
    expect(screen.getByTestId('chat-nav')).toBeInTheDocument();
  });

  it('selects the chat the first send created, leaving Back where it was', async () => {
    const { router } = await render();
    await act(async () => {
      (paneProps.current!.onCreated as (id: string) => void)('c9');
    });

    expect(search(router)).toMatchObject({ chat: 'c9' });
    expect(router.history.length).toBe(1);
  });

  it('returns to the list when the open chat turns out to be gone', async () => {
    const { router } = await render('/dashboard?chat=c1');
    await act(async () => {
      (paneProps.current!.onGone as () => void)();
    });

    expect(search(router).chat).toBeUndefined();
    expect(screen.getByTestId('chat-nav')).toBeInTheDocument();
  });
});
