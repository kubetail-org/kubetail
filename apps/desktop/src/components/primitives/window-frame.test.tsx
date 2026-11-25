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

import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  MAC_USER_AGENT,
  NON_MAC_USER_AGENT,
  WINDOWS_USER_AGENT,
  mockTauriWindow,
  restoreUserAgent,
  setUserAgent,
} from '@/test-utils';

// The frame queries the native window's maximized state (see `useWindowMaximized`).
const { windowMock, factory } = mockTauriWindow();
vi.mock('@tauri-apps/api/window', () => factory());

const { WindowFrame } = await import('./window-frame');
const { useWindowFrame } = await import('@/lib/window-frame');

function FrameReader() {
  const frame = useWindowFrame();
  return <div data-testid="reader" data-frame={frame?.dataset.testid ?? 'none'} />;
}

beforeEach(() => {
  windowMock.isMaximized.mockClear().mockResolvedValue(false);
  windowMock.onResized.mockClear();
  setUserAgent(NON_MAC_USER_AGENT);
});

afterEach(() => {
  restoreUserAgent();
});

describe('WindowFrame', () => {
  it('wraps children in the bordered frame on Linux', () => {
    render(
      <WindowFrame>
        <div>content</div>
      </WindowFrame>,
    );
    const frame = screen.getByTestId('window-frame');
    expect(frame).toContainElement(screen.getByText('content'));
    // Restored (not maximized): inset gutter + rounded corners + shadow.
    expect(frame.className).toContain('inset-4');
    expect(frame.className).toContain('rounded-lg');
  });

  // A popup portalled outside the frame keeps within its edge, not the viewport's.
  it('hands its element down on Linux, and none elsewhere', () => {
    const { unmount } = render(
      <WindowFrame>
        <FrameReader />
      </WindowFrame>,
    );
    expect(screen.getByTestId('reader')).toHaveAttribute('data-frame', 'window-frame');
    unmount();

    setUserAgent(MAC_USER_AGENT);
    render(
      <WindowFrame>
        <FrameReader />
      </WindowFrame>,
    );
    expect(screen.getByTestId('reader')).toHaveAttribute('data-frame', 'none');
  });

  it('collapses the gutter to full-bleed when the window is maximized', async () => {
    windowMock.isMaximized.mockResolvedValue(true);
    render(
      <WindowFrame>
        <div>content</div>
      </WindowFrame>,
    );
    await waitFor(() => {
      const frame = screen.getByTestId('window-frame');
      expect(frame.className).toContain('inset-0');
      expect(frame.className).not.toContain('inset-4');
      expect(frame.className).not.toContain('rounded-lg');
    });
  });

  // macOS (native decorations own the frame) and Windows (DWM draws the
  // borderless window's own shadow) are both opaque, full-bleed passthroughs.
  it.each([
    ['macOS', MAC_USER_AGENT],
    ['Windows', WINDOWS_USER_AGENT],
  ])('renders children full-bleed on %s', (_platform, userAgent) => {
    setUserAgent(userAgent);
    render(
      <WindowFrame>
        <div>content</div>
      </WindowFrame>,
    );
    expect(screen.queryByTestId('window-frame')).not.toBeInTheDocument();
    expect(screen.getByText('content')).toBeInTheDocument();
  });
});
