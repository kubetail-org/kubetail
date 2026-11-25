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

vi.mock('@/routes/__root', async () => {
  const { createRootRoute, Outlet } = await import('@tanstack/react-router');
  return { Route: createRootRoute({ component: () => <Outlet /> }) };
});

const { Route: rootRoute } = (await import('@/routes/__root')) as unknown as {
  Route: import('@tanstack/react-router').AnyRoute;
};
const { Route: logsRoute } = await import('./_logs');

describe('the log window layout route', () => {
  it('draws its pages in the log window layout', async () => {
    const page = createRoute({ getParentRoute: () => logsRoute, path: '/x', component: () => <p>the page</p> });
    await renderWithRouter(rootRoute.addChildren([logsRoute.addChildren([page])]), '/x');
    expect(screen.getByTestId('logs-window-layout')).toContainElement(screen.getByText('the page'));
  });
});
