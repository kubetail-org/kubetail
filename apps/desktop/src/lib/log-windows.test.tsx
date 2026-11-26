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
import { act, fireEvent, render, screen } from '@testing-library/react';
import { Profiler, StrictMode, useEffect, useSyncExternalStore } from 'react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { createLogViewportHandle, EMPTY_LOG_VIEWPORT } from '@/lib/log-viewport';
import type { LogViewportHandle } from '@/lib/log-viewport';
import type { ViewportPush } from '@/lib/log-window-push';
import type { LogsView } from '@/lib/logs-view';
import { mockTauriCore } from '@/test-utils';

const { invokeMock, channels, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { navigateMock, active, listeners } = vi.hoisted(() => ({
  navigateMock: vi.fn(),
  active: { clusterID: undefined as string | undefined },
  listeners: new Set<() => void>(),
}));
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigateMock }));
vi.mock('@/lib/active-cluster', async () => {
  const react = await import('react');
  const subscribe = (l: () => void) => {
    listeners.add(l);
    return () => listeners.delete(l);
  };
  return { useActiveCluster: () => ({ clusterID: react.useSyncExternalStore(subscribe, () => active.clusterID) }) };
});

const { onError } = await import('@/lib/error-bus');
const { LogsViewProvider, useLogsView } = await import('@/lib/logs-view');
const { RightSidebarProvider, useRightSidebar } = await import('@/lib/right-sidebar');
const { LogWindowsProvider, useLogWindows } = await import('./log-windows');

// Helpers -------------------------------------------------------------

