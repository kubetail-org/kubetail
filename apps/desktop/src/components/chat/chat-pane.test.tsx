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
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { useChatsMock, useChatMessagesMock } = vi.hoisted(() => ({
  useChatsMock: vi.fn(),
  useChatMessagesMock: vi.fn(),
}));
vi.mock('@/lib/chats', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/chats')>()),
  useChats: useChatsMock,
  useChatMessages: useChatMessagesMock,
}));

// The window's cluster, with the clusters watch's phase: what a new chat is filed
// under, and what the composer waits on before it can start one. The clusters
// themselves are what the out-of-scope notice looks a chat's cluster up in.
const { useActiveClusterMock, useClustersMock } = vi.hoisted(() => ({
  useActiveClusterMock: vi.fn(),
  useClustersMock: vi.fn(),
}));
vi.mock('@/lib/active-cluster', () => ({ useActiveCluster: useActiveClusterMock }));
vi.mock('@/lib/clusters', () => ({ useClusters: useClustersMock }));

// The picker's contexts are what the out-of-scope button is gated on — a cluster the
// picker does not offer is one `setContext` could not switch to.
const { setContext, kubeContexts } = vi.hoisted(() => ({
  setContext: vi.fn(),
  kubeContexts: { current: [] as { name: string }[] },
}));
vi.mock('@/lib/active-kube-context', () => ({
  useActiveKubeContext: () => ({ contexts: kubeContexts.current, setContext }),
}));
// Whether the machine offers a sandbox, which the composer's switch and the
// transcript's headings both follow.
const { useSandboxMock, sandboxRetry, switchState, networkSwitchState } = vi.hoisted(() => ({
  useSandboxMock: vi.fn(),
  sandboxRetry: vi.fn(),
  switchState: { switching: false, setSandboxDisabled: vi.fn() },
  networkSwitchState: { switching: false, setNetworkEnabled: vi.fn() },
}));
vi.mock('@/lib/sandbox', () => ({ useSandbox: useSandboxMock }));
vi.mock('@/lib/sandbox-switch', () => ({ useSandboxSwitch: () => switchState }));
vi.mock('@/lib/network-switch', () => ({ useNetworkSwitch: () => networkSwitchState }));
vi.mock('urql', () => ({ useMutation: () => [{}, vi.fn()] }));
vi.mock('@/gql', () => ({ graphql: () => ({}) }));

// Both have suites of their own; here they report what they were handed. The
// composer reads the real outbox, as it does in the app, so a draft moving between
// chats is observable through it.
vi.mock('@/components/chat/chat-composer', async () => {
  const { useChatOutbox } = await import('@/lib/chat-outbox');
  return {
    ChatComposer: (props: {
      chatID: string | null;
      mode: 'chat' | 'dashboard';
      clusterID?: string;
      onShowWaiting?: () => void;
    }) => {
      const { chatID, mode, clusterID, onShowWaiting } = props;
      const { draft, setDraft } = useChatOutbox(mode, chatID, clusterID);
      return (
        <div data-testid="composer">
          {JSON.stringify(props)}
          <input aria-label="draft" value={draft} onChange={(e) => setDraft(e.target.value)} />
          {onShowWaiting && (
            <button type="button" onClick={onShowWaiting}>
              show
            </button>
          )}
        </div>
      );
    },
  };
});
vi.mock('@/components/chat/chat-transcript/transcript', () => ({
  // The footer is the composer, drawn as the transcript draws it, under the props.
  ChatTranscript: ({ footer, ...props }: { footer?: ReactNode }) => (
    <>
      <div data-testid="transcript">{JSON.stringify(props)}</div>
      {footer}
    </>
  ),
}));

// The title bar has its own suite; here it says which chat it was given.
vi.mock('./chat-header', () => ({
  ChatHeader: ({ chat }: { chat?: { title: string } }) => <div data-testid="chat-header">{chat?.title}</div>,
}));

const { ChatOutboxProvider } = await import('@/lib/chat-outbox');
const { ChatPane, NewChatPane } = await import('./chat-pane');

