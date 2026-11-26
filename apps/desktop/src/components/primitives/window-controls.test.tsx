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

import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { mockTauriWindow } from '@/test-utils';

// Mocks ---------------------------------------------------------------

// The native window handle the controls drive. `getCurrentWindow()` returns
// this same object so tests can assert the method calls directly.
const { windowMock, factory } = mockTauriWindow();
vi.mock('@tauri-apps/api/window', () => factory());

const { WindowControls } = await import('./window-controls');

// Helpers -------------------------------------------------------------

beforeEach(() => {
  windowMock.minimize.mockClear();
  windowMock.toggleMaximize.mockClear();
  windowMock.close.mockClear();
});

// Tests ---------------------------------------------------------------

describe('WindowControls', () => {
  it('renders minimize, maximize, and close buttons', () => {
    render(<WindowControls />);
    expect(screen.getByRole('button', { name: /minimize/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /maximize/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /close/i })).toBeInTheDocument();
  });

  it('minimizes the window when the minimize button is clicked', async () => {
    const user = userEvent.setup();
    render(<WindowControls />);
    await user.click(screen.getByRole('button', { name: /minimize/i }));
    expect(windowMock.minimize).toHaveBeenCalledTimes(1);
  });

  it('toggles maximize when the maximize button is clicked', async () => {
    const user = userEvent.setup();
    render(<WindowControls />);
    await user.click(screen.getByRole('button', { name: /maximize/i }));
    expect(windowMock.toggleMaximize).toHaveBeenCalledTimes(1);
  });

  it('closes the window when the close button is clicked', async () => {
    const user = userEvent.setup();
    render(<WindowControls />);
    await user.click(screen.getByRole('button', { name: /close/i }));
    expect(windowMock.close).toHaveBeenCalledTimes(1);
  });
});
