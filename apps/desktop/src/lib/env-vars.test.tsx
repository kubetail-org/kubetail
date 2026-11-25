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

import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, type Mock, vi } from 'vitest';

const { useQueryMock, useMutationMock, reexecute, mutations, sources } = vi.hoisted(() => ({
  useQueryMock: vi.fn(),
  useMutationMock: vi.fn(),
  reexecute: vi.fn(),
  mutations: {} as Record<string, Mock<(variables: unknown) => Promise<unknown>>>,
  sources: [] as string[],
}));
vi.mock('urql', () => ({ useQuery: useQueryMock, useMutation: useMutationMock }));
// Each document is its name, so the mock can tell the mutations apart; its
// source is kept for the test to read.
vi.mock('@/gql', () => ({
  graphql: (source: string) => {
    sources.push(source);
    return /(?:query|mutation) (\w+)/.exec(source)![1];
  },
}));

const { useEnvVars } = await import('./env-vars');

const listed = [
  { name: 'HELM_NAMESPACE', value: 'apps', target: 'Both', refusal: null },
  { name: 'LD_PRELOAD', value: '/x', target: 'Sandbox', refusal: 'LD_PRELOAD changes how a program starts.' },
];

beforeEach(() => {
  vi.clearAllMocks();
  // The answer is the scope's own unless a test says otherwise.
  useQueryMock.mockImplementation(({ variables }) => [
    { fetching: false, data: { envVarsGet: listed }, operation: { variables } },
    reexecute,
  ]);
  useMutationMock.mockImplementation((doc: string) => {
    mutations[doc] ??= vi.fn();
    return [{}, mutations[doc]];
  });
});

describe('useEnvVars', () => {
  it("reads its scope's list", () => {
    const { result } = renderHook(() => useEnvVars('prod'));
    expect(result.current.vars).toEqual(listed);
    expect(useQueryMock).toHaveBeenCalledWith(
      expect.objectContaining({ query: 'EnvVarsGet', variables: { clusterID: 'prod' } }),
    );
  });

  it('asks one query, the list with each target and refusal', () => {
    renderHook(() => useEnvVars(null));
    expect(useQueryMock).toHaveBeenCalledWith(expect.objectContaining({ variables: { clusterID: null } }));
    const queries = sources.filter((s) => s.trim().startsWith('query'));
    expect(queries).toHaveLength(1);
    expect(queries[0].replaceAll(/\s+/g, ' ')).toContain(
      'envVarsGet(clusterID: $clusterID) { name value target refusal }',
    );
  });

  it('sets a variable, in flight until it answers, then asks for the list again', async () => {
    let answer!: (value: unknown) => void;
    mutations.EnvVarSet.mockImplementation(
      () =>
        new Promise((resolve) => {
          answer = resolve;
        }),
    );
    const { result } = renderHook(() => useEnvVars('prod'));

    let refusal: Promise<string | null>;
    act(() => {
      refusal = result.current.set('EDITOR', 'vi', 'Outside');
    });
    expect(mutations.EnvVarSet).toHaveBeenCalledWith({
      clusterID: 'prod',
      name: 'EDITOR',
      value: 'vi',
      target: 'Outside',
    });
    expect(result.current.changing).toBe(true);

    await act(async () => answer({ data: { envVarSet: [] } }));
    await expect(refusal!).resolves.toBeNull();
    expect(result.current.changing).toBe(false);
    expect(reexecute).toHaveBeenCalledWith({ requestPolicy: 'network-only' });
  });

  it("answers a refused set's message", async () => {
    mutations.EnvVarSet.mockResolvedValue({
      error: { graphQLErrors: [{ message: 'Kubetail sets HOME itself.' }] },
    });
    const { result } = renderHook(() => useEnvVars('prod'));

    let refusal: string | null = null;
    await act(async () => {
      refusal = await result.current.set('HOME', '/', 'Both');
    });
    expect(refusal).toBe('Kubetail sets HOME itself.');
  });

  it("removes a variable and keeps a refused remove's message", async () => {
    mutations.EnvVarRemove.mockResolvedValue({
      error: { graphQLErrors: [{ message: 'That variable is not set.' }] },
    });
    const { result } = renderHook(() => useEnvVars('prod'));
    await act(async () => result.current.remove('PAGER'));
    expect(mutations.EnvVarRemove).toHaveBeenCalledWith({ clusterID: 'prod', name: 'PAGER' });
    expect(result.current.removeError).toBe('That variable is not set.');
    expect(reexecute).toHaveBeenCalledWith({ requestPolicy: 'network-only' });

    let answer!: (value: unknown) => void;
    mutations.EnvVarRemove.mockImplementation(
      () =>
        new Promise((resolve) => {
          answer = resolve;
        }),
    );
    act(() => void result.current.remove('HELM_NAMESPACE'));
    expect(result.current.removeError).toBeNull();
    await act(async () => answer({ data: { envVarRemove: [] } }));
  });

  // urql keeps the last answer while it asks for the next scope's, and a remove
  // there would act on the new scope.
  it("draws no list of another scope's, and no refusal from one", async () => {
    useQueryMock.mockReturnValue([
      {
        fetching: true,
        data: { envVarsGet: listed },
        error: { graphQLErrors: [{ message: 'no app.db' }] },
        operation: { variables: { clusterID: 'prod' } },
      },
      reexecute,
    ]);
    const { result } = renderHook(() => useEnvVars(null));
    expect(result.current.vars).toBeUndefined();
    expect(result.current.loadError).toBeNull();
  });

  it('answers why the list could not be read', () => {
    useQueryMock.mockReturnValue([
      {
        fetching: false,
        error: { graphQLErrors: [{ message: 'no app.db' }] },
        operation: { variables: { clusterID: 'prod' } },
      },
      reexecute,
    ]);
    const { result } = renderHook(() => useEnvVars('prod'));
    expect(result.current.vars).toBeUndefined();
    expect(result.current.loadError).toBe('no app.db');
  });

  it('keeps a refused remove with its scope, until the next change', async () => {
    mutations.EnvVarRemove.mockResolvedValue({ error: { graphQLErrors: [{ message: 'That variable is not set.' }] } });
    mutations.EnvVarSet.mockResolvedValue({ data: { envVarSet: [] } });
    const initialProps: { clusterID: string | null } = { clusterID: 'prod' };
    const { result, rerender } = renderHook(({ clusterID }) => useEnvVars(clusterID), { initialProps });
    await act(async () => result.current.remove('PAGER'));
    expect(result.current.removeError).toBe('That variable is not set.');

    rerender({ clusterID: null });
    expect(result.current.removeError).toBeNull();
    rerender({ clusterID: 'prod' });
    expect(result.current.removeError).toBe('That variable is not set.');

    await act(async () => void (await result.current.set('EDITOR', 'vi', 'Both')));
    expect(result.current.removeError).toBeNull();
  });

  it('asks for the list again when the window takes focus', () => {
    renderHook(() => useEnvVars('prod'));
    window.dispatchEvent(new Event('focus'));
    expect(reexecute).toHaveBeenCalledWith({ requestPolicy: 'network-only' });
  });
});
