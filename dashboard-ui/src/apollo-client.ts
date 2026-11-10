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

import { ApolloClient, ApolloLink, HttpLink, InMemoryCache, Observable } from '@apollo/client';
import type { FieldPolicy } from '@apollo/client/cache';
import { CombinedGraphQLErrors, CombinedProtocolErrors } from '@apollo/client/errors';
import { ErrorLink } from '@apollo/client/link/error';
import { RetryLink } from '@apollo/client/link/retry';
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { getOperationAST, OperationTypeNode } from 'graphql';
import { ClientOptions, createClient } from 'graphql-ws';
import { toast } from 'react-hot-toast';

import appConfig from '@/app-config';
import { waitForCsrfToken, getCsrfToken, resetCsrfToken } from '@/lib/auth';
import clusterAPI from '@/lib/graphql/cluster-api/__generated__/introspection-result.json';
import dashboard from '@/lib/graphql/dashboard/__generated__/introspection-result.json';
import { clusterAPIProxyPath, getBasename, joinPaths, sleep } from '@/lib/util';

const HEALTHZ_RETRY_DELAY_MS = 3000;
const HEALTHZ_TIMEOUT_MS = 2000;
const WS_RETRY_BACKOFF_MS = 1000;
const WS_RETRY_BACKOFF_JITTER_MS = 1000;

/**
 * Helper methods
 */

export const waitUntilVisible = (): Promise<void> => {
  if (document.visibilityState === 'visible') return Promise.resolve();
  return new Promise<void>((resolve) => {
    const handler = () => {
      if (document.visibilityState === 'visible') {
        document.removeEventListener('visibilitychange', handler);
        resolve();
      }
    };
    document.addEventListener('visibilitychange', handler);
  });
};

export const waitUntilOnline = (): Promise<void> => {
  if (navigator.onLine) return Promise.resolve();
  return new Promise<void>((resolve) => {
    window.addEventListener('online', () => resolve(), { once: true });
  });
};

const createRetryWait = (basepath: string) => {
  const healthzUrl = new URL(joinPaths(basepath, 'healthz'), window.location.origin).toString();

  // Retry HTTP health endpoint every three seconds until healthy
  return async () => {
    let healthy = false;
    while (!healthy) {
      // oxlint-disable-next-line no-await-in-loop -- Each retry waits for the one before it.
      await waitUntilVisible();

      // oxlint-disable-next-line no-await-in-loop -- Each retry waits for the one before it.
      await waitUntilOnline();

      try {
        // oxlint-disable-next-line no-await-in-loop -- Each retry waits for the one before it.
        const response = await fetch(healthzUrl, { signal: AbortSignal.timeout(HEALTHZ_TIMEOUT_MS) });
        if (response.ok) healthy = true;
      } catch {
        // ignore
      }

      if (!healthy) {
        // oxlint-disable-next-line no-await-in-loop -- Each retry waits for the one before it.
        await sleep(HEALTHZ_RETRY_DELAY_MS);
      }
    }

    // Always back off before reconnecting, even when healthy
    const backoff = WS_RETRY_BACKOFF_MS + Math.random() * WS_RETRY_BACKOFF_JITTER_MS; // 3–5s jitter
    await sleep(backoff);
  };
};

/**
 * Shared items
 */

const basename = getBasename();

const wsClientOptions = (basepath: string): ClientOptions => ({
  url: '',
  lazy: true,
  connectionAckWaitTimeout: 3000,
  keepAlive: 3000,
  retryAttempts: Infinity,
  shouldRetry: () => true,
  retryWait: createRetryWait(basepath),
  connectionParams: async () => {
    await waitForCsrfToken();
    return { csrfToken: getCsrfToken() };
  },
});

const errorLink = new ErrorLink(({ error }) => {
  if (CombinedGraphQLErrors.is(error)) {
    error.errors.forEach((gqlError) => {
      const msg = `[GraphQL Error] ${gqlError.message}`;
      toast(msg, { id: `${gqlError.path?.join('.')}` });
    });
  } else if (CombinedProtocolErrors.is(error)) {
    const msg = `[Protocol Error] ${error.message}`;
    console.error(msg);
  } else {
    const msg = `[Network Error] ${error.message}`;
    console.error(msg);
  }
});

const csrfLink = new ApolloLink((operation, forward) => {
  const setHeader = (tok: string) => {
    operation.setContext(({ headers = {} }: { headers?: Record<string, string> }) => ({
      headers: { ...headers, 'X-CSRF-Token': tok },
    }));
  };

  const token = getCsrfToken();
  if (token) {
    setHeader(token);
    return forward(operation);
  }

  // No token yet — wait for the session fetch before sending the request so
  // the first GraphQL POST reaches the server with the CSRF header already set.
  return new Observable((subscriber) => {
    let cancelled = false;
    let innerSub: { unsubscribe(): void } | null = null;

    waitForCsrfToken()
      .then(() => {
        if (cancelled) return;
        const tok = getCsrfToken();
        if (tok) setHeader(tok);
        innerSub = forward(operation).subscribe(subscriber);
      })
      .catch((err: unknown) => {
        if (!cancelled) subscriber.error(err);
      });

    return () => {
      cancelled = true;
      innerSub?.unsubscribe();
    };
  });
});

