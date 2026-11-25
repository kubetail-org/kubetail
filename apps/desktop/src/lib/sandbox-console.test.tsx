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

const { useSandboxConsole } = await import('./sandbox-console');

const run = (number: number, over: object = {}) => ({
  number,
  command: `echo ${number}`,
  clusterID: null,
  network: false,
  running: true,
  output: '',
  discarded: 0,
  exitCode: null,
  notice: null,
  durationMs: 0,
  ...over,
});

// The watch's folded value, through the reducer the hook handed over.
let folded: unknown;
let generation = 1;
let lastReducer: ((prev: unknown, frames: unknown[]) => unknown) | undefined;
function pushFrames(...runs: (ReturnType<typeof run> | null)[]) {
  folded = lastReducer!(
    folded,
    runs.map((r) => ({ sandboxConsoleStateWatch: { run: r } })),
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  folded = undefined;
  generation = 1;
  useFoldedSubscriptionMock.mockImplementation((_args, reduce) => {
    lastReducer = reduce;
    return { data: folded, connected: true, generation };
  });
  mutations.SandboxConsoleRun = vi.fn(async () => ({ data: { sandboxConsoleRun: 1 } }));
  mutations.SandboxConsoleStop = vi.fn(async () => ({ data: { sandboxConsoleStop: true } }));
  useMutationMock.mockImplementation((doc: string) => [{}, mutations[doc]]);
});

/** Holds the run mutation until the test answers it. */
function heldRun(): (value: unknown) => void {
  let answer!: (value: unknown) => void;
  mutations.SandboxConsoleRun.mockImplementation(
    () =>
      new Promise((resolve) => {
        answer = resolve;
      }),
  );
  return (value) => answer(value);
}

/** The answer of a run that started command number. */
const startedAs = (number: number) => ({ data: { sandboxConsoleRun: number } });

