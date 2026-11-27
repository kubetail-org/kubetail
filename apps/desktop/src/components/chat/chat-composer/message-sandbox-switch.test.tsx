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

import { MessageSandboxSwitch } from './message-sandbox-switch';

const button = () => screen.getByRole('button', { name: /this message/i });
const onSwitch = vi.fn();
const TITLE = "Run this message's commands outside the sandbox?";

beforeEach(() => {
  vi.clearAllMocks();
});

describe('MessageSandboxSwitch', () => {
  it("draws the message's toggle", () => {
    const view = render(<MessageSandboxSwitch on={false} onSwitch={onSwitch} />);
    expect(button()).toHaveTextContent('Run this message outside the sandbox');
    view.rerender(<MessageSandboxSwitch on onSwitch={onSwitch} />);
    expect(button()).toHaveTextContent('Keep this message in the sandbox');
  });

  // Widening what one turn's commands can reach is said before it is done.
  it('turns on only once the dialog is confirmed', async () => {
    const user = userEvent.setup();
    render(<MessageSandboxSwitch on={false} onSwitch={onSwitch} />);

    await user.click(button());
    expect(screen.getByText(TITLE)).toBeInTheDocument();
    expect(onSwitch).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onSwitch).not.toHaveBeenCalled();

    await user.click(button());
    await user.keyboard('{Escape}');
    expect(onSwitch).not.toHaveBeenCalled();

    await user.click(button());
    await user.click(screen.getByRole('button', { name: 'Run outside for this message' }));
    expect(onSwitch).toHaveBeenCalledWith(true);
  });

  // Narrowing needs no warning.
  it('turns off at once', async () => {
    const user = userEvent.setup();
    render(<MessageSandboxSwitch on onSwitch={onSwitch} />);

    await user.click(button());
    expect(onSwitch).toHaveBeenCalledWith(false);
    expect(screen.queryByText(TITLE)).toBeNull();
  });
});
