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

import { render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { mockTauriCore } from '@/test-utils';

const { factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { ThemeProvider } = await import('@/lib/theme');
const { GeneralSettings } = await import('./general');

describe('GeneralSettings', () => {
  it('holds the Appearance group, its Mode row holding the scheme picker', () => {
    render(
      <ThemeProvider>
        <GeneralSettings />
      </ThemeProvider>,
    );

    const appearance = screen.getByRole('group', { name: 'Appearance' });
    expect(appearance).toHaveTextContent('Follow your system, or pick one.');
    expect(within(appearance).getByRole('radiogroup', { name: 'Mode' })).toBeInTheDocument();
  });
});