const chat = (id: string, clusterID = '1', sandboxDisabled = false, networkEnabled = false) => ({
  id,
  title: 'A chat',
  mode: 'Chat',
  clusterID,
  sandboxDisabled,
  networkEnabled,
  createdAt: '2026-09-01T00:00:00Z',
  updatedAt: '2026-09-01T10:00:00Z',
});

const message = (over: Record<string, unknown> = {}) => ({
  id: 'm1',
  chatID: 'c1',
  seq: 1,
  role: 'Assistant',
  content: [{ type: 'text', text: 'hi' }],
  status: 'Complete',
  ...over,
});

const onGone = vi.fn();
const onCreated = vi.fn();

type Watches = {
  chats?: ReturnType<typeof chat>[];
  listPhase?: string;
  messages?: ReturnType<typeof message>[];
  messagesPhase?: string;
};

function setWatches({
  chats = [chat('c1')],
  listPhase = 'live',
  messages = [message()],
  messagesPhase = 'live',
}: Watches) {
  useChatsMock.mockReturnValue({ chats, phase: listPhase });
  useChatMessagesMock.mockReturnValue({ messages, phase: messagesPhase });
}

// One provider across rerenders, as the layout's is, so a draft can be followed
// from one pane to another.
function renderPanes() {
  const view = render(<ChatOutboxProvider>{null}</ChatOutboxProvider>);
  return {
    open: (chatID: string, watches: Watches = {}) => {
      setWatches(watches);
      // A fresh callback each time: the watches are mocks, so nothing else makes a
      // pane re-read them when it is reopened on the same chat.
      view.rerender(
        <ChatOutboxProvider>
          <ChatPane chatID={chatID} mode="chat" onGone={() => onGone()} />
        </ChatOutboxProvider>,
      );
    },
    fresh: () =>
      view.rerender(
        <ChatOutboxProvider>
          <NewChatPane mode="chat" empty={<p>Ask about your cluster.</p>} onCreated={onCreated} />
        </ChatOutboxProvider>,
      ),
  };
}

// A cluster record as the window's watch serves it, for the button that follows a
// chat to the cluster it belongs to.
const cluster = (id: string, context?: string) => ({
  id,
  spec: { source: { kubeconfig: context ? { context } : null } },
});

const props = (testid: string) => JSON.parse(screen.getByTestId(testid).textContent ?? '');
const draftBox = () => screen.getByRole('textbox', { name: 'draft' });

beforeEach(() => {
  vi.clearAllMocks();
  useActiveClusterMock.mockReturnValue({ clusterID: '1', phase: 'live' });
  useClustersMock.mockReturnValue({ clusters: [] });
  useSandboxMock.mockReturnValue({
    available: true,
    networkAvailable: true,
    networkReason: '',
    failed: false,
    retry: sandboxRetry,
  });
  switchState.switching = false;
  networkSwitchState.switching = false;
  kubeContexts.current = [{ name: 'prod' }, { name: 'staging' }];
});

describe('NewChatPane', () => {
  it('opens no watch at all, and fills the space above the composer', () => {
    renderPanes().fresh();

    expect(useChatsMock).not.toHaveBeenCalled();
    expect(useChatMessagesMock).not.toHaveBeenCalled();
    expect(screen.getByText('Ask about your cluster.')).toBeInTheDocument();
    expect(props('composer')).toMatchObject({ chatID: null, mode: 'chat', clusterID: '1', last: null });
  });

  it('draws no title bar, since there is no chat yet', () => {
    renderPanes().fresh();
    expect(screen.queryByTestId('chat-header')).not.toBeInTheDocument();
  });

  it('draws the empty slot and the composer as one group when centred', () => {
    const pane = (centered: boolean) => (
      <ChatOutboxProvider>
        <NewChatPane mode="chat" centered={centered} empty={<p>Ask about your cluster.</p>} onCreated={onCreated} />
      </ChatOutboxProvider>
    );
    const slotParent = () => screen.getByText('Ask about your cluster.').parentElement;
    const composerParent = () => screen.getByTestId('composer').parentElement;

    // At the bottom, the slot fills the space above the composer.
    const view = render(pane(false));
    expect(slotParent()).not.toBe(composerParent());
    view.rerender(pane(true));
    expect(slotParent()).toBe(composerParent());
  });

  // The clusters watch is what it waits on: with no snapshot yet there is no answer
  // to "is there a cluster to file this under?", so the composer stays closed.
  it('hands the composer the clusters watch’s phase, not a hardcoded one', () => {
    useActiveClusterMock.mockReturnValue({ clusterID: undefined, phase: 'connecting' });
    renderPanes().fresh();

    expect(props('composer').phase).toBe('connecting');
  });

  // A chat that has not started has the toggle, which needs the machine's word.
  it("hands the composer the machine's sandbox and network", () => {
    useSandboxMock.mockReturnValue({
      available: true,
      networkAvailable: false,
      networkReason: 'pasta not found',
      failed: false,
      retry: sandboxRetry,
    });
    renderPanes().fresh();

    expect(props('composer')).toMatchObject({
      sandboxAvailable: true,
      networkAvailable: false,
      networkReason: 'pasta not found',
    });
  });
});

