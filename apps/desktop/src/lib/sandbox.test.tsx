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

import { renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { state, useQueryMock } = vi.hoisted(() => ({ state: { current: {} }, useQueryMock: vi.fn() }));
vi.mock('urql', () => ({ useQuery: useQueryMock }));
vi.mock('@/gql', () => ({ graphql: () => ({}) }));

const { useSandbox } = await import('./sandbox');

beforeEach(() => {
  vi.clearAllMocks();
  state.current = { fetching: true };
  useQueryMock.mockImplementation(() => [state.current, vi.fn()]);
});

describe('useSandbox', () => {
  it('is unknown while the query is in flight', () => {
    const { result } = renderHook(() => useSandbox());
    expect(result.current.available).toBeUndefined();
  });

  it("answers the sidecar's word once it has answered", () => {
    state.current = { fetching: false, data: { sandboxStateGet: { available: true } } };
    expect(renderHook(() => useSandbox()).result.current.available).toBe(true);
    state.current = { fetching: false, data: { sandboxStateGet: { available: false } } };
    expect(renderHook(() => useSandbox()).result.current.available).toBe(false);
  });

  // Whether a sandboxed command can be given the internet, and why not, rides the
  // same answer; unknown until it comes.
  it('answers whether the machine offers network, and why not', () => {
    expect(renderHook(() => useSandbox()).result.current.networkAvailable).toBeUndefined();
    state.current = {
      fetching: false,
      data: { sandboxStateGet: { available: true, networkAvailable: false, networkReason: 'pasta not found' } },
    };
    const { result } = renderHook(() => useSandbox());
    expect(result.current.networkAvailable).toBe(false);
    expect(result.current.networkReason).toBe('pasta not found');
  });

  // What the probe found, or why there is none, in the sidecar's words.
  it("answers the sidecar's reason", () => {
    expect(renderHook(() => useSandbox()).result.current.reason).toBe('');
    state.current = {
      fetching: false,
      data: { sandboxStateGet: { available: false, reason: 'user namespaces are disabled' } },
    };
    expect(renderHook(() => useSandbox()).result.current.reason).toBe('user namespaces are disabled');
  });

  // A sidecar that could not be reached has not said there is no sandbox.
  it('is unknown and failed after a failure', () => {
    state.current = { fetching: false, error: new Error('unreachable') };
    const { result } = renderHook(() => useSandbox());
    expect(result.current.available).toBeUndefined();
    expect(result.current.failed).toBe(true);
  });

  it('is not failed while in flight or once answered', () => {
    expect(renderHook(() => useSandbox()).result.current.failed).toBe(false);
    state.current = { fetching: false, data: { sandboxStateGet: { available: true } } };
    expect(renderHook(() => useSandbox()).result.current.failed).toBe(false);
  });

  // Nothing re-runs a query for nobody, so a failure is asked again past the cache.
  it('asks the sidecar again on retry', () => {
    const reexecute = vi.fn();
    useQueryMock.mockImplementation(() => [state.current, reexecute]);
    const { result } = renderHook(() => useSandbox());
    result.current.retry();
    expect(reexecute).toHaveBeenCalledWith({ requestPolicy: 'network-only' });
  });

  // The answer is fixed for the sidecar's life, so the hook asks once and never
  // polls.
  it('asks with a plain query', () => {
    renderHook(() => useSandbox());
    expect(useQueryMock).toHaveBeenCalledWith({ query: expect.anything() });
  });
});
