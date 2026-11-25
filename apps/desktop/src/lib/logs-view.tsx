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
// The focused view: the one log view a window draws in full, in chat mode's right
// sidebar. It is a call of a chat, the call's id and the view the sidecar resolved:
// a LogsView call's action, or a LogsRead or LogsWatch call's View around this. It is held
// beside the right sidebar's state. It is chrome, not what the window
// is looking at, so it is out of the URL and not persisted: a new window opens with
// none. `set` also opens the right sidebar, since a view set with the panel closed is
// a view nobody sees. The grep the user applies from the viewer's toolbar is held here,
// not in the panel, so it outlives the panel's unmount while the sidebar is collapsed.
// `viewport` is where the viewer writes what is on screen, one per window, which the
// composer pulls at send.
import { createContext, useContext, useMemo, useState } from 'react';
import type { ReactNode } from 'react';

import type { ChatToolCall } from '@/lib/chats';
import type { LogViewportHandle } from '@/lib/log-viewport';
import { createLogViewportHandle } from '@/lib/log-viewport';
import { useRightSidebar } from '@/lib/right-sidebar';

export type LogsViewAction = NonNullable<NonNullable<ChatToolCall['action']>['logsView']>;

export type LogsReadAction = NonNullable<NonNullable<ChatToolCall['action']>['logsRead']>;

export type LogsWatchAction = NonNullable<NonNullable<ChatToolCall['action']>['logsWatch']>;

/** A LogsWatch call's task: its lines, why it ended and its view around arrive on it. */
export type LogsWatchTask = NonNullable<ChatToolCall['background']>;

export type LogsView = {
  /** The chat's cluster: the action does not carry it, and the client reads it. */
  clusterId: string;
  chatId: string;
  callId: string;
  action: LogsViewAction;
  /** The grep the user applied over the action's; unset until they do. */
  grep?: string;
};

/**
 * The view a call opens, as its card's Expand or View around this sets it: a LogsView
 * call's action, a LogsRead call's view around, or a LogsWatch task's, which arrives
 * when the watch ends. Null for any other call.
 */
export function viewOfCall(call: Pick<ChatToolCall, 'action' | 'background'>): LogsViewAction | null {
  if (call.action?.logsView) return call.action.logsView;
  if (call.action?.logsRead) return call.action.logsRead.viewAround;
  if (call.action?.logsWatch) return call.background?.viewAround ?? null;
  return null;
}

// Where a view starts, in words. The moment is drawn as the backend stamped it.
export function anchorLine(anchor: LogsViewAction['anchor']): string {
  if (anchor.kind === 'Head') return 'from the start';
  if (anchor.kind === 'Tail') return 'at the newest line';
  return `from ${anchor.at ?? ''}`;
}

type LogsViewContextValue = {
  view: LogsView | null;
  /** Focuses a view, keeping the user's grep when it is the call already focused. */
  set: (view: LogsView) => void;
  setGrep: (grep: string) => void;
  clear: () => void;
  viewport: LogViewportHandle;
};

const LogsViewContext = createContext<LogsViewContextValue | null>(null);

export function LogsViewProvider({ children }: { children: ReactNode }) {
  const [view, setView] = useState<LogsView | null>(null);
  const [viewport] = useState(createLogViewportHandle);
  const { show } = useRightSidebar();
  const value = useMemo<LogsViewContextValue>(
    () => ({
      view,
      set: (next) => {
        setView((last) => (last?.callId === next.callId ? { ...next, grep: last.grep } : next));
        show();
      },
      setGrep: (grep) => setView((last) => last && { ...last, grep }),
      clear: () => setView(null),
      viewport,
    }),
    [view, show, viewport],
  );
  return <LogsViewContext.Provider value={value}>{children}</LogsViewContext.Provider>;
}

export function useLogsView(): LogsViewContextValue {
  const ctx = useContext(LogsViewContext);
  if (!ctx) throw new Error('useLogsView must be used within a LogsViewProvider');
  return ctx;
}
