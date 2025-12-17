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

import { useQuery, useSubscription } from '@apollo/client/react';
import { useSetAtom } from 'jotai';
import { useEffect, useMemo, useRef } from 'react';

import { getClusterAPIClient } from '@/apollo-client';
import { CLUSTER_API_READY_WAIT } from '@/lib/graphql/dashboard/ops';
import { LOG_METADATA_LIST_FETCH, LOG_METADATA_LIST_WATCH } from '@/lib/graphql/cluster-api/ops';
import type { LogMetadataListFetchQuery, LogMetadataWatchEvent } from '@/lib/graphql/cluster-api/__generated__/graphql';
import { useIsClusterAPIEnabled, useRetryOnError } from '@/lib/hooks';

import type { FileInfo, KubeContext } from './shared';
import { logMetadataMapAtomFamily } from './state';

const BATCH_INTERVAL_MS = 2000;

/**
 * LogMetadataProvider component
 */

type LogMetadataProviderProps = {
  kubeContext: KubeContext;
};

export const LogMetadataProvider = ({ kubeContext }: LogMetadataProviderProps) => {
  const retryOnError = useRetryOnError();
  const setLogMetadataMap = useSetAtom(logMetadataMapAtomFamily(kubeContext));

  const isClusterAPIEnabled = useIsClusterAPIEnabled(kubeContext);
  const isEnabled = isClusterAPIEnabled && kubeContext !== null;

  const connectArgs = useMemo(
    () => ({
      kubeContext: kubeContext || '',
      namespace: 'kubetail-system',
      serviceName: 'kubetail-cluster-api',
    }),
    [kubeContext],
  );

  const readyWait = useSubscription(CLUSTER_API_READY_WAIT, {
    skip: !isEnabled,
    variables: connectArgs,
  });

  const isReady = readyWait.data?.clusterAPIReadyWait ?? false;

  const client = useMemo(() => getClusterAPIClient(connectArgs), [connectArgs]);

  // Initial query
  const { loading, error, data, subscribeToMore, refetch } = useQuery(LOG_METADATA_LIST_FETCH, {
    skip: !isEnabled || !isReady,
    client,
  });

  // Handle errors with retry
  useEffect(() => {
    if (error) {
      retryOnError(refetch);
    }
  }, [error, retryOnError, refetch]);

  // Initialize data map
  useEffect(() => {
    const items = data?.logMetadataList?.items || [];
    if (!items.length) return;

    const inner = new Map<string, FileInfo>();
    for (let i = 0; i < items.length; i += 1) {
      const item = items[i];
      inner.set(item.spec.containerID, item.fileInfo);
    }
    setLogMetadataMap({ inner });
  }, [loading, setLogMetadataMap]);

  // Set up batch update mechanism
  const eventBufferRef = useRef<LogMetadataWatchEvent[]>([]);

  useEffect(() => {
    const id = setInterval(() => {
      // Exit early if no data in buffer
      if (!eventBufferRef.current.length) return;

      // Capture current buffer
      const eventBuffer = eventBufferRef.current;
      eventBufferRef.current = [];

      // Init selectors array for flashing UI later
      const selectors = new Array<string>();

      // Update LogMetadataMap
      setLogMetadataMap(({ inner }) => {
        // Loop over events in buffer
        eventBuffer.forEach((ev) => {
          if (!ev?.type || !ev?.object) return;

          const { containerID } = ev.object.spec;

          if (ev.type === 'MODIFIED' || ev.type === 'ADDED') {
            const { size } = ev.object.fileInfo;
            let { lastModifiedAt } = ev.object.fileInfo;
            lastModifiedAt = lastModifiedAt ? new Date(lastModifiedAt) : new Date(0);

            // Update atom
            inner.set(containerID, { size, lastModifiedAt });

            // Update selectors
            selectors.push(`.last_event_${containerID}`);

            // Flash data
          } else if (ev.type === 'DELETED') {
            // Update atom
            inner.delete(containerID);
          }
        });

        // Return new instance to trigger update
        return { inner };
      });

      // Flash UI
      if (selectors.length) {
        document.querySelectorAll(selectors.join(', ')).forEach((el) => {
          const k = 'animate-flash-bg-green';
          el.classList.remove(k);
          el.classList.add(k);
          setTimeout(() => el.classList.remove(k), 1000);
        });
      }
    }, BATCH_INTERVAL_MS);

    return () => {
      // Stop interval timer
      clearInterval(id);

      // Clear buffer
      eventBufferRef.current = [];
    };
  }, [eventBufferRef, setLogMetadataMap]);

  // Subscribe to changes
  useEffect(() => {
    // Wait for all data to get fetched
    if (!isEnabled || !isReady || loading || error) return;

    return subscribeToMore({
      document: LOG_METADATA_LIST_WATCH,
      updateQuery: (prev, { subscriptionData }) => {
        const ev = subscriptionData.data.logMetadataWatch;
        if (ev) eventBufferRef.current.push(ev);
        return prev as LogMetadataListFetchQuery;
      },
    });
  }, [isEnabled, isReady, loading, error, subscribeToMore, setLogMetadataMap]);

  return null;
};
