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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  MAC_USER_AGENT,
  NON_MAC_USER_AGENT,
  mockTauriCore,
  mockTauriWindow,
  restoreUserAgent,
  setUserAgent,
  stubResizeObserver,
} from '@/test-utils';

// Mocks ---------------------------------------------------------------

// The bar measures its two ends.
stubResizeObserver();

// `WindowControls` (rendered off macOS) drives the native window via
// `getCurrentWindow`, so the window API must be mocked before importing.
const { factory } = mockTauriWindow();
vi.mock('@tauri-apps/api/window', () => factory());

// The history nav needs a router; this suite renders the bar outside one, so stub
// it — its own suite covers the real one.
vi.mock('./history-nav', () => ({
  HistoryNav: () => <div data-testid="history-nav" />,
}));
// The notification button opens a popover and the settings button opens the
// dialog host; their own suites cover the real ones.
vi.mock('./notification-button', () => ({
  NotificationButton: () => <div data-testid="notification-button" />,
}));
vi.mock('./settings-button', () => ({
  SettingsButton: () => <div data-testid="settings-button" />,
}));
// The toggles read the sidebars' providers; their own suites cover them.
vi.mock('./left-sidebar-toggle', () => ({
  LeftSidebarToggle: () => <button type="button" aria-label="Toggle sidebar" data-testid="left-sidebar-toggle" />,
}));
vi.mock('./right-sidebar-toggle', () => ({
  RightSidebarToggle: () => (
    <button type="button" aria-label="Toggle right sidebar" data-testid="right-sidebar-toggle" />
  ),
}));
// The omnibox reads the kubeconfig; its own suite covers it.
vi.mock('@/components/shell/context-omnibox/omnibox', () => ({
  ContextOmnibox: () => <div data-testid="context-omnibox" />,
}));

// `AppMenu` (rendered off macOS) drives the host over `invoke`.
const { invokeMock, factory: coreFactory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => coreFactory());

const { AppBar } = await import('./bar');

// Helpers -------------------------------------------------------------

beforeEach(() => {
  invokeMock.mockReset();
  invokeMock.mockResolvedValue(undefined);
  setUserAgent(NON_MAC_USER_AGENT);
});

afterEach(() => {
  restoreUserAgent();
});

// Tests ---------------------------------------------------------------

describe('AppBar', () => {
  it('holds its height as the layout column’s top band', () => {
    render(<AppBar />);
    // 56px, and never shrunk by the row below it.
    expect(screen.getByTestId('app-bar')).toHaveClass('h-14', 'shrink-0');
  });

  it('drags the window from anywhere it is not a control, on every platform', () => {
    const { unmount } = render(<AppBar />);
    // `deep`, so the spacing and margins between the controls drag too; Tauri
    // stops at the first clickable element, which is what keeps buttons clicking.
    expect(screen.getByTestId('app-bar')).toHaveAttribute('data-tauri-drag-region', 'deep');
    unmount();

    setUserAgent(MAC_USER_AGENT);
    render(<AppBar />);
    expect(screen.getByTestId('app-bar')).toHaveAttribute('data-tauri-drag-region', 'deep');
  });

  it('clicks the pressed element when the press drags the window', () => {
    render(<AppBar />);
    const start = screen.getByTestId('app-bar-start');
    const onClick = vi.fn();
    start.addEventListener('click', onClick);
    fireEvent.pointerDown(start, { button: 0 });
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('centers the omnibox on the window, in a middle column that shrinks first, to a floor', () => {
    // The left end, with the traffic lights and the history, is the wider.
    const width = vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(function (
      this: HTMLElement,
    ) {
      return { 'app-bar-start': 260, 'app-bar-end': 112 }[this.dataset.testid ?? ''] ?? 0;
    });
    render(<AppBar />);
    const bar = screen.getByTestId('app-bar');
    // Flexible ends, each at least its own content and the other end's while that leaves
    // the omnibox 8.5rem, around a column of at most 420px that gives way first.
    expect(bar).toHaveClass('grid');
    expect(bar.style.gridTemplateColumns).toBe(
      'minmax(max(260px, min(112px, 100% - 112px - 8.5rem)), 1fr) minmax(0px, 420px) ' +
        'minmax(max(112px, min(260px, 100% - 260px - 8.5rem)), 1fr)',
    );
    expect(screen.getByTestId('app-bar-start')).toHaveClass('justify-self-start');
    expect(screen.getByTestId('app-bar-end')).toHaveClass('justify-self-end');
    width.mockRestore();
    const columns = Array.from(bar.children);
    expect(columns).toHaveLength(3);
    expect(columns[1].contains(screen.getByTestId('context-omnibox'))).toBe(true);
  });

  // The Chat/Dashboard switch is the omnibox's.
  it('lays out the left group: the sidebar toggle, then history', () => {
    render(<AppBar />);
    const controls = screen.getByTestId('app-bar-controls');
    expect(controls).toHaveClass('gap-2');
    expect(Array.from(controls.children)).toEqual([
      screen.getByTestId('left-sidebar-toggle'),
      screen.getByTestId('history-nav'),
    ]);
  });

  it('lays out the right group: notifications and settings in a capsule, then the right sidebar toggle', () => {
    render(<AppBar />);
    const actions = screen.getByTestId('app-bar-actions');
    expect(actions).toHaveClass('gap-3', 'pr-3.5');
    const global = screen.getByTestId('app-bar-global');
    expect(Array.from(actions.children)).toEqual([global, screen.getByTestId('right-sidebar-toggle')]);
    expect(Array.from(global.children)).toEqual([
      screen.getByTestId('notification-button'),
      screen.getByTestId('settings-button'),
    ]);
  });

  it('carries the app menu and the caption buttons off macOS', () => {
    render(<AppBar />);
    // Frameless there, so the bar is the whole title bar.
    const [start, , end] = Array.from(screen.getByTestId('app-bar').children);
    const menu = screen.getByRole('button', { name: /application menu/i });
    expect(start.firstElementChild?.contains(menu)).toBe(true);
    // The caption buttons come last, so the close button reaches the corner.
    expect(end.lastElementChild?.contains(screen.getByRole('button', { name: /close/i }))).toBe(true);
    expect(screen.queryByTestId('traffic-light-gutter')).not.toBeInTheDocument();
  });

  // The bar is the one place that decides, so `AppMenu`'s `Ctrl/Cmd+N`/`Q`
  // accelerators ride on that decision — they register only where it mounts.
  it('carries the menu accelerators off macOS', () => {
    render(<AppBar />);
    fireEvent.keyDown(window, { key: 'n', ctrlKey: true });
    expect(invokeMock).toHaveBeenCalledWith('new_window');
  });

  it('leaves the accelerators to the native menu on macOS', () => {
    setUserAgent(MAC_USER_AGENT);
    render(<AppBar />);
    // Would fire twice beside the native menu item if the menu mounted here.
    fireEvent.keyDown(window, { key: 'n', metaKey: true });
    expect(invokeMock).not.toHaveBeenCalled();
  });

  it('reserves the traffic lights and drops the caption chrome on macOS', () => {
    setUserAgent(MAC_USER_AGENT);
    render(<AppBar />);
    // The OS draws the lights and owns the menu bar, so the bar only makes room.
    const [start] = Array.from(screen.getByTestId('app-bar').children);
    expect(start.firstElementChild).toBe(screen.getByTestId('traffic-light-gutter'));
    expect(screen.queryByRole('button', { name: /application menu/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /minimize/i })).not.toBeInTheDocument();
  });
});
