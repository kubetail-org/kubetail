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

import { useHeldRow } from './use-held-row';

type Row = { name: string };
type Props = { row: Row | undefined; settled: boolean };

const A: Row = { name: 'a' };

function hold(initialProps: Props) {
  return renderHook(({ row, settled }: Props) => useHeldRow(row, settled), { initialProps });
}

describe('useHeldRow', () => {
  it('shows the row’s newest body', () => {
    const { result, rerender } = hold({ row: A, settled: true });
    const next = { name: 'a, changed' };
    rerender({ row: next, settled: true });
    expect(result.current).toEqual({ shown: next, gone: false });
  });

  it('holds the row while unsettled rows lack it, and says it is gone once they settle', () => {
    const { result, rerender } = hold({ row: A, settled: true });
    rerender({ row: undefined, settled: false });
    expect(result.current).toEqual({ shown: A, gone: false });
    rerender({ row: undefined, settled: true });
    expect(result.current).toEqual({ shown: A, gone: true });
  });

  it('stays gone through a reconnect, until the row comes back', () => {
    const { result, rerender } = hold({ row: A, settled: true });
    rerender({ row: undefined, settled: true });
    rerender({ row: undefined, settled: false });
    expect(result.current.gone).toBe(true);
    rerender({ row: A, settled: false });
    expect(result.current).toEqual({ shown: A, gone: false });
  });

  it('shows nothing for a row it never saw', () => {
    const { result } = hold({ row: undefined, settled: true });
    expect(result.current).toEqual({ shown: undefined, gone: true });
  });
});
