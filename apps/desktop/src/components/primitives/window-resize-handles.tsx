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

// Invisible edge/corner resize grips for the frameless Linux/Windows window
// (`decorations(false)` drops the native resize borders); each starts a native
// resize drag. Corners sit above the edges (later in DOM + wider). Renders nothing
// on macOS or while maximized. Must remain a SIBLING of `WindowFrame`, never a
// child — its `contain: paint` would re-anchor and clip the fixed grips.
// See docs/adr/2026-08-09-per-platform-window-chrome.md
import { getCurrentWindow } from '@tauri-apps/api/window';

import { isLinux, isMacOS } from '@/lib/platform';
import { useWindowMaximized } from '@/lib/window-maximized';

// `@tauri-apps/api` declares this union but doesn't export it; structurally
// matches `startResizeDragging`'s argument.
type ResizeDirection = 'North' | 'South' | 'East' | 'West' | 'NorthEast' | 'NorthWest' | 'SouthEast' | 'SouthWest';

function startResize(direction: ResizeDirection) {
  getCurrentWindow()
    .startResizeDragging(direction)
    .catch(() => {});
}

// `select-none` + `touch-none` keep the webview from claiming the press (see the
// mousedown handler).
const EDGE = 'fixed z-50 select-none touch-none';
const CORNER = 'fixed z-50 select-none touch-none';

function buildHandles(edge: { h: string; w: string }, corner: string) {
  return [
    // Edges.
    { key: 'n', className: `${EDGE} inset-x-0 top-0 ${edge.h}`, cursor: 'ns-resize', direction: 'North' },
    { key: 's', className: `${EDGE} inset-x-0 bottom-0 ${edge.h}`, cursor: 'ns-resize', direction: 'South' },
    { key: 'w', className: `${EDGE} inset-y-0 left-0 ${edge.w}`, cursor: 'ew-resize', direction: 'West' },
    { key: 'e', className: `${EDGE} inset-y-0 right-0 ${edge.w}`, cursor: 'ew-resize', direction: 'East' },
    // Corners (above the edges).
    { key: 'nw', className: `${CORNER} ${corner} left-0 top-0`, cursor: 'nwse-resize', direction: 'NorthWest' },
    { key: 'ne', className: `${CORNER} ${corner} right-0 top-0`, cursor: 'nesw-resize', direction: 'NorthEast' },
    { key: 'sw', className: `${CORNER} ${corner} left-0 bottom-0`, cursor: 'nesw-resize', direction: 'SouthWest' },
    { key: 'se', className: `${CORNER} ${corner} right-0 bottom-0`, cursor: 'nwse-resize', direction: 'SouthEast' },
  ] satisfies { key: string; className: string; cursor: string; direction: ResizeDirection }[];
}

// Linux: wide strips filling the transparent gutter. Windows (full-bleed): thin
// edge-hugging strips so the resize cursor stays at the very edge.
const LINUX_HANDLES = buildHandles({ h: 'h-4', w: 'w-4' }, 'size-5');
const NARROW_HANDLES = buildHandles({ h: 'h-[5px]', w: 'w-[5px]' }, 'size-2.5');

export function WindowResizeHandles() {
  const maximized = useWindowMaximized();
  if (isMacOS() || maximized) return null;
  const handles = isLinux() ? LINUX_HANDLES : NARROW_HANDLES;
  return (
    <>
      {handles.map(({ key, className, cursor, direction }) => (
        <div
          key={key}
          aria-hidden
          className={className}
          style={{ cursor }}
          onMouseDown={(e) => {
            // Primary button only.
            if (e.button !== 0) return;
            // Critical on Linux/WebKitGTK: if the webview starts a selection grab
            // on this press, GTK's async resize grab loses the race and the drag
            // fails into a text selection. `mousedown` (not `pointerdown`)
            // preventDefault is what reliably suppresses it.
            e.preventDefault();
            startResize(direction);
          }}
        />
      ))}
    </>
  );
}
