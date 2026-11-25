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

// The window's right sidebar: flush against the app bar and the window's bottom
// edge — a wall of the window, not a panel over the page. Closed until its toggle in
// the app bar asks for it.
//
// What it holds follows the mode, since the two have nothing in common: the dashboard
// gets the focused object, else a chat of its own; chat mode the focused log view, or a
// placeholder while none is set. The panel does not scroll — its contents own a scroller
// each, and one inside another gives two scrollbars that fight.
import type { RefObject } from 'react';
import { useCallback, useRef } from 'react';

import { DashboardChat } from '@/components/chat/dashboard-chat';
import { ObjectPanel } from '@/components/dashboard/object-panel';
import { LogsViewPanel } from '@/components/logs/log-panel';
import { ResizeHandle } from '@/components/primitives/resize-handle';
import type { AppMode } from '@/lib/app-mode';
import { useLogWindows } from '@/lib/log-windows';
import { useLogsView } from '@/lib/logs-view';
import { useObjectView } from '@/lib/object-view';
import { type WidthBounds, usePersistedWidth } from '@/lib/persisted-width';
import { useRightSidebar } from '@/lib/right-sidebar';

// Drag-resize bounds (px), per mode like the width itself: the two panels hold different
// things. No fixed maximum: a drag stops where the page reaches its floor, and a stored
// width wider than the window has room for shrinks to fit. `initial` is wide enough for a
// transcript to read on first open.
const WIDTH: Record<AppMode, WidthBounds> = {
  chat: { min: 240, max: Infinity, initial: 400 },
  dashboard: { min: 240, max: Infinity, initial: 400 },
};

// The page's floor, as `min(PAGE_MIN_WIDTH px, 50%)` of the content row: enough for the
// dashboard's table and the chat composer's controls.
const PAGE_MIN_WIDTH = 320;
export const pageMinWidth = `min(${PAGE_MIN_WIDTH}px, 50%)`;

const pageFloor = (rowWidth: number) => Math.min(PAGE_MIN_WIDTH, rowWidth / 2);

// The widest the left sidebar can be beside the page and this panel: the content row less
// the page's floor and, while this panel is open, its minimum. `css` is over the row's width
// (`100cqw`: the row is their container), for the layout to follow; `px` is for a row measured.
export type LeftSidebarRoom = { css: string; px: (rowWidth: number) => number };
export function leftSidebarRoom(mode: AppMode, open: boolean): LeftSidebarRoom {
  const reserved = open ? WIDTH[mode].min : 0;
  const floor = `min(${PAGE_MIN_WIDTH}px, 50cqw)`;
  return {
    css: open ? `calc(100cqw - ${floor} - ${reserved}px)` : `calc(100cqw - ${floor})`,
    px: (rowWidth) => rowWidth - pageFloor(rowWidth) - reserved,
  };
}

// How wide the panel can be before the page goes under its floor: everything right of the
// page's left edge, less the floor.
function room(aside: HTMLElement, page: HTMLElement) {
  const rowWidth = page.parentElement?.clientWidth ?? 0;
  return aside.getBoundingClientRect().right - page.getBoundingClientRect().left - pageFloor(rowWidth);
}
const widthKey = (mode: AppMode) => `right-sidebar-width:${mode}`;

function Placeholder({ title, detail }: { title: string; detail: string }) {
  return (
    <div className="flex flex-col gap-1 p-4">
      <h2 className="text-sm font-semibold">{title}</h2>
      <p className="text-sm text-muted-foreground">{detail}</p>
    </div>
  );
}

function ChatPanel() {
  const { view, viewport, setGrep } = useLogsView();
  const { close } = useRightSidebar();
  const { ready, open } = useLogWindows();
  // The panel's close is the toggle's collapse: the view stays focused, with its grep,
  // for the toggle or the card's Expand to bring back. Open in window moves it.
  if (view) {
    return (
      <LogsViewPanel
        view={view}
        viewport={viewport}
        onClose={close}
        onOpenInWindow={ready ? () => open(view) : undefined}
        onGrepApplied={setGrep}
      />
    );
  }
  return <Placeholder title="Conversation" detail="What the chat is working from will appear here." />;
}

// Closing the object brings the chat back; the toggle collapses either.
function DashboardPanel() {
  const { object, tab, setTab, clear } = useObjectView();
  if (object) return <ObjectPanel key={object.uid} object={object} tab={tab} onTab={setTab} onClose={clear} />;
  return <DashboardChat />;
}

export function RightSidebar({ page }: { page: RefObject<HTMLElement | null> }) {
  const { mode, open } = useRightSidebar();
  const [width, setWidth] = usePersistedWidth(widthKey(mode), WIDTH[mode]);
  const asideRef = useRef<HTMLElement>(null);

  // The widest a resize may make it: as wide as the page's floor allows.
  const bounds = useCallback(
    () => ({ min: WIDTH[mode].min, max: room(asideRef.current!, page.current!) }),
    [mode, page],
  );

  if (!open) return null;

  const onDashboard = mode === 'dashboard';

  return (
    // An ordinary item in the content row, so it takes its width from the page
    // rather than covering it, and the row's height makes it flush top and bottom.
    // `relative` anchors the resize handle on its inner edge. It shrinks before the page
    // goes under its floor, down to its minimum; the stored width is left alone for a wider
    // window.
    <aside
      ref={asideRef}
      data-testid="right-sidebar"
      aria-label={onDashboard ? 'Dashboard details' : 'Chat details'}
      className="relative flex flex-col overflow-hidden bg-card"
      style={{ width, minWidth: WIDTH[mode].min }}
    >
      {onDashboard ? <DashboardPanel /> : <ChatPanel />}
      <ResizeHandle edge="left" bounds={bounds} onResize={setWidth} label="Resize right sidebar" />
    </aside>
  );
}
