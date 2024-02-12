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
import LoginPage from '@/pages/auth/login';
import { renderElement } from '@/test-utils';

describe('Login Page', () => {
  it('renders loading page while waiting for session', () => {
    // mock auth
    (useSession as Mock).mockReturnValue({
      session: undefined,
    });

    const { getByText } = renderElement(<LoginPage />);

    // assertions
    expect(getByText('Loading...')).toBeInTheDocument();
  })

  it('renders login form when user is logged out', () => {
    const { getByText } = renderElement(<LoginPage />);
    expect(getByText('Sign into Kubernetes')).toBeInTheDocument();
  });

  it('navigates to callbackUrl when user is logged in', () => {
    // mock auth
    (useSession as Mock).mockReturnValue({
      session: { user: 'test' },
    });

    const history = createMemoryHistory();

    render(
      <Router location="?callbackUrl=%2Ftest-url" navigator={history}>
        <LoginPage />
      </Router>
    );

    // assertions
    expect(history.location.pathname).toBe('/test-url');
  });
});
