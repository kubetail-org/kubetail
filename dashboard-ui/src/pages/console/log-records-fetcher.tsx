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

import { useQuery } from '@apollo/client';
import { useAtomValue } from 'jotai';
import { forwardRef, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react';

import { getClusterAPIClient } from '@/apollo-client';
import { LogRecordsQueryMode, LogSourceFilter } from '@/lib/graphql/dashboard/__generated__/graphql';
import { LOG_RECORDS_FETCH, LOG_RECORDS_FOLLOW } from '@/lib/graphql/dashboard/ops';

import type { LogRecord } from './shared';
import { isFollowAtom } from './state';

const BATCH_SIZE = 300;

type LogRecordsFetchOptions = {
  mode: LogRecordsQueryMode;
  since?: string;
  after?: string | null;
  before?: string | null;
};

type LogRecordsFetchResponse = {
  records: LogRecord[];
  nextCursor: string | null;
};

export type LogRecordsFetcherHandle = {
  fetch: (opts: LogRecordsFetchOptions) => Promise<LogRecordsFetchResponse>;
  reset: () => void;
};

type LogRecordsFetcherProps = {
  useClusterAPI?: boolean;
  kubeContext: string | null;
  sources: string[];
  sourceFilter: LogSourceFilter;
  grep: string | null;
  onFollowData: (record: LogRecord) => void;
};

const LogRecordsFetcherImpl: React.ForwardRefRenderFunction<LogRecordsFetcherHandle, LogRecordsFetcherProps> = (
  { useClusterAPI, kubeContext, sources, sourceFilter, grep, onFollowData }: LogRecordsFetcherProps,
  ref: React.ForwardedRef<LogRecordsFetcherHandle>,
) => {
  const isFollow = useAtomValue(isFollowAtom);

  const [isReachedEnd, setIsReachedEnd] = useState(false);
  const lastTS = useRef<string>(undefined);

  const client = useMemo(() => {
    if (!useClusterAPI) return undefined;
    return getClusterAPIClient({
      kubeContext: kubeContext || '',
      namespace: 'kubetail-system',
      serviceName: 'kubetail-cluster-api',
    });
  }, [useClusterAPI, kubeContext]);

  // Initialize query
  const query = useQuery(LOG_RECORDS_FETCH, {
    client,
    skip: true,
    variables: { kubeContext, sources, sourceFilter, grep, limit: BATCH_SIZE + 1 },
  });

  // Expose handler
  useImperativeHandle(
    ref,
    () => ({
      fetch: async (opts: LogRecordsFetchOptions) => {
        // Reset previous refetch() args
        const newOpts = { after: undefined, before: undefined, since: undefined, ...opts };

        // Execute query
        const response = (await query.refetch(newOpts)).data.logRecordsFetch;
        if (!response) throw new Error('query response is null');

        let records: LogRecord[] = [];
        let nextCursor: string | null = null;

        // Handle response
        switch (opts.mode) {
          case LogRecordsQueryMode.Head:
            records = response.records.slice(0, BATCH_SIZE);
            if (response.records.length > BATCH_SIZE) nextCursor = records[records.length - 1].timestamp;
            setIsReachedEnd(!nextCursor);
            break;
          case LogRecordsQueryMode.Tail:
            records = response.records.slice(Math.max(response.records.length - BATCH_SIZE, 0));
            if (response.records.length > BATCH_SIZE) nextCursor = records[0].timestamp;
            setIsReachedEnd(true);
            break;
          default:
            throw new Error('not implemented');
        }

        // Update last TS
        if (records.length) lastTS.current = records[records.length - 1].timestamp;

        return { records, nextCursor };
      },
      reset: () => {
        lastTS.current = undefined;
        setIsReachedEnd(false);
      },
    }),
    [kubeContext, sources, sourceFilter, grep],
  );

  // Follow
  useEffect(() => {
    if (!isReachedEnd || !isFollow) return;

    return query.subscribeToMore({
      document: LOG_RECORDS_FOLLOW,
      variables: { kubeContext, sources, sourceFilter, grep, after: lastTS.current },
      updateQuery: (_, { subscriptionData }) => {
        const {
          data: { logRecordsFollow: record },
        } = subscriptionData;
        if (record) {
          // Update last TS
          lastTS.current = record.timestamp;

          // Execute callback
          onFollowData(record);
        }
        return { logRecordsFetch: null };
      },
    });
  }, [kubeContext, sources, isReachedEnd, isFollow, query.subscribeToMore]);

  return null;
};

export const LogRecordsFetcher = forwardRef(LogRecordsFetcherImpl);
