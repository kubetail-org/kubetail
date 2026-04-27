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
import { CombinedGraphQLErrors, CombinedProtocolErrors } from '@apollo/client/errors';
import { ErrorLink } from '@apollo/client/link/error';
import { RetryLink } from '@apollo/client/link/retry';
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { ClientOptions, createClient } from 'graphql-ws';
import { getOperationAST, OperationTypeNode } from 'graphql';
import toast from 'react-hot-toast';

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
      // eslint-disable-next-line no-await-in-loop
      await waitUntilVisible();

      // eslint-disable-next-line no-await-in-loop
      await waitUntilOnline();

      try {
        // eslint-disable-next-line no-await-in-loop
        const response = await fetch(healthzUrl, { signal: AbortSignal.timeout(HEALTHZ_TIMEOUT_MS) });
        if (response.ok) healthy = true;
      } catch {
        // ignore
      }

      if (!healthy) {
        // eslint-disable-next-line no-await-in-loop
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
      .catch((err) => {
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

export function k8sPagination() {
  return {
    keyArgs: ['kubeContext', 'namespace', 'options', ['labelSelector'], '@connection', ['key']],
    merge(existing: any, incoming: any, x: any) {
      // first call
      if (existing === undefined) return incoming;

      // refetch call
      if (x.args.options?.continue === '') return incoming;

      // merge if incoming is called with continue arg from existing
      if (x.args.options.continue && x.args.options.continue === existing.metadata.continue) {
        const mergedObj = { ...existing };
        mergedObj.metadata = incoming.metadata;
        mergedObj.items = [...existing.items, ...incoming.items];
        return mergedObj;
      }

      // otherwise take existing
      return existing;
    },
  };
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
            clusterAPIServicesList: k8sPagination(),
            coreV1NamespacesList: k8sPagination(),
            coreV1PodsList: k8sPagination(),
            podLogQuery: {
              merge: false,
            },
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

type ClusterAPIContext = {
  kubeContext: string;
  namespace: string;
  serviceName: string;
};

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

export const getClusterAPIClient = (context: ClusterAPIContext) => {
  // Build cache key
  let k = context.kubeContext;
  if (appConfig.environment === 'desktop') {
    k += `::${context.namespace}::${context.serviceName}`;
  }

  // Check cache
  let client = clusterAPIClientCache.get(k);

  if (!client) {
    const basepath = clusterAPIProxyPath({
      basename,
      environment: appConfig.environment,
      kubeContext: context.kubeContext,
    });

    const { link } = createLink(basepath);

    // Init new client
    client = new ApolloClient({
      cache: new ClusterAPICustomCache(),
      link,
      queryDeduplication: false,
    });

    // Add to cache
    clusterAPIClientCache.set(k, client);
  }

  return client;
};
