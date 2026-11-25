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
  it('says the sandbox is on, names its mechanism, and opens the console', async () => {
    const onTry = vi.fn();
    render(<SandboxCard sandbox={sandbox()} on onTry={onTry} />);
    expect(screen.getByText('Sandbox is on')).toBeInTheDocument();
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

  // Placeholder: no global switch exists yet, so nothing here may turn it off.
  it('draws the switch checked and disabled without a handler', () => {
    render(<SandboxCard sandbox={sandbox()} on onTry={vi.fn()} />);
    const toggle = screen.getByRole('switch', { name: 'Sandbox' });
    expect(toggle).toBeChecked();
    expect(toggle).toHaveAttribute('aria-disabled', 'true');
  });

  it('says the sandbox is off, unchecked, with no console row', () => {
    render(<SandboxCard sandbox={sandbox()} on={false} onSwitch={vi.fn()} onTry={vi.fn()} />);
    expect(screen.getByText('Sandbox is off')).toBeInTheDocument();
    expect(screen.getByRole('switch', { name: 'Sandbox' })).not.toBeChecked();
    expect(screen.queryByRole('button', { name: 'Try a command' })).toBeNull();
  });

  it('asks before turning the sandbox off, and turns it off on confirm', async () => {
    const onSwitch = vi.fn();
    render(<SandboxCard sandbox={sandbox()} on onSwitch={onSwitch} onTry={vi.fn()} />);

    await userEvent.click(screen.getByRole('switch', { name: 'Sandbox' }));
    let dialog = await screen.findByRole('dialog', { name: 'Turn off the sandbox?' });
    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(onSwitch).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole('switch', { name: 'Sandbox' }));
    dialog = await screen.findByRole('dialog', { name: 'Turn off the sandbox?' });
    expect(dialog).toHaveTextContent('Every command in every chat will run outside the sandbox');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Turn off' }));
    expect(onSwitch).toHaveBeenCalledWith(false);
  });

  it('turns the sandbox back on with no confirm', async () => {
    const onSwitch = vi.fn();
    render(<SandboxCard sandbox={sandbox()} on={false} onSwitch={onSwitch} onTry={vi.fn()} />);
    await userEvent.click(screen.getByRole('switch', { name: 'Sandbox' }));
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
