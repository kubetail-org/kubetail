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
import { atom, useAtom, useAtomValue, useSetAtom } from 'jotai';
import { atomWithStorage } from 'jotai/utils';
import { atomFamily } from 'jotai-family';
import { useEffect, useMemo } from 'react';

import appConfig from '@/app-config';
import { CLI_LATEST_VERSION, CLUSTER_VERSION_STATUS } from '@/lib/graphql/dashboard/ops';
import { kubeConfigAtom, useKubeConfig } from '@/lib/kubeconfig';

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

// --- Atoms (single source of truth)

// atomWithStorage handles localStorage read/write AND cross-tab sync via the storage event.
const cliPersistedAtom = atomWithStorage<UpdateState>(STORAGE_KEY_CLI, {});

const clusterPersistedAtomFamily = atomFamily((kubeContext: string) =>
  atomWithStorage<ClusterUpdateState>(`${CLUSTER_STORAGE_PREFIX}${kubeContext}`, {}),
);

const readyAtom = atom(false);

// --- Derived view atoms

const cliViewAtom = atom<Omit<UpdateNotificationState, 'dismiss' | 'dontRemindMe'>>((get) => {
  const persisted = get(cliPersistedAtom);
  const ready = get(readyAtom);
  const currentVersion = appConfig.cliVersion;

  const cacheValid = isCLICacheValid(persisted);
  const latestVersion = cacheValid ? persisted.latestVersion! : null;

  const updateAvailable =
    currentVersion !== '' && latestVersion !== null && compareSemver(latestVersion, currentVersion) > 0;
  const dismissed = persisted.dismissedAt !== undefined && Date.now() - persisted.dismissedAt < DISMISS_TTL_MS;
  const skipped = latestVersion !== null && (persisted.skippedVersions ?? []).includes(latestVersion);

  return {
    showBanner: ready && !dismissed && updateAvailable && !skipped,
    currentVersion,
    latestVersion,
    updateAvailable,
  };
});

type ClusterView = Omit<ClusterUpdateNotificationState, 'dismiss' | 'dontRemindMe'>;

const clusterViewAtomFamily = atomFamily((kubeContext: string) =>
  atom<ClusterView>((get) => {
    const persisted = get(clusterPersistedAtomFamily(kubeContext));
    const cacheValid = isClusterCacheValid(persisted);
    const currentVersion = cacheValid ? (persisted.currentVersion ?? null) : null;
    const latestVersion = cacheValid ? (persisted.latestVersion ?? null) : null;

    const updateAvailable =
      currentVersion !== null && latestVersion !== null && compareSemver(latestVersion, currentVersion) > 0;
    const dismissed = persisted.dismissedAt !== undefined && Date.now() - persisted.dismissedAt < DISMISS_TTL_MS;
    const skipped = latestVersion !== null && (persisted.skippedVersions ?? []).includes(latestVersion);

    return {
      updateAvailable: updateAvailable && !dismissed && !skipped,
      currentVersion,
      latestVersion,
    };
  }),
);

const allClusterViewsAtom = atom((get) => {
  const cfg = get(kubeConfigAtom);
  const names = appConfig.environment !== 'desktop' ? [''] : (cfg.data?.contexts?.map((c) => c.name) ?? []);
  return names.map((kubeContext) => ({ kubeContext, view: get(clusterViewAtomFamily(kubeContext)) }));
});

const hasAnyClusterUpdateAtom = atom((get) => get(allClusterViewsAtom).some(({ view }) => view.updateAvailable));

// --- Side-effect components mounted by the provider

function CLIFetcher() {
  const [persisted, setPersisted] = useAtom(cliPersistedAtom);
  const cacheValid = isCLICacheValid(persisted);

  const { data } = useQuery(CLI_LATEST_VERSION, {
    skip: !appConfig.cliVersion || cacheValid,
    fetchPolicy: 'network-only',
  });

  useEffect(() => {
    const version = data?.cliLatestVersion;
    if (!version) return;
    setPersisted((prev) => ({ ...prev, latestVersion: version, fetchedAt: Date.now() }));
  }, [data, setPersisted]);

  return null;
}

