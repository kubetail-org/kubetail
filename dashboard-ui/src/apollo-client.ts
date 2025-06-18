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

import {
  ApolloClient,
  InMemoryCache,
  NormalizedCacheObject,
  createHttpLink,
  split,
  from,
} from '@apollo/client';
import type { Operation } from '@apollo/client/link/core';
import { onError } from '@apollo/client/link/error';
import { RetryLink } from '@apollo/client/link/retry';
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { getMainDefinition } from '@apollo/client/utilities';
import { ClientOptions, createClient } from 'graphql-ws';
import toast from 'react-hot-toast';

import appConfig from '@/app-config';
import clusterAPI from '@/lib/graphql/cluster-api/__generated__/introspection-result.json';
import dashboard from '@/lib/graphql/dashboard/__generated__/introspection-result.json';
import { getBasename, getCSRFToken, joinPaths } from '@/lib/util';

/**
 * Shared items
 */

const basename = getBasename();

const wsClientOptions: ClientOptions = {
  url: '',
  connectionAckWaitTimeout: 3000,
  keepAlive: 3000,
  retryAttempts: Infinity,
  shouldRetry: () => true,
  retryWait: () => new Promise((resolve) => {
    setTimeout(resolve, 3000);
  }),
};

const errorLink = onError(({ graphQLErrors, networkError }) => {
  if (networkError) {
    const msg = `[Network Error] ${networkError.message}`;
    console.error(msg);
    return;
  }

  if (graphQLErrors) {
    graphQLErrors.forEach(({ message, path }) => {
      const msg = `[GraphQL Error] ${message}`;
      toast(msg, { id: `${path}` });
    });
  }
});

const retryLink = new RetryLink({
  delay: {
    initial: 1000,
    max: 30000,
    jitter: false,
  },
  attempts: {
    max: Infinity,
    retryIf: (error: any, operation: Operation) => {
      const msg = `[NetworkError] ${error.message} (${operation.operationName})`;
      toast(msg, { id: `${error.name}|${operation.operationName}` });
      return true;
    },
  },
});

const createLink = (basepath: string) => {
  const uri = new URL(joinPaths(basepath, 'graphql'), window.location.origin).toString();

  // Create http link
  const httpLink = createHttpLink({ uri });

  // Create wsClient
  const wsClient = createClient({
    ...wsClientOptions,
    url: uri.replace(/^(http)/, 'ws'),
    connectionParams: async () => ({
      authorization: `${await getCSRFToken(basepath)}`,
    }),
  });

  // Create websocket link
  const wsLink = new GraphQLWsLink(wsClient);

  // Combine using split link
  const link = split(
    ({ query }) => {
      const definition = getMainDefinition(query);
      return definition.kind === 'OperationDefinition' && definition.operation === 'subscription';
    },
    wsLink,
    from([errorLink, retryLink, httpLink]),
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
        return mergedObj as typeof incoming;
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

export const dashboardClient = new ApolloClient({
  cache: new DashboardCustomCache(),
  link: dashboardLink,
  name: 'kubetail/dashboard',
  version: '0.1.0',
});

export { dashboardWSClient };

/**
 * Cluster API client
 */

type ClusterAPIContext = {
  kubeContext: string;
  namespace: string;
  serviceName: string;
};

const clusterAPIClientCache = new Map<string, ApolloClient<NormalizedCacheObject>>();

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
    // Build basepath
    let basepath = joinPaths(basename, 'cluster-api-proxy');
    if (appConfig.environment === 'desktop') {
      basepath = joinPaths(basepath, context.kubeContext, context.namespace, context.serviceName);
    }

    const { link } = createLink(basepath);

    // Init new client
    client = new ApolloClient({
      cache: new ClusterAPICustomCache(),
      link,
      name: 'kubetail/dashboard',
      version: '0.1.0',
    });

    // Add to cache
    clusterAPIClientCache.set(k, client);
  }

  return client;
};
