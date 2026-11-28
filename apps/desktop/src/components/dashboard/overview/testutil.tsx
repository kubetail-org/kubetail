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

// Fixtures for the Overview's section tests.
import { createRootRoute, createRoute, Outlet } from '@tanstack/react-router';
import { screen, within } from '@testing-library/react';
import type { ReactNode } from 'react';

import { renderWithRouter } from '@/test-utils';

// Renders `node` on a dashboard route that keeps the search params a card's link writes.
export function renderOnDashboard(node: ReactNode, path = '/dashboard') {
  const root = createRootRoute({ component: () => <Outlet /> });
  const dashboard = createRoute({
    getParentRoute: () => root,
    path: '/dashboard',
    validateSearch: (search: Record<string, unknown>) => ({
      ...(typeof search.resource === 'string' ? { resource: search.resource } : {}),
      ...(typeof search.kubeContext === 'string' ? { kubeContext: search.kubeContext } : {}),
      ...(typeof search.namespace === 'string' ? { namespace: search.namespace } : {}),
    }),
    component: () => node,
  });
  return renderWithRouter(root.addChildren([dashboard]), path);
}

// The rows under the column headers of the card titled `title`.
export function bodyRows(title: string): HTMLElement[] {
  return within(screen.getByRole('table', { name: title }))
    .getAllByRole('row')
    .slice(1);
}
