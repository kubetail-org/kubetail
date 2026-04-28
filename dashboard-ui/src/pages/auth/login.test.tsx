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

import { createMemoryHistory } from 'history';
import { Router } from 'react-router-dom';
import { fireEvent, render, waitFor } from '@testing-library/react';
import type { Mock } from 'vitest';

import { useSession } from '@/lib/auth';
import LoginPage from '@/pages/auth/login';
import { renderElement } from '@/test-utils';

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);

describe('Login Page', () => {
  it('renders loading page while waiting for session', () => {
    // mock auth
    (useSession as Mock).mockReturnValue({
      session: undefined,
    });

    const { getByText } = renderElement(<LoginPage />);

    // assertions
    expect(getByText('Loading...')).toBeInTheDocument();
  });

  it('renders login form when user is logged out', () => {
    const { getByText } = renderElement(<LoginPage />);
    expect(getByText('Sign into Kubernetes')).toBeInTheDocument();
  });

  it('sends X-CSRF-Token header on login POST', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({}) });

    const { getByText, getByPlaceholderText } = renderElement(<LoginPage />);

    fireEvent.change(getByPlaceholderText(/Enter your kubernetes token/i), {
      target: { value: 'my-k8s-token' },
    });
    fireEvent.click(getByText('Sign in'));

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    const init = fetchMock.mock.calls[0][1] as RequestInit;
    const headers = init.headers as Record<string, string>;
    expect(headers['X-CSRF-Token']).toBe('test-csrf-token');
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
      </Router>,
    );

    // assertions
    expect(history.location.pathname).toBe('/test-url');
  });
});
