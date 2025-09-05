// Copyright 2024-2025 Andres Morey
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

import { createMemoryHistory } from 'history';
import { Router } from 'react-router-dom';
import { render, waitFor } from '@testing-library/react';
import type { Mock } from 'vitest';

import { useSession } from '@/lib/auth';
import * as dashboardOps from '@/lib/graphql/dashboard/ops';
import { mocks } from '@/mocks/home';
import { renderElement } from '@/test-utils';

import Page from '.';

describe('auth tests', () => {
  it('blocks access if user is unauthenticated', () => {
    const history = createMemoryHistory();

    render(
      <Router location={history.location} navigator={history}>
        <Page />
      </Router>,
    );

    // Assertions
    expect(history.location.pathname).toBe('/auth/login');
  });

  it('reders page if user is authenticated', async () => {
    // Mock auth
    (useSession as Mock).mockReturnValue({
      session: { user: 'test' },
    });

    const { getByText } = renderElement(<Page />, mocks);

    // Check initial message before kubeConfig is resolved
    await waitFor(() => {
      expect(getByText('Connecting...')).toBeInTheDocument();
    });

    // Check message after kubeConfig is resolved
    await waitFor(() => {
      expect(getByText('Dashboard')).toBeInTheDocument();
    });
  });
});

describe('initial connection', () => {
  it('shows loading message while waiting for kubeConfig to return', async () => {
    // Mock auth
    (useSession as Mock).mockReturnValue({
      session: { user: 'test' },
    });

    const { getByText } = renderElement(<Page />, [
      {
        request: {
          query: dashboardOps.KUBE_CONFIG_WATCH,
        },
        delay: Infinity,
      },
    ]);

    // Check looading message
    await waitFor(() => {
      expect(getByText('Connecting...')).toBeInTheDocument();
    });
  });
});
