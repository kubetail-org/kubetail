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

import { SandboxSwitch } from './sandbox-switch';

const button = () => screen.getByRole('button', { name: /sandbox/i });
const onSwitch = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
});

describe('SandboxSwitch', () => {
  it("draws the chat's switch", () => {
    const view = render(<SandboxSwitch sandboxDisabled={false} switching={false} onSwitch={onSwitch} />);
    expect(button()).toHaveTextContent('Sandboxed');
    expect(button()).toBeEnabled();
    view.rerender(<SandboxSwitch sandboxDisabled switching={false} onSwitch={onSwitch} />);
    expect(button()).toHaveTextContent('Outside the sandbox');
  });

  // Before the list watch delivers the chat, it reads as a chat starts: sandboxed.
  it('is drawn disabled as Sandboxed before the chat arrives', () => {
    render(<SandboxSwitch sandboxDisabled={undefined} switching={false} onSwitch={onSwitch} />);
    expect(button()).toHaveTextContent('Sandboxed');
    expect(button()).toBeDisabled();
  });

  // Widening what a command can reach is said before it is done.
  it('switches outside only once the dialog is confirmed', async () => {
    const user = userEvent.setup();
    render(<SandboxSwitch sandboxDisabled={false} switching={false} onSwitch={onSwitch} />);

    await user.click(button());
    expect(screen.getByText("Run this chat's commands outside the sandbox?")).toBeInTheDocument();
    expect(
      screen.getByText(
        'Every command will run as you, with your files, your credentials and the network, after you approve it. ' +
          "Kubetail's sandbox will not confine it. You can switch back at any time; what already started keeps running as it started.",
      ),
    ).toBeInTheDocument();
    expect(onSwitch).not.toHaveBeenCalled();

    await user.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onSwitch).not.toHaveBeenCalled();

    await user.click(button());
    await user.click(screen.getByRole('button', { name: 'Run outside the sandbox' }));
    expect(onSwitch).toHaveBeenCalledWith(true);
  });

  it('stays sandboxed when the dialog is dismissed', async () => {
    const user = userEvent.setup();
    render(<SandboxSwitch sandboxDisabled={false} switching={false} onSwitch={onSwitch} />);

    await user.click(button());
    await user.keyboard('{Escape}');
    expect(screen.queryByText("Run this chat's commands outside the sandbox?")).not.toBeInTheDocument();
    expect(onSwitch).not.toHaveBeenCalled();
  });

  // Narrowing needs no warning.
  it('switches back at once', async () => {
    const user = userEvent.setup();
    render(<SandboxSwitch sandboxDisabled switching={false} onSwitch={onSwitch} />);

    await user.click(button());
    expect(onSwitch).toHaveBeenCalledWith(false);
    expect(screen.queryByText("Run this chat's commands outside the sandbox?")).toBeNull();
  });

  it('is disabled while the switch is in flight', () => {
    render(<SandboxSwitch sandboxDisabled switching onSwitch={onSwitch} />);
    expect(button()).toBeDisabled();
  });
});
