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
import type { LogsWatchAction, LogsWatchTask } from '@/lib/logs-view';
import { fakeLogWindows } from '@/test-utils';

// The card makes no client: any request it made would land here.
const { requested } = vi.hoisted(() => ({ requested: [] as unknown[] }));
vi.mock('urql', () => ({ useClient: () => requested.push('client') }));
vi.mock('@/lib/logs-client', () => ({ createLogsClient: () => requested.push('logs client') }));

const { LogsViewProvider, useLogsView } = await import('@/lib/logs-view');
const { LogWindowsContext } = await import('@/lib/log-windows');
const { windows, value: logWindows } = fakeLogWindows();
const { RightSidebarProvider } = await import('@/lib/right-sidebar');
const { LogsWatchCard } = await import('./watch-card');

beforeEach(() => {
  windows.length = 0;
  logWindows.open.mockReset();
  logWindows.focus.mockReset();
  requested.length = 0;
  localStorage.clear();
});

const line = (i: number, message = `panic ${i}`): LogsWatchTask['matched'][number] => ({
  timestamp: `2026-10-18T13:51:${String(i).padStart(2, '0')}Z`,
  namespace: 'payments',
  pod: 'webapp-a',
  container: 'app',
  previous: false,
  message,
  truncatedBytes: 0,
  gap: null,
});

const sources = [
  {
    namespace: 'payments',
    kind: 'Deployment',
    name: 'webapp',
    containers: ['app'],
    allContainers: false,
    previous: false,
  },
] as LogsWatchAction['sources'];

const action: LogsWatchAction = {
  sources,
  filters: [],
  grep: 'panic|Exception',
  since: '2026-10-18T13:45:00Z',
  limit: 100,
  maxBytes: 16384,
  maxMessageBytes: 4096,
  resolvedAt: '2026-10-18T13:50:00Z',
  until: '2026-10-18T14:00:00Z',
};

const viewAround = {
  sources,
  filters: [],
  grep: 'panic|Exception',
  anchor: { kind: 'At', at: '2026-10-18T13:51:00Z' },
  pinToEnd: false,
  resolvedAt: '2026-10-18T13:55:00Z',
} as LogsWatchTask['viewAround'];

const matched: LogsWatchTask = {
  status: 'Matched',
  exitCode: null,
  report: '',
  matched: [
    line(0, '<b>bold</b> ‮reversed'),
    { ...line(1), gap: { after: '2026-10-18T13:51:00Z', before: '2026-10-18T13:51:01Z' } },
    ...Array.from({ length: 10 }, (_, i) => line(i + 2)),
  ],
  endedBecause: 'it reached its line limit',
  viewAround,
};

const running: LogsWatchTask = { ...matched, status: 'Running', matched: [], endedBecause: '', viewAround: null };

const call = {
  id: 'call-1',
  name: 'LogsWatch',
  status: 'Succeeded',
  runsOn: 'Sidecar',
  actionKind: 'LogsWatch',
  action: { description: 'Panics in webapp', logsWatch: action },
  approval: null,
  network: null,
  output: 'Watching',
  agentCallID: null,
  sandboxActions: [],
} as unknown as ChatToolCall;

function Focused() {
  const { view } = useLogsView();
  return (
    <output data-testid="focused">
      {view ? `${view.clusterId}/${view.chatId}/${view.callId}/${view.action.anchor.at}` : 'none'}
    </output>
  );
}

const draw = ({
  mode = 'chat',
  watch = action,
  task = matched,
  clusterID = 'cluster-1',
}: { mode?: AppMode; watch?: LogsWatchAction; task?: LogsWatchTask | null; clusterID?: string | null } = {}) =>
  render(
    <RightSidebarProvider mode={mode}>
      <LogsViewProvider>
        <LogWindowsContext.Provider value={logWindows}>
          <LogsWatchCard
            call={call}
            action={watch}
            task={task}
            chatID="chat-1"
            clusterID={clusterID ?? undefined}
            mode={mode}
          />
          <Focused />
        </LogWindowsContext.Provider>
      </LogsViewProvider>
    </RightSidebarProvider>,
  );

const rows = () => screen.queryAllByTestId('logs-inline-row');

