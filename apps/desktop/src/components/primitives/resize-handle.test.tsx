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

import { ResizeHandle } from './resize-handle';

// Helpers -------------------------------------------------------------

// jsdom doesn't implement pointer capture; the handle calls it, so stub it out.
if (!HTMLElement.prototype.setPointerCapture) {
  HTMLElement.prototype.setPointerCapture = () => {};
}

stubResizeObserver();

const BOUNDS = () => ({ min: 200, max: 480 });
// For the drag arithmetic alone.
const UNBOUNDED = () => ({ min: 0, max: Infinity });
const handle = () => screen.getByRole('separator', { name: 'Resize' });

// jsdom lays nothing out: draws the handle's pane at `width`.
function drawPane(width: number) {
  vi.spyOn(handle().parentElement!, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, width, 400));
}

const drag = (clientX: number) => {
  fireEvent.pointerDown(handle());
  fireEvent.pointerMove(window, { clientX });
};

// A drag that a test leaves mid-flight keeps its window listeners and the forced
// cursor, which the next test would inherit.
afterEach(() => {
  fireEvent.pointerUp(window);
});

// Tests ---------------------------------------------------------------

describe('ResizeHandle', () => {
  it('reports the pointer’s x for a pane anchored at the left', () => {
    const onResize = vi.fn();
    render(<ResizeHandle edge="right" bounds={BOUNDS} onResize={onResize} label="Resize" />);

    drag(300);
    expect(onResize).toHaveBeenCalledWith(300);
  });

  it('measures from the pane’s left edge, wherever the pane sits', () => {
    const onResize = vi.fn();
    render(
      <div>
        <ResizeHandle edge="right" bounds={UNBOUNDED} onResize={onResize} label="Resize" />
      </div>,
    );
    vi.spyOn(handle().parentElement!, 'getBoundingClientRect').mockReturnValue(new DOMRect(120, 0, 140, 400));

    drag(300);
    expect(onResize).toHaveBeenCalledWith(180);
  });

  // The sidebar drawer slides in from off the row's left edge; a press during the slide
  // reads the pane where it is then.
  it('measures the pane’s left edge on every move, while the pane is still moving', () => {
    const onResize = vi.fn();
    render(
      <div>
        <ResizeHandle edge="right" bounds={BOUNDS} onResize={onResize} label="Resize" />
      </div>,
    );
    const rect = vi.spyOn(handle().parentElement!, 'getBoundingClientRect');
    rect.mockReturnValue(new DOMRect(-200, 0, 256, 400));
    fireEvent.pointerDown(handle());
    // Slid in: the pane's left edge is at the row's.
    rect.mockReturnValue(new DOMRect(0, 0, 256, 400));
    fireEvent.pointerMove(window, { clientX: 300 });
    expect(onResize).toHaveBeenLastCalledWith(300);
  });

  // Unmaximized on Linux, the window's frame ends inside the viewport: the pane's right
  // edge, not the viewport's, is what it grows from.
  it('measures a pane grown from its left edge from its own right edge', () => {
    const onResize = vi.fn();
    render(
      <div>
        <ResizeHandle edge="left" bounds={UNBOUNDED} onResize={onResize} label="Resize" />
      </div>,
    );
    vi.spyOn(handle().parentElement!, 'getBoundingClientRect').mockReturnValue(new DOMRect(600, 0, 384, 400));

    drag(300);
    expect(onResize).toHaveBeenCalledWith(684);
  });

  // The left sidebar's and the right sidebar's handles, pressed by two fingers.
  it('starts no drag while another handle’s runs', () => {
    const onResize = vi.fn();
    render(
      <>
        <ResizeHandle edge="right" bounds={BOUNDS} onResize={() => {}} label="Resize" />
        <ResizeHandle edge="left" bounds={BOUNDS} onResize={onResize} label="Resize other" />
      </>,
    );
    fireEvent.pointerDown(handle(), { pointerId: 1 });
    fireEvent.pointerDown(screen.getByRole('separator', { name: 'Resize other' }), { pointerId: 2 });
    fireEvent.pointerMove(window, { pointerId: 2, clientX: 100 });
    expect(onResize).not.toHaveBeenCalled();
    fireEvent.pointerUp(window, { pointerId: 1 });
  });

  it('forces the resize cursor for the whole drag, then restores it', () => {
    render(<ResizeHandle edge="right" bounds={BOUNDS} onResize={() => {}} label="Resize" />);
    expect(document.body.style.cursor).toBe('');

    // Held even when the pointer strays off the thin strip.
    drag(300);
    expect(document.body.style.cursor).toBe('col-resize');

    fireEvent.pointerUp(window);
    expect(document.body.style.cursor).toBe('');
  });

  it('stops reporting once the pointer is released', () => {
    const onResize = vi.fn();
    render(<ResizeHandle edge="right" bounds={BOUNDS} onResize={onResize} label="Resize" />);

    drag(300);
    fireEvent.pointerUp(window);
    fireEvent.pointerMove(window, { clientX: 400 });
    expect(onResize).toHaveBeenCalledTimes(1);
  });

  it('rides the edge it resizes from, straddling a right-edge border', () => {
    // Half the strip sits outside the pane.
    const { rerender } = render(<ResizeHandle edge="right" bounds={BOUNDS} onResize={() => {}} label="Resize" />);
    expect(handle()).toHaveClass('-right-1');

    rerender(<ResizeHandle edge="left" bounds={BOUNDS} onResize={() => {}} label="Resize" />);
    expect(handle()).toHaveClass('left-0');
  });

  it('takes a class that moves or widens its strip', () => {
    render(<ResizeHandle edge="right" bounds={BOUNDS} onResize={() => {}} label="Resize" className="-right-2 w-4" />);
    expect(handle()).toHaveClass('-right-2', 'w-4');
    expect(handle()).not.toHaveClass('-right-1', 'w-2');
  });

  it('takes focus, carrying the pane’s width as drawn and its bounds', () => {
    let bounds = { min: 200, max: 480 };
    render(
      <div>
        <ResizeHandle edge="right" bounds={() => bounds} onResize={() => {}} label="Resize" />
      </div>,
    );
    drawPane(240.4);
    bounds = { min: 180, max: 300 };
    // Measured again on focus: the layout can move the bounds without resizing the pane.
    act(() => handle().focus());
    expect(handle()).toHaveFocus();
    expect(handle()).toHaveAttribute('aria-valuenow', '240');
    expect(handle()).toHaveAttribute('aria-valuemin', '180');
    expect(handle()).toHaveAttribute('aria-valuemax', '300');
  });

  it('carries no maximum the caller cannot name, and none below its minimum', () => {
    let bounds = { min: 240, max: Infinity };
    render(<ResizeHandle edge="left" bounds={() => bounds} onResize={() => {}} label="Resize" />);
    expect(handle()).not.toHaveAttribute('aria-valuemax');
    bounds = { min: 240, max: 100 };
    act(() => handle().focus());
    expect(handle()).toHaveAttribute('aria-valuemax', '240');
  });

  // A window resize, or the pane beside it taking the left sidebar's width, moves the
  // bounds while the pane stays put.
  it('measures again when its pane’s container or a pane beside it resizes', () => {
    const observed: Element[] = [];
    let report = () => {};
    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(cb: () => void) {
          report = cb;
        }

        observe(el: Element) {
          observed.push(el);
        }

        disconnect() {}
      },
    );
    let bounds = { min: 240, max: 400 };
    render(
      <section>
        <main />
        <div>
          <ResizeHandle edge="left" bounds={() => bounds} onResize={() => {}} label="Resize" />
        </div>
      </section>,
    );
    const pane = handle().parentElement!;
    expect(observed).toEqual([pane.parentElement, pane.previousElementSibling, pane]);
    bounds = { min: 240, max: 600 };
    act(() => report());
    expect(handle()).toHaveAttribute('aria-valuemax', '600');
    stubResizeObserver();
  });

  // A pane drawn narrower than its stored width moves on the first press.
  it.each([
    ['right', 'ArrowRight', 256],
    ['right', 'ArrowLeft', 224],
    ['left', 'ArrowLeft', 256],
    ['left', 'ArrowRight', 224],
  ] as const)('steps a pane on its %s edge on %s from its width as drawn', (edge, key, px) => {
    const onResize = vi.fn();
    render(
      <div>
        <ResizeHandle edge={edge} bounds={BOUNDS} onResize={onResize} label="Resize" />
      </div>,
    );
    drawPane(240);
    fireEvent.keyDown(handle(), { key });
    expect(onResize).toHaveBeenCalledWith(px);
  });

  it('steps no further than its bounds', () => {
    const onResize = vi.fn();
    render(
      <div>
        <ResizeHandle edge="right" bounds={BOUNDS} onResize={onResize} label="Resize" />
      </div>,
    );
    drawPane(470);
    fireEvent.keyDown(handle(), { key: 'ArrowRight' });
    expect(onResize).toHaveBeenLastCalledWith(480);
    drawPane(210);
    fireEvent.keyDown(handle(), { key: 'ArrowLeft' });
    expect(onResize).toHaveBeenLastCalledWith(200);
  });

  it('keeps a drag within its bounds', () => {
    const onResize = vi.fn();
    render(
      <div>
        <ResizeHandle edge="right" bounds={BOUNDS} onResize={onResize} label="Resize" />
      </div>,
    );
    drawPane(300);
    drag(900);
    expect(onResize).toHaveBeenLastCalledWith(480);
    drag(50);
    expect(onResize).toHaveBeenLastCalledWith(200);
  });

  // A second finger opens the right sidebar mid-drag, capping the left.
  it('takes the bounds the layout has now on each move of a drag', () => {
    const onResize = vi.fn();
    const ui = (max: number) => (
      <div>
        <ResizeHandle edge="right" bounds={() => ({ min: 200, max })} onResize={onResize} label="Resize" />
      </div>
    );
    const { rerender } = render(ui(480));
    drawPane(300);
    drag(320);
    expect(onResize).toHaveBeenLastCalledWith(320);
    rerender(ui(240));
    drawPane(240);
    onResize.mockClear();
    fireEvent.pointerMove(window, { clientX: 400 });
    expect(onResize).not.toHaveBeenCalled();
  });

  // A capped pane's stored width is the user's for a wider window.
  it('writes nothing for a drag outward that cannot move the edge', () => {
    const onResize = vi.fn();
    render(
      <div>
        <ResizeHandle edge="right" bounds={() => ({ min: 200, max: 240 })} onResize={onResize} label="Resize" />
      </div>,
    );
    drawPane(240);
    drag(260);
    expect(onResize).not.toHaveBeenCalled();
    // Inward it moves.
    fireEvent.pointerMove(window, { clientX: 230 });
    expect(onResize).toHaveBeenCalledWith(230);
  });

  // A capped pane's stored width is the user's for a wider window.
  it('writes nothing for a press that cannot move the edge its way', () => {
    const onResize = vi.fn();
    render(
      <div>
        <ResizeHandle edge="right" bounds={() => ({ min: 200, max: 240 })} onResize={onResize} label="Resize" />
      </div>,
    );
    drawPane(240);
    fireEvent.keyDown(handle(), { key: 'ArrowRight' });
    // Drawn past its maximum, a grow would shrink it.
    drawPane(260);
    fireEvent.keyDown(handle(), { key: 'ArrowRight' });
    // Held at its minimum by the layout.
    drawPane(190);
    fireEvent.keyDown(handle(), { key: 'ArrowLeft' });
    expect(onResize).not.toHaveBeenCalled();
  });

  it('leaves other keys alone', () => {
    const onResize = vi.fn();
    render(<ResizeHandle edge="right" bounds={BOUNDS} onResize={onResize} label="Resize" />);
    expect(fireEvent.keyDown(handle(), { key: 'ArrowUp' })).toBe(true);
    expect(onResize).not.toHaveBeenCalled();
  });

  it('keeps reporting after its pane draws it again', () => {
    const onResize = vi.fn();
    const { rerender } = render(<ResizeHandle edge="right" bounds={BOUNDS} onResize={onResize} label="Resize" />);
    rerender(<ResizeHandle edge="right" bounds={BOUNDS} onResize={onResize} label="Resize" />);

    drag(250);
    expect(onResize).toHaveBeenCalledWith(250);
  });
});
