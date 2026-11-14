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

import { ApolloCache, ApolloClient, ApolloLink, InMemoryCache, Observable } from '@apollo/client';
import { ApolloProvider } from '@apollo/client/react';
import { act, renderHook, waitFor } from '@testing-library/react';
import type React from 'react';

import { DashboardCustomCache } from '@/apollo-client';
import dashboardIntrospection from '@/lib/graphql/dashboard/__generated__/introspection-result.json';
import {
  HOME_NAMESPACES_LIST_FETCH,
  HOME_NAMESPACES_LIST_WATCH,
  SOURCE_PICKER_PODS_COUNT_FETCH,
  SOURCE_PICKER_PODS_COUNT_WATCH,
} from '@/lib/graphql/dashboard/ops';
import { withAppConfig } from '@/test-utils';

import {
  useCounterQueryWithSubscription,
  useListQueryWithSubscription,
  useRafThrottle,
  useWorkloadCounter,
} from './hooks';
import { WorkloadKind } from './workload';

// vitest.setup.ts mocks useIsClusterAPIEnabled; this is the real one.
const { useIsClusterAPIEnabled } = await vi.importActual<typeof import('./hooks')>('./hooks');

/**
 * A link that holds every operation open until the test answers it.
 */

type LinkObserver = {
  next: (value: ApolloLink.Result) => void;
  error: (err: unknown) => void;
  complete: () => void;
};

type PendingRequest = {
  operationName: string | undefined;
  variables: Record<string, unknown>;
  observer: LinkObserver;
  taken: boolean;
};

function createTestClient(cache: ApolloCache = new DashboardCustomCache()) {
  const requests: PendingRequest[] = [];

  const link = new ApolloLink(
    (operation) =>
      new Observable<ApolloLink.Result>((observer) => {
        requests.push({
          operationName: operation.operationName,
          variables: operation.variables,
          observer,
          taken: false,
        });
      }),
  );

  const client = new ApolloClient({ cache, link });

  const wrapper = ({ children }: React.PropsWithChildren) => (
    <ApolloProvider client={client}>{children}</ApolloProvider>
  );

  const findRequest = (operationName: string) => {
    const req = requests.find((r) => r.operationName === operationName && !r.taken);
    if (req) req.taken = true;
    return req;
  };

  // Waits for the next operation of that name that the test has not taken yet.
  const nextRequest = async (operationName: string) => {
    let req: PendingRequest | undefined;
    await waitFor(() => {
      req = findRequest(operationName);
      expect(req).toBeDefined();
    });
    return req!;
  };

  return { client, wrapper, requests, findRequest, nextRequest };
}

const respond = (req: PendingRequest, data: unknown) =>
  act(() => {
    req.observer.next({ data: data as Record<string, unknown> });
    req.observer.complete();
  });

const emit = (req: PendingRequest, data: unknown) =>
  act(() => {
    req.observer.next({ data: data as Record<string, unknown> });
  });

const watchExpiredError = () =>
  Object.assign(new Error('expired'), {
    graphQLErrors: [
      { message: 'expired', extensions: { code: 'KUBETAIL_WATCH_ERROR', reason: 'Expired', status: 'Failure' } },
    ],
  });

/**
 * useRafThrottle
 */

describe('useRafThrottle', () => {
  it('runs the callback at most once per frame, with the first call’s arguments', async () => {
    const fn = vi.fn();
    const { result } = renderHook(() => useRafThrottle(fn));

    result.current(1);
    result.current(2);
    expect(fn).not.toHaveBeenCalled();

    await waitFor(() => expect(fn).toHaveBeenCalledTimes(1));
    expect(fn).toHaveBeenCalledWith(1);

    result.current(3);
    await waitFor(() => expect(fn).toHaveBeenCalledTimes(2));
    expect(fn).toHaveBeenLastCalledWith(3);
  });

  it('cancels a pending frame on unmount', () => {
    const requestFrame = vi.spyOn(globalThis, 'requestAnimationFrame');
    const cancelFrame = vi.spyOn(globalThis, 'cancelAnimationFrame');
    const { result, unmount } = renderHook(() => useRafThrottle(vi.fn()));

    result.current();
    const frameID = requestFrame.mock.results[0].value as number;
    unmount();

    expect(cancelFrame).toHaveBeenCalledWith(frameID);
  });
});

/**
 * useListQueryWithSubscription
 */