type Deferred<T> = { promise: Promise<T>; resolve: (v: T) => void; reject: (e: unknown) => void };
function deferred<T>(): Deferred<T> {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const flush = () => act(async () => {});

const viewOn = (clusterId: string, callId = 'call-1', grep?: string): LogsView => ({
  clusterId,
  chatId: 'chat-1',
  callId,
  action: {
    sources: [{ namespace: 'prod', kind: 'Pod', name: 'web-0', containers: [], allContainers: false, previous: false }],
    filters: [],
    grep: '',
    anchor: { kind: 'Tail', at: null },
    pinToEnd: true,
    resolvedAt: '2026-10-08T14:04:00Z',
  },
  ...(grep !== undefined && { grep }),
});

const windowOn = (
  label: string,
  clusterId: string,
  callId = 'call-1',
  { focusSeq = 1, push = null as ViewportPush | null, chatId = 'chat-1' } = {},
) => ({ label, clusterId, chatId, callId, focusSeq, push });

// A push from page load `generation` with `count` lines selected, under `revision`.
const pushOf = (count: number, { generation = 1, revision = 1, visible = 1 } = {}): ViewportPush => {
  const selection = Array.from({ length: count }, (_, i) => ({
    timestamp: '2026-10-08T14:04:00Z',
    stream: { namespace: 'prod', pod: 'web-0', container: 'app', previous: false },
    message: `line ${i}`,
  }));
  return {
    generation,
    grep: null,
    viewport: { ...EMPTY_LOG_VIEWPORT, visible, selection, selectedCount: count, selectionRevision: revision },
  };
};

function setCluster(id: string | undefined) {
  act(() => {
    active.clusterID = id;
    listeners.forEach((l) => l());
  });
}

// Sends a frame down the channel the last subscribe was handed, the one the host keeps.
function frame(f: unknown) {
  act(() => {
    const [{ channel }] = sent('log_window_subscribe').slice(-1) as { channel: { onmessage: (f: unknown) => void } }[];
    channel.onmessage(f);
  });
}

const sent = (cmd: string) => invokeMock.mock.calls.filter(([c]) => c === cmd).map(([, arg]) => arg);

// Reads and drives the provider, the focused view and the sidebar.
function Probe({ open }: { open?: LogsView }) {
  const { ready, windowOf, open: openWindow, focus } = useLogWindows();
  const { view, set, setGrep } = useLogsView();
  const { open: sidebarOpen, close } = useRightSidebar();
  return (
    <>
      <output data-testid="ready">{ready ? 'ready' : 'not ready'}</output>
      <output data-testid="window">{windowOf('chat-1', 'call-1')?.label ?? 'none'}</output>
      <output data-testid="focused">{view ? `${view.callId}:${view.grep ?? ''}` : 'none'}</output>
      <output data-testid="sidebar">{sidebarOpen ? 'open' : 'closed'}</output>
      <button type="button" onClick={() => openWindow(open ?? viewOn('c-a'))}>
        open window
      </button>
      <button type="button" onClick={() => focus('call-1')}>
        focus
      </button>
      <button type="button" onClick={() => set(viewOn('c-a', 'call-1'))}>
        focus call-1
      </button>
      <button type="button" onClick={() => set(viewOn('c-a', 'call-2'))}>
        focus call-2
      </button>
      <button type="button" onClick={() => setGrep('panic')}>
        grep panic
      </button>
      <button type="button" onClick={close}>
        collapse
      </button>
    </>
  );
}

function wrap(children: ReactNode) {
  return (
    <RightSidebarProvider mode="chat">
      <LogsViewProvider>
        <LogWindowsProvider>{children}</LogWindowsProvider>
      </LogsViewProvider>
    </RightSidebarProvider>
  );
}

// Answers each subscribe with the next page, and every other command at once.
function answerPages() {
  let page = 0;
  invokeMock.mockImplementation((cmd: string) => {
    if (cmd === 'log_window_subscribe') {
      page += 1;
      return Promise.resolve({ page });
    }
    return Promise.resolve();
  });
}

// What the provider reported, off the real bus.
const reported = vi.fn();
let stopListening = () => {};

beforeEach(() => {
  invokeMock.mockReset();
  navigateMock.mockReset();
  reported.mockReset();
  stopListening = onError(reported);
  channels.length = 0;
  localStorage.clear();
  active.clusterID = 'c-a';
  answerPages();
});

afterEach(() => {
  stopListening();
});

// Tests ---------------------------------------------------------------

describe('LogWindowsProvider: the subscription', () => {
  it('subscribes with a channel, and sends nothing before the page answers', async () => {
    invokeMock.mockImplementation(() => new Promise(() => {}));
    render(wrap(<Probe />));
    expect(sent('log_window_subscribe')).toEqual([{ channel: channels[0] }]);
    expect(screen.getByTestId('ready')).toHaveTextContent('not ready');
    fireEvent.click(screen.getByText('open window'));
    fireEvent.click(screen.getByText('focus'));
    await flush();
    expect(invokeMock).toHaveBeenCalledTimes(1);
  });

  it('is ready once the page has answered and the cluster is known', async () => {
    active.clusterID = undefined;
    render(wrap(<Probe />));
    await flush();
    expect(screen.getByTestId('ready')).toHaveTextContent('not ready');
    setCluster('c-a');
    expect(screen.getByTestId('ready')).toHaveTextContent('ready');
    setCluster(undefined);
    expect(screen.getByTestId('ready')).toHaveTextContent('not ready');
  });

  it('folds the frames in order, a closed for a window it lacks changing nothing', async () => {
    render(wrap(<Probe />));
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a')] } });
    expect(screen.getByTestId('window')).toHaveTextContent('logs-1');
    frame({ closed: { label: 'logs-1' } });
    expect(screen.getByTestId('window')).toHaveTextContent('none');
    frame({ closed: { label: 'logs-9' } });
    frame({ opened: { window: windowOn('logs-2', 'c-a') } });
    expect(screen.getByTestId('window')).toHaveTextContent('logs-2');
    frame({ snapshot: { windows: [] } });
    expect(screen.getByTestId('window')).toHaveTextContent('none');
  });

  it("folds a frame on either double-mount subscribe's channel, whichever the host kept", async () => {
    render(<StrictMode>{wrap(<Probe />)}</StrictMode>);
    await flush();
    const [first, second] = (sent('log_window_subscribe') as { channel: { onmessage: (f: unknown) => void } }[]).map(
      (s) => s.channel,
    );
    act(() => first.onmessage({ snapshot: { windows: [windowOn('logs-1', 'c-a')] } }));
    expect(screen.getByTestId('window')).toHaveTextContent('logs-1');
    act(() => second.onmessage({ closed: { label: 'logs-1' } }));
    expect(screen.getByTestId('window')).toHaveTextContent('none');
  });

  it('ignores a frame it does not know', async () => {
    render(wrap(<Probe />));
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a')] } });
    frame({ resized: { label: 'logs-1' } });
    expect(screen.getByTestId('window')).toHaveTextContent('logs-1');
    expect(navigateMock).not.toHaveBeenCalled();
  });

  it('navigates to the chat a showChat frame names', async () => {
    render(wrap(<Probe />));
    await flush();
    frame({ showChat: { chatId: 'chat-7' } });
    expect(navigateMock).toHaveBeenCalledWith({ to: '/chat/$chatId', params: { chatId: 'chat-7' } });
  });

  it.each([
    ['in order', [0, 1]],
    ['out of order', [1, 0]],
  ])('keeps the larger page after a double mount whose answers land %s', async (_, order) => {
    const answers = [deferred<{ page: number }>(), deferred<{ page: number }>()];
    const pending = [...answers];
    invokeMock.mockImplementation((cmd: string) =>
      cmd === 'log_window_subscribe' ? pending.shift()!.promise : Promise.resolve(),
    );
    render(<StrictMode>{wrap(<Probe />)}</StrictMode>);
    // Tauri ends a channel when the host drops the side a subscribe handed it, and the
    // second subscribe drops the first's, so each must hand the host a channel of its own.
    const subscribes = sent('log_window_subscribe') as { channel: unknown }[];
    expect(subscribes).toHaveLength(2);
    expect(subscribes[0].channel).not.toBe(subscribes[1].channel);

    const [early, late] = order;
    answers[early].resolve({ page: early + 1 });
    await flush();
    answers[late].resolve({ page: late + 1 });
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a')] } });
    expect(screen.getByTestId('window')).toHaveTextContent('logs-1');
    fireEvent.click(screen.getByText('focus'));
    expect(sent('log_window_focus')).toEqual([{ page: 2, callId: 'call-1' }]);
  });
});

// Reads the window `shownWindowOf` picks for a chat, and its mirror, as the composer
// does. `held` keeps each mirror it was handed, and `renders` counts its commits.
const held: LogViewportHandle[] = [];
const none = createLogViewportHandle();
let renders = 0;
const counted = () => {
  renders += 1;
};
function ShownReader({ chatId }: { chatId: string }) {
  const shown = useLogWindows().shownWindowOf(chatId);
  useEffect(() => {
    if (shown && held.at(-1) !== shown.viewport) held.push(shown.viewport);
  });
  const viewport = shown?.viewport ?? none;
  const count = useSyncExternalStore(viewport.subscribe, () => viewport.get().selectedCount);
  return <output data-testid="shown">{shown ? `${shown.window.label}:${count}` : 'none'}</output>;
}
function Shown({ chatId = 'chat-1' }: { chatId?: string }) {
  return (
    <Profiler id="shown" onRender={counted}>
      <ShownReader chatId={chatId} />
    </Profiler>
  );
}

describe('LogWindowsProvider: the mirrors', () => {
  beforeEach(() => {
    held.length = 0;
    renders = 0;
  });

  it("seeds a mirror from a snapshot's and an opened window's push, and drops it on closed", async () => {
    render(wrap(<Shown />));
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a', 'call-1', { push: pushOf(1) })] } });
    expect(screen.getByTestId('shown')).toHaveTextContent('logs-1:1');
    frame({ opened: { window: windowOn('logs-2', 'c-a', 'call-2', { focusSeq: 2, push: pushOf(2) }) } });
    expect(screen.getByTestId('shown')).toHaveTextContent('logs-2:2');
    frame({ closed: { label: 'logs-2' } });
    expect(screen.getByTestId('shown')).toHaveTextContent('logs-1:1');
  });

  it('folds a later snapshot into the mirror a reader holds, and drops one a snapshot lacks', async () => {
    render(wrap(<Shown />));
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a')] } });
    expect(screen.getByTestId('shown')).toHaveTextContent('logs-1:0');
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a', 'call-1', { push: pushOf(3) })] } });
    expect(screen.getByTestId('shown')).toHaveTextContent('logs-1:3');
    expect(held).toHaveLength(1);

    frame({ snapshot: { windows: [] } });
    expect(screen.getByTestId('shown')).toHaveTextContent('none');
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a')] } });
    expect(held).toHaveLength(2);
    expect(held[1].get().selectedCount).toBe(0);
  });

  it("folds a viewport frame into the mirror, re-rendering none of the provider's consumers", async () => {
    render(wrap(<Shown />));
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a')] } });
    const before = renders;
    frame({ viewport: { label: 'logs-1', push: pushOf(4) } });
    frame({ viewport: { label: 'logs-9', push: pushOf(4) } });
    expect(held[0].get().selectedCount).toBe(4);
    // The reader re-renders for the mirror it reads, and for nothing else.
    expect(renders).toBe(before + 1);

    frame({ viewport: { label: 'logs-1', push: null } });
    expect(held[0].get()).toBe(EMPTY_LOG_VIEWPORT);
  });

  it("keeps a mirror's selection while a push names the same one", async () => {
    render(wrap(<Shown />));
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a', 'call-1', { push: pushOf(2) })] } });
    const kept = held[0].get().selection;
    frame({ viewport: { label: 'logs-1', push: pushOf(2, { visible: 9 }) } });
    expect(held[0].get().selection).toBe(kept);
    frame({ viewport: { label: 'logs-1', push: pushOf(2, { generation: 2 }) } });
    expect(held[0].get().selection).not.toBe(kept);
  });

  it('answers none from a value held across a closed frame, whose mirror is gone', async () => {
    let latest: (chatId: string) => unknown = () => null;
    function Hold() {
      const { shownWindowOf } = useLogWindows();
      useEffect(() => {
        latest = shownWindowOf;
      });
      return null;
    }
    render(wrap(<Hold />));
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a')] } });
    const before = latest;
    expect(before('chat-1')).not.toBeNull();
    frame({ closed: { label: 'logs-1' } });
    expect(before('chat-1')).toBeNull();
  });

  it('picks the window focused last, as focused frames say, and none for a chat with none', async () => {
    render(
      wrap(
        <>
          <Shown />
          <Shown chatId="chat-2" />
        </>,
      ),
    );
    await flush();
    frame({
      snapshot: {
        windows: [
          windowOn('logs-1', 'c-a', 'call-1', { focusSeq: 1 }),
          windowOn('logs-2', 'c-a', 'call-2', { focusSeq: 2 }),
          windowOn('logs-3', 'c-a', 'call-3', { focusSeq: 9, chatId: 'chat-3' }),
        ],
      },
    });
    const [one, two] = screen.getAllByTestId('shown');
    expect(one).toHaveTextContent('logs-2');
    expect(two).toHaveTextContent('none');
    frame({ focused: { label: 'logs-1', focusSeq: 3 } });
    expect(one).toHaveTextContent('logs-1');
    frame({ focused: { label: 'logs-9', focusSeq: 4 } });
    expect(one).toHaveTextContent('logs-1');
  });
});

describe('useLogWindows', () => {
  it('throws outside its provider', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => render(<Probe />)).toThrow('useLogWindows must be used within a LogWindowsProvider');
    vi.mocked(console.error).mockRestore();
  });
});

