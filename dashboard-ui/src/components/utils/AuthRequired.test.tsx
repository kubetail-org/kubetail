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

import { render } from '@testing-library/react';
import { createMemoryHistory } from 'history';
import { Router, MemoryRouter } from 'react-router-dom';
import type { Mock } from 'vitest';

import { useSession } from '@/lib/auth';

import AuthRequired from './AuthRequired';

describe('AuthRequired component tests', () => {
  it('should redirect to /auth/login if session is not authenticated', () => {
    const history = createMemoryHistory();

    render(
      <Router location={history.location} navigator={history}>
        <AuthRequired>my content</AuthRequired>
      </Router>,
    );

    // assertions
    expect(history.location.pathname).toBe('/auth/login');
    expect(history.location.search).toBe('?callbackUrl=%2F');
  });

  it('should show loading page while waiting for session', () => {
    // configure mock
    (useSession as Mock).mockReturnValue({
      session: undefined,
    });

    const { getByText } = render(
      <MemoryRouter>
        <AuthRequired>my content</AuthRequired>
      </MemoryRouter>,
    );

    // assertions
    expect(getByText('Loading...')).toBeInTheDocument();
  });

  it('should render inner content if session is authenticated', () => {
    // configure mock
    (useSession as Mock).mockReturnValue({
      session: { user: 'test' },
    });

    const { getByText } = render(
      <MemoryRouter>
        <AuthRequired>my content</AuthRequired>
      </MemoryRouter>,
    );

    // assertions
    expect(getByText('my content')).toBeInTheDocument();
  });
});
