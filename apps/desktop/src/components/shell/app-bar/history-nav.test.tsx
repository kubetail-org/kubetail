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

import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Link,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { act, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { renderWithRouter } from '@/test-utils';

import { FORWARD_CEILING_STORAGE_KEY, HistoryNav } from './history-nav';

// A minimal tree: the nav plus two pages linked to each other, so history can be
// grown by clicking links and observed through which page renders and how the
// buttons enable/disable. The paths reuse registered app routes (`/chat`,
// `/dashboard`) so the typed `Link` accepts them.
function buildTree() {
  const root = createRootRoute({
    component: () => (
      <>
        <HistoryNav />
        <Outlet />
      </>
    ),
  });
  const home = createRoute({
    getParentRoute: () => root,
    path: '/chat',
    component: () => (
      <div>
        <span>home-page</span>
        <Link to="/dashboard">to-other</Link>
      </div>
    ),
  });
  const other = createRoute({
    getParentRoute: () => root,
    path: '/dashboard',
    component: () => (
      <div>
        <span>other-page</span>
        <Link to="/chat">to-home</Link>
      </div>
    ),
  });
  return root.addChildren([home, other]);
}

const back = () => screen.getByRole('button', { name: 'Go back' });
const forward = () => screen.getByRole('button', { name: 'Go forward' });

// The nav persists its forward ceiling in sessionStorage; clear it between tests
// so one test's navigation doesn't seed the next.
beforeEach(() => sessionStorage.clear());

describe('HistoryNav', () => {
  it('disables both buttons at the start of history', async () => {
    await renderWithRouter(buildTree(), '/chat');
    expect(back()).toBeDisabled();
    expect(forward()).toBeDisabled();
  });

  it('enables back after navigating, and going back returns to the prior page', async () => {
    await renderWithRouter(buildTree(), '/chat');

    act(() => screen.getByText('to-other').click());
    await waitFor(() => expect(screen.getByText('other-page')).toBeInTheDocument());
    expect(back()).toBeEnabled();
    expect(forward()).toBeDisabled();

    act(() => back().click());
    await waitFor(() => expect(screen.getByText('home-page')).toBeInTheDocument());
  });

  it('enables forward after going back, and going forward re-applies the entry', async () => {
    await renderWithRouter(buildTree(), '/chat');

    act(() => screen.getByText('to-other').click());
    await waitFor(() => expect(screen.getByText('other-page')).toBeInTheDocument());

    act(() => back().click());
    await waitFor(() => expect(screen.getByText('home-page')).toBeInTheDocument());
    expect(forward()).toBeEnabled();

    act(() => forward().click());
    await waitFor(() => expect(screen.getByText('other-page')).toBeInTheDocument());
    expect(forward()).toBeDisabled();
  });

  it('drops the forward entry once a new navigation replaces it', async () => {
    await renderWithRouter(buildTree(), '/chat');

    act(() => screen.getByText('to-other').click());
    await waitFor(() => expect(screen.getByText('other-page')).toBeInTheDocument());
    act(() => back().click());
    await waitFor(() => expect(screen.getByText('home-page')).toBeInTheDocument());
    expect(forward()).toBeEnabled();

    // A fresh navigation from a rewound position truncates the forward stack.
    act(() => screen.getByText('to-other').click());
    await waitFor(() => expect(screen.getByText('other-page')).toBeInTheDocument());
    expect(forward()).toBeDisabled();
  });
});

// The memory history above keeps `__TSR_index` and the entry count in lockstep,
// so it can't exercise the divergence that only real browser history shows: when
// the frontend is served as a standalone site reached from another page, the
// browser's history holds entries predating the app. Forward availability must
// come from the router-managed stack, not that total length.
describe('HistoryNav (standalone-site browser history)', () => {
  it('keeps Forward disabled at the app entry when the browser history predates the app', async () => {
    // Arrive at the app from another page in the same tab: a foreign entry now
    // sits beneath the app's first entry, so window.history.length > 1 while
    // TanStack boots the app at __TSR_index 0.
    window.history.pushState({}, '', '/chat');
    expect(window.history.length).toBeGreaterThan(1);

    const router = createRouter({ routeTree: buildTree(), history: createBrowserHistory() });
    await router.load();
    await act(async () => {
      render(<RouterProvider router={router} />);
    });

    // Inferring Forward from the total browser length would wrongly enable it
    // (0 < length - 1); the router-managed ceiling correctly disables it. Back is
    // likewise confined to app entries, so it doesn't offer to leave for the
    // foreign page.
    expect(screen.getByRole('button', { name: 'Go forward' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Go back' })).toBeDisabled();
  });

  it('restores the forward ceiling from sessionStorage across a reload of the same stack', async () => {
    // Before the reload the app had navigated forward to index 2; the reload
    // lands back at an earlier entry (index 0) with the browser's forward entries
    // intact — so Forward must stay available. A reload preserves the entry's
    // __TSR_key, so a ceiling recorded under that key is trusted. Store it scoped
    // to the actual current entry key (as a prior navigation would have).
    window.history.replaceState({}, '', '/chat');
    const router = createRouter({ routeTree: buildTree(), history: createBrowserHistory() });
    await router.load();
    sessionStorage.setItem(
      FORWARD_CEILING_STORAGE_KEY,
      JSON.stringify({ ceiling: 2, key: router.history.location.state.__TSR_key }),
    );
    await act(async () => {
      render(<RouterProvider router={router} />);
    });

    expect(screen.getByRole('button', { name: 'Go forward' })).toBeEnabled();
  });

  it('discards a stale ceiling from a previous visit in the same tab (new history stack)', async () => {
    // Leaving the app and following a link back lands on a fresh entry with a new
    // __TSR_key while sessionStorage still holds the prior visit's higher ceiling.
    // Scoping the ceiling to the entry key discards it, so Forward isn't a no-op.
    window.history.replaceState({}, '', '/chat'); // fresh entry: a new __TSR_key is stamped
    sessionStorage.setItem(FORWARD_CEILING_STORAGE_KEY, JSON.stringify({ ceiling: 2, key: 'previous-visit' }));

    const router = createRouter({ routeTree: buildTree(), history: createBrowserHistory() });
    await router.load();
    await act(async () => {
      render(<RouterProvider router={router} />);
    });

    expect(screen.getByRole('button', { name: 'Go forward' })).toBeDisabled();
  });

  it('reads a stored ceiling that is not JSON as none', async () => {
    window.history.replaceState({}, '', '/chat');
    sessionStorage.setItem(FORWARD_CEILING_STORAGE_KEY, '{not json');

    const router = createRouter({ routeTree: buildTree(), history: createBrowserHistory() });
    await router.load();
    await act(async () => {
      render(<RouterProvider router={router} />);
    });

    expect(screen.getByRole('button', { name: 'Go forward' })).toBeDisabled();
  });
});

describe('HistoryNav look', () => {
  it('draws two round ghost buttons in a muted capsule', async () => {
    await renderWithRouter(buildTree(), '/chat');
    for (const button of [back(), forward()]) {
      expect(button).toHaveClass('size-7', 'rounded-full', 'border-transparent');
    }
    expect(back().parentElement).toHaveClass('rounded-full', 'bg-muted');
  });
});