const retryLink = new RetryLink({
  delay: {
    initial: 1000,
    max: 30000,
    jitter: false,
  },
  attempts: {
    max: Infinity,
    retryIf: (error, operation) => {
      if ((error as { statusCode?: number })?.statusCode === 403) {
        // Session key changed (e.g. server restart) — wipe the cached token so
        // csrfLink fetches a fresh one before the next attempt.
        resetCsrfToken();
        return true;
      }
      const msg = `[NetworkError] ${error.message} (${operation.operationName})`;
      toast(msg, { id: `${error.name}|${operation.operationName}` });
      return true;
    },
  },
});

const createLink = (basepath: string) => {
  const uri = new URL(joinPaths(basepath, 'graphql'), window.location.origin).toString();

  // Create http link
  const httpLink = new HttpLink({ uri });

  // Create wsClient
  const wsClient = createClient({
    ...wsClientOptions(basepath),
    url: uri.replace(/^(http)/, 'ws'),
  });

  // Create websocket link
  const wsLink = new GraphQLWsLink(wsClient);

  // Combine using split link
  const link = ApolloLink.split(
    ({ query }) => {
      const op = getOperationAST(query);
      return op?.operation === OperationTypeNode.SUBSCRIPTION;
    },
    wsLink,
    ApolloLink.from([errorLink, retryLink, csrfLink, httpLink]),
  );

  return { link, wsClient };
};

/**
 * Dashboard client
 */

const bigIntField = {
  read(value?: string | null): bigint | undefined | null {
    if (value === undefined || value === null) return value;
    return BigInt(value);
  },
};

const dateField = {
  read(value?: string | null): Date | undefined | null {
    if (value === undefined || value === null) return value;
    return new Date(value);
  },
};

type K8sList = {
  metadata: { continue: string };
  items: unknown[];
};

export function k8sPagination() {
  return {
    keyArgs: ['kubeContext', 'namespace', 'options', ['labelSelector'], '@connection', ['key']],
    merge(existing: K8sList | undefined, incoming: K8sList, { args }: { args: Record<string, unknown> | null }) {
      // first call
      if (existing === undefined) return incoming;

      const continueArg = (args?.options as { continue?: string } | null | undefined)?.continue;

      // refetch call
      if (continueArg === '') return incoming;

      // merge if incoming is called with continue arg from existing
      if (continueArg && continueArg === existing.metadata.continue) {
        return {
          ...existing,
          metadata: incoming.metadata,
          items: [...existing.items, ...incoming.items],
        };
      }

      // otherwise take existing
      return existing;
    },
  } satisfies FieldPolicy<K8sList>;
}

export class DashboardCustomCache extends InMemoryCache {
  constructor() {
    super({
      possibleTypes: dashboard.possibleTypes,
      typePolicies: {
        BatchV1CronJobStatus: {
          fields: {
            lastScheduleTime: dateField,
            lastSuccessfulTime: dateField,
          },
        },
        MetaV1ListMeta: {
          fields: {
            remainingItemCount: bigIntField,
          },
        },
        MetaV1ObjectMeta: {
          fields: {
            creationTimestamp: dateField,
          },
        },
        Query: {
          fields: {
            appsV1DaemonSetsList: k8sPagination(),
            appsV1DeploymentsList: k8sPagination(),
            appsV1ReplicaSetsList: k8sPagination(),
            appsV1StatefulSetsList: k8sPagination(),
            batchV1CronJobsList: k8sPagination(),
            batchV1JobsList: k8sPagination(),
            coreV1NamespacesList: k8sPagination(),
            coreV1NodesList: k8sPagination(),
            coreV1PodsList: k8sPagination(),
          },
        },
        Object: {
          fields: {
            metadata: {
              merge: true,
            },
          },
        },
      },
    });
  }
}

const { link: dashboardLink, wsClient: dashboardWSClient } = createLink(basename);

export { dashboardWSClient };

export const dashboardClient = new ApolloClient({
  cache: new DashboardCustomCache(),
  link: dashboardLink,
  queryDeduplication: false,
});

/**
 * Cluster API client
 */

const clusterAPIClientCache = new Map<string, ApolloClient>();

export class ClusterAPICustomCache extends InMemoryCache {
  constructor() {
    super({
      possibleTypes: clusterAPI.possibleTypes,
      typePolicies: {
        LogMetadataFileInfo: {
          fields: {
            lastModifiedAt: dateField,
          },
        },
      },
    });
  }
}

export const getClusterAPIClient = (kubeContext: string) => {
  let client = clusterAPIClientCache.get(kubeContext);

  if (!client) {
    const basepath = clusterAPIProxyPath({
      basename,
      environment: appConfig.environment,
      kubeContext,
    });

    const { link } = createLink(basepath);

    client = new ApolloClient({
      cache: new ClusterAPICustomCache(),
      link,
      queryDeduplication: false,
    });

    clusterAPIClientCache.set(kubeContext, client);
  }

  return client;
};
