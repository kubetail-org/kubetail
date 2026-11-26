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
import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it } from 'vitest';

import type { ChatToolCall } from '@/lib/chats';
import { RightSidebarProvider, useRightSidebar } from '@/lib/right-sidebar';

import { anchorLine, LogsViewProvider, useLogsView, viewOfCall } from './logs-view';
import type { LogsView } from './logs-view';

const view: LogsView = {
  clusterId: 'cluster-1',
  chatId: 'chat-1',
  callId: 'call-1',
  action: {
    sources: [
      { namespace: 'prod', kind: 'Deployment', name: 'webapp', containers: [], allContainers: false, previous: false },
    ],
    filters: [],
    grep: '',
    anchor: { kind: 'Tail', at: null },
    pinToEnd: true,
    resolvedAt: '2026-10-08T14:04:00Z',
  },
};

function wrapper({ children }: { children: ReactNode }) {
  return (
    <RightSidebarProvider mode="chat">
      <LogsViewProvider>{children}</LogsViewProvider>
    </RightSidebarProvider>
  );
}

// The focused view beside the sidebar's open state, which it writes.
const renderBoth = () => renderHook(() => ({ logs: useLogsView(), sidebar: useRightSidebar() }), { wrapper });

// The sidebar's open state is persisted app-wide, so each case starts pristine.
beforeEach(() => {
  localStorage.clear();
});

describe('useLogsView', () => {
  it('starts with no view, so a new window opens on none', () => {
    const { result } = renderBoth();
    expect(result.current.logs.view).toBeNull();
    expect(result.current.sidebar.open).toBe(false);
  });

  it('opens the right sidebar when a view is set', () => {
    const { result } = renderBoth();
    act(() => result.current.logs.set(view));
    expect(result.current.logs.view).toBe(view);
    expect(result.current.sidebar.open).toBe(true);
  });

  it('leaves the sidebar open when the view is cleared', () => {
    const { result } = renderBoth();
    act(() => result.current.logs.set(view));
    act(() => result.current.logs.clear());
    expect(result.current.logs.view).toBeNull();
    expect(result.current.sidebar.open).toBe(true);
  });

  it('holds one viewport for the window, whichever view is set', () => {
    const { result } = renderBoth();
    const { viewport } = result.current.logs;
    expect(viewport.get().range).toBeNull();
    act(() => result.current.logs.set(view));
    expect(result.current.logs.viewport).toBe(viewport);
  });

  it('keeps the user’s grep while its call stays focused, and drops it for another call', () => {
    const { result } = renderBoth();
    act(() => result.current.logs.set(view));
    act(() => result.current.logs.setGrep('panic'));
    expect(result.current.logs.view?.grep).toBe('panic');

    act(() => result.current.logs.set(view));
    expect(result.current.logs.view?.grep).toBe('panic');

    act(() => result.current.logs.set({ ...view, callId: 'call-2' }));
    expect(result.current.logs.view?.grep).toBeUndefined();
  });

  it('needs its provider', () => {
    expect(() => renderHook(() => useLogsView())).toThrow('within a LogsViewProvider');
  });
});

describe('viewOfCall', () => {
  const callWith = (action: Record<string, unknown> | null, background: unknown = null) =>
    ({ id: 'call-1', action, background }) as unknown as ChatToolCall;
  const around = { ...view.action, grep: 'around' };

  it("is a LogsView call's action", () => {
    expect(viewOfCall(callWith({ logsView: view.action, logsRead: null, logsWatch: null }))).toBe(view.action);
  });

  it("is a LogsRead call's view around", () => {
    expect(viewOfCall(callWith({ logsView: null, logsRead: { viewAround: around }, logsWatch: null }))).toBe(around);
  });

  it("is a LogsWatch call's task's view around, null while the watch runs", () => {
    const watch = { logsView: null, logsRead: null, logsWatch: {} };
    expect(viewOfCall(callWith(watch, { viewAround: around }))).toBe(around);
    expect(viewOfCall(callWith(watch, { viewAround: null }))).toBeNull();
    expect(viewOfCall(callWith(watch, null))).toBeNull();
  });

  it('is null for a call with no action, or one of another tool', () => {
    expect(viewOfCall(callWith(null))).toBeNull();
    expect(viewOfCall(callWith({ logsView: null, logsRead: null, logsWatch: null }))).toBeNull();
  });
});

describe('anchorLine', () => {
  it('names each edge and the moment as stamped', () => {
    expect(anchorLine({ kind: 'Head', at: null })).toBe('from the start');
    expect(anchorLine({ kind: 'Tail', at: null })).toBe('at the newest line');
    expect(anchorLine({ kind: 'At', at: '2026-10-08T14:02:00Z' })).toBe('from 2026-10-08T14:02:00Z');
    expect(anchorLine({ kind: 'At', at: null })).toBe('from ');
  });
});
