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

import { render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { Countdown } from './countdown';

describe('Countdown', () => {
  // The clock is frozen so each reading is exact.
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('counts down as kubectl prints a duration', () => {
    const { rerender } = render(<Countdown ms={Date.now() + 5_000} />);
    expect(screen.getByText('in 5s')).toBeInTheDocument();
    rerender(<Countdown ms={Date.now() + 90_000} />);
    expect(screen.getByText('in 90s')).toBeInTheDocument();
    rerender(<Countdown ms={Date.now() + 5 * 60_000 + 7_000} />);
    expect(screen.getByText('in 5m7s')).toBeInTheDocument();
  });

  it('reads now once due', () => {
    render(<Countdown ms={Date.now() - 1_000} />);
    expect(screen.getByText('now')).toBeInTheDocument();
  });
});
