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
import { describe, expect, it, vi } from 'vitest';

import { useAskAgainWhenLive } from './ask-again-when-live';

describe('useAskAgainWhenLive', () => {
  // Asking clears the failure and the answer sets it, so asking on every failure
  // would be a request loop.
  it('asks again once per live watch', () => {
    const retry = vi.fn();
    const { rerender } = renderHook(({ failed, live }) => useAskAgainWhenLive(failed, live, retry), {
      initialProps: { failed: true, live: false },
    });
    expect(retry).not.toHaveBeenCalled();

    rerender({ failed: true, live: true });
    expect(retry).toHaveBeenCalledTimes(1);
    rerender({ failed: false, live: true });
    rerender({ failed: true, live: true });
    expect(retry).toHaveBeenCalledTimes(1);

    // The watch going live again is the evidence the sidecar answers again.
    rerender({ failed: true, live: false });
    rerender({ failed: true, live: true });
    expect(retry).toHaveBeenCalledTimes(2);
  });

  it('asks nothing while nothing has failed', () => {
    const retry = vi.fn();
    renderHook(() => useAskAgainWhenLive(false, true, retry));
    expect(retry).not.toHaveBeenCalled();
  });
});
