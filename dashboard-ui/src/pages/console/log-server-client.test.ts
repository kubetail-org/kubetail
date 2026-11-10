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

import type { ApolloClient } from '@apollo/client';

import {
  LogRecordsQueryMode,
  type LogRecordsFragmentFragment as ServerLogRecord,
} from '@/lib/graphql/dashboard/__generated__/graphql';
import { LOG_RECORDS_FETCH, LOG_RECORDS_FOLLOW } from '@/lib/graphql/dashboard/ops';

import { LogServerClient } from './log-server-client';

const source = {
  __typename: 'LogSource' as const,
  namespace: 'default',
  podName: 'pod-1',
  containerName: 'app',
  containerID: 'cid-1',
  metadata: {
    __typename: 'LogSourceMetadata' as const,
    region: 'r1',
    zone: 'z1',
    os: 'linux',
    arch: 'amd64',
    node: 'node-1',
  },
};

const serverRecord = (timestamp: string, message: string): ServerLogRecord => ({
  __typename: 'LogRecord',
  timestamp,
  message,
  source,
});

// The client's options, which it sends as the variables of every operation.
const queryArgs = {
  kubeContext: 'ctx',
  sources: ['default:pod-1'],
  sourceFilter: { region: ['r1'] },
  grep: 'error',
};

type Observer = {
  next: (value: { data?: unknown }) => void;
  error: (err: unknown) => void;
};

// Returns a LogServerClient over a fake Apollo client, and the fake's parts.
function setup() {
  let observer: Observer | undefined;
  const unsubscribe = vi.fn();

  const query = vi.fn();
  const subscribe = vi.fn(() => ({
    subscribe: (o: Observer) => {
      observer = o;
      return { unsubscribe };
    },
  }));

  const apolloClient = { query, subscribe } as unknown as ApolloClient;

  return {
    client: new LogServerClient({ apolloClient, ...queryArgs }),
    query,
    subscribe,
    unsubscribe,
    getObserver: () => observer!,
  };
}

describe('LogServerClient fetch methods', () => {
  it.each([
    ['fetchSince', 'since', LogRecordsQueryMode.Head],
    ['fetchUntil', 'until', LogRecordsQueryMode.Tail],
    ['fetchAfter', 'after', LogRecordsQueryMode.Head],
    ['fetchBefore', 'before', LogRecordsQueryMode.Tail],
  ] as const)('%s queries with the cursor as `%s` in %s mode', async (method, cursorArg, mode) => {
    const { client, query } = setup();
    query.mockResolvedValue({
      data: {
        logRecordsFetch: {
          records: [serverRecord('2024-01-01T00:00:00Z', 'line 1'), serverRecord('2024-01-01T00:00:01Z', 'line 2')],
          nextCursor: 'next',
        },
      },
    });

    const result = await client[method]({ cursor: 'cur', limit: 10 });

    expect(query).toHaveBeenCalledWith({
      query: LOG_RECORDS_FETCH,
      variables: { ...queryArgs, [cursorArg]: 'cur', limit: 10, mode },
      fetchPolicy: 'no-cache',
    });

    expect(result.nextCursor).toBe('next');
    expect(result.records).toEqual([
      { timestamp: '2024-01-01T00:00:00Z', message: 'line 1', cursor: '2024-01-01T00:00:00Z', source },
      { timestamp: '2024-01-01T00:00:01Z', message: 'line 2', cursor: '2024-01-01T00:00:01Z', source },
    ]);
  });

  it.each(['fetchSince', 'fetchUntil', 'fetchAfter', 'fetchBefore'] as const)(
    '%s rejects when the response has no data',
    async (method) => {
      const { client, query } = setup();
      query.mockResolvedValue({ data: undefined });

      await expect(client[method]({})).rejects.toThrow('unexpected');
    },
  );
});

describe('LogServerClient.subscribe', () => {
  it('follows from the `after` cursor and passes each record to the callback', () => {
    const { client, subscribe, getObserver } = setup();
    const callback = vi.fn();

    client.subscribe(callback, { after: 'cur' });

    expect(subscribe).toHaveBeenCalledWith({
      query: LOG_RECORDS_FOLLOW,
      variables: { ...queryArgs, after: 'cur' },
    });

    getObserver().next({ data: { logRecordsFollow: serverRecord('2024-01-01T00:00:02Z', 'line 3') } });

    expect(callback).toHaveBeenCalledTimes(1);
    expect(callback).toHaveBeenCalledWith({
      timestamp: '2024-01-01T00:00:02Z',
      message: 'line 3',
      cursor: '2024-01-01T00:00:02Z',
      source,
    });
  });

  it('ignores an event without a record', () => {
    const { client, subscribe, getObserver } = setup();
    const callback = vi.fn();

    client.subscribe(callback);

    expect(subscribe).toHaveBeenCalledWith({
      query: LOG_RECORDS_FOLLOW,
      variables: { ...queryArgs, after: undefined },
    });

    getObserver().next({ data: { logRecordsFollow: null } });
    getObserver().next({ data: undefined });

    expect(callback).not.toHaveBeenCalled();
  });

  it('logs a subscription error', () => {
    const { client, getObserver } = setup();
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});

    client.subscribe(vi.fn());
    const err = new Error('boom');
    getObserver().error(err);

    expect(consoleError).toHaveBeenCalledWith('Subscription error:', err);
    consoleError.mockRestore();
  });

  it('returns a function that unsubscribes', () => {
    const { client, unsubscribe } = setup();

    const cancel = client.subscribe(vi.fn());
    expect(unsubscribe).not.toHaveBeenCalled();

    cancel();
    expect(unsubscribe).toHaveBeenCalledTimes(1);
  });
});
