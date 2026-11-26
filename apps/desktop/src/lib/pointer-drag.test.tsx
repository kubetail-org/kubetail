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
import { afterEach, describe, expect, it, vi } from 'vitest';

import { dragFrom } from './pointer-drag';

// jsdom doesn't implement pointer capture; the drag calls it, so stub it out.
if (!HTMLElement.prototype.setPointerCapture) {
  HTMLElement.prototype.setPointerCapture = () => {};
}

const setup = () => {
  const onMove = vi.fn();
  render(<div role="separator" aria-label="Resize" onPointerDown={(e) => dragFrom(e, 'ns-resize', onMove)} />);
  fireEvent.pointerDown(screen.getByRole('separator'));
  return onMove;
};

// A drag a test leaves mid-flight keeps its window listeners and the forced
// cursor, which the next test would inherit.
afterEach(() => {
  fireEvent.pointerUp(window);
});

describe('dragFrom', () => {
  it('follows the pointer across the window until it lifts', () => {
    const onMove = setup();

    fireEvent.pointerMove(window, { clientX: 10, clientY: 20 });
    expect(onMove).toHaveBeenCalledTimes(1);
    expect(onMove.mock.calls[0][0]).toMatchObject({ clientX: 10, clientY: 20 });

    fireEvent.pointerUp(window);
    fireEvent.pointerMove(window, { clientX: 30 });
    expect(onMove).toHaveBeenCalledTimes(1);
  });

  it('forces the cursor and disables selection for the drag alone', () => {
    document.body.style.cursor = 'pointer';
    setup();
    expect(document.body.style.cursor).toBe('ns-resize');
    expect(document.body.style.userSelect).toBe('none');

    fireEvent.pointerUp(window);
    expect(document.body.style.cursor).toBe('pointer');
    expect(document.body.style.userSelect).toBe('');
    document.body.style.cursor = '';
  });

  it('starts no drag for a press of another button than the primary', () => {
    const onMove = vi.fn();
    render(<div role="separator" aria-label="Resize" onPointerDown={(e) => dragFrom(e, 'ns-resize', onMove)} />);
    fireEvent.pointerDown(screen.getByRole('separator'), { button: 2 });

    fireEvent.pointerMove(window, { clientX: 10 });
    expect(onMove).not.toHaveBeenCalled();
    expect(document.body.style.cursor).toBe('');
  });

  // A touch or pen gesture the browser takes over ends with no pointerup.
  it('ends on pointercancel as on pointerup', () => {
    const onEnd = vi.fn();
    const onMove = vi.fn();
    document.body.style.cursor = 'pointer';
    render(<div role="separator" aria-label="Resize" onPointerDown={(e) => dragFrom(e, 'ns-resize', onMove, onEnd)} />);
    fireEvent.pointerDown(screen.getByRole('separator'));

    fireEvent.pointerCancel(window);
    expect(onEnd).toHaveBeenCalledOnce();
    expect(document.body.style.cursor).toBe('pointer');
    fireEvent.pointerMove(window, { clientX: 30 });
    expect(onMove).not.toHaveBeenCalled();
    // Ended once: a later pointerup is no second end.
    fireEvent.pointerUp(window);
    expect(onEnd).toHaveBeenCalledOnce();
    document.body.style.cursor = '';
  });

  // A second touch or pen contact, cancelled or lifted, is not the one dragging.
  it('follows and ends with the pointer that pressed alone', () => {
    const onEnd = vi.fn();
    const onMove = vi.fn();
    render(<div role="separator" aria-label="Resize" onPointerDown={(e) => dragFrom(e, 'ns-resize', onMove, onEnd)} />);
    fireEvent.pointerDown(screen.getByRole('separator'), { pointerId: 1 });

    fireEvent.pointerMove(window, { pointerId: 2, clientX: 10 });
    fireEvent.pointerCancel(window, { pointerId: 2 });
    fireEvent.pointerUp(window, { pointerId: 2 });
    expect(onMove).not.toHaveBeenCalled();
    expect(onEnd).not.toHaveBeenCalled();
    expect(document.body.style.cursor).toBe('ns-resize');

    fireEvent.pointerMove(window, { pointerId: 1, clientX: 20 });
    expect(onMove).toHaveBeenCalledOnce();
    fireEvent.pointerCancel(window, { pointerId: 1 });
    expect(onEnd).toHaveBeenCalledOnce();
  });

  // Two fingers on two handles: the second would save the first's forced cursor and
  // selection, and restore them when it ended.
  it('runs one drag at a time, leaving the body as it was', () => {
    const first = vi.fn();
    const second = vi.fn();
    document.body.style.cursor = 'pointer';
    render(
      <>
        <div role="separator" aria-label="First" onPointerDown={(e) => dragFrom(e, 'col-resize', first)} />
        <div role="separator" aria-label="Second" onPointerDown={(e) => dragFrom(e, 'row-resize', second)} />
      </>,
    );
    fireEvent.pointerDown(screen.getByRole('separator', { name: 'First' }), { pointerId: 1 });
    // Refused: `fireEvent` reports a prevented default as false.
    expect(fireEvent.pointerDown(screen.getByRole('separator', { name: 'Second' }), { pointerId: 2 })).toBe(true);
    expect(document.body.style.cursor).toBe('col-resize');

    fireEvent.pointerMove(window, { pointerId: 2, clientX: 10 });
    expect(second).not.toHaveBeenCalled();
    fireEvent.pointerUp(window, { pointerId: 1 });
    fireEvent.pointerUp(window, { pointerId: 2 });
    expect(document.body.style.cursor).toBe('pointer');
    expect(document.body.style.userSelect).toBe('');

    // Free again once the first ends.
    fireEvent.pointerDown(screen.getByRole('separator', { name: 'Second' }), { pointerId: 2 });
    expect(document.body.style.cursor).toBe('row-resize');
    fireEvent.pointerUp(window, { pointerId: 2 });
    document.body.style.cursor = '';
  });

  // Cmd/Ctrl+B removes the handle mid-drag; a release outside the window never comes back
  // as a pointerup, and the next drag would be refused.
  it('ends when the handle loses its capture, even once it is gone', () => {
    const onEnd = vi.fn();
    document.body.style.cursor = 'pointer';
    const ui = (shown: boolean) =>
      shown ? (
        <div role="separator" aria-label="Resize" onPointerDown={(e) => dragFrom(e, 'col-resize', () => {}, onEnd)} />
      ) : null;
    const { rerender } = render(ui(true));
    fireEvent.pointerDown(screen.getByRole('separator'), { pointerId: 1 });
    rerender(ui(false));

    // Another pointer's loss is not this drag's.
    fireEvent.lostPointerCapture(document, { pointerId: 2 });
    expect(onEnd).not.toHaveBeenCalled();
    // Where the browser sends it once the captured element is removed.
    fireEvent.lostPointerCapture(document, { pointerId: 1 });
    expect(onEnd).toHaveBeenCalledOnce();
    expect(document.body.style.cursor).toBe('pointer');
    expect(document.body.style.userSelect).toBe('');

    // The guard is clear: a new drag starts.
    rerender(ui(true));
    expect(fireEvent.pointerDown(screen.getByRole('separator'), { pointerId: 3 })).toBe(false);
    fireEvent.pointerUp(window, { pointerId: 3 });
    document.body.style.cursor = '';
  });

  it('says when the pointer lifts', () => {
    const onEnd = vi.fn();
    render(
      <div role="separator" aria-label="Resize" onPointerDown={(e) => dragFrom(e, 'ns-resize', () => {}, onEnd)} />,
    );
    fireEvent.pointerDown(screen.getByRole('separator'));
    expect(onEnd).not.toHaveBeenCalled();
    fireEvent.pointerUp(window);
    expect(onEnd).toHaveBeenCalledOnce();
  });
});