const namespace = (name: string, resourceVersion: string, deletionTimestamp: string | null = null) => ({
  __typename: 'CoreV1Namespace',
  id: `id-${name}`,
  metadata: {
    __typename: 'MetaV1ObjectMeta',
    namespace: '',
    name,
    uid: `uid-${name}`,
    creationTimestamp: '2024-01-01T00:00:00Z',
    deletionTimestamp,
    resourceVersion,
    ownerReferences: [],
  },
});

const namespacesList = (items: ReturnType<typeof namespace>[], continueVal: string, resourceVersion: string) => ({
  coreV1NamespacesList: {
    __typename: 'CoreV1NamespaceList',
    metadata: { __typename: 'MetaV1ListMeta', continue: continueVal, resourceVersion },
    items,
  },
});

const namespaceEvent = (type: string, object: ReturnType<typeof namespace> | null) => ({
  coreV1NamespacesWatch: { __typename: 'CoreV1NamespacesWatchEvent', type, object },
});

describe('useListQueryWithSubscription', () => {
  const listArgs = (skip?: boolean) => ({
    query: HOME_NAMESPACES_LIST_FETCH,
    subscription: HOME_NAMESPACES_LIST_WATCH,
    queryDataKey: 'coreV1NamespacesList' as const,
    subscriptionDataKey: 'coreV1NamespacesWatch' as const,
    skip,
    variables: { kubeContext: 'ctx' },
  });

  const names = (data: unknown) =>
    (data as ReturnType<typeof namespacesList>).coreV1NamespacesList.items.map((item) => item.metadata.name);

  // Fetches two pages, then returns the watch the hook opens.
  async function renderFetched(t: ReturnType<typeof createTestClient>) {
    const hook = renderHook(() => useListQueryWithSubscription(listArgs()), { wrapper: t.wrapper });

    expect(hook.result.current.loading).toBe(true);
    expect(hook.result.current.fetching).toBe(true);

    const page1 = await t.nextRequest('HomeNamespacesListFetch');
    expect(page1.variables).toMatchObject({ kubeContext: 'ctx' });
    respond(page1, namespacesList([namespace('bravo', '1')], 'c1', '1'));

    const page2 = await t.nextRequest('HomeNamespacesListFetch');
    expect(page2.variables).toMatchObject({ kubeContext: 'ctx', continue: 'c1' });
    respond(page2, namespacesList([namespace('alpha', '2')], '', '2'));

    await waitFor(() => expect(hook.result.current.fetching).toBe(false));
    expect(names(hook.result.current.data)).toEqual(['bravo', 'alpha']);

    const watch = await t.nextRequest('HomeNamespacesListWatch');
    expect(watch.variables).toMatchObject({ kubeContext: 'ctx', resourceVersion: '2' });

    return { ...hook, watch };
  }

  it('fetches every page, then watches from the last resourceVersion', async () => {
    const t = createTestClient();
    const { result } = await renderFetched(t);

    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBeUndefined();
  });

  it('adds a new item in name order and ignores one already listed', async () => {
    const t = createTestClient();
    const { result, watch } = await renderFetched(t);

    emit(watch, namespaceEvent('ADDED', namespace('charlie', '3')));
    await waitFor(() => expect(names(result.current.data)).toEqual(['alpha', 'bravo', 'charlie']));

    emit(watch, namespaceEvent('ADDED', namespace('alpha', '4')));
    await waitFor(() => expect(result.current.data?.coreV1NamespacesList?.metadata.resourceVersion).toBe('4'));
    expect(names(result.current.data)).toEqual(['alpha', 'bravo', 'charlie']);
  });

  it('keeps the list on a modification and on an event it does not handle', async () => {
    const t = createTestClient();
    const { result, watch } = await renderFetched(t);

    emit(watch, namespaceEvent('MODIFIED', namespace('alpha', '3')));
    await waitFor(() => expect(result.current.data?.coreV1NamespacesList?.metadata.resourceVersion).toBe('3'));
    expect(names(result.current.data)).toEqual(['bravo', 'alpha']);

    emit(watch, namespaceEvent('BOOKMARK', namespace('delta', '4')));
    emit(watch, namespaceEvent('ADDED', null));
    expect(result.current.data?.coreV1NamespacesList?.metadata.resourceVersion).toBe('3');
    expect(names(result.current.data)).toEqual(['bravo', 'alpha']);
  });

  it('removes a deleted item and marks a forced deletion in the cache', async () => {
    const t = createTestClient();
    const { result, watch } = await renderFetched(t);

    emit(watch, namespaceEvent('DELETED', namespace('bravo', '3')));
    await waitFor(() => expect(names(result.current.data)).toEqual(['alpha']));

    const cached = (t.client.cache.extract() as Record<string, { metadata: { deletionTimestamp: unknown } }>)[
      'CoreV1Namespace:id-bravo'
    ];
    expect(cached.metadata.deletionTimestamp).toEqual(expect.any(String));
  });

  it('refetches when the watch expires', async () => {
    const t = createTestClient();
    const { watch } = await renderFetched(t);

    act(() => watch.observer.error(watchExpiredError()));

    const refetch = await t.nextRequest('HomeNamespacesListFetch');
    expect(refetch.variables).toMatchObject({ kubeContext: 'ctx' });
  });

  it('refetches when the variables change, and not when they are only a new object', async () => {
    const t = createTestClient();
    const { rerender } = renderHook(
      ({ kubeContext }) => useListQueryWithSubscription({ ...listArgs(), variables: { kubeContext } }),
      { wrapper: t.wrapper, initialProps: { kubeContext: 'ctx' } },
    );

    respond(await t.nextRequest('HomeNamespacesListFetch'), namespacesList([namespace('alpha', '1')], '', '1'));
    await t.nextRequest('HomeNamespacesListWatch');

    rerender({ kubeContext: 'ctx' });
    rerender({ kubeContext: 'other' });

    const next = await t.nextRequest('HomeNamespacesListFetch');
    expect(next.variables).toMatchObject({ kubeContext: 'other' });
    expect(t.requests.filter((r) => r.operationName === 'HomeNamespacesListFetch')).toHaveLength(2);
  });

  it('sends nothing while skipped', () => {
    const t = createTestClient();
    const { result } = renderHook(() => useListQueryWithSubscription(listArgs(true)), { wrapper: t.wrapper });

    expect(result.current.loading).toBe(false);
    expect(result.current.fetching).toBe(false);
    expect(result.current.data).toBeUndefined();
    expect(t.requests).toHaveLength(0);
  });

  describe('on a query error', () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it('polls until the query succeeds', async () => {
      const t = createTestClient();
      const { result } = renderHook(() => useListQueryWithSubscription(listArgs()), { wrapper: t.wrapper });
      await act(() => vi.advanceTimersByTimeAsync(0));

      const first = t.findRequest('HomeNamespacesListFetch')!;
      act(() => first.observer.error(new Error('boom')));
      await act(() => vi.advanceTimersByTimeAsync(0));
      expect(result.current.error?.message).toBe('boom');

      await act(() => vi.advanceTimersByTimeAsync(5000));
      const retry = t.findRequest('HomeNamespacesListFetch');
      expect(retry).toBeDefined();

      respond(retry!, namespacesList([namespace('alpha', '1')], '', '1'));
      await act(() => vi.advanceTimersByTimeAsync(0));
      expect(result.current.error).toBeUndefined();
      expect(names(result.current.data)).toEqual(['alpha']);
    });
  });
});

