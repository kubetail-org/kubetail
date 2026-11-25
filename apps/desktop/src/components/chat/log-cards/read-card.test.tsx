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
import type { LogsReadAction } from '@/lib/logs-view';
import { fakeLogWindows } from '@/test-utils';

// The card makes no client: any request it made would land here.
const { requested } = vi.hoisted(() => ({ requested: [] as unknown[] }));
vi.mock('urql', () => ({ useClient: () => requested.push('client') }));
vi.mock('@/lib/logs-client', () => ({ createLogsClient: () => requested.push('logs client') }));

const { LogsViewProvider, useLogsView } = await import('@/lib/logs-view');
const { LogWindowsContext } = await import('@/lib/log-windows');
const { windows, value: logWindows } = fakeLogWindows();
const { RightSidebarProvider } = await import('@/lib/right-sidebar');
const { LogsReadCard } = await import('./read-card');

beforeEach(() => {
  windows.length = 0;
  logWindows.open.mockReset();
  logWindows.focus.mockReset();
  requested.length = 0;
  localStorage.clear();
});

const line = (i: number, message = `line ${i}`): LogsReadAction['lines'][number] => ({
  timestamp: `2026-10-17T09:14:${String(i).padStart(2, '0')}Z`,
  namespace: 'prod',
  pod: 'webapp-a',
  container: 'app',
  previous: false,
  message,
  truncatedBytes: 0,
  gap: null,
});

const viewAround = {
  sources: [
    {
      namespace: 'prod',
      kind: 'Deployment',
      name: 'webapp',
      containers: ['app'],
      allContainers: false,
      previous: false,
    },
  ],
  filters: [],
  grep: 'error',
  anchor: { kind: 'At', at: '2026-10-17T09:14:00Z' },
  pinToEnd: false,
  resolvedAt: '2026-10-17T10:00:00Z',
} as LogsReadAction['viewAround'];

const action: LogsReadAction = {
  sources: viewAround!.sources,
  filters: [],
  grep: 'error',
  since: '2026-10-17T09:13:00Z',
  until: null,
  before: { lines: 0, span: '' },
  after: { lines: 0, span: '' },
  pageFrom: 'First',
  resolvedAt: '2026-10-17T10:00:00Z',
  lines: [
    line(0, '<b>bold</b> ‮reversed'),
    { ...line(1), gap: { after: '2026-10-17T09:14:00Z', before: '2026-10-17T09:14:01Z' } },
    ...Array.from({ length: 10 }, (_, i) => line(i + 2)),
  ],
  olderMore: true,
  newerMore: true,
  notices: [{ code: 'stream-skipped', message: 'webapp-b/app: forbidden' }],
  viewAround,
};

const call = {
  id: 'call-1',
  toolUseID: 'tu-1',
  name: 'LogsRead',
  contract: 'logsread/v1',
  arguments: {},
  status: 'Succeeded',
  runsOn: 'Sidecar',
  actionKind: 'LogsRead',
  action: { description: 'Why checkout failed', logsRead: action },
  approval: null,
  network: null,
  output: 'Read 12 lines',
  background: null,
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
  read = action,
  clusterID = 'cluster-1',
}: { mode?: AppMode; read?: LogsReadAction; clusterID?: string | null } = {}) =>
  render(
    <RightSidebarProvider mode={mode}>
      <LogsViewProvider>
        <LogWindowsContext.Provider value={logWindows}>
          <LogsReadCard call={call} action={read} chatID="chat-1" clusterID={clusterID ?? undefined} mode={mode} />
          <Focused />
        </LogWindowsContext.Provider>
      </LogsViewProvider>
    </RightSidebarProvider>,
  );

const rows = () => screen.getAllByTestId('logs-inline-row');

