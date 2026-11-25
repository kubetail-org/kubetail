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

// Where a transcript card's view is drawn, and the buttons that move it. A view is in
// one place at a time: its card, the right sidebar, or a log window. The three cards
// that can show a view (`view-card.tsx`, `read-card.tsx`,
// `watch-card.tsx`) share these.
import { Button } from '@kubetail/ui/elements/button';

import type { AppMode } from '@/lib/app-mode';
import { useLogWindows } from '@/lib/log-windows';
import { useLogsView } from '@/lib/logs-view';
import type { LogsViewAction } from '@/lib/logs-view';
import { useRightSidebar } from '@/lib/right-sidebar';

export type ViewPlace = 'card' | 'sidebar' | 'window';

/** Where a call's view is drawn. Only chat mode has a sidebar view or log windows. */
export function useViewPlace(chatID: string, callID: string, mode: AppMode): ViewPlace {
  const { view } = useLogsView();
  const { open } = useRightSidebar();
  const { windowOf } = useLogWindows();
  if (mode !== 'chat') return 'card';
  if (windowOf(chatID, callID)) return 'window';
  if (open && view?.chatId === chatID && view.callId === callID) return 'sidebar';
  return 'card';
}

/**
 * A card's buttons for its view, in chat mode alone. `label` names the card's own
 * button (Expand, View around this), which focuses the view in the sidebar; Open in
 * window moves it to a window. Both wait for the chat's cluster, and the window
 * buttons for the provider. While the view is elsewhere the card says where, with
 * Show window for a log window.
 */
export function LogsViewButtons({
  label,
  place,
  chatID,
  callID,
  clusterID,
  action,
}: {
  label: string;
  place: ViewPlace;
  chatID: string;
  callID: string;
  clusterID: string | undefined;
  action: LogsViewAction;
}) {
  const { set } = useLogsView();
  const { ready, open, focus } = useLogWindows();
  if (place === 'sidebar') return <span className="shrink-0 opacity-70">Showing in the sidebar</span>;
  if (place === 'window') {
    return (
      <div className="flex shrink-0 items-center gap-2">
        <span className="opacity-70">Showing in a window</span>
        <Button variant="outline" size="sm" disabled={!ready} onClick={() => focus(callID)}>
          Show window
        </Button>
      </div>
    );
  }
  const view = clusterID === undefined ? undefined : { clusterId: clusterID, chatId: chatID, callId: callID, action };
  return (
    <div className="flex shrink-0 items-center gap-2">
      <Button variant="outline" size="sm" disabled={!view} onClick={() => view && set(view)}>
        {label}
      </Button>
      <Button variant="outline" size="sm" disabled={!view || !ready} onClick={() => view && open(view)}>
        Open in window
      </Button>
    </div>
  );
}
