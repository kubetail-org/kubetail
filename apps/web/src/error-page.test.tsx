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

import { render, screen } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router';

import ErrorPage from './error-page';

function renderRouteError(loader: () => never) {
  const router = createMemoryRouter([{ path: '/', loader, element: <div />, errorElement: <ErrorPage /> }]);
  render(<RouterProvider router={router} />);
}

describe('ErrorPage', () => {
  it('shows the status text before the message', async () => {
    renderRouteError(() => {
      throw Object.assign(new Error('boom'), { statusText: 'Not Found' });
    });

    expect(await screen.findByText('Not Found')).toBeInTheDocument();
  });

  it("shows a thrown error's message", async () => {
    renderRouteError(() => {
      throw new Error('boom');
    });

    expect(await screen.findByText('boom')).toBeInTheDocument();
  });
});
