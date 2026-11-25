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

import { screen, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { describe, expect, it, vi } from 'vitest';

import { mockTauriCore, renderWithRouter } from '@/test-utils';

const { invokeMock, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());
// The window chrome reads the native window, which jsdom has none of.
vi.mock('@/components/primitives/window-frame', () => ({
  WindowFrame: ({ children }: { children: ReactNode }) => <div data-testid="window-frame">{children}</div>,
}));
vi.mock('@/components/primitives/window-resize-handles', () => ({
  WindowResizeHandles: () => <div data-testid="resize-handles" />,
}));

const { Route } = await import('./__root');

describe('the root route', () => {
  it('holds the app behind the ready gate, inside the window frame', async () => {
    invokeMock.mockReturnValue(new Promise(() => {}));
    await renderWithRouter(Route, '/');

    expect(invokeMock).toHaveBeenCalledWith('ready');
    expect(within(screen.getByTestId('window-frame')).getByRole('status')).toHaveAttribute('aria-busy', 'true');
    // A sibling of the frame, whose `contain: paint` would clip the fixed grips.
    expect(screen.getByTestId('window-frame')).not.toContainElement(screen.getByTestId('resize-handles'));
  });

  // Every other call stays in flight, so the providers mount and wait.
  it('says a path no route matches was not found, once the host is ready', async () => {
    invokeMock.mockImplementation((cmd: string) => (cmd === 'ready' ? Promise.resolve() : new Promise(() => {})));
    await renderWithRouter(Route, '/nowhere');

    expect(await screen.findByText('Page not found.')).toBeInTheDocument();
  });
});
