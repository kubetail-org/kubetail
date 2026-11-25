import { useEffect, useState } from 'react';
import type { RefObject } from 'react';

/**
 * Whether the element has kept its place on screen for `ms`: its bounding rect,
 * read every animation frame while it is mounted, unchanged for that long. Any
 * move starts the wait again — something above it coming, going or growing, the
 * page scrolling it — so a click aimed elsewhere cannot land on it as it slides
 * under the pointer. `0` holds at once.
 */
export function useHeldStill(ref: RefObject<HTMLElement | null>, ms: number): boolean {
  const [held, setHeld] = useState(ms <= 0);

  useEffect(() => {
    if (ms <= 0) return undefined;
    let last: DOMRect | undefined;
    let since = 0;
    const tick = (now: number) => {
      const rect = ref.current?.getBoundingClientRect();
      const moved =
        !rect ||
        !last ||
        rect.x !== last.x ||
        rect.y !== last.y ||
        rect.width !== last.width ||
        rect.height !== last.height;
      if (moved) since = now;
      setHeld(!moved && now - since >= ms);
      last = rect;
      frame = requestAnimationFrame(tick);
    };
    let frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [ref, ms]);

  return held;
}
