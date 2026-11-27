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

import { act, render, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ChatSwitchProvider, useChatSwitch, usePendingSwitch, useSwitchingAny } from './chat-switch';

const commit = vi.fn<(value: boolean) => Promise<boolean | undefined>>();

// The commit's answer, held until the test lets it go.
function deferred() {
  let resolve!: (result: boolean | undefined) => void;
  commit.mockReturnValueOnce(
    new Promise((r) => {
      resolve = r;
    }),
  );
  return (result: boolean | undefined) => act(async () => resolve(result));
}

const wrapper = ({ children }: { children: ReactNode }) => <ChatSwitchProvider>{children}</ChatSwitchProvider>;

beforeEach(() => {
  vi.clearAllMocks();
});

describe('useChatSwitch', () => {
  const renderSwitch = (watched: boolean | undefined) =>
    renderHook(({ value }) => useChatSwitch('sandbox:c1', value, commit), {
      wrapper,
      initialProps: { value: watched },
    });

  it('commits the switch, and is switching until the watch shows what it committed', async () => {
    const answer = deferred();
    const { result, rerender } = renderSwitch(false);
    expect(result.current.switching).toBe(false);

    act(() => {
      result.current.switchTo(true);
    });
    expect(commit).toHaveBeenCalledWith(true);
    expect(result.current.switching).toBe(true);

    // The mutation's answer comes before the watch's frame: a send now would pass
    // the old value.
    await answer(true);
    expect(result.current.switching).toBe(true);

    rerender({ value: true });
    expect(result.current.switching).toBe(false);

    // Settled: a later switch from another window is not this one's to wait on.
    rerender({ value: false });
    expect(result.current.switching).toBe(false);
  });

  it('stops switching once the mutation answers when the watch got there first', async () => {
    const answer = deferred();
    const { result, rerender } = renderSwitch(false);
    act(() => {
      result.current.switchTo(true);
    });
    rerender({ value: true });
    expect(result.current.switching).toBe(true);

    await answer(true);
    expect(result.current.switching).toBe(false);
  });

  it('stops switching when the mutation fails', async () => {
    const answer = deferred();
    const { result } = renderSwitch(false);
    act(() => {
      result.current.switchTo(true);
    });

    await answer(undefined);
    expect(result.current.switching).toBe(false);
  });

  // The pane remounts on navigation, and the mutation it started keeps going.
  it('is still switching in a pane that remounts meanwhile', async () => {
    const answer = deferred();
    const seen: boolean[] = [];
    function Pane({ value }: { value: boolean }) {
      const { switching, switchTo } = useChatSwitch('sandbox:c1', value, commit);
      seen.push(switching);
      return (
        <button type="button" onClick={() => switchTo(true)}>
          switch
        </button>
      );
    }
    const view = render(
      <ChatSwitchProvider>
        <Pane key="a" value={false} />
      </ChatSwitchProvider>,
    );
    act(() => view.getByRole('button').click());

    view.rerender(
      <ChatSwitchProvider>
        <Pane key="b" value={false} />
      </ChatSwitchProvider>,
    );
    expect(seen.at(-1)).toBe(true);

    await answer(true);
    expect(seen.at(-1)).toBe(true);

    view.rerender(
      <ChatSwitchProvider>
        <Pane key="b" value />
      </ChatSwitchProvider>,
    );
    expect(seen.at(-1)).toBe(false);
  });

  // A pane that mounts after the answer opens its own watch, whose snapshot
  // already holds the commit.
  it('leaves a committed switch behind with the pane that started it', async () => {
    const answer = deferred();
    const seen: boolean[] = [];
    function Pane({ value }: { value: boolean }) {
      const { switching, switchTo } = useChatSwitch('sandbox:c1', value, commit);
      seen.push(switching);
      return (
        <button type="button" onClick={() => switchTo(true)}>
          switch
        </button>
      );
    }
    const view = render(
      <ChatSwitchProvider>
        <Pane key="a" value={false} />
      </ChatSwitchProvider>,
    );
    act(() => view.getByRole('button').click());
    await answer(true);

    view.rerender(
      <ChatSwitchProvider>
        <Pane key="b" value={false} />
      </ChatSwitchProvider>,
    );
    expect(seen.at(-1)).toBe(false);
  });

  // Each switch of each chat waits on its own key.
  it("leaves another switch's wait alone", () => {
    deferred();
    const { result } = renderHook(
      () => ({
        sandbox: useChatSwitch('sandbox:c1', false, commit),
        network: useChatSwitch('network:c1', false, commit),
        other: useChatSwitch('sandbox:c2', false, commit),
      }),
      { wrapper },
    );
    act(() => {
      result.current.sandbox.switchTo(true);
    });
    expect(result.current.sandbox.switching).toBe(true);
    expect(result.current.network.switching).toBe(false);
    expect(result.current.other.switching).toBe(false);
  });
});

describe('useSwitchingAny', () => {
  it('is whether any switch it names is in flight', async () => {
    const release = deferred();
    const { result } = renderHook(
      () => ({
        change: useChatSwitch('default:c1', false, commit),
        named: useSwitchingAny(['default:', 'default:c1']),
        other: useSwitchingAny(['default:c2']),
      }),
      { wrapper },
    );
    expect(result.current.named).toBe(false);
    act(() => result.current.change.switchTo(true));
    expect(result.current.named).toBe(true);
    expect(result.current.other).toBe(false);
    await release(undefined);
    expect(result.current.named).toBe(false);
  });

  // A pending switch is held until its mutation answers, even once the hook
  // that started it has gone.
  it('holds a pending switch until its mutation answers', async () => {
    let answer!: () => void;
    const mutation = () =>
      new Promise<void>((resolve) => {
        answer = resolve;
      });
    const { result, rerender } = renderHook(
      ({ mounted }) => ({
        started: mounted ? usePendingSwitch('default:c1') : null,
        held: useSwitchingAny(['default:c1']),
      }),
      { wrapper, initialProps: { mounted: true } },
    );
    act(() => result.current.started?.run(mutation));
    expect(result.current.started?.pending).toBe(true);
    rerender({ mounted: false });
    expect(result.current.held).toBe(true);
    await act(async () => answer());
    expect(result.current.held).toBe(false);
  });

  it('refuses to work outside its provider', () => {
    // React reports the throw on the console as well.
    const quiet = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => renderHook(() => useSwitchingAny(['default:']))).toThrow(/ChatSwitchProvider/);
    expect(() => renderHook(() => usePendingSwitch('default:'))).toThrow(/ChatSwitchProvider/);
    quiet.mockRestore();
  });
});
