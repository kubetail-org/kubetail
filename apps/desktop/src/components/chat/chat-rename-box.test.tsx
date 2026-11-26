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

import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { expectProseText } from '@/test-utils';

const { renameMock } = vi.hoisted(() => ({ renameMock: vi.fn() }));
vi.mock('urql', () => ({ useMutation: () => [{ fetching: false }, renameMock] }));
vi.mock('@/gql', () => ({ graphql: () => ({}) }));

const { ChatRenameBox } = await import('./chat-rename-box');

const CHAT = { id: 'c1', title: 'First chat' };
const onDone = vi.fn();
const box = () => screen.getByRole('textbox', { name: 'Chat title' });

// jsdom lays nothing out, so it has no scrollIntoView.
const scrollIntoView = vi.fn();
Element.prototype.scrollIntoView = scrollIntoView;

beforeEach(() => {
  vi.clearAllMocks();
  renameMock.mockResolvedValue({});
});

describe('ChatRenameBox', () => {
  it('opens focused on the title, as prose', () => {
    render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    expect(box()).toHaveFocus();
    expect(box()).toHaveValue('First chat');
    expectProseText(box());
  });

  it('renames on Enter, trimmed, and is done', async () => {
    const user = userEvent.setup();
    render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    await user.clear(box());
    await user.type(box(), '  Renamed {Enter}');
    expect(renameMock).toHaveBeenCalledWith({ id: 'c1', title: 'Renamed' });
    expect(onDone).toHaveBeenCalled();
  });

  it('writes nothing for an unchanged or empty title', async () => {
    const user = userEvent.setup();
    render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    await user.type(box(), '{Enter}');
    await user.clear(box());
    await user.type(box(), '{Enter}');
    expect(renameMock).not.toHaveBeenCalled();
  });

  // Another window renamed the chat while this box was open.
  it('writes nothing for a draft left as editing began, though the title has changed since', () => {
    const { rerender } = render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    rerender(<ChatRenameBox chat={{ ...CHAT, title: 'Renamed elsewhere' }} onDone={onDone} />);
    expect(box()).toHaveValue('First chat');
    fireEvent.keyDown(box(), { key: 'Enter' });
    expect(renameMock).not.toHaveBeenCalled();
    expect(onDone).toHaveBeenCalledWith('keyboard');
  });

  // A generated title can end in a space: the sidecar trims before it cuts to 80.
  it('writes nothing for an untouched draft, though trimming it would change it', () => {
    const { rerender } = render(<ChatRenameBox chat={{ ...CHAT, title: 'First chat ' }} onDone={onDone} />);
    rerender(<ChatRenameBox chat={{ ...CHAT, title: 'Renamed elsewhere' }} onDone={onDone} />);
    fireEvent.keyDown(box(), { key: 'Enter' });
    expect(renameMock).not.toHaveBeenCalled();
  });

  it('trims the space a derived title’s cut left at its end, when the user removes it', async () => {
    const user = userEvent.setup();
    render(<ChatRenameBox chat={{ ...CHAT, title: 'First chat ' }} onDone={onDone} />);
    await user.type(box(), '{Backspace}{Enter}');
    expect(renameMock).toHaveBeenCalledWith({ id: 'c1', title: 'First chat' });
  });

  // An input drops a typed or pasted newline, but not every line break Unicode has.
  it('keeps a title of more than one line marked, the reason in view, for correcting', () => {
    render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    fireEvent.change(box(), { target: { value: 'ops\u2028west' } });
    fireEvent.keyDown(box(), { key: 'Enter' });
    expect(renameMock).not.toHaveBeenCalled();
    expect(onDone).not.toHaveBeenCalled();
    expect(box()).toHaveAttribute('aria-invalid', 'true');
    expect(box()).toHaveAccessibleDescription('A chat title must be one line.');

    fireEvent.change(box(), { target: { value: 'ops west' } });
    expect(box()).not.toHaveAttribute('aria-invalid');
    fireEvent.keyDown(box(), { key: 'Enter' });
    expect(renameMock).toHaveBeenCalledWith({ id: 'c1', title: 'ops west' });
  });

  it('writes nothing for a draft that only respaced the title, though it has changed since', () => {
    const { rerender } = render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    fireEvent.change(box(), { target: { value: 'First chat  ' } });
    rerender(<ChatRenameBox chat={{ ...CHAT, title: 'Renamed elsewhere' }} onDone={onDone} />);
    fireEvent.keyDown(box(), { key: 'Enter' });
    expect(renameMock).not.toHaveBeenCalled();
    expect(onDone).toHaveBeenCalledWith('keyboard');
  });

  // Two windows renamed it alike: a second write would move the chat up Recents again.
  it('writes nothing for a title the chat already has', () => {
    const { rerender } = render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    rerender(<ChatRenameBox chat={{ ...CHAT, title: 'Beta' }} onDone={onDone} />);
    fireEvent.change(box(), { target: { value: ' Beta ' } });
    fireEvent.keyDown(box(), { key: 'Enter' });
    expect(renameMock).not.toHaveBeenCalled();
    expect(onDone).toHaveBeenCalledWith('keyboard');
  });

  it('trims and counts a title as the sidecar does', () => {
    render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    fireEvent.change(box(), { target: { value: `\u0085${'a'.repeat(80)}\u3000` } });
    fireEvent.keyDown(box(), { key: 'Enter' });
    expect(renameMock).toHaveBeenCalledWith({ id: 'c1', title: 'a'.repeat(80) });
  });

  it('keeps a too-long title marked, the reason in view, until it is short enough', () => {
    render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    fireEvent.change(box(), { target: { value: 'a'.repeat(82) } });
    expect(box()).not.toHaveAttribute('aria-invalid');
    fireEvent.keyDown(box(), { key: 'Enter' });
    const reason = screen.getByRole('alert');
    expect(reason).toHaveTextContent('A chat title can be at most 80 characters.');
    expect(reason).not.toHaveClass('sr-only');
    // As wide as the box, wrapping in the 200px sidebar, and brought into view under the
    // list's last row.
    expect(reason).toHaveClass('absolute', 'top-full', 'inset-x-0');
    expect(reason).not.toHaveClass('whitespace-nowrap');
    expect(scrollIntoView).toHaveBeenCalledWith({ block: 'nearest' });

    fireEvent.change(box(), { target: { value: 'a'.repeat(81) } });
    expect(box()).toHaveAttribute('aria-invalid', 'true');
    expect(box()).toHaveAccessibleDescription('A chat title can be at most 80 characters.');

    fireEvent.change(box(), { target: { value: 'a'.repeat(80) } });
    expect(box()).not.toHaveAttribute('aria-invalid');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('keeps a title the sidecar would refuse as too long, marked, for correcting', () => {
    render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    // 80 code points of astral characters are 160 UTF-16 units: the limit counts the former.
    fireEvent.change(box(), { target: { value: `  ${'😀'.repeat(80)}x  ` } });
    fireEvent.keyDown(box(), { key: 'Enter' });
    expect(renameMock).not.toHaveBeenCalled();
    expect(onDone).not.toHaveBeenCalled();
    expect(box()).toHaveValue(`  ${'😀'.repeat(80)}x  `);
    expect(box()).toHaveAttribute('aria-invalid', 'true');
    expect(box()).toHaveAccessibleDescription('A chat title can be at most 80 characters.');

    // Corrected, the mark goes, and Enter renames.
    fireEvent.change(box(), { target: { value: `  ${'😀'.repeat(80)}  ` } });
    expect(box()).not.toHaveAttribute('aria-invalid');
    expect(box()).not.toHaveAccessibleDescription();
    fireEvent.keyDown(box(), { key: 'Enter' });
    expect(renameMock).toHaveBeenCalledWith({ id: 'c1', title: '😀'.repeat(80) });
    expect(onDone).toHaveBeenCalledWith('keyboard');
  });

  it('cancels on Escape or a blur, writing nothing', async () => {
    const user = userEvent.setup();
    render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    await user.type(box(), 'x{Escape}');
    expect(onDone).toHaveBeenCalledOnce();
    await user.tab();
    expect(onDone).toHaveBeenCalledTimes(2);
    expect(renameMock).not.toHaveBeenCalled();
  });

  // In the narrow drawer, an Escape that reached `window` would close the drawer as well.
  it('keeps its Escape from reaching the window', () => {
    const reached = vi.fn();
    window.addEventListener('keydown', reached);
    render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    fireEvent.keyDown(box(), { key: 'Escape' });
    window.removeEventListener('keydown', reached);
    expect(onDone).toHaveBeenCalledWith('keyboard');
    expect(reached).not.toHaveBeenCalled();
  });

  it('leaves Enter and Escape to an IME while it composes', () => {
    render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    fireEvent.change(box(), { target: { value: 'にほ' } });
    fireEvent.keyDown(box(), { key: 'Enter', isComposing: true });
    fireEvent.keyDown(box(), { key: 'Escape', isComposing: true });
    expect(box()).toHaveValue('にほ');
    expect(onDone).not.toHaveBeenCalled();
    expect(renameMock).not.toHaveBeenCalled();
  });

  it('leaves WebKit’s Enter after a composition to the IME, and saves on the next', () => {
    render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    fireEvent.change(box(), { target: { value: '日本' } });
    // The candidate's Enter, sent after `compositionend`: not composing, but keyCode 229.
    fireEvent.keyDown(box(), { key: 'Enter', keyCode: 229 });
    expect(onDone).not.toHaveBeenCalled();
    expect(renameMock).not.toHaveBeenCalled();

    fireEvent.keyDown(box(), { key: 'Enter', keyCode: 13 });
    expect(renameMock).toHaveBeenCalledWith({ id: 'c1', title: '日本' });
    expect(onDone).toHaveBeenCalledWith('keyboard');
  });

  it('says whether a key or a blur closed it', async () => {
    const user = userEvent.setup();
    render(<ChatRenameBox chat={CHAT} onDone={onDone} />);
    await user.keyboard('{Escape}');
    expect(onDone).toHaveBeenLastCalledWith('keyboard');
    await user.tab();
    expect(onDone).toHaveBeenLastCalledWith('blur');
  });

  it('takes a caller’s class', () => {
    render(<ChatRenameBox chat={CHAT} onDone={onDone} className="h-7" />);
    expect(box()).toHaveClass('h-7');
  });
});
