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

const LATEST_VERSION_CACHE_KEY = 'kubetail:versionCheck:cliLatest';
const DISMISSED_KEY = 'kubetail:versionCheck:dismissed';
const IGNORED_VERSIONS_KEY = 'kubetail:versionCheck:ignoredVersions';

const CACHE_TTL_MS = 12 * 60 * 60 * 1000;
const DISMISS_TTL_MS = 24 * 60 * 60 * 1000;
const SHOW_DELAY_MS = 4000;

interface CachedLatestVersion {
  timestamp: number;
  version: string;
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

function readCachedLatestVersion(): string | null {
  try {
    const raw = localStorage.getItem(LATEST_VERSION_CACHE_KEY);
    if (!raw) return null;
    const entry: CachedLatestVersion = JSON.parse(raw);
    if (Date.now() - entry.timestamp >= CACHE_TTL_MS) return null;
    return entry.version;
  } catch {
    return null;
  }
}

function writeCachedLatestVersion(version: string) {
  try {
    const entry: CachedLatestVersion = { timestamp: Date.now(), version };
    localStorage.setItem(LATEST_VERSION_CACHE_KEY, JSON.stringify(entry));
  } catch {
    // fail silently
  }
}

function isDismissed(): boolean {
  try {
    const raw = localStorage.getItem(DISMISSED_KEY);
    if (!raw) return false;
    return Date.now() - Number(raw) < DISMISS_TTL_MS;
  } catch {
    return false;
  }
}

function setDismissed() {
  try {
    localStorage.setItem(DISMISSED_KEY, String(Date.now()));
  } catch {
    // fail silently
  }
}

function getIgnoredVersions(): string[] {
  try {
    const raw = localStorage.getItem(IGNORED_VERSIONS_KEY);
    if (!raw) return [];
    return JSON.parse(raw);
  } catch {
    return [];
  }
}

function addIgnoredVersion(version: string) {
  try {
    const versions = getIgnoredVersions();
    if (!versions.includes(version)) {
      versions.push(version);
      localStorage.setItem(IGNORED_VERSIONS_KEY, JSON.stringify(versions));
    }
  } catch {
    // fail silently
  }
}

export interface UpgradeNotificationState {
  showBanner: boolean;
  currentVersion: string;
  latestVersion: string | null;
  updateAvailable: boolean;
  dismiss: () => void;
  dontRemindMe: () => void;
}

const defaultState: UpgradeNotificationState = {
  showBanner: false,
  currentVersion: '',
  latestVersion: null,
  updateAvailable: false,
  dismiss: () => {},
  dontRemindMe: () => {},
};

const UpgradeNotificationContext = createContext<UpgradeNotificationState>(defaultState);

export function UpgradeNotificationProvider({ children }: React.PropsWithChildren) {
  const isDesktop = appConfig.environment === 'desktop';
  const currentVersion = appConfig.cliVersion;
  const [ready, setReady] = useState(false);
  const [dismissed, setDismissedState] = useState(() => isDismissed());
  const [ignored, setIgnored] = useState(() => getIgnoredVersions());

  const cachedLatest = useMemo(() => readCachedLatestVersion(), []);
  const skipQuery = !isDesktop || !currentVersion || cachedLatest !== null;

  useEffect(() => {
    const timer = setTimeout(() => setReady(true), SHOW_DELAY_MS);
    return () => clearTimeout(timer);
  }, []);

  const { data } = useQuery(CLI_LATEST_VERSION, {
    skip: skipQuery,
    fetchPolicy: 'network-only',
  });

  const latestVersion: string | null = cachedLatest ?? data?.cliLatestVersion ?? null;

  useEffect(() => {
    if (cachedLatest !== null || !isDesktop) return;
    const version = data?.cliLatestVersion;
    if (version) {
      writeCachedLatestVersion(version);
    }
  }, [cachedLatest, isDesktop, data]);

  const updateAvailable =
    currentVersion !== '' && latestVersion !== null && compareSemver(latestVersion, currentVersion) > 0;

  const hasUpdate = updateAvailable && !ignored.includes(latestVersion);

  const showBanner = ready && !dismissed && hasUpdate;

  const dismiss = useCallback(() => {
    setDismissed();
    setDismissedState(true);
  }, []);

  const dontRemindMe = useCallback(() => {
    if (updateAvailable && latestVersion) addIgnoredVersion(latestVersion);
    setIgnored(getIgnoredVersions());
    setDismissed();
    setDismissedState(true);
  }, [updateAvailable, latestVersion]);

  const value = useMemo(
    () => ({ showBanner, currentVersion, latestVersion, updateAvailable, dismiss, dontRemindMe }),
    [showBanner, currentVersion, latestVersion, updateAvailable, dismiss, dontRemindMe],
  );

  return <UpgradeNotificationContext.Provider value={value}>{children}</UpgradeNotificationContext.Provider>;
}

export function useUpgradeNotification(): UpgradeNotificationState {
  return useContext(UpgradeNotificationContext);
}
