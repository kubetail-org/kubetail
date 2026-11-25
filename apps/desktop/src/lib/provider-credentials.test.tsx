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

import type { ProviderCredentialState } from './provider-credentials';

// The seams: the watch's frames, folded by the hook's own reducer, and the
// client's mutation, answering the result each test stages.
const { watch, client } = vi.hoisted(() => {
  const frames: { frames?: unknown[]; options?: unknown } = {};
  return { watch: frames, client: { mutation: vi.fn() } };
});
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
vi.mock('@/gql', () => ({
  graphql: (source: string) => ({ name: /(?:mutation|subscription) (\w+)/.exec(source)![1] }),
}));
vi.mock('urql', () => ({ useClient: () => client }));

const { useProviderCredentials } = await import('./provider-credentials');

function credential(c: Partial<ProviderCredentialState> = {}): ProviderCredentialState {
  return {
    id: 'groq',
    label: 'Groq',
    keyVariable: 'GROQ_API_KEY',
    source: 'None',
    hasEnvironmentKey: false,
    hasSavedKey: false,
    failure: null,
    ...c,
  };
}

// answers makes the next mutation fail with code, '' for a failure with no
// code, or succeed with none.
function answers(code?: string) {
  const error = code === undefined ? undefined : { graphQLErrors: code ? [{ extensions: { code } }] : [] };
  client.mutation.mockReturnValueOnce({ toPromise: () => Promise.resolve({ data: error ? undefined : {}, error }) });
}

beforeEach(() => {
  watch.frames = undefined;
  client.mutation.mockReset();
});

describe('useProviderCredentials', () => {
  it('has not loaded before the first frame, then holds the latest list', () => {
    const { result, rerender } = renderHook(() => useProviderCredentials());
    expect(result.current).toMatchObject({ credentials: [], snapshotComplete: false });

    watch.frames = [
      { providerCredentialsStateWatch: [credential()] },
      { providerCredentialsStateWatch: [credential({ source: 'SavedKey', hasSavedKey: true })] },
    ];
    rerender();
    expect(result.current.snapshotComplete).toBe(true);
    expect(result.current.credentials).toEqual([credential({ source: 'SavedKey', hasSavedKey: true })]);
    expect(watch.options).toEqual({ unshared: true });
  });

  it.each([
    [undefined, 'saved'],
    ['KUBETAIL_VALIDATION_ERROR', 'bad-key'],
    ['KUBETAIL_PROVIDER_KEY_NOT_SAVED', 'not-saved'],
    ['INTERNAL_SERVER_ERROR', 'failed'],
    ['', 'failed'],
  ] as const)('maps a save answering %s to %s', async (code, outcome) => {
    const { result } = renderHook(() => useProviderCredentials());
    answers(code);
    await expect(result.current.saveKey('groq', 'gsk-0123456789abcdef')).resolves.toBe(outcome);
    expect(client.mutation).toHaveBeenCalledWith(
      { name: 'ProviderKeySave' },
      { providerID: 'groq', key: 'gsk-0123456789abcdef' },
    );
  });

  it('removes a key and says whether it went', async () => {
    const { result } = renderHook(() => useProviderCredentials());
    answers();
    await expect(result.current.deleteKey('groq')).resolves.toBe(true);
    answers('KUBETAIL_PROVIDER_KEY_NOT_DELETED');
    await expect(result.current.deleteKey('groq')).resolves.toBe(false);
    expect(client.mutation).toHaveBeenLastCalledWith({ name: 'ProviderKeyDelete' }, { providerID: 'groq' });
  });
});