/**
 * useCounterQueryWithSubscription
 */

const pod = (name: string, resourceVersion: string) => ({
  __typename: 'CoreV1Pod',
  id: `id-${name}`,
  metadata: { __typename: 'MetaV1ObjectMeta', resourceVersion },
});

const podsCount = (items: ReturnType<typeof pod>[], remainingItemCount: string, resourceVersion: string) => ({
  coreV1PodsList: {
    __typename: 'CoreV1PodList',
    metadata: { __typename: 'MetaV1ListMeta', remainingItemCount, resourceVersion },
    items,
  },
});

const podEvent = (type: string, object: ReturnType<typeof pod> | null) => ({
  coreV1PodsWatch: { __typename: 'CoreV1PodsWatchEvent', type, object },
});

// DashboardCustomCache's k8sPagination() merge keeps the cached list when a write has no
// `continue` argument, as a counter query's never does, so it drops the watch's updates. This
// cache keeps the scalar policy and leaves out the merge, to test the hook's own update.
const counterCache = () =>
  new InMemoryCache({
    possibleTypes: dashboardIntrospection.possibleTypes,
    typePolicies: {
      MetaV1ListMeta: {
        fields: {
          remainingItemCount: { read: (value?: string | bigint | null) => (value == null ? value : BigInt(value)) },
        },
      },
    },
  });

