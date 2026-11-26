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
import { beforeEach, describe, expect, it, vi } from 'vitest';

// The seams: the watch's frames, folded by the hook's own reducer, and the
// mutation, answering what each test stages.
const { watch, useMutationMock, setMutation } = vi.hoisted(() => ({
  watch: {} as { frames?: unknown[]; options?: unknown },
  useMutationMock: vi.fn(),
  setMutation: vi.fn(),
}));
vi.mock('@/lib/graphql/use-folded-subscription', () => ({
  perFrame:
    <D, R>(step: (prev: R | undefined, frame: D) => R) =>
    (prev: R | undefined, frames: D[]) =>
      frames.reduce<R | undefined>(step, prev),
  useFoldedSubscription: (_args: unknown, reduce: (prev: unknown, frames: unknown[]) => unknown, options: unknown) => {
    watch.options = options;
    return { data: watch.frames ? reduce(undefined, watch.frames) : undefined, connected: true };
  },
}));
vi.mock('urql', () => ({ useMutation: useMutationMock }));
vi.mock('@/gql', () => ({ graphql: () => ({}) }));

const { useToolSettings } = await import('./tool-settings');

const listed = [
  {
    name: 'Bash',
    label: 'Bash',
    group: 'Commands',
    description: 'Run shell commands.',
    enabled: true,
    alwaysOn: false,
  },
  {
    name: 'WebFetch',
    label: 'WebFetch',
    group: 'Web',
    description: 'Fetch a web page.',
    enabled: false,
    alwaysOn: false,
  },
];

beforeEach(() => {
  vi.clearAllMocks();
  watch.frames = undefined;
  useMutationMock.mockImplementation(() => [{}, setMutation]);
});

describe('useToolSettings', () => {
  it('is undefined until the first frame, then holds the latest list', () => {
    const { result, rerender } = renderHook(() => useToolSettings());
    expect(result.current.tools).toBeUndefined();

    watch.frames = [{ toolSettingsStateWatch: [] }, { toolSettingsStateWatch: listed }];
    rerender();
    expect(result.current.tools).toEqual(listed);
    expect(watch.options).toEqual({ unshared: true });
  });

  it('sets a tool, in flight until it answers', async () => {
    let answer!: (value: unknown) => void;
    setMutation.mockImplementation(
      () =>
        new Promise((resolve) => {
          answer = resolve;
        }),
    );
    const { result } = renderHook(() => useToolSettings());

    act(() => {
      void result.current.set('Bash', false);
    });
    expect(setMutation).toHaveBeenCalledWith({ name: 'Bash', enabled: false });
    expect(result.current.changing).toEqual(new Set(['Bash']));

    await act(async () => answer({ data: { toolSettingSet: [] } }));
    expect(result.current.changing).toEqual(new Set());
    expect(result.current.error).toBeNull();
  });

  it("keeps a refused change's message until the next one starts", async () => {
    setMutation.mockResolvedValue({ error: { graphQLErrors: [{ message: 'No listed tool has that name.' }] } });
    const { result } = renderHook(() => useToolSettings());
    await act(async () => result.current.set('Nope', true));
    expect(result.current.error).toBe('No listed tool has that name.');

    setMutation.mockResolvedValue({ data: { toolSettingSet: [] } });
    await act(async () => result.current.set('Bash', true));
    expect(result.current.error).toBeNull();
  });
});
