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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Column } from './columns';
import { MIN_MESSAGE_PX, useMessageWidth } from './message-width';
import type { WidthKey } from './widths';

let resize: () => void = () => {};

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame', 'setTimeout'] });
  // A browser's observer reports once on attach; the test fires the rest by hand.
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(private readonly callback: () => void) {
        resize = callback;
      }

      observe() {
        this.callback();
      }

      disconnect() {}
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function scrollerOf(clientWidth: number) {
  const el = document.createElement('div');
  let width = clientWidth;
  Object.defineProperty(el, 'clientWidth', { get: () => width });
  return {
    el,
    setWidth: (w: number) => {
      width = w;
    },
  };
}

const frame = () =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(16);
  });

const widths = new Map<WidthKey, number>([
  ['Pos', 48],
  ['Timestamp', 100],
  ['Pod', 80],
  ['Message', 900],
]);
const columns: Column[] = ['Timestamp', 'Message'];

describe('useMessageWidth', () => {
  it('is the scroller’s width less the position and the other visible columns', async () => {
    const { el } = scrollerOf(600);
    const { result } = renderHook(() => useMessageWidth(el, widths, columns, true));
    await frame();
    // The pod is hidden, so its width does not count.
    expect(result.current).toBe(600 - 48 - 100);
  });

  it('follows the scroller as it resizes, once per frame', async () => {
    const scroller = scrollerOf(600);
    const { result } = renderHook(() => useMessageWidth(scroller.el, widths, columns, true));
    await frame();
    scroller.setWidth(800);
    act(() => resize());
    expect(result.current).toBe(600 - 148);
    await frame();
    expect(result.current).toBe(800 - 148);
  });

  it('observes nothing while wrap is off', async () => {
    const observe = vi.spyOn(ResizeObserver.prototype, 'observe');
    const { el } = scrollerOf(600);
    renderHook(() => useMessageWidth(el, widths, columns, false));
    await frame();
    expect(observe).not.toHaveBeenCalled();
  });

  it('never goes under its floor', async () => {
    const { el } = scrollerOf(120);
    const { result } = renderHook(() => useMessageWidth(el, widths, columns, true));
    await frame();
    expect(result.current).toBe(MIN_MESSAGE_PX);
  });
});
