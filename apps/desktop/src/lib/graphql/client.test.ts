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

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const invokeMock = vi.fn();
vi.mock('@tauri-apps/api/core', () => ({
  invoke: (...args: unknown[]) => invokeMock(...args),
}));

const { createGraphqlClient } = await import('./client');
const { onError } = await import('@/lib/error-bus');
const { startErrorLogForwarder } = await import('@/lib/error-log');

// Whether key appears anywhere under x: every own property, enumerable or not,
// and every getter on its prototype chain, cycles walked once.
function holds(x: unknown, key: string, seen = new Set<unknown>()): boolean {
  if (typeof x === 'string') return x.includes(key);
  if (typeof x !== 'object' || x === null || seen.has(x)) return false;
  seen.add(x);
  const names = new Set<PropertyKey>();
  for (let o: object | null = x; o && o !== Object.prototype; o = Object.getPrototypeOf(o) as object | null) {
    for (const name of Reflect.ownKeys(o)) names.add(name);
  }
  for (const name of names) {
    let value: unknown;
    try {
      value = (x as Record<PropertyKey, unknown>)[name];
    } catch {
      continue;
    }
    if (typeof value !== 'function' && holds(value, key, seen)) return true;
  }
  return false;
}

// Worst-case retry window: maxNumberAttempts(3) over a backoff capped at
// maxDelayMs(5_000) is < 15s; this comfortably drains every scheduled
// retry timer (advanceTimersByTimeAsync also flushes the microtasks
// between them) without depending on the exact jittered delays.
const RETRY_DRAIN_MS = 20_000;

describe('createGraphqlClient', () => {
  beforeEach(() => {
    invokeMock.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('routes a query through invokeFetch and returns parsed data', async () => {
    invokeMock.mockResolvedValueOnce({ status: 200, body: '{"data":{"ping":"pong"}}' });

    const client = createGraphqlClient();
    const result = await client.query('{ ping }', {}).toPromise();

    expect(result.error).toBeUndefined();
    expect(result.data).toEqual({ ping: 'pong' });
    expect(invokeMock).toHaveBeenCalledTimes(1);
    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_query',
      expect.objectContaining({
        body: expect.stringContaining('ping'),
      }),
    );
  });

  it('retries a query that fails with a network error, then succeeds', async () => {
    vi.useFakeTimers();
    invokeMock
      .mockRejectedValueOnce('sidecar momentarily unreachable')
      .mockResolvedValueOnce({ status: 200, body: '{"data":{"ping":"pong"}}' });

    const client = createGraphqlClient();
    const pending = client.query('{ ping }', {}).toPromise();

    await vi.advanceTimersByTimeAsync(RETRY_DRAIN_MS);
    const result = await pending;

    expect(result.error).toBeUndefined();
    expect(result.data).toEqual({ ping: 'pong' });
    expect(invokeMock).toHaveBeenCalledTimes(2);
  });

  it('gives up with a network error after the bounded number of attempts', async () => {
    vi.useFakeTimers();
    invokeMock.mockRejectedValue('sidecar down');

    const client = createGraphqlClient();
    const pending = client.query('{ ping }', {}).toPromise();

    await vi.advanceTimersByTimeAsync(RETRY_DRAIN_MS);
    const result = await pending;

    // 1 initial attempt + 2 retries = maxNumberAttempts, then it fails
    // fast with a terminal networkError rather than retrying forever.
    expect(invokeMock).toHaveBeenCalledTimes(3);
    expect(result.error?.networkError).toBeDefined();
  });

  it('does not retry a GraphQL error (deterministic — only transport failures retry)', async () => {
    vi.useFakeTimers();
    invokeMock.mockResolvedValue({ status: 200, body: '{"errors":[{"message":"bad field"}]}' });

    const client = createGraphqlClient();
    const pending = client.query('{ ping }', {}).toPromise();

    await vi.advanceTimersByTimeAsync(RETRY_DRAIN_MS);
    const result = await pending;

    expect(result.error?.graphQLErrors[0]?.message).toBe('bad field');
    expect(invokeMock).toHaveBeenCalledTimes(1);
  });

  // A key typed into the Providers pane rides one mutation's variables. When the
  // save fails, the error bus gets the urql error as its cause, and the log
  // forwarder sends the host a record of it: neither holds the key.
  it('puts no saved key on the error bus or in the error log when a save fails', async () => {
    const key = 'sk-typed-in-the-pane-0123456789';
    invokeMock.mockImplementation((command: string) =>
      Promise.resolve(
        command === 'graphql_query'
          ? {
              status: 200,
              body: '{"errors":[{"message":"The keyring refused the key","extensions":{"code":"KUBETAIL_PROVIDER_KEY_NOT_SAVED"}}]}',
            }
          : undefined,
      ),
    );
    const reported: unknown[] = [];
    const unsubscribe = onError((e) => reported.push(e));
    const stopForwarder = startErrorLogForwarder();

    const result = await createGraphqlClient()
      .mutation(
        'mutation ($providerID: String!, $key: String!) { providerKeySave(providerID: $providerID, key: $key) }',
        {
          providerID: 'groq',
          key,
        },
      )
      .toPromise();
    unsubscribe();
    stopForwarder();

    expect(result.error?.graphQLErrors[0]?.extensions.code).toBe('KUBETAIL_PROVIDER_KEY_NOT_SAVED');
    expect(reported).toHaveLength(1);
    expect(holds(reported, 'KUBETAIL_PROVIDER_KEY_NOT_SAVED')).toBe(true); // the walk reaches the cause
    expect(holds(reported, key)).toBe(false);
    const sent = invokeMock.mock.calls.filter(([command]) => command === 'webview_record_error');
    expect(sent).toHaveLength(1);
    expect(holds(sent, key)).toBe(false);
  });
});
