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

import { useApolloClient, useQuery } from '@apollo/client';
import type { TypedDocumentNode, OperationVariables } from '@apollo/client';
import distinctColors from 'distinct-colors';
import { useEffect, useRef, useState } from 'react';

import * as ops from '@/lib/graphql/ops';

type GenericListFragment = {
  metadata: {
    continue: string;
    resourceVersion: string;
  };
  items: {
    metadata: {
      name: string;
      uid: string;
      deletionTimestamp: null | string;
    };
  }[];
};

type GenericCounterFragment = {
  metadata: {
    remainingItemCount: bigint;
    resourceVersion: string;
  };
  items: {
    metadata: {
      resourceVersion: string;
    }
  }[];
};

type GenericWatchEventFragment = {
  type: string;
  object: any;
};

type CustomError = Error & {
  graphQLErrors?: {
    message: string;
    extensions: {
      code: string;
      reason: string;
      status: string;
    };
  }[];
};

const RETRY_TIMEOUT = 5000;

/**
 * Is watch expired error?
 */

function isWatchExpiredError(err: Error): boolean {
  const { graphQLErrors } = err as CustomError;
  if (graphQLErrors && graphQLErrors.length) {
    const gqlErr = graphQLErrors[0];
    return (gqlErr.extensions?.code === 'KUBETAIL_WATCH_ERROR' && gqlErr.extensions?.reason === 'Expired');
  }
  return false;
}

/**
 * Retries query until hook is unmounted
 */

