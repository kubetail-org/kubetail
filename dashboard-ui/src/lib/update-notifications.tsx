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

import { useQuery } from '@apollo/client/react';
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';

import appConfig from '@/app-config';
import { CLI_LATEST_VERSION } from '@/lib/graphql/dashboard/ops';

const STORAGE_KEY = 'kubetail:updates:cli';

const CACHE_TTL_MS = 12 * 60 * 60 * 1000;
const DISMISS_TTL_MS = 24 * 60 * 60 * 1000;
const SHOW_DELAY_MS = 4000;

interface UpdateState {
  latestVersion?: string;
  fetchedAt?: number;
  dismissedAt?: number;
  skippedVersions?: string[];
}

export function compareSemver(a: string, b: string): number {
  const parse = (v: string) => v.replace(/^v/, '').split('.').map(Number);
  const pa = parse(a);
  const pb = parse(b);
  for (let i = 0; i < 3; i += 1) {
    const diff = (pa[i] || 0) - (pb[i] || 0);
    if (diff !== 0) return diff;
  }
  return 0;
}

function readState(): UpdateState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return {};
    return JSON.parse(raw);
  } catch {
    return {};
  }
}

function writeState(state: UpdateState) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
  } catch {
    // fail silently
  }
}

function patchState(patch: Partial<UpdateState>) {
  writeState({ ...readState(), ...patch });
}

function isCacheValid(state: UpdateState): boolean {
  return !!state.latestVersion && !!state.fetchedAt && Date.now() - state.fetchedAt < CACHE_TTL_MS;
}

export interface UpdateNotificationState {
  showBanner: boolean;
  currentVersion: string;
  latestVersion: string | null;
  updateAvailable: boolean;
  dismiss: () => void;
  dontRemindMe: () => void;
}

const UpdateNotificationContext = createContext({} as UpdateNotificationState);

export function UpdateNotificationProvider({ children }: React.PropsWithChildren) {
  const isDesktop = appConfig.environment === 'desktop';
  const currentVersion = appConfig.cliVersion;
  const [ready, setReady] = useState(false);
  const [state, setState] = useState(() => readState());

  const cacheValid = isCacheValid(state);

  useEffect(() => {
    const timer = setTimeout(() => setReady(true), SHOW_DELAY_MS);
    return () => clearTimeout(timer);
  }, []);

  const { data } = useQuery(CLI_LATEST_VERSION, {
    skip: !isDesktop || !currentVersion || cacheValid,
    fetchPolicy: 'network-only',
  });

  useEffect(() => {
    const version = data?.cliLatestVersion;
    if (version) patchState({ latestVersion: version, fetchedAt: Date.now() });
  }, [data]);

  const latestVersion = cacheValid ? state.latestVersion! : (data?.cliLatestVersion ?? null);

  const updateAvailable =
    currentVersion !== '' && latestVersion !== null && compareSemver(latestVersion, currentVersion) > 0;

  const dismissed = state.dismissedAt !== undefined && Date.now() - state.dismissedAt < DISMISS_TTL_MS;
  const skipped = latestVersion !== null && (state.skippedVersions ?? []).includes(latestVersion);
  const hasUpdate = updateAvailable && !skipped;

  const showBanner = ready && !dismissed && hasUpdate;

  const dismiss = useCallback(() => {
    patchState({ dismissedAt: Date.now() });
    setState(readState());
  }, []);

  const dontRemindMe = useCallback(() => {
    const { skippedVersions = [] } = readState();
    if (latestVersion && !skippedVersions.includes(latestVersion)) {
      skippedVersions.push(latestVersion);
    }
    patchState({ skippedVersions, dismissedAt: Date.now() });
    setState(readState());
  }, [latestVersion]);

  const value = useMemo(
    () => ({ showBanner, currentVersion, latestVersion, updateAvailable, dismiss, dontRemindMe }),
    [showBanner, currentVersion, latestVersion, updateAvailable, dismiss, dontRemindMe],
  );

  return <UpdateNotificationContext.Provider value={value}>{children}</UpdateNotificationContext.Provider>;
}

export function useUpdateNotification(): UpdateNotificationState {
  return useContext(UpdateNotificationContext);
}
