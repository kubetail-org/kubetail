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

const { useQueryMock, useMutationMock, reexecute, mutations } = vi.hoisted(() => ({
  useQueryMock: vi.fn(),
  useMutationMock: vi.fn(),
  reexecute: vi.fn(),
  mutations: {} as Record<string, Mock<(variables: unknown) => Promise<unknown>>>,
}));
vi.mock('urql', () => ({ useQuery: useQueryMock, useMutation: useMutationMock }));
// Each document is its name, so the mock can tell the mutations apart.
vi.mock('@/gql', () => ({ graphql: (source: string) => /(?:query|mutation) (\w+)/.exec(source)![1] }));

const { useSandboxPath } = await import('./sandbox-path');

const folder = (path: string, searched: boolean) => ({
  path,
  origin: 'Shell',
  target: path,
  searched,
  reason: searched ? null : 'No sandboxed command reads this folder; type it into your PATH to include it.',
  addable: !searched,
  shared: false,
});
const folders = [folder('/opt/homebrew/bin', true), folder('/Users/ren/scripts', false)];

const queried = {
  fetching: false,
  data: { sandboxPathGet: { value: '$PATH', refusal: null, folders }, sandboxPathFaultGet: 'timeout' },
};

beforeEach(() => {
  vi.clearAllMocks();
  useQueryMock.mockImplementation(() => [queried, reexecute]);
  mutations.SandboxPathSet = vi.fn();
  mutations.SandboxPathRefresh = vi.fn();
  useMutationMock.mockImplementation((doc: string) => [{}, mutations[doc]]);
});

describe('useSandboxPath', () => {
  it('reads the setting, its folders and the fault', () => {
    const { result } = renderHook(() => useSandboxPath());
    expect(result.current.value).toBe('$PATH');
    expect(result.current.refusal).toBeNull();
    expect(result.current.folders).toEqual(folders);
    expect(result.current.fault).toBe('timeout');
  });

  it('saves a value, in flight until it answers, then asks for the setting again', async () => {
    let answer!: (value: unknown) => void;
    mutations.SandboxPathSet.mockImplementation(
      () =>
        new Promise((resolve) => {
          answer = resolve;
        }),
    );
    const { result } = renderHook(() => useSandboxPath());

    let saved!: Promise<boolean>;
    act(() => {
      saved = result.current.save('~/bin:$PATH');
    });
    expect(mutations.SandboxPathSet).toHaveBeenCalledWith({ value: '~/bin:$PATH' });
    expect(result.current.saving).toBe(true);

    await act(async () => answer({ data: { sandboxPathSet: { value: '~/bin:$PATH' } } }));
    expect(await saved).toBe(true);
    expect(result.current.saving).toBe(false);
    expect(result.current.saveError).toBeNull();
    expect(reexecute).toHaveBeenCalledWith({ requestPolicy: 'network-only' });
  });

  it("keeps a refused save's message, and answers that it was not taken", async () => {
    mutations.SandboxPathSet.mockResolvedValue({
      error: { graphQLErrors: [{ message: '"bin" is not a full path.' }] },
    });
    const { result } = renderHook(() => useSandboxPath());
    let taken!: boolean;
    await act(async () => {
      taken = await result.current.save('bin');
    });
    expect(taken).toBe(false);
    expect(result.current.saveError).toBe('"bin" is not a full path.');
  });

  // A save whose write took, though the setting could not be read after, was
  // taken: the message stays, and the caller treats it as saved.
  it('takes a save answered KUBETAIL_SAVED_UNREAD, keeping its message', async () => {
    mutations.SandboxPathSet.mockResolvedValue({
      error: {
        graphQLErrors: [
          {
            message: 'Your PATH was saved, but Kubetail could not read the settings back.',
            extensions: { code: 'KUBETAIL_SAVED_UNREAD' },
          },
        ],
      },
    });
    const { result } = renderHook(() => useSandboxPath());
    let taken!: boolean;
    await act(async () => {
      taken = await result.current.save('/opt/bin');
    });
    expect(taken).toBe(true);
    expect(result.current.saveError).toBe('Your PATH was saved, but Kubetail could not read the settings back.');
  });

  // The fault is the last read's, so a refresh reads it again whatever the answer.
  it("reads the fault again after a refresh, and keeps a refused one's message", async () => {
    mutations.SandboxPathRefresh.mockResolvedValue({
      error: { graphQLErrors: [{ message: 'Your shell did not answer: timeout.' }] },
    });
    const { result } = renderHook(() => useSandboxPath());
    await act(async () => result.current.refresh());
    expect(result.current.refreshError).toBe('Your shell did not answer: timeout.');
    expect(reexecute).toHaveBeenCalledWith({ requestPolicy: 'network-only' });

    mutations.SandboxPathRefresh.mockResolvedValue({ data: { sandboxPathRefresh: { value: '$PATH' } } });
    await act(async () => result.current.refresh());
    expect(result.current.refreshError).toBeNull();
    expect(result.current.refreshing).toBe(false);
    expect(reexecute).toHaveBeenCalledTimes(2);
  });

  // Another window can change the setting, and each window caches its own, so
  // it is asked again on opening and whenever the window takes focus.
  it('asks again on opening and on focus', () => {
    renderHook(() => useSandboxPath());
    expect(useQueryMock).toHaveBeenCalledWith(expect.objectContaining({ requestPolicy: 'cache-and-network' }));

    window.dispatchEvent(new Event('focus'));
    expect(reexecute).toHaveBeenCalledWith({ requestPolicy: 'network-only' });
  });
});