describe('LogsReadCard', () => {
  it('names what was read over which range, with the description', () => {
    const { container } = draw();
    expect(container).toHaveTextContent('Read: Deployment webapp in prod (app)');
    expect(container).toHaveTextContent('from 2026-10-17T09:13:00Z, oldest first, matching /error/');
    expect(container).toHaveTextContent('“Why checkout failed”');
  });

  it('draws the range’s two ends and a read of the newest', () => {
    const { container } = draw({ read: { ...action, since: null, until: '2026-10-17T09:15:00Z', pageFrom: 'Last' } });
    expect(container).toHaveTextContent('up to 2026-10-17T09:15:00Z, the newest');
  });

  it('draws the padding as the action holds it, and no end for a continuation', () => {
    const read = { ...action, before: { lines: 100, span: '' }, after: { lines: 0, span: 'PT5M' }, pageFrom: null };
    const { container } = draw({ read });
    expect(container).toHaveTextContent('from 2026-10-17T09:13:00Z, 100 lines before, PT5M after, matching /error/');
  });

  it('draws the first eight rows from the action alone, holes included, and requests nothing', () => {
    draw();
    expect(rows()).toHaveLength(8);
    expect(screen.getByText(/lines may be missing/i)).toBeInTheDocument();
    expect(requested).toEqual([]);
  });

  it('shows every line on Show all', () => {
    draw();
    fireEvent.click(screen.getByRole('button', { name: 'Show all 12 lines' }));
    expect(rows()).toHaveLength(12);
    expect(screen.queryByRole('button', { name: /Show all/ })).not.toBeInTheDocument();
  });

  it('draws markup and reordering characters in a message as text', () => {
    const { container } = draw();
    expect(container.querySelector('b')).toBeNull();
    expect(container).toHaveTextContent('<b>bold</b>');
    expect(container.querySelector('mark')).not.toBeNull();
  });

  it('words what the model saw beyond the lines: each side with more, then each notice', () => {
    draw();
    expect(screen.getByTestId('logs-read-beyond')).toHaveTextContent(
      'Older lines exist. Newer lines exist. webapp-b/app: forbidden',
    );
    expect(screen.getByText('webapp-b/app: forbidden').closest('[title]')).toHaveAttribute('title', 'stream-skipped');
  });

  it('says nothing of a side with no more, and nothing at all with neither and no notice', () => {
    const { container, unmount } = draw({ read: { ...action, newerMore: false, notices: [] } });
    expect(screen.getByTestId('logs-read-beyond')).toHaveTextContent(/^Older lines exist\.$/);
    expect(container).not.toHaveTextContent('Newer lines');
    unmount();

    draw({ read: { ...action, olderMore: false, newerMore: false, notices: [] } });
    expect(screen.queryByTestId('logs-read-beyond')).toBeNull();
  });

  it('ends a cut line with what the server cut', () => {
    draw({ read: { ...action, lines: [{ ...line(0), truncatedBytes: 4096 }] } });
    expect(rows()[0]).toHaveTextContent('line 0 … 4 KiB cut');
  });

  it('View around this focuses the read’s view around at this call', () => {
    draw();
    fireEvent.click(screen.getByRole('button', { name: 'View around this' }));
    expect(screen.getByTestId('focused')).toHaveTextContent('cluster-1/chat-1/call-1/2026-10-17T09:14:00Z');
    expect(screen.getByText('Showing in the sidebar')).toBeInTheDocument();
  });

  it('Open in window moves the read’s view around to a window', () => {
    draw();
    fireEvent.click(screen.getByRole('button', { name: 'Open in window' }));
    expect(logWindows.open).toHaveBeenCalledWith(
      expect.objectContaining({
        clusterId: 'cluster-1',
        chatId: 'chat-1',
        callId: 'call-1',
        action: action.viewAround,
      }),
    );
  });

  it('holds its buttons until the chat’s cluster is known', () => {
    draw({ clusterID: null });
    expect(screen.getByRole('button', { name: 'View around this' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Open in window' })).toBeDisabled();
  });

  it('says the view is in a window while its call has one, and keeps the lines it read', () => {
    windows.push({ label: 'logs-1', clusterId: 'cluster-1', chatId: 'chat-1', callId: 'call-1', focusSeq: 1 });
    draw();
    expect(screen.getByText('Showing in a window')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'View around this' })).not.toBeInTheDocument();
    expect(rows().length).toBeGreaterThan(0);
  });

  it('has no View around this on the dashboard, or for a read of no line', () => {
    draw({ mode: 'dashboard' });
    expect(screen.queryByRole('button', { name: 'View around this' })).not.toBeInTheDocument();
  });

  it('has no View around this for a read of no line, and says there were none', () => {
    const { container } = draw({ read: { ...action, lines: [], viewAround: null } });
    expect(screen.queryByRole('button', { name: 'View around this' })).not.toBeInTheDocument();
    expect(container).toHaveTextContent('No lines');
  });
});

describe('LogsReadCard tags', () => {
  const tagOf = (row: HTMLElement) => {
    const first = row.firstElementChild?.textContent ?? '';
    return first === 'before' || first === 'after' ? first : null;
  };
  const tagged = (read: LogsReadAction) => {
    draw({ read });
    return rows().map(tagOf);
  };
  const at = (stamp: string) => ({ ...line(0), timestamp: stamp });

  it('tags a line stamped before `since` as before', () => {
    const read = {
      ...action,
      before: { lines: 2, span: '' },
      lines: [at('2026-10-17T09:12:59Z'), at('2026-10-17T09:13:00Z')],
    };
    expect(tagged(read)).toEqual(['before', null]);
  });

  it('tags a line stamped after `until` as after', () => {
    const read = {
      ...action,
      until: '2026-10-17T09:15:00Z',
      after: { lines: 0, span: 'PT1M' },
      lines: [at('2026-10-17T09:15:00Z'), at('2026-10-17T09:15:00.000000001Z')],
    };
    expect(tagged(read)).toEqual([null, 'after']);
  });

  it('with `until` open and padding after, tags a line at or past `resolvedAt` as after', () => {
    const read = {
      ...action,
      after: { lines: 5, span: '' },
      lines: [at('2026-10-17T09:59:59.999999999Z'), at('2026-10-17T10:00:00Z')],
    };
    expect(tagged(read)).toEqual([null, 'after']);
  });

  it('with `until` open and no padding after, tags nothing after', () => {
    const read = { ...action, lines: [at('2026-10-17T10:00:00Z'), at('2026-10-17T11:00:00Z')] };
    expect(tagged(read)).toEqual([null, null]);
  });

  it('compares stamps as instants, whatever their fraction digits', () => {
    const read = { ...action, lines: [at('2026-10-17T09:13:00.000Z'), at('2026-10-17T09:12:59.999999999Z')] };
    expect(tagged(read)).toEqual([null, 'before']);
  });
});
