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

import { ThemeProvider } from '@/lib/theme';

import { SettingsPopover } from './SettingsPopover';

function renderSettingsPopover() {
  return render(
    <ThemeProvider>
      <SettingsPopover>
        <button type="button">Settings</button>
      </SettingsPopover>
    </ThemeProvider>,
  );
}

describe('SettingsPopover', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'matchMedia',
      vi.fn(() => ({
        matches: false,
        media: '',
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    );
  });

  it('renders a Timezone row when open', () => {
    renderSettingsPopover();

    fireEvent.click(screen.getByRole('button', { name: 'Settings' }));

    expect(screen.getByText('Timezone')).toBeInTheDocument();
  });

  it('shows UTC as the default timezone value', () => {
    renderSettingsPopover();

    fireEvent.click(screen.getByRole('button', { name: 'Settings' }));

    expect(screen.getByText('UTC')).toBeInTheDocument();
  });
});
