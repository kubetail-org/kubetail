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
import type { LogWindow } from '@/lib/log-windows';
import type { LogsView } from '@/lib/logs-view';

const { LogWindowsContext } = await import('@/lib/log-windows');
const { LogsViewProvider, useLogsView } = await import('@/lib/logs-view');
const { RightSidebarProvider, useRightSidebar } = await import('@/lib/right-sidebar');
const { LogsViewButtons, useViewPlace } = await import('./buttons');

const view: LogsView = {
  clusterId: 'cluster-1',
  chatId: 'chat-1',
  callId: 'call-1',
  action: {
    sources: [{ namespace: 'prod', kind: 'Pod', name: 'web-0', containers: [], allContainers: false, previous: false }],
    filters: [],
    grep: '',
    anchor: { kind: 'Tail', at: null },
    pinToEnd: true,
    resolvedAt: '2026-10-08T14:04:00Z',
  },
};

const open = vi.fn();
const focus = vi.fn();

function Card({ mode, clusterID }: { mode: AppMode; clusterID: string | undefined }) {
  const place = useViewPlace('chat-1', 'call-1', mode);
  const { view: focused, set } = useLogsView();
  const { close } = useRightSidebar();
  return (
    <>
      <output data-testid="place">{place}</output>
      <output data-testid="focused">{focused?.callId ?? 'none'}</output>
      {mode === 'chat' && (
        <LogsViewButtons
          label="Expand"
          place={place}
          chatID="chat-1"
          callID="call-1"
          clusterID={clusterID}
          action={view.action}
        />
      )}
      <button type="button" onClick={() => set(view)}>
        focus it
      </button>
      <button type="button" onClick={close}>
        collapse
      </button>
    </>
  );
}

const draw = ({
  mode = 'chat',
  windows = [],
  clusterID = 'cluster-1',
  ready = true,
}: { mode?: AppMode; windows?: LogWindow[]; clusterID?: string | null; ready?: boolean } = {}) =>
  render(
    <RightSidebarProvider mode={mode}>
      <LogsViewProvider>
        <LogWindowsContext.Provider
          value={{
            ready,
            windowOf: (chatId, callId) => windows.find((w) => w.chatId === chatId && w.callId === callId),
            open,
            focus,
            shownWindowOf: () => null,
          }}
        >
          <Card mode={mode} clusterID={clusterID ?? undefined} />
        </LogWindowsContext.Provider>
      </LogsViewProvider>
    </RightSidebarProvider>,
  );

const inWindow: LogWindow = {
  label: 'logs-1',
  clusterId: 'cluster-1',
  chatId: 'chat-1',
  callId: 'call-1',
  focusSeq: 1,
};

beforeEach(() => {
  open.mockReset();
  focus.mockReset();
  localStorage.clear();
});

describe('a view’s place', () => {
  it('is its card: the card’s button and Open in window, each acting on the view', () => {
    draw();
    expect(screen.getByTestId('place')).toHaveTextContent('card');
    fireEvent.click(screen.getByRole('button', { name: 'Open in window' }));
    expect(open).toHaveBeenCalledWith(view);
    fireEvent.click(screen.getByRole('button', { name: 'Expand' }));
    expect(screen.getByTestId('focused')).toHaveTextContent('call-1');
  });

  it('is the sidebar while the call is focused and the sidebar is open, with no buttons', () => {
    draw();
    fireEvent.click(screen.getByText('focus it'));
    expect(screen.getByTestId('place')).toHaveTextContent('sidebar');
    expect(screen.getByText('Showing in the sidebar')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Open in window' })).not.toBeInTheDocument();

    fireEvent.click(screen.getByText('collapse'));
    expect(screen.getByTestId('place')).toHaveTextContent('card');
  });

  it('is a window while its call has one, with Show window surfacing it', () => {
    draw({ windows: [inWindow] });
    expect(screen.getByTestId('place')).toHaveTextContent('window');
    expect(screen.getByText('Showing in a window')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Expand' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Show window' }));
    expect(focus).toHaveBeenCalledWith('call-1');
  });

  it('is its card on the dashboard, whatever holds the call, with no buttons', () => {
    draw({ mode: 'dashboard', windows: [inWindow] });
    expect(screen.getByTestId('place')).toHaveTextContent('card');
    expect(screen.queryByRole('button', { name: /Expand|Open in window|Show window/ })).not.toBeInTheDocument();
  });

  it('holds both buttons until the view is known', () => {
    draw({ clusterID: null });
    expect(screen.getByRole('button', { name: 'Expand' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Open in window' })).toBeDisabled();
  });

  it('holds the window buttons, and not Expand, until the provider is ready', () => {
    draw({ ready: false });
    expect(screen.getByRole('button', { name: 'Expand' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Open in window' })).toBeDisabled();

    draw({ ready: false, windows: [inWindow] });
    expect(screen.getByRole('button', { name: 'Show window' })).toBeDisabled();
  });
});
