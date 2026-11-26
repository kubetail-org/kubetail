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

import { act, render, screen } from '@testing-library/react';
import { useRef } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useHeldStill } from '@/lib/held-still';

function Probe({ ms }: { ms: number }) {
  const ref = useRef<HTMLDivElement>(null);
  const held = useHeldStill(ref, ms);
  return <div ref={ref} data-testid="probe" data-held={String(held)} />;
}

const held = () => screen.getByTestId('probe').dataset.held;

describe('useHeldStill', () => {
  // Where the element is on screen: a test moves it by changing top.
  let top = 0;

  beforeEach(() => {
    vi.useFakeTimers();
    top = 0;
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => new DOMRect(0, top, 100, 20));
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('holds once the element has kept its place for the whole wait', async () => {
    render(<Probe ms={500} />);
    expect(held()).toBe('false');

    await act(() => vi.advanceTimersByTimeAsync(400));
    expect(held()).toBe('false');

    await act(() => vi.advanceTimersByTimeAsync(200));
    expect(held()).toBe('true');
  });

  it('starts the wait again when the element moves', async () => {
    render(<Probe ms={500} />);
    await act(() => vi.advanceTimersByTimeAsync(600));
    expect(held()).toBe('true');

    top = 40;
    await act(() => vi.advanceTimersByTimeAsync(50));
    expect(held()).toBe('false');

    await act(() => vi.advanceTimersByTimeAsync(600));
    expect(held()).toBe('true');
  });

  it('holds at once with no wait', () => {
    render(<Probe ms={0} />);
    expect(held()).toBe('true');
  });
});
