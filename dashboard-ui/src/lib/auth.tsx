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
  namespace_lock: string[] | null;
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
 * Parse session
 */

// Returns null unless `value` has the shape of the session endpoint's JSON body.
function parseSession(value: unknown): Session | null {
  if (typeof value !== 'object' || value === null) return null;

  const {
    auth_mode: authMode,
    user,
    message,
    namespace_lock: namespaceLock,
    timestamp,
  } = value as Record<string, unknown>;
  if (typeof authMode !== 'string') return null;
  if (typeof user !== 'string' && user !== null) return null;
  if (typeof message !== 'string' && message !== null) return null;
  if (typeof timestamp !== 'string') return null;
  const isStringArray = Array.isArray(namespaceLock) && namespaceLock.every((ns) => typeof ns === 'string');
  if (!isStringArray && namespaceLock !== null) return null;

  return {
    auth_mode: authMode,
    user,
    message,
    namespace_lock: isStringArray ? namespaceLock : null,
    timestamp: new Date(timestamp),
  };
}

/**
 * Get session
 */

export async function getSession(): Promise<Session> {
  const url = new URL(joinPaths(getBasename(), '/api/auth/session'), window.location.origin);
  const resp = await fetch(url, { cache: 'no-store' });

  const tok = resp.headers.get('X-CSRF-Token');
  if (tok) csrfToken = tok;

  const body: unknown = await resp.json();
  const sess = parseSession(body);
  if (!sess) throw new Error('Invalid session response');

  // publish the JSON body, which each tab parses as it parsed this one
  // oxlint-disable-next-line unicorn/require-post-message-target-origin -- A BroadcastChannel takes no origin.
  bcOut.postMessage(body);

  return sess;
}

export function resetCsrfToken(): void {
  csrfToken = '';
}

export function waitForCsrfToken(): Promise<void> {
  if (csrfToken) return Promise.resolve();
  pendingTokenRefresh ??= getSession()
    .then(() => {
      if (!csrfToken) throw new Error('CSRF token missing from session response');
    })
    .finally(() => {
      pendingTokenRefresh = null;
    });
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
    const fn = (ev: MessageEvent<unknown>) => {
      const newSession = parseSession(ev.data);
      if (newSession && newSession.timestamp.getTime() > lastTS) {
        lastTS = newSession.timestamp.getTime();
        setSession(newSession);
      }
    };
    bcIn.addEventListener('message', fn);
    void getSession();
    return () => bcIn.removeEventListener('message', fn);
  }, []);

  useEffect(() => {
    const onVisibilityChange = () => {
      void getSession();
    };
    document.addEventListener('visibilitychange', onVisibilityChange);
    return () => document.removeEventListener('visibilitychange', onVisibilityChange);
  }, []);

  const context = useMemo(() => ({ session }), [session]);

  return <Context.Provider value={context}>{children}</Context.Provider>;
};
