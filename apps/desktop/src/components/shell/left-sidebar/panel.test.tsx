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

import { Collapsible } from '@base-ui/react/collapsible';
import { Menu } from '@base-ui/react/menu';
import { Popover } from '@base-ui/react/popover';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { stubResizeObserver } from '@/test-utils';

import { LeftSidebarPanel } from './panel';

// Helpers -------------------------------------------------------------

stubResizeObserver();

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

// jsdom loads no CSS and lays nothing out, so every element reads as changing instantly at
// no width; this gives the panel a computed transition, by default its 200ms slide, and the
// width it starts at, which the returned setter moves. The contents hold the 256px open width.
function stubPanelTransition(transition: Partial<CSSStyleDeclaration> = SLIDE, startWidth = 256) {
  let width = startWidth;
  const isPanel = (el: Element) => (el as HTMLElement).dataset.testid === 'left-sidebar-panel';
  const computed = window.getComputedStyle;
  vi.spyOn(window, 'getComputedStyle').mockImplementation((el, pseudo) =>
    isPanel(el) ? (transition as CSSStyleDeclaration) : computed(el, pseudo),
  );
  const isContents = (el: HTMLElement) => el.dataset.testid === 'left-sidebar-contents';
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(function (this: HTMLElement) {
    if (isContents(this)) return 256;
    return isPanel(this) ? width : 0;
  });
  return (px: number) => {
    width = px;
  };
}
const SLIDE = { transitionProperty: 'width', transitionDuration: '0.2s' };

// jsdom doesn't implement pointer capture; the handle calls it, so stub it out.
if (!HTMLElement.prototype.setPointerCapture) {
  HTMLElement.prototype.setPointerCapture = () => {};
}

// A sidebar's popups, and a disclosure, whose trigger is `aria-expanded` too.
const NAV = (
  <>
    <Popover.Root>
      <Popover.Trigger>Pick</Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner>
          <Popover.Popup>Popover body</Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
    <Menu.Root>
      <Menu.Trigger>Row actions</Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner>
          <Menu.Popup>
            <Menu.Item>Rename</Menu.Item>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
    <Collapsible.Root defaultOpen>
      <Collapsible.Trigger>Section</Collapsible.Trigger>
      <Collapsible.Panel>Section body</Collapsible.Panel>
    </Collapsible.Root>
  </>
);

const panel = () => screen.getByTestId('left-sidebar-panel');
const contents = () => screen.getByTestId('left-sidebar-contents');
const handle = () => screen.getByRole('separator', { name: 'Resize sidebar' });

// Tests ---------------------------------------------------------------

