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

import { SidebarProvider, useSidebar } from '@kubetail/ui/elements/sidebar';
import { createRootRoute, createRoute, Link, Outlet } from '@tanstack/react-router';
import { act, fireEvent, screen, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it } from 'vitest';

import { RightSidebarProvider, useRightSidebar } from '@/lib/right-sidebar';
import { mockMatchMedia, renderWithRouter, stubResizeObserver } from '@/test-utils';

import { LeftSidebar } from './sidebar';

// Helpers -------------------------------------------------------------

stubResizeObserver();

// The app bar's toggle, as far as this suite needs it: it flips the library's state.
function Toggle() {
  const { toggleSidebar } = useSidebar();
  return (
    <button type="button" aria-label="Toggle sidebar" onClick={toggleSidebar}>
      toggle
    </button>
  );
}

function RightToggle() {
  const { toggle } = useRightSidebar();
  return (
    <button type="button" onClick={toggle}>
      toggle-right
    </button>
  );
}

// The sidebar reads open/closed state from the provider the layout mounts, and closes
// the drawer on navigation — hence the router around it. The link is the one the
// drawer would carry.
function renderSidebar(nav?: ReactNode) {
  const root = createRootRoute({
    component: () => (
      <RightSidebarProvider mode="chat">
        <SidebarProvider>
          <Toggle />
          <RightToggle />
          <LeftSidebar nav={nav ?? <Link to="/dashboard">go-dashboard</Link>} />
          <Outlet />
        </SidebarProvider>
      </RightSidebarProvider>
    ),
  });
  const chat = createRoute({ getParentRoute: () => root, path: '/chat', component: () => null });
  const dashboard = createRoute({ getParentRoute: () => root, path: '/dashboard', component: () => null });
  return renderWithRouter(root.addChildren([chat, dashboard]), '/chat');
}

const clickToggle = () => fireEvent.click(screen.getByRole('button', { name: /toggle sidebar/i }));

// The library's own narrow signal reads `window.innerWidth` on every change event,
// so a width has to move with the media query for it to follow.
function narrowWindow(fireChange: (matches: boolean) => void) {
  return (narrow: boolean) => {
    Object.defineProperty(window, 'innerWidth', { value: narrow ? 600 : 1200, configurable: true });
    fireChange(narrow);
  };
}

// A test that ends narrow would start the next one narrow.
beforeEach(() => {
  Object.defineProperty(window, 'innerWidth', { value: 1200, configurable: true });
});

const panel = () => screen.queryByTestId('left-sidebar-panel');
const open = () => panel() !== null && !panel()!.hasAttribute('inert');
const overlaid = () => screen.queryByTestId('sidebar-overlay') !== null;

// Tests ---------------------------------------------------------------

