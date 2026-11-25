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

const { useFoldedSubscriptionMock, useMutationMock, mutations } = vi.hoisted(() => ({
  useFoldedSubscriptionMock: vi.fn(),
  useMutationMock: vi.fn(),
  mutations: {} as Record<string, Mock<(variables: unknown) => Promise<unknown>>>,
}));
vi.mock('urql', () => ({ useMutation: useMutationMock }));
vi.mock('@/lib/graphql/use-folded-subscription', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/graphql/use-folded-subscription')>()),
  useFoldedSubscription: useFoldedSubscriptionMock,
}));
// Each document is its name, so the mock can tell the mutations apart.
vi.mock('@/gql', () => ({ graphql: (source: string) => /(?:subscription|mutation) (\w+)/.exec(source)![1] }));

const { useSandboxExecutables } = await import('./sandbox-executables');

const exe = (name: string) => ({
  name,
  invocation: `${name} --version`,
  registered: false,
  probed: true,
  resolved: `/opt/homebrew/bin/${name}`,
  shim: false,
  target: '',
  ok: true,
  version: 'v1',
  error: '',
  refusal: null,
});
const listed = [exe('kubectl'), exe('helm')];

// The watch's last frame, folded through the reducer the hook handed over.
let report: unknown;
let lastReducer: ((prev: unknown, frames: unknown[]) => unknown) | undefined;
function pushFrame(probing: boolean, probes: number, executables = listed) {
  report = lastReducer!(report, [{ sandboxExecutablesStateWatch: { probing, probes, executables } }]);
}

beforeEach(() => {
  vi.clearAllMocks();
  report = undefined;
  useFoldedSubscriptionMock.mockImplementation((_args, reduce) => {
    lastReducer = reduce;
    return { data: report, connected: true };
  });
  ['SandboxExecutablesProbe', 'SandboxExecutableRegister', 'SandboxExecutableRemove'].forEach((doc) => {
    mutations[doc] = vi.fn(async () => ({ data: {} }));
  });
  useMutationMock.mockImplementation((doc: string) => [{}, mutations[doc]]);
});

/** A mutation that answers when the test says. */
function held(doc: string): (value: unknown) => void {
  let answer!: (value: unknown) => void;
  mutations[doc].mockImplementation(
    () =>
      new Promise((resolve) => {
        answer = resolve;
      }),
  );
  return (value) => answer(value);
}

describe('useSandboxExecutables', () => {
  it('reads the report off the watch, undefined until it answers', () => {
    const { result, rerender } = renderHook(() => useSandboxExecutables());
    expect(result.current.report).toBeUndefined();
    expect(result.current.probing).toBe(false);

    pushFrame(false, 0);
    rerender();
    expect(result.current.report).toEqual(listed);
  });

  it('probes: held down from the press until the watch counts the probe, whether the mutation has answered or not', async () => {
    const answer = held('SandboxExecutablesProbe');
    const { result, rerender } = renderHook(() => useSandboxExecutables());
    pushFrame(false, 3);
    rerender();

    act(() => {
      void result.current.probe();
    });
    expect(result.current.probing).toBe(true);

    await act(async () => answer({ data: { sandboxExecutablesProbe: listed } }));
    expect(result.current.probing).toBe(true);

    pushFrame(true, 4);
    rerender();
    expect(result.current.probing).toBe(true);

    pushFrame(false, 4);
    rerender();
    expect(result.current.probing).toBe(false);
  });

  it('is released by a probe whose start and end arrived as one frame', () => {
    const { result, rerender } = renderHook(() => useSandboxExecutables());
    pushFrame(false, 0);
    rerender();
    act(() => {
      void result.current.probe();
    });
    expect(result.current.probing).toBe(true);

    pushFrame(false, 1);
    rerender();
    expect(result.current.probing).toBe(false);
  });

  it("keeps a refused probe's message", async () => {
    mutations.SandboxExecutablesProbe.mockResolvedValue({
      error: { graphQLErrors: [{ message: 'This machine has no sandbox.' }] },
    });
    const { result } = renderHook(() => useSandboxExecutables());
    await act(async () => result.current.probe());
    expect(result.current.probeError).toBe('This machine has no sandbox.');
    expect(result.current.probing).toBe(false);
  });

  it('registers an executable, its invocation null when none was typed, and says whether it was taken', async () => {
    const { result } = renderHook(() => useSandboxExecutables());
    let taken = false;
    await act(async () => {
      taken = await result.current.register('k9s', '');
    });
    expect(mutations.SandboxExecutableRegister).toHaveBeenCalledWith({ name: 'k9s', invocation: null });
    expect(taken).toBe(true);

    mutations.SandboxExecutableRegister.mockResolvedValue({
      error: { graphQLErrors: [{ message: 'Kubetail probes kubectl already.' }] },
    });
    await act(async () => {
      taken = await result.current.register('kubectl', 'kubectl version');
    });
    expect(mutations.SandboxExecutableRegister).toHaveBeenLastCalledWith({
      name: 'kubectl',
      invocation: 'kubectl version',
    });
    expect(taken).toBe(false);
    expect(result.current.registerError).toBe('Kubetail probes kubectl already.');
  });

  it('counts a register that took though its answer could not be read as taken, and keeps its message', async () => {
    const message =
      'The executable was registered, but Kubetail could not read the settings back. Reopen Settings to see them.';
    mutations.SandboxExecutableRegister.mockResolvedValue({
      error: { graphQLErrors: [{ message, extensions: { code: 'KUBETAIL_SAVED_UNREAD' } }] },
    });
    const { result } = renderHook(() => useSandboxExecutables());
    let taken = false;
    await act(async () => {
      taken = await result.current.register('k9s', '');
    });
    expect(taken).toBe(true);
    expect(result.current.registerError).toBe(message);
  });

  it('removes an executable, in flight until it answers', async () => {
    const answer = held('SandboxExecutableRemove');
    const { result } = renderHook(() => useSandboxExecutables());

    act(() => {
      void result.current.remove('k9s');
    });
    expect(mutations.SandboxExecutableRemove).toHaveBeenCalledWith({ name: 'k9s' });
    expect(result.current.changing).toBe(true);

    await act(async () => answer({ error: { graphQLErrors: [{ message: 'That executable is not registered.' }] } }));
    expect(result.current.changing).toBe(false);
    expect(result.current.removeError).toBe('That executable is not registered.');
  });
});
