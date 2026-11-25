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

import { act, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { Elapsed } from './elapsed';

describe('Elapsed', () => {
  beforeEach(() => {
    vi.useFakeTimers({ now: 42_000 });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('reads the time since its start and grows with the clock', () => {
    const { container } = render(<Elapsed since={0} />);
    expect(container).toHaveTextContent('42s');
    act(() => {
      vi.advanceTimersByTime(10 * 60_000);
    });
    expect(container).toHaveTextContent('10m');
  });

  it('stops its clock when it unmounts', () => {
    render(<Elapsed since={0} />).unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