describe('LogWindowsProvider: open', () => {
  const opens = () => sent('log_window_open');

  it('opens the call, then lets go of the focused view and closes the sidebar showing it', async () => {
    render(wrap(<Probe />));
    await flush();
    fireEvent.click(screen.getByText('focus call-1'));
    expect(screen.getByTestId('sidebar')).toHaveTextContent('open');

    fireEvent.click(screen.getByText('open window'));
    await flush();
    expect(opens()).toEqual([{ page: 1, clusterId: 'c-a', chatId: 'chat-1', callId: 'call-1', grep: null }]);
    expect(screen.getByTestId('focused')).toHaveTextContent('none');
    expect(screen.getByTestId('sidebar')).toHaveTextContent('closed');
  });

  it("carries a collapsed sidebar's grep for the call, and lets go of its view", async () => {
    render(wrap(<Probe />));
    await flush();
    fireEvent.click(screen.getByText('focus call-1'));
    fireEvent.click(screen.getByText('grep panic'));
    fireEvent.click(screen.getByText('collapse'));

    fireEvent.click(screen.getByText('open window'));
    await flush();
    expect(opens()).toEqual([expect.objectContaining({ callId: 'call-1', grep: 'panic' })]);
    expect(screen.getByTestId('focused')).toHaveTextContent('none');
    expect(screen.getByTestId('sidebar')).toHaveTextContent('closed');
  });

  it('carries no grep for a call other than the focused one, and leaves that one focused', async () => {
    render(wrap(<Probe />));
    await flush();
    fireEvent.click(screen.getByText('focus call-2'));
    fireEvent.click(screen.getByText('grep panic'));

    fireEvent.click(screen.getByText('open window'));
    await flush();
    expect(opens()).toEqual([expect.objectContaining({ callId: 'call-1', grep: null })]);
    expect(screen.getByTestId('focused')).toHaveTextContent('call-2:panic');
    expect(screen.getByTestId('sidebar')).toHaveTextContent('open');
  });

  it('leaves a view focused on another call while the open builds', async () => {
    const built = deferred<string>();
    render(wrap(<Probe />));
    await flush();
    invokeMock.mockImplementation((cmd: string) => (cmd === 'log_window_open' ? built.promise : Promise.resolve()));
    fireEvent.click(screen.getByText('focus call-1'));
    fireEvent.click(screen.getByText('open window'));
    await flush();
    fireEvent.click(screen.getByText('focus call-2'));

    built.resolve('logs-1');
    await flush();
    expect(screen.getByTestId('focused')).toHaveTextContent('call-2:');
    expect(screen.getByTestId('sidebar')).toHaveTextContent('open');
  });

  it('leaves the focused view and the sidebar as they were after a failed open, and reports it', async () => {
    render(wrap(<Probe />));
    await flush();
    invokeMock.mockImplementation((cmd: string) =>
      cmd === 'log_window_open' ? Promise.reject(new Error('refused')) : Promise.resolve(),
    );
    fireEvent.click(screen.getByText('focus call-1'));
    fireEvent.click(screen.getByText('open window'));
    await flush();
    expect(screen.getByTestId('focused')).toHaveTextContent('call-1:');
    expect(screen.getByTestId('sidebar')).toHaveTextContent('open');
    expect(reported).toHaveBeenCalledWith(expect.objectContaining({ source: 'network', message: 'refused' }));
  });

  it('sends each open after the last settles', async () => {
    const first = deferred<string>();
    render(wrap(<Probe open={viewOn('c-a', 'call-1')} />));
    await flush();
    invokeMock.mockImplementationOnce(() => first.promise);
    fireEvent.click(screen.getByText('open window'));
    fireEvent.click(screen.getByText('open window'));
    await flush();
    expect(opens()).toHaveLength(1);

    first.resolve('logs-1');
    await flush();
    expect(opens()).toHaveLength(2);
  });

  it('sends nothing for a view on a cluster other than the held one', async () => {
    render(wrap(<Probe open={viewOn('c-b')} />));
    await flush();
    fireEvent.click(screen.getByText('open window'));
    await flush();
    expect(opens()).toEqual([]);
  });

  it('surfaces the window on a call, naming the page', async () => {
    render(wrap(<Probe />));
    await flush();
    fireEvent.click(screen.getByText('focus'));
    expect(sent('log_window_focus')).toEqual([{ page: 1, callId: 'call-1' }]);
  });
});

