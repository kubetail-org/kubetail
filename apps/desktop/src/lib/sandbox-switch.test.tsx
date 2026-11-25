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

const { setMock } = vi.hoisted(() => ({ setMock: vi.fn() }));
vi.mock('urql', () => ({ useMutation: () => [{}, setMock] }));
vi.mock('@/gql', () => ({ graphql: () => ({}) }));

const { ChatSwitchProvider } = await import('./chat-switch');
const { useSandboxSwitch } = await import('./sandbox-switch');

const wrapper = ({ children }: { children: ReactNode }) => <ChatSwitchProvider>{children}</ChatSwitchProvider>;

beforeEach(() => {
  vi.clearAllMocks();
});

describe('useSandboxSwitch', () => {
  const renderSwitch = (watched: boolean | undefined) =>
    renderHook(({ outside }) => useSandboxSwitch('c1', outside), { wrapper, initialProps: { outside: watched } });

  it("sets the chat's switch, and is switching until the watch shows what it committed", async () => {
    setMock.mockResolvedValue({ data: { chatSandboxDisabledSet: { id: 'c1', sandboxDisabled: true } } });
    const { result, rerender } = renderSwitch(false);

    await act(async () => {
      result.current.setSandboxDisabled(true);
    });
    expect(setMock).toHaveBeenCalledWith({ id: 'c1', sandboxDisabled: true });
    expect(result.current.switching).toBe(true);

    rerender({ outside: true });
    expect(result.current.switching).toBe(false);
  });

  it('stops switching when the mutation fails', async () => {
    setMock.mockResolvedValue({ error: { networkError: new Error('sidecar unreachable'), graphQLErrors: [] } });
    const { result } = renderSwitch(false);

    await act(async () => {
      result.current.setSandboxDisabled(true);
    });
    expect(result.current.switching).toBe(false);
  });
});
