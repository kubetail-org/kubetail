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

const { useQueryMock, useMutationMock, reexecute, removeMock } = vi.hoisted(() => ({
  useQueryMock: vi.fn(),
  useMutationMock: vi.fn(),
  reexecute: vi.fn(),
  removeMock: vi.fn(),
}));
vi.mock('urql', () => ({ useQuery: useQueryMock, useMutation: useMutationMock }));
vi.mock('@/gql', () => ({ graphql: (source: string) => /(?:query|mutation) (\w+)/.exec(source)![1] }));

const { chatGrantsContext, useChatGrants } = await import('./chat-grants');

const rules = [{ id: 'r1', line: 'Allow cluster writes in dev / web' }];

beforeEach(() => {
  vi.clearAllMocks();
  useQueryMock.mockImplementation(() => [{ data: { chatPermissionRulesGet: rules } }, reexecute]);
  useMutationMock.mockImplementation(() => [{}, removeMock]);
});

describe('useChatGrants', () => {
  it("reads the chat's rules", () => {
    const { result } = renderHook(() => useChatGrants('c1'));
    expect(result.current.rules).toEqual(rules);
    expect(useQueryMock).toHaveBeenCalledWith(expect.objectContaining({ variables: { chatID: 'c1' } }));
  });

  // An approval that writes a chat's rule names the type, so the query, which
  // names it too, is asked again even while it holds none.
  it('is asked again when an approval writes a rule', () => {
    renderHook(() => useChatGrants('c1'));
    expect(useQueryMock).toHaveBeenCalledWith(expect.objectContaining({ context: chatGrantsContext }));
    expect(chatGrantsContext.additionalTypenames).toEqual(['PermissionRule']);
  });

  it('removes a rule, in flight until it answers, then asks again', async () => {
    let answer!: (value: unknown) => void;
    removeMock.mockImplementation(
      () =>
        new Promise((resolve) => {
          answer = resolve;
        }),
    );
    const { result } = renderHook(() => useChatGrants('c1'));
    act(() => {
      void result.current.remove('r1');
    });
    expect(removeMock).toHaveBeenCalledWith({ chatID: 'c1', id: 'r1' });
    expect(result.current.removing).toBe('r1');

    await act(async () => answer({ data: { chatPermissionRuleRemove: [] } }));
    expect(result.current.removing).toBeNull();
    expect(reexecute).toHaveBeenCalledWith({ requestPolicy: 'network-only' });
  });

  it("keeps a refused removal's reason", async () => {
    removeMock.mockResolvedValue({ error: { graphQLErrors: [{ message: 'not found' }] } });
    const { result } = renderHook(() => useChatGrants('c1'));
    await act(async () => result.current.remove('r1'));
    expect(result.current.error).toBe('not found');
  });

  it('is asked again when the window takes focus', () => {
    renderHook(() => useChatGrants('c1'));
    act(() => {
      window.dispatchEvent(new Event('focus'));
    });
    expect(reexecute).toHaveBeenCalledWith({ requestPolicy: 'network-only' });
  });
});
