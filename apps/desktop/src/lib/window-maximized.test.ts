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

import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { MAC_USER_AGENT, NON_MAC_USER_AGENT, restoreUserAgent, setUserAgent } from '@/test-utils';

// Mocks ---------------------------------------------------------------

// Tauri has no maximize event, so the hook re-queries on every resize; the fake
// keeps the resize callback so a test can fire one.
const { isMaximized, onResized, unlisten } = vi.hoisted(() => ({
  isMaximized: vi.fn<() => Promise<boolean>>(),
  onResized: vi.fn<(cb: () => void) => Promise<() => void>>(),
  unlisten: vi.fn(),
}));
vi.mock('@tauri-apps/api/window', () => ({ getCurrentWindow: () => ({ isMaximized, onResized }) }));

const { useWindowMaximized } = await import('./window-maximized');

// Helpers -------------------------------------------------------------

// The resize callback the hook registered, so a test can fire one.
let resizedCallback: (() => void) | undefined;

beforeEach(() => {
  vi.clearAllMocks();
  isMaximized.mockResolvedValue(false);
  onResized.mockImplementation((cb) => {
    resizedCallback = cb;
    return Promise.resolve(unlisten);
  });
  setUserAgent(NON_MAC_USER_AGENT);
});

afterEach(() => {
  restoreUserAgent();
});

// Tests ---------------------------------------------------------------

describe('useWindowMaximized', () => {
  it('reads the window’s state on mount', async () => {
    isMaximized.mockResolvedValue(true);
    const { result } = renderHook(() => useWindowMaximized());
    // False until the first query answers.
    expect(result.current).toBe(false);
    await waitFor(() => expect(result.current).toBe(true));
  });

  it('re-queries on every resize, the only signal Tauri gives', async () => {
    const { result } = renderHook(() => useWindowMaximized());
    await waitFor(() => expect(isMaximized).toHaveBeenCalledTimes(1));

    isMaximized.mockResolvedValue(true);
    await act(async () => {
      resizedCallback?.();
    });
    await waitFor(() => expect(result.current).toBe(true));
  });

  it('stays false on macOS, which keeps its native decorations', () => {
    setUserAgent(MAC_USER_AGENT);
    const { result } = renderHook(() => useWindowMaximized());
    expect(result.current).toBe(false);
    expect(isMaximized).not.toHaveBeenCalled();
    expect(onResized).not.toHaveBeenCalled();
  });

  it('stops listening on unmount', async () => {
    const { unmount } = renderHook(() => useWindowMaximized());
    await waitFor(() => expect(onResized).toHaveBeenCalled());

    unmount();
    expect(unlisten).toHaveBeenCalledTimes(1);
  });

  it('drops a resize registration that lands after unmount', async () => {
    let resolveListen!: (fn: () => void) => void;
    onResized.mockImplementation(
      () =>
        new Promise<() => void>((resolve) => {
          resolveListen = resolve;
        }),
    );
    const { unmount } = renderHook(() => useWindowMaximized());
    unmount();

    // Registration resolving into an unmounted hook must undo itself.
    await act(async () => {
      resolveListen(unlisten);
    });
    expect(unlisten).toHaveBeenCalledTimes(1);
  });

  it('ignores a failed query rather than reporting it', async () => {
    isMaximized.mockRejectedValue(new Error('no window'));
    const { result } = renderHook(() => useWindowMaximized());
    await waitFor(() => expect(isMaximized).toHaveBeenCalled());
    expect(result.current).toBe(false);
  });
});
