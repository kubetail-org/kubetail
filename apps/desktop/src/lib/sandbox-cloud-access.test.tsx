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

const { state, useQueryMock } = vi.hoisted(() => ({ state: { current: {} }, useQueryMock: vi.fn() }));
vi.mock('urql', () => ({ useQuery: useQueryMock }));
vi.mock('@/gql', () => ({ graphql: () => ({}) }));

const { useSandboxCloudAccess } = await import('./sandbox-cloud-access');

beforeEach(() => {
  state.current = { fetching: true };
  useQueryMock.mockImplementation(() => [state.current, vi.fn()]);
});

describe('useSandboxCloudAccess', () => {
  it('is undefined until the sidecar answers', () => {
    expect(renderHook(() => useSandboxCloudAccess()).result.current).toEqual({
      platforms: undefined,
      readFailed: false,
    });
  });

  it("answers the platforms sandboxed commands may reach, in the sidecar's order", () => {
    state.current = {
      fetching: false,
      data: {
        sandboxCloudAccessGet: [
          { target: 'AWS', on: true },
          { target: 'GitHub', on: false },
          { target: 'GCP', on: true },
        ],
      },
    };
    expect(renderHook(() => useSandboxCloudAccess()).result.current).toEqual({
      platforms: ['AWS', 'GCP'],
      readFailed: false,
    });
  });

  it('answers every platform off as an empty list, not a failure', () => {
    state.current = { fetching: false, data: { sandboxCloudAccessGet: [{ target: 'AWS', on: false }] } };
    expect(renderHook(() => useSandboxCloudAccess()).result.current).toEqual({ platforms: [], readFailed: false });
  });

  it('says the read failed, and answers no platforms', () => {
    state.current = { fetching: false, error: new Error('could not read the switches') };
    expect(renderHook(() => useSandboxCloudAccess()).result.current).toEqual({
      platforms: undefined,
      readFailed: true,
    });
  });

  it('prefers a failed read over the platforms an earlier one answered', () => {
    state.current = {
      fetching: false,
      data: { sandboxCloudAccessGet: [{ target: 'AWS', on: true }] },
      error: new Error('could not read the switches'),
    };
    expect(renderHook(() => useSandboxCloudAccess()).result.current).toEqual({
      platforms: undefined,
      readFailed: true,
    });
  });
});
