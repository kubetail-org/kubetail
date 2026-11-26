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

import { fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { MAC_USER_AGENT, NON_MAC_USER_AGENT, WINDOWS_USER_AGENT, restoreUserAgent, setUserAgent } from '@/test-utils';

// Mocks ---------------------------------------------------------------

// Each grip hands the drag to the native window manager.
const { startResizeDragging } = vi.hoisted(() => ({ startResizeDragging: vi.fn(() => Promise.resolve()) }));
vi.mock('@tauri-apps/api/window', () => ({ getCurrentWindow: () => ({ startResizeDragging }) }));

const { useWindowMaximizedMock } = vi.hoisted(() => ({ useWindowMaximizedMock: vi.fn(() => false) }));
vi.mock('@/lib/window-maximized', () => ({ useWindowMaximized: useWindowMaximizedMock }));

const { WindowResizeHandles } = await import('./window-resize-handles');

// Helpers -------------------------------------------------------------

beforeEach(() => {
  vi.clearAllMocks();
  useWindowMaximizedMock.mockReturnValue(false);
  // Linux: frameless and transparent, so the grips fill the gutter.
  setUserAgent(NON_MAC_USER_AGENT);
});

afterEach(() => {
  restoreUserAgent();
});

const gripsOf = (container: HTMLElement) => Array.from(container.children) as HTMLElement[];

// Tests ---------------------------------------------------------------

describe('WindowResizeHandles', () => {
  it('covers all four edges and all four corners', () => {
    const { container } = render(<WindowResizeHandles />);
    const cursors = gripsOf(container).map((el) => el.style.cursor);
    expect(cursors).toEqual([
      'ns-resize',
      'ns-resize',
      'ew-resize',
      'ew-resize',
      'nwse-resize',
      'nesw-resize',
      'nesw-resize',
      'nwse-resize',
    ]);
  });

  it('puts the corners after the edges, so they win the overlap', () => {
    const { container } = render(<WindowResizeHandles />);
    const corners = gripsOf(container).slice(4);
    expect(corners.every((el) => el.classList.contains('size-5'))).toBe(true);
  });

  it('starts a native resize in the grip’s own direction', () => {
    const { container } = render(<WindowResizeHandles />);
    fireEvent.mouseDown(gripsOf(container)[0], { button: 0 });
    expect(startResizeDragging).toHaveBeenCalledWith('North');

    fireEvent.mouseDown(gripsOf(container)[7], { button: 0 });
    expect(startResizeDragging).toHaveBeenLastCalledWith('SouthEast');
  });

  it('suppresses the webview’s own grab, which would beat the resize to the press', () => {
    const { container } = render(<WindowResizeHandles />);
    // WebKitGTK starts a text selection otherwise, and the async resize grab loses.
    const handled = fireEvent.mouseDown(gripsOf(container)[0], { button: 0 });
    expect(handled).toBe(false);
  });

  it('ignores anything but the primary button', () => {
    const { container } = render(<WindowResizeHandles />);
    fireEvent.mouseDown(gripsOf(container)[0], { button: 2 });
    expect(startResizeDragging).not.toHaveBeenCalled();
  });

  it('hugs the edge on Windows, where the window is full-bleed', () => {
    setUserAgent(WINDOWS_USER_AGENT);
    const { container } = render(<WindowResizeHandles />);
    // No transparent gutter to fill, so the strips stay thin.
    expect(gripsOf(container)[0].className).toContain('h-[5px]');
  });

  it('renders nothing on macOS, which keeps its native borders', () => {
    setUserAgent(MAC_USER_AGENT);
    const { container } = render(<WindowResizeHandles />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing while maximized, where there is no edge to drag', () => {
    useWindowMaximizedMock.mockReturnValue(true);
    const { container } = render(<WindowResizeHandles />);
    expect(container).toBeEmptyDOMElement();
  });
});
