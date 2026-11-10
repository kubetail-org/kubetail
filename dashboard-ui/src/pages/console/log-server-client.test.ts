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

import { LogServerClient } from './log-server-client';

const record = {
  timestamp: '2024-01-01T00:00:00Z',
  message: 'hello',
  source: {
    metadata: { region: '', zone: '', os: '', arch: '', node: 'node-1' },
    namespace: 'ns',
    podName: 'pod',
    containerName: 'c1',
  },
};

type Observer = { next: (result: ApolloClient.SubscribeResult) => void };

// A stand-in for the observable `ApolloClient.subscribe` returns.
function createClient(onSubscriptionError?: (error: Error) => void) {
  const observers = new Set<Observer>();
  const results = {
    next: (result: ApolloClient.SubscribeResult) => observers.forEach((o) => o.next(result)),
  };
  const observable = {
    subscribe: (observer: Observer) => {
      observers.add(observer);
      return { unsubscribe: () => observers.delete(observer) };
    },
  };
  const apolloClient = { subscribe: () => observable } as unknown as ApolloClient;
  const client = new LogServerClient({ apolloClient, kubeContext: '', sources: ['ns/pod'], onSubscriptionError });
  return { client, results };
}

describe('LogServerClient.subscribe', () => {
  it('passes each record to the callback', () => {
    const { client, results } = createClient();
    const callback = vi.fn();
    client.subscribe(callback);

    results.next({ data: { logRecordsFollow: record } });

    expect(callback).toHaveBeenCalledWith(expect.objectContaining({ message: 'hello', cursor: record.timestamp }));
  });

  it('reports an error to onSubscriptionError', () => {
    const onSubscriptionError = vi.fn();
    const { client, results } = createClient(onSubscriptionError);
    const callback = vi.fn();
    client.subscribe(callback);

    const error = new Error('connection lost');
    results.next({ data: undefined, error });

    expect(onSubscriptionError).toHaveBeenCalledWith(error);
    expect(callback).not.toHaveBeenCalled();
  });

  it('stops delivering records after cancel', () => {
    const { client, results } = createClient();
    const callback = vi.fn();
    const cancel = client.subscribe(callback);

    cancel();
    results.next({ data: { logRecordsFollow: record } });

    expect(callback).not.toHaveBeenCalled();
  });
});