describe('LeftSidebarPanel', () => {
  // Cmd/Ctrl+B closes the panel with the popup still open; `inert` does not reach a portal.
  it.each([
    {
      kind: 'popover',
      trigger: 'Pick',
      popup: () => screen.queryByText('Popover body'),
    },
    {
      kind: 'menu',
      trigger: 'Row actions',
      popup: () => screen.queryByRole('menuitem', { name: 'Rename' }),
    },
  ])('closes a $kind opened from it when it closes', async ({ trigger, popup }) => {
    // The slide keeps the nav, and so the popup's trigger, mounted: only a dismissal closes
    // the popup then.
    stubPanelTransition();
    const user = userEvent.setup();
    const { rerender } = render(<LeftSidebarPanel open nav={NAV} />);
    await user.click(screen.getByRole('button', { name: trigger }));
    await waitFor(() => expect(popup()).toBeInTheDocument());

    rerender(<LeftSidebarPanel open={false} nav={NAV} />);
    await waitFor(() => expect(popup()).not.toBeInTheDocument());
    expect(screen.getByRole('button', { name: trigger, hidden: true })).toBeInTheDocument();
  });

  it('leaves a popup opened outside it, such as the cluster picker, and its disclosures', async () => {
    // So the disclosure is still there to look at while the panel slides shut.
    stubPanelTransition();
    const user = userEvent.setup();
    const ui = (open: boolean) => (
      <>
        <Popover.Root>
          <Popover.Trigger>Cluster</Popover.Trigger>
          <Popover.Portal>
            <Popover.Positioner>
              <Popover.Popup>Cluster picker</Popover.Popup>
            </Popover.Positioner>
          </Popover.Portal>
        </Popover.Root>
        <LeftSidebarPanel open={open} nav={NAV} />
      </>
    );
    const { rerender } = render(ui(true));
    await user.click(screen.getByRole('button', { name: 'Cluster' }));
    await screen.findByText('Cluster picker');

    rerender(ui(false));
    // A click it was dealt would close it at once, as it did on opening.
    await act(async () => {});
    expect(screen.getByText('Cluster picker')).toBeInTheDocument();
    // `aria-expanded` with no `aria-haspopup`: not a popup's trigger, so left as it was.
    expect(screen.getByText('Section body')).toBeVisible();
  });

  it('keeps its contents against the moving edge, so they slide with it', () => {
    render(<LeftSidebarPanel open />);
    expect(contents().parentElement).toHaveClass('justify-end', 'overflow-hidden');
  });

  it('draws the nav flush in the sidebar color, padded 8px', () => {
    render(<LeftSidebarPanel open nav={<div>nav-content</div>} />);
    expect(panel()).toHaveClass('bg-sidebar');
    expect(panel().className).not.toMatch(/\b(ring|shadow|rounded)/);
    expect(contents()).toHaveClass('p-2');
    expect(contents().contains(screen.getByText('nav-content'))).toBe(true);
  });

  it('sits in the row as an ordinary flex item', () => {
    render(<LeftSidebarPanel open />);
    expect(panel()).toHaveClass('shrink-0');
    expect(panel()).not.toHaveClass('absolute', 'fixed');
  });

  it('closes to no width, keeping its contents at the open width so they slide out', () => {
    stubPanelTransition();
    const { rerender } = render(<LeftSidebarPanel open nav={<div>nav-content</div>} />);
    expect(panel().style.width).toBe('256px');
    expect(panel()).not.toHaveAttribute('inert');

    rerender(<LeftSidebarPanel open={false} nav={<div>nav-content</div>} />);
    expect(panel().style.width).toBe('0px');
    expect(contents().style.width).toBe('256px');
    // The library's `flex-1` would make the row's width its basis, and reflow it.
    expect(contents()).toHaveClass('flex-none');
    expect(contents()).not.toHaveClass('flex-1');
    // Still mounted, so the slide has something to show; inert, so Tab cannot reach it.
    expect(screen.getByText('nav-content')).toBeInTheDocument();
    expect(panel()).toHaveAttribute('inert');
    expect(screen.queryByRole('separator', { name: 'Resize sidebar' })).not.toBeInTheDocument();
  });

  // Closed, the chat list would keep its watch for rows no one sees.
  it('mounts its nav only while it shows', () => {
    const { rerender } = render(<LeftSidebarPanel open={false} nav={<div>nav-content</div>} />);
    expect(screen.queryByText('nav-content')).not.toBeInTheDocument();

    rerender(<LeftSidebarPanel open nav={<div>nav-content</div>} />);
    expect(screen.getByText('nav-content')).toBeInTheDocument();
  });

  it('drops its nav once the closing slide ends, keeping it if the panel reopens first', () => {
    const setPanelWidth = stubPanelTransition();
    const ui = (open: boolean) => (
      <LeftSidebarPanel
        open={open}
        nav={
          <div data-testid="nav" className="transition-opacity">
            nav-content
          </div>
        }
      />
    );
    const { rerender } = render(ui(true));
    rerender(ui(false));
    // A transition inside the panel, or another of its properties, is not the slide.
    fireEvent.transitionEnd(screen.getByTestId('nav'), { propertyName: 'width' });
    fireEvent.transitionEnd(panel(), { propertyName: 'opacity' });
    expect(screen.getByText('nav-content')).toBeInTheDocument();

    // Reopened mid-slide: the slide that ends is the opening one.
    rerender(ui(true));
    fireEvent.transitionEnd(panel(), { propertyName: 'width' });
    expect(screen.getByText('nav-content')).toBeInTheDocument();

    rerender(ui(false));
    // Slid shut.
    setPanelWidth(0);
    fireEvent.transitionEnd(panel(), { propertyName: 'width' });
    expect(screen.queryByText('nav-content')).not.toBeInTheDocument();
  });

  // Reduced motion turned on mid-slide drops the slide: a `transitioncancel`, with the panel
  // already at no width, and no `transitionend` after it.
  it('drops its nav when the closing slide is cancelled at no width', () => {
    const setPanelWidth = stubPanelTransition();
    const ui = (open: boolean) => <LeftSidebarPanel open={open} nav={<div>nav-content</div>} />;
    const { rerender } = render(ui(true));
    rerender(ui(false));
    setPanelWidth(120);
    // Still part-way: a cancel there is no close yet.
    fireEvent.transitionCancel(panel(), { propertyName: 'width' });
    expect(screen.getByText('nav-content')).toBeInTheDocument();

    setPanelWidth(0);
    fireEvent.transitionCancel(panel(), { propertyName: 'width' });
    expect(screen.queryByText('nav-content')).not.toBeInTheDocument();
  });

  it('keeps its nav when reopening cancels the closing slide', () => {
    stubPanelTransition();
    const ui = (open: boolean) => <LeftSidebarPanel open={open} nav={<div>nav-content</div>} />;
    const { rerender } = render(ui(true));
    rerender(ui(false));
    rerender(ui(true));
    // The closing slide gives way to the opening one, from wherever it had got to.
    fireEvent.transitionCancel(panel(), { propertyName: 'width' });
    expect(screen.getByText('nav-content')).toBeInTheDocument();
  });

  // Opened and closed again before the open width was laid out: still at no width, it
  // starts no slide, and no `transitionend` comes.
  it('drops its nav at once when it closes already at no width', () => {
    stubPanelTransition(SLIDE, 0);
    const { rerender } = render(<LeftSidebarPanel open nav={<div>nav-content</div>} />);
    rerender(<LeftSidebarPanel open={false} nav={<div>nav-content</div>} />);
    expect(screen.queryByText('nav-content')).not.toBeInTheDocument();
  });

  // With no slide no `transitionend` comes. Reduced motion's `transition-none` keeps the
  // duration and drops the property; a drag drops the transition whole.
  it.each([
    { why: 'reduced motion', transition: { transitionProperty: 'none', transitionDuration: '0.2s' } },
    {
      why: 'a slide of another property',
      transition: { transitionProperty: 'opacity, width', transitionDuration: '0.2s, 0s' },
    },
    { why: 'no transition', transition: { transitionProperty: 'all', transitionDuration: '0s' } },
  ])('drops its nav at once when it closes with $why', ({ transition }) => {
    stubPanelTransition(transition);
    const { rerender } = render(<LeftSidebarPanel open nav={<div>nav-content</div>} />);
    rerender(<LeftSidebarPanel open={false} nav={<div>nav-content</div>} />);
    expect(screen.queryByText('nav-content')).not.toBeInTheDocument();
  });

  it('waits out a slide named through `all`', () => {
    stubPanelTransition({ transitionProperty: 'all', transitionDuration: '0.2s' });
    const { rerender } = render(<LeftSidebarPanel open nav={<div>nav-content</div>} />);
    rerender(<LeftSidebarPanel open={false} nav={<div>nav-content</div>} />);
    expect(screen.getByText('nav-content')).toBeInTheDocument();
  });

  // Grabbed mid-slide, the drag would drop the slide and jump the edge to the stored width.
  it('offers its handle once it has slid open', () => {
    const setPanelWidth = stubPanelTransition(SLIDE, 0);
    const ui = (open: boolean) => <LeftSidebarPanel open={open} />;
    const { rerender } = render(ui(false));
    rerender(ui(true));
    expect(screen.queryByRole('separator', { name: 'Resize sidebar' })).not.toBeInTheDocument();

    setPanelWidth(120);
    // A slide cancelled part-way, as a reversal cancels it, is not the end of the opening.
    fireEvent.transitionCancel(panel(), { propertyName: 'width' });
    expect(screen.queryByRole('separator', { name: 'Resize sidebar' })).not.toBeInTheDocument();

    setPanelWidth(256);
    fireEvent.transitionEnd(panel(), { propertyName: 'width' });
    expect(handle()).toBeInTheDocument();
  });

  // Capped short of its stored width, it has slid open at the cap.
  it('offers its handle once it has slid open to its cap', () => {
    const setPanelWidth = stubPanelTransition(SLIDE, 0);
    localStorage.setItem('kubetail:sidebar-width', '480');
    const ui = (open: boolean) => <LeftSidebarPanel open={open} room={{ css: '256px', px: () => 256 }} />;
    const { rerender } = render(ui(false));
    rerender(ui(true));
    expect(panel().style.maxWidth).toBe('256px');
    expect(contents().style.maxWidth).toBe('256px');
    setPanelWidth(256);
    fireEvent.transitionEnd(panel(), { propertyName: 'width' });
    expect(handle()).toBeInTheDocument();
  });

  it('offers its handle at once when it opens with no slide', () => {
    stubPanelTransition({ transitionProperty: 'none', transitionDuration: '0.2s' }, 0);
    const { rerender } = render(<LeftSidebarPanel open={false} />);
    rerender(<LeftSidebarPanel open />);
    expect(handle()).toBeInTheDocument();
  });

  // Closed and reopened before the closing slide moved it: still at its width, no slide starts.
  it('offers its handle at once when it reopens already at its width', () => {
    stubPanelTransition();
    const { rerender } = render(<LeftSidebarPanel open />);
    rerender(<LeftSidebarPanel open={false} />);
    rerender(<LeftSidebarPanel open />);
    expect(handle()).toBeInTheDocument();
  });

  it('slides its width over 200ms while it opens and closes, unless the user asks for less motion', () => {
    stubPanelTransition(SLIDE, 0);
    const { rerender } = render(<LeftSidebarPanel open={false} />);
    expect(panel()).toHaveClass('transition-[width]', 'duration-200', 'ease-out', 'motion-reduce:transition-none');
    rerender(<LeftSidebarPanel open />);
    expect(panel()).toHaveClass('transition-[width]');
  });

  // A slide would lag the pointer, and an arrow press during one would step from where it
  // had got to.
  it('changes its width at once once open, for a drag or a key to keep up with', () => {
    render(<LeftSidebarPanel open />);
    expect(panel()).not.toHaveClass('transition-[width]');
  });

  it('resizes by dragging the handle on its edge and persists the width', () => {
    render(<LeftSidebarPanel open />);
    fireEvent.pointerDown(handle());
    fireEvent.pointerMove(window, { clientX: 300 });
    expect(panel().style.width).toBe('300px');
    expect(contents().style.width).toBe('300px');
    expect(localStorage.getItem('kubetail:sidebar-width')).toBe('300');
    fireEvent.pointerUp(window);
  });

  it('clamps the dragged width to the min/max bounds', () => {
    render(<LeftSidebarPanel open />);
    fireEvent.pointerDown(handle());
    fireEvent.pointerMove(window, { clientX: 9999 });
    expect(panel().style.width).toBe('480px');
    fireEvent.pointerMove(window, { clientX: 10 });
    expect(panel().style.width).toBe('200px');
    fireEvent.pointerUp(window);
  });

  it('steps its width from the keyboard, from its width as drawn, up to its maximum', () => {
    localStorage.setItem('kubetail:sidebar-width', '464');
    render(<LeftSidebarPanel open />);
    const drawn = vi.spyOn(panel(), 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 464, 400));
    act(() => handle().focus());
    expect(handle()).toHaveAttribute('aria-valuemin', '200');
    expect(handle()).toHaveAttribute('aria-valuemax', '480');
    fireEvent.keyDown(handle(), { key: 'ArrowRight' });
    expect(panel().style.width).toBe('480px');
    drawn.mockReturnValue(new DOMRect(0, 0, 480, 400));
    fireEvent.keyDown(handle(), { key: 'ArrowLeft' });
    expect(panel().style.width).toBe('464px');
  });

  // Capped at 240px in an 800px row with the right sidebar open, a stored 480px is the
  // user's for a wider window.
  it('takes its bounds from its cap, and keeps the stored width on a press the cap holds back', () => {
    localStorage.setItem('kubetail:sidebar-width', '480');
    const px = vi.fn(() => 240);
    render(
      <div style={{ width: 800 }}>
        <LeftSidebarPanel open room={{ css: '240px', px }} />
      </div>,
    );
    Object.defineProperty(panel().parentElement!, 'clientWidth', { value: 800, configurable: true });
    vi.spyOn(panel(), 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 240, 400));
    act(() => handle().focus());
    expect(px).toHaveBeenLastCalledWith(800);
    expect(handle()).toHaveAttribute('aria-valuenow', '240');
    expect(handle()).toHaveAttribute('aria-valuemax', '240');
    fireEvent.keyDown(handle(), { key: 'ArrowRight' });
    expect(localStorage.getItem('kubetail:sidebar-width')).toBe('480');
    fireEvent.keyDown(handle(), { key: 'ArrowLeft' });
    expect(panel().style.width).toBe('224px');
  });

  it('keeps the stored width on a drag outward the cap holds back', () => {
    localStorage.setItem('kubetail:sidebar-width', '480');
    render(<LeftSidebarPanel open room={{ css: '240px', px: () => 240 }} />);
    vi.spyOn(panel(), 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 240, 400));
    fireEvent.pointerDown(handle());
    fireEvent.pointerMove(window, { clientX: 260 });
    fireEvent.pointerUp(window);
    expect(localStorage.getItem('kubetail:sidebar-width')).toBe('480');
  });

  // Near the breakpoint, in a window inset by Linux's frame.
  it('reads a cap below its minimum as its minimum too', () => {
    render(<LeftSidebarPanel open room={{ css: '180px', px: () => 180 }} />);
    vi.spyOn(panel(), 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 180, 400));
    act(() => handle().focus());
    expect(handle()).toHaveAttribute('aria-valuemin', '180');
    expect(handle()).toHaveAttribute('aria-valuemax', '180');
  });

  it('restores the last-saved width on mount (new windows inherit it)', () => {
    localStorage.setItem('kubetail:sidebar-width', '320');
    render(<LeftSidebarPanel open />);
    expect(panel().style.width).toBe('320px');
  });
});
