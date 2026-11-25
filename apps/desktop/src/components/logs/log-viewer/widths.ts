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

// The log viewer's column widths, measured off the cells drawn. Rows are positioned apart, so
// each is its own grid; every cell carries its column's width as `minWidth`, which is what
// lines the rows up. Widths only grow, and the mount's key resets them with the view.
import { useCallback, useEffect, useRef, useState } from 'react';

import type { Column } from './columns';

/** A width's key: a column, or the position cell every row opens with. */
export type WidthKey = Column | 'Pos';

/** A cell's `px-2`, both sides. */
export const CELL_PADDING_PX = 16;

/** Kubetail's position column: a sign and five digits. */
export const POS_MIN_PX = 48;

const initialWidths = (): Map<WidthKey, number> => new Map([['Pos', POS_MIN_PX]]);

export function useMeasureWidths() {
  const [widths, setWidths] = useState(initialWidths);
  const [rowWidth, setRowWidth] = useState(0);

  const pending = useRef({ widths: initialWidths(), rowWidth: 0 });
  const dirty = useRef({ widths: false, rowWidth: false });
  const measured = useRef(new WeakSet<Element>());
  const frame = useRef<number | null>(null);

  // One publish per frame, and a new map only when a width moved: a new map re-renders every
  // row memoized over it.
  const flush = useCallback(() => {
    if (frame.current !== null) return;
    frame.current = requestAnimationFrame(() => {
      frame.current = null;
      if (dirty.current.widths) setWidths(new Map(pending.current.widths));
      if (dirty.current.rowWidth) setRowWidth(pending.current.rowWidth);
      dirty.current = { widths: false, rowWidth: false };
    });
  }, []);

  // Refs measure before effects run, so StrictMode's replay cancels a frame already asked for;
  // the setup asks again for whatever it left unpublished.
  useEffect(() => {
    if (dirty.current.widths || dirty.current.rowWidth) flush();
    return () => {
      if (frame.current !== null) cancelAnimationFrame(frame.current);
      frame.current = null;
    };
  }, [flush]);

  // A cell is measured by its content, never by itself: its own width is its track and its
  // `minWidth`, so it would report the clamp, or grow on every render.
  const measureCell = useCallback(
    (el: HTMLElement | null) => {
      if (!el || measured.current.has(el)) return;
      measured.current.add(el);
      const content = el.firstElementChild;
      const col = el.dataset.col as WidthKey | undefined;
      if (!content || !col) return;
      const next = Math.ceil(content.getBoundingClientRect().width) + CELL_PADDING_PX;
      if (next <= (pending.current.widths.get(col) ?? 0)) return;
      pending.current.widths.set(col, next);
      dirty.current.widths = true;
      flush();
    },
    [flush],
  );

  const measureRow = useCallback(
    (el: HTMLElement | null) => {
      if (!el || measured.current.has(el)) return;
      measured.current.add(el);
      if (el.scrollWidth <= pending.current.rowWidth) return;
      pending.current.rowWidth = el.scrollWidth;
      dirty.current.rowWidth = true;
      flush();
    },
    [flush],
  );

  return { widths, rowWidth, measureCell, measureRow };
}
