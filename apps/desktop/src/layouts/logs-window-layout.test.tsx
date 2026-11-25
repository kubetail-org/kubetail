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
import { createRootRoute, createRoute } from '@tanstack/react-router';
import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { renderWithRouter } from '@/test-utils';

import { LogsWindowLayout } from './logs-window-layout';

describe('LogsWindowLayout', () => {
  it("holds the routed page in a column the window's height bounds", async () => {
    const root = createRootRoute();
    const layout = createRoute({ getParentRoute: () => root, id: 'layout', component: LogsWindowLayout });
    const page = createRoute({ getParentRoute: () => layout, path: '/', component: () => <p>the page</p> });
    await renderWithRouter(root.addChildren([layout.addChildren([page])]), '/');

    const column = screen.getByTestId('logs-window-layout');
    expect(column).toHaveClass('h-(--app-min-h)', 'min-h-0', 'flex-col');
    expect(column).toContainElement(screen.getByText('the page'));
  });
});
