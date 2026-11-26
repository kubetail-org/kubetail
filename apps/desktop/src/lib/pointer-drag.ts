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

// A drag from a resize handle's pointerdown to the pointerup, pointercancel or lost capture
// that ends it: `onMove` gets every pointer position in between, and `onEnd` the end. A touch or
// pen gesture the browser takes over ends in pointercancel, with no pointerup. Only the
// pointer that pressed counts: another touch or pen contact neither moves nor ends it.
//
// One drag runs at a time, window-wide: each saves the body's cursor and selection to
// restore, and a second drag started meanwhile would save the first's forced ones.
import type { PointerEvent as ReactPointerEvent } from 'react';

let active = false;

/** Starts a drag, returning false, with nothing done, for a press other than the primary
 * button's or while another drag runs. */
export function dragFrom(
  e: ReactPointerEvent<HTMLElement>,
  cursor: string,
  onMove: (ev: PointerEvent) => void,
  onEnd?: () => void,
): boolean {
  if (active || e.button !== 0) return false;
  active = true;
  e.preventDefault();
  const { pointerId } = e;
  // Pointer capture keeps the drag alive when the cursor outruns the thin strip.
  e.currentTarget.setPointerCapture(pointerId);
  // Force the cursor for the whole drag (else it reverts off the strip);
  // `user-select: none` stops text selection.
  const { body } = document;
  const prevCursor = body.style.cursor;
  const prevSelect = body.style.userSelect;
  body.style.cursor = cursor;
  body.style.userSelect = 'none';
  const move = (ev: PointerEvent) => {
    if (ev.pointerId === pointerId) onMove(ev);
  };
  const end = (ev: PointerEvent) => {
    if (ev.pointerId !== pointerId) return;
    active = false;
    body.style.cursor = prevCursor;
    body.style.userSelect = prevSelect;
    window.removeEventListener('pointermove', move);
    window.removeEventListener('pointerup', end);
    window.removeEventListener('pointercancel', end);
    window.removeEventListener('lostpointercapture', end);
    onEnd?.();
  };
  window.addEventListener('pointermove', move);
  window.addEventListener('pointerup', end);
  window.addEventListener('pointercancel', end);
  // Removing the handle mid-drag (Cmd/Ctrl+B closing its sidebar) drops the capture, and a
  // release outside the window then never arrives as a pointerup. The loss is dispatched at
  // the handle, or at the document once the handle is gone; both bubble to `window`.
  window.addEventListener('lostpointercapture', end);
  return true;
}
