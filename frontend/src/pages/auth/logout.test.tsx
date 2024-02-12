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
import { render } from '@testing-library/react';
import type { Mock } from 'vitest';

import { useSession } from '@/lib/auth';
import LogoutPage from '@/pages/auth/logout';
import { renderElement } from '@/test-utils';

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock); //vi.fn().mockResolvedValue({ ok: true }));

describe('Logout Page', () => {
  it('renders loading page while waiting for session', () => {
    // mock fetch
    fetchMock.mockResolvedValue({ ok: true });

    // mock auth
    (useSession as Mock).mockReturnValue({
      session: undefined,
    });

    const { getByText } = renderElement(<LogoutPage />);

    // assertions
    expect(getByText('Loading...')).toBeInTheDocument();
  })

  it('navigates to callbackUrl when user is logged out', () => {
    // mock fetch
    fetchMock.mockResolvedValue({ ok: true });

    const history = createMemoryHistory();

    render(
      <Router location="?callbackUrl=%2Ftest-url" navigator={history}>
        <LogoutPage />
      </Router>
    );

    // assertions
    expect(history.location.pathname).toBe('/test-url');
  });
});