function useRetryOnError() {
  const isMountedRef = useRef(true);

  useEffect(() => {
    isMountedRef.current = true;
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  return (retryFn: () => Promise<any>) => {
    const timeout = setInterval(async () => {
      // check isMounted
      if (!isMountedRef.current) {
        clearInterval(timeout);
        return;
      }

      // execute query
      try {
        await retryFn();
        clearInterval(timeout);
      } catch (e) {
        // do nothing
      }
    }, RETRY_TIMEOUT);
  };
}

/**
 * Get-style query with subscription hook
 */

interface GetQueryWithSubscriptionTQVariables {
  namespace: string;
  name: string;
}

interface GetQueryWithSubscriptionTSVariables {
  namespace: string;
  fieldSelector: string;
}

interface GetQueryWithSubscriptionArgs<TQData, TQVariables, TSData, TSVariables> {
  query: TypedDocumentNode<TQData, TQVariables>;
  subscription: TypedDocumentNode<TSData, TSVariables>;
  queryDataKey: keyof TQData;
  subscriptionDataKey: keyof TSData;
  skip?: boolean;
  variables: TQVariables;
}

export function useGetQueryWithSubscription<
  TQData = any,
  TQVariables extends GetQueryWithSubscriptionTQVariables = GetQueryWithSubscriptionTQVariables,
  TSData = any,
  TSVariables extends GetQueryWithSubscriptionTSVariables = GetQueryWithSubscriptionTSVariables,
>(args: GetQueryWithSubscriptionArgs<TQData, TQVariables, TSData, TSVariables>) {
  const { name, namespace } = args.variables;

  const retryOnError = useRetryOnError();

  // get workload object
  const { loading, error, data, subscribeToMore, refetch } = useQuery(args.query, {
    skip: args.skip,
    variables: args.variables,
    onError: () => {
      retryOnError(refetch);
    },
  });

  // subscribe to changes
  useEffect(
    () => subscribeToMore({
      document: args.subscription,
      variables: { namespace, fieldSelector: `metadata.name=${name}` } as any,
      updateQuery: (prev, { subscriptionData }) => {
        const ev = subscriptionData.data[args.subscriptionDataKey] as GenericWatchEventFragment;
        if (ev?.type === 'ADDED' && ev.object) return { [args.queryDataKey]: ev.object } as TQData;
        return prev;
      },
      onError: (err) => {
        if (isWatchExpiredError(err)) refetch();
      },
    }),
    [subscribeToMore],
  );

  return { loading, error, data };
}

/**
 * List-style query with subscription hook
 */

interface ListQueryWithSubscriptionArgs<TQData, TQVariables, TSData, TSVariables> {
  query: TypedDocumentNode<TQData, TQVariables>;
  subscription: TypedDocumentNode<TSData, TSVariables>;
  queryDataKey: keyof TQData;
  subscriptionDataKey: keyof TSData;
  skip?: boolean;
  variables?: TQVariables;
}

export function useListQueryWithSubscription<
  TQData = any,
  TQVariables extends OperationVariables = OperationVariables,
  TSData = any,
  TSVariables extends OperationVariables = OperationVariables,
>(args: ListQueryWithSubscriptionArgs<TQData, TQVariables, TSData, TSVariables>) {
  const client = useApolloClient();
  const retryOnError = useRetryOnError();

  // initial query
  const { loading, error, data, fetchMore, subscribeToMore, refetch } = useQuery(args.query, {
    skip: args.skip,
    variables: args.variables,
    onError: () => {
      retryOnError(refetch);
    },
  });

  // TODO: tighten `any`
  const respData = data ? data[args.queryDataKey] as GenericListFragment : null;

  // fetch rest
  const fetchMoreRef = useRef(new Set<string>([]));
  const continueVal = respData?.metadata.continue;
  useEffect(() => {
    if (continueVal && !fetchMoreRef.current.has(continueVal)) {
      fetchMoreRef.current.add(continueVal);
      fetchMore({ variables: { ...args.variables, continue: continueVal } });
    }
  }, [continueVal]);

  // subscribe to changes
  useEffect(() => {
    // wait for all data to get fetched
    if (loading || continueVal) return;

    const resourceVersion = respData?.metadata.resourceVersion || '';

    // add `resourceVersion`
    const variables = { ...args.variables, resourceVersion } as any;

    return subscribeToMore({
      document: args.subscription,
      variables,
      updateQuery: (prev, { subscriptionData }) => {
        const ev = subscriptionData.data[args.subscriptionDataKey] as GenericWatchEventFragment;

        if (!ev?.type || !ev?.object) return prev;

        // handle forced deletions that don't set `deletionTimestamp`
        if (ev.type === 'DELETED' && ev.object.metadata.deletionTimestamp === null) {
          // update the cache
          client.cache.modify({
            id: client.cache.identify(ev.object),
            fields: {
              metadata: (currMetadata) => ({
                ...currMetadata,
                deletionTimestamp: new Date().toISOString(),
              }),
            },
          });

          // remove from list result
          const newResult = { ...prev[args.queryDataKey] } as GenericListFragment;
          newResult.items = newResult.items.filter((item) => item.metadata.uid !== ev.object.metadata.uid);
          return { [args.queryDataKey]: newResult } as TQData;
        }

        // only handle additions
        if (ev.type !== 'ADDED') return prev;

        // merge
        if (!prev[args.queryDataKey]) return prev;
        const merged = { ...prev[args.queryDataKey] } as GenericListFragment;

        // update resourceVersion
        merged.metadata = { ...merged.metadata };
        merged.metadata.resourceVersion = ev.object.metadata.resourceVersion;

        // add and re-sort item if not already in list
        if (!merged.items.some((item) => item.metadata.uid === ev.object.metadata.uid)) {
          const items = Array.from(merged.items);
          items.push(ev.object);
          items.sort((a, b) => {
            if (!a.metadata.name) return 1;
            if (!b.metadata.name) return -1;
            return a.metadata.name.localeCompare(b.metadata.name);
          });
          merged.items = items;
        }

        return { [args.queryDataKey]: merged } as TQData;
      },
      onError: (err) => {
        if (isWatchExpiredError(err)) refetch();
      },
    });
  }, [subscribeToMore, loading, continueVal]);

  const fetching = Boolean(loading || continueVal);

  return {
    loading, fetching, error, data,
  };
}

/**
 * Counter-style query with subscription hook
 */

interface CounterQueryWithSubscriptionArgs<TQData, TQVariables, TSData, TSVariables> {
  query: TypedDocumentNode<TQData, TQVariables>;
  subscription: TypedDocumentNode<TSData, TSVariables>;
  queryDataKey: keyof TQData;
  subscriptionDataKey: keyof TSData;
  skip?: boolean;
  variables?: TQVariables;
}

export function useCounterQueryWithSubscription<
  TQData = any,
  TQVariables extends OperationVariables = OperationVariables,
  TSData = any,
  TSVariables extends OperationVariables = OperationVariables,
>(args: CounterQueryWithSubscriptionArgs<TQData, TQVariables, TSData, TSVariables>) {
  const retryOnError = useRetryOnError();

  // initial query
  const { loading, error, data, subscribeToMore, refetch } = useQuery(args.query, {
    skip: args.skip,
    variables: args.variables,
    onError: () => {
      retryOnError(refetch);
    },
  });

  // TODO: tighten `any`
  const respData = data ? data[args.queryDataKey] as GenericCounterFragment : null;

  // subscribe to changes
  useEffect(() => {
    // wait for all data to get fetched
    if (loading || error) return;

    const resourceVersion = respData?.metadata.resourceVersion || '';

    // add `resourceVersion`
    const variables = { ...args.variables, resourceVersion } as any;

    return subscribeToMore({
      document: args.subscription,
      variables,
      updateQuery: (prev, { subscriptionData }) => {
        const ev = subscriptionData.data[args.subscriptionDataKey] as GenericWatchEventFragment;

        if (!ev?.type || !ev?.object) return prev;

        // only handle additions and deletions
        if (!['ADDED', 'DELETED'].includes(ev.type)) return prev;

        // merge
        if (!prev[args.queryDataKey]) return prev;
        const merged = { ...prev[args.queryDataKey] } as GenericCounterFragment;

        // update resourceVersion
        merged.metadata.resourceVersion = ev.object.metadata.resourceVersion;

        // update remainingItemsCount
        if (ev.type === 'ADDED') merged.metadata.remainingItemCount += BigInt(1);
        else merged.metadata.remainingItemCount -= BigInt(1);

        return { [args.queryDataKey]: merged } as TQData;
      },
      onError: (err) => {
        if (isWatchExpiredError(err)) refetch();
      },
    });
  }, [subscribeToMore, loading, error]);

  let count: number | undefined;
  if (respData) count = respData.items.length + Number(respData.metadata.remainingItemCount);

  return {
    loading, error, count,
  };
}

/**
 * LogMetadata hook
 */

type LogMetadataHookOptions = {
  onUpdate?: (containerID: string) => void;
}

export function useLogMetadata(options?: LogMetadataHookOptions) {
  const retryOnError = useRetryOnError();

  // initial query
  const { loading, error, data, subscribeToMore, refetch } = useQuery(ops.LOGMETADATA_LIST_FETCH, {
    onError: () => {
      retryOnError(refetch);
    },
  });

  const { onUpdate } = options || {};

  // subscribe to changes
  useEffect(() => {
    // wait for all data to get fetched
    if (loading || error) return;

    return subscribeToMore({
      document: ops.LOGMETADATA_LIST_WATCH,
      updateQuery: (prev, { subscriptionData }) => {
        const ev = subscriptionData.data.logMetadataWatch;

        if (!ev?.type || !ev?.object) return prev;

        if (!prev.logMetadataList) return prev;

        // execute callback
        if (ev.type === 'MODIFIED' || ev.type === 'ADDED') {
          onUpdate && onUpdate(ev.object.spec.containerID);
        }

        // let apollo handle update
        if (ev.type === 'MODIFIED') {
          return prev;
        }

        const merged = { ...prev.logMetadataList};
        let items = Array.from(merged.items);

        // merge
        switch (ev.type) {
          case 'ADDED':
            items.push(ev.object);
            break;
          case 'DELETED':
            items = items.filter((item) => item.id !== ev.object?.id);
            break;
          default:
            throw new Error('not implemented');
        }

        merged.items = items;
        return { logMetadataList: merged };
      },
    });
  }, [subscribeToMore, loading, error]);

  return { loading, error, data };
}

/**
 * Color picker hook
 */

const palette = distinctColors({
  count: 20,
  chromaMin: 40,
  chromaMax: 100,
  lightMin: 20,
  lightMax: 80,
});

export function useColors(streams: string[]) {
  const [colorMap, setColorMap] = useState<Map<string, string>>(new Map());
  console.log(colorMap);
  useEffect(() => {
    const promises: Promise<ArrayBuffer>[] = [];

    streams.forEach((stream) => {
      const streamUTF8 = new TextEncoder().encode(stream);
      promises.push(crypto.subtle.digest('SHA-256', streamUTF8));
    });

    Promise.all(promises).then((values) => {
      values.forEach((value, i) => {
        const view = new DataView(value);
        const n = view.getUint8(0);
        const idx = ((2 * n) % 400) % 20;
        colorMap.set(streams[i], palette[idx].hex());
      });
      setColorMap(new Map(colorMap));
    });
  }, [streams]);

  return { colorMap };
}
