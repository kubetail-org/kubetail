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

import type { ChatGrants as Grants } from '@/lib/chat-grants';
import type { SandboxFolders } from '@/lib/sandbox-folders';

const { grants, folders } = vi.hoisted(() => ({
  grants: { current: undefined as unknown as Grants },
  folders: { current: {} as Pick<SandboxFolders, 'chat' | 'revoking' | 'revokeError' | 'revoke'> },
}));
vi.mock('@/lib/chat-grants', () => ({ useChatGrants: () => grants.current }));
vi.mock('@/lib/sandbox-folders', () => ({ useSandboxFolders: () => folders.current }));

const { ChatGrants } = await import('./grants');

const remove = vi.fn();
const revoke = vi.fn();
const held = (over: Partial<Grants> = {}): Grants => ({
  rules: [
    { id: 'r1', line: 'Allow writes in dev / web' },
    { id: 'r2', line: 'Allow writes in "say ​\\"hi\\""' },
  ],
  removing: null,
  error: null,
  remove,
  ...over,
});

beforeEach(() => {
  vi.clearAllMocks();
  grants.current = held();
  folders.current = { chat: [], revoking: new Set(), revokeError: null, revoke };
});

describe('ChatGrants', () => {
  it('draws nothing for a chat with no rules', () => {
    grants.current = held({ rules: [] });
    const { container } = render(<ChatGrants chatID="c1" />);
    expect(container).toBeEmptyDOMElement();
  });

  it('draws the count, then one row per rule through VisibleText', async () => {
    render(<ChatGrants chatID="c1" />);
    const trigger = screen.getByRole('button', { name: /2 allowed/ });
    await userEvent.click(trigger);
    const list = screen.getByRole('list', { name: 'Allowed for this chat' });
    expect(list.querySelectorAll('li')).toHaveLength(2);
    expect(list).toHaveTextContent('Allow writes in dev / web');
    expect(list.querySelector('mark')).not.toBeNull();
  });

  it('removes a rule, its button down while in flight, and shows a refusal', async () => {
    render(<ChatGrants chatID="c1" />);
    await userEvent.click(screen.getByRole('button', { name: /2 allowed/ }));
    await userEvent.click(screen.getAllByRole('button', { name: 'Remove' })[0]);
    expect(remove).toHaveBeenCalledWith('r1');

    grants.current = held({ removing: 'r1', error: 'not found' });
    render(<ChatGrants chatID="c1" />);
    await userEvent.click(screen.getAllByRole('button', { name: /2 allowed/ })[1]);
    const buttons = screen.getAllByRole('button', { name: 'Remove' });
    expect(buttons.at(-2)).toBeDisabled();
    expect(screen.getByRole('alert')).toHaveTextContent('not found');
  });

  it("draws the chat's folders after its rules, each with its refusal, and revokes one as the chat's", async () => {
    folders.current = {
      ...folders.current,
      chat: [
        { id: 'f1', path: '/Users/ren/code', write: false, refusal: 'This folder does not exist.' },
        { id: 'f2', path: '/Users/ren/data', write: true, refusal: null },
      ],
    };
    render(<ChatGrants chatID="c1" />);
    await userEvent.click(screen.getByRole('button', { name: /4 allowed/ }));
    const rows = within(screen.getByRole('list', { name: 'Allowed for this chat' })).getAllByRole('listitem');
    expect(rows).toHaveLength(4);
    expect(rows[2]).toHaveTextContent('Reads of /Users/ren/code');
    expect(within(rows[2]).getByText('This folder does not exist.')).toBeInTheDocument();
    expect(rows[3]).toHaveTextContent('Reads and writes of /Users/ren/data');
    await userEvent.click(within(rows[2]).getByRole('button', { name: 'Remove' }));
    expect(revoke).toHaveBeenCalledWith('f1', 'Chat');
    expect(remove).not.toHaveBeenCalled();
  });

  it('draws a chat with folders alone, and a refused revoke', async () => {
    grants.current = held({ rules: [] });
    folders.current = {
      ...folders.current,
      chat: [{ id: 'f1', path: '/srv', write: false, refusal: null }],
      revoking: new Set(['f1']),
      revokeError: 'gone',
    };
    render(<ChatGrants chatID="c1" />);
    await userEvent.click(screen.getByRole('button', { name: /1 allowed/ }));
    expect(screen.getByRole('button', { name: 'Remove' })).toBeDisabled();
    expect(screen.getByRole('alert')).toHaveTextContent('gone');
  });

  // Two folders that differ by trailing whitespace never look alike beside Remove.
  it("spells a folder path's trailing whitespace", async () => {
    grants.current = held({ rules: [] });
    folders.current = {
      ...folders.current,
      chat: [{ id: 'f1', path: '/work/project  ', write: false, refusal: null }],
    };
    render(<ChatGrants chatID="c1" />);
    await userEvent.click(screen.getByRole('button', { name: /1 allowed/ }));
    const row = within(screen.getByRole('list', { name: 'Allowed for this chat' })).getByRole('listitem');
    expect(row.querySelector('mark')).not.toBeNull();
  });
});