describe('LogsWatchCard', () => {
  it('names what it watches, for what, from when, its limits and its end, with the description', () => {
    const { container } = draw({ task: running });
    expect(container).toHaveTextContent('Watch: Deployment webapp in payments (app)');
    expect(container).toHaveTextContent(
      'watching for /panic|Exception/ from 2026-10-18T13:45:00Z, up to 100 lines, until 2026-10-18T14:00:00Z',
    );
    expect(container).toHaveTextContent('“Panics in webapp”');
  });

  it('says only what differs from a bare watch', () => {
    const { container } = draw({
      task: running,
      watch: { ...action, grep: '', since: action.resolvedAt, limit: 1 },
    });
    expect(container).toHaveTextContent('watching for any line until 2026-10-18T14:00:00Z');
    expect(container).not.toHaveTextContent('up to');
    const bytes = draw({ task: running, watch: { ...action, maxBytes: 4096 } });
    expect(bytes.container).toHaveTextContent('up to 100 lines or 4096 bytes');
  });

  it('draws a running watch as watching, with no lines, and a call with no task yet as nothing', () => {
    const { container, unmount } = draw({ task: running });
    expect(container).toHaveTextContent('watching…');
    expect(rows()).toHaveLength(0);
    unmount();
    const none = draw({ task: null });
    expect(none.container).not.toHaveTextContent(/watching…|lost|stopped|ended/);
  });

  it('draws a matched watch’s first eight rows from the task alone, holes included, and requests nothing', () => {
    const { container } = draw();
    expect(rows()).toHaveLength(8);
    expect(screen.getByText(/lines may be missing/i)).toBeInTheDocument();
    expect(container).toHaveTextContent('ended because it reached its line limit');
    expect(requested).toEqual([]);
  });

  it('shows every line on Show all', () => {
    draw();
    fireEvent.click(screen.getByRole('button', { name: 'Show all 12 lines' }));
    expect(rows()).toHaveLength(12);
  });

  it('ends a cut line with what the server cut, and tags none', () => {
    draw({ task: { ...matched, matched: [{ ...line(0), truncatedBytes: 1536 }] } });
    expect(rows()[0]).toHaveTextContent('panic 0 … 1.5 KiB cut');
    expect(rows()[0]).not.toHaveClass('text-muted-foreground');
  });

  it('draws markup and reordering characters in a line as text', () => {
    const { container } = draw();
    expect(container.querySelector('b')).toBeNull();
    expect(container).toHaveTextContent('<b>bold</b>');
    expect(container.querySelector('mark')).not.toBeNull();
  });

  it('View around this focuses the task’s view around at this call', () => {
    draw();
    fireEvent.click(screen.getByRole('button', { name: 'View around this' }));
    expect(screen.getByTestId('focused')).toHaveTextContent('cluster-1/chat-1/call-1/2026-10-18T13:51:00Z');
    expect(screen.getByText('Showing in the sidebar')).toBeInTheDocument();
  });

  it('Open in window moves the task’s view around to a window', () => {
    draw();
    fireEvent.click(screen.getByRole('button', { name: 'Open in window' }));
    expect(logWindows.open).toHaveBeenCalledWith(
      expect.objectContaining({
        clusterId: 'cluster-1',
        chatId: 'chat-1',
        callId: 'call-1',
        action: matched.viewAround,
      }),
    );
  });

  it('holds its buttons until the chat’s cluster is known', () => {
    draw({ clusterID: null });
    expect(screen.getByRole('button', { name: 'View around this' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Open in window' })).toBeDisabled();
  });

  it('says the view is in a window while its call has one', () => {
    windows.push({ label: 'logs-1', clusterId: 'cluster-1', chatId: 'chat-1', callId: 'call-1', focusSeq: 1 });
    draw();
    expect(screen.getByText('Showing in a window')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'View around this' })).not.toBeInTheDocument();
  });

  it('has no View around this on the dashboard', () => {
    draw({ mode: 'dashboard' });
    expect(screen.queryByRole('button', { name: 'View around this' })).not.toBeInTheDocument();
  });

  it('says an expired watch matched nothing by its end', () => {
    const { container } = draw({
      task: { ...running, status: 'Expired', endedBecause: 'its time ran out' },
    });
    expect(container).toHaveTextContent('nothing matched by 2026-10-18T14:00:00Z');
    expect(screen.queryByRole('button', { name: 'View around this' })).not.toBeInTheDocument();
  });

  it('says why a failed watch ended', () => {
    const { container } = draw({
      task: { ...running, status: 'Failed', endedBecause: 'every container of this view has stopped' },
    });
    expect(container).toHaveTextContent('ended: every container of this view has stopped');
  });

  it('spells invisible and reordering characters in why it ended, which carries cluster text', () => {
    const failed = draw({ task: { ...running, status: 'Failed', endedBecause: 'denied \u202eyb' } });
    expect(failed.container.querySelector('mark')).toHaveTextContent('\\u{202E}');
    failed.unmount();
    const held = draw({ task: { ...matched, matched: [line(2)], endedBecause: 'denied \u202eyb' } });
    expect(held.container.querySelector('mark')).toHaveTextContent('\\u{202E}');
  });

  it('draws a stopped watch’s lines, and a lost one’s words', () => {
    const stopped = draw({ task: { ...matched, status: 'Stopped', endedBecause: 'the user stopped it' } });
    expect(rows()).toHaveLength(8);
    expect(stopped.container).toHaveTextContent('ended because the user stopped it');
    stopped.unmount();
    const lost = draw({ task: { ...running, status: 'Lost' } });
    expect(lost.container).toHaveTextContent('lost when Kubetail stopped');
  });

  it('says a stopped watch with no line was stopped', () => {
    const { container } = draw({ task: { ...running, status: 'Stopped' } });
    expect(container).toHaveTextContent('stopped');
    expect(rows()).toHaveLength(0);
  });

  // The transcript reads the action afresh from each frame of the call's row.
  it('keeps what it watches for when the same action arrives again', () => {
    const { container, rerender } = draw({ task: running });
    rerender(
      <RightSidebarProvider mode="chat">
        <LogsViewProvider>
          <LogWindowsContext.Provider value={logWindows}>
            <LogsWatchCard
              call={call}
              action={{ ...action }}
              task={running}
              chatID="chat-1"
              clusterID="cluster-1"
              mode="chat"
            />
          </LogWindowsContext.Provider>
        </LogsViewProvider>
      </RightSidebarProvider>,
    );
    expect(container).toHaveTextContent(
      'watching for /panic|Exception/ from 2026-10-18T13:45:00Z, up to 100 lines, until 2026-10-18T14:00:00Z',
    );
  });
});
