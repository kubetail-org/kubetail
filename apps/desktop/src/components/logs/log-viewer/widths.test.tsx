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

import { act, render, renderHook, screen } from '@testing-library/react';
import { StrictMode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { CELL_PADDING_PX, POS_MIN_PX, useMeasureWidths } from './widths';

// A cell as the row draws it: the column on `data-col`, its content in an inline-block span
// that jsdom says is `width` wide.
function cell(col: string, width: number): HTMLDivElement {
  const el = document.createElement('div');
  el.dataset.col = col;
  const span = document.createElement('span');
  span.getBoundingClientRect = () => ({ width }) as DOMRect;
  el.append(span);
  return el;
}

function row(scrollWidth: number): HTMLDivElement {
  const el = document.createElement('div');
  Object.defineProperty(el, 'scrollWidth', { value: scrollWidth });
  return el;
}

const frame = () =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(16);
  });

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame', 'setTimeout'] });
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('useMeasureWidths', () => {
  it('gives a column its widest cell plus padding, published on the next frame', async () => {
    const { result } = renderHook(() => useMeasureWidths());
    result.current.measureCell(cell('Pod', 80));
    result.current.measureCell(cell('Pod', 120));
    expect(result.current.widths.get('Pod')).toBeUndefined();

    await frame();
    expect(result.current.widths.get('Pod')).toBe(120 + CELL_PADDING_PX);
  });

  it('keeps the width when a narrower cell comes later', async () => {
    const { result } = renderHook(() => useMeasureWidths());
    result.current.measureCell(cell('Node', 100));
    await frame();
    result.current.measureCell(cell('Node', 40));
    await frame();
    expect(result.current.widths.get('Node')).toBe(100 + CELL_PADDING_PX);
  });

  it('keeps the map’s identity when no width moved', async () => {
    const { result } = renderHook(() => useMeasureWidths());
    result.current.measureCell(cell('Pod', 80));
    await frame();
    const before = result.current.widths;
    result.current.measureCell(cell('Pod', 60));
    result.current.measureRow(row(500));
    await frame();
    expect(result.current.widths).toBe(before);
    expect(result.current.rowWidth).toBe(500);
  });

  it('measures each element once', async () => {
    const { result } = renderHook(() => useMeasureWidths());
    const el = cell('Pod', 80);
    result.current.measureCell(el);
    await frame();
    (el.firstElementChild as HTMLElement).getBoundingClientRect = () => ({ width: 300 }) as DOMRect;
    result.current.measureCell(el);
    await frame();
    expect(result.current.widths.get('Pod')).toBe(80 + CELL_PADDING_PX);
  });

  it('gives the position column 48px at least, and more for a wider key', async () => {
    const { result } = renderHook(() => useMeasureWidths());
    expect(result.current.widths.get('Pos')).toBe(POS_MIN_PX);
    result.current.measureCell(cell('Pos', 10));
    await frame();
    expect(result.current.widths.get('Pos')).toBe(POS_MIN_PX);
    result.current.measureCell(cell('Pos', 60));
    await frame();
    expect(result.current.widths.get('Pos')).toBe(60 + CELL_PADDING_PX);
  });

  it('takes the widest row', async () => {
    const { result } = renderHook(() => useMeasureWidths());
    result.current.measureRow(row(400));
    result.current.measureRow(row(300));
    await frame();
    expect(result.current.rowWidth).toBe(400);
  });

  it('publishes what a ref measured before StrictMode replayed the effect', async () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ width: 80 } as DOMRect);
    function Probe() {
      const { widths, measureCell } = useMeasureWidths();
      return (
        <div ref={measureCell} data-col="Pod">
          <span>{widths.get('Pod') ?? 'none'}</span>
        </div>
      );
    }
    render(
      <StrictMode>
        <Probe />
      </StrictMode>,
    );
    await frame();
    expect(screen.getByText(String(80 + CELL_PADDING_PX))).toBeInTheDocument();
  });
});
