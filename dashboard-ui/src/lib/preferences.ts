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

import { dashboardClient } from '@/apollo-client';
import appConfig from '@/app-config';
import { PREFERENCES_GET, PREFERENCES_UPDATE } from '@/lib/graphql/dashboard/ops';

const CURRENT_VERSION = 1;
const STORAGE_KEY = 'kubetail:preferences';

export interface Preferences {
  version: number;
  theme?: string;
}

export interface PreferencesBackend {
  load(): Promise<Preferences>;
  save(patch: Partial<Preferences>): Promise<Preferences>;
}

function defaultPreferences(): Preferences {
  return { version: CURRENT_VERSION, theme: 'system' };
}

function merge(base: Preferences, patch: Partial<Preferences>): Preferences {
  return {
    version: CURRENT_VERSION,
    theme: patch.theme !== undefined ? patch.theme : base.theme,
  };
}

/**
 * LocalStorage backend (used in cluster mode)
 */

function createLocalStorageBackend(): PreferencesBackend {
  function load(): Promise<Preferences> {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return Promise.resolve(defaultPreferences());
    try {
      const parsed = JSON.parse(raw) as Partial<Preferences>;
      return Promise.resolve(merge(defaultPreferences(), parsed));
    } catch {
      return Promise.resolve(defaultPreferences());
    }
  }

  return {
    load,
    async save(patch: Partial<Preferences>): Promise<Preferences> {
      const current = await load();
      const merged = merge(current, patch);
      localStorage.setItem(STORAGE_KEY, JSON.stringify(merged));
      return merged;
    },
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
      return {
        version: data.preferencesGet.version,
        theme: data.preferencesGet.theme ?? undefined,
      };
    },

    async save(patch: Partial<Preferences>): Promise<Preferences> {
      const { data } = await dashboardClient.mutate({
        mutation: PREFERENCES_UPDATE,
        variables: { input: { theme: patch.theme } },
      });
      if (!data) throw new Error('no data returned from preferencesUpdate');
      const result = data.preferencesUpdate;
      return {
        version: result.version,
        theme: result.theme ?? undefined,
      };
    },
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
