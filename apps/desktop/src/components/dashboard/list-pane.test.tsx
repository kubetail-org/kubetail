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
import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { stubResizeObserver } from '@/test-utils';

import { ListPane } from './list-pane';

stubResizeObserver();

const KEY = 'kubetail:test-list-width';
const handle = () => screen.getByRole('separator', { name: 'Resize pods list' });
const pane = () => handle().parentElement!;

function show() {
  render(
    <div>
      <ListPane widthKey="test-list-width" resizeLabel="Resize pods list">
        rows
      </ListPane>
    </div>,
  );
}

// jsdom lays nothing out: the row is `rowWidth` wide and the pane as wide as it says.
function layOut(rowWidth: number) {
  Object.defineProperty(pane().parentElement!, 'clientWidth', { configurable: true, value: rowWidth });
  vi.spyOn(pane(), 'getBoundingClientRect').mockImplementation(
    () => ({ width: Number.parseFloat(pane().style.width) }) as DOMRect,
  );
}

function press(key: string, times = 1) {
  for (let i = 0; i < times; i += 1) {
    act(() => {
      fireEvent.keyDown(handle(), { key });
    });
  }
}

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe('ListPane', () => {
  it('opens 320px wide, beside its half of the 16px strip, over a grip the handle covers', () => {
    show();
    expect(pane()).toHaveTextContent('rows');
    expect(pane().style.width).toBe('328px');
    expect(pane().style.paddingRight).toBe('8px');
    expect(handle()).toHaveClass('-right-2', 'w-4');
    expect(handle().previousElementSibling!.firstElementChild).toHaveClass('h-8', 'w-1', 'bg-muted-foreground');
  });

  it('reads the kept width back', () => {
    localStorage.setItem(KEY, '400');
    show();
    expect(pane().style.width).toBe('408px');
  });

  it('resizes within 240px and the width that leaves the detail 360px, and keeps it', () => {
    show();
    layOut(1000);
    press('ArrowRight');
    expect(pane().style.width).toBe('344px');
    expect(localStorage.getItem(KEY)).toBe('336');

    // 1000 − 360 for the detail − 8 for its half of the strip.
    press('ArrowRight', 30);
    expect(pane().style.width).toBe('632px');
    press('ArrowLeft', 40);
    expect(pane().style.width).toBe('248px');
    expect(localStorage.getItem(KEY)).toBe('240');
  });

  it('lets the detail win on a row too narrow for both panes', () => {
    show();
    expect(pane().style.maxWidth).toBe('calc(100% - 368px)');
    // 500 − 368 leaves the list 132px, under its 240px minimum.
    layOut(500);
    fireEvent.focus(handle());
    expect(handle()).toHaveAttribute('aria-valuemin', '132');
    expect(handle()).toHaveAttribute('aria-valuemax', '132');
    // Narrower than the detail alone, the list has no room at all.
    layOut(300);
    fireEvent.focus(handle());
    expect(handle()).toHaveAttribute('aria-valuemax', '0');
  });
});
