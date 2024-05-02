// Copyright 2024 Andres Morey
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
  createHttpLink,
  split,
  from,
} from '@apollo/client';
import type { HttpOptions } from '@apollo/client';
import { onError } from '@apollo/client/link/error';
import { RetryLink } from '@apollo/client/link/retry';
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { getMainDefinition } from '@apollo/client/utilities';
import { ClientOptions, createClient } from 'graphql-ws';
import toast from 'react-hot-toast';

import generatedIntrospection from '@/lib/graphql/__generated__/introspection-result.json';
import { getBasename, getCSRFToken, joinPaths } from './lib/helpers';

const graphqlEndpoint = (new URL(joinPaths(getBasename(), '/graphql'), window.location.origin)).toString();

// http client options
const httpClientOptions: HttpOptions = {
  uri: graphqlEndpoint,
};

// init websocket client
const wsClientOptions: ClientOptions = {
  url: graphqlEndpoint.replace(/^(http)/, 'ws'),
  connectionAckWaitTimeout: 3000,
  connectionParams: async () => ({
    authorization: `${await getCSRFToken()}`,
  }),
  keepAlive: 3000,
  retryAttempts: Infinity,
  shouldRetry: () => true,
  retryWait: () => new Promise((resolve) => {
    setTimeout(resolve, 3000);
  }),
};

export const wsClient = createClient(wsClientOptions);

// init links
const httpLink = createHttpLink(httpClientOptions);

const wsLink = new GraphQLWsLink(wsClient);

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
    retryIf: (error) => {
      console.log(error);
      return true;
    },
  },
});

const splitLink = split(
  ({ query }) => {
    const definition = getMainDefinition(query);
    return definition.kind === 'OperationDefinition' && definition.operation === 'subscription';
  },
  wsLink,
  from([errorLink, retryLink, httpLink]),
);

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

// pagination helper
function k8sPagination() {
  return {
    keyArgs: ['namespace', 'options', ['labelSelector'], '@connection', ['key']],
    merge(existing: any, incoming: any, x: any) {
      // first call
      if (existing === undefined) return incoming;

      // merge if incoming is called with continue arg from existing
      if (x.args.options.continue && x.args.options.continue === existing.metadata.continue) {
        const mergedObj = { ...existing };
        mergedObj.metadata = incoming.metadata;
        mergedObj.items = [...existing.items, ...incoming.items];
        return mergedObj as typeof incoming;
      }

      // otherwise take incoming
      return incoming;
    },
  };
}

// define CustomCache
export class CustomCache extends InMemoryCache {
  constructor() {
    super({
      possibleTypes: generatedIntrospection.possibleTypes,
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

// init client
const client = new ApolloClient({
  cache: new CustomCache(),
  link: splitLink,
  name: 'kubetail',
  version: '0.1.0',
});

export default client;
