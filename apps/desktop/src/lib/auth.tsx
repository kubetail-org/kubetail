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

// Auth state from the sidecar over GraphQL. The renderer never holds tokens — the
// sidecar owns OAuth + keychain and publishes `authStateWatch` (snapshot, then
// deltas); `login`/`logout` are thin mutations whose result arrives back over the
// same subscription. Sign-in is non-blocking: the mutation returns once the browser
// opens; signed-in state lands later (or never, if abandoned).
import { createContext, useCallback, useContext, useMemo } from 'react';
import { useMutation } from 'urql';

import { graphql } from '@/gql';
import type { AuthStateWatchSubscription } from '@/gql/graphql';
import { stateWatchReducer } from '@/lib/graphql/state-watch';
import { useFoldedSubscription } from '@/lib/graphql/use-folded-subscription';

// Mirrors the sidecar's GraphQL shape; `identity` is non-null only when authenticated.
export type Identity = {
  sub: string;
  email: string;
  name: string;
};

export type AuthState = {
  authenticated: boolean;
  identity: Identity | null;
};

const SIGNED_OUT: AuthState = { authenticated: false, identity: null };

const AuthStateWatchDocument = graphql(`
  subscription AuthStateWatch {
    authStateWatch {
      restoring
      authenticated
      identity {
        sub
        email
        name
      }
    }
  }
`);

const AuthLoginStartMutation = graphql(`
  mutation AuthLoginStart {
    authLoginStart
  }
`);

const AuthLogoutMutation = graphql(`
  mutation AuthLogout {
    authLogout
  }
`);

type AuthStateContextValue = {
  authState: AuthState;
  loading: boolean;
  login: () => Promise<void>;
  logout: () => Promise<void>;
};

const AuthStateContext = createContext<AuthStateContextValue | null>(null);

function toAuthState(s: AuthStateWatchSubscription['authStateWatch']): AuthState {
  return {
    authenticated: s.authenticated,
    identity: s.identity ? { sub: s.identity.sub, email: s.identity.email, name: s.identity.name } : null,
  };
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  // Last-value semantics; reverts to "no frame yet" (→ loading) on a transport
  // reconnect until the snapshot replays.
  const { data } = useFoldedSubscription<AuthStateWatchSubscription, AuthStateWatchSubscription>(
    { query: AuthStateWatchDocument },
    stateWatchReducer((next) => next),
  );
  const [, runStartLogin] = useMutation(AuthLoginStartMutation);
  const [, runLogout] = useMutation(AuthLogoutMutation);

  // Loading until the first frame, and while the launch reads the keyring: until
  // then a signed-out frame says nothing about the account.
  const authState = data ? toAuthState(data.authStateWatch) : SIGNED_OUT;
  const loading = data === undefined || data.authStateWatch.restoring;

  // Resulting state arrives over the subscription; mutation errors go to the error
  // bus and are rethrown to callers. A browser sign-in's outcome is never a
  // mutation error — it shows up as an auth-state change (or stays signed-out).
  const login = useCallback(async () => {
    const res = await runStartLogin({});
    if (res.error) throw res.error;
  }, [runStartLogin]);

  const logout = useCallback(async () => {
    const res = await runLogout({});
    if (res.error) throw res.error;
  }, [runLogout]);

  const value = useMemo(() => ({ authState, loading, login, logout }), [authState, loading, login, logout]);

  return <AuthStateContext.Provider value={value}>{children}</AuthStateContext.Provider>;
}

export function useAuthState(): AuthStateContextValue {
  const ctx = useContext(AuthStateContext);
  if (!ctx) throw new Error('useAuthState must be used inside <AuthProvider>');
  return ctx;
}
