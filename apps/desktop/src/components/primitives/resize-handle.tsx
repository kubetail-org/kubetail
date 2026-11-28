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

// A pane's drag handle: a thin strip on the edge it resizes from, reporting the width the
// pointer implies, or the arrow keys step to, within the bounds the caller gives. The caller
// persists it.
//
// `edge` is which side of the pane the handle rides, and decides the arithmetic:
// a pane grown from its right edge is as wide as the pointer is from its left
// edge, one grown from its left edge as wide as the pointer is from its right
// edge. Both edges are the handle's parent's, which can sit inside the viewport
// (Linux's window frame).
import { cn } from '@kubetail/ui/lib/utils';
import type { KeyboardEvent, PointerEvent as ReactPointerEvent } from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';

import { dragFrom } from '@/lib/pointer-drag';

// How far one arrow press moves the edge (px).
const KEY_STEP = 16;

type Range = { now: number; min: number; max: number };

type ResizeHandleProps = {
  edge: 'left' | 'right';
  /** The narrowest and widest the layout lets the pane be now, read each time it is measured. */
  bounds: () => { min: number; max: number };
  onResize: (px: number) => void;
  /** Names the handle for the keyboard and for tests. */
  label: string;
  /** Moves or widens the strip, for a pane whose divider is wider than the default. */
  className?: string;
};

export function ResizeHandle({ edge, bounds, onResize, label, className }: ResizeHandleProps) {
  const ref = useRef<HTMLDivElement>(null);
  // The pane as drawn, which can be narrower than the width its caller stores (a cap, or
  // the row's flex shrinking it), and its bounds: what the arrows step within and a screen
  // reader reads.
  const read = useCallback((): Range => {
    const { min, max } = bounds();
    return { now: ref.current!.parentElement!.getBoundingClientRect().width, min, max: Math.max(min, max) };
  }, [bounds]);
  const [range, setRange] = useState<Range>();
  const measure = useCallback(() => setRange(read()), [read]);
  // The pane's container and everything in it are observed: a window resize, or a pane
  // beside this one opening, moves the bounds while this pane stays put.
  useEffect(() => {
    const pane = ref.current!.parentElement!;
    const observer = new ResizeObserver(measure);
    const container = pane.parentElement!;
    for (const el of [container, ...container.children]) observer.observe(el);
    return () => observer.disconnect();
  }, [measure]);

  // Moves the edge toward `px`, within the bounds. A move that cannot shift the edge that
  // way writes nothing, so it leaves a stored width the layout is capping as it was.
  const resizeTo = useCallback(
    (px: number) => {
      const { now, min, max } = read();
      const next = Math.min(max, Math.max(min, px));
      if (px > now ? next - now >= 1 : now - next >= 1) onResize(next);
    },
    [read, onResize],
  );

  // A drag outlives the render it started in: the layout's bounds can move under it (the
  // right sidebar opening caps the left), so each move reads the latest `resizeTo`.
  const latestResizeTo = useRef(resizeTo);
  useEffect(() => {
    latestResizeTo.current = resizeTo;
  }, [resizeTo]);

  const handlePointerDown = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      // Measured on every move: a pane can still be moving, as the sidebar drawer does as it
      // slides in, and a left edge read once would leave the pane's edge off the pointer.
      const pane = e.currentTarget.parentElement!;
      dragFrom(e, 'col-resize', (ev) => {
        const { left, right } = pane.getBoundingClientRect();
        latestResizeTo.current(edge === 'right' ? ev.clientX - left : right - ev.clientX);
      });
    },
    [edge],
  );

  // The arrow pointing away from the pane grows it.
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    e.preventDefault();
    const grows = e.key === (edge === 'right' ? 'ArrowRight' : 'ArrowLeft');
    resizeTo(read().now + (grows ? KEY_STEP : -KEY_STEP));
  };

  return (
    // Focusable, a separator is a widget: it takes the arrows and carries its value.
    <div
      ref={ref}
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={range && Math.round(range.now)}
      aria-valuemin={range && Math.round(range.min)}
      aria-valuemax={range && Number.isFinite(range.max) ? Math.round(range.max) : undefined}
      tabIndex={0}
      onPointerDown={handlePointerDown}
      onKeyDown={onKeyDown}
      // The layout can move the bounds without resizing the pane.
      onFocus={measure}
      // A right-edge strip straddles the border, half of its 8px either side.
      className={cn(
        'absolute inset-y-0 z-20 w-2 cursor-col-resize touch-none outline-none focus-visible:bg-ring',
        edge === 'left' ? 'left-0' : '-right-1',
        className,
      )}
    />
  );
}