describe('LogWindowsProvider: the cluster', () => {
  const closes = () => sent('log_window_close_all');

  it('closes the windows when the cluster changes to another', async () => {
    render(wrap(<Probe />));
    await flush();
    setCluster('c-b');
    await flush();
    expect(closes()).toEqual([{ page: 1 }]);
  });

  it('closes none while the cluster is undefined, or when it comes back the same', async () => {
    render(wrap(<Probe />));
    await flush();
    setCluster(undefined);
    setCluster('c-a');
    await flush();
    expect(closes()).toEqual([]);
  });

  it('closes none for the first cluster after a subscribe, before or after a snapshot on it', async () => {
    active.clusterID = undefined;
    const { unmount } = render(wrap(<Probe />));
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a')] } });
    setCluster('c-a');
    await flush();
    unmount();

    render(wrap(<Probe />));
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a')] } });
    await flush();
    expect(closes()).toEqual([]);
  });

  it("closes a snapshot holding another cluster's window, before or after the cluster lands", async () => {
    active.clusterID = undefined;
    const { unmount } = render(wrap(<Probe />));
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a')] } });
    setCluster('c-b');
    await flush();
    expect(closes()).toEqual([{ page: 1 }]);
    unmount();

    active.clusterID = 'c-b';
    render(wrap(<Probe />));
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-2', 'c-a')] } });
    await flush();
    expect(closes()).toEqual([{ page: 1 }, { page: 2 }]);
  });

  it("checks the windows again on a new page, so a close the old page's end refused is sent on it", async () => {
    active.clusterID = 'c-b';
    const answers = [deferred<{ page: number }>(), deferred<{ page: number }>()];
    const pending = [...answers];
    invokeMock.mockImplementation((cmd: string) => {
      if (cmd === 'log_window_subscribe') return pending.shift()!.promise;
      if (cmd === 'log_window_close_all') return Promise.reject(new Error('page ended'));
      return Promise.resolve();
    });
    render(<StrictMode>{wrap(<Probe />)}</StrictMode>);
    answers[0].resolve({ page: 1 });
    await flush();
    frame({ snapshot: { windows: [windowOn('logs-1', 'c-a')] } });
    await flush();
    expect(closes()).toEqual([{ page: 1 }]);

    invokeMock.mockImplementation(() => Promise.resolve());
    answers[1].resolve({ page: 2 });
    await flush();
    expect(closes()).toEqual([{ page: 1 }, { page: 2 }]);
  });

  it("closes an old cluster's window that lands after the change, and leaves the new cluster's open alone", async () => {
    const built = deferred<string>();
    render(wrap(<Probe open={viewOn('c-a', 'call-1')} />));
    await flush();
    invokeMock.mockImplementation((cmd: string) => (cmd === 'log_window_open' ? built.promise : Promise.resolve()));
    fireEvent.click(screen.getByText('open window'));
    await flush();
    setCluster('c-b');
    await flush();
    expect(closes()).toEqual([]);

    built.resolve('logs-1');
    await flush();
    expect(invokeMock.mock.calls.map(([cmd]) => cmd).filter((c) => c !== 'log_window_subscribe')).toEqual([
      'log_window_open',
      'log_window_close_all',
    ]);
  });

  it('sends a close queued behind an open in flight only after it, and an open queued after it only after the close', async () => {
    const built = deferred<string>();
    const closed = deferred<void>();
    function Both() {
      const { open } = useLogWindows();
      return (
        <button type="button" onClick={() => open(viewOn('c-b', 'call-2'))}>
          open on b
        </button>
      );
    }
    render(
      wrap(
        <>
          <Probe />
          <Both />
        </>,
      ),
    );
    await flush();
    invokeMock.mockImplementation((cmd: string) => {
      if (cmd === 'log_window_open') return built.promise;
      if (cmd === 'log_window_close_all') return closed.promise;
      return Promise.resolve();
    });
    fireEvent.click(screen.getByText('open window'));
    await flush();
    setCluster('c-b');
    fireEvent.click(screen.getByText('open on b'));
    await flush();
    const order = () => invokeMock.mock.calls.map(([cmd]) => cmd).filter((c) => c !== 'log_window_subscribe');
    expect(order()).toEqual(['log_window_open']);

    built.resolve('logs-1');
    await flush();
    expect(order()).toEqual(['log_window_open', 'log_window_close_all']);

    closed.resolve();
    await flush();
    expect(order()).toEqual(['log_window_open', 'log_window_close_all', 'log_window_open']);
    expect(sent('log_window_open').at(-1)).toMatchObject({ clusterId: 'c-b', callId: 'call-2' });
  });

  it('reports a close that fails, and keeps the queue moving', async () => {
    render(wrap(<Probe open={viewOn('c-b')} />));
    await flush();
    invokeMock.mockImplementation((cmd: string) =>
      cmd === 'log_window_close_all' ? Promise.reject(new Error('gone')) : Promise.resolve(),
    );
    setCluster('c-b');
    await flush();
    expect(reported).toHaveBeenCalledWith(expect.objectContaining({ message: 'gone' }));
    fireEvent.click(screen.getByText('open window'));
    await flush();
    expect(sent('log_window_open')).toHaveLength(1);
  });

  it('drops an open still queued when the cluster changes under it', async () => {
    const built = deferred<string>();
    render(wrap(<Probe open={viewOn('c-a', 'call-2')} />));
    await flush();
    invokeMock.mockImplementationOnce(() => built.promise);
    fireEvent.click(screen.getByText('open window'));
    fireEvent.click(screen.getByText('open window'));
    await flush();
    setCluster('c-b');
    built.resolve('logs-1');
    await flush();
    expect(sent('log_window_open')).toHaveLength(1);
  });
});
