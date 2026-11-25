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
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { mockTauriCore } from '@/test-utils';

const { invokeMock, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { ThemeProvider } = await import('@/lib/theme');
const { ColorSchemePicker } = await import('./color-scheme-picker');

function renderPicker() {
  render(
    <ThemeProvider>
      <ColorSchemePicker />
    </ThemeProvider>,
  );
}

const isDark = () => document.documentElement.classList.contains('dark');

beforeEach(() => {
  document.documentElement.classList.remove('dark');
  invokeMock.mockReset();
  invokeMock.mockResolvedValue(undefined);
});

describe('ColorSchemePicker', () => {
  it('offers System, Light and Dark as one Mode choice, System picked by default', () => {
    renderPicker();
    const group = screen.getByRole('radiogroup', { name: 'Mode' });
    expect(group).toBeInTheDocument();
    expect(screen.getAllByRole('radio').map((r) => r.textContent)).toEqual(['System', 'Light', 'Dark']);
    expect(screen.getByRole('radio', { name: 'System' })).toBeChecked();
  });

  it('applies and persists the scheme picked', async () => {
    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByRole('radio', { name: 'Dark' }));

    expect(screen.getByRole('radio', { name: 'Dark' })).toBeChecked();
    expect(isDark()).toBe(true);
    await waitFor(() =>
      expect(invokeMock).toHaveBeenCalledWith('update_host_file', { patch: { colorSchemePreference: 'dark' } }),
    );
  });

  it('moves the choice with the arrow keys', async () => {
    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByRole('radio', { name: 'Light' }));
    await user.keyboard('{ArrowRight}');

    expect(screen.getByRole('radio', { name: 'Dark' })).toBeChecked();
    expect(screen.getByRole('radio', { name: 'Dark' })).toHaveFocus();
    expect(isDark()).toBe(true);
  });
});
