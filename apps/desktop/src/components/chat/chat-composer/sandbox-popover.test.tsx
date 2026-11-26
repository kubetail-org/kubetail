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
import { describe, expect, it } from 'vitest';

import { SandboxPopover } from './sandbox-popover';

const button = () => screen.getByRole('button', { name: /sandbox/i });

describe('SandboxPopover', () => {
  it('names where the chat runs', () => {
    const view = render(<SandboxPopover sandboxDisabled={false} />);
    expect(button()).toHaveTextContent('Sandbox');
    expect(button()).toBeEnabled();
    view.rerender(<SandboxPopover sandboxDisabled />);
    expect(button()).toHaveTextContent('Outside the sandbox');
  });

  it('is disabled until the list watch delivers the chat', () => {
    render(<SandboxPopover sandboxDisabled={undefined} />);
    expect(button()).toHaveTextContent('Sandbox');
    expect(button()).toBeDisabled();
  });

  it('opens the sandbox popup', async () => {
    render(<SandboxPopover sandboxDisabled={false} />);
    await userEvent.click(button());
    expect(await screen.findByText('Network options are coming soon.')).toBeInTheDocument();
  });
});
