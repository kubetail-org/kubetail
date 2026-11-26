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

const { deleteMock } = vi.hoisted(() => ({ deleteMock: vi.fn() }));
vi.mock('urql', () => ({ useMutation: () => [{ fetching: false }, deleteMock] }));
vi.mock('@/gql', () => ({ graphql: () => ({}) }));

const { ChatDeleteDialog } = await import('./chat-delete-dialog');

const CHAT = { id: 'c1', title: 'First chat' };
const onClose = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  deleteMock.mockResolvedValue({});
});

describe('ChatDeleteDialog', () => {
  it('stays closed with no chat to confirm', () => {
    render(<ChatDeleteDialog chat={null} onClose={onClose} />);
    expect(screen.queryByText('Delete chat?')).not.toBeInTheDocument();
  });

  it('names the chat and what deleting it means', () => {
    render(<ChatDeleteDialog chat={CHAT} onClose={onClose} />);
    expect(screen.getByText('Delete chat?')).toBeInTheDocument();
    // Announced with the dialog: which chat, then what deleting it means.
    expect(screen.getByRole('dialog')).toHaveAccessibleDescription(
      'First chat This chat and its messages go from every window. This cannot be undone.',
    );
  });

  it('spells the title as the title bar does', () => {
    render(<ChatDeleteDialog chat={{ id: 'c1', title: 'First\u200bchat' }} onClose={onClose} />);
    expect(screen.getByRole('dialog')).toHaveTextContent('First\\u{200B}chat');
  });

  // A run of spaces keeps its width, and a title spelled out to hundreds of characters wraps
  // within the dialog rather than hiding its end.
  it('draws every character of the title, wrapping an unbroken run', () => {
    render(<ChatDeleteDialog chat={{ id: 'c1', title: 'ops  west' }} onClose={onClose} />);
    const title = screen.getByText('ops  west', { normalizer: (text) => text });
    expect(title).toHaveClass('whitespace-pre-wrap', 'wrap-anywhere');
  });

  // A title spelled out to hundreds of characters runs many lines; in the header, under the
  // dialog's height cap, it would leave the buttons no room.
  it('draws the title in the scrolling body with the buttons, not the header', () => {
    render(<ChatDeleteDialog chat={{ id: 'c1', title: `ops${'\u200b'.repeat(77)}` }} onClose={onClose} />);
    const box = screen.getByText(/^ops/);
    expect(screen.getByRole('dialog').querySelector('[data-slot="dialog-header"]')).not.toContainElement(box);
    expect(box.closest('.overflow-y-auto')).toContainElement(screen.getByRole('button', { name: 'Delete chat' }));
    expect(
      box.compareDocumentPosition(screen.getByRole('button', { name: 'Delete chat' })) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it('keeps naming the chat while it fades out', async () => {
    // An exit animation that never ends: jsdom has none, so the dialog would unmount at once.
    const unfinished = { finished: new Promise<never>(() => {}) } as unknown as Animation;
    Element.prototype.getAnimations = () => [unfinished];
    try {
      const { rerender } = render(<ChatDeleteDialog chat={CHAT} onClose={onClose} />);
      rerender(<ChatDeleteDialog chat={null} onClose={onClose} />);
      expect(await screen.findByRole('dialog')).toHaveTextContent('First chat');
    } finally {
      // @ts-expect-error -- jsdom has no getAnimations; put it back as it was
      delete Element.prototype.getAnimations;
    }
  });

  it('names the next chat it is asked about', () => {
    const { rerender } = render(<ChatDeleteDialog chat={CHAT} onClose={onClose} />);
    rerender(<ChatDeleteDialog chat={null} onClose={onClose} />);
    rerender(<ChatDeleteDialog chat={{ id: 'c2', title: 'Second chat' }} onClose={onClose} />);
    expect(screen.getByRole('dialog')).toHaveTextContent('Second chat');
  });

  it('deletes on confirm, and closes', async () => {
    const user = userEvent.setup();
    render(<ChatDeleteDialog chat={CHAT} onClose={onClose} />);
    await user.click(screen.getByRole('button', { name: 'Delete chat' }));
    expect(deleteMock).toHaveBeenCalledWith({ id: 'c1' });
    expect(onClose).toHaveBeenCalled();
  });

  it('closes once, writing nothing, when kept', async () => {
    const user = userEvent.setup();
    render(<ChatDeleteDialog chat={CHAT} onClose={onClose} />);
    await user.click(screen.getByRole('button', { name: 'Keep chat' }));
    expect(onClose).toHaveBeenCalledOnce();
    expect(deleteMock).not.toHaveBeenCalled();
  });

  it('closes once, writing nothing, when dismissed with Escape', async () => {
    const user = userEvent.setup();
    render(<ChatDeleteDialog chat={CHAT} onClose={onClose} />);
    await user.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalledOnce();
    expect(deleteMock).not.toHaveBeenCalled();
  });
});