describe('ChatPane', () => {
  // The chat's switch rides the list watch, so the composer draws what every
  // window sees; the machine's sandbox is what the transcript's headings follow.
  // A send reserves its turn under the switch as it is, so both ways of sending
  // wait for a switch in flight.
  it('hands both the switch in flight', () => {
    switchState.switching = true;
    renderPanes().open('c1', { chats: [chat('c1')] });
    expect(props('composer')).toMatchObject({ switching: true });
    expect(props('transcript')).toMatchObject({ switching: true });
  });

  // The query re-runs for nobody, so a sidecar unreachable when the pane opened
  // would leave the switch hidden until it remounts.
  it('asks for the sandbox again once the watch is live', () => {
    useSandboxMock.mockReturnValue({ available: undefined, failed: true, retry: sandboxRetry });
    const panes = renderPanes();
    panes.open('c1', { chats: [chat('c1')], messagesPhase: 'reconnecting' });
    expect(sandboxRetry).not.toHaveBeenCalled();
    panes.open('c1', { chats: [chat('c1')], messagesPhase: 'live' });
    expect(sandboxRetry).toHaveBeenCalledTimes(1);
  });

  // Either switch in flight holds both ways of sending.
  it('hands both a network switch in flight', () => {
    networkSwitchState.switching = true;
    renderPanes().open('c1', { chats: [chat('c1')] });
    expect(props('composer')).toMatchObject({ switching: true });
    expect(props('transcript')).toMatchObject({ switching: true });
  });

  it("hands the composer the chat's network switch and the machine's network, and Ask again the switch", () => {
    renderPanes().open('c1', { chats: [chat('c1', '1', false, true)] });
    expect(props('composer')).toMatchObject({ networkAvailable: true, networkReason: '', networkEnabled: true });
    expect(props('transcript')).toMatchObject({ networkEnabled: true });
  });

  it("hands the composer the chat's switch and both the machine's sandbox", () => {
    const panes = renderPanes();
    panes.open('c1', { chats: [chat('c1', '1', true)] });
    expect(props('composer')).toMatchObject({ sandboxAvailable: true, sandboxDisabled: true });
    // Ask again sends it as what the user saw.
    expect(props('transcript')).toMatchObject({ sandboxAvailable: true, sandboxDisabled: true });

    // Before the list answers, the composer holds no switch to draw.
    panes.open('c1', { chats: [], listPhase: 'connecting' });
    expect(props('composer').sandboxDisabled).toBeUndefined();

    useSandboxMock.mockReturnValue({ available: undefined, failed: false, retry: sandboxRetry });
    panes.open('c1', { chats: [chat('c1')] });
    expect(props('composer').sandboxAvailable).toBeUndefined();
    expect(props('transcript').sandboxAvailable).toBeUndefined();
  });

  it('draws the open chat and hands the composer its last message', () => {
    renderPanes().open('c1', { messages: [message(), message({ id: 'm2', seq: 2, status: 'Streaming' })] });

    expect(useChatMessagesMock).toHaveBeenCalledWith('c1');
    expect(props('composer')).toMatchObject({ chatID: 'c1', last: { seq: 2, status: 'Streaming' } });
  });

  it('points the composer to a request waiting in an earlier message, and scrolls to it', async () => {
    const waitingCall = (id: string) => ({
      id,
      status: 'AwaitingApproval',
      approval: { id, status: 'Pending', duration: null },
      sandboxActions: [],
    });
    renderPanes().open('c1', {
      messages: [
        message({ awaitingApproval: true, toolCalls: [waitingCall('ap-2'), waitingCall('ap-1')] }),
        message({ id: 'm2', seq: 2, toolCalls: [] }),
      ],
    });
    const request = document.createElement('div');
    request.id = 'approval-ap-1';
    request.scrollIntoView = vi.fn();
    document.body.append(request);

    await userEvent.click(screen.getByRole('button', { name: 'show' }));

    expect(request.scrollIntoView).toHaveBeenCalled();
    request.remove();
  });

  it('points to no request the last message holds, which the transcript follows itself', () => {
    renderPanes().open('c1', {
      messages: [message({ awaitingApproval: true, toolCalls: [] })],
    });

    expect(screen.queryByRole('button', { name: 'show' })).toBeNull();
  });

  it('keeps each chat’s draft to itself', async () => {
    const panes = renderPanes();
    panes.open('c1');
    await userEvent.type(draftBox(), 'for c1');

    panes.open('c2', { chats: [chat('c2')] });
    expect(draftBox()).toHaveValue('');

    panes.open('c1');
    expect(draftBox()).toHaveValue('for c1');
  });

  it('says a chat is deleted once the list has been listed without it', () => {
    renderPanes().open('c1', { chats: [], messages: [] });

    expect(screen.getByText('This chat was deleted.')).toBeInTheDocument();
    expect(screen.queryByTestId('transcript')).not.toBeInTheDocument();
  });

  it('waits for the list’s snapshot before calling a chat deleted', () => {
    renderPanes().open('c1', { chats: [], listPhase: 'connecting', messages: [] });

    expect(screen.queryByText('This chat was deleted.')).not.toBeInTheDocument();
    expect(props('transcript').phase).toBe('connecting');
  });

  // A chat belongs to a cluster: a deep link, Back, or a cluster switch in chat mode
  // can leave the window on one cluster with another's chat open. The chat is still
  // there, so the way out is to follow it — not to start a new one.
  it('says a chat belongs to another cluster, and follows it there', async () => {
    useClustersMock.mockReturnValue({ clusters: [cluster('2', 'staging')] });
    renderPanes().open('c1', { chats: [chat('c1', '2')] });

    expect(screen.getByText('This chat belongs to another cluster.')).toBeInTheDocument();
    expect(screen.queryByTestId('transcript')).not.toBeInTheDocument();
    expect(screen.queryByTestId('composer')).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Switch to it' }));
    expect(setContext).toHaveBeenCalledWith('staging');
  });

  // A chat is filed under a cluster, so an unanswered clusters watch makes every chat
  // look like another cluster's — the pane waits for that snapshot exactly as it waits
  // for the list's before calling a chat deleted.
  it('waits for the clusters snapshot before calling a chat out of scope', () => {
    useActiveClusterMock.mockReturnValue({ clusterID: undefined, phase: 'connecting' });
    renderPanes().open('c1', { chats: [chat('c1', '2')] });

    expect(screen.queryByText('This chat belongs to another cluster.')).not.toBeInTheDocument();
    expect(props('transcript').phase).toBe('connecting');
  });

  // A disabled cluster, or one the kubeconfig no longer declares, keeps the context
  // name it was imported under — but the picker does not offer it, so the switch
  // would write a param that yields to the default and leave this notice up.
  it('offers no way back to a cluster the picker does not offer', () => {
    kubeContexts.current = [{ name: 'prod' }];
    useClustersMock.mockReturnValue({ clusters: [cluster('2', 'staging')] });
    renderPanes().open('c1', { chats: [chat('c1', '2')] });

    expect(screen.getByText('This chat belongs to another cluster.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Switch to it' })).not.toBeInTheDocument();
  });

  // Nothing to switch to: the record is gone, or it is not a kubeconfig cluster.
  it('offers no way back to a cluster it cannot name', () => {
    useClustersMock.mockReturnValue({ clusters: [cluster('2')] });
    renderPanes().open('c1', { chats: [chat('c1', '2')] });

    expect(screen.getByText('This chat belongs to another cluster.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Switch to it' })).not.toBeInTheDocument();
  });

  it('draws a chat of the window’s own cluster', () => {
    renderPanes().open('c1', { chats: [chat('c1', '1')] });

    expect(screen.queryByText('This chat belongs to another cluster.')).not.toBeInTheDocument();
    expect(screen.getByTestId('transcript')).toBeInTheDocument();
  });

  // A button rather than a link: the pane does not know where "new" is. The draft
  // goes along, since a chat that no longer exists has no send to carry it.
  it('leaves a deleted chat for the unstarted one, draft in hand', async () => {
    const panes = renderPanes();
    panes.open('c1');
    await userEvent.type(draftBox(), 'half a question');

    panes.open('c1', { chats: [], messages: [] });
    await userEvent.click(screen.getByRole('button', { name: 'Start a new one' }));
    expect(onGone).toHaveBeenCalled();

    panes.fresh();
    expect(draftBox()).toHaveValue('half a question');
  });

  it('draws the chat’s title bar over the transcript in chat mode', () => {
    const { open } = renderPanes();
    open('c1');
    const header = screen.getByTestId('chat-header');
    expect(header).toHaveTextContent('A chat');
    expect(
      header.compareDocumentPosition(screen.getByTestId('transcript')) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  // The bar holds an open rename's draft and a delete's confirm: it stays mounted, so they
  // ride a reconnect out.
  it.each([
    // `useChats` holds the chat through its replay.
    ['chats', { listPhase: 'connecting' }, 'live'],
    ['clusters', {}, 'connecting'],
  ])('keeps the same title bar through a reconnect of the %s watch', (_, watches, clusterPhase) => {
    const { open } = renderPanes();
    open('c1');
    const header = screen.getByTestId('chat-header');

    useActiveClusterMock.mockReturnValue({ clusterID: clusterPhase === 'live' ? '1' : undefined, phase: clusterPhase });
    open('c1', watches);
    expect(screen.getByTestId('chat-header')).toBe(header);
    expect(header).toHaveTextContent('A chat');

    useActiveClusterMock.mockReturnValue({ clusterID: '1', phase: 'live' });
    open('c1');
    expect(screen.getByTestId('chat-header')).toBe(header);
  });

  it('leaves for the out-of-scope notice once the clusters watch settles on another cluster', () => {
    const { open } = renderPanes();
    open('c1');
    useActiveClusterMock.mockReturnValue({ clusterID: '2', phase: 'live' });
    open('c1', { listPhase: 'connecting' });
    expect(screen.queryByTestId('chat-header')).not.toBeInTheDocument();
    expect(screen.getByText('This chat belongs to another cluster.')).toBeInTheDocument();
  });

  // Its Rename and Delete would reach a chat that may be another cluster's.
  it('draws no title bar before the clusters snapshot', () => {
    useActiveClusterMock.mockReturnValue({ clusterID: undefined, phase: 'connecting' });
    renderPanes().open('c1');
    expect(screen.queryByTestId('chat-header')).not.toBeInTheDocument();
  });

  // A link to /chat/$chatId can carry a dashboard chat's id; the chat list would not list it.
  it('draws no title bar over another mode’s chat', () => {
    renderPanes().open('c1', { chats: [{ ...chat('c1'), mode: 'Dashboard' }] });
    expect(screen.queryByTestId('chat-header')).not.toBeInTheDocument();
  });

  it('draws no title bar in the dashboard’s panel', () => {
    setWatches({});
    render(
      <ChatOutboxProvider>
        <ChatPane chatID="c1" mode="dashboard" onGone={() => onGone()} />
      </ChatOutboxProvider>,
    );
    expect(screen.queryByTestId('chat-header')).not.toBeInTheDocument();
  });
});
