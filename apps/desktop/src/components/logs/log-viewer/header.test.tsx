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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Column } from './columns';
import { LogViewerHeader } from './header';
import { gridTemplate } from './row';

// jsdom keeps no scroll offsets, so `scrollLeft` is stored per element here.
const lefts = new WeakMap<Element, number>();
const original = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollLeft');

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'cancelAnimationFrame'] });
  Object.defineProperty(Element.prototype, 'scrollLeft', {
    configurable: true,
    get(this: Element) {
      return lefts.get(this) ?? 0;
    },
    set(this: Element, value: number) {
      lefts.set(this, value);
    },
  });
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(private readonly callback: () => void) {}

      observe() {
        this.callback();
      }

      disconnect() {}
    },
  );
});

afterEach(() => {
  if (original) Object.defineProperty(Element.prototype, 'scrollLeft', original);
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

const frame = () =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(16);
  });

// A user's scroll: the offset moves, then the element says so.
function userScroll(el: HTMLElement, left: number) {
  lefts.set(el, left);
  el.dispatchEvent(new Event('scroll'));
}

function scrollerOf({ offsetWidth = 300, clientWidth = 300 } = {}) {
  const el = document.createElement('div');
  Object.defineProperty(el, 'offsetWidth', { value: offsetWidth });
  Object.defineProperty(el, 'clientWidth', { value: clientWidth });
  return el;
}

const header = (columns: Column[], scroller: HTMLElement | null = null) => (
  <LogViewerHeader
    gridTemplate={gridTemplate(columns)}
    columns={columns}
    widths={new Map()}
    rowWidth={0}
    wrap={false}
    scroller={scroller}
  />
);

describe('LogViewerHeader', () => {
  it('names the position column, then the visible columns in order', () => {
    render(header(['Timestamp', 'Dot', 'Pod', 'Message']));
    const cells = [...screen.getByTestId('log-header').children].map((el) => el.textContent);
    expect(cells).toEqual(['Position', 'Timestamp', 'Source', 'Pod', 'Message']);
    // The position and the dot draw no name, only one for the screen reader.
    expect(screen.getByText('Position')).toHaveClass('sr-only');
    expect(screen.getByText('Source')).toHaveClass('sr-only');
  });

  it('follows the scroller sideways on the next frame', async () => {
    const scroller = scrollerOf();
    const { container } = render(header(['Message'], scroller));
    const wrapper = container.firstElementChild as HTMLElement;
    expect(wrapper).toHaveClass('overflow-x-scroll', 'no-scrollbar');

    userScroll(scroller, 120);
    expect(wrapper.scrollLeft).toBe(0);
    await frame();
    expect(wrapper.scrollLeft).toBe(120);
  });

  it('lines up with a new scroller at once, before it scrolls', () => {
    const scroller = scrollerOf();
    const { container, rerender } = render(header(['Message'], scroller));
    const wrapper = container.firstElementChild as HTMLElement;
    lefts.set(wrapper, 120);
    rerender(header(['Message'], scrollerOf()));
    expect(wrapper.scrollLeft).toBe(0);
  });

  it('moves the scroller when the user scrolls it, and ignores the echo', async () => {
    const scroller = scrollerOf();
    const { container } = render(header(['Message'], scroller));
    const wrapper = container.firstElementChild as HTMLElement;

    userScroll(wrapper, 80);
    await frame();
    expect(scroller.scrollLeft).toBe(80);
    // The scroller's own scroll event, fired by the write above, must not write back.
    lefts.set(wrapper, 70);
    scroller.dispatchEvent(new Event('scroll'));
    await frame();
    expect(wrapper.scrollLeft).toBe(70);
  });

  it('reserves the scroller’s scrollbar width at its right edge', () => {
    render(header(['Message'], scrollerOf({ offsetWidth: 300, clientWidth: 285 })));
    expect(screen.getByTestId('log-header-gutter')).toHaveStyle({ width: '15px' });
  });

  it('draws no gutter for an overlay scrollbar', () => {
    render(header(['Message'], scrollerOf()));
    expect(screen.queryByTestId('log-header-gutter')).not.toBeInTheDocument();
  });
});
