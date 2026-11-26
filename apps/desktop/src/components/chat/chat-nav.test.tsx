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

import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { expectCodeText, expectProseText } from '@/test-utils';

const { useChatsMock, renameMock, deleteMock } = vi.hoisted(() => ({
  useChatsMock: vi.fn(),
  renameMock: vi.fn(),
  deleteMock: vi.fn(),
}));

vi.mock('@/lib/chats', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/chats')>()),
  useChats: useChatsMock,
}));
vi.mock('urql', () => ({
  useMutation: (doc: { kind?: string }) => [{ fetching: false }, doc.kind === 'delete' ? deleteMock : renameMock],
}));
vi.mock('@/gql', () => ({
  graphql: (source: string) => ({ kind: source.includes('chatDelete') ? 'delete' : 'rename' }),
}));
// The window's cluster: the list is the active cluster's chats and nobody else's.
const { useActiveClusterMock } = vi.hoisted(() => ({ useActiveClusterMock: vi.fn() }));
vi.mock('@/lib/active-cluster', () => ({ useActiveCluster: useActiveClusterMock }));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ to, params, search, children, ...rest }: Record<string, unknown> & { children: React.ReactNode }) => {
    const path =
      typeof to === 'string' && params ? to.replace('$chatId', (params as { chatId: string }).chatId) : String(to);
    // A dashboard row writes its search with an updater, so call it the way the
    // router would — over a stand-in previous search — and put the result on the href.
    const next = typeof search === 'function' ? (search as (p: unknown) => Record<string, unknown>)(PREV_SEARCH) : null;
    return (
      <a href={next ? `${path}?${new URLSearchParams(next as Record<string, string>)}` : path} {...rest}>
        {children}
      </a>
    );
  },
}));

// What a row's `search` updater is handed: the rest of the window's scope, which it
// must carry rather than replace.
const PREV_SEARCH = { kubeContext: 'prod', resource: 'pods' };

const { ChatNav } = await import('./chat-nav');

const chat = (id: string, title: string, mode = 'Chat', clusterID = '1') => ({
  id,
  title,
  mode,
  clusterID,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T10:00:00Z',
  awaitingApproval: false,
  sandboxDisabled: false,
});

function renderNav(chats = [chat('c1', 'First chat')], phase = 'live', mode: 'chat' | 'dashboard' = 'chat') {
  useChatsMock.mockReturnValue({ chats, phase });
  return render(<ChatNav mode={mode} />);
}

const menuFor = async (user: ReturnType<typeof userEvent.setup>, title: string) => {
  await user.click(screen.getByRole('button', { name: `Actions for ${title}` }));
};

beforeEach(() => {
  vi.clearAllMocks();
  useActiveClusterMock.mockReturnValue({ clusterID: '1', phase: 'live' });
  renameMock.mockResolvedValue({ data: { chatRename: chat('c1', 'renamed') } });
  deleteMock.mockResolvedValue({ data: { chatDelete: true } });
});

