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

const { renameMock, deleteMock } = vi.hoisted(() => ({ renameMock: vi.fn(), deleteMock: vi.fn() }));
vi.mock('urql', () => ({
  useMutation: (doc: { kind?: string }) => [{ fetching: false }, doc.kind === 'delete' ? deleteMock : renameMock],
}));
vi.mock('@/gql', () => ({
  graphql: (source: string) => ({ kind: source.includes('chatDelete') ? 'delete' : 'rename' }),
}));

const { ChatHeader } = await import('./chat-header');

const CHAT = { id: 'c1', title: 'First chat' };
const titleButton = () => screen.getByRole('button', { name: /First chat/ });

beforeEach(() => {
  vi.clearAllMocks();
  renameMock.mockResolvedValue({});
  deleteMock.mockResolvedValue({});
});

describe('ChatHeader', () => {
  it('is a 44px bar underlined across the panel', () => {
    render(<ChatHeader chat={CHAT} />);
    expect(screen.getByTestId('chat-header')).toHaveClass(
      'h-11',
      'shrink-0',
      'pl-6',
      'pr-4',
      'border-b',
      'border-border',
      'justify-between',
    );
  });

  it('names the chat, through VisibleText, on a button with a down chevron', () => {
    render(<ChatHeader chat={{ id: 'c1', title: 'a\u200bb' }} />);
    const button = screen.getByRole('button', { name: /a\\u\{200B\}b/ });
    expect(button).toHaveClass('text-sm', 'font-medium', 'text-foreground');
    expect(button.querySelector('.lucide-chevron-down')).toHaveClass('size-4');
  });

  it('names a chat with no title Untitled chat, on its button and in its delete confirmation', async () => {
    const user = userEvent.setup();
    render(<ChatHeader chat={{ id: 'c1', title: '' }} />);
    await user.click(screen.getByRole('button', { name: 'Untitled chat' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Delete' }));
    expect(await screen.findByRole('dialog')).toHaveTextContent('Untitled chat');
  });

  // The library's button won't shrink; a long title ellipsizes inside it only if it does.
  it('lets a long title’s button shrink, keeping its chevron in view', () => {
    render(<ChatHeader chat={CHAT} />);
    const button = screen.getByRole('button', { name: /First chat/ });
    expect(button).toHaveClass('shrink', 'min-w-0', 'truncate');
    expect(button).not.toHaveClass('shrink-0');
  });

  it('renames the chat in place from its menu', async () => {
    const user = userEvent.setup();
    render(<ChatHeader chat={CHAT} />);
    await user.click(titleButton());
    await user.click(await screen.findByRole('menuitem', { name: 'Rename' }));

    const box = screen.getByRole('textbox', { name: 'Chat title' });
    expect(screen.getByTestId('chat-header').contains(box)).toBe(true);
    await user.clear(box);
    await user.type(box, 'Renamed{Enter}');
    expect(renameMock).toHaveBeenCalledWith({ id: 'c1', title: 'Renamed' });
    expect(titleButton()).toBeInTheDocument();
  });

  it.each(['{Enter}', '{Escape}'])('gives focus back to the title after %s closes the rename', async (key) => {
    const user = userEvent.setup();
    render(<ChatHeader chat={CHAT} />);
    await user.click(titleButton());
    await user.click(await screen.findByRole('menuitem', { name: 'Rename' }));
    await user.keyboard(key);
    expect(titleButton()).toHaveFocus();
  });

  it('leaves focus where the user moved it when the rename closes on a blur', async () => {
    const user = userEvent.setup();
    render(
      <>
        <ChatHeader chat={CHAT} />
        <button type="button">elsewhere</button>
      </>,
    );
    await user.click(titleButton());
    await user.click(await screen.findByRole('menuitem', { name: 'Rename' }));
    await user.click(screen.getByRole('button', { name: 'elsewhere' }));
    expect(titleButton()).not.toHaveFocus();
    expect(screen.getByRole('button', { name: 'elsewhere' })).toHaveFocus();
  });

  it('asks before deleting the chat from its menu', async () => {
    const user = userEvent.setup();
    render(<ChatHeader chat={CHAT} />);
    await user.click(titleButton());
    await user.click(await screen.findByRole('menuitem', { name: 'Delete' }));

    expect(deleteMock).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Delete chat' }));
    expect(deleteMock).toHaveBeenCalledWith({ id: 'c1' });
  });
});