describe('useSandboxConsole', () => {
  it('holds the watch paused while the console is closed', () => {
    renderHook(() => useSandboxConsole(false));
    expect(useFoldedSubscriptionMock.mock.calls[0][0]).toMatchObject({ pause: true });
  });

  it('folds each command by its number, oldest first, its latest frame winning', () => {
    const { result, rerender } = renderHook(() => useSandboxConsole(true));
    expect(result.current.runs).toEqual([]);

    pushFrames(null, run(1), run(1, { output: 'a' }));
    rerender();
    expect(result.current.runs).toEqual([run(1, { output: 'a' })]);
    expect(result.current.running).toBe(true);

    pushFrames(run(1, { running: false, exitCode: 0 }), run(2));
    rerender();
    expect(result.current.runs.map((r) => [r.number, r.running])).toEqual([
      [1, false],
      [2, true],
    ]);

    pushFrames(run(2, { running: false, exitCode: 1 }));
    rerender();
    expect(result.current.running).toBe(false);
  });

  it('is running from the press until the watch shows the command the run started', async () => {
    const answer = heldRun();
    const { result, rerender } = renderHook(() => useSandboxConsole(true));

    let started!: Promise<boolean>;
    act(() => {
      started = result.current.run('ls ~', '7', true);
    });
    expect(result.current.running).toBe(true);
    expect(mutations.SandboxConsoleRun).toHaveBeenCalledWith({ command: 'ls ~', clusterID: '7', network: true });

    // The snapshot the watch sends as it subscribes: an older command.
    pushFrames(run(5, { running: false, exitCode: 0 }));
    rerender();
    expect(result.current.running).toBe(true);

    await act(async () => answer(startedAs(6)));
    expect(await started).toBe(true);
    expect(result.current.running).toBe(true);
    expect(result.current.error).toBeNull();

    pushFrames(run(6, { running: false, exitCode: 0 }));
    rerender();
    expect(result.current.running).toBe(false);
  });

  it('drops the wait on a new connection, as a restarted sidecar numbers afresh', async () => {
    mutations.SandboxConsoleRun.mockResolvedValueOnce(startedAs(6));
    const { result, rerender } = renderHook(() => useSandboxConsole(true));
    await act(async () => {
      await result.current.run('true', null, false);
    });
    expect(result.current.running).toBe(true);

    generation = 2;
    folded = undefined;
    pushFrames(null);
    rerender();
    expect(result.current.running).toBe(false);
  });

  it('keeps a refusal until the next run, and drops it when the console closes', async () => {
    mutations.SandboxConsoleRun.mockResolvedValueOnce({
      error: { graphQLErrors: [{ message: 'a console command is already running' }], message: 'x' },
    });
    let open = true;
    const { result, rerender } = renderHook(() => useSandboxConsole(open));

    await act(async () => {
      expect(await result.current.run('ls', null, false)).toBe(false);
    });
    expect(result.current.error).toBe('a console command is already running');
    expect(result.current.running).toBe(false);

    await act(async () => {
      await result.current.run('ls', null, false);
    });
    expect(result.current.error).toBeNull();

    mutations.SandboxConsoleRun.mockResolvedValueOnce({ error: { graphQLErrors: [], message: 'gone' } });
    await act(async () => {
      await result.current.run('ls', null, false);
    });
    expect(result.current.error).toBe('gone');
    open = false;
    rerender();
    expect(result.current.error).toBeNull();
  });

  it('refuses a second run while one is asked for', async () => {
    const answer = heldRun();
    const { result } = renderHook(() => useSandboxConsole(true));
    let first!: Promise<boolean>;
    act(() => {
      first = result.current.run('ls', null, false);
    });

    let second!: boolean;
    await act(async () => {
      second = await result.current.run('pwd', null, false);
    });
    expect(second).toBe(false);
    expect(mutations.SandboxConsoleRun).toHaveBeenCalledTimes(1);

    await act(async () => answer(startedAs(1)));
    expect(await first).toBe(true);
  });

  it('stops whichever command runs', () => {
    const { result } = renderHook(() => useSandboxConsole(true));
    act(() => result.current.stop());
    expect(mutations.SandboxConsoleStop).toHaveBeenCalledWith({ number: null });
  });

  it('stops a command asked for while its run was pending once it starts, by its number', async () => {
    const answer = heldRun();
    const { result } = renderHook(() => useSandboxConsole(true));
    let started!: Promise<boolean>;
    act(() => {
      started = result.current.run('sleep 60', null, false);
    });
    act(() => result.current.stop());

    await act(async () => answer(startedAs(7)));
    await started;
    expect(mutations.SandboxConsoleStop.mock.calls).toEqual([[{ number: null }], [{ number: 7 }]]);
  });

  it('sends no second stop when the pending run is refused, or was never stopped', async () => {
    let answer = heldRun();
    const { result } = renderHook(() => useSandboxConsole(true));
    let started!: Promise<boolean>;
    act(() => {
      started = result.current.run('ls', null, false);
    });
    act(() => result.current.stop());
    await act(async () => answer({ error: { graphQLErrors: [{ message: 'busy' }], message: 'x' } }));
    await started;
    expect(mutations.SandboxConsoleStop).toHaveBeenCalledTimes(1);

    answer = heldRun();
    act(() => {
      started = result.current.run('ls', null, false);
    });
    await act(async () => answer(startedAs(2)));
    await started;
    expect(mutations.SandboxConsoleStop).toHaveBeenCalledTimes(1);
  });

  it('stops its own command when it unmounts, by number, and nothing when it started none', async () => {
    const idle = renderHook(() => useSandboxConsole(true));
    idle.unmount();
    expect(mutations.SandboxConsoleStop).not.toHaveBeenCalled();

    mutations.SandboxConsoleRun.mockResolvedValueOnce(startedAs(3));
    const { result, unmount } = renderHook(() => useSandboxConsole(true));
    await act(async () => {
      await result.current.run('sleep 60', null, false);
    });
    unmount();
    expect(mutations.SandboxConsoleStop).toHaveBeenCalledWith({ number: 3 });
  });

  it('stops a run still asked for when it unmounts, once it starts', async () => {
    const answer = heldRun();
    const { result, unmount } = renderHook(() => useSandboxConsole(true));
    let started!: Promise<boolean>;
    act(() => {
      started = result.current.run('sleep 60', null, false);
    });
    unmount();
    expect(mutations.SandboxConsoleStop).not.toHaveBeenCalled();

    await act(async () => answer(startedAs(4)));
    await started;
    expect(mutations.SandboxConsoleStop).toHaveBeenCalledWith({ number: 4 });
  });
});
