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
import { beforeEach, describe, expect, it, vi } from 'vitest';

// Mocks ---------------------------------------------------------------

// The dialog renders in `AppDialogs`; the button only requests it.
const { openDialog } = vi.hoisted(() => ({ openDialog: vi.fn() }));
vi.mock('@/lib/dialog', () => ({ useDialog: () => ({ openDialog }) }));

const { SettingsButton } = await import('./settings-button');

// Helpers -------------------------------------------------------------

const button = () => screen.getByRole('button', { name: 'Settings' });

beforeEach(() => {
  vi.clearAllMocks();
});

// Tests ---------------------------------------------------------------

describe('SettingsButton', () => {
  it('offers settings from the app bar', () => {
    render(<SettingsButton />);
    expect(button()).toBeInTheDocument();
  });

  it('asks the dialog host to open settings', () => {
    render(<SettingsButton />);
    fireEvent.click(button());
    expect(openDialog).toHaveBeenCalledWith('settings');
  });
});
