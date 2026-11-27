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
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

// The seams: the watch's frames, folded by the hook's own reducer, and the two
// mutations, answering what each test stages.
const { watch, setMock, resetMock } = vi.hoisted(() => ({
  watch: {} as { frames?: unknown[]; args?: { variables?: unknown; pause?: boolean }; options?: unknown },
  setMock: vi.fn(),
  resetMock: vi.fn(),
}));
vi.mock('@/lib/graphql/use-folded-subscription', () => ({
  perFrame:
    <D, R>(step: (prev: R | undefined, frame: D) => R) =>
    (prev: R | undefined, frames: D[]) =>
      frames.reduce<R | undefined>(step, prev),
  useFoldedSubscription: (
    args: { variables?: unknown; pause?: boolean },
    reduce: (prev: unknown, frames: unknown[]) => unknown,
    options: unknown,
  ) => {
    watch.args = args;
    watch.options = options;
    return { data: watch.frames && !args.pause ? reduce(undefined, watch.frames) : undefined, connected: true };
  },
}));
// The set mutation is the hook's first useMutation, the reset its second.
let mutations = 0;
vi.mock('urql', () => ({
  useMutation: () => {
    mutations += 1;
    return [{}, mutations % 2 === 1 ? setMock : resetMock];
  },
}));
vi.mock('@/gql', () => ({ graphql: () => ({}) }));

const { ChatSwitchProvider } = await import('./chat-switch');
const { useSandboxDefault, useSandboxDefaultChanging, useSandboxDefaultSwitch } = await import('./sandbox-default');

const wrapper = ({ children }: { children: ReactNode }) => <ChatSwitchProvider>{children}</ChatSwitchProvider>;

beforeEach(() => {
  vi.clearAllMocks();
  watch.frames = undefined;
  mutations = 0;
});

describe('useSandboxDefault', () => {
  it("is undefined until the first frame, then holds the scope's latest default", () => {
    const { result, rerender } = renderHook(() => useSandboxDefault('c1'));
    expect(result.current).toBeUndefined();

    watch.frames = [
      { sandboxDefaultStateWatch: { sandboxDisabled: false, clusterOverride: false } },
      { sandboxDefaultStateWatch: { sandboxDisabled: true, clusterOverride: true } },
    ];
    rerender();
    expect(result.current).toEqual({ sandboxDisabled: true, clusterOverride: true });
    expect(watch.args?.variables).toEqual({ clusterID: 'c1' });
    expect(watch.options).toEqual({ unshared: true });
  });

  it('reads every cluster with no cluster, and nothing while paused', () => {
    renderHook(() => useSandboxDefault(null));
    expect(watch.args?.variables).toEqual({ clusterID: null });

    watch.frames = [{ sandboxDefaultStateWatch: { sandboxDisabled: true, clusterOverride: false } }];
    const { result } = renderHook(() => useSandboxDefault(undefined));
    expect(watch.args?.pause).toBe(true);
    expect(result.current).toBeUndefined();
  });
});

describe('useSandboxDefaultSwitch', () => {
  // An answer held until the test lets it go.
  function deferred(mock: typeof setMock) {
    let resolve!: (value: unknown) => void;
    mock.mockReturnValueOnce(
      new Promise((r) => {
        resolve = r;
      }),
    );
    return (value: unknown) => act(async () => resolve(value));
  }

  const renderSwitch = (clusterID: string | null = 'c1') =>
    renderHook(
      () => ({
        change: useSandboxDefaultSwitch(clusterID),
        changing: useSandboxDefaultChanging('c1'),
        other: useSandboxDefaultChanging('c2'),
        none: useSandboxDefaultChanging(undefined),
      }),
      { wrapper },
    );

  it('writes the default, and is changing until the mutation answers', async () => {
    const answer = deferred(setMock);
    const { result } = renderSwitch();

    act(() => result.current.change.setSandboxDisabled(true));
    expect(setMock).toHaveBeenCalledWith({ clusterID: 'c1', sandboxDisabled: true });
    expect(result.current.change.switching).toBe(true);
    expect(result.current.changing).toBe(true);
    // Another cluster's default does not move with this one.
    expect(result.current.other).toBe(false);

    await answer({ data: { sandboxDefaultSet: { sandboxDisabled: true, clusterOverride: true } } });
    expect(result.current.change.switching).toBe(false);
    expect(result.current.changing).toBe(false);
  });

  it("resets the cluster's default to every cluster's", async () => {
    const answer = deferred(resetMock);
    const { result } = renderSwitch();

    act(() => result.current.change.reset(true));
    expect(resetMock).toHaveBeenCalledWith({ clusterID: 'c1', sandboxDisabled: true });
    expect(result.current.changing).toBe(true);

    await answer({ data: { sandboxDefaultReset: { sandboxDisabled: false, clusterOverride: false } } });
    expect(result.current.changing).toBe(false);
  });

  // The hold ends with the answer, whether it took or not.
  it('stops changing when the mutation fails', async () => {
    setMock.mockRejectedValue(new Error('sidecar unreachable'));
    const { result } = renderSwitch();

    await act(async () => {
      result.current.change.setSandboxDisabled(true);
    });
    expect(result.current.changing).toBe(false);
  });

  // Every cluster's default moves a window with no cluster yet, and every other.
  it("holds every scope while every cluster's default changes", async () => {
    const answer = deferred(setMock);
    const { result } = renderSwitch(null);

    act(() => result.current.change.setSandboxDisabled(true));
    expect(result.current.none).toBe(true);
    expect(result.current.changing).toBe(true);
    expect(result.current.other).toBe(true);
    await answer({ data: { sandboxDefaultSet: { sandboxDisabled: true, clusterOverride: false } } });
  });

  // The sidecar refuses a reset that would land elsewhere than its caller saw.
  it('says when a reset was refused because every cluster changed, until the next change', async () => {
    resetMock.mockResolvedValue({ error: { graphQLErrors: [{ extensions: { code: 'KUBETAIL_CONFLICT' } }] } });
    const { result } = renderSwitch();
    expect(result.current.change.everyClusterChanged).toBe(false);

    await act(async () => {
      result.current.change.reset(false);
    });
    expect(result.current.change.everyClusterChanged).toBe(true);
    expect(result.current.changing).toBe(false);

    setMock.mockResolvedValue({ data: {} });
    await act(async () => {
      result.current.change.setSandboxDisabled(false);
    });
    expect(result.current.change.everyClusterChanged).toBe(false);

    resetMock.mockResolvedValue({ data: {} });
    await act(async () => {
      result.current.change.reset(false);
    });
    expect(result.current.change.everyClusterChanged).toBe(false);
  });

  // Every cluster's default has nothing to go back to.
  it("resets nothing for every cluster's default", () => {
    const { result } = renderSwitch(null);
    act(() => result.current.change.reset(false));
    expect(resetMock).not.toHaveBeenCalled();
    expect(result.current.change.switching).toBe(false);
  });
});
