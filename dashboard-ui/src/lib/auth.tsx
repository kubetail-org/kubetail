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

import { createContext, useContext, useEffect, useMemo, useState } from 'react';

import { getBasename, joinPaths } from '@/lib/util';

const bcName = 'auth/session';
const bcIn = new BroadcastChannel(bcName);
const bcOut = new BroadcastChannel(bcName);

export type Session = {
  auth_mode: string;
  user: string | null;
  message: string | null;
  timestamp: Date;
};

/**
 * Get CSRF token
 */
let csrfToken = '';
let pendingTokenRefresh: Promise<void> | null = null;

export function getCsrfToken(): string {
  return csrfToken;
}

type ContextType = {
  session: Session | undefined;
};

const Context = createContext({} as ContextType);

/**
 * Get session
 */

export async function getSession(): Promise<Session> {
  const url = new URL(joinPaths(getBasename(), '/api/auth/session'), window.location.origin);
  const resp = await fetch(url, { cache: 'no-store' });

  const tok = resp.headers.get('X-CSRF-Token');
  if (tok) csrfToken = tok;

  const sess = await resp.json();

  // convert timestamp
  sess.timestamp = new Date(sess.timestamp);

  // publish
  bcOut.postMessage(sess);

  return sess;
}

export function resetCsrfToken(): void {
  csrfToken = '';
}

export function waitForCsrfToken(): Promise<void> {
  if (csrfToken) return Promise.resolve();
  if (!pendingTokenRefresh) {
    pendingTokenRefresh = getSession()
      .then(() => {
        if (!csrfToken) throw new Error('CSRF token missing from session response');
      })
      .finally(() => {
        pendingTokenRefresh = null;
      });
  }
  return pendingTokenRefresh;
}

/**
 * Session hook
 */

export function useSession() {
  const { session } = useContext(Context);

  return {
    loading: session === undefined,
    session,
  };
}

/**
 * Session provider
 */

export const SessionProvider = ({ children }: React.PropsWithChildren) => {
  const [session, setSession] = useState<Session | undefined>(undefined);

  useEffect(() => {
    let lastTS = 0;
    const fn = (ev: any) => {
      const newSession = ev.data;
      if (newSession.timestamp > lastTS) {
        lastTS = newSession.timestamp;
        setSession(newSession);
      }
    };
    bcIn.addEventListener('message', fn);
    getSession();
    return () => bcIn.removeEventListener('message', fn);
  }, []);

  useEffect(() => {
    document.addEventListener('visibilitychange', getSession);
    return () => document.removeEventListener('visibilitychange', getSession);
  }, []);

  const context = useMemo(() => ({ session }), [session]);

  return <Context.Provider value={context}>{children}</Context.Provider>;
};