describe('ChatNav', () => {
  it('keeps its rows when the layout draws it again', () => {
    const { rerender } = renderNav();
    rerender(<ChatNav mode="chat" />);
    expect(screen.getByRole('link', { name: 'First chat' })).toHaveAttribute('href', '/chat/c1');
  });

  it('offers a new chat without writing one', () => {
    renderNav();
    expect(screen.getByRole('link', { name: /new chat/i })).toHaveAttribute('href', '/chat');
  });

  it('draws a border around New chat', () => {
    renderNav();
    const link = screen.getByRole('link', { name: /new chat/i });
    expect(link).toHaveClass('border-border');
    expect(link).not.toHaveClass('border-transparent');
  });

  it('waits for the snapshot before saying there are no chats', () => {
    renderNav([], 'connecting');
    expect(screen.queryByText(/No chats yet/)).not.toBeInTheDocument();
  });

  it('says there are no chats once the snapshot has closed', () => {
    renderNav([], 'live');
    expect(screen.getByText(/No chats yet/)).toBeInTheDocument();
    expect(screen.getByText(/Your chats will show up here/)).toBeInTheDocument();
  });

  // The panel has no New chat link to point at: its composer is right there.
  it('keeps the dashboard’s empty state to one line', () => {
    renderNav([], 'live', 'dashboard');
    expect(screen.getByText(/No chats yet/)).toBeInTheDocument();
    expect(screen.queryByText(/Your chats will show up here/)).not.toBeInTheDocument();
  });

  it('lists the chats under one Recents heading, and drops it with no chats', () => {
    const view = renderNav([chat('c1', 'First chat'), chat('c2', 'Second chat')]);
    expect(screen.getAllByRole('heading')).toHaveLength(1);
    expect(screen.getByRole('heading', { name: 'Recents' })).toBeInTheDocument();

    useChatsMock.mockReturnValue({ chats: [], phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.queryByRole('heading', { name: 'Recents' })).not.toBeInTheDocument();
  });

  it('has 8px under the search box, as over it, before Recents', () => {
    renderNav([chat('c1', 'First chat')]);
    expect(screen.getByRole('link', { name: 'New chat' })).toHaveClass('mb-2');
    expect(screen.getByRole('searchbox', { name: 'Search chats' }).closest('.sticky')).toHaveClass('pb-2');
    expect(screen.getByRole('heading', { name: 'Recents' })).toHaveClass('mt-0');
  });

  it('narrows the rows to the titles the search matches, ignoring case', async () => {
    const user = userEvent.setup();
    renderNav([chat('c1', 'Why is checkout crashing?'), chat('c2', 'Scale down staging')]);
    const box = screen.getByRole('searchbox', { name: 'Search chats' });
    expectCodeText(box);

    await user.type(box, '  CHECKOUT ');
    expect(screen.getByRole('link', { name: 'Why is checkout crashing?' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Scale down staging' })).not.toBeInTheDocument();

    await user.keyboard('{Escape}');
    expect(box).toHaveValue('');
    expect(screen.getByRole('link', { name: 'Scale down staging' })).toBeInTheDocument();
  });

  // In the narrow drawer, an Escape that reached `window` would close it too.
  it('keeps the Escape that clears its query from the window, and lets the next one through', () => {
    const reached = vi.fn();
    window.addEventListener('keydown', reached);
    renderNav();
    const box = screen.getByRole('searchbox', { name: 'Search chats' });
    fireEvent.change(box, { target: { value: 'first' } });
    fireEvent.keyDown(box, { key: 'Escape' });
    expect(box).toHaveValue('');
    expect(reached).not.toHaveBeenCalled();
    fireEvent.keyDown(box, { key: 'Escape' });
    window.removeEventListener('keydown', reached);
    expect(reached).toHaveBeenCalledOnce();
  });

  it('leaves the query to an IME’s Escape', () => {
    renderNav();
    const box = screen.getByRole('searchbox', { name: 'Search chats' });
    fireEvent.change(box, { target: { value: 'first' } });
    fireEvent.keyDown(box, { key: 'Escape', isComposing: true });
    expect(box).toHaveValue('first');
  });

  it.each([
    ['stored', 'first\u200bchat'],
    ['drawn', 'first\\u{200b}chat'],
  ])('finds a spelled title by its %s text', (_, query) => {
    renderNav([chat('c1', 'First\u200bchat'), chat('c2', 'Second chat')]);
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search chats' }), { target: { value: query } });
    expect(screen.getByRole('link', { name: 'First\\u{200B}chat' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Second chat' })).not.toBeInTheDocument();
  });

  it('says so when no title matches, in place of the heading', async () => {
    const user = userEvent.setup();
    renderNav([chat('c1', 'First chat')]);
    await user.type(screen.getByRole('searchbox', { name: 'Search chats' }), 'nothing');

    expect(screen.getByText('No chats match.')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Recents' })).not.toBeInTheDocument();
    expect(screen.queryByText(/No chats yet/)).not.toBeInTheDocument();
  });

  it('offers a search only in chat mode, and only with chats to search', () => {
    const { unmount } = renderNav([], 'live');
    expect(screen.queryByRole('searchbox')).not.toBeInTheDocument();
    unmount();

    renderNav([chat('c1', 'A dashboard chat', 'Dashboard')], 'live', 'dashboard');
    expect(screen.queryByRole('searchbox')).not.toBeInTheDocument();
  });

  it('names a chat with no title Untitled chat, on its row and its actions', () => {
    renderNav([chat('c1', '')]);
    expect(screen.getByRole('link', { name: 'Untitled chat' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Actions for Untitled chat' })).toBeInTheDocument();
  });

  it('finds a chat with no title by the name its row draws', () => {
    renderNav([chat('c1', ''), chat('c2', 'Second chat')]);
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search chats' }), { target: { value: 'untitled' } });
    expect(screen.getByRole('link', { name: 'Untitled chat' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Second chat' })).not.toBeInTheDocument();
  });

  it('links each chat to its own route', () => {
    renderNav([chat('c1', 'First chat'), chat('c2', 'Second chat')]);
    expect(screen.getByRole('link', { name: 'First chat' })).toHaveAttribute('href', '/chat/c1');
    expect(screen.getByRole('link', { name: 'Second chat' })).toHaveAttribute('href', '/chat/c2');
  });

  // As the title bar and the delete confirm spell it, so the three can be matched.
  it('draws each title through VisibleText', () => {
    renderNav([chat('c1', 'First\u200bchat')]);
    expect(screen.getByRole('link', { name: 'First\\u{200B}chat' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Actions for First\\u{200B}chat' })).toBeInTheDocument();
  });

  it('marks a chat a request is waiting in, on either mode', () => {
    (['chat', 'dashboard'] as const).forEach((mode) => {
      const kind = mode === 'chat' ? 'Chat' : 'Dashboard';
      const { unmount } = renderNav(
        [{ ...chat('c1', 'First chat', kind), awaitingApproval: true }, chat('c2', 'Second chat', kind)],
        'live',
        mode,
      );
      const first = screen.getByRole('link', { name: /First chat/ });
      expect(within(first).getByRole('img', { name: 'Waiting on you' })).toBeInTheDocument();
      expect(screen.getAllByRole('img', { name: 'Waiting on you' })).toHaveLength(1);
      unmount();
    });
  });

  it('lists only the chats of its own mode', () => {
    renderNav([chat('c1', 'A chat chat'), chat('c2', 'A dashboard chat', 'Dashboard')]);
    expect(screen.getByRole('link', { name: 'A chat chat' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'A dashboard chat' })).not.toBeInTheDocument();

    screen.getByRole('link', { name: /new chat/i });
  });

  // A chat belongs to one cluster, so the list is the window's cluster's — switching
  // cluster switches the rows, and a window on no cluster lists none.
  it('lists only the active cluster’s chats', () => {
    const view = renderNav([chat('c1', 'On prod'), chat('c2', 'On staging', 'Chat', '2')]);
    expect(screen.getByRole('link', { name: 'On prod' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'On staging' })).not.toBeInTheDocument();

    useActiveClusterMock.mockReturnValue({ clusterID: '2', phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.getByRole('link', { name: 'On staging' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'On prod' })).not.toBeInTheDocument();

    useActiveClusterMock.mockReturnValue({ clusterID: undefined, phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.getByText(/No chats yet/)).toBeInTheDocument();
  });

  // The rows are the window's cluster's, so a chats snapshot that lands first has
  // every row filtered out — an empty state there would be about a cluster the
  // window has not been told it is on.
  it('waits for the clusters snapshot too before saying there are no chats', () => {
    useActiveClusterMock.mockReturnValue({ clusterID: undefined, phase: 'connecting' });
    renderNav([chat('c1', 'First chat')], 'live');

    expect(screen.queryByText(/No chats yet/)).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'First chat' })).not.toBeInTheDocument();
  });

  // The spinner gates on the unfiltered phase; the empty text on what is left after
  // the filter, so a mode with none of its own says so even when the other has some.
  it('says there are no chats when only the other mode has any', () => {
    renderNav([chat('c1', 'A chat chat')], 'live', 'dashboard');
    expect(screen.getByText(/No chats yet/)).toBeInTheDocument();
  });

  // On the dashboard a chat is a search param, not a route, and the rest of the
  // window's scope has to survive the click.
  it('points a dashboard row at the panel, keeping the rest of the search', () => {
    renderNav([chat('c2', 'A dashboard chat', 'Dashboard')], 'live', 'dashboard');

    const href = screen.getByRole('link', { name: 'A dashboard chat' }).getAttribute('href')!;
    const [path, query] = href.split('?');
    expect(path).toBe('/dashboard');
    expect(Object.fromEntries(new URLSearchParams(query))).toEqual({ ...PREV_SEARCH, chat: 'c2' });
  });

  // The panel's header is where a dashboard chat starts — by typing in the composer.
  it('offers no new chat on the dashboard', () => {
    renderNav([], 'live', 'dashboard');
    expect(screen.queryByRole('link', { name: /new chat/i })).not.toBeInTheDocument();
  });

  it('renames in place, committing on Enter', async () => {
    const user = userEvent.setup();
    renderNav();
    await menuFor(user, 'First chat');
    await user.click(await screen.findByRole('menuitem', { name: /rename/i }));

    const input = screen.getByRole('textbox', { name: /chat title/i });
    expectProseText(input);
    await user.clear(input);
    await user.type(input, 'Renamed{Enter}');
    expect(renameMock).toHaveBeenCalledWith({ id: 'c1', title: 'Renamed' });
  });

  it('cancels a rename on Escape, writing nothing', async () => {
    const user = userEvent.setup();
    renderNav();
    await menuFor(user, 'First chat');
    await user.click(await screen.findByRole('menuitem', { name: /rename/i }));

    await user.type(screen.getByRole('textbox', { name: /chat title/i }), 'Renamed{Escape}');
    expect(renameMock).not.toHaveBeenCalled();
    expect(screen.getByRole('link', { name: 'First chat' })).toBeInTheDocument();
  });

  // The input the keyboard was in goes away; the drawer's trap would send the next Tab to
  // its first control.
  it.each(['{Enter}', '{Escape}'])('hands focus to the row’s actions after %s ends a rename', async (key) => {
    const user = userEvent.setup();
    renderNav([chat('c1', 'First chat'), chat('c2', 'Second chat')]);
    await menuFor(user, 'Second chat');
    await user.click(await screen.findByRole('menuitem', { name: /rename/i }));
    await user.keyboard(key);
    expect(screen.getByRole('button', { name: 'Actions for Second chat' })).toHaveFocus();
  });

  it('leaves focus where the user moved it when a rename ends on a blur', async () => {
    const user = userEvent.setup();
    renderNav([chat('c1', 'First chat'), chat('c2', 'Second chat')]);
    await menuFor(user, 'Second chat');
    await user.click(await screen.findByRole('menuitem', { name: /rename/i }));
    await user.click(screen.getByRole('searchbox', { name: 'Search chats' }));
    expect(screen.getByRole('searchbox', { name: 'Search chats' })).toHaveFocus();
    // A later redraw doesn't pull it back either.
    await user.type(screen.getByRole('searchbox', { name: 'Search chats' }), 's');
    expect(screen.getByRole('searchbox', { name: 'Search chats' })).toHaveFocus();
  });

  it('ends a rename whose row the search no longer matches', async () => {
    const user = userEvent.setup();
    const { rerender } = renderNav([chat('c1', 'Alpha'), chat('c2', 'Gamma')]);
    const search = screen.getByRole('searchbox', { name: 'Search chats' });
    fireEvent.change(search, { target: { value: 'alpha' } });
    await menuFor(user, 'Alpha');
    await user.click(await screen.findByRole('menuitem', { name: /rename/i }));
    expect(screen.getByRole('textbox', { name: /chat title/i })).toBeInTheDocument();

    // Renamed in another window: the search drops the row, and its focused box with it.
    useChatsMock.mockReturnValue({ chats: [chat('c1', 'Beta'), chat('c2', 'Gamma')], phase: 'live' });
    rerender(<ChatNav mode="chat" />);
    expect(search).toHaveFocus();
    fireEvent.change(search, { target: { value: '' } });
    expect(screen.queryByRole('textbox', { name: /chat title/i })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Beta' })).toBeInTheDocument();
    expect(search).toHaveFocus();
  });

  it('leaves the row as it was when a rename fails', async () => {
    renameMock.mockResolvedValue({ error: new Error('nope') });
    const user = userEvent.setup();
    renderNav();
    await menuFor(user, 'First chat');
    await user.click(await screen.findByRole('menuitem', { name: /rename/i }));
    await user.type(screen.getByRole('textbox', { name: /chat title/i }), '{Enter}');

    // The row is the watch's, so a refused rename simply leaves it alone.
    expect(await screen.findByRole('link', { name: 'First chat' })).toBeInTheDocument();
  });

  it('asks before deleting, since deletion is instant and permanent', async () => {
    const user = userEvent.setup();
    renderNav();
    await menuFor(user, 'First chat');
    await user.click(await screen.findByRole('menuitem', { name: /delete/i }));

    expect(deleteMock).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Delete chat' }));
    expect(deleteMock).toHaveBeenCalledWith({ id: 'c1' });
  });

  it('writes nothing when the confirm is dismissed', async () => {
    const user = userEvent.setup();
    renderNav();
    await menuFor(user, 'First chat');
    await user.click(await screen.findByRole('menuitem', { name: /delete/i }));
    await user.click(screen.getByRole('button', { name: 'Keep chat' }));

    expect(deleteMock).not.toHaveBeenCalled();
  });

  it('writes nothing when the confirm is closed with Escape', async () => {
    const user = userEvent.setup();
    renderNav();
    await menuFor(user, 'First chat');
    await user.click(await screen.findByRole('menuitem', { name: /delete/i }));
    await user.keyboard('{Escape}');

    expect(screen.queryByText('Delete chat?')).not.toBeInTheDocument();
    expect(deleteMock).not.toHaveBeenCalled();
  });

  it('names the chat as another window renamed it while the confirm is open', async () => {
    const user = userEvent.setup();
    const view = renderNav();
    await menuFor(user, 'First chat');
    await user.click(await screen.findByRole('menuitem', { name: /delete/i }));

    useChatsMock.mockReturnValue({ chats: [chat('c1', 'Renamed chat')], phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.getByRole('dialog')).toHaveTextContent('Renamed chat');
  });

  it('closes the confirm once the snapshot shows another window deleted the chat', async () => {
    const user = userEvent.setup();
    const view = renderNav([chat('c1', 'First chat'), chat('c2', 'Second chat')]);
    await menuFor(user, 'First chat');
    await user.click(await screen.findByRole('menuitem', { name: /delete/i }));

    useChatsMock.mockReturnValue({ chats: [chat('c2', 'Second chat')], phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());

    // Back again, the chat brings no confirm with it: the one asked about is gone.
    useChatsMock.mockReturnValue({ chats: [chat('c1', 'First chat'), chat('c2', 'Second chat')], phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  // `useChats` holds the rows through a replay, taking each record as it lands.
  it('keeps the confirm through a replay, naming the chat as its record lands', async () => {
    const user = userEvent.setup();
    const view = renderNav();
    await menuFor(user, 'First chat');
    await user.click(await screen.findByRole('menuitem', { name: /delete/i }));

    const dialog = screen.getByRole('dialog');
    useChatsMock.mockReturnValue({ chats: [chat('c1', 'First chat')], phase: 'connecting' });
    view.rerender(<ChatNav mode="chat" />);
    expect(dialog).toHaveTextContent('First chat');
    useChatsMock.mockReturnValue({ chats: [chat('c1', 'Renamed chat')], phase: 'connecting' });
    view.rerender(<ChatNav mode="chat" />);
    expect(dialog).toHaveTextContent('Renamed chat');
    useChatsMock.mockReturnValue({ chats: [chat('c1', 'Renamed chat')], phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    // The same dialog, never closed and opened again.
    expect(screen.getByRole('dialog')).toBe(dialog);
  });

  it('moves focus to the row taking the place of a focused row that leaves', () => {
    const chats = [chat('c1', 'First chat'), chat('c2', 'Second chat'), chat('c3', 'Third chat')];
    const view = renderNav(chats);
    screen.getByRole('button', { name: 'Actions for Second chat' }).focus();

    useChatsMock.mockReturnValue({ chats: [chats[0], chats[2]], phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.getByRole('link', { name: 'Third chat' })).toHaveFocus();
  });

  it('moves focus to the last row when the focused last row leaves', () => {
    const chats = [chat('c1', 'First chat'), chat('c2', 'Second chat')];
    const view = renderNav(chats);
    screen.getByRole('link', { name: 'Second chat' }).focus();

    useChatsMock.mockReturnValue({ chats: [chats[0]], phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.getByRole('link', { name: 'First chat' })).toHaveFocus();
  });

  it('moves focus to the search box when a filtered rename leaves no row', async () => {
    const user = userEvent.setup();
    const view = renderNav([chat('c1', 'Alpha'), chat('c2', 'Gamma')]);
    await user.type(screen.getByRole('searchbox', { name: 'Search chats' }), 'alpha');
    await menuFor(user, 'Alpha');
    await user.click(await screen.findByRole('menuitem', { name: /rename/i }));
    await user.clear(screen.getByRole('textbox', { name: /chat title/i }));
    await user.type(screen.getByRole('textbox', { name: /chat title/i }), 'Beta{Enter}');
    expect(await screen.findByRole('button', { name: 'Actions for Alpha' })).toHaveFocus();

    useChatsMock.mockReturnValue({ chats: [chat('c1', 'Beta'), chat('c2', 'Gamma')], phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.getByRole('searchbox', { name: 'Search chats' })).toHaveFocus();
  });

  it('moves focus to the next row when another window deletes the row being renamed', async () => {
    const user = userEvent.setup();
    const view = renderNav([chat('c1', 'First chat'), chat('c2', 'Second chat')]);
    await menuFor(user, 'First chat');
    await user.click(await screen.findByRole('menuitem', { name: /rename/i }));
    expect(screen.getByRole('textbox', { name: /chat title/i })).toHaveFocus();

    useChatsMock.mockReturnValue({ chats: [chat('c2', 'Second chat')], phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.getByRole('link', { name: 'Second chat' })).toHaveFocus();
  });

  it('moves focus to New chat when the focused last chat leaves', () => {
    const view = renderNav([chat('c1', 'First chat')]);
    screen.getByRole('link', { name: 'First chat' }).focus();

    useChatsMock.mockReturnValue({ chats: [], phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.getByRole('link', { name: 'New chat' })).toHaveFocus();
  });

  it.each([
    [
      'chats',
      () =>
        useChatsMock.mockReturnValue({
          chats: [chat('c1', 'First chat'), chat('c2', 'Second chat')],
          phase: 'connecting',
        }),
    ],
    ['clusters', () => useActiveClusterMock.mockReturnValue({ clusterID: undefined, phase: 'connecting' })],
  ])('keeps the rows and a rename’s draft through a reconnect of the %s watch', async (_, reconnect) => {
    const user = userEvent.setup();
    const view = renderNav([chat('c1', 'First chat'), chat('c2', 'Second chat')]);
    await menuFor(user, 'First chat');
    await user.click(await screen.findByRole('menuitem', { name: /rename/i }));
    await user.type(screen.getByRole('textbox', { name: /chat title/i }), ' draft');

    reconnect();
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.getByRole('textbox', { name: /chat title/i })).toHaveValue('First chat draft');
    expect(screen.getByRole('textbox', { name: /chat title/i })).toHaveFocus();
    expect(screen.getByRole('link', { name: 'Second chat' })).toBeInTheDocument();

    useChatsMock.mockReturnValue({ chats: [chat('c1', 'First chat'), chat('c2', 'Second chat')], phase: 'live' });
    useActiveClusterMock.mockReturnValue({ clusterID: '1', phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.getByRole('textbox', { name: /chat title/i })).toHaveValue('First chat draft');
  });

  it.each(['rename', 'delete'])(
    'drops the old cluster’s rows and an open %s once the clusters watch settles on another',
    async (action) => {
      const user = userEvent.setup();
      const view = renderNav([chat('c1', 'First chat'), chat('c2', 'Other cluster’s chat', 'Chat', '2')]);
      await menuFor(user, 'First chat');
      await user.click(await screen.findByRole('menuitem', { name: new RegExp(action, 'i') }));

      // The chats watch still replays when the window moves to cluster 2.
      useChatsMock.mockReturnValue({
        chats: [chat('c1', 'First chat'), chat('c2', 'Other cluster’s chat', 'Chat', '2')],
        phase: 'connecting',
      });
      useActiveClusterMock.mockReturnValue({ clusterID: '2', phase: 'live' });
      view.rerender(<ChatNav mode="chat" />);
      expect(screen.queryByRole('link', { name: 'First chat' })).not.toBeInTheDocument();
      expect(screen.queryByRole('textbox', { name: /chat title/i })).not.toBeInTheDocument();
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
      expect(screen.getByRole('link', { name: 'Other cluster’s chat' })).toBeInTheDocument();
    },
  );

  it('moves focus to New chat when the last chat goes while the search box holds it', () => {
    const view = renderNav([chat('c1', 'First chat')]);
    screen.getByRole('searchbox', { name: 'Search chats' }).focus();

    useChatsMock.mockReturnValue({ chats: [], phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.getByRole('link', { name: 'New chat' })).toHaveFocus();
  });

  it('moves focus to the caller’s fallback when the focused last row leaves the dashboard’s list', () => {
    const fallback = { current: null as HTMLButtonElement | null };
    useChatsMock.mockReturnValue({ chats: [chat('c1', 'First chat', 'Dashboard')], phase: 'live' });
    const view = render(
      <>
        <ChatNav mode="dashboard" focusFallback={fallback} />
        <button ref={fallback} type="button">
          Composer
        </button>
      </>,
    );
    screen.getByRole('link', { name: 'First chat' }).focus();

    useChatsMock.mockReturnValue({ chats: [], phase: 'live' });
    view.rerender(
      <>
        <ChatNav mode="dashboard" focusFallback={fallback} />
        <button ref={fallback} type="button">
          Composer
        </button>
      </>,
    );
    expect(screen.getByRole('button', { name: 'Composer' })).toHaveFocus();
  });

  it('leaves focus alone when a row leaves without it', () => {
    const chats = [chat('c1', 'First chat'), chat('c2', 'Second chat')];
    const view = renderNav(chats);
    screen.getByRole('link', { name: 'New chat' }).focus();

    useChatsMock.mockReturnValue({ chats: [chats[0]], phase: 'live' });
    view.rerender(<ChatNav mode="chat" />);
    expect(screen.getByRole('link', { name: 'New chat' })).toHaveFocus();
  });

  it('leaves the row in the list when a delete fails', async () => {
    deleteMock.mockResolvedValue({ error: new Error('nope') });
    const user = userEvent.setup();
    renderNav();
    await menuFor(user, 'First chat');
    await user.click(await screen.findByRole('menuitem', { name: /delete/i }));
    await user.click(screen.getByRole('button', { name: 'Delete chat' }));

    expect(await screen.findByRole('link', { name: 'First chat' })).toBeInTheDocument();
  });
});
