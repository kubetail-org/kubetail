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
import { useRef } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { AppMode } from '@/lib/app-mode';
import { LogWindowsContext } from '@/lib/log-windows';
import { LogsViewProvider, useLogsView } from '@/lib/logs-view';
import type { LogsView } from '@/lib/logs-view';
import { ObjectViewProvider, useObjectView } from '@/lib/object-view';
import { RightSidebarProvider, useRightSidebar } from '@/lib/right-sidebar';
import { fakeLogWindows, stubResizeObserver } from '@/test-utils';

// The panel's contents have a suite of their own, and the real widgets want a router,
// a urql client, the Tauri bridge and a layout. This file is about which one is mounted.
// The focused object is bound to the active cluster.
vi.mock('@/lib/active-cluster', () => ({ useActiveCluster: () => ({ clusterID: 'c1' }) }));
vi.mock('@/components/chat/dashboard-chat', () => ({
  DashboardChat: () => <div data-testid="dashboard-chat" />,
}));
vi.mock('@/components/dashboard/object-panel/panel', () => ({
  ObjectPanel: ({
    object,
    tab,
    onTab,
    onClose,
  }: {
    object: { name: string };
    tab: string;
    onTab: (tab: string) => void;
    onClose: () => void;
  }) => (
    <div data-testid="object-panel" data-tab={tab}>
      <button type="button" onClick={() => onTab('yaml')}>
        to yaml
      </button>
      <button type="button" onClick={onClose}>
        close {object.name}
      </button>
    </div>
  ),
}));
vi.mock('@/components/logs/log-viewer', () => ({
  LogViewer: ({ query, onGrepApplied }: { query: { grep: string }; onGrepApplied: (grep: string) => void }) => (
    <div data-testid="log-viewer" data-grep={query.grep}>
      <button type="button" onClick={() => onGrepApplied('panic')}>
        apply panic
      </button>
    </div>
  ),
}));

const { RightSidebar, leftSidebarRoom } = await import('./right-sidebar');

// The app bar's toggle, as far as this suite needs it.
function RightSidebarToggle() {
  const { open, toggle } = useRightSidebar();
  return (
    <button type="button" aria-label="Toggle right sidebar" aria-pressed={open} onClick={toggle}>
      toggle
    </button>
  );
}

const { value: logWindows } = fakeLogWindows();

// Helpers -------------------------------------------------------------

// The content row as the layout draws it: the page, then the panel.
function Row() {
  const page = useRef<HTMLElement>(null);
  return (
    <div data-testid="row">
      <main ref={page} />
      <RightSidebar page={page} />
    </div>
  );
}

// The toggle and the panel sit in different branches of the layout, so a test
// renders both under the state they share.
const renderPanel = ({ mode = 'chat' }: { mode?: AppMode } = {}) =>
  render(
    <RightSidebarProvider mode={mode}>
      <ObjectViewProvider>
        <LogsViewProvider>
          <LogWindowsContext.Provider value={logWindows}>
            <RightSidebarToggle />
            <Row />
            <FocusView />
            <FocusObject />
          </LogWindowsContext.Provider>
        </LogsViewProvider>
      </ObjectViewProvider>
    </RightSidebarProvider>,
  );

// jsdom lays nothing out, so a drag's bound needs the geometry the layout would give: the
// row spans the window, the page starts `pageLeft` px in (past the left card), and the
// panel ends at the window's right edge.
function layOut({ pageLeft = 0 }: { pageLeft?: number } = {}) {
  const row = screen.getByTestId('row');
  Object.defineProperty(row, 'clientWidth', { value: window.innerWidth - pageLeft, configurable: true });
  vi.spyOn(row.querySelector('main')!, 'getBoundingClientRect').mockReturnValue(new DOMRect(pageLeft, 0, 0, 0));
  // The panel at the window's right edge, as wide as its style says.
  const width = Number.parseFloat(panel()!.style.width);
  vi.spyOn(panel()!, 'getBoundingClientRect').mockReturnValue(new DOMRect(window.innerWidth - width, 0, width, 0));
}