describe('useCounterQueryWithSubscription', () => {
  const counterArgs = {
    query: SOURCE_PICKER_PODS_COUNT_FETCH,
    subscription: SOURCE_PICKER_PODS_COUNT_WATCH,
    queryDataKey: 'coreV1PodsList' as const,
    subscriptionDataKey: 'coreV1PodsWatch' as const,
    variables: { kubeContext: 'ctx', namespace: 'default' },
  };

  async function renderCounted(t: ReturnType<typeof createTestClient>) {
    const hook = renderHook(() => useCounterQueryWithSubscription(counterArgs), { wrapper: t.wrapper });

    expect(hook.result.current.loading).toBe(true);
    expect(hook.result.current.count).toBeUndefined();

    const fetch = await t.nextRequest('SourcePickerPodsCountFetch');
    expect(fetch.variables).toMatchObject({ kubeContext: 'ctx', namespace: 'default' });
    respond(fetch, podsCount([pod('p1', '1')], '4', '10'));

    // one listed item plus the remaining count
    await waitFor(() => expect(hook.result.current.count).toBe(5));

    const watch = await t.nextRequest('SourcePickerPodsCountWatch');
    expect(watch.variables).toMatchObject({ kubeContext: 'ctx', namespace: 'default', resourceVersion: '10' });

    return { ...hook, watch };
  }

  it('counts additions and deletions from the watch', async () => {
    const t = createTestClient(counterCache());
    const { result, watch } = await renderCounted(t);

    emit(watch, podEvent('ADDED', pod('p2', '11')));
    await waitFor(() => expect(result.current.count).toBe(6));

    emit(watch, podEvent('DELETED', pod('p1', '12')));
    await waitFor(() => expect(result.current.count).toBe(5));
  });

  it('ignores a modification and an event without an object', async () => {
    const t = createTestClient(counterCache());
    const { result, watch } = await renderCounted(t);

    emit(watch, podEvent('MODIFIED', pod('p1', '11')));
    emit(watch, podEvent('ADDED', null));

    // a later addition counts from 5, so the two events above changed nothing
    emit(watch, podEvent('ADDED', pod('p2', '12')));
    await waitFor(() => expect(result.current.count).toBe(6));
  });

  it('refetches when the watch expires', async () => {
    const t = createTestClient(counterCache());
    const { watch } = await renderCounted(t);

    act(() => watch.observer.error(watchExpiredError()));

    const refetch = await t.nextRequest('SourcePickerPodsCountFetch');
    expect(refetch.variables).toMatchObject({ kubeContext: 'ctx', namespace: 'default' });
  });

  it('returns the query error and opens no watch', async () => {
    const t = createTestClient(counterCache());
    const { result } = renderHook(() => useCounterQueryWithSubscription(counterArgs), { wrapper: t.wrapper });

    const fetch = await t.nextRequest('SourcePickerPodsCountFetch');
    act(() => fetch.observer.error(new Error('boom')));

    await waitFor(() => expect(result.current.error?.message).toBe('boom'));
    expect(result.current.count).toBeUndefined();
    expect(t.requests.some((r) => r.operationName === 'SourcePickerPodsCountWatch')).toBe(false);
  });
});

/**
 * useWorkloadCounter
 */

const workloadCountQueries = [
  {
    kind: WorkloadKind.CRONJOBS,
    op: 'SourcePickerCronJobsCountFetch',
    key: 'batchV1CronJobsList',
    type: 'BatchV1CronJob',
  },
  {
    kind: WorkloadKind.DAEMONSETS,
    op: 'SourcePickerDaemonSetsCountFetch',
    key: 'appsV1DaemonSetsList',
    type: 'AppsV1DaemonSet',
  },
  {
    kind: WorkloadKind.DEPLOYMENTS,
    op: 'SourcePickerDeploymentsCountFetch',
    key: 'appsV1DeploymentsList',
    type: 'AppsV1Deployment',
  },
  { kind: WorkloadKind.JOBS, op: 'SourcePickerJobsCountFetch', key: 'batchV1JobsList', type: 'BatchV1Job' },
  { kind: WorkloadKind.PODS, op: 'SourcePickerPodsCountFetch', key: 'coreV1PodsList', type: 'CoreV1Pod' },
  {
    kind: WorkloadKind.REPLICASETS,
    op: 'SourcePickerReplicaSetsCountFetch',
    key: 'appsV1ReplicaSetsList',
    type: 'AppsV1ReplicaSet',
  },
  {
    kind: WorkloadKind.STATEFULSETS,
    op: 'SourcePickerStatefulSetsCountFetch',
    key: 'appsV1StatefulSetsList',
    type: 'AppsV1StatefulSet',
  },
];

