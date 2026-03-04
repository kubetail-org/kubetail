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
import { useCallback, useEffect, useMemo, useState } from 'react';

import appConfig from '@/app-config';
import { CLI_VERSION_STATUS, CLUSTER_VERSION_STATUS } from '@/lib/graphql/dashboard/ops';

const CACHE_KEY = 'kubetail:versionCheck:cache';
const DISMISSED_KEY = 'kubetail:versionCheck:dismissed';
const IGNORED_VERSIONS_KEY = 'kubetail:versionCheck:ignoredVersions';

const CACHE_TTL_MS = 12 * 60 * 60 * 1000;
const DISMISS_TTL_MS = 24 * 60 * 60 * 1000;
const SHOW_DELAY_MS = 4000;

interface VersionStatusData {
  currentVersion: string;
  latestVersion: string;
  updateAvailable: boolean;
}

interface CachedVersionData {
  timestamp: number;
  cli: VersionStatusData | null;
  cluster: VersionStatusData | null;
}

function isCacheFresh(): boolean {
  try {
    const raw = localStorage.getItem(CACHE_KEY);
    if (!raw) return false;
    const cached: CachedVersionData = JSON.parse(raw);
    if (Date.now() - cached.timestamp >= CACHE_TTL_MS) return false;
    // Re-verify with backend when an update was pending (user may have upgraded since)
    if (cached.cli?.updateAvailable || cached.cluster?.updateAvailable) return false;
    return true;
  } catch {
    return false;
  }
}

function getCachedData(): CachedVersionData | null {
  try {
    const raw = localStorage.getItem(CACHE_KEY);
    if (!raw) return null;
    const cached: CachedVersionData = JSON.parse(raw);
    if (Date.now() - cached.timestamp < CACHE_TTL_MS) return cached;
    return null;
  } catch {
    return null;
  }
}

function setCachedData(cli: VersionStatusData | null, cluster: VersionStatusData | null) {
  try {
    const data: CachedVersionData = { timestamp: Date.now(), cli, cluster };
    localStorage.setItem(CACHE_KEY, JSON.stringify(data));
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
}

export function useUpgradeNotification(kubeContext: string | null): UpgradeNotificationState {
  const isDesktop = appConfig.environment === 'desktop';
  const [ready, setReady] = useState(false);
  const [dismissed, setDismissedState] = useState(() => isDismissed());
  const [ignored, setIgnored] = useState(() => getIgnoredVersions());

  const cacheFresh = useMemo(() => isCacheFresh(), []);
  const cachedData = useMemo(() => getCachedData(), []);

  // Delay showing the banner by SHOW_DELAY_MS after mount
  useEffect(() => {
    const timer = setTimeout(() => setReady(true), SHOW_DELAY_MS);
    return () => clearTimeout(timer);
  }, []);

  const { data: cliData } = useQuery(CLI_VERSION_STATUS, {
    skip: !isDesktop || cacheFresh,
    fetchPolicy: 'network-only',
  });

  const { data: clusterData } = useQuery(CLUSTER_VERSION_STATUS, {
    skip: cacheFresh,
    variables: kubeContext ? { kubeContext } : {},
    fetchPolicy: 'network-only',
  });

  const cliStatus: VersionStatusData | null = cacheFresh
    ? (cachedData?.cli ?? null)
    : (cliData?.cliVersionStatus ?? null);

  const clusterStatus: VersionStatusData | null = cacheFresh
    ? (cachedData?.cluster ?? null)
    : (clusterData?.clusterVersionStatus ?? null);

  // Cache fresh results from the network
  useEffect(() => {
    if (cacheFresh) return;
    const hasCli = cliData !== undefined;
    const hasCluster = clusterData !== undefined;
    if (hasCli || hasCluster) {
      setCachedData(cliData?.cliVersionStatus ?? null, clusterData?.clusterVersionStatus ?? null);
    }
  }, [cacheFresh, cliData, clusterData]);

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

  return { showBanner, cliStatus, clusterStatus, dismiss, dontRemindMe };
}
