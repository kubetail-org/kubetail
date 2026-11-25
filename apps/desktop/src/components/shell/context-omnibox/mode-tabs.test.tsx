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
import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { renderWithRouter } from '@/test-utils';

import { ModeTabs } from './mode-tabs';

// A minimal tree: the tabs plus two destination pages, so navigation can be
// observed by which page renders.
function buildTree() {
  const root = createRootRoute({
    component: () => (
      <>
        <ModeTabs />
        <Outlet />
      </>
    ),
  });
  const chat = createRoute({
    getParentRoute: () => root,
    path: '/chat',
    component: () => <div>chat-page</div>,
  });
  const dashboard = createRoute({
    getParentRoute: () => root,
    path: '/dashboard',
    component: () => <div>dashboard-page</div>,
  });
  return root.addChildren([chat, dashboard]);
}

describe('ModeTabs', () => {
  it('links to the chat and dashboard routes', async () => {
    await renderWithRouter(buildTree(), '/chat');
    expect(screen.getByRole('link', { name: 'Chat' })).toHaveAttribute('href', '/chat');
    expect(screen.getByRole('link', { name: 'Dashboard' })).toHaveAttribute('href', '/dashboard');
  });

  it('draws each mode as an icon and its label, named by the label', async () => {
    await renderWithRouter(buildTree(), '/chat');
    ['Chat', 'Dashboard'].forEach((name) => {
      const link = screen.getByRole('link', { name });
      expect(link).toHaveAttribute('aria-label', name);
      expect(link).toHaveTextContent(name);
      expect(link.querySelector('svg')).toHaveAttribute('aria-hidden', 'true');
      // As wide as its label with 12px each side, 28px tall, the label 13px medium.
      expect(link).toHaveClass('px-3', 'h-7', 'rounded-full', 'text-[13px]', 'font-medium');
      expect(link).not.toHaveClass('flex-1');
    });
  });

  it('sits in a nav named for the view', async () => {
    await renderWithRouter(buildTree(), '/chat');
    const nav = screen.getByRole('navigation', { name: 'View' });
    expect(nav.contains(screen.getByRole('link', { name: 'Chat' }))).toBe(true);
    expect(nav.contains(screen.getByRole('link', { name: 'Dashboard' }))).toBe(true);
  });

  it('is a 32px muted track', async () => {
    await renderWithRouter(buildTree(), '/chat');
    expect(screen.getByRole('navigation', { name: 'View' })).toHaveClass('h-8', 'rounded-full', 'bg-muted', 'p-0.5');
  });

  // jsdom has no container queries, so the class stands in for the hiding.
  it('hides the labels under 20rem of omnibox, each tab keeping its name', async () => {
    await renderWithRouter(buildTree(), '/chat');
    for (const name of ['Chat', 'Dashboard']) {
      const tab = screen.getByRole('link', { name });
      expect(tab).toHaveAttribute('aria-label', name);
      expect(within(tab).getByText(name)).toHaveClass('@max-xs:hidden');
    }
  });

  // jsdom draws nothing, so every label reads as hidden, as under 20rem of omnibox.
  it('names a mode in a tooltip on hover while its label is hidden', async () => {
    await renderWithRouter(buildTree(), '/chat');
    await userEvent.hover(screen.getByRole('link', { name: 'Dashboard' }));
    await waitFor(() => expect(document.querySelector('[data-slot=tooltip-content]')).toHaveTextContent('Dashboard'));
  });

  it('says nothing on hover while its label is drawn', async () => {
    await renderWithRouter(buildTree(), '/chat');
    const link = screen.getByRole('link', { name: 'Dashboard' });
    const label = within(link).getByText('Dashboard');
    const drawn = vi
      .spyOn(label, 'getClientRects')
      .mockReturnValue([new DOMRect(0, 0, 60, 16)] as unknown as DOMRectList);
    await userEvent.hover(link);
    // The tooltip asks the label once it would open.
    await waitFor(() => expect(drawn).toHaveBeenCalled());
    expect(document.querySelector('[data-slot=tooltip-content]')).toBeNull();
  });

  it('marks only the current route active, white on the track', async () => {
    await renderWithRouter(buildTree(), '/chat');
    expect(screen.getByRole('link', { name: 'Chat' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('link', { name: 'Chat' })).toHaveAttribute('data-status', 'active');
    expect(screen.getByRole('link', { name: 'Chat' })).toHaveClass(
      'data-[status=active]:bg-background',
      'data-[status=active]:text-foreground',
      'data-[status=active]:shadow-pill',
    );
    expect(screen.getByRole('link', { name: 'Dashboard' })).not.toHaveAttribute('data-status', 'active');
    expect(screen.getByRole('link', { name: 'Dashboard' })).not.toHaveAttribute('aria-current');
  });

  it('navigates to the dashboard when its link is clicked', async () => {
    await renderWithRouter(buildTree(), '/chat');
    expect(screen.getByText('chat-page')).toBeInTheDocument();

    act(() => {
      screen.getByRole('link', { name: 'Dashboard' }).click();
    });

    await waitFor(() => expect(screen.getByText('dashboard-page')).toBeInTheDocument());
    expect(screen.getByRole('link', { name: 'Dashboard' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('link', { name: 'Chat' })).not.toHaveAttribute('aria-current');
  });
});
