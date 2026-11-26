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

// The column names over the log viewer's rows, in the rows' grid (kubetail's `HeaderRow`). It
// sits outside the scroller, a scroll container of its own with no bar drawn, and the two
// follow each other sideways, so a swipe over either moves both. It reserves the scroller's
// vertical scrollbar at its right edge so the names stay over their columns at the far right.
import { useEffect, useRef, useState } from 'react';

import type { Column } from './columns';
import { cellStyle } from './row';
import type { WidthKey } from './widths';

/** The header's height, its borders included. */
export const LOG_HEADER_PX = 19;

// The position and the dot draw no name; a screen reader still gets one. `Container` names a
// column of its own, so the dot's is the source.
const HIDDEN_NAMES: Partial<Record<WidthKey, string>> = { Pos: 'Position', Dot: 'Source' };

function Name({ col }: { col: WidthKey }) {
  const hidden = HIDDEN_NAMES[col];
  return hidden ? <span className="sr-only">{hidden}</span> : <span className="inline-block">{col}</span>;
}

export function LogViewerHeader({
  gridTemplate,
  columns,
  widths,
  rowWidth,
  wrap,
  scroller,
  measureCell,
}: {
  /** The rows' `gridTemplateColumns`. */
  gridTemplate: string;
  columns: Column[];
  widths: Map<WidthKey, number>;
  rowWidth: number;
  wrap: boolean;
  /** The core's scroller, once a landing has mounted one. */
  scroller: HTMLElement | null;
  measureCell?: (el: HTMLElement | null) => void;
}) {
  const wrapper = useRef<HTMLDivElement>(null);
  const [gutter, setGutter] = useState(0);

  useEffect(() => {
    if (!scroller) return undefined;
    const observer = new ResizeObserver(() => setGutter(scroller.offsetWidth - scroller.clientWidth));
    observer.observe(scroller);
    return () => observer.disconnect();
  }, [scroller]);

  // Each side follows the other once a frame. Setting one's `scrollLeft` fires the other's
  // scroll event, so the side that moved ignores the echo until the frame after.
  useEffect(() => {
    const header = wrapper.current;
    const content = scroller;
    if (!content || !header) return undefined;
    let headerScrolling = false;
    let contentScrolling = false;
    let contentFrame: number | null = null;
    let headerFrame: number | null = null;

    const onContentScroll = () => {
      if (headerScrolling || contentFrame !== null) return;
      contentFrame = requestAnimationFrame(() => {
        contentScrolling = true;
        header.scrollLeft = content.scrollLeft;
        contentFrame = null;
        requestAnimationFrame(() => {
          contentScrolling = false;
        });
      });
    };
    const onHeaderScroll = () => {
      if (contentScrolling || headerFrame !== null) return;
      headerFrame = requestAnimationFrame(() => {
        headerScrolling = true;
        content.scrollLeft = header.scrollLeft;
        headerFrame = null;
        requestAnimationFrame(() => {
          headerScrolling = false;
        });
      });
    };

    // A landing mounts a new scroller at the left edge, and it need not scroll to say so.
    header.scrollLeft = content.scrollLeft;
    content.addEventListener('scroll', onContentScroll, { passive: true });
    header.addEventListener('scroll', onHeaderScroll, { passive: true });
    return () => {
      if (contentFrame !== null) cancelAnimationFrame(contentFrame);
      if (headerFrame !== null) cancelAnimationFrame(headerFrame);
      content.removeEventListener('scroll', onContentScroll);
      header.removeEventListener('scroll', onHeaderScroll);
    };
  }, [scroller]);

  return (
    <div ref={wrapper} className="no-scrollbar w-full shrink-0 cursor-default overflow-x-scroll">
      <div className="flex">
        <div
          data-testid="log-header"
          className="grid shrink-0 border-y bg-muted text-xs uppercase [&>*:not(:last-child)]:border-r"
          style={{
            height: LOG_HEADER_PX,
            gridTemplateColumns: gridTemplate,
            minWidth: wrap ? '100%' : rowWidth || '100%',
          }}
        >
          {(['Pos', ...columns] as WidthKey[]).map((col) => (
            <div
              key={col}
              ref={col === 'Message' && wrap ? undefined : measureCell}
              data-col={col}
              className="flex items-center px-2 whitespace-nowrap"
              style={col === 'Message' && wrap ? undefined : cellStyle(widths, col)}
            >
              <Name col={col} />
            </div>
          ))}
        </div>
        {gutter > 0 && (
          <div
            data-testid="log-header-gutter"
            aria-hidden
            className="shrink-0 border-y bg-muted"
            style={{ width: gutter, height: LOG_HEADER_PX }}
          />
        )}
      </div>
    </div>
  );
}
