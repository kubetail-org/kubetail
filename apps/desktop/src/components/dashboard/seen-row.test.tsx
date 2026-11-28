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

import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { SeenRow } from './seen-row';

const NOW = new Date(2026, 10, 22, 11, 14, 0).getTime();

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
});
afterEach(() => vi.useRealTimers());

const show = (ms: number) =>
  render(
    <dl>
      <SeenRow label="Created" ms={ms} />
    </dl>,
  );

describe('SeenRow', () => {
  it('draws the moment to the minute, and how long ago it was', () => {
    show(new Date(2026, 10, 22, 9, 14).getTime());
    expect(screen.getByText('Created').nextElementSibling).toHaveTextContent('2026-11-22 09:14 · 2h ago');
  });

  it('titles the moment with its age as the pointer arrives', () => {
    show(new Date(2026, 10, 22, 9, 4).getTime());
    const value = screen.getByText('Created').nextElementSibling!;
    expect(value).toHaveAttribute('title', '2026-11-22 09:04');
    fireEvent.pointerEnter(value);
    expect(value).toHaveAttribute('title', '2026-11-22 09:04 · 130m ago');
  });

  it('dashes a moment that did not parse', () => {
    show(Number.NaN);
    expect(screen.getByText('Created').nextElementSibling).toHaveTextContent('—');
  });
});
