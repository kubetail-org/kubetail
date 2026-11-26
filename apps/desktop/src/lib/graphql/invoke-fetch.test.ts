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

import { beforeEach, describe, expect, it, vi } from 'vitest';

// Mock the Tauri invoke surface; the adapter is the only thing that should
// touch it, so the mock fully simulates the host bridge for these tests.
const invokeMock = vi.fn();
vi.mock('@tauri-apps/api/core', () => ({
  invoke: (...args: unknown[]) => invokeMock(...args),
}));

// Imported after vi.mock so the module under test sees the mock.
const { invokeFetch } = await import('./invoke-fetch');

describe('invokeFetch', () => {
  beforeEach(() => {
    invokeMock.mockReset();
  });

  it('forwards the request body to graphql_query and wraps the response', async () => {
    invokeMock.mockResolvedValueOnce({ status: 200, body: '{"data":{"ping":"pong"}}' });

    const res = await invokeFetch('tauri://graphql', {
      method: 'POST',
      body: '{"query":"{ ping }"}',
    });

    expect(invokeMock).toHaveBeenCalledWith('graphql_query', { body: '{"query":"{ ping }"}' });
    expect(res.ok).toBe(true);
    expect(res.status).toBe(200);
    await expect(res.json()).resolves.toEqual({ data: { ping: 'pong' } });
  });

  it('preserves a non-2xx status so urql treats it as a server error, not a network error', async () => {
    // A 401 from the sidecar (e.g. auth required) must reach urql as a
    // Response with status=401 — that path is non-retryable in urql's
    // retryExchange. If we collapsed it to status=200 the GraphQL
    // CombinedError would have no networkError, retryExchange wouldn't
    // touch it, and the auth failure would propagate. If we collapsed it
    // to a thrown error retryExchange would loop on it forever.
    invokeMock.mockResolvedValueOnce({
      status: 401,
      body: '{"errors":[{"message":"no auth"}]}',
    });

    const res = await invokeFetch('tauri://graphql', { method: 'POST', body: '{}' });
    expect(res.ok).toBe(false);
    expect(res.status).toBe(401);
    await expect(res.json()).resolves.toEqual({ errors: [{ message: 'no auth' }] });
  });

  it('rethrows an invoke rejection as an Error (a real network failure, not a 500 envelope)', async () => {
    invokeMock.mockRejectedValueOnce('boom');

    // A transport failure must surface as a thrown error so urql produces a
    // CombinedError with a `networkError` — not a synthetic GraphQL 500,
    // which is indistinguishable from a real server error and not retryable.
    await expect(invokeFetch('tauri://graphql', { method: 'POST', body: '{}' })).rejects.toThrow('boom');
  });

  it('preserves an Error rejection instance as-is', async () => {
    const original = new Error('socket closed');
    invokeMock.mockRejectedValueOnce(original);

    await expect(invokeFetch('tauri://graphql', { method: 'POST', body: '{}' })).rejects.toBe(original);
  });
});
