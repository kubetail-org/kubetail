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
import { useCallback, useRef } from 'react';
import type { ReactNode } from 'react';

import { ResizeHandle } from '@/components/primitives/resize-handle';
import { usePersistedWidth } from '@/lib/persisted-width';

type ListPaneProps = {
  // The storage name its width is kept under.
  widthKey: string;
  resizeLabel: string;
  children: ReactNode;
};

// The detail keeps at least DETAIL_MIN_PX beside the pane, so on a row too narrow for both
// the list goes under its minimum, to nothing if it must.
const LIST_WIDTH = { min: 240, max: Number.POSITIVE_INFINITY, initial: 320 };
const DETAIL_MIN_PX = 360;
// The 16px strip between the panes, its grip centred on the list pane's edge: the pane draws
// the strip's first half, the detail's margin the second, so the handle on that edge sits
// under the grip.
const HALF_STRIP_PX = 8;

// The list beside an open row's detail, in the row the two share: its controls over its
// rows, at a width its right edge drags.
export function ListPane({ widthKey, resizeLabel, children }: ListPaneProps) {
  const [width, setWidth] = usePersistedWidth(widthKey, LIST_WIDTH);
  const ref = useRef<HTMLDivElement>(null);
  const bounds = useCallback(() => {
    const max = Math.max(0, ref.current!.parentElement!.clientWidth - DETAIL_MIN_PX - HALF_STRIP_PX);
    return { min: Math.min(LIST_WIDTH.min + HALF_STRIP_PX, max), max };
  }, []);
  const onResize = useCallback((px: number) => setWidth(px - HALF_STRIP_PX), [setWidth]);

  return (
    <div
      ref={ref}
      className="relative flex min-w-0 shrink-0 flex-col gap-3"
      style={{
        width: width + HALF_STRIP_PX,
        maxWidth: `calc(100% - ${DETAIL_MIN_PX + HALF_STRIP_PX}px)`,
        paddingRight: HALF_STRIP_PX,
      }}
    >
      {children}
      <div aria-hidden className="pointer-events-none absolute inset-y-0 -right-2 flex w-4 items-center justify-center">
        <div className="h-8 w-1 rounded-full bg-muted-foreground" />
      </div>
      <ResizeHandle edge="right" label={resizeLabel} bounds={bounds} onResize={onResize} className="-right-2 w-4" />
    </div>
  );
}
