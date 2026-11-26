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
import { describe, expect, it } from 'vitest';

import { useLastSettled } from './use-last-settled';

describe('useLastSettled', () => {
  it('passes the value through while settled', () => {
    const { result, rerender } = renderHook(({ value }) => useLastSettled(value, true), {
      initialProps: { value: 'a' },
    });
    rerender({ value: 'b' });
    expect(result.current).toBe('b');
  });

  it('holds the last settled value while unsettled, then takes the next', () => {
    const { result, rerender } = renderHook(({ value, settled }) => useLastSettled(value, settled), {
      initialProps: { value: 'a', settled: true },
    });
    rerender({ value: 'gone', settled: false });
    expect(result.current).toBe('a');
    rerender({ value: 'b', settled: true });
    expect(result.current).toBe('b');
  });

  it('passes the value through before anything settles', () => {
    const { result, rerender } = renderHook(({ value }) => useLastSettled(value, false), {
      initialProps: { value: 'first' },
    });
    rerender({ value: 'second' });
    expect(result.current).toBe('second');
  });

  it('holds an undefined it settled on', () => {
    const { result, rerender } = renderHook(({ value, settled }) => useLastSettled(value, settled), {
      initialProps: { value: undefined as string | undefined, settled: true },
    });
    rerender({ value: 'unsettled', settled: false });
    expect(result.current).toBeUndefined();
  });
});