function ClusterVersionFetcher({ kubeContext }: { kubeContext: string }) {
  const [persisted, setPersisted] = useAtom(clusterPersistedAtomFamily(kubeContext));
  const cacheValid = isClusterCacheValid(persisted);

  const { data, error, loading } = useQuery(CLUSTER_VERSION_STATUS, {
    skip: !kubeContext || cacheValid,
    variables: { kubeContext },
    fetchPolicy: 'network-only',
  });

  useEffect(() => {
    if (!kubeContext || cacheValid) return;
    if (loading || error) return;
    if (data === undefined) return;

    const result = data.clusterVersionStatus;
    setPersisted((prev) => ({
      ...prev,
      currentVersion: result?.currentVersion ?? undefined,
      latestVersion: result?.latestVersion ?? undefined,
      fetchedAt: Date.now(),
    }));
  }, [data, error, loading, kubeContext, cacheValid, setPersisted]);

  return null;
}

/**
 * Mounts the kubeconfig subscriber, the CLI fetcher, and one cluster fetcher per kubeContext.
 * All shared state lives in atoms; this component only runs side effects.
 */
export function UpdateNotificationProvider({ children }: React.PropsWithChildren) {
  const isDesktop = appConfig.environment === 'desktop';
  const setReady = useSetAtom(readyAtom);
  const { data } = useKubeConfig();
  const kubeContexts = isDesktop ? (data?.contexts?.map((c) => c.name) ?? []) : [''];

  // Delay showing the CLI banner so it does not flash on cold load.
  useEffect(() => {
    const timer = setTimeout(() => setReady(true), SHOW_DELAY_MS);
    return () => clearTimeout(timer);
  }, [setReady]);

  return (
    <>
      {isDesktop && <CLIFetcher />}
      {kubeContexts.map((ctx) => (
        <ClusterVersionFetcher key={ctx || '__default__'} kubeContext={ctx} />
      ))}
      {children}
    </>
  );
}

// --- Public hooks

function buildDismissHandlers<T extends UpdateState>(
  setPersisted: (updater: (prev: T) => T) => void,
  latestVersion: string | null,
): { dismiss: () => void; dontRemindMe: () => void } {
  return {
    dismiss: () => setPersisted((prev) => ({ ...prev, dismissedAt: Date.now() })),
    dontRemindMe: () =>
      setPersisted((prev) => {
        const skipped = prev.skippedVersions ?? [];
        const next = latestVersion && !skipped.includes(latestVersion) ? [...skipped, latestVersion] : skipped;
        return { ...prev, skippedVersions: next, dismissedAt: Date.now() };
      }),
  };
}

export function useCLIUpdateNotification(): UpdateNotificationState {
  const view = useAtomValue(cliViewAtom);
  const setPersisted = useSetAtom(cliPersistedAtom);
  return useMemo(() => ({ ...view, ...buildDismissHandlers(setPersisted, view.latestVersion) }), [view, setPersisted]);
}

export function useClusterUpdateNotification(kubeContext: string): ClusterUpdateNotificationState {
  const view = useAtomValue(clusterViewAtomFamily(kubeContext));
  const setPersisted = useSetAtom(clusterPersistedAtomFamily(kubeContext));
  return useMemo(() => ({ ...view, ...buildDismissHandlers(setPersisted, view.latestVersion) }), [view, setPersisted]);
}

export function useHasAnyClusterUpdate(): boolean {
  return useAtomValue(hasAnyClusterUpdateAtom);
}

export function useAllClusterUpdateViews(): { kubeContext: string; view: ClusterView }[] {
  return useAtomValue(allClusterViewsAtom);
}