const logsView: LogsView = {
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

// Stands in for whatever sets the focused view: the card's Expand, or the newest call.
function FocusView() {
  const { set } = useLogsView();
  return (
    <button type="button" onClick={() => set(logsView)}>
      focus view
    </button>
  );
}

function FocusObject() {
  const { set } = useObjectView();
  return (
    <button
      type="button"
      onClick={() =>
        set({ apiVersion: 'v1', resource: 'pods', kind: 'Pod', uid: 'u1', namespace: 'shop', name: 'web-1' }, 'events')
      }
    >
      focus object
    </button>
  );
}

stubResizeObserver();

const panel = () => screen.queryByTestId('right-sidebar');
const panelWidth = () => screen.getByTestId('right-sidebar').style.width;
const handle = () => screen.getByRole('separator', { name: 'Resize right sidebar' });

// jsdom doesn't implement pointer capture; the handle calls it, so stub it out.
if (!HTMLElement.prototype.setPointerCapture) {
  HTMLElement.prototype.setPointerCapture = () => {};
}

beforeEach(() => {
  localStorage.clear();
  // The drag measures from the window's right edge, so tests need a known one.
  Object.defineProperty(window, 'innerWidth', { value: 1000, configurable: true });
});
const clickToggle = () => fireEvent.click(screen.getByRole('button', { name: /toggle right sidebar/i }));

// A drag that a test leaves mid-flight keeps its window listeners and the forced
// cursor, which the next test would inherit.
afterEach(() => {
  fireEvent.pointerUp(window);
  vi.restoreAllMocks();
});

// Tests ---------------------------------------------------------------

describe('leftSidebarRoom', () => {
  it('leaves the page its floor, and the right sidebar its minimum while it is open', () => {
    expect(leftSidebarRoom('chat', false).px(800)).toBe(480);
    expect(leftSidebarRoom('chat', true).px(800)).toBe(240);
    // The floor is half a row under 640px.
    expect(leftSidebarRoom('dashboard', true).px(600)).toBe(60);
  });
});

describe('RightSidebar', () => {
  it('stays closed until it is asked for', () => {
    renderPanel();
    expect(panel()).not.toBeInTheDocument();
  });

  it('opens and closes from the toggle', () => {
    renderPanel();
    clickToggle();
    expect(panel()).toBeInTheDocument();

    clickToggle();
    expect(panel()).not.toBeInTheDocument();
  });

  it('says whether it is open, for anything reading the button', () => {
    renderPanel();
    const toggle = screen.getByRole('button', { name: /toggle right sidebar/i });
    expect(toggle).toHaveAttribute('aria-pressed', 'false');

    clickToggle();
    expect(toggle).toHaveAttribute('aria-pressed', 'true');
  });

  it('sits in the row as an ordinary flex item, flush to the window', () => {
    renderPanel();
    clickToggle();
    // Not floating: it takes width from the page and the row's height makes it
    // flush with the bar above and the window's bottom edge.
    expect(panel()).not.toHaveClass('absolute', 'fixed', 'rounded-lg');
  });

  it('resizes by dragging its inner edge, and persists the width', () => {
    renderPanel();
    clickToggle();
    layOut();
    expect(panelWidth()).toBe('400px');

    // The panel ends at the window's right edge, so its width is what is left of
    // the window from the pointer.
    fireEvent.pointerDown(handle());
    fireEvent.pointerMove(window, { clientX: 650 });
    expect(panelWidth()).toBe('350px');
    expect(localStorage.getItem('kubetail:right-sidebar-width:chat')).toBe('350');

    fireEvent.pointerUp(window);
    // Releasing detaches the listeners: further movement is ignored.
    fireEvent.pointerMove(window, { clientX: 700 });
    expect(panelWidth()).toBe('350px');
  });

  // Shrunk by the row to 300px, a stored 400px moves on the first press; it grows no
  // further than the page's floor allows, the bound it reports.
  it('steps its width from the keyboard, from its width as drawn, the left arrow growing it', () => {
    renderPanel();
    clickToggle();
    layOut();
    const drawn = vi.spyOn(panel()!, 'getBoundingClientRect').mockReturnValue(new DOMRect(700, 0, 300, 0));
    act(() => handle().focus());
    expect(handle()).toHaveAttribute('aria-valuenow', '300');
    expect(handle()).toHaveAttribute('aria-valuemin', '240');
    // Right of the page's left edge (0) to the panel's right (1000), less the 320px floor.
    expect(handle()).toHaveAttribute('aria-valuemax', '680');
    fireEvent.keyDown(handle(), { key: 'ArrowRight' });
    expect(panelWidth()).toBe('284px');
    drawn.mockReturnValue(new DOMRect(330, 0, 670, 0));
    fireEvent.keyDown(handle(), { key: 'ArrowLeft' });
    expect(panelWidth()).toBe('680px');
  });

  // Shrunk by the row to the room it has, a stored 400px is the user's for a wider window.
  it('keeps the stored width on a press the room holds back', () => {
    renderPanel();
    clickToggle();
    // A 600px row: room for 1000 − 400 − the 300px floor.
    layOut({ pageLeft: 400 });
    vi.spyOn(panel()!, 'getBoundingClientRect').mockReturnValue(new DOMRect(700, 0, 300, 0));
    fireEvent.keyDown(handle(), { key: 'ArrowLeft' });
    expect(localStorage.getItem('kubetail:right-sidebar-width:chat')).toBeNull();
    expect(panelWidth()).toBe('400px');
  });

  it('clamps the dragged width at 240px', () => {
    renderPanel();
    clickToggle();
    layOut();

    fireEvent.pointerDown(handle());
    fireEvent.pointerMove(window, { clientX: 990 });
    expect(panelWidth()).toBe('240px');
  });

  it.each<AppMode>(['chat', 'dashboard'])('in %s mode, grows until the page reaches its 320px floor', (mode) => {
    renderPanel({ mode });
    clickToggle();
    layOut({ pageLeft: 200 });

    // Past the left card, the row is 800px: the page keeps 320px of it.
    fireEvent.pointerDown(handle());
    fireEvent.pointerMove(window, { clientX: -500 });
    expect(panelWidth()).toBe('480px');
    expect(localStorage.getItem(`kubetail:right-sidebar-width:${mode}`)).toBe('480');
  });

  it('leaves the page half the row once the row is under 640px', () => {
    Object.defineProperty(window, 'innerWidth', { value: 600, configurable: true });
    localStorage.setItem('kubetail:right-sidebar-width:chat', '250');
    renderPanel();
    clickToggle();
    layOut();

    fireEvent.pointerDown(handle());
    fireEvent.pointerMove(window, { clientX: 0 });
    expect(panelWidth()).toBe('300px');
  });

  // Shrunk by the row to the room it has, a stored 400px is the user's for a wider window.
  it('keeps the stored width on a drag outward the room holds back', () => {
    Object.defineProperty(window, 'innerWidth', { value: 600, configurable: true });
    renderPanel();
    clickToggle();
    layOut();
    vi.spyOn(panel()!, 'getBoundingClientRect').mockReturnValue(new DOMRect(300, 0, 300, 0));

    fireEvent.pointerDown(handle());
    fireEvent.pointerMove(window, { clientX: 0 });
    expect(panelWidth()).toBe('400px');
    expect(localStorage.getItem('kubetail:right-sidebar-width:chat')).toBeNull();
  });

  it('gives way to the page on a narrow window, down to its minimum', () => {
    renderPanel();
    clickToggle();
    expect(screen.getByTestId('right-sidebar')).not.toHaveClass('shrink-0');
    expect(screen.getByTestId('right-sidebar')).toHaveStyle({ minWidth: '240px' });
  });

  it('restores the last-saved width on mount (new windows inherit it)', () => {
    localStorage.setItem('kubetail:right-sidebar-width:chat', '500');
    renderPanel();
    clickToggle();
    expect(panelWidth()).toBe('500px');
  });

  it('keeps a stored width wider than the window, for the row to shrink to fit', () => {
    localStorage.setItem('kubetail:right-sidebar-width:dashboard', '1600');
    renderPanel({ mode: 'dashboard' });
    clickToggle();
    expect(panelWidth()).toBe('1600px');
  });

  it('keeps its width per mode, since the two hold different things', () => {
    localStorage.setItem('kubetail:right-sidebar-width:chat', '300');
    localStorage.setItem('kubetail:right-sidebar-width:dashboard', '500');

    const chat = renderPanel();
    clickToggle();
    expect(panelWidth()).toBe('300px');
    chat.unmount();

    renderPanel({ mode: 'dashboard' });
    clickToggle();
    expect(panelWidth()).toBe('500px');
  });

  it('shows the chat’s panel in chat mode', () => {
    renderPanel();
    clickToggle();
    expect(screen.getByRole('complementary', { name: 'Chat details' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Conversation' })).toBeInTheDocument();
  });

  it('draws the focused log view in chat mode, and its close collapses the sidebar around it', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'focus view' }));
    // Setting the view opens the panel by itself.
    expect(screen.getByRole('heading', { name: /Logs: Deployment webapp in prod/ })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Conversation' })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Close log view' }));
    expect(panel()).not.toBeInTheDocument();
    // The view stays focused, so the toggle brings it back.
    clickToggle();
    expect(screen.getByRole('heading', { name: /Logs: Deployment webapp in prod/ })).toBeInTheDocument();
  });

  it('opens the focused log view in a window from its header', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'focus view' }));
    fireEvent.click(screen.getByRole('button', { name: 'Open in window' }));
    expect(logWindows.open).toHaveBeenCalledWith(expect.objectContaining({ chatId: 'chat-1', callId: 'call-1' }));
  });

  it('draws no Open in window until the log windows are ready', () => {
    logWindows.ready = false;
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'focus view' }));
    expect(screen.queryByRole('button', { name: 'Open in window' })).not.toBeInTheDocument();
    logWindows.ready = true;
  });

  it('keeps the user’s grep for the focused view across a collapse', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'focus view' }));
    fireEvent.click(screen.getByRole('button', { name: 'apply panic' }));
    expect(screen.getByText(/matching/)).toHaveTextContent('matching /panic/');

    clickToggle();
    clickToggle();
    expect(screen.getByText(/matching/)).toHaveTextContent('matching /panic/');
    // The panel unmounted with the sidebar; the viewer it mounts again runs the held grep.
    expect(screen.getByTestId('log-viewer')).toHaveAttribute('data-grep', 'panic');

    fireEvent.click(screen.getByRole('button', { name: 'Close log view' }));
    clickToggle();
    expect(screen.getByTestId('log-viewer')).toHaveAttribute('data-grep', 'panic');
  });

  it('holds a chat in dashboard mode', () => {
    renderPanel({ mode: 'dashboard' });
    clickToggle();
    expect(screen.getByRole('complementary', { name: 'Dashboard details' })).toBeInTheDocument();
    expect(screen.getByTestId('dashboard-chat')).toBeInTheDocument();
  });

  it('opens on a focused object’s tab in dashboard mode, and its close brings the chat back', () => {
    renderPanel({ mode: 'dashboard' });
    fireEvent.click(screen.getByRole('button', { name: 'focus object' }));
    expect(screen.getByTestId('object-panel')).toHaveAttribute('data-tab', 'events');
    expect(screen.queryByTestId('dashboard-chat')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'to yaml' }));
    expect(screen.getByTestId('object-panel')).toHaveAttribute('data-tab', 'yaml');

    fireEvent.click(screen.getByRole('button', { name: 'close web-1' }));
    expect(panel()).toBeInTheDocument();
    expect(screen.getByTestId('dashboard-chat')).toBeInTheDocument();
  });

  // The scroller is inside the panel now, and a scroller inside a scroller gives two
  // scrollbars that fight.
  it('does not scroll itself', () => {
    renderPanel({ mode: 'dashboard' });
    clickToggle();
    expect(panel()).not.toHaveClass('overflow-y-auto');
  });
});
