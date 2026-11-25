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
import { act, screen, waitFor } from '@testing-library/react';
import { Compass } from 'lucide-react';
import type { ReactNode } from 'react';
import { describe, expect, it } from 'vitest';

import { renderWithRouter } from '@/test-utils';

import { NavItem } from './nav-item';

function tree(node: ReactNode) {
  const root = createRootRoute({ component: () => <Outlet /> });
  const dashboard = createRoute({
    getParentRoute: () => root,
    path: '/dashboard',
    validateSearch: (search: Record<string, unknown>) => ({
      ...(typeof search.resource === 'string' ? { resource: search.resource } : {}),
      ...(typeof search.kubeContext === 'string' ? { kubeContext: search.kubeContext } : {}),
    }),
    component: () => node,
  });
  return root.addChildren([dashboard]);
}

describe('NavItem', () => {
  it('links to its resource, keeping the rest of the search', async () => {
    const { router } = await renderWithRouter(
      tree(<NavItem id="pods" label="Pods" level="kind" active={false} />),
      '/dashboard?kubeContext=prod',
    );
    const link = screen.getByRole('link', { name: 'Pods' });
    expect(link).not.toHaveAttribute('aria-current');

    act(() => link.click());
    await waitFor(() => expect(router.state.location.search).toEqual({ kubeContext: 'prod', resource: 'pods' }));
  });

  it('marks the active row as the current page', async () => {
    await renderWithRouter(tree(<NavItem id="pods" label="Pods" level="kind" active />), '/dashboard');
    expect(screen.getByRole('link', { name: 'Pods' })).toHaveAttribute('aria-current', 'page');
  });

  it('draws a known count, and none while it is unknown', async () => {
    await renderWithRouter(
      tree(
        <>
          <NavItem id="pods" label="Pods" level="kind" active={false} count={1234} />
          <NavItem id="jobs" label="Jobs" level="kind" active={false} />
        </>,
      ),
      '/dashboard',
    );
    expect(screen.getByRole('link', { name: /Pods/ })).toHaveTextContent('Pods1,234');
    expect(screen.getByRole('link', { name: 'Jobs' })).toHaveTextContent(/^Jobs$/);
  });

  it('draws a pinned row with its icon', async () => {
    await renderWithRouter(
      tree(<NavItem id="overview" label="Overview" level="top" icon={Compass} active={false} />),
      '/dashboard',
    );
    expect(screen.getByRole('link', { name: 'Overview' }).querySelector('svg')).toBeInTheDocument();
  });

  it('spells out an invisible character in a label the cluster named', async () => {
    // A zero-width space between "Wid" and "get".
    const label = 'Wid\u200Bget';
    await renderWithRouter(
      tree(<NavItem id="example.com/widgets" label={label} level="kind" active={false} />),
      '/dashboard',
    );
    expect(screen.getByRole('link').querySelector('mark')).toBeInTheDocument();
  });
});
