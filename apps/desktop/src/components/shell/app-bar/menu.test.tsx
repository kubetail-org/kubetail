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

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { mockTauriCore } from '@/test-utils';

// Mocks ---------------------------------------------------------------

const { invokeMock, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { AppMenu } = await import('./menu');

// Helpers -------------------------------------------------------------

beforeEach(() => {
  invokeMock.mockReset();
  invokeMock.mockResolvedValue(undefined);
});

// Tests ---------------------------------------------------------------

describe('AppMenu', () => {
  it('renders an application menu trigger', () => {
    render(<AppMenu />);
    expect(screen.getByRole('button', { name: /application menu/i })).toBeInTheDocument();
  });

  it('invokes new_window when the New Window item is clicked', async () => {
    const user = userEvent.setup();
    render(<AppMenu />);
    await user.click(screen.getByRole('button', { name: /application menu/i }));
    await user.click(await screen.findByRole('menuitem', { name: /new window/i }));
    expect(invokeMock).toHaveBeenCalledWith('new_window');
  });

  it('invokes quit when the Quit item is clicked', async () => {
    const user = userEvent.setup();
    render(<AppMenu />);
    await user.click(screen.getByRole('button', { name: /application menu/i }));
    await user.click(await screen.findByRole('menuitem', { name: /quit/i }));
    expect(invokeMock).toHaveBeenCalledWith('quit');
  });

  it('invokes new_window on Ctrl+N', async () => {
    render(<AppMenu />);
    fireEvent.keyDown(window, { key: 'n', ctrlKey: true });
    await waitFor(() => expect(invokeMock).toHaveBeenCalledWith('new_window'));
  });

  it('invokes quit on Ctrl+Q', async () => {
    render(<AppMenu />);
    fireEvent.keyDown(window, { key: 'q', ctrlKey: true });
    await waitFor(() => expect(invokeMock).toHaveBeenCalledWith('quit'));
  });

  it('also accepts the Cmd (meta) modifier for shortcuts', async () => {
    render(<AppMenu />);
    fireEvent.keyDown(window, { key: 'n', metaKey: true });
    await waitFor(() => expect(invokeMock).toHaveBeenCalledWith('new_window'));
  });

  it('ignores plain keypresses without a modifier', () => {
    render(<AppMenu />);
    fireEvent.keyDown(window, { key: 'n' });
    expect(invokeMock).not.toHaveBeenCalled();
  });
});
