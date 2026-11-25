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
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { AppMode } from '@/lib/app-mode';
import type { ChatToolCall } from '@/lib/chats';
import type { FetchQuery } from '@/lib/log-viewer';
import type { LogsViewAction } from '@/lib/logs-view';
import { fakeLogWindows } from '@/test-utils';

// The seam: the client the window builds reads nothing, and says what it was asked.
const { fetched } = vi.hoisted(() => ({ fetched: [] as FetchQuery[] }));
vi.mock('urql', () => ({ useClient: () => ({}) }));
vi.mock('@/lib/logs-client', () => ({
  createLogsClient: () => ({
    fetch: (query: FetchQuery) => {
      fetched.push(query);
      return new Promise(() => {});
    },
    tail: () => () => {},
  }),
}));

const { LogsViewProvider, useLogsView } = await import('@/lib/logs-view');
const { LogWindowsContext } = await import('@/lib/log-windows');
const { windows, value: logWindows } = fakeLogWindows();
const { RightSidebarProvider, useRightSidebar } = await import('@/lib/right-sidebar');
const { LogsViewCard } = await import('./view-card');

beforeEach(() => {
  windows.length = 0;
  logWindows.open.mockReset();
  logWindows.focus.mockReset();
  fetched.length = 0;
  localStorage.clear();
});

const action: LogsViewAction = {
  sources: [
    {
      namespace: 'prod',
      kind: 'Deployment',
      name: 'web‮app',
      containers: ['app'],
      allContainers: false,
      previous: false,
    },
    { namespace: 'prod', kind: 'Pod', name: 'db-0', containers: [], allContainers: false, previous: true },
  ],
  filters: [],
  grep: 'error',
  anchor: { kind: 'At', at: '2026-10-08T14:02:00Z' },
  pinToEnd: true,
  resolvedAt: '2026-10-08T14:04:00Z',
};

const call = {
  id: 'call-1',
  toolUseID: 'tu-1',
  name: 'LogsView',
  contract: 'logsview/v1',
  arguments: {},
  status: 'Succeeded',
  runsOn: 'Sidecar',
  actionKind: 'LogsView',
  action: {
    description: 'Tail the app',
    command: null,
    read: null,
    write: null,
    edit: null,
    search: null,
    fetch: null,
    memory: null,
    delegate: null,
    kubeQuery: null,
    logsView: action,
    logsRead: null,
    logsWatch: null,
  },
  approval: null,
  network: null,
  output: 'Viewing deployments/webapp in prod from 2026-10-08T14:02:00Z, pinned to the end.',
  background: null,
  agentCallID: null,
  sandboxActions: [],
} as ChatToolCall;

// What the focused view is, as the card set it, and the sidebar's controls.
function Focused() {
  const { view, clear } = useLogsView();
  const { toggle } = useRightSidebar();
  return (
    <>
      <output data-testid="focused">{view ? `${view.clusterId}/${view.chatId}/${view.callId}` : 'none'}</output>
      <button type="button" onClick={toggle}>
        Toggle sidebar
      </button>
      <button type="button" onClick={clear}>
        Clear focus
      </button>
    </>
  );
}

const tail: LogsViewAction = { ...action, anchor: { kind: 'Tail', at: null } };

// A null cluster draws the card before the list watch names the chat's.
const draw = ({
  mode = 'chat',
  clusterID = 'cluster-1',
  view = action,
  until = null,
}: {
  mode?: AppMode;
  clusterID?: string | null;
  view?: LogsViewAction;
  until?: string | null;
} = {}) =>
  render(
    <RightSidebarProvider mode={mode}>
      <LogsViewProvider>
        <LogWindowsContext.Provider value={logWindows}>
          <LogsViewCard
            call={call}
            action={view}
            chatID="chat-1"
            clusterID={clusterID ?? undefined}
            mode={mode}
            until={until}
          />
          <Focused />
        </LogWindowsContext.Provider>
      </LogsViewProvider>
    </RightSidebarProvider>,
  );

const inline = () => screen.queryByTestId('logs-inline');

describe('LogsViewCard', () => {
  it('names the view, where it opened, the description and the receipt, every name spelled out', () => {
    const { container } = draw();
    expect(container).toHaveTextContent('Logs: Deployment web');
    expect(container).toHaveTextContent('app in prod (app) and 1 more');
    expect(container).toHaveTextContent('from 2026-10-08T14:02:00Z, pinned to the end, matching /error/');
    expect(container).toHaveTextContent('“Tail the app”');
    expect(container).toHaveTextContent(call.output);
    expect(container.querySelector('mark')).not.toBeNull();
  });

  it('Expand makes the call the focused view, and the card then says so', () => {
    draw();
    expect(screen.getByTestId('focused')).toHaveTextContent('none');

    fireEvent.click(screen.getByRole('button', { name: 'Expand' }));

    expect(screen.getByTestId('focused')).toHaveTextContent('cluster-1/chat-1/call-1');
    expect(screen.queryByRole('button', { name: 'Expand' })).not.toBeInTheDocument();
    expect(screen.getByText('Showing in the sidebar')).toBeInTheDocument();
  });

  it('Open in window moves the call’s view to a window', () => {
    draw();
    fireEvent.click(screen.getByRole('button', { name: 'Open in window' }));
    expect(logWindows.open).toHaveBeenCalledWith({
      clusterId: 'cluster-1',
      chatId: 'chat-1',
      callId: 'call-1',
      action,
    });
  });

  it('says the view is in a window, with Show window and no preview, while its call has one', () => {
    windows.push({ label: 'logs-1', clusterId: 'cluster-1', chatId: 'chat-1', callId: 'call-1', focusSeq: 1 });
    draw();
    expect(screen.getByText('Showing in a window')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Expand' })).not.toBeInTheDocument();
    expect(inline()).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Show window' }));
    expect(logWindows.focus).toHaveBeenCalledWith('call-1');
  });

  it('holds Expand until the chat’s cluster is known', () => {
    draw({ clusterID: null });
    expect(screen.getByRole('button', { name: 'Expand' })).toBeDisabled();
  });

  it('has no Expand on the dashboard', () => {
    draw({ mode: 'dashboard' });
    expect(screen.queryByRole('button', { name: 'Expand' })).not.toBeInTheDocument();
    expect(screen.queryByText('Showing in the sidebar')).not.toBeInTheDocument();
    expect(inline()).toBeInTheDocument();
  });

  it('draws its window under the receipt, the newest pinned tail reading live', () => {
    draw({ view: tail });
    expect(inline()).toBeInTheDocument();
    expect(fetched).toEqual([{ kind: 'Last', limit: 8 }]);
  });

  it('reads an older card up to the next view’s moment', () => {
    draw({ view: tail, until: '2026-10-08T14:09:00Z' });
    expect(fetched).toEqual([{ kind: 'At', at: '2026-10-08T14:09:00Z', limit: 8 }]);
  });

  it('draws the window empty until the chat’s cluster is known', () => {
    draw({ clusterID: null });
    expect(inline()).toBeInTheDocument();
    expect(fetched).toEqual([]);
  });

  it('the focused card draws no window while the sidebar is open, and draws it again once hidden or cleared', () => {
    draw();
    fireEvent.click(screen.getByRole('button', { name: 'Expand' }));
    expect(inline()).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Toggle sidebar' }));
    expect(inline()).toBeInTheDocument();
    expect(screen.queryByText('Showing in the sidebar')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Expand' }));
    expect(inline()).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Clear focus' }));
    expect(inline()).toBeInTheDocument();
  });
});