const waitForAllCountQueries = (t: ReturnType<typeof createTestClient>) =>
  waitFor(() => {
    workloadCountQueries.forEach((q) => {
      expect(t.requests.some((r) => r.operationName === q.op)).toBe(true);
    });
  });

describe('useWorkloadCounter', () => {
  it('counts each workload kind once every query has answered', async () => {
    const t = createTestClient();
    const { result } = renderHook(() => useWorkloadCounter('ctx', 'default'), { wrapper: t.wrapper });

    expect(result.current.loading).toBe(true);
    expect(result.current.counter.size).toBe(0);

    await waitForAllCountQueries(t);
    for (const [i, q] of workloadCountQueries.entries()) {
      const req = t.findRequest(q.op)!;
      expect(req.variables).toMatchObject({ kubeContext: 'ctx', namespace: 'default' });
      respond(req, {
        [q.key]: {
          __typename: `${q.type}List`,
          metadata: { __typename: 'MetaV1ListMeta', remainingItemCount: String(i), resourceVersion: '1' },
          items: [
            {
              __typename: q.type,
              id: `id-${q.kind}`,
              metadata: { __typename: 'MetaV1ObjectMeta', resourceVersion: '1' },
            },
          ],
        },
      });
    }

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBeUndefined();
    workloadCountQueries.forEach((q, i) => {
      expect(result.current.counter.get(q.kind)).toBe(i + 1);
    });
  });

  it('returns the first error and no counts', async () => {
    const t = createTestClient();
    const { result } = renderHook(() => useWorkloadCounter('ctx'), { wrapper: t.wrapper });

    await waitForAllCountQueries(t);
    for (const q of workloadCountQueries) {
      const req = t.findRequest(q.op)!;
      act(() => req.observer.error(new Error(`${q.kind} failed`)));
    }

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error?.error?.message).toBe('cronjobs failed');
    expect(result.current.counter.size).toBe(0);
  });
});

/**
 * useIsClusterAPIEnabled
 */

describe('useIsClusterAPIEnabled', () => {
  const healthz = (status: string) => ({
    clusterAPIHealthzWatch: {
      __typename: 'HealthCheckResponse',
      status,
      message: null,
      timestamp: '2024-01-01T00:00:00Z',
    },
  });

  it('is undefined until the health check answers', async () => {
    const t = createTestClient();
    const { result } = renderHook(() => useIsClusterAPIEnabled('ctx'), { wrapper: t.wrapper });

    expect(result.current).toBeUndefined();
    const watch = await t.nextRequest('ServerStatusClusterAPIHealthzWatch');
    expect(watch.variables).toEqual({ kubeContext: 'ctx' });
  });

  it.each([
    ['SUCCESS', true],
    ['FAILURE', true],
    ['UNKNOWN', true],
    ['NOTFOUND', false],
  ])('is %s → %s', async (status, expected) => {
    const t = createTestClient();
    const { result } = renderHook(() => useIsClusterAPIEnabled(null), { wrapper: t.wrapper });

    const watch = await t.nextRequest('ServerStatusClusterAPIHealthzWatch');
    expect(watch.variables).toEqual({ kubeContext: '' });

    emit(watch, healthz(status));
    await waitFor(() => expect(result.current).toBe(expected));
  });

  it('is undefined again while the Cluster API is pending', async () => {
    const t = createTestClient();
    const { result } = renderHook(() => useIsClusterAPIEnabled('ctx'), { wrapper: t.wrapper });

    const watch = await t.nextRequest('ServerStatusClusterAPIHealthzWatch');
    emit(watch, healthz('SUCCESS'));
    await waitFor(() => expect(result.current).toBe(true));

    emit(watch, healthz('PENDING'));
    await waitFor(() => expect(result.current).toBeUndefined());
  });

  it('is false when the Cluster API is not available', async () => {
    const t = createTestClient();
    const { result } = renderHook(() => useIsClusterAPIEnabled('ctx'), { wrapper: t.wrapper });

    const watch = await t.nextRequest('ServerStatusClusterAPIHealthzWatch');
    act(() => watch.observer.error(new Error('not available')));

    await waitFor(() => expect(result.current).toBe(false));
  });

  it('returns the configured value in the cluster environment', async () => {
    await withAppConfig({ environment: 'cluster', clusterAPIEnabled: false }, () => {
      const t = createTestClient();
      const { result } = renderHook(() => useIsClusterAPIEnabled('ctx'), { wrapper: t.wrapper });

      expect(result.current).toBe(false);
      expect(t.requests).toHaveLength(0);
    });
  });
});
