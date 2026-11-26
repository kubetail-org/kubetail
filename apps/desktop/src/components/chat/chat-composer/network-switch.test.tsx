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

import { NetworkSwitch } from './network-switch';

const button = () => screen.getByRole('button', { name: /network/i });
const onSwitch = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
});

describe('NetworkSwitch', () => {
  it("draws the chat's network switch", () => {
    const view = render(<NetworkSwitch networkEnabled={false} available switching={false} onSwitch={onSwitch} />);
    expect(button()).toHaveTextContent('No network');
    expect(button()).toBeEnabled();
    view.rerender(<NetworkSwitch networkEnabled available switching={false} onSwitch={onSwitch} />);
    expect(button()).toHaveTextContent('Network on');
  });

  // Before the list watch delivers the chat, it reads as a chat starts: no network.
  it('is drawn disabled as No network before the chat arrives', () => {
    render(<NetworkSwitch networkEnabled={undefined} available switching={false} onSwitch={onSwitch} />);
    expect(button()).toHaveTextContent('No network');
    expect(button()).toBeDisabled();
  });

  // Widening what a command can reach is said before it is done.
  it('turns network on only once the dialog is confirmed', async () => {
    const user = userEvent.setup();
    render(<NetworkSwitch networkEnabled={false} available switching={false} onSwitch={onSwitch} />);

    await user.click(button());
    expect(screen.getByText("Give this chat's commands the internet?")).toBeInTheDocument();
    expect(
      screen.getByText(
        'Commands in this chat can reach any server on the internet, and send it what they read: ' +
          'cluster data and your workspace. They still hold no credential and cannot read your private files.',
      ),
    ).toBeInTheDocument();
    expect(onSwitch).not.toHaveBeenCalled();

    await user.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onSwitch).not.toHaveBeenCalled();

    await user.click(button());
    await user.click(screen.getByRole('button', { name: 'Turn network on' }));
    expect(onSwitch).toHaveBeenCalledWith(true);
  });

  it('leaves network off when the dialog is dismissed', async () => {
    const user = userEvent.setup();
    render(<NetworkSwitch networkEnabled={false} available switching={false} onSwitch={onSwitch} />);

    await user.click(button());
    await user.keyboard('{Escape}');
    expect(screen.queryByText("Give this chat's commands the internet?")).not.toBeInTheDocument();
    expect(onSwitch).not.toHaveBeenCalled();
  });

  // Narrowing needs no warning.
  it('turns network off at once', async () => {
    const user = userEvent.setup();
    render(<NetworkSwitch networkEnabled available switching={false} onSwitch={onSwitch} />);

    await user.click(button());
    expect(onSwitch).toHaveBeenCalledWith(false);
    expect(screen.queryByText("Give this chat's commands the internet?")).toBeNull();
  });

  it('is disabled while a switch is in flight', () => {
    render(<NetworkSwitch networkEnabled available switching onSwitch={onSwitch} />);
    expect(button()).toBeDisabled();
  });

  // Where the machine offers no network nothing turns it on, but a switch left on
  // can still be turned off.
  it('cannot be turned on where network is unavailable', () => {
    const view = render(
      <NetworkSwitch networkEnabled={false} available={false} switching={false} onSwitch={onSwitch} />,
    );
    expect(button()).toBeDisabled();
    view.rerender(<NetworkSwitch networkEnabled available={false} switching={false} onSwitch={onSwitch} />);
    expect(button()).toBeEnabled();
  });
});
