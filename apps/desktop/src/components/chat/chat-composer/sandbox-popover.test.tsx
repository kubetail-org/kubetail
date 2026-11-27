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

import { SandboxPopover } from './sandbox-popover';

// The popover's trigger, the first button drawn.
const trigger = () => screen.getAllByRole('button')[0];
const onSwitch = vi.fn();
const onThisMessage = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
});

const chat = { switching: false, onSwitch };

describe('SandboxPopover', () => {
  it('names where the next message runs', () => {
    const view = render(<SandboxPopover sandboxDisabled={false} thisMessage={false} onThisMessage={onThisMessage} />);
    expect(trigger()).toHaveTextContent('Sandbox');
    expect(trigger()).toBeEnabled();
    view.rerender(<SandboxPopover sandboxDisabled thisMessage={false} onThisMessage={onThisMessage} />);
    expect(trigger()).toHaveTextContent('Outside the sandbox');
    view.rerender(<SandboxPopover sandboxDisabled={false} thisMessage onThisMessage={onThisMessage} />);
    expect(trigger()).toHaveTextContent('Outside for this message');
  });

  it('is disabled until the value it names is known', () => {
    render(<SandboxPopover sandboxDisabled={undefined} thisMessage={false} onThisMessage={onThisMessage} />);
    expect(trigger()).toBeDisabled();
  });

  // The open chat's switch is the only one in the composer, and leaving the
  // sandbox goes through its dialog, which the popup must not take down.
  it("switches an open chat outside only through the switch's dialog", async () => {
    const user = userEvent.setup();
    render(<SandboxPopover sandboxDisabled={false} chat={chat} thisMessage={false} onThisMessage={onThisMessage} />);

    await user.click(trigger());
    await user.click(await screen.findByRole('button', { name: 'Sandboxed' }));
    expect(onSwitch).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Run outside the sandbox' }));
    expect(onSwitch).toHaveBeenCalledWith(true);
  });

  // A chat that has not started has no switch: it starts with its default,
  // which Settings changes.
  it("names a new chat's default and draws no switch", async () => {
    const user = userEvent.setup();
    const view = render(<SandboxPopover sandboxDisabled={false} thisMessage={false} onThisMessage={onThisMessage} />);
    await user.click(trigger());
    expect(await screen.findByText(/New chats on this cluster start in the sandbox/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Sandboxed' })).toBeNull();

    view.rerender(<SandboxPopover sandboxDisabled thisMessage={false} onThisMessage={onThisMessage} />);
    expect(screen.getByText(/New chats on this cluster start outside the sandbox/)).toBeInTheDocument();
  });

  it("runs the next message outside only through the toggle's dialog", async () => {
    const user = userEvent.setup();
    render(<SandboxPopover sandboxDisabled={false} chat={chat} thisMessage={false} onThisMessage={onThisMessage} />);

    await user.click(trigger());
    await user.click(await screen.findByRole('button', { name: 'Run this message outside the sandbox' }));
    expect(onThisMessage).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Run outside for this message' }));
    expect(onThisMessage).toHaveBeenCalledWith(true);
  });

  it('fixes the toggle of a held send, which Retry resends as it is', async () => {
    const user = userEvent.setup();
    render(<SandboxPopover sandboxDisabled={false} thisMessage onThisMessage={onThisMessage} held />);
    await user.click(trigger());
    expect(await screen.findByRole('button', { name: 'Keep this message in the sandbox' })).toBeDisabled();
  });

  it("fixes an open chat's switch under a held send", async () => {
    const user = userEvent.setup();
    render(
      <SandboxPopover sandboxDisabled={false} chat={chat} thisMessage={false} onThisMessage={onThisMessage} held />,
    );
    await user.click(trigger());
    expect(await screen.findByRole('button', { name: 'Sandboxed' })).toBeDisabled();
  });

  // A held create keeps the default it left with, so the popup says so of
  // this message, not of the cluster the window shows.
  it("names a held create's sandbox as its message's", async () => {
    const user = userEvent.setup();
    const view = render(<SandboxPopover sandboxDisabled thisMessage={false} onThisMessage={onThisMessage} held />);
    await user.click(trigger());
    expect(await screen.findByText('This message starts its chat outside the sandbox.')).toBeInTheDocument();

    view.rerender(<SandboxPopover sandboxDisabled={false} thisMessage={false} onThisMessage={onThisMessage} held />);
    expect(screen.getByText('This message starts its chat in the sandbox.')).toBeInTheDocument();
  });

  // A chat already outside has no turn to take out.
  it('offers no toggle outside the sandbox', async () => {
    const user = userEvent.setup();
    render(<SandboxPopover sandboxDisabled chat={chat} thisMessage={false} onThisMessage={onThisMessage} />);
    await user.click(trigger());
    expect(await screen.findAllByRole('button', { name: 'Outside the sandbox' })).toHaveLength(2);
    expect(screen.queryByRole('button', { name: /this message/ })).toBeNull();
  });
});
