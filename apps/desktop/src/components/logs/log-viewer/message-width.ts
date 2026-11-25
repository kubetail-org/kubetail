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

// How wide the message column is while wrap is on: what the wrap estimate divides a message
// by. The other visible columns keep their measured widths, so the message gets what is left
// of the scroller.
import { useEffect, useState } from 'react';

import type { Column } from './columns';
import type { WidthKey } from './widths';

export const MIN_MESSAGE_PX = 100;

/** The estimate's first guess, until the scroller reports its size. */
const UNMEASURED_PX = 400;

// A `ResizeObserver` reports once on attach, so the scroller's width is read the frame it
// mounts and on every resize after, coalesced per frame. Only a wrapped message reads it, so
// nothing is observed while wrap is off.
function useScrollerWidth(scroller: HTMLElement | null, wrap: boolean): number {
  const [width, setWidth] = useState(UNMEASURED_PX);
  useEffect(() => {
    if (!scroller || !wrap) return undefined;
    let frame: number | null = null;
    const observer = new ResizeObserver(() => {
      if (frame !== null) return;
      frame = requestAnimationFrame(() => {
        frame = null;
        setWidth(scroller.clientWidth);
      });
    });
    observer.observe(scroller);
    return () => {
      if (frame !== null) cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, [scroller, wrap]);
  return width;
}

export function useMessageWidth(
  scroller: HTMLElement | null,
  widths: Map<WidthKey, number>,
  columns: Column[],
  wrap: boolean,
): number {
  const width = useScrollerWidth(scroller, wrap);
  let others = widths.get('Pos') ?? 0;
  columns.forEach((col) => {
    if (col !== 'Message') others += widths.get(col) ?? 0;
  });
  return Math.max(MIN_MESSAGE_PX, width - others);
}