describe('LeftSidebar', () => {
  it('opens by default, holding the nav', async () => {
    await renderSidebar(<div>nav-content</div>);
    expect(open()).toBe(true);
    expect(panel()!.contains(screen.getByText('nav-content'))).toBe(true);
  });

  it('slides closed and open again from the toggle', async () => {
    await renderSidebar();
    clickToggle();
    expect(panel()).toHaveAttribute('inert');
    expect(panel()!.style.width).toBe('0px');
    clickToggle();
    expect(open()).toBe(true);
  });

  it('reopens on the keyboard shortcut', async () => {
    await renderSidebar();
    clickToggle();
    expect(open()).toBe(false);
    // `Cmd/Ctrl+B` is the library's own binding.
    fireEvent.keyDown(window, { key: 'b', ctrlKey: true });
    expect(open()).toBe(true);
  });

  // At 800px, a 480px sidebar and the page's 320px floor fill the row: the right sidebar's
  // 240px would be clipped away.
  it('caps its width beside the page, leaving the right sidebar its minimum while open', async () => {
    await renderSidebar();
    expect(panel()!.style.maxWidth).toBe('calc(100cqw - min(320px, 50cqw))');
    expect(screen.getByTestId('left-sidebar-contents').style.maxWidth).toBe('calc(100cqw - min(320px, 50cqw))');
    fireEvent.click(screen.getByRole('button', { name: 'toggle-right' }));
    expect(panel()!.style.maxWidth).toBe('calc(100cqw - 240px - min(320px, 50cqw))');
  });

  it('leaves the drawer uncapped, since it covers the page', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    setNarrow(true);
    clickToggle();
    expect(panel()!.style.maxWidth).toBe('');
  });

  it('remembers a manual close across a narrow→wide breakpoint round-trip', async () => {
    // `matches` here means "below the medium breakpoint".
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    clickToggle();
    setNarrow(true);
    setNarrow(false);
    expect(open()).toBe(false);
  });

  it('leaves the row when the window narrows, rather than throwing a drawer over the page', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    setNarrow(true);
    // No pill: nothing holds space at the left edge.
    expect(panel()).toBeNull();
    expect(overlaid()).toBe(false);
    // Back in the row once there is space for it beside the page.
    setNarrow(false);
    expect(open()).toBe(true);
  });

  it('opens a drawer that slides in from the row’s left edge when the toggle asks while narrow', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar(<div>nav-content</div>);
    setNarrow(true);
    clickToggle();
    const drawer = screen.getByTestId('sidebar-overlay');
    expect(drawer.contains(screen.getByText('nav-content'))).toBe(true);
    expect(drawer).toHaveClass(
      'left-0',
      'transition-transform',
      'duration-200',
      'ease-out',
      'starting:-translate-x-full',
      'motion-reduce:transition-none',
    );
    // In the panel's own look.
    expect(drawer.contains(panel())).toBe(true);
    expect(open()).toBe(true);
  });

  it('leaves a drawer behind when the window widens again', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    setNarrow(true);
    clickToggle();
    expect(overlaid()).toBe(true);
    setNarrow(false);
    expect(overlaid()).toBe(false);
    expect(open()).toBe(true);
  });

  it('leaves the drawer closed when the window narrows again', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    setNarrow(true);
    clickToggle();
    setNarrow(false);
    setNarrow(true);
    expect(overlaid()).toBe(false);
  });

  it('keeps the keyboard in the drawer, and hands focus back on close', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    setNarrow(true);
    // Focused first, the way a keyboard user reaches it: jsdom's click moves no
    // focus of its own, and it is where focus has to come back to.
    const toggle = screen.getByRole('button', { name: /toggle sidebar/i });
    toggle.focus();
    clickToggle();
    const drawer = screen.getByTestId('sidebar-overlay');
    expect(drawer).toHaveAttribute('role', 'dialog');
    expect(drawer).toHaveAttribute('aria-modal', 'true');
    expect(drawer.contains(document.activeElement)).toBe(true);
    fireEvent.click(screen.getByTestId('sidebar-scrim'));
    expect(document.activeElement).toBe(toggle);
  });

  it('closes the drawer from the scrim', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    setNarrow(true);
    clickToggle();
    fireEvent.click(screen.getByTestId('sidebar-scrim'));
    expect(overlaid()).toBe(false);
  });

  // The scrim and the toggle sit outside the modal; a keyboard user Tabbing through the
  // drawer reaches this one.
  it('closes the drawer from a close button inside it, handing focus back', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    setNarrow(true);
    const toggle = screen.getByRole('button', { name: /toggle sidebar/i });
    toggle.focus();
    clickToggle();
    const close = within(screen.getByTestId('sidebar-overlay')).getByRole('button', { name: 'Close sidebar' });
    fireEvent.click(close);
    expect(overlaid()).toBe(false);
    expect(document.activeElement).toBe(toggle);
  });

  it('closes the drawer on Escape', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    setNarrow(true);
    clickToggle();
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(overlaid()).toBe(false);
  });

  it('leaves the drawer open on an Escape an IME takes', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    setNarrow(true);
    clickToggle();
    fireEvent.keyDown(window, { key: 'Escape', isComposing: true });
    expect(overlaid()).toBe(true);
  });

  // A dialog opened from inside the drawer (the chat list's delete confirm) dismisses
  // on Escape by stopping the keydown at `document`. This listener is on `window`,
  // later in the bubble path, so one keypress closes the dialog alone — nothing else
  // in the code records that ordering.
  it('leaves the drawer open when an overlay inside it consumed the Escape', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    setNarrow(true);
    clickToggle();
    const consume = (e: Event) => e.stopPropagation();
    document.addEventListener('keydown', consume);
    fireEvent.keyDown(screen.getByTestId('sidebar-overlay'), { key: 'Escape' });
    document.removeEventListener('keydown', consume);
    expect(overlaid()).toBe(true);
  });

  it('closes the drawer when a link in it navigates', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    setNarrow(true);
    clickToggle();
    // Otherwise it sits over the page it just opened.
    await act(async () => {
      screen.getByRole('link', { name: 'go-dashboard' }).click();
    });
    expect(overlaid()).toBe(false);
  });

  // New chat on /chat, or the resource already shown: the location stays put.
  it('closes the drawer on a link to where the window already is', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar(
      <>
        <Link to="/chat">new-chat</Link>
        <button type="button">
          <span>not-a-link</span>
        </button>
      </>,
    );
    setNarrow(true);
    clickToggle();
    fireEvent.click(screen.getByText('not-a-link'));
    expect(overlaid()).toBe(true);
    // A modified click navigates nothing in this window.
    for (const modifier of [
      { metaKey: true },
      { ctrlKey: true },
      { shiftKey: true },
      { altKey: true },
      { button: 1 },
    ]) {
      fireEvent.click(screen.getByRole('link', { name: 'new-chat' }), modifier);
      expect(overlaid()).toBe(true);
    }
    await act(async () => {
      fireEvent.click(screen.getByRole('link', { name: 'new-chat' }));
    });
    expect(overlaid()).toBe(false);
  });

  it('reopens the drawer from the toggle', async () => {
    const setNarrow = narrowWindow(mockMatchMedia());
    await renderSidebar();
    setNarrow(true);
    clickToggle();
    fireEvent.keyDown(window, { key: 'Escape' });
    // The toggle means the same thing at every width.
    clickToggle();
    expect(overlaid()).toBe(true);
  });
});
