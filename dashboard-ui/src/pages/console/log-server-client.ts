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

import type { ApolloClient } from '@apollo/client';

import {
  LogSourceFilter,
  LogRecord as ServerLogRecord,
  LogRecordsQueryMode,
} from '@/lib/graphql/dashboard/__generated__/graphql';
import { LOG_RECORDS_FETCH, LOG_RECORDS_FOLLOW } from '@/lib/graphql/dashboard/ops';

import type {
  Client,
  FetchOptions,
  FetchResult,
  LogRecord,
  SubscriptionCallback,
  SubscriptionCancelFunction,
  SubscriptionOptions,
} from '@/components/widgets/log-viewer';

/**
 * createRecord - Create LogRecord from server response item
 */

function createRecord(item: ServerLogRecord): LogRecord {
  return {
    timestamp: item.timestamp,
    message: item.message,
    cursor: item.timestamp,
    source: item.source,
  };
}

function upgradeRecords(records: any[]): LogRecord[] {
  return records.map((r) => {
    const copy = { ...r };
    copy.cursor = r.timestamp;
    return copy;
  });
}

/**
 * LogServerClient - Represents a log server client that satisfies the LogViewer Client interface
 */

export type LogServerClientOptions = {
  apolloClient: ApolloClient;
  kubeContext: string;
  sources: string[];
  sourceFilter?: LogSourceFilter;
  grep?: string;
};

export class LogServerClient implements Client {
  private apolloClient: ApolloClient;

  private queryArgs: {
    kubeContext: string;
    sources: string[];
    sourceFilter?: LogSourceFilter;
    grep?: string;
  };

  /**
   * Constructor
   * @param apolloClient - Apollo client instance
   */
  constructor({ apolloClient, kubeContext, sources, sourceFilter, grep }: LogServerClientOptions) {
    this.apolloClient = apolloClient;
    this.queryArgs = {
      kubeContext,
      sources,
      sourceFilter,
      grep,
    };
  }

  /**
   * fetchSince - Get the first `limit` log entries starting with `cursor`
   * @param options - Fetch options
   * @returns A promise that resolves to the fetch result
   */
  async fetchSince(options: FetchOptions) {
    const result = await this.apolloClient.query({
      query: LOG_RECORDS_FETCH,
      variables: {
        ...this.queryArgs,
        since: options.cursor,
        limit: options.limit,
        mode: LogRecordsQueryMode.Head,
      },
      fetchPolicy: 'no-cache',
    });

    if (!result.data?.logRecordsFetch) throw new Error('unexpected');

    const { records, nextCursor } = result.data.logRecordsFetch;
    return { records: upgradeRecords(records), nextCursor } as FetchResult;
  }

  /**
   * fetchUntil - Get the last `limit` log entries ending with the `cursor`
   * @param options - Fetch options
   * @returns A promise that resolves to the fetch result
   */
  async fetchUntil(options: FetchOptions) {
    const result = await this.apolloClient.query({
      query: LOG_RECORDS_FETCH,
      variables: {
        ...this.queryArgs,
        until: options.cursor,
        limit: options.limit,
        mode: LogRecordsQueryMode.Tail,
      },
      fetchPolicy: 'no-cache',
    });

    if (!result.data?.logRecordsFetch) throw new Error('unexpected');

    const { records, nextCursor } = result.data.logRecordsFetch;
    return { records: upgradeRecords(records), nextCursor } as FetchResult;
  }

  /**
   * fetchAfter - Get the first `limit` log entries after `curosor`
   * @param options - Fetch options
   * @returns A promise that resolves to the fetch result
   */
  async fetchAfter(options: FetchOptions) {
    const result = await this.apolloClient.query({
      query: LOG_RECORDS_FETCH,
      variables: {
        ...this.queryArgs,
        after: options.cursor,
        limit: options.limit,
        mode: LogRecordsQueryMode.Head,
      },
      fetchPolicy: 'no-cache',
    });

    if (!result.data?.logRecordsFetch) throw new Error('unexpected');

    const { records, nextCursor } = result.data.logRecordsFetch;
    return { records: upgradeRecords(records), nextCursor } as FetchResult;
  }

  /**
   * fetchBefore - Get the last `limit` log entries before `cursor`
   * @param options - Fetch options
   * @returns A promise that resolves to the fetch result
   */
  async fetchBefore(options: FetchOptions) {
    const result = await this.apolloClient.query({
      query: LOG_RECORDS_FETCH,
      variables: {
        ...this.queryArgs,
        before: options.cursor,
        limit: options.limit,
        mode: LogRecordsQueryMode.Tail,
      },
      fetchPolicy: 'no-cache',
    });

    if (!result.data?.logRecordsFetch) throw new Error('unexpected');

    const { records, nextCursor } = result.data.logRecordsFetch;
    return { records: upgradeRecords(records), nextCursor } as FetchResult;
  }

  /**
   * subscribe - Subscribe to new lines
   * @param callback - The function to call with every record
   * @param options - Subscription options
   * @returns cancel - The cancellation function
   */
  subscribe(callback: SubscriptionCallback, options?: SubscriptionOptions): SubscriptionCancelFunction {
    const observable = this.apolloClient.subscribe({
      query: LOG_RECORDS_FOLLOW,
      variables: {
        ...this.queryArgs,
        after: options?.after,
      },
    });

    const subscription = observable.subscribe({
      next({ data }) {
        if (data?.logRecordsFollow) callback(createRecord(data.logRecordsFollow));
      },
      error(err) {
        console.error('Subscription error:', err);
      },
    });

    return () => subscription.unsubscribe();
  }
}
