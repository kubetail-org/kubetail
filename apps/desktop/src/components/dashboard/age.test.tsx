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
import { afterEach, describe, expect, it, vi } from 'vitest';

import { Age, ageSortValue, timestampMs } from './age';

afterEach(() => vi.useRealTimers());

describe('Age', () => {
  it('draws the largest unit alone, and ticks', () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.parse('2026-07-20T10:00:00Z'));
    render(<Age ms={Date.parse('2026-07-20T09:59:15Z')} />);
    expect(screen.getByText('45s').parentElement).toHaveClass('text-xs', 'text-muted-foreground');
    // Each tick schedules the next as it draws.
    for (let i = 0; i < 20; i += 1) {
      act(() => {
        vi.advanceTimersByTime(1000);
      });
    }
    expect(screen.getByText('1m')).toBeInTheDocument();
  });

  it('draws a dash for a time that did not parse, in the caller’s style', () => {
    const { container } = render(<Age ms={Number.NaN} className="text-sm" />);
    expect(container.firstElementChild).toHaveTextContent('—');
    expect(container.firstElementChild).toHaveClass('text-sm');
  });
});

describe('timestampMs', () => {
  it('reads a timestamp, and NaN for none', () => {
    expect(timestampMs('2026-07-20T10:00:00Z')).toBe(Date.parse('2026-07-20T10:00:00Z'));
    expect(timestampMs(null)).toBeNaN();
    expect(timestampMs('')).toBeNaN();
  });
});

describe('ageSortValue', () => {
  it('sorts the youngest first, and a missing time last', () => {
    expect(ageSortValue(2)).toBeLessThan(ageSortValue(1)!);
    expect(ageSortValue(Number.NaN)).toBeUndefined();
  });
});
