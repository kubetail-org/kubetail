// Copyright 2024 Andres Morey
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

import { MockedProvider } from '@apollo/client/testing';
import type { MockedResponse } from '@apollo/client/testing';
import { render, waitFor } from '@testing-library/react';
import { Suspense } from 'react';
import { ErrorBoundary } from 'react-error-boundary';
import { MemoryRouter, Routes } from 'react-router-dom';

import * as ops from '@/lib/graphql/ops';
import { routes } from './routes';

vi.mock('@/pages/home', () => ({
  default: () => <div>Home</div>,
}));

vi.mock('@/pages/console', () => ({
  default: () => <div>Console</div>,
}));

vi.mock('@/pages/auth/login', () => ({
  default: () => <div>Auth-Login</div>,
}));

vi.mock('@/pages/auth/logout', () => ({
  default: () => <div>Auth-Logout</div>,
}));

const mocks: MockedResponse[] = [
  {
    request: {
      query: ops.READY_WAIT,
    },
    result: {
      data: {
        readyWait: true,
      },
    },
  },
];

const renderPage = (path: string) => (
  render(
    <MockedProvider
      mocks={mocks}
      addTypename={false}
    >
      <ErrorBoundary fallback={<div>error</div>}>
        <Suspense fallback={<div>loading...</div>}>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              {routes}
            </Routes>
          </MemoryRouter>
        </Suspense>
      </ErrorBoundary>
    </MockedProvider>,
  )
);

describe('route tests', () => {
  it('/', async () => {
    const { getByText } = renderPage('/');
    await waitFor(() => {
      expect(getByText('Home')).toBeInTheDocument();
    });
  });

  it('/console', async () => {
    const { getByText } = renderPage('/console');
    await waitFor(() => {
      expect(getByText('Console')).toBeInTheDocument();
    });
  });

  it('/auth/login', async () => {
    const { getByText } = renderPage('/auth/login');
    await waitFor(() => {
      expect(getByText('Auth-Login')).toBeInTheDocument();
    });
  });

  it('/auth/logout', async () => {
    const { getByText } = renderPage('/auth/logout');
    await waitFor(() => {
      expect(getByText('Auth-Logout')).toBeInTheDocument();
    });
  });
});
