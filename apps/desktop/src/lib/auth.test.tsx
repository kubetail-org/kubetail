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

import { act, cleanup, render, renderHook, waitFor } from '@testing-library/react';
import { Provider as UrqlProvider } from 'urql';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { flushSubscriptionsSynchronously, mockTauriCore } from '@/test-utils';

// Mocks ---------------------------------------------------------------

const { invokeMock, channels, liveChannel, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());
flushSubscriptionsSynchronously();

const { createGraphqlClient } = await import('@/lib/graphql/client');
const { AuthProvider, useAuthState } = await import('./auth');

// Helpers -------------------------------------------------------------

const flush = () => act(async () => {});

type GqlIdentity = { sub: string; email: string; name: string };
type GqlAuthState = { restoring: boolean; authenticated: boolean; identity: GqlIdentity | null };

// The mutation response graphql_query returns; overridable per-test.
let mutationBody: unknown = { data: { login: true, logout: true } };

function pushAuthState(s: GqlAuthState) {
  liveChannel().onmessage!(JSON.stringify({ type: 'next', payload: { data: { authStateWatch: s } } }));
}

const SIGNED_IN: GqlAuthState = {
  restoring: false,
  authenticated: true,
  identity: { sub: 'sub-1', email: 'a@example.com', name: 'Ada' },
};
const SIGNED_OUT: GqlAuthState = { restoring: false, authenticated: false, identity: null };

function renderAuthState() {
  const client = createGraphqlClient();
  function wrapper({ children }: { children: React.ReactNode }) {
    return (
      <UrqlProvider value={client}>
        <AuthProvider>{children}</AuthProvider>
      </UrqlProvider>
    );
  }
  return renderHook(() => useAuthState(), { wrapper });
}

describe('AuthProvider / useAuthState', () => {
  beforeEach(() => {
    invokeMock.mockReset();
    channels.length = 0;
    mutationBody = { data: { login: true, logout: true } };
    let id = 0;
    invokeMock.mockImplementation(async (cmd: string) => {
      if (cmd === 'graphql_subscribe') {
        id += 1;
        return id;
      }
      if (cmd === 'graphql_unsubscribe') return undefined;
      if (cmd === 'graphql_query') {
        return { status: 200, body: JSON.stringify(mutationBody) };
      }
      throw new Error(`unexpected ${cmd}`);
    });
  });

  afterEach(cleanup);

  it('starts loading with an anonymous auth state before the first frame', async () => {
    const { result } = renderAuthState();
    await flush();
    expect(result.current.loading).toBe(true);
    expect(result.current.authState.authenticated).toBe(false);
  });

  it('subscribes to authStateWatch', async () => {
    renderAuthState();
    await flush();
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_subscribe',
      expect.objectContaining({ query: expect.stringContaining('authStateWatch') }),
    );
  });

  it('settles to anonymous on the first (signed-out) snapshot', async () => {
    const { result } = renderAuthState();
    await flush();
    await act(async () => {
      pushAuthState(SIGNED_OUT);
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.authState).toEqual({ authenticated: false, identity: null });
  });

  it('stays loading while the account is restoring, and not after', async () => {
    const { result } = renderAuthState();
    await flush();
    act(() => pushAuthState({ ...SIGNED_OUT, restoring: true }));
    expect(result.current.loading).toBe(true);

    act(() => pushAuthState(SIGNED_IN));
    expect(result.current.loading).toBe(false);
    expect(result.current.authState.authenticated).toBe(true);
  });

  it('adopts the identity from a signed-in frame', async () => {
    const { result } = renderAuthState();
    await flush();
    await act(async () => {
      pushAuthState(SIGNED_IN);
    });
    await waitFor(() =>
      expect(result.current.authState).toEqual({
        authenticated: true,
        identity: { sub: 'sub-1', email: 'a@example.com', name: 'Ada' },
      }),
    );
    expect(result.current.loading).toBe(false);
  });

  it('returns to anonymous when a signed-out frame arrives (sign-out / other window)', async () => {
    const { result } = renderAuthState();
    await flush();
    await act(async () => {
      pushAuthState(SIGNED_IN);
    });
    await waitFor(() => expect(result.current.authState.authenticated).toBe(true));

    await act(async () => {
      pushAuthState(SIGNED_OUT);
    });
    await waitFor(() => expect(result.current.authState.authenticated).toBe(false));
  });

  it('login() runs the authLoginStart mutation', async () => {
    const { result } = renderAuthState();
    await flush();
    await act(async () => {
      pushAuthState(SIGNED_OUT);
    });

    await act(async () => {
      await result.current.login();
    });
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_query',
      expect.objectContaining({ body: expect.stringContaining('authLoginStart') }),
    );
  });

  it('logout() runs the authLogout mutation', async () => {
    const { result } = renderAuthState();
    await flush();
    await act(async () => {
      pushAuthState(SIGNED_IN);
    });

    await act(async () => {
      await result.current.logout();
    });
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_query',
      expect.objectContaining({ body: expect.stringContaining('authLogout') }),
    );
  });

  it('login() rejects when the mutation errors', async () => {
    const { result } = renderAuthState();
    await flush();
    await act(async () => {
      pushAuthState(SIGNED_OUT);
    });

    mutationBody = { errors: [{ message: 'cloud account is not configured' }] };
    await act(async () => {
      await expect(result.current.login()).rejects.toBeTruthy();
    });
  });

  it('throws when useAuthState is used outside a provider', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    function Bare() {
      useAuthState();
      return null;
    }
    expect(() => render(<Bare />)).toThrow(/useAuthState must be used inside <AuthProvider>/);
    spy.mockRestore();
  });
});
