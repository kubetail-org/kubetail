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

import { useQuery, useSubscription } from '@apollo/client/react';
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';

import appConfig from '@/app-config';
import { CLI_LATEST_VERSION, CLUSTER_VERSION_STATUS, KUBE_CONFIG_WATCH } from '@/lib/graphql/dashboard/ops';

/**
 * CLI vs cluster update hints: localStorage + Apollo, exposed via separate React contexts.
 *
 * Layout:
 * - Persistence + cache helpers (this file, top)
 * - `ClusterVersionSubscriber`: one `CLUSTER_VERSION_STATUS` query per kubeContext on desktop
 * - `UpdateNotificationProvider`: wires CLI + cluster subscribers and context providers
 * - `useCLIUpdateNotification` / `useClusterUpdateNotification`: read-only hooks for UI
 */

const STORAGE_KEY_CLI = 'kubetail:updates:cli';
const CLUSTER_STORAGE_PREFIX = 'kubetail:updates:cluster:';

const CACHE_TTL_MS = 12 * 60 * 60 * 1000;
const DISMISS_TTL_MS = 24 * 60 * 60 * 1000;
const SHOW_DELAY_MS = 4000;

interface UpdateState {
  latestVersion?: string;
  fetchedAt?: number;
  dismissedAt?: number;
  skippedVersions?: string[];
}

interface ClusterUpdateState extends UpdateState {
  currentVersion?: string;
}

// --- localStorage (CLI key + one blob per kubeContext)
function clusterStorageKey(kubeContext: string): string {
  return `${CLUSTER_STORAGE_PREFIX}${kubeContext}`;
}

function readPersisted<T extends object>(storageKey: string): T {
  try {
    const raw = localStorage.getItem(storageKey);
    if (!raw) return {} as T;
    return JSON.parse(raw) as T;
  } catch {
    return {} as T;
  }
}

function writePersisted(storageKey: string, state: object) {
  try {
    localStorage.setItem(storageKey, JSON.stringify(state));
  } catch {
    // fail silently
  }
}

function patchPersisted<T extends object>(storageKey: string, patch: Partial<T>) {
  writePersisted(storageKey, { ...readPersisted<T>(storageKey), ...patch });
}

function readCLIState(): UpdateState {
  return readPersisted<UpdateState>(STORAGE_KEY_CLI);
}

function readClusterState(kubeContext: string): ClusterUpdateState {
  return readPersisted<ClusterUpdateState>(clusterStorageKey(kubeContext));
}

function patchCLIState(patch: Partial<UpdateState>) {
  patchPersisted(STORAGE_KEY_CLI, patch);
}

function patchClusterState(kubeContext: string, patch: Partial<ClusterUpdateState>) {
  patchPersisted(clusterStorageKey(kubeContext), patch);
}

// Cache TTL (avoid hammering the network; still respect dismiss/skip in the view builders)
function isCLICacheValid(state: UpdateState): boolean {
  return !!state.latestVersion && !!state.fetchedAt && Date.now() - state.fetchedAt < CACHE_TTL_MS;
}

