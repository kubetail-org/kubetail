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

// The left sidebar's panel: flush against the app bar, a step lighter than it and a step
// darker than the main panel. It slides between its stored width and none; the contents keep
// the open width and are clipped, so they slide out rather than reflow, and `inert` keeps Tab
// out while closed. The nav mounts only while the panel shows, the closing slide included:
// closed, it would hold its watches (the chat list's) for rows no one sees.
import { SidebarContent } from '@kubetail/ui/elements/sidebar';
import { cn } from '@kubetail/ui/lib/utils';
import type { ReactNode, TransitionEvent } from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';

import { ResizeHandle } from '@/components/primitives/resize-handle';
import type { LeftSidebarRoom } from '@/components/shell/right-sidebar';
import { usePersistedWidth } from '@/lib/persisted-width';

// Drag-resize bounds (px); `initial` matches the library's 16rem.
const WIDTH = { min: 200, max: 480, initial: 256 };
const WIDTH_KEY = 'sidebar-width';

// Closes the popups opened from `root`, leaving any opened elsewhere: a Base UI popup's
// trigger carries `aria-haspopup` and, while it is open, `aria-expanded`, and a click on
// it toggles the popup shut. A disclosure's `aria-expanded` has no `aria-haspopup`.
function closePopupsOpenedFrom(root: HTMLElement) {
  for (const trigger of root.querySelectorAll('[aria-haspopup][aria-expanded="true"]')) {
    trigger.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  }
}

// Whether `el`'s width changes with no transition, so no `transitionend` follows: under
// reduced motion, which leaves the duration but sets the property to `none`. The durations
// repeat to cover the properties, as in CSS.
function widthChangesInstantly(el: HTMLElement) {
  const { transitionProperty, transitionDuration } = getComputedStyle(el);
  const durations = transitionDuration.split(',');
  const slides = transitionProperty.split(',').some((property, i) => {
    const name = property.trim();
    return (name === 'width' || name === 'all') && Number.parseFloat(durations[i % durations.length]) > 0;
  });
  return !slides;
}

// `offsetWidth` is whole pixels; a dragged width need not be.
function atWidth(el: HTMLElement, px: number) {
  return Math.abs(el.offsetWidth - px) < 1;
}

type LeftSidebarPanelProps = {
  open: boolean;
  /** Caps the open width beside the page, below the stored width when the row is short. */
  room?: LeftSidebarRoom;
  /** Sidebar body (navigation, pickers); it scrolls. */
  nav?: ReactNode;
};

export function LeftSidebarPanel({ open, room, nav }: LeftSidebarPanelProps) {
  const [width, setWidth] = usePersistedWidth(WIDTH_KEY, WIDTH);
  const maxWidth = room?.css;
  // A popup opened from the panel renders outside it, beyond `inert`'s reach: closing the
  // panel closes it too.
  const panelRef = useRef<HTMLDivElement>(null);
  // The contents hold the open width, capped as the panel is, so the panel has slid open
  // once it is as wide as they are.
  const contentsRef = useRef<HTMLDivElement>(null);
  const openWidth = () => contentsRef.current!.offsetWidth;
  // Set as the panel opens, cleared once it has slid shut.
  const [navShown, setNavShown] = useState(open);
  if (open && !navShown) setNavShown(true);
  // Set once the panel has slid open. Before then its edge is short of its open width, and a
  // resize would jump the edge there. After it the width changes at once: only opening and
  // closing slide, so the edge keeps up with the pointer and each arrow press.
  const [slidOpen, setSlidOpen] = useState(open);
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    setSlidOpen(false);
  }
  // A panel already at its width (reopened before the closing slide moved it) starts no slide.
  useEffect(() => {
    if (!open) return;
    const panel = panelRef.current!;
    if (widthChangesInstantly(panel) || atWidth(panel, openWidth())) setSlidOpen(true);
  }, [open, width]);
  // A panel already at no width (closed before its opening width was laid out) starts no
  // slide either.
  useEffect(() => {
    if (open) return;
    const panel = panelRef.current!;
    closePopupsOpenedFrom(panel);
    if (widthChangesInstantly(panel) || panel.offsetWidth === 0) setNavShown(false);
  }, [open]);
  // A slide ends in `transitionend`, or in `transitioncancel` when it is dropped (reduced
  // motion turned on mid-slide). A slide a reversal cancels is still part-way, so each end
  // checks the panel reached its width: none to drop the nav, its own to show the handle.
  // The property is read off the native event: React's `transitioncancel` carries none of
  // its own.
  const onSlideDone = (e: TransitionEvent<HTMLDivElement>) => {
    if (e.target !== e.currentTarget || e.nativeEvent.propertyName !== 'width') return;
    if (!open && e.currentTarget.offsetWidth === 0) setNavShown(false);
    if (open && atWidth(e.currentTarget, openWidth())) setSlidOpen(true);
  };
  // The bounds the layout allows: under a cap, the cap, even below the minimum.
  const bounds = useCallback(() => {
    const max = room ? Math.min(WIDTH.max, room.px(panelRef.current!.parentElement!.clientWidth)) : WIDTH.max;
    return { min: Math.min(WIDTH.min, max), max };
  }, [room]);
  return (
    // An ordinary item in the content row: the row's height bounds it, and the page takes
    // whatever width is left. `relative` anchors the resize handle, which sits outside the
    // clip so it can straddle the edge.
    <div
      ref={panelRef}
      data-testid="left-sidebar-panel"
      inert={!open}
      className={cn(
        'relative flex shrink-0 bg-sidebar',
        !slidOpen && 'transition-[width] duration-200 ease-out motion-reduce:transition-none',
      )}
      style={{ width: open ? width : 0, maxWidth }}
      onTransitionEnd={onSlideDone}
      onTransitionCancel={onSlideDone}
    >
      {/* `justify-end` keeps the contents against the moving edge, so they slide with it. */}
      <div className="flex min-w-0 flex-1 justify-end overflow-hidden">
        <SidebarContent
          ref={contentsRef}
          data-testid="left-sidebar-contents"
          className="flex-none p-2"
          style={{ width, maxWidth }}
        >
          {navShown && nav}
        </SidebarContent>
      </div>
      {open && slidOpen && <ResizeHandle edge="right" bounds={bounds} onResize={setWidth} label="Resize sidebar" />}
    </div>
  );
}
