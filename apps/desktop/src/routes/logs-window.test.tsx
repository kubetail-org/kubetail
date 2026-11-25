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
import { screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { renderWithRouter } from '@/test-utils';

vi.mock('@/routes/_logs', async () => {
  const { createRootRoute, createRoute, Outlet } = await import('@tanstack/react-router');
  const testRoot = createRootRoute();
  return {
    testRoot,
    Route: createRoute({ getParentRoute: () => testRoot, id: 'logs-window', component: () => <Outlet /> }),
  };
});
vi.mock('@/components/logs/log-window/window', () => ({
  LogsWindow: () => <div data-testid="logs-window" />,
}));

const { testRoot, Route: layoutRoute } = (await import('@/routes/_logs')) as unknown as {
  testRoot: import('@tanstack/react-router').AnyRoute;
  Route: import('@tanstack/react-router').AnyRoute;
};
const { Route: logsWindowRoute } = await import('./logs-window');

describe('the log window route', () => {
  it('draws the log window at /logs', async () => {
    await renderWithRouter(testRoot.addChildren([layoutRoute.addChildren([logsWindowRoute])]), '/logs');
    expect(screen.getByTestId('logs-window')).toBeInTheDocument();
  });
});