function isClusterCacheValid(state: ClusterUpdateState): boolean {
  return state.fetchedAt !== undefined && Date.now() - state.fetchedAt < CACHE_TTL_MS;
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

// Public shapes for hooks
interface BaseUpdateNotificationState {
  latestVersion: string | null;
  dismiss: () => void;
  dontRemindMe: () => void;
}

export interface UpdateNotificationState extends BaseUpdateNotificationState {
  showBanner: boolean;
  currentVersion: string;
  updateAvailable: boolean;
}

export interface ClusterUpdateNotificationState extends BaseUpdateNotificationState {
  updateAvailable: boolean;
  currentVersion: string | null;
}

type ClusterVersionQuerySnapshot = {
  data?: {
    clusterVersionStatus?: {
      currentVersion?: string | null;
      latestVersion?: string | null;
      updateAvailable: boolean;
    } | null;
  } | null;
  error?: Error;
  loading: boolean;
};

type ClusterNotificationsRegistry = {
  querySnapshots: Record<string, ClusterVersionQuerySnapshot>;
};

// CLI value vs cluster registry are split so CLI-only UI does not subscribe to cluster updates.
const CLIUpdateNotificationContext = createContext({} as UpdateNotificationState);
const ClusterNotificationsContext = createContext<ClusterNotificationsRegistry | null>(null);
const ClusterNotificationsInvalidateContext = createContext<(() => void) | null>(null);

/** Derive what to show for one kubeContext from persisted state + latest Apollo snapshot. */
function buildClusterNotificationView(
  kubeContext: string,
  snapshot: ClusterVersionQuerySnapshot | undefined,
  dismiss: () => void,
  dontRemindMe: () => void,
): ClusterUpdateNotificationState {
  const persisted = readClusterState(kubeContext);
  const cacheValid = isClusterCacheValid(persisted);
  const data = snapshot?.data;

  const currentVersion = cacheValid
    ? (persisted.currentVersion ?? null)
    : (data?.clusterVersionStatus?.currentVersion ?? null);

  const latestVersion = cacheValid
    ? (persisted.latestVersion ?? null)
    : (data?.clusterVersionStatus?.latestVersion ?? null);

  const queryUpdateAvailable = cacheValid
    ? currentVersion !== null && latestVersion !== null && currentVersion !== latestVersion
    : (data?.clusterVersionStatus?.updateAvailable ?? false);

  const dismissed = persisted.dismissedAt !== undefined && Date.now() - persisted.dismissedAt < DISMISS_TTL_MS;
  const skipped = latestVersion !== null && (persisted.skippedVersions ?? []).includes(latestVersion);
  const updateAvailable = queryUpdateAvailable && !dismissed && !skipped;

  return {
    updateAvailable,
    currentVersion,
    latestVersion,
    dismiss,
    dontRemindMe,
  };
}

/**
 * Invisible per-context worker: runs `CLUSTER_VERSION_STATUS`, mirrors results into `querySnapshots`,
 * and refreshes localStorage after a successful fetch (or error) so the cache stays coherent.
 */
function ClusterVersionSubscriber({
  kubeContext,
  bumpCluster,
  setSnapshot,
}: {
  kubeContext: string;
  bumpCluster: () => void;
  setSnapshot: (ctx: string, snap: ClusterVersionQuerySnapshot) => void;
}) {
  const isDesktop = appConfig.environment === 'desktop';
  const [persisted, setPersisted] = useState(() => readClusterState(kubeContext));

  useEffect(() => {
    setPersisted(readClusterState(kubeContext));
  }, [kubeContext]);

  const cacheValid = isClusterCacheValid(persisted);

  // Skip while we already have a fresh persisted row (see patch effect below).
  const { data, error, loading } = useQuery(CLUSTER_VERSION_STATUS, {
    skip: !isDesktop || !kubeContext || cacheValid,
    variables: { kubeContext },
    fetchPolicy: 'network-only',
  });

  useEffect(() => {
    setSnapshot(kubeContext, { data, error, loading });
  }, [kubeContext, data, error, loading, setSnapshot]);

  // After load: persist versions (or mark fetch time on failure), then refresh registry (see bumpCluster).
  useEffect(() => {
    if (!isDesktop || !kubeContext || cacheValid) return;
    if (loading) return;

    if (error) {
      patchClusterState(kubeContext, {
        fetchedAt: Date.now(),
        currentVersion: undefined,
        latestVersion: undefined,
      });
      setPersisted(readClusterState(kubeContext));
      bumpCluster();
      return;
    }

    if (data === undefined) return;

    const result = data.clusterVersionStatus;
    if (result) {
      patchClusterState(kubeContext, {
        currentVersion: result.currentVersion,
        latestVersion: result.latestVersion,
        fetchedAt: Date.now(),
      });
    } else {
      patchClusterState(kubeContext, {
        fetchedAt: Date.now(),
        currentVersion: undefined,
        latestVersion: undefined,
      });
    }
    setPersisted(readClusterState(kubeContext));
    bumpCluster();
  }, [data, error, loading, isDesktop, kubeContext, cacheValid, bumpCluster]);

  return null;
}

/**
 * Root provider: CLI banner state + one `ClusterVersionSubscriber` per kubeContext (desktop).
 * Children render after subscribers so snapshots exist before hooks in descendants run.
 */
export function UpdateNotificationProvider({ children }: React.PropsWithChildren) {
  const isDesktop = appConfig.environment === 'desktop';
  const currentVersionCLI = appConfig.cliVersion;
  const [ready, setReady] = useState(false);
  const [cliPersisted, setCLIPersisted] = useState(() => readCLIState());
  const [mountedAt] = useState(() => Date.now());

  const cliCacheValid = isCLICacheValid(cliPersisted);

  // Desktop: watch kubeconfig so we know which contexts exist (cluster mode uses a single synthetic context).
  const { data: kubeData } = useSubscription(KUBE_CONFIG_WATCH, {
    skip: appConfig.environment !== 'desktop',
  });

  const kubeContexts = useMemo(() => {
    if (appConfig.environment === 'cluster') return [''];
    const contexts = kubeData?.kubeConfigWatch?.object?.contexts;
    return contexts?.map((c) => c.name) ?? [];
  }, [kubeData]);

  // Cluster registry: Apollo snapshots per kubeContext; `bumpCluster` shallow-copies the map to invalidate
  // consumers when only localStorage changed (dismiss/skip) or after persist without a new Apollo payload.
  const [querySnapshots, setQuerySnapshots] = useState<Record<string, ClusterVersionQuerySnapshot>>({});

  const bumpCluster = useCallback(() => {
    setQuerySnapshots((prev) => ({ ...prev }));
  }, []);

  const setSnapshot = useCallback((kubeContext: string, snap: ClusterVersionQuerySnapshot) => {
    setQuerySnapshots((prev) => {
      const cur = prev[kubeContext];
      if (cur && cur.data === snap.data && cur.error === snap.error && cur.loading === snap.loading) {
        return prev;
      }
      return { ...prev, [kubeContext]: snap };
    });
  }, []);

  const clusterRegistry = useMemo(() => ({ querySnapshots }), [querySnapshots]);

  // Delay showing the CLI banner so it does not flash on cold load.
  useEffect(() => {
    const timer = setTimeout(() => setReady(true), SHOW_DELAY_MS);
    return () => clearTimeout(timer);
  }, []);

  const { data: cliQueryData } = useQuery(CLI_LATEST_VERSION, {
    skip: !isDesktop || !currentVersionCLI || cliCacheValid,
    fetchPolicy: 'network-only',
  });

  useEffect(() => {
    const version = cliQueryData?.cliLatestVersion;
    if (version) patchCLIState({ latestVersion: version, fetchedAt: Date.now() });
  }, [cliQueryData]);

  const latestVersionCLI = cliCacheValid ? cliPersisted.latestVersion! : (cliQueryData?.cliLatestVersion ?? null);

  const cliUpdateAvailable =
    currentVersionCLI !== '' && latestVersionCLI !== null && compareSemver(latestVersionCLI, currentVersionCLI) > 0;

  // `mountedAt` is fixed at provider mount so dismiss TTL is stable for this session (matches prior behavior).
  const cliDismissed = cliPersisted.dismissedAt !== undefined && mountedAt - cliPersisted.dismissedAt < DISMISS_TTL_MS;
  const cliSkipped = latestVersionCLI !== null && (cliPersisted.skippedVersions ?? []).includes(latestVersionCLI);
  const cliHasUpdate = cliUpdateAvailable && !cliSkipped;

  const showBanner = ready && !cliDismissed && cliHasUpdate;

  const dismissCLI = useCallback(() => {
    patchCLIState({ dismissedAt: Date.now() });
    setCLIPersisted(readCLIState());
  }, []);

  const dontRemindCLI = useCallback(() => {
    const { skippedVersions = [] } = readCLIState();
    if (latestVersionCLI && !skippedVersions.includes(latestVersionCLI)) {
      skippedVersions.push(latestVersionCLI);
    }
    patchCLIState({ skippedVersions, dismissedAt: Date.now() });
    setCLIPersisted(readCLIState());
  }, [latestVersionCLI]);

  const cliValue = useMemo(
    () => ({
      showBanner,
      currentVersion: currentVersionCLI,
      latestVersion: latestVersionCLI,
      updateAvailable: cliUpdateAvailable,
      dismiss: dismissCLI,
      dontRemindMe: dontRemindCLI,
    }),
    [showBanner, currentVersionCLI, latestVersionCLI, cliUpdateAvailable, dismissCLI, dontRemindCLI],
  );

  return (
    <CLIUpdateNotificationContext.Provider value={cliValue}>
      {/* Invalidate is separate from registry data: dismiss/remind only needs the callback, not the snapshot map. */}
      <ClusterNotificationsInvalidateContext.Provider value={bumpCluster}>
        <ClusterNotificationsContext.Provider value={clusterRegistry}>
          {kubeContexts.map((ctx) => (
            <ClusterVersionSubscriber
              key={ctx || '__default__'}
              kubeContext={ctx}
              bumpCluster={bumpCluster}
              setSnapshot={setSnapshot}
            />
          ))}
          {children}
        </ClusterNotificationsContext.Provider>
      </ClusterNotificationsInvalidateContext.Provider>
    </CLIUpdateNotificationContext.Provider>
  );
}

/** Latest CLI update banner state (no cluster context subscription). */
export function useCLIUpdateNotification(): UpdateNotificationState {
  return useContext(CLIUpdateNotificationContext);
}

/** Per-kubeContext cluster update row; reads shared registry + bumps via invalidate after mutations. */
export function useClusterUpdateNotification(kubeContext: string): ClusterUpdateNotificationState {
  const clusterRegistry = useContext(ClusterNotificationsContext);
  const invalidateClusterNotifications = useContext(ClusterNotificationsInvalidateContext);

  const dismissCluster = useCallback(() => {
    if (!clusterRegistry || !invalidateClusterNotifications) return;
    patchClusterState(kubeContext, { dismissedAt: Date.now() });
    invalidateClusterNotifications();
  }, [kubeContext, clusterRegistry, invalidateClusterNotifications]);

  const dontRemindCluster = useCallback(() => {
    if (!clusterRegistry || !invalidateClusterNotifications) return;
    const persisted = readClusterState(kubeContext);
    const valid = isClusterCacheValid(persisted);
    const snap = clusterRegistry.querySnapshots[kubeContext];
    const lv = valid ? (persisted.latestVersion ?? null) : (snap?.data?.clusterVersionStatus?.latestVersion ?? null);
    const { skippedVersions = [] } = persisted;
    if (lv && !skippedVersions.includes(lv)) {
      skippedVersions.push(lv);
    }
    patchClusterState(kubeContext, { skippedVersions, dismissedAt: Date.now() });
    invalidateClusterNotifications();
  }, [kubeContext, clusterRegistry, invalidateClusterNotifications]);

  const clusterView = useMemo(() => {
    if (!clusterRegistry) {
      return {
        updateAvailable: false,
        currentVersion: null,
        latestVersion: null,
        dismiss: dismissCluster,
        dontRemindMe: dontRemindCluster,
      } as ClusterUpdateNotificationState;
    }
    return buildClusterNotificationView(
      kubeContext,
      clusterRegistry.querySnapshots[kubeContext],
      dismissCluster,
      dontRemindCluster,
    );
  }, [kubeContext, clusterRegistry, dismissCluster, dontRemindCluster]);
  return clusterView;
}
