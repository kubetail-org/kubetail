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

import type { ApolloClient, ErrorLike } from '@apollo/client';

import type {
  Client,
  FetchOptions,
  LogRecord,
  SubscriptionCallback,
  SubscriptionCancelFunction,
  SubscriptionOptions,
} from '@/components/widgets/log-viewer';
import {
  LogRecordsQueryMode,
  type LogSourceFilter,
  type LogRecordsFragmentFragment as ServerLogRecord,
} from '@/lib/graphql/dashboard/__generated__/graphql';
import { LOG_RECORDS_FETCH, LOG_RECORDS_FOLLOW } from '@/lib/graphql/dashboard/ops';

/**
 * createRecord - Create LogRecord from server response item
 */

function createRecord(item: ServerLogRecord): LogRecord {
  // `timestamp` is a Time scalar, which arrives as an RFC 3339 string.
  const timestamp = item.timestamp as string;
  return {
    timestamp,
    message: item.message,
    cursor: timestamp,
    source: item.source,
  };
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
  onSubscriptionError?: (error: ErrorLike) => void;
};

export class LogServerClient implements Client {
  private apolloClient: ApolloClient;

  readonly kubeContext: string;

  readonly sources: string[];

  readonly sourceFilter?: LogSourceFilter;

  readonly grep?: string;

  private onSubscriptionError?: (error: ErrorLike) => void;

  constructor({ apolloClient, kubeContext, sources, sourceFilter, grep, onSubscriptionError }: LogServerClientOptions) {
    this.apolloClient = apolloClient;
    this.kubeContext = kubeContext;
    this.sources = sources;
    this.sourceFilter = sourceFilter;
    this.grep = grep;
    this.onSubscriptionError = onSubscriptionError;
  }

  private get queryArgs() {
    return {
      kubeContext: this.kubeContext,
      sources: this.sources,
      sourceFilter: this.sourceFilter,
      grep: this.grep,
    };
  }

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
    return { records: records.map(createRecord), nextCursor };
  }

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
    return { records: records.map(createRecord), nextCursor };
  }

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
    return { records: records.map(createRecord), nextCursor };
  }

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
    return { records: records.map(createRecord), nextCursor };
  }

  subscribe(callback: SubscriptionCallback, options?: SubscriptionOptions): SubscriptionCancelFunction {
    const observable = this.apolloClient.subscribe({
      query: LOG_RECORDS_FOLLOW,
      variables: {
        ...this.queryArgs,
        after: options?.after,
      },
    });

    // Apollo delivers both GraphQL and network errors on `next`, as `error`.
    const subscription = observable.subscribe({
      next: ({ data, error }) => {
        if (error) this.onSubscriptionError?.(error);
        else if (data?.logRecordsFollow) callback(createRecord(data.logRecordsFollow));
      },
    });

    return () => subscription.unsubscribe();
  }
}
