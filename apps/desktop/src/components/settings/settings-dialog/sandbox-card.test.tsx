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
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Sandbox } from '@/lib/sandbox';

const { platform } = vi.hoisted(() => ({ platform: { mac: false, linux: true } }));
vi.mock('@/lib/platform', () => ({ isMacOS: () => platform.mac, isLinux: () => platform.linux }));

const { SandboxCard } = await import('./sandbox-card');

const sandbox = (over: Partial<Sandbox> = {}): Sandbox => ({
  available: true,
  reason: '',
  networkAvailable: true,
  networkReason: '',
  failed: false,
  retry: vi.fn(),
  ...over,
});

beforeEach(() => {
  platform.mac = false;
  platform.linux = true;
});

describe('SandboxCard', () => {
  it('says new chats start in the sandbox, names its mechanism, and opens the console', async () => {
    const onTry = vi.fn();
    render(<SandboxCard sandbox={sandbox()} on onTry={onTry} />);
    expect(screen.getByText('Sandbox is on for new chats')).toBeInTheDocument();
    expect(screen.getByText(/^New chats run their commands in bubblewrap/)).toHaveTextContent(
      'Chats that exist keep their own switch',
    );
    expect(screen.getByText(/bubblewrap/)).toBeInTheDocument();
    expect(screen.getByText('Run a command in the sandbox to see what it can reach.')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Try a command' }));
    expect(onTry).toHaveBeenCalled();
  });

  it('names Seatbelt on macOS', () => {
    platform.mac = true;
    platform.linux = false;
    render(<SandboxCard sandbox={sandbox()} on onTry={vi.fn()} />);
    expect(screen.getByText(/Seatbelt/)).toBeInTheDocument();
  });

  // A change in flight leaves no handler.
  it('draws the switch disabled without a handler', () => {
    render(<SandboxCard sandbox={sandbox()} on onTry={vi.fn()} />);
    const toggle = screen.getByRole('switch', { name: 'Sandbox for new chats' });
    expect(toggle).toBeChecked();
    expect(toggle).toHaveAttribute('aria-disabled', 'true');
  });

  // The console tries the sandbox, which is there whatever new chats start with.
  it('says new chats start outside, unchecked, and keeps the console row', () => {
    render(<SandboxCard sandbox={sandbox()} on={false} onSwitch={vi.fn()} onTry={vi.fn()} />);
    expect(screen.getByText('Sandbox is off for new chats')).toBeInTheDocument();
    expect(screen.getByText(/^New chats run their commands as you/)).toBeInTheDocument();
    expect(screen.getByRole('switch', { name: 'Sandbox for new chats' })).not.toBeChecked();
    expect(screen.getByRole('button', { name: 'Try a command' })).toBeInTheDocument();
  });

  it('draws nothing until the default is known', () => {
    const { container } = render(<SandboxCard sandbox={sandbox()} on={undefined} onTry={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('asks before starting new chats outside, and does on confirm', async () => {
    const onSwitch = vi.fn();
    render(<SandboxCard sandbox={sandbox()} on onSwitch={onSwitch} onTry={vi.fn()} />);

    await userEvent.click(screen.getByRole('switch', { name: 'Sandbox for new chats' }));
    let dialog = await screen.findByRole('dialog', { name: 'Start new chats outside the sandbox?' });
    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(onSwitch).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole('switch', { name: 'Sandbox for new chats' }));
    dialog = await screen.findByRole('dialog', { name: 'Start new chats outside the sandbox?' });
    expect(dialog).toHaveTextContent('on a cluster without its own setting');
    expect(dialog).toHaveTextContent('Chats that exist keep their own switch.');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Start outside the sandbox' }));
    expect(onSwitch).toHaveBeenCalledWith(false);
  });

  it('starts new chats in the sandbox again with no confirm', async () => {
    const onSwitch = vi.fn();
    render(<SandboxCard sandbox={sandbox()} on={false} onSwitch={onSwitch} onTry={vi.fn()} />);
    await userEvent.click(screen.getByRole('switch', { name: 'Sandbox for new chats' }));
    expect(onSwitch).toHaveBeenCalledWith(true);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it("says there is no sandbox, with the install hint and the sidecar's reason spelled", () => {
    render(<SandboxCard sandbox={sandbox({ available: false, reason: 'bwrap not found ' })} on onTry={vi.fn()} />);
    expect(screen.getByText('No sandbox on this computer')).toBeInTheDocument();
    expect(screen.getByText(/Every command asks before it runs, and folder grants don't apply\./)).toHaveTextContent(
      'Install bubblewrap, then restart Kubetail.',
    );
    expect(screen.getByTestId('reason').textContent).toBe('bwrap not found\\u{20}');
    expect(screen.queryByRole('switch')).toBeNull();
  });

  it('gives no install hint off Linux', () => {
    platform.linux = false;
    render(<SandboxCard sandbox={sandbox({ available: false, reason: 'no' })} on onTry={vi.fn()} />);
    expect(screen.queryByText(/Install bubblewrap/)).toBeNull();
  });

  it('draws nothing before the sidecar answers', () => {
    const { container } = render(<SandboxCard sandbox={sandbox({ available: undefined })} on onTry={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('offers a retry after a failure', async () => {
    const failed = sandbox({ available: undefined, failed: true });
    render(<SandboxCard sandbox={failed} on onTry={vi.fn()} />);
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(failed.retry).toHaveBeenCalled();
  });
});
