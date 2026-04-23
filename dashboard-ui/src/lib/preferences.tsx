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

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';

import { dashboardClient } from '@/apollo-client';
import appConfig from '@/app-config';
import { PREFERENCES_GET, PREFERENCES_UPDATE } from '@/lib/graphql/dashboard/ops';

const CURRENT_VERSION = 1;
const STORAGE_KEY = 'kubetail:preferences';

export interface Preferences {
  version: number;
  theme?: string;
  timezone?: string;
}

type PreferencesListener = (prefs: Preferences) => void;

export interface PreferencesBackend {
  load(): Promise<Preferences>;
  loadCached(): Preferences;
  save(patch: Partial<Preferences>): Promise<Preferences>;
  subscribe(listener: PreferencesListener): () => void;
}

function defaultPreferences(): Preferences {
  return { version: CURRENT_VERSION, theme: 'system', timezone: 'UTC' };
}

function merge(base: Preferences, patch: Partial<Preferences>): Preferences {
  return {
    version: CURRENT_VERSION,
    theme: patch.theme !== undefined ? patch.theme : base.theme,
    timezone: patch.timezone !== undefined ? patch.timezone : base.timezone,
  };
}

function readCache(): Preferences {
  const raw = localStorage.getItem(STORAGE_KEY);
  if (!raw) return defaultPreferences();
  try {
    const parsed = JSON.parse(raw) as Partial<Preferences>;
    return merge(defaultPreferences(), parsed);
  } catch {
    return defaultPreferences();
  }
}

function writeCache(prefs: Preferences): void {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(prefs));
}

function subscribeToStorageEvents(listener: PreferencesListener): () => void {
  const handler = (ev: StorageEvent) => {
    if (ev.key !== STORAGE_KEY) return;
    listener(readCache());
  };
  window.addEventListener('storage', handler);
  return () => window.removeEventListener('storage', handler);
}

/**
 * LocalStorage backend (used in cluster mode)
 */

function createLocalStorageBackend(): PreferencesBackend {
  return {
    load(): Promise<Preferences> {
      return Promise.resolve(readCache());
    },

    loadCached(): Preferences {
      return readCache();
    },

    async save(patch: Partial<Preferences>): Promise<Preferences> {
      const merged = merge(readCache(), patch);
      writeCache(merged);
      return merged;
    },

    subscribe: subscribeToStorageEvents,
  };
}

/**
 * GraphQL backend (used in desktop mode)
 */

function createGraphQLBackend(): PreferencesBackend {
  return {
    async load(): Promise<Preferences> {
      const { data } = await dashboardClient.query({
        query: PREFERENCES_GET,
        fetchPolicy: 'no-cache',
      });
      if (!data) throw new Error('no data returned from preferencesGet');
      const prefs: Preferences = {
        version: data.preferencesGet.version,
        theme: data.preferencesGet.theme ?? undefined,
        timezone: data.preferencesGet.timezone ?? undefined,
      };
      writeCache(prefs);
      return prefs;
    },

    loadCached(): Preferences {
      return readCache();
    },

    async save(patch: Partial<Preferences>): Promise<Preferences> {
      // write cache immediately for instant UI
      const optimistic = merge(readCache(), patch);
      writeCache(optimistic);

      const { data } = await dashboardClient.mutate({
        mutation: PREFERENCES_UPDATE,
        variables: { input: { theme: patch.theme, timezone: patch.timezone } },
      });
      if (!data) throw new Error('no data returned from preferencesUpdate');
      const result = data.preferencesUpdate;
      const prefs: Preferences = {
        version: result.version,
        theme: result.theme ?? undefined,
        timezone: result.timezone ?? undefined,
      };
      writeCache(prefs);
      return prefs;
    },

    subscribe: subscribeToStorageEvents,
  };
}

/**
 * Factory
 */

export function createPreferencesBackend(environment: string): PreferencesBackend {
  if (environment === 'desktop') {
    return createGraphQLBackend();
  }
  return createLocalStorageBackend();
}

export const preferencesBackend = createPreferencesBackend(appConfig.environment);

/**
 * PreferencesProvider
 */

type PreferencesContextType = {
  preferences: Preferences;
  updatePreferences: (patch: Partial<Preferences>) => void;
};

const PreferencesContext = createContext({} as PreferencesContextType);

export function PreferencesProvider({ children }: React.PropsWithChildren) {
  const [preferences, setPreferences] = useState(() => preferencesBackend.loadCached());

  // load from backend on mount (reconcile with server)
  useEffect(() => {
    preferencesBackend.load().then(setPreferences);
  }, []);

  // subscribe to cross-tab changes
  useEffect(() => preferencesBackend.subscribe(setPreferences), []);

  const updatePreferences = useCallback((patch: Partial<Preferences>) => {
    setPreferences((prev) => ({ ...prev, ...patch }));
    preferencesBackend.save(patch);
  }, []);

  const context = useMemo(() => ({ preferences, updatePreferences }), [preferences, updatePreferences]);

  return <PreferencesContext.Provider value={context}>{children}</PreferencesContext.Provider>;
}

/**
 * usePreferences hook
 */

export function usePreferences() {
  return useContext(PreferencesContext);
}
