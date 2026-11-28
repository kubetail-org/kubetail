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

// The main window's layout, mounted once by the pathless `_app` route: the app bar
// over an inset box holding the left sidebar, the routed page and the right sidebar.
// Secondary windows get their own sidebar-less layout alongside this one.
//
// Every band is a flex item, so the sidebar and the page start below the chrome by
// construction. The column's height is definite (`h-`, not `min-h-`) and `main`
// clips, so a route scrolls inside it, never the page. Every plain flex item between
// here and a route's scroller needs `min-h-0` to shrink into it.
import { SidebarProvider } from '@kubetail/ui/elements/sidebar';
import { Outlet, useLocation } from '@tanstack/react-router';
import { useRef } from 'react';

import { ChatNav } from '@/components/chat/chat-nav';
import { DashboardResourceNav } from '@/components/dashboard/dashboard-resource-nav';
import { AppBar } from '@/components/shell/app-bar';
import { AppDialogs } from '@/components/shell/app-dialogs';
import { ConnectionStatus } from '@/components/shell/connection-status';
import { LeftSidebar } from '@/components/shell/left-sidebar';
import { RightSidebar, pageMinWidth } from '@/components/shell/right-sidebar';
import { useFixWindowContext } from '@/lib/active-kube-context';
import type { AppMode } from '@/lib/app-mode';
import { ChatOutboxProvider } from '@/lib/chat-outbox';
import { ChatSwitchProvider } from '@/lib/chat-switch';
import { ClusterCachedDataKindsProvider } from '@/lib/cluster-cached-data-kinds';
import { ClusterNamespacesProvider } from '@/lib/cluster-namespaces';
import { DialogProvider } from '@/lib/dialog';
import { IncidentsProvider } from '@/lib/incidents';
import { LogWindowsProvider } from '@/lib/log-windows';
import { LogsViewProvider } from '@/lib/logs-view';
import { ObjectViewProvider } from '@/lib/object-view';
import { usePersistedFlag } from '@/lib/persisted-flag';
import { RightSidebarProvider } from '@/lib/right-sidebar';

export function AppLayout() {
  // Each mode's nav mounts only in that mode. This layout stays mounted across
  // the mode switch, so subscribe via `useLocation` (re-renders per navigation) —
  // `useMatchRoute` would read stale until a reload.
  const pathname = useLocation({ select: (location) => location.pathname });
  const mode: AppMode = pathname === '/dashboard' || pathname.startsWith('/dashboard/') ? 'dashboard' : 'chat';
  const onDashboard = mode === 'dashboard';
  // The library's provider persists open/collapsed to a `sidebar_state` cookie it
  // never reads back, so hold the state here to keep it across windows.
  const [sidebarOpen, setSidebarOpen] = usePersistedFlag('sidebar-open', true);
  // The right sidebar measures the page to stop a drag at the page's floor.
  const pageRef = useRef<HTMLElement>(null);

  return (
    // The dialogs host sits outside the sidebar and the bar alike; the controls in
    // them only request an open.
    // The chat outbox lives here, above the routes: the first send moves chat mode to
    // the new chat's own route, which unmounts the pane that held the text. A sandbox
    // or network switch in flight is held beside it for the same reason.
    <ChatOutboxProvider>
      <ChatSwitchProvider>
        <DialogProvider>
          {/* Above the bar and the dialogs alike: the bell and the Incidents dialog read
          the same watches, so neither joins a shared stream late. */}
          <IncidentsProvider>
            <ConnectionStatus />
            <FixWindowContext />
            {/* Both sidebars' states wrap the bar and the row alike: their toggles are
          in one, their panels in the other. The library's provider owns the left
          sidebar's narrow-window state and `Cmd/Ctrl+B`; it hardcodes `min-h-svh`,
          taller than the `WindowFrame` inset on Linux, and only `min-h-0` displaces
          it (same tailwind-merge group) — `h-` alone would not. */}
            <RightSidebarProvider mode={mode}>
              {/* The focused view and the focused object open the right sidebar when
            set, so they sit inside the sidebar's state and above the row that draws it. */}
              <ObjectViewProvider>
                <LogsViewProvider>
                  {/* The log windows move the focused view out of the sidebar, so they sit
              inside both. */}
                  <LogWindowsProvider>
                    {/* Above the sidebar and the routes: both read the catalog, and the
                breadcrumb bar's namespace picker reads the namespaces on every page. */}
                    <ClusterCachedDataKindsProvider enabled={onDashboard}>
                      <ClusterNamespacesProvider enabled={onDashboard}>
                        <SidebarProvider
                          open={sidebarOpen}
                          onOpenChange={setSidebarOpen}
                          className="h-(--app-min-h) min-h-0 flex-col bg-app-bar"
                        >
                          <AppBar />
                          {/* The content row, a box inset 8px from the window's left, right and
                      bottom edges; the column's fill, the bar's colour, shows around it.
                      `relative`: too narrow for the sidebar beside the page, `LeftSidebar`
                      floats it over this box as a drawer. `overflow-clip`: the three items'
                      minimum widths can sum past a narrow window, an absolute element with no
                      nearer positioned ancestor anchors here, past `main`'s clip, and the
                      children clip to the rounded corners. `@container`: the left sidebar caps
                      its width by the box's (`leftSidebarRoom`). */}
                          <div className="@container relative mx-2 mb-2 flex min-h-0 flex-1 overflow-clip rounded-xl border border-border shadow-[0_1px_1px_rgb(0_0_0/0.1),0_1px_1.5px_rgb(0_0_0/0.1)]">
                            <LeftSidebar nav={onDashboard ? <DashboardResourceNav /> : <ChatNav mode="chat" />} />
                            <main
                              ref={pageRef}
                              className="flex min-h-0 flex-1 flex-col overflow-hidden bg-background"
                              style={{ minWidth: pageMinWidth }}
                            >
                              <Outlet />
                            </main>
                            <RightSidebar page={pageRef} />
                          </div>
                        </SidebarProvider>
                      </ClusterNamespacesProvider>
                    </ClusterCachedDataKindsProvider>
                  </LogWindowsProvider>
                </LogsViewProvider>
              </ObjectViewProvider>
            </RightSidebarProvider>
            <AppDialogs />
          </IncidentsProvider>
        </DialogProvider>
      </ChatSwitchProvider>
    </ChatOutboxProvider>
  );
}

// A leaf of its own, so the cluster watch's batches re-render it alone, not the layout.
function FixWindowContext() {
  useFixWindowContext();
  return null;
}
