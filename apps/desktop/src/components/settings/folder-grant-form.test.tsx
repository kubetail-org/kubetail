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

import type { SandboxFolders } from '@/lib/sandbox-folders';
import { expectCodeText } from '@/test-utils';

const { folders, chats } = vi.hoisted(() => ({
  folders: { current: {} as SandboxFolders },
  chats: [] as (string | undefined)[],
}));
vi.mock('@/lib/sandbox-folders', () => ({
  useSandboxFolders: (chatID?: string) => {
    chats.push(chatID);
    return folders.current;
  },
}));

const { FolderGrantForm } = await import('./folder-grant-form');

function withFolders(over: Partial<SandboxFolders>) {
  folders.current = {
    always: [],
    chat: [],
    never: [],
    closed: [],
    wide: ['/Users/ren', '/Users'],
    granting: false,
    revoking: new Set(),
    grantError: null,
    revokeError: null,
    grant: vi.fn(async () => true),
    revoke: vi.fn(async () => {}),
    ...over,
  };
}

const field = () => screen.getByRole('textbox', { name: 'Folder to grant' });

beforeEach(() => {
  chats.length = 0;
  withFolders({});
});

describe('FolderGrantForm', () => {
  it("turns off the webview's suggestions in the path", () => {
    render(<FolderGrantForm durations={['Always']} submitLabel="Add" />);
    expectCodeText(field());
  });

  it('grants the folder as typed, read or read and write, and clears once it is taken', async () => {
    render(<FolderGrantForm durations={['Always']} submitLabel="Add" />);
    await userEvent.type(field(), ' /Users/ren/code  ');
    await userEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(folders.current.grant).toHaveBeenCalledWith(' /Users/ren/code  ', false, 'Always');
    expect(field()).toHaveValue('');

    await userEvent.type(field(), '/Users/ren/svc');
    await userEvent.click(screen.getByRole('checkbox', { name: 'read and write' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(folders.current.grant).toHaveBeenLastCalledWith('/Users/ren/svc', true, 'Always');
    expect(screen.getByRole('checkbox', { name: 'read and write' })).not.toBeChecked();
  });

  it("reads the chat's folders", () => {
    render(<FolderGrantForm chatID="c1" durations={['Always']} submitLabel="Grant" />);
    expect(chats).toContain('c1');
  });

  it('holds the submit while the field is empty or a grant is in flight', async () => {
    const { unmount } = render(<FolderGrantForm durations={['Always']} submitLabel="Grant" />);
    expect(screen.getByRole('button', { name: 'Grant' })).toBeDisabled();
    await userEvent.type(field(), '/Users/ren/code');
    expect(screen.getByRole('button', { name: 'Grant' })).toBeEnabled();
    unmount();

    withFolders({ granting: true });
    render(<FolderGrantForm durations={['Always']} submitLabel="Grant" />);
    await userEvent.type(field(), '/Users/ren/code');
    expect(screen.getByRole('button', { name: 'Grant' })).toBeDisabled();
  });

  // For this chat is the narrower grant, so it comes first and is picked.
  it('offers both durations, For this chat first and picked', async () => {
    render(<FolderGrantForm chatID="c1" durations={['Chat', 'Always']} submitLabel="Grant" />);
    const picker = screen.getByRole('tablist', { name: 'How long' });
    expect(
      within(picker)
        .getAllByRole('tab')
        .map((t) => t.textContent),
    ).toEqual(['For this chat', 'Always']);
    expect(screen.getByRole('tab', { name: 'For this chat', selected: true })).toBeInTheDocument();

    await userEvent.type(field(), '/Users/ren/code');
    await userEvent.click(screen.getByRole('button', { name: 'Grant' }));
    expect(folders.current.grant).toHaveBeenLastCalledWith('/Users/ren/code', false, 'Chat');

    await userEvent.click(screen.getByRole('tab', { name: 'Always' }));
    expect(screen.getByRole('tab', { name: 'Always', selected: true })).toBeInTheDocument();
    await userEvent.type(field(), '/Users/ren/svc');
    await userEvent.click(screen.getByRole('button', { name: 'Grant' }));
    expect(folders.current.grant).toHaveBeenLastCalledWith('/Users/ren/svc', false, 'Always');
  });

  it('draws no picker for Always alone', () => {
    render(<FolderGrantForm durations={['Always']} submitLabel="Add" />);
    expect(screen.queryByRole('tablist', { name: 'How long' })).toBeNull();
  });

  it('starts from initial', () => {
    render(
      <FolderGrantForm
        durations={['Always']}
        submitLabel="Grant"
        initial={{ path: '/Users/ren/.helm', write: true }}
      />,
    );
    expect(field()).toHaveValue('/Users/ren/.helm');
    expect(screen.getByRole('checkbox', { name: 'read and write' })).toBeChecked();
  });

  it('warns of a wide folder as it is typed', async () => {
    withFolders({ wide: ['/Users/ren', '/Users', '/Volumes'] });
    render(<FolderGrantForm durations={['Chat', 'Always']} submitLabel="Grant" />);
    const home =
      /This lets commands read everything in your home folder except the credential folders Kubetail knows of/;
    const typeIn = async (typed: string) => {
      await userEvent.clear(field());
      await userEvent.type(field(), typed);
    };
    await typeIn('/Users/ren/');
    expect(screen.getByText(home)).toBeInTheDocument();
    await typeIn('/Users/ren/code/..');
    expect(screen.getByText(home)).toBeInTheDocument();
    await typeIn('/Users/ren/code');
    expect(screen.queryByText(home)).toBeNull();
    await typeIn('/Volumes');
    expect(screen.getByText(/This lets commands read every disk mounted on this Mac\./)).toBeInTheDocument();
  });

  it("keeps the field on a refusal, draws its reason, and offers a link's target", async () => {
    withFolders({
      grant: vi.fn(async () => false),
      grantError: {
        message: '/Users/ren/src is a link to /Users/ren/code; grant /Users/ren/code instead.',
        target: '/Users/ren/code',
      },
    });
    render(<FolderGrantForm durations={['Chat', 'Always']} submitLabel="Grant" />);
    await userEvent.type(field(), '/Users/ren/src');
    await userEvent.click(screen.getByRole('button', { name: 'Grant' }));
    expect(field()).toHaveValue('/Users/ren/src');
    expect(
      screen.getByText('/Users/ren/src is a link to /Users/ren/code; grant /Users/ren/code instead.'),
    ).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Grant /Users/ren/code' }));
    expect(field()).toHaveValue('/Users/ren/code');
  });

  it('draws Cancel with onCancel alone', async () => {
    const { unmount } = render(<FolderGrantForm durations={['Always']} submitLabel="Add" />);
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull();
    unmount();

    const onCancel = vi.fn();
    render(<FolderGrantForm durations={['Chat', 'Always']} submitLabel="Grant" onCancel={onCancel} />);
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onCancel).toHaveBeenCalledOnce();
  });

  it('keeps what was typed when its pane draws it again, and grants it', async () => {
    const onCancel = vi.fn();
    const { rerender } = render(<FolderGrantForm durations={['Always']} submitLabel="Grant" onCancel={onCancel} />);
    await userEvent.type(field(), '/Users/ren/code');

    rerender(<FolderGrantForm durations={['Always']} submitLabel="Grant" onCancel={onCancel} />);
    expect(field()).toHaveValue('/Users/ren/code');
    await userEvent.click(screen.getByRole('button', { name: 'Grant' }));
    expect(folders.current.grant).toHaveBeenCalledWith('/Users/ren/code', false, 'Always');
  });

  it('calls onGranted once the sidecar takes the grant, and not on a refusal', async () => {
    const onGranted = vi.fn();
    withFolders({ grant: vi.fn(async () => false) });
    const { unmount } = render(<FolderGrantForm durations={['Always']} submitLabel="Grant" onGranted={onGranted} />);
    await userEvent.type(field(), '/Users/ren/code');
    await userEvent.click(screen.getByRole('button', { name: 'Grant' }));
    expect(onGranted).not.toHaveBeenCalled();
    unmount();

    withFolders({});
    render(<FolderGrantForm durations={['Always']} submitLabel="Grant" onGranted={onGranted} />);
    await userEvent.type(field(), '/Users/ren/code');
    await userEvent.click(screen.getByRole('button', { name: 'Grant' }));
    expect(onGranted).toHaveBeenCalledOnce();
  });
});
