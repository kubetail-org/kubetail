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

import { createMemoryHistory } from 'history';
import { Router } from 'react-router-dom';
import { render, waitFor } from '@testing-library/react';
import type { Mock } from 'vitest';

import { useSession } from '@/lib/auth';
import { mocks } from '@/mocks/home';
import Home from '@/pages/home';
import { renderElement } from '@/test-utils';

describe('home page', () => {
  it('blocks access if user is unauthenticated', () => {
    const history = createMemoryHistory();

    render(
      <Router location={history.location} navigator={history}>
        <Home />
      </Router>,
    );

    // assertions
    expect(history.location.pathname).toBe('/auth/login');
  });

  it('renders loading modal while waiting for workloads', async () => {
    // mock auth
    (useSession as Mock).mockReturnValue({
      session: { user: 'test' },
    });

    const { getByText, queryByText } = renderElement(<Home />, mocks);

    // before
    expect(getByText('Loading Workloads')).toBeInTheDocument();

    // after
    await waitFor(() => {
      expect(queryByText('Loading Workloads')).not.toBeInTheDocument();
    });
  });
});
