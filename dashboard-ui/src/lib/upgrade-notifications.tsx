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
import { CLI_VERSION_STATUS, CLUSTER_VERSION_STATUS } from '@/lib/graphql/dashboard/ops';

const CLI_CACHE_KEY = 'kubetail:versionCheck:cli';
const CLUSTER_CACHE_KEY_PREFIX = 'kubetail:versionCheck:cluster:';
const DISMISSED_KEY = 'kubetail:versionCheck:dismissed';
const IGNORED_VERSIONS_KEY = 'kubetail:versionCheck:ignoredVersions';

const CACHE_TTL_MS = 12 * 60 * 60 * 1000;
const DISMISS_TTL_MS = 24 * 60 * 60 * 1000;
const SHOW_DELAY_MS = 4000;

export interface VersionStatusData {
  currentVersion: string;
  latestVersion: string;
  updateAvailable: boolean;
}

interface CachedEntry {
  timestamp: number;
  data: VersionStatusData | null;
}

function clusterCacheKey(kubeContext: string | null): string {
  return CLUSTER_CACHE_KEY_PREFIX + (kubeContext ?? '__default__');
}

function readCachedEntry(key: string): CachedEntry | null {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return null;
    const entry: CachedEntry = JSON.parse(raw);
    if (Date.now() - entry.timestamp >= CACHE_TTL_MS) return null;
    return entry;
  } catch {
    return null;
  }
}

function isCacheEntryFresh(key: string): boolean {
  const entry = readCachedEntry(key);
  if (!entry) return false;
  if (entry.data?.updateAvailable) return false;
  return true;
}

function writeCachedEntry(key: string, data: VersionStatusData | null) {
  try {
    const entry: CachedEntry = { timestamp: Date.now(), data };
    localStorage.setItem(key, JSON.stringify(entry));
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
  cliStatus: VersionStatusData | null;
  clusterStatus: VersionStatusData | null;
  dismiss: () => void;
  dontRemindMe: () => void;
  setKubeContext: (ctx: string | null) => void;
}

const defaultState: UpgradeNotificationState = {
  showBanner: false,
  cliStatus: null,
  clusterStatus: null,
  dismiss: () => {},
  dontRemindMe: () => {},
  setKubeContext: () => {},
};

const UpgradeNotificationContext = createContext<UpgradeNotificationState>(defaultState);

export function UpgradeNotificationProvider({ children }: React.PropsWithChildren) {
  const isDesktop = appConfig.environment === 'desktop';
  const [ready, setReady] = useState(false);
  const [dismissed, setDismissedState] = useState(() => isDismissed());
  const [ignored, setIgnored] = useState(() => getIgnoredVersions());
  const [kubeContext, setKubeContext] = useState<string | null>(null);

  const cliCacheFresh = useMemo(() => isCacheEntryFresh(CLI_CACHE_KEY), []);
  const clusterCacheFresh = useMemo(() => isCacheEntryFresh(clusterCacheKey(kubeContext)), [kubeContext]);

  useEffect(() => {
    const timer = setTimeout(() => setReady(true), SHOW_DELAY_MS);
    return () => clearTimeout(timer);
  }, []);

  const { data: cliData } = useQuery(CLI_VERSION_STATUS, {
    skip: !isDesktop || cliCacheFresh,
    fetchPolicy: 'network-only',
  });

  const { data: clusterData, loading: clusterLoading } = useQuery(CLUSTER_VERSION_STATUS, {
    skip: clusterCacheFresh,
    variables: kubeContext ? { kubeContext } : {},
    fetchPolicy: 'network-only',
  });

  const cliStatus: VersionStatusData | null = cliCacheFresh
    ? (readCachedEntry(CLI_CACHE_KEY)?.data ?? null)
    : (cliData?.cliVersionStatus ?? null);

  let clusterStatus: VersionStatusData | null = null;
  if (clusterCacheFresh) {
    clusterStatus = readCachedEntry(clusterCacheKey(kubeContext))?.data ?? null;
  } else if (!clusterLoading) {
    clusterStatus = clusterData?.clusterVersionStatus ?? null;
  }

  useEffect(() => {
    if (cliCacheFresh || !isDesktop) return;
    if (cliData !== undefined) {
      writeCachedEntry(CLI_CACHE_KEY, cliData?.cliVersionStatus ?? null);
    }
  }, [cliCacheFresh, isDesktop, cliData]);

  useEffect(() => {
    if (clusterCacheFresh) return;
    if (clusterData !== undefined) {
      writeCachedEntry(clusterCacheKey(kubeContext), clusterData?.clusterVersionStatus ?? null);
    }
  }, [clusterCacheFresh, kubeContext, clusterData]);

  const hasUpdate =
    (cliStatus?.updateAvailable && !ignored.includes(cliStatus.latestVersion)) ||
    (clusterStatus?.updateAvailable && !ignored.includes(clusterStatus.latestVersion));

  const showBanner = ready && !dismissed && Boolean(hasUpdate);

  const dismiss = useCallback(() => {
    setDismissed();
    setDismissedState(true);
  }, []);

  const dontRemindMe = useCallback(() => {
    if (cliStatus?.updateAvailable) addIgnoredVersion(cliStatus.latestVersion);
    if (clusterStatus?.updateAvailable) addIgnoredVersion(clusterStatus.latestVersion);
    setIgnored(getIgnoredVersions());
    setDismissed();
    setDismissedState(true);
  }, [cliStatus, clusterStatus]);

  const value = useMemo(
    () => ({ showBanner, cliStatus, clusterStatus, dismiss, dontRemindMe, setKubeContext }),
    [showBanner, cliStatus, clusterStatus, dismiss, dontRemindMe],
  );

  return <UpgradeNotificationContext.Provider value={value}>{children}</UpgradeNotificationContext.Provider>;
}

export function useUpgradeNotification(): UpgradeNotificationState {
  return useContext(UpgradeNotificationContext);
}
