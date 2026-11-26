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

// The window's left sidebar, as the layout mounts it: `LeftSidebarPanel`, sliding between
// its width and none. Too narrow for it beside the page, it leaves the row, and the app
// bar's toggle opens it over the row as a drawer — so narrowing never throws a panel over
// the page.
//
// Open/closed state, the drawer's own state, and `Cmd/Ctrl+B` come from the
// library's `SidebarProvider`, mounted by `app-layout.tsx`; this reads them through
// `useSidebar`. Narrowness comes from there too (`isMobile`, its own hardcoded
// 768px) rather than a second media query of our own: it is what the toggle
// branches on, and a component rendering off a different rule would leave that
// button flipping a state nothing shows.
import { useSidebar } from '@kubetail/ui/elements/sidebar';
import { useLocation } from '@tanstack/react-router';
import { X } from 'lucide-react';
import type { MouseEvent, ReactNode } from 'react';
import { useCallback, useEffect, useMemo } from 'react';

import { leftSidebarRoom } from '@/components/shell/right-sidebar';
import { useFocusTrap } from '@/lib/focus-trap';
import { isImeKey } from '@/lib/ime';
import { useRightSidebar } from '@/lib/right-sidebar';

import { LeftSidebarPanel } from './panel';

type LeftSidebarProps = {
  /** Sidebar body (navigation, pickers); it scrolls. */
  nav?: ReactNode;
};

export function LeftSidebar({ nav }: LeftSidebarProps) {
  const { open, isMobile, openMobile, setOpenMobile } = useSidebar();
  const { mode, open: rightOpen } = useRightSidebar();
  const room = useMemo(() => leftSidebarRoom(mode, rightOpen), [mode, rightOpen]);
  const drawer = isMobile && openMobile;
  const trapRef = useFocusTrap<HTMLDivElement>(drawer);
  const closeDrawer = useCallback(() => setOpenMobile(false), [setOpenMobile]);

  // Escape closes it, as it does any overlay. The listener stays on `window`, which
  // is later in the bubble path than `document`: a dialog opened from inside the
  // drawer dismisses by stopping the keydown there, so one keypress closes the dialog
  // and leaves the drawer alone.
  useEffect(() => {
    if (!drawer) return undefined;
    const onKeyDown = (e: KeyboardEvent) => {
      // An IME's Escape drops its candidate and leaves the drawer open.
      if (e.key === 'Escape' && !isImeKey(e)) closeDrawer();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [drawer, closeDrawer]);

  // The drawer belongs to the narrow layout: widening the window ends it, so narrowing
  // again does not bring it back.
  useEffect(() => {
    if (!isMobile) closeDrawer();
  }, [isMobile, closeDrawer]);

  // A drawer left open covers the page it just opened, so a navigation closes it.
  // Keyed on the whole location: picking a resource moves the search param, not the
  // path. A link to where the window already is moves nothing, so a plain click on any
  // link in the drawer closes it too.
  const href = useLocation({ select: (location) => location.href });
  useEffect(() => {
    closeDrawer();
  }, [href, closeDrawer]);
  const closeOnLinkClick = (e: MouseEvent) => {
    // A modified click opens no page in this window, as the router leaves it alone.
    const modified = e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey;
    if (!modified && (e.target as Element).closest('a[href]')) closeDrawer();
  };

  // Beside the page, it leaves room for the page's floor and the right sidebar's minimum.
  if (!isMobile) {
    return <LeftSidebarPanel open={open} nav={nav} room={room} />;
  }
  if (!drawer) return null;
  return (
    // Both anchor to the content row (`app-layout.tsx` positions it).
    <>
      <button
        type="button"
        aria-label="Close sidebar"
        data-testid="sidebar-scrim"
        onClick={closeDrawer}
        className="absolute inset-0 z-30 cursor-default bg-black/40"
      />
      {/* A panel over the page has to hold the keyboard as well as the eye:
          the trap keeps Tab inside it and hands focus back on close, and
          `aria-modal` tells assistive tech to ignore what it covers. It slides in
          from off the row's left edge on mount. Its close button sits inside it, beside
          the panel's edge, so the controls the trap holds Tab among include one that
          dismisses it. */}
      {/* oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions -- it hears its links' own clicks, which a key press on a link sends too */}
      <div
        ref={trapRef}
        role="dialog"
        aria-modal
        aria-label="Sidebar"
        data-testid="sidebar-overlay"
        onClick={closeOnLinkClick}
        className="absolute inset-y-0 left-0 z-40 flex drop-shadow-xl transition-transform duration-200 ease-out starting:-translate-x-full motion-reduce:transition-none"
      >
        <LeftSidebarPanel open nav={nav} />
        <button
          type="button"
          aria-label="Close sidebar"
          onClick={closeDrawer}
          className="absolute top-2 left-full ml-2 flex size-7 items-center justify-center rounded-full bg-background text-muted-foreground shadow-sm hover:text-foreground"
        >
          <X aria-hidden className="size-4" />
        </button>
      </div>
    </>
  );
}
