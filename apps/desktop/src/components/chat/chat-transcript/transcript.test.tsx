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

import { act, fireEvent, render, screen, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { ChatClusterRequest, ChatMessage, ChatSandboxAction, ChatToolCall } from '@/lib/chats';
import type { FetchQuery } from '@/lib/log-viewer';
import { fakeLogWindows } from '@/test-utils';

// The seams: urql's hooks, and the client a view's window reads through, which reads
// nothing and says what it was asked. The outbox is the real one, since Ask again is its
// operation, and so is the catalog the label reads.
const { sendMock, catalog, fetched, markdownRenders, openUsage } = vi.hoisted(() => ({
  sendMock: vi.fn(),
  openUsage: vi.fn(),
  catalog: { current: {} },
  fetched: [] as FetchQuery[],
  markdownRenders: [] as string[],
}));
vi.mock('urql', () => ({
  useMutation: () => [{}, sendMock],
  useQuery: () => [catalog.current, vi.fn()],
  useClient: () => ({}),
}));
// The chat's folders arrive on a watch, which answers nothing here.
vi.mock('@/lib/graphql/use-folded-subscription', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/graphql/use-folded-subscription')>()),
  useFoldedSubscription: () => ({ data: undefined, connected: true }),
}));
vi.mock('@/lib/connectors', () => ({ CHATGPT_ID: 'chatgpt', useOpenChatGPTUsage: () => openUsage }));
vi.mock('@/lib/models', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/models')>()),
  // The catalog's watch, as the stub's last frame: none until it holds models.
  useModels: () => {
    const { data } = catalog.current as { data?: { models: import('@/lib/models').Model[] } };
    return { models: data?.models ?? [], snapshotComplete: data !== undefined };
  },
}));
// The real Markdown, under a spy that notes the text of each render.
vi.mock('@/components/primitives/markdown', async (importOriginal) => {
  const { Markdown } = await importOriginal<typeof import('@/components/primitives/markdown')>();
  return {
    Markdown: ({ text }: { text: string }) => {
      markdownRenders.push(text);
      return <Markdown text={text} />;
    },
  };
});
vi.mock('@/lib/logs-client', () => ({
  createLogsClient: () => ({
    fetch: (query: FetchQuery) => {
      fetched.push(query);
      return new Promise(() => {});
    },
    tail: () => () => {},
  }),
}));

const { ChatOutboxProvider } = await import('@/lib/chat-outbox');
const { LogsViewProvider, useLogsView } = await import('@/lib/logs-view');
const { RightSidebarProvider } = await import('@/lib/right-sidebar');
const { LogWindowsContext } = await import('@/lib/log-windows');

// The app window has no log windows here; the cards' own suites cover theirs.
const { value: noLogWindows } = fakeLogWindows();
const { APPROVE_ARM_MS, ChatTranscript, viewEnds } = await import('./transcript');

const opus = {
  provider: { id: 'anthropic', label: 'Anthropic' },
  id: 'claude-opus-5',
  label: 'Opus 5',
  efforts: ['low', 'high'],
  defaultEffort: 'high',
};
const fake = {
  provider: { id: 'fake', label: 'Fake' },
  id: 'fake',
  label: 'Fake model',
  efforts: ['low', 'high'],
  defaultEffort: 'high',
};

beforeEach(() => {
  vi.clearAllMocks();
  fetched.length = 0;
  markdownRenders.length = 0;
  catalog.current = { fetching: false, data: { models: [fake, opus] } };
  sendMock.mockResolvedValue({ data: { chatSend: { id: 'm9', chatID: 'c1', seq: 9, status: 'Streaming' } } });
});

// The providers the layout mounts around the transcript: the outbox Ask again
// sends from, the focused view the card sets, and the log windows it reads. One
// wrapper for every tree, so a rerender keeps the transcript mounted.
const wrap = (ui: ReactNode, mode: 'chat' | 'dashboard' = 'chat') => (
  <RightSidebarProvider mode={mode}>
    <LogsViewProvider>
      <LogWindowsContext.Provider value={noLogWindows}>
        <ChatOutboxProvider>{ui}</ChatOutboxProvider>
      </LogWindowsContext.Provider>
    </LogsViewProvider>
  </RightSidebarProvider>
);

// The answer as it grows, for the pinning tests.
const grown = () =>
  wrap(
    <ChatTranscript
      messages={[msg({ status: 'Streaming', content: [{ type: 'text', text: 'more' }] })]}
      phase="live"
      mode="chat"
      chatID="c1"
      clusterID="1"
    />,
  );

// The transcript reads the chat's outbox, so it renders inside the provider the
// layout mounts. Approve arms at once unless a case asks for the real wait, which
// only the cases about arming do.
// On a machine with no sandbox unless a test says otherwise: today's headings.
const draw = (
  messages: ChatMessage[],
  props: {
    phase?: 'connecting' | 'live' | 'reconnecting';
    approveArmMs?: number;
    sandboxAvailable?: boolean;
    sandboxDisabled?: boolean;
    networkEnabled?: boolean;
    switching?: boolean;
    mode?: 'chat' | 'dashboard';
  } = {
    sandboxAvailable: false,
  },
) =>
  render(
    wrap(
      <ChatTranscript
        messages={messages}
        phase={props.phase ?? 'live'}
        mode={props.mode ?? 'chat'}
        chatID="c1"
        clusterID="1"
        approveArmMs={props.approveArmMs ?? 0}
        sandboxAvailable={'sandboxAvailable' in props ? props.sandboxAvailable : false}
        sandboxDisabled={'sandboxDisabled' in props ? props.sandboxDisabled : false}
        networkEnabled={'networkEnabled' in props ? props.networkEnabled : false}
        switching={props.switching}
      />,
      props.mode ?? 'chat',
    ),
  );

function msg(over: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: 'm1',
    chatID: 'c1',
    seq: 1,
    role: 'Assistant',
    createdAt: '2026-11-15T10:00:00Z',
    finishedAt: null,
    content: [{ type: 'text', text: 'the answer' }],
    thinking: '',
    status: 'Complete',
    // What the sidecar serves for an answer whose own run waits; a case with an
    // agent waiting under a settled answer sets it.
    awaitingApproval: over.status === 'WaitingApproval',
    error: '',
    failure: null,
    finishReason: 'end_turn',
    model: 'fake',
    provider: { id: 'fake', label: 'Fake' },
    effort: 'high',
    toolCalls: [],
    citations: [],
    ...over,
  };
}

// One of an answer's bash calls, as the row carries it: waiting on the user
// unless a case says otherwise.
function call(over: Partial<ChatToolCall> = {}): ChatToolCall {
  return {
    id: 'tc1',
    name: 'bash',
    actionKind: 'Command',
    status: 'AwaitingApproval',
    runsOn: 'Sidecar',
    action: {
      description: '',
      command: {
        text: 'wc -l ~/.kube/config',
        cwd: '/Users/ana',
        background: false,
        sandboxed: false,
        network: false,
        env: [],
      },
      read: null,
      write: null,
      edit: null,
      search: null,
      fetch: null,
      memory: null,
      delegate: null,
      kubeQuery: null,
      logsView: null,
      logsRead: null,
      logsWatch: null,
    },
    approval: { id: 'ap-1', status: 'Pending', duration: null },
    network: null,
    output: '',
    background: null,
    agentCallID: null,
    sandboxActions: [],
    ...over,
  };
}

// A call the user has decided on, and whatever became of it after.
const decided = (over: Partial<ChatToolCall>, approval: 'Approved' | 'Denied' = 'Approved') =>
  call({
    action: {
      description: '',
      command: {
        text: 'wc -l ~/.kube/config',
        cwd: '/Users/ana',
        background: false,
        sandboxed: false,
        network: false,
        env: [],
      },
      read: null,
      write: null,
      edit: null,
      search: null,
      fetch: null,
      memory: null,
      delegate: null,
      kubeQuery: null,
      logsView: null,
      logsRead: null,
      logsWatch: null,
    },
    approval: { id: 'ap-1', status: approval, duration: null },
    ...over,
  });

// jsdom lays nothing out: give the scroller a viewport and a content height so the
// pinning rule has numbers to work with.
function sizeScroller(el: HTMLElement, { scrollHeight = 1000, clientHeight = 400 } = {}) {
  Object.defineProperty(el, 'scrollHeight', { configurable: true, value: scrollHeight });
  Object.defineProperty(el, 'clientHeight', { configurable: true, value: clientHeight });
}

const scroller = () => screen.getByTestId('chat-transcript');

// A LogsView call that opened a view.
const logsViewCall = (over: Partial<ChatToolCall> = {}) =>
  call({
    id: 'lv-1',
    name: 'LogsView',
    actionKind: 'LogsView',
    status: 'Succeeded',
    approval: null,
    output: 'Viewing deployments/webapp in prod at the newest line.',
    action: {
      description: '',
      command: null,
      read: null,
      write: null,
      edit: null,
      search: null,
      fetch: null,
      memory: null,
      delegate: null,
      kubeQuery: null,
      logsView: {
        sources: [
          {
            namespace: 'prod',
            kind: 'Deployment',
            name: 'webapp',
            containers: ['app'],
            allContainers: false,
            previous: false,
          },
        ],
        filters: [],
        grep: '',
        anchor: { kind: 'Tail', at: null },
        pinToEnd: false,
        resolvedAt: '2026-10-08T14:04:00Z',
      },
      logsRead: null,
      logsWatch: null,
    },
    ...over,
  });

// A pinned tail view, resolved at `resolvedAt`.
const pinnedView = (id: string, resolvedAt: string) => {
  const opened = logsViewCall().action!;
  return logsViewCall({ id, action: { ...opened, logsView: { ...opened.logsView!, pinToEnd: true, resolvedAt } } });
};
const T1 = '2026-10-08T14:01:00Z';
const T2 = '2026-10-08T14:02:00Z';
const T3 = '2026-10-08T14:03:00Z';

describe('viewEnds', () => {
  it('ends each view at the next one’s moment, and the chat’s newest nowhere', () => {
    const ends = viewEnds([
      msg({ id: 'm1', seq: 1, toolCalls: [pinnedView('a', T1)] }),
      msg({ id: 'm2', seq: 2, content: [{ type: 'text', text: 'again' }], role: 'User', toolCalls: [] }),
      msg({ id: 'm3', seq: 3, toolCalls: [pinnedView('b', T2), pinnedView('c', T3)] }),
    ]);
    expect(ends).toEqual(
      new Map([
        ['a', T2],
        ['b', T3],
        ['c', null],
      ]),
    );
  });

  it('passes over a refused newer call', () => {
    const refused = logsViewCall({ id: 'r', status: 'Failed', action: null, output: '{"error":"not-found"}' });
    expect(viewEnds([msg({ toolCalls: [pinnedView('a', T1), refused] })])).toEqual(new Map([['a', null]]));
  });
});

describe('ChatTranscript', () => {
  describe('while an answer streams', () => {
    const answer = (id: string, seq: number, text: string, over: Partial<ChatMessage> = {}) =>
      msg({ id, seq, content: [{ type: 'text', text }], ...over });
    const transcript = (messages: ChatMessage[]) =>
      wrap(<ChatTranscript messages={messages} phase="live" mode="chat" chatID="c1" clusterID="1" />);

    it('renders the streaming message alone on each frame', () => {
      const finished = [answer('m1', 1, 'first'), answer('m2', 2, 'second')];
      const { rerender } = render(transcript([...finished, answer('m3', 3, 'Hel', { status: 'Streaming' })]));
      expect(markdownRenders).toEqual(['first', 'second', 'Hel']);

      rerender(transcript([...finished, answer('m3', 3, 'Hello', { status: 'Streaming' })]));
      expect(markdownRenders).toEqual(['first', 'second', 'Hel', 'Hello']);
    });

    // The rows' `ends` must not go stale.
    it('ends an earlier view where a later message opens the next one', () => {
      const first = answer('m1', 1, 'first', { toolCalls: [pinnedView('a', T1)] });
      const { rerender } = render(transcript([first]));
      expect(fetched).toEqual([{ kind: 'Last', limit: 8 }]);

      // The new view runs live; the earlier one reads again, up to the new one's moment.
      fetched.length = 0;
      rerender(transcript([first, answer('m2', 2, 'second', { toolCalls: [pinnedView('b', T2)] })]));
      expect(fetched).toEqual([
        { kind: 'At', at: T2, limit: 8 },
        { kind: 'Last', limit: 8 },
      ]);
    });
  });

  it('runs only the newest view live, and reads the older one up to the newer one’s moment', () => {
    draw([msg({ status: 'Complete', toolCalls: [pinnedView('a', T1), pinnedView('b', T2)] })]);
    expect(fetched).toEqual([
      { kind: 'At', at: T2, limit: 8 },
      { kind: 'Last', limit: 8 },
    ]);
  });

  it('draws a LogsView call that opened as a card with Expand, and one refused as Logs', () => {
    draw([
      msg({
        status: 'Complete',
        toolCalls: [
          logsViewCall(),
          logsViewCall({ id: 'lv-2', status: 'Failed', action: null, output: '{"error":"not-found","source":0}' }),
        ],
      }),
    ]);

    expect(screen.getByText(/Logs: Deployment webapp in prod/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Expand' })).toBeInTheDocument();
    expect(screen.getByText('Logs')).toBeInTheDocument();
    expect(screen.getByText('error')).toBeInTheDocument();
  });

  it('draws a LogsRead call that read as its card, and one refused as Read logs', () => {
    const opened = logsViewCall().action!;
    const read = logsViewCall({
      id: 'lr-1',
      name: 'LogsRead',
      actionKind: 'LogsRead',
      output: 'Read 1 line',
      action: {
        ...opened,
        logsView: null,
        logsRead: {
          sources: opened.logsView!.sources,
          filters: [],
          grep: '',
          resolvedAt: '2026-10-17T10:00:00Z',
          since: '2026-10-17T09:13:00Z',
          until: null,
          before: { lines: 0, span: '' },
          after: { lines: 0, span: '' },
          pageFrom: 'First',
          lines: [
            {
              timestamp: '2026-10-17T09:14:00Z',
              namespace: 'prod',
              pod: 'webapp-a',
              container: 'app',
              previous: false,
              message: 'panic: nil map',
              truncatedBytes: 0,
              gap: null,
            },
          ],
          olderMore: true,
          newerMore: false,
          notices: [],
          viewAround: null,
        },
      },
    });
    const refused = logsViewCall({
      id: 'lr-2',
      name: 'LogsRead',
      actionKind: 'LogsRead',
      status: 'Failed',
      action: null,
      output: '{"error":"timed-out"}',
    });
    draw([msg({ status: 'Complete', toolCalls: [read, refused] })]);

    expect(screen.getByText(/Read: Deployment webapp in prod/)).toBeInTheDocument();
    expect(screen.getByText('panic: nil map')).toBeInTheDocument();
    expect(screen.getByText('Read logs')).toBeInTheDocument();
    expect(fetched).toEqual([]);
  });

  it('draws a LogsWatch call that started as its card, with Stop while it runs', () => {
    const opened = logsViewCall().action!;
    const logsWatch = {
      sources: opened.logsView!.sources,
      filters: [],
      grep: 'panic',
      since: '2026-10-18T13:50:00Z',
      limit: 1,
      maxBytes: 16384,
      maxMessageBytes: 4096,
      resolvedAt: '2026-10-18T13:50:00Z',
      until: '2026-10-19T13:50:00Z',
    };
    const task = {
      status: 'Running' as const,
      exitCode: null,
      report: '',
      matched: [],
      endedBecause: '',
      viewAround: null,
    };
    const watching = logsViewCall({
      id: 'lw-1',
      name: 'LogsWatch',
      actionKind: 'LogsWatch',
      output: 'Watching',
      action: { ...opened, logsView: null, logsWatch },
      background: task,
    });
    const { unmount } = draw([msg({ status: 'Complete', toolCalls: [watching] })]);
    expect(screen.getByText(/Watch: Deployment webapp in prod/)).toBeInTheDocument();
    expect(screen.getByText('watching…')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Stop' })).toBeInTheDocument();
    unmount();

    const line = {
      timestamp: '2026-10-18T13:51:00Z',
      namespace: 'prod',
      pod: 'webapp-a',
      container: 'app',
      previous: false,
      message: 'panic: nil map',
      truncatedBytes: 0,
      gap: null,
    };
    draw([
      msg({
        status: 'Complete',
        toolCalls: [
          {
            ...watching,
            background: { ...task, status: 'Matched', matched: [line], endedBecause: 'it reached its line limit' },
          },
        ],
      }),
    ]);
    expect(screen.getByText('panic: nil map')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Stop' })).toBeNull();
    expect(fetched).toEqual([]);
  });

  it('draws the card without Expand on the dashboard', () => {
    draw([msg({ status: 'Complete', toolCalls: [logsViewCall()] })], { mode: 'dashboard' });

    expect(screen.getByText(/Logs: Deployment webapp in prod/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Expand' })).not.toBeInTheDocument();
  });

  it('waits for the snapshot before saying a chat is empty', () => {
    draw([], { phase: 'connecting' });
    expect(screen.queryByText('No messages yet.')).not.toBeInTheDocument();
    expect(screen.getByText('Loading messages…')).toBeInTheDocument();
  });

  it('keeps waiting while other props change before the snapshot', () => {
    const { rerender } = draw([], { phase: 'connecting' });
    rerender(
      wrap(<ChatTranscript messages={[]} phase="connecting" mode="chat" chatID="c1" clusterID="1" sandboxAvailable />),
    );
    expect(screen.getByText('Loading messages…')).toBeInTheDocument();
  });

  it('shows the empty state once the snapshot has closed', () => {
    draw([]);
    expect(screen.getByText('No messages yet.')).toBeInTheDocument();
  });

  it('draws the text of a complete message', () => {
    draw([msg()]);
    expect(screen.getByText('the answer')).toBeInTheDocument();
  });

  it('skips a complete message with nothing to say about itself, whatever its role', () => {
    draw([
      msg({
        id: 'm1',
        role: 'User',
        content: [{ type: 'tool_result', tool_use_id: 't1', content: 'rows' }],
        finishReason: '',
      }),
      msg({ id: 'm2', seq: 2, content: [{ type: 'thinking', thinking: 'hmm' }], finishReason: '' }),
    ]);
    expect(screen.getByText('No messages yet.')).toBeInTheDocument();
  });

  it('draws a streaming answer with its partial text', () => {
    draw([msg({ status: 'Streaming', content: [{ type: 'text', text: 'Hel' }] })]);
    expect(screen.getByText('Hel')).toBeInTheDocument();
    expect(screen.getByLabelText('Answering')).toBeInTheDocument();
  });

  it('draws an empty failed answer for its error alone', () => {
    draw([msg({ status: 'Failed', content: [], error: 'context window exceeded' })]);
    expect(screen.getByText('context window exceeded')).toBeInTheDocument();
  });

  // An answer that failed on the ChatGPT plan offers its usage settings; at a
  // limit it says whose the limit may be.
  it('offers Manage usage under an answer that failed on the ChatGPT plan', () => {
    const plan = { id: 'chatgpt', label: 'ChatGPT plan' };
    draw([msg({ status: 'Failed', content: [], error: 'usage limit reached', failure: 'UsageLimit', provider: plan })]);

    expect(screen.getByText("The limit may be your plan's or the one set for Kubetail.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Manage usage' }));
    expect(openUsage).toHaveBeenCalledOnce();
  });

  it('says nothing of a limit under another failure on the plan, nor on another provider', () => {
    const plan = { id: 'chatgpt', label: 'ChatGPT plan' };
    draw([
      msg({ id: 'a', status: 'Failed', content: [], error: 'usage limit reached', failure: 'Other', provider: plan }),
      msg({ id: 'b', seq: 2, status: 'Failed', content: [], error: 'usage limit reached', failure: 'UsageLimit' }),
    ]);

    expect(screen.getAllByRole('button', { name: 'Manage usage' })).toHaveLength(1);
    expect(screen.queryByText("The limit may be your plan's or the one set for Kubetail.")).toBeNull();
  });

  it('marks a cancelled answer as stopped, keeping the partial text', () => {
    draw([msg({ status: 'Cancelled', content: [{ type: 'text', text: 'half an ans' }] })]);
    expect(screen.getByText('half an ans')).toBeInTheDocument();
    expect(screen.getByText('Stopped')).toBeInTheDocument();
  });

  it('renders text as text, never as markup', () => {
    draw([msg({ content: [{ type: 'text', text: '<b>not bold</b>' }] })]);
    expect(screen.getByText('<b>not bold</b>')).toBeInTheDocument();
    expect(document.querySelector('b')).toBeNull();
  });

  // With a real provider an empty complete answer is one cut off by its cap while
  // still reasoning: the user would see their question and silence.
  it('draws an empty complete answer as the reason it stopped', () => {
    draw([msg({ content: [{ type: 'thinking', thinking: 'hmm' }], finishReason: 'max_tokens' })]);
    expect(screen.getByText('Stopped: max_tokens')).toBeInTheDocument();
  });

  // A turn that spent its tool budget and asked again settles Complete on tool_use,
  // and its earlier rounds may hold text that would otherwise read as the answer.
  it('draws a complete answer that stopped on tool_use as stopped, whatever its text', () => {
    draw([msg({ content: [{ type: 'text', text: 'Let me check the other namespaces.' }], finishReason: 'tool_use' })]);
    expect(screen.getByText('Let me check the other namespaces.')).toBeInTheDocument();
    expect(screen.getByText('Stopped: tool_use')).toBeInTheDocument();
  });

  // A turn that ran out of resumes settles Complete on pause_turn with whatever
  // text its rounds hold, which would otherwise read as a finished answer.
  it('says an answer stopped on pause_turn even when it has text', () => {
    draw([msg({ content: [{ type: 'text', text: 'So far, 1.34 removed' }], finishReason: 'pause_turn' })]);
    expect(screen.getByText('So far, 1.34 removed')).toBeInTheDocument();
    expect(screen.getByText('Stopped: pause_turn')).toBeInTheDocument();
  });

  // The sources say where an answer drawn from the web came from; the queries
  // are what left the machine, so they are drawn as soon as they are seen —
  // while the answer streams and on one that stopped — and never as markup.
  describe('sources and searches', () => {
    // A search the provider ran, as its row carries it.
    const providerSearch = (query: string) =>
      call({
        id: `srch-${query}`,
        name: 'anthropic_web_search_20260318',
        actionKind: 'Search',
        status: null,
        runsOn: 'Provider',
        action: {
          description: '',
          command: null,
          read: null,
          write: null,
          edit: null,
          search: { query },
          fetch: null,
          memory: null,
          delegate: null,
          kubeQuery: null,
          logsView: null,
          logsRead: null,
          logsWatch: null,
        },
        approval: null,
      });
    const cite = (url: string, title: string) => ({ type: 'web_search_result_location', url, title, citedText: '' });
    const searched = (over: Partial<ChatMessage> = {}) =>
      msg({
        content: [
          { type: 'server_use', id: 'srv_1', name: 'anthropic_web_search_20260318', input: {} },
          { type: 'text', text: '1.34 removed it.' },
        ],
        toolCalls: [providerSearch('k8s 1.34 deprecations')],
        citations: [cite('https://k8s.io/1.34', 'Kubernetes 1.34'), cite('https://k8s.io/blog', 'The <b>blog</b>')],
        ...over,
      });

    it("draws an answer's sources and its queries", () => {
      draw([searched()]);
      expect(screen.getByText('Sources')).toBeInTheDocument();
      const source = screen.getByText('Kubernetes 1.34');
      expect(source.closest('li')).toHaveAttribute('title', 'https://k8s.io/1.34');
      expect(source.closest('li')).toHaveTextContent('Kubernetes 1.34 · k8s.io');
      expect(screen.getByText('The <b>blog</b>')).toBeInTheDocument();
      const disclosure = screen.getByText('Searched the web · 1').closest('details')!;
      expect(disclosure.open).toBe(false);
      expect(disclosure).toHaveTextContent('k8s 1.34 deprecations');
    });

    // The provider's call is drawn by its queries alone: no disclosure of its
    // own, as the sidecar's calls have.
    it('draws no call disclosure for a provider search', () => {
      draw([searched()]);
      expect(screen.queryByText(/web_search/)).toBeNull();
      expect(document.querySelectorAll('details'), 'the search disclosure alone').toHaveLength(1);
    });

    // A URL is web text; one that does not parse draws its title alone.
    it('draws the title alone for a source whose url has no host', () => {
      draw([msg({ citations: [cite('not a url', 'Odd')] })]);
      expect(screen.getByText('Odd').closest('li')).toHaveTextContent(/^Odd$/);
    });

    it('draws neither under an answer with neither', () => {
      draw([msg()]);
      expect(screen.queryByText('Sources')).toBeNull();
      expect(screen.queryByText(/^Searched the web/)).toBeNull();
    });

    it('draws the queries while the answer streams, before any source', () => {
      draw([searched({ status: 'Streaming', citations: [] })]);
      expect(screen.getByText('Searched the web · 1')).toBeInTheDocument();
      expect(screen.queryByText('Sources')).toBeNull();
    });

    // The kind is the tool's, so a search whose arguments did not parse is still
    // counted; with no query to show, the line opens onto nothing.
    it('counts a search with no action, as a plain line', () => {
      draw([searched({ toolCalls: [{ ...providerSearch(''), action: null }] })]);
      const line = screen.getByText('Searched the web · 1');
      expect(line.closest('details')).toBeNull();
    });

    // A search the sidecar ran is counted beside the provider's, however it was
    // offered, and drawn by its query alone.
    it('counts a succeeded sidecar search, with no disclosure of its own', () => {
      const mine = {
        ...providerSearch('mine'),
        id: 'mine',
        name: 'Search',
        runsOn: 'Sidecar' as const,
        status: 'Succeeded' as const,
      };
      draw([searched({ toolCalls: [mine, providerSearch('theirs')] })]);
      expect(screen.getByText('Searched the web · 2').closest('details')).toHaveTextContent('mine');
      expect(document.querySelectorAll('details'), 'the search disclosure alone').toHaveLength(1);
    });

    // A provider call this build cannot name still left the machine, so it is
    // drawn; it has no status, so it has no tag.
    it('draws a provider call of no known kind as a disclosure with no tag', () => {
      const unknown = call({
        id: 'fetch',
        name: 'acme_fetch',
        actionKind: null,
        status: null,
        runsOn: 'Provider',
        action: null,
        approval: null,
      });
      draw([msg({ content: [{ type: 'text', text: 'Fetched.' }], toolCalls: [unknown] })]);
      expect(screen.getByText('acme_fetch').closest('summary')!.textContent).toBe('acme_fetch');
    });

    it('draws the queries under a cancelled answer', () => {
      draw([searched({ status: 'Cancelled', citations: [] })]);
      expect(screen.getByText('Searched the web · 1')).toBeInTheDocument();
    });
  });

  it('draws no stop reason under a complete answer with text', () => {
    draw([msg({ content: [{ type: 'text', text: 'Two pods.' }], finishReason: 'end_turn' })]);
    expect(screen.getByText('Two pods.')).toBeInTheDocument();
    expect(screen.queryByText(/^Stopped/)).toBeNull();
  });

  it('draws nothing for an empty complete answer with nothing to say', () => {
    draw([msg({ content: [], finishReason: '' })]);
    expect(screen.getByText('No messages yet.')).toBeInTheDocument();
  });

  it("draws a question's text and none of the context it carried", () => {
    draw([
      msg({
        role: 'User',
        content: [
          { type: 'context', text: '## Cluster\n\n```json\n{"context":"prod"}\n```' },
          { type: 'text', text: 'why?' },
        ],
      }),
    ]);
    expect(screen.getByText('why?')).toBeInTheDocument();
    expect(screen.queryByText(/"context":"prod"/)).toBeNull();
  });

  describe('the work disclosure', () => {
    // The disclosure is a <details>; its open attribute is the state under test.
    const label = () => screen.getByText(/^(Working .*|Worked for .*)$/);
    const disclosure = () => label().closest('details')!;
    const redraw = (rerender: (ui: ReactNode) => void, messages: ChatMessage[]) =>
      rerender(wrap(<ChatTranscript messages={messages} phase="live" mode="chat" chatID="c1" clusterID="1" />));
    const thinkingOnly = (over: Partial<ChatMessage> = {}) =>
      msg({ status: 'Streaming', content: [], thinking: 'the pod question', ...over });
    const withText = (over: Partial<ChatMessage> = {}) =>
      msg({ status: 'Streaming', content: [{ type: 'text', text: 'A pod' }], thinking: 'the pod question', ...over });
    // 14 seconds after `msg`'s createdAt.
    const FINISHED = '2026-11-15T10:00:14Z';
    const settled = (over: Partial<ChatMessage> = {}) =>
      msg({ thinking: 'the pod question', finishedAt: FINISHED, ...over });

    afterEach(() => {
      vi.useRealTimers();
    });

    it('is absent on an answer with no summary and no calls', () => {
      draw([msg({ finishedAt: FINISHED })]);
      expect(screen.queryByText(/^(Working .*|Worked for .*)$/)).toBeNull();
    });

    // The calls an answer made are its work: they fold away with the summary, and
    // draw the disclosure on an answer with no summary.
    it('holds the calls, with or without a summary', () => {
      const { rerender } = draw([msg({ finishedAt: FINISHED, toolCalls: [decided({ status: 'Succeeded' })] })]);
      expect(label()).toHaveTextContent('Worked for 14s');
      expect(disclosure()).toContainElement(screen.getByText('wc -l ~/.kube/config'));

      redraw(rerender, [settled({ toolCalls: [decided({ status: 'Succeeded' })] })]);
      expect(disclosure()).toHaveTextContent('the pod question');
      expect(disclosure()).toContainElement(screen.getByText('wc -l ~/.kube/config'));
    });

    // A card the user looks at, a task the user may stop and a failure that offers
    // a grant stay under the answer, outside the fold.
    it('leaves a log card, a running task and a grant offer outside', () => {
      const running = decided({ id: 'bg', status: 'Succeeded' });
      running.action!.command!.text = 'make serve';
      running.background = {
        status: 'Running',
        exitCode: null,
        report: '',
        matched: [],
        endedBecause: '',
        viewAround: null,
      };
      const failed = decided({ id: 'sb', status: 'Failed', output: 'Exit code 1\n' });
      failed.action!.command!.text = 'cat /secret';
      failed.action!.command!.sandboxed = true;
      draw([settled({ toolCalls: [logsViewCall(), running, failed, decided({ id: 'in', status: 'Succeeded' })] })]);
      expect(disclosure()).toContainElement(screen.getByText('wc -l ~/.kube/config'));
      expect(disclosure()).not.toContainElement(screen.getByText(/Logs: Deployment webapp in prod/));
      expect(disclosure()).not.toContainElement(screen.getByText('make serve'));
      expect(disclosure()).not.toContainElement(screen.getByRole('button', { name: 'Stop' }));
      expect(disclosure()).not.toContainElement(screen.getByText('cat /secret'));
      expect(disclosure()).not.toContainElement(screen.getByRole('button', { name: 'Grant a folder…' }));
    });

    it('holds the summary as markdown', () => {
      draw([settled({ thinking: '**bold** thought' })]);
      expect(disclosure().querySelector('strong')).toHaveTextContent('bold');
    });

    it('counts how long the turn has worked, then says how long it worked', async () => {
      // 8 seconds after `msg`'s createdAt.
      vi.useFakeTimers({ now: Date.parse('2026-11-15T10:00:08Z'), toFake: ['Date', 'setInterval', 'clearInterval'] });
      const { rerender } = draw([thinkingOnly()]);
      expect(label()).toHaveTextContent('Working 8s');

      await act(() => vi.advanceTimersByTimeAsync(2000));
      expect(label()).toHaveTextContent('Working 10s');

      redraw(rerender, [settled()]);
      expect(label()).toHaveTextContent('Worked for 14s');
    });

    it("draws the answer's actions only once its turn finishes", () => {
      const { rerender } = draw([withText()]);
      expect(screen.queryByRole('button', { name: 'Copy answer' })).toBeNull();

      redraw(rerender, [settled()]);
      expect(screen.getByRole('button', { name: 'Copy answer' })).toBeInTheDocument();
    });

    // The pause before the first word is what it exists to fill.
    it('is open on a streaming answer with no text and closes when the first text lands', () => {
      const { rerender } = draw([thinkingOnly()]);
      expect(disclosure().open).toBe(true);

      redraw(rerender, [withText()]);
      expect(disclosure().open).toBe(false);

      redraw(rerender, [withText({ status: 'Complete', finishedAt: FINISHED })]);
      expect(disclosure().open).toBe(false);
    });

    it('is closed on a settled answer', () => {
      draw([settled()]);
      expect(disclosure().open).toBe(false);
    });

    // A click pins the reader's choice for the message: chunks, text and
    // completion no longer move it.
    it('stays open once the reader opened it, through the first text and completion', () => {
      const { rerender } = draw([withText()]);
      expect(disclosure().open).toBe(false);

      fireEvent.click(label());
      expect(disclosure().open).toBe(true);

      redraw(rerender, [withText({ content: [{ type: 'text', text: 'A pod is a group.' }] })]);
      expect(disclosure().open).toBe(true);
      redraw(rerender, [withText({ status: 'Complete', finishedAt: FINISHED })]);
      expect(disclosure().open).toBe(true);
    });

    it('stays closed once the reader closed it, even while the answer still has no text', () => {
      const { rerender } = draw([thinkingOnly()]);
      fireEvent.click(label());
      expect(disclosure().open).toBe(false);

      redraw(rerender, [thinkingOnly({ thinking: 'the pod question, longer' })]);
      expect(disclosure().open).toBe(false);
    });

    it('toggles on a settled answer with each click', () => {
      draw([settled()]);
      fireEvent.click(label());
      expect(disclosure().open).toBe(true);
      fireEvent.click(label());
      expect(disclosure().open).toBe(false);
    });

    it('stays readable on an answer whose model differs from the next', () => {
      draw([
        settled({ id: 'm1', seq: 1, model: 'fake' }),
        settled({
          id: 'm2',
          seq: 2,
          thinking: 'the node question',
          model: 'claude-opus-5',
          provider: { id: 'anthropic', label: 'Anthropic' },
        }),
      ]);
      expect(screen.getAllByText('Worked for 14s')).toHaveLength(2);
      expect(screen.getByText('Opus 5')).toBeInTheDocument();
    });
  });

  describe('the turn actions', () => {
    const turns = [
      msg({ id: 'q1', seq: 1, role: 'User', content: [{ type: 'text', text: 'what is a pod?' }] }),
      msg({ id: 'a1', seq: 2, content: [{ type: 'text', text: 'a group of containers' }] }),
      msg({ id: 'q2', seq: 3, role: 'User', content: [{ type: 'text', text: 'and a node?' }] }),
      msg({ id: 'a2', seq: 4, content: [{ type: 'text', text: 'a machine' }] }),
    ];

    it('gives every question and answer a row, the last answer always shown', () => {
      draw(turns);
      expect(screen.getAllByRole('button', { name: 'Copy message' })).toHaveLength(2);
      const answers = screen.getAllByRole('button', { name: 'Copy answer' });
      expect(answers).toHaveLength(2);
      expect(answers[0].closest('.opacity-0')).not.toBeNull();
      expect(answers[1].closest('.opacity-0')).toBeNull();
    });

    it("copies a turn's text", () => {
      Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
      draw(turns);
      fireEvent.click(screen.getAllByRole('button', { name: 'Copy message' })[1]);
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('and a node?');
      fireEvent.click(screen.getAllByRole('button', { name: 'Copy answer' })[0]);
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('a group of containers');
    });
  });

  describe('Ask again', () => {
    const failed = [
      msg({ id: 'm1', seq: 1, role: 'User', content: [{ type: 'text', text: 'what is a pod?' }] }),
      msg({ id: 'm2', seq: 2, status: 'Failed', content: [], error: '429 rate_limit_error', effort: 'low' }),
    ];

    // The sidecar never retries a turn, but the send was accepted and the draft is
    // gone, so the user's own second ask is the way back.
    it("asks the failed answer's question again, under what it ran on", async () => {
      draw(failed);

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: 'Ask again' }));
      });

      expect(sendMock).toHaveBeenCalledWith(
        expect.objectContaining({
          chatID: 'c1',
          content: 'what is a pod?',
          providerID: 'fake',
          modelID: 'fake',
          effort: 'low',
          sandboxDisabled: false,
        }),
      );
    });

    it('asks again with the switch the chat has', async () => {
      draw(failed, { sandboxAvailable: true, sandboxDisabled: true });
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: 'Ask again' }));
      });
      expect(sendMock).toHaveBeenCalledWith(expect.objectContaining({ sandboxDisabled: true }));
    });

    it('asks again with the network switch the chat has', async () => {
      draw(failed, { sandboxAvailable: true, networkEnabled: true });
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: 'Ask again' }));
      });
      expect(sendMock).toHaveBeenCalledWith(expect.objectContaining({ networkEnabled: true, networkThisTurn: false }));
    });

    it('is not offered before the list delivers the network switch', () => {
      draw(failed, { sandboxAvailable: true, networkEnabled: undefined });
      expect(screen.queryByRole('button', { name: 'Ask again' })).toBeNull();
    });

    it('is not offered before the list delivers the switch', () => {
      draw(failed, { sandboxAvailable: true, sandboxDisabled: undefined });
      expect(screen.queryByRole('button', { name: 'Ask again' })).toBeNull();
    });

    it('is not offered while the sandbox switch is in flight', () => {
      draw(failed, { sandboxAvailable: false, switching: true });
      expect(screen.queryByRole('button', { name: 'Ask again' })).toBeNull();
    });

    // A fresh send lands at the end, so an answer to a question mid-transcript would
    // sit far from it.
    it('is offered on the last row alone', () => {
      draw([...failed, msg({ id: 'm3', seq: 3, content: [{ type: 'text', text: 'and this one worked' }] })]);
      expect(screen.queryByRole('button', { name: 'Ask again' })).toBeNull();
    });

    // A transcript that opens on a failed answer — its question folded away by a
    // straggling frame — still offers the button, with nothing to send.
    it('sends an empty question when the transcript holds none before it', async () => {
      draw([msg({ id: 'm2', seq: 2, status: 'Failed', content: [], error: '429 rate_limit_error' })]);

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: 'Ask again' }));
      });

      expect(sendMock).toHaveBeenCalledWith(expect.objectContaining({ content: '' }));
    });

    it('is not offered on an answer that did not fail', () => {
      draw([msg()]);
      expect(screen.queryByRole('button', { name: 'Ask again' })).toBeNull();
    });
  });

  describe('an Agent call', () => {
    const agent = (over: Partial<ChatToolCall> = {}) =>
      call({
        id: 'ag1',
        name: 'Agent',
        actionKind: 'Delegate',
        status: 'Succeeded',
        approval: null,
        action: {
          description: 'Count the pods',
          command: null,
          read: null,
          write: null,
          edit: null,
          search: null,
          fetch: null,
          memory: null,
          delegate: { prompt: 'Count the pods in every namespace.', agentType: 'general-purpose', model: '' },
          kubeQuery: null,
          logsView: null,
          logsRead: null,
          logsWatch: null,
        },
        output: 'Agent launched: t1. It is working in the background.',
        background: {
          status: 'Completed',
          exitCode: null,
          report: 'There are **twelve** pods.',
          matched: [],
          endedBecause: '',
          viewAround: null,
        },
        ...over,
      });
    const subagentCall = (over: Partial<ChatToolCall> = {}) =>
      decided({ id: 'cc1', status: 'Succeeded', output: 'pod-a\npod-b', agentCallID: 'ag1', ...over });

    it('draws the call by its description, with the brief, the calls and the report inside it', () => {
      draw([msg({ toolCalls: [agent(), subagentCall()] })]);

      const summaries = screen.getAllByText('Agent');
      expect(summaries).toHaveLength(1);
      const disclosure = summaries[0].closest('details')!;
      expect(within(disclosure).getByText('Count the pods')).toBeInTheDocument();
      expect(within(disclosure).getByText('general-purpose')).toBeInTheDocument();
      expect(within(disclosure).getByText('Count the pods in every namespace.')).toBeInTheDocument();
      const nested = within(disclosure).getByText('wc -l ~/.kube/config').closest('details')!;
      expect(disclosure).toContainElement(nested);
      expect(within(disclosure).getByText('twelve').tagName).toBe('STRONG');
    });

    it('hands a subagent’s view the chat’s cluster', () => {
      function Focused() {
        const { view } = useLogsView();
        return <output data-testid="focused">{view ? `${view.clusterId}/${view.callId}` : 'none'}</output>;
      }
      render(
        wrap(
          <>
            <ChatTranscript
              messages={[msg({ toolCalls: [agent(), logsViewCall({ agentCallID: 'ag1' })] })]}
              phase="live"
              mode="chat"
              chatID="c1"
              clusterID="1"
            />
            <Focused />
          </>,
        ),
      );

      fireEvent.click(screen.getByRole('button', { name: 'Expand' }));
      expect(screen.getByTestId('focused')).toHaveTextContent('1/lv-1');
    });

    it('draws the report off its task, never the launch text the model read', () => {
      draw([msg({ toolCalls: [agent()] })]);

      expect(screen.getByText('twelve').tagName).toBe('STRONG');
      expect(screen.queryByText(/Agent launched/)).toBeNull();
    });

    it('draws the brief and no report for an agent that never got a task', () => {
      draw([msg({ toolCalls: [agent({ status: 'Failed', output: 'launch failed', background: null })] })]);

      const disclosure = screen.getByText('Agent').closest('details')!;
      expect(within(disclosure).getByText('Count the pods in every namespace.')).toBeInTheDocument();
      expect(screen.queryByText('twelve')).toBeNull();
    });

    it('draws no report while the agent runs, and a Stop under the call', () => {
      draw([
        msg({
          toolCalls: [
            agent({
              background: {
                status: 'Running',
                exitCode: null,
                report: '',
                matched: [],
                endedBecause: '',
                viewAround: null,
              },
            }),
          ],
        }),
      ]);

      const summary = screen.getByText('Agent').closest('summary')!;
      expect(summary).toHaveTextContent('running in background');
      expect(screen.queryByText('twelve')).toBeNull();
      expect(screen.getByRole('button', { name: 'Stop' })).toBeInTheDocument();
    });

    it('tags an ended agent by how it ended, with no Stop', () => {
      (
        [
          ['Completed', ''],
          ['Failed', 'error'],
          ['Stopped', 'stopped'],
          ['Lost', 'lost'],
        ] as const
      ).forEach(([status, tag]) => {
        const { unmount } = draw([
          msg({
            toolCalls: [
              agent({
                background: { status, exitCode: null, report: '', matched: [], endedBecause: '', viewAround: null },
              }),
            ],
          }),
        ]);
        expect(screen.getByText('Agent').closest('summary')).toHaveTextContent(new RegExp(`${tag}$`));
        expect(screen.queryByRole('button', { name: 'Stop' })).toBeNull();
        unmount();
      });
    });

    it("never draws a subagent's call at the top level", () => {
      draw([msg({ toolCalls: [agent(), subagentCall()] })]);

      const drawn = screen.getAllByText('wc -l ~/.kube/config');
      expect(drawn).toHaveLength(1);
      expect(screen.getByText('Agent').closest('details')).toContainElement(drawn[0]);
    });

    // The user is approving a call whose reasoning they cannot see, so the
    // request says whose it is, above its own title; the rest is unchanged.
    it("opens a subagent's request with the agent asking", () => {
      draw([
        msg({
          status: 'WaitingApproval',
          toolCalls: [agent({ status: 'Running', output: '' }), call({ id: 'cc1', agentCallID: 'ag1' })],
        }),
      ]);

      const request = screen.getByRole('group', { name: 'Command awaiting approval' });
      const lines = within(request).getAllByText(/An agent asks:|Count the pods|Run this command\?/);
      expect(lines.map((el) => el.textContent)).toEqual(['An agent asks:', 'Count the pods', 'Run this command?']);
      expect(within(request).getByRole('button', { name: 'Approve' })).toBeEnabled();
    });

    it('draws every waiting request, each Approve reaching its own id', async () => {
      draw([
        msg({
          awaitingApproval: true,
          toolCalls: [
            agent({
              background: {
                status: 'Running',
                exitCode: null,
                report: '',
                matched: [],
                endedBecause: '',
                viewAround: null,
              },
            }),
            call({ id: 'cc1', agentCallID: 'ag1', approval: { id: 'ap-1', status: 'Pending', duration: null } }),
            call({ id: 'cc2', agentCallID: 'ag1', approval: { id: 'ap-2', status: 'Pending', duration: null } }),
          ],
        }),
      ]);

      const requests = screen.getAllByRole('group', { name: 'Command awaiting approval' });
      expect(requests).toHaveLength(2);
      await act(async () => {
        fireEvent.click(within(requests[1]).getByRole('button', { name: 'Approve' }));
      });
      expect(sendMock).toHaveBeenCalledWith({ id: 'ap-2', decision: 'Once' });
      expect(within(requests[0]).getByRole('button', { name: 'Approve' })).toBeEnabled();
    });

    it('draws the requests in the order they were asked', () => {
      draw([
        msg({
          awaitingApproval: true,
          toolCalls: [
            call({
              id: 'cc2',
              agentCallID: 'ag1',
              approval: { id: 'ap-2', status: 'Pending', duration: null },
              action: {
                ...call().action!,
                command: { text: 'second', cwd: '', background: false, sandboxed: false, network: false, env: [] },
              },
            }),
            call({
              id: 'cc1',
              agentCallID: 'ag1',
              approval: { id: 'ap-1', status: 'Pending', duration: null },
              action: {
                ...call().action!,
                command: { text: 'first', cwd: '', background: false, sandboxed: false, network: false, env: [] },
              },
            }),
            agent(),
          ],
        }),
      ]);

      const first = screen.getByText('first');
      expect(first.compareDocumentPosition(screen.getByText('second'))).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    });

    describe('arming', () => {
      // Where the buttons are on screen: a case moves them by changing top.
      let top = 0;

      beforeEach(() => {
        vi.useFakeTimers();
        top = 0;
        vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => new DOMRect(0, top, 100, 20));
      });

      afterEach(() => {
        vi.useRealTimers();
        vi.restoreAllMocks();
      });

      const waitingAgent = () =>
        msg({
          awaitingApproval: true,
          toolCalls: [
            agent({
              background: {
                status: 'Running',
                exitCode: null,
                report: '',
                matched: [],
                endedBecause: '',
                viewAround: null,
              },
            }),
            call({ id: 'cc1', agentCallID: 'ag1' }),
          ],
        });
      const approveButton = () => screen.getByRole('button', { name: 'Approve' });
      const denyButton = () => screen.getByRole('button', { name: 'Deny' });

      it('arms Approve once its place has held for the wait, and never holds Deny back', async () => {
        draw([waitingAgent()], { approveArmMs: APPROVE_ARM_MS });
        expect(approveButton()).toBeDisabled();
        expect(denyButton()).toBeEnabled();

        await act(() => vi.advanceTimersByTimeAsync(APPROVE_ARM_MS - 100));
        expect(approveButton()).toBeDisabled();

        await act(() => vi.advanceTimersByTimeAsync(200));
        expect(approveButton()).toBeEnabled();
      });

      it('disarms Approve when the page moves it, and arms it again once it holds', async () => {
        draw([waitingAgent()], { approveArmMs: APPROVE_ARM_MS });
        await act(() => vi.advanceTimersByTimeAsync(APPROVE_ARM_MS + 100));
        expect(approveButton()).toBeEnabled();

        top = 60;
        await act(() => vi.advanceTimersByTimeAsync(50));
        expect(approveButton()).toBeDisabled();
        expect(denyButton()).toBeEnabled();

        await act(() => vi.advanceTimersByTimeAsync(APPROVE_ARM_MS + 100));
        expect(approveButton()).toBeEnabled();
      });

      it('waits the half second in production', () => {
        expect(APPROVE_ARM_MS).toBe(500);
      });
    });

    it('draws a request under a settled answer, live while the answer awaits approval', () => {
      const settled = (awaitingApproval: boolean) =>
        msg({
          awaitingApproval,
          toolCalls: [
            agent({
              background: {
                status: 'Running',
                exitCode: null,
                report: '',
                matched: [],
                endedBecause: '',
                viewAround: null,
              },
            }),
            call({ id: 'cc1', agentCallID: 'ag1' }),
          ],
        });
      const { unmount } = draw([settled(true)]);
      expect(screen.getByRole('button', { name: 'Approve' })).toBeEnabled();
      unmount();

      draw([settled(false)]);
      expect(screen.getByRole('button', { name: 'Approve' })).toBeDisabled();
      expect(screen.getByRole('button', { name: 'Deny' })).toBeDisabled();
    });

    it("draws no agent line on the answer's own request", () => {
      draw([msg({ status: 'WaitingApproval', toolCalls: [call()] })]);

      expect(screen.queryByText('An agent asks:')).not.toBeInTheDocument();
    });

    it('names the model it ran on by its catalog label', () => {
      draw([
        msg({
          toolCalls: [
            agent({
              action: {
                ...agent().action!,
                delegate: { prompt: 'p', agentType: 'general-purpose', model: 'fake' },
              },
            }),
          ],
        }),
      ]);

      expect(screen.getByText('general-purpose · Fake model')).toBeInTheDocument();
    });
  });

  describe('commands', () => {
    // The transcript draws a command off the row alone, so every case here has
    // a content holding no tool block.
    const disclosure = (text: string) => screen.getByText(text).closest('details')!;
    // The tag rides the summary, apart from the output the body holds.
    const summary = (text: string) => disclosure(text).querySelector('summary')!;

    const waiting = (toolCall: ChatToolCall = call()) =>
      msg({ status: 'WaitingApproval', content: [], toolCalls: [toolCall] });
    const request = () => screen.getByRole('group', { name: 'Command awaiting approval' });
    const approve = () => screen.getByRole('button', { name: 'Approve' });
    const deny = () => screen.getByRole('button', { name: 'Deny' });

    it('draws a request with the command as text and two buttons while it waits', async () => {
      draw([waiting()]);
      expect(request()).toHaveTextContent('Run this command?');
      expect(request().querySelector('pre')).toHaveTextContent('wc -l ~/.kube/config');
      expect(request().querySelector('a')).toBeNull();
      expect(approve()).toBeEnabled();
      expect(deny()).toBeEnabled();

      await act(async () => {
        fireEvent.click(approve());
      });
      expect(sendMock).toHaveBeenCalledWith({ id: 'ap-1', decision: 'Once' });
      expect(approve()).toBeDisabled();
      expect(deny()).toBeDisabled();
    });

    // A sandboxed command asks only for the internet, so its request says so,
    // read off the call before anything else.
    it.each([true, false, undefined])('asks a sandboxed command for the network (sandbox %s)', (available) => {
      const sandboxed = call();
      sandboxed.action!.command!.sandboxed = true;
      sandboxed.action!.command!.network = true;
      draw([waiting(sandboxed)], { sandboxAvailable: available });
      expect(request()).toHaveTextContent('Run this command with network access?');
      expect(request()).not.toHaveTextContent('outside');
    });

    it('asks a sandboxed background command for the network', () => {
      const sandboxed = call();
      sandboxed.action!.command!.sandboxed = true;
      sandboxed.action!.command!.background = true;
      draw([waiting(sandboxed)], { sandboxAvailable: true });
      expect(request()).toHaveTextContent('Run this command in the background, with network access?');
    });

    // Outside the sandbox the argument changes nothing, so the heading is the
    // outside one whatever the call asked for.
    it('keeps the outside heading for a call outside the sandbox that asked for the network', () => {
      const outside = call();
      outside.action!.command!.network = true;
      draw([waiting(outside)], { sandboxAvailable: true });
      expect(request()).toHaveTextContent('Run this command outside the sandbox?');
      expect(request()).not.toHaveTextContent('network');
    });

    // The user decides on the command and its network, so the request never says
    // sandboxed.
    it('never says sandboxed on a sandboxed request', () => {
      const sandboxed = call();
      sandboxed.action!.command!.sandboxed = true;
      draw([waiting(sandboxed)]);
      expect(request()).not.toHaveTextContent('sandbox');
    });

    // On a machine with a sandbox a command asks only when it runs outside it, so
    // every command's request there says so. Unknown draws the same: on a machine
    // with no sandbox every command does run outside one.
    it.each([true, undefined])('says a command runs outside the sandbox where there may be one (%s)', (available) => {
      draw([waiting(call())], { sandboxAvailable: available });
      expect(request()).toHaveTextContent('Run this command outside the sandbox?');
      expect(request()).not.toHaveTextContent('Run this command?');
    });

    // The turn read the switch when it started, so the heading must not follow a
    // switch flipped while the request waits.
    it("draws the heading whatever the chat's switch is now", () => {
      draw([waiting(call())], { sandboxAvailable: true, sandboxDisabled: false });
      expect(request()).toHaveTextContent('Run this command outside the sandbox?');
    });

    it('says a background command outside the sandbox runs outside it', () => {
      const outside = call();
      outside.action!.command!.background = true;
      draw([waiting(outside)], { sandboxAvailable: true });
      expect(request()).toHaveTextContent('Run this command in the background, outside the sandbox?');
      expect(request()).toHaveTextContent('It keeps running after this answer, until it exits or you stop it.');
    });

    // A gated call the sidecar cannot show is never approvable: the request says
    // so, offers no Approve, and Deny stays.
    it('draws no Approve for a request it cannot show', async () => {
      draw([waiting(call({ action: null }))]);
      expect(request()).toHaveTextContent("This request can't be shown.");
      expect(request().querySelector('pre')).toBeNull();
      expect(screen.queryByRole('button', { name: 'Approve' })).toBeNull();
      expect(deny()).toBeEnabled();

      await act(async () => {
        fireEvent.click(deny());
      });
      expect(sendMock).toHaveBeenCalledWith({ id: 'ap-1', decision: 'Deny' });
    });

    // A read asks for the file by its path, spelled like a command: the path is
    // what the model chose, and an override in it must not reorder it.
    it('draws a read request with the path as text and two buttons', async () => {
      const path = '/Users/ana/.kube/con‮fig';
      draw([
        waiting(
          call({
            name: 'Read',
            actionKind: 'Read',
            action: {
              description: '',
              command: null,
              read: { path },
              write: null,
              edit: null,
              search: null,
              fetch: null,
              memory: null,
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
          }),
        ),
      ]);
      const read = screen.getByRole('group', { name: 'File read awaiting approval' });
      expect(read).toHaveTextContent('Read this file?');
      expect(read.querySelector('mark')).toHaveTextContent('\\u{202E}');
      expect(read).not.toHaveTextContent('The model says:');
      expect(screen.queryByRole('group', { name: 'Command awaiting approval' })).toBeNull();
      expect(approve()).toBeEnabled();

      await act(async () => {
        fireEvent.click(approve());
      });
      expect(sendMock).toHaveBeenCalledWith({ id: 'ap-1', decision: 'Once' });
    });

    // A fetch asks for the URL and names, on its own line, the host the
    // sidecar dials, since a long URL can hide which host it goes to.
    it('draws a fetch request with the URL and the host as text', async () => {
      const url = 'https://evil.test/\u{202E}gro.buhtig';
      draw([
        waiting(
          call({
            name: 'WebFetch',
            actionKind: 'Fetch',
            action: {
              description: '',
              command: null,
              read: null,
              write: null,
              edit: null,
              search: null,
              fetch: { url, host: 'evil.test' },
              memory: null,
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
          }),
        ),
      ]);
      const fetchRequest = screen.getByRole('group', { name: 'Page fetch awaiting approval' });
      expect(fetchRequest).toHaveTextContent('Fetch this page?');
      expect(fetchRequest).toHaveTextContent('https://evil.test/\\u{202E}gro.buhtig');
      expect(fetchRequest.querySelector('mark')).toHaveTextContent('\\u{202E}');
      expect(fetchRequest).toHaveTextContent(/Host:\s*evil\.test/);
      expect(approve()).toBeEnabled();

      await act(async () => {
        fireEvent.click(approve());
      });
      expect(sendMock).toHaveBeenCalledWith({ id: 'ap-1', decision: 'Once' });
    });

    // A memory call for one cluster never waits on the user, so a request naming
    // one is nothing the request can draw.
    it('offers no Approve for a memory call for one cluster', () => {
      draw([
        waiting(
          call({
            name: 'Memory',
            actionKind: 'Memory',
            action: {
              description: '',
              command: null,
              read: null,
              write: null,
              edit: null,
              search: null,
              fetch: null,
              memory: { op: 'save', name: 'prefs', body: 'Answer with commands.', scope: 'cluster' },
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
          }),
        ),
      ]);
      expect(screen.getByText("This request can't be shown.")).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: 'Approve' })).toBeNull();
    });

    describe('a call for every cluster', () => {
      const everywhere = (memory: { op: string; name: string; body: string }) =>
        waiting(
          call({
            name: 'Memory',
            actionKind: 'Memory',
            action: {
              description: '',
              command: null,
              read: null,
              write: null,
              edit: null,
              search: null,
              fetch: null,
              memory: { ...memory, scope: 'everywhere' },
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
          }),
        );
      const remembering = (body: string, name = 'prefs') => everywhere({ op: 'save', name, body });
      const memoryRequest = () => screen.getByRole('group', { name: 'Memory save awaiting approval' });

      // The note is the thing approved: its name, then every character it will
      // hold, and who will read it.
      it('draws the name and the body as text, with two buttons', async () => {
        draw([remembering('The user is Bob.\u{202E}', 'user-name')]);
        expect(memoryRequest()).toHaveTextContent('Remember this for every cluster?');
        expect(memoryRequest()).toHaveTextContent('user-name');
        expect(memoryRequest().querySelector('pre')).toHaveTextContent('The user is Bob.');
        expect(memoryRequest().querySelector('pre mark')).toHaveTextContent('\\u{202E}');
        expect(memoryRequest()).toHaveTextContent("Kubetail will read it in every cluster's chats.");
        expect(memoryRequest()).not.toHaveTextContent('your memory');
        expect(screen.queryByRole('group', { name: 'Command awaiting approval' })).toBeNull();
        expect(approve()).toBeEnabled();

        await act(async () => {
          fireEvent.click(approve());
        });
        expect(sendMock).toHaveBeenCalledWith({ id: 'ap-1', decision: 'Once' });
      });

      it('spells the whitespace that ends a line', () => {
        draw([remembering('Bob \nRobert')]);
        expect([...memoryRequest().querySelectorAll('pre mark')].map((m) => m.textContent)).toEqual(['\\u{20}']);
      });

      it('folds a long body and holds Approve until the rest is shown', async () => {
        draw([remembering(`${'a\n'.repeat(30)}the last line`)]);
        expect(memoryRequest().querySelector('pre')).not.toHaveTextContent('the last line');
        expect(approve()).toBeDisabled();
        expect(deny()).toBeEnabled();

        await act(async () => {
          fireEvent.click(screen.getByRole('button', { name: /^Show the rest/ }));
        });
        expect(memoryRequest().querySelector('pre')).toHaveTextContent('the last line');
        expect(approve()).toBeEnabled();
      });

      // A forget carries no body: the name is what goes.
      it('draws a forget request with the name as text', async () => {
        draw([everywhere({ op: 'forget', name: 'user\u{202E}name', body: '' })]);
        const forgetRequest = screen.getByRole('group', { name: 'Memory forget awaiting approval' });
        expect(forgetRequest).toHaveTextContent('Forget this memory for every cluster?');
        expect(forgetRequest.querySelector('mark')).toHaveTextContent('\\u{202E}');
        expect(forgetRequest.querySelector('pre')).toBeNull();
        expect(approve()).toBeEnabled();

        await act(async () => {
          fireEvent.click(approve());
        });
        expect(sendMock).toHaveBeenCalledWith({ id: 'ap-1', decision: 'Once' });
      });
    });

    describe('a write', () => {
      const writing = (content: string, path = '/Users/ana/values.yaml') =>
        waiting(
          call({
            name: 'Write',
            actionKind: 'Write',
            action: {
              description: '',
              command: null,
              read: null,
              write: { path, content },
              edit: null,
              search: null,
              fetch: null,
              memory: null,
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
          }),
        );
      const writeRequest = () => screen.getByRole('group', { name: 'File write awaiting approval' });

      // The file is the thing approved: its path, then every byte it will hold,
      // spelled like a command.
      it('draws the path and the content as text, with two buttons', async () => {
        draw([writing('replicas: 2\n', '/Users/ana/val\u{202E}ues.yaml')]);
        expect(writeRequest()).toHaveTextContent('Write this file?');
        expect(writeRequest().querySelector('mark')).toHaveTextContent('\\u{202E}');
        expect(writeRequest().querySelector('pre')).toHaveTextContent('replicas: 2');
        expect(writeRequest()).not.toHaveTextContent('The model says:');
        expect(screen.queryByRole('group', { name: 'Command awaiting approval' })).toBeNull();
        expect(approve()).toBeEnabled();

        await act(async () => {
          fireEvent.click(approve());
        });
        expect(sendMock).toHaveBeenCalledWith({ id: 'ap-1', decision: 'Once' });
      });

      it('folds a long content and holds Approve until the rest is shown', async () => {
        draw([writing(`${'a: 1\n'.repeat(30)}secret: x`)]);
        expect(writeRequest().querySelector('pre')).not.toHaveTextContent('secret: x');
        expect(approve()).toBeDisabled();
        expect(deny()).toBeEnabled();

        await act(async () => {
          fireEvent.click(screen.getByRole('button', { name: /^Show the rest/ }));
        });
        expect(writeRequest().querySelector('pre')).toHaveTextContent('secret: x');
        expect(approve()).toBeEnabled();
      });

      // The bytes that land are the bytes drawn: a CRLF content marks every \r.
      it('spells each \\r of a CRLF content out', () => {
        draw([writing('a\r\nb\r\n')]);
        expect([...writeRequest().querySelectorAll('pre mark')].map((m) => m.textContent)).toEqual([
          '\\u{D}',
          '\\u{D}',
        ]);
      });

      // A space or tab that ends a line draws as nothing, and a final newline
      // leaves no mark: each is spelled or said, so "foo" and "foo\n" differ.
      it('spells the whitespace that ends a line and says how the file ends', () => {
        const { unmount } = draw([writing('a: 1  \nb: 2')]);
        expect([...writeRequest().querySelectorAll('pre mark')].map((m) => m.textContent)).toEqual([
          '\\u{20}',
          '\\u{20}',
        ]);
        expect(writeRequest()).toHaveTextContent('No newline at the end.');
        unmount();

        draw([writing('b: 2\n')]);
        expect(writeRequest()).toHaveTextContent('Ends with a newline.');
      });

      it('spells content of whitespace alone', () => {
        draw([writing(' \t')]);
        expect([...writeRequest().querySelectorAll('pre mark')].map((m) => m.textContent)).toEqual([
          '\\u{20}',
          '\\u{9}',
        ]);
        expect(writeRequest()).not.toHaveTextContent('The file will be empty.');
      });

      // The fold can fall just before a newline, so a line the head ends on is
      // still a line end.
      it('spells the whitespace ending the last line before the fold', () => {
        draw([writing(`${'a\n'.repeat(23)}b \n${'c\n'.repeat(5)}`)]);
        expect([...writeRequest().querySelectorAll('pre mark')].map((m) => m.textContent)).toEqual(['\\u{20}']);
        expect(screen.getByRole('button', { name: /^Show the rest/ })).toBeInTheDocument();
      });

      it('says so when the file will be empty', () => {
        draw([writing('')]);
        expect(writeRequest()).toHaveTextContent('The file will be empty.');
        expect(writeRequest().querySelector('pre')).toBeNull();
        expect(approve()).toBeEnabled();
      });

      // A settled write reads as its path, and its body is the content it wrote,
      // folded as on the request, then what the model read.
      it('summarises a settled write by its path, with the content in its body', async () => {
        draw([
          msg({
            toolCalls: [
              call({
                name: 'Write',
                actionKind: 'Write',
                status: 'Succeeded',
                action: {
                  description: '',
                  command: null,
                  read: null,
                  write: { path: '/Users/ana/values.yaml', content: `${'a: 1\n'.repeat(30)}end: x` },
                  edit: null,
                  search: null,
                  fetch: null,
                  memory: null,
                  delegate: null,
                  kubeQuery: null,
                  logsView: null,
                  logsRead: null,
                  logsWatch: null,
                },
                approval: { id: 'ap-1', status: 'Approved', duration: null },
                output: 'File created successfully at: /Users/ana/values.yaml',
              }),
            ],
          }),
        ]);
        const details = document.querySelector('details details')!;
        expect(details.querySelector('summary')!.textContent).toBe('Write/Users/ana/values.yaml');
        expect(details.querySelectorAll('pre')[0]).not.toHaveTextContent('end: x');
        expect(details.querySelectorAll('pre')[1]).toHaveTextContent('File created successfully');

        await act(async () => {
          fireEvent.click(screen.getByRole('button', { name: /^Show the rest/ }));
        });
        expect(details.querySelectorAll('pre')[0]).toHaveTextContent('end: x');
      });

      // A write nobody was asked about is in the chat's workspace: the user sees
      // what it wrote under its summary, since a later command may run it.
      describe('unasked', () => {
        const unasked = (over: Partial<ChatToolCall>) =>
          msg({
            toolCalls: [
              call({
                name: 'Write',
                actionKind: 'Write',
                status: 'Succeeded',
                action: {
                  description: '',
                  command: null,
                  read: null,
                  write: { path: '/data/chats/c1/workspace/run.sh', content: `${'echo a\n'.repeat(30)}end: x` },
                  edit: null,
                  search: null,
                  fetch: null,
                  memory: null,
                  delegate: null,
                  kubeQuery: null,
                  logsView: null,
                  logsRead: null,
                  logsWatch: null,
                },
                approval: null,
                output: 'File created successfully at: /data/chats/c1/workspace/run.sh',
                ...over,
              }),
            ],
          });
        const outside = () => [...document.querySelectorAll('pre')].filter((p) => !p.closest('details'));

        it('draws its content open, folded, and not again inside the disclosure', async () => {
          draw([unasked({})]);
          const details = document.querySelector('details')!;
          expect(details).not.toHaveAttribute('open');
          expect(outside()).toHaveLength(1);
          expect(outside()[0]).toHaveTextContent('echo a');
          expect(outside()[0]).not.toHaveTextContent('end: x');
          expect(details.querySelectorAll('pre')).toHaveLength(1);
          expect(details.querySelector('pre')).toHaveTextContent('File created successfully');

          const more = screen.getByRole('button', { name: /^Show the rest/ });
          expect(more.closest('details')).toBeNull();
          await act(async () => {
            fireEvent.click(more);
          });
          expect(outside()[0]).toHaveTextContent('end: x');
        });

        it('draws one refused before asking as today, since it wrote nothing', () => {
          draw([unasked({ status: 'Failed', output: 'Write cannot change Kubetail’s directories.' })]);
          expect(outside()).toHaveLength(0);
          expect(document.querySelector('details')!.querySelectorAll('pre')).toHaveLength(2);
        });
      });
    });

    // A query asks no one: it reads as Query with the model's description, and its
    // body is the SQL above the rows the model read.
    it('draws a KubeQuery call as Query, its description, then its SQL above its rows', () => {
      const sql = "SELECT namespace, name FROM objects WHERE status = 'CrashLoopBackOff'";
      draw([
        msg({
          toolCalls: [
            call({
              name: 'KubeQuery',
              actionKind: 'KubeQuery',
              status: 'Succeeded',
              action: {
                description: 'Pods in a crash loop',
                command: null,
                read: null,
                write: null,
                edit: null,
                search: null,
                fetch: null,
                memory: null,
                delegate: null,
                kubeQuery: { sql, limit: 200 },
                logsView: null,
                logsRead: null,
                logsWatch: null,
              },
              approval: null,
              output:
                '{"cluster":"prod","freshness":{"status":"watching"},"columns":["namespace","name"],"more":false,"rows":[["default","worker"]]}',
            }),
          ],
        }),
      ]);
      const details = document.querySelector('details details')!;
      const head = details.querySelector('summary')!;
      expect(head.querySelector('.font-medium')).toHaveTextContent(/^Query$/);
      expect(head).toHaveTextContent('Pods in a crash loop');
      const pres = details.querySelectorAll('pre');
      expect(pres).toHaveLength(1);
      expect(pres[0]).toHaveTextContent(sql);
      expect(within(details as HTMLElement).getByRole('cell', { name: 'worker' })).toBeInTheDocument();
      expect(details).toHaveTextContent('1 row · prod');
      expect(screen.queryByRole('button', { name: 'Approve' })).toBeNull();
    });

    describe('an edit', () => {
      const editAction = (
        oldString: string,
        newString: string,
        replaceAll = false,
        path = '/Users/ana/values.yaml',
      ) => ({
        description: '',
        command: null,
        read: null,
        write: null,
        edit: { path, oldString, newString, replaceAll },
        search: null,
        fetch: null,
        memory: null,
        delegate: null,
        kubeQuery: null,
        logsView: null,
        logsRead: null,
        logsWatch: null,
      });
      const settled = (oldString: string, newString: string, replaceAll = false) =>
        msg({
          toolCalls: [
            call({
              name: 'Edit',
              actionKind: 'Edit',
              status: 'Succeeded',
              action: editAction(oldString, newString, replaceAll),
              approval: { id: 'ap-1', status: 'Approved', duration: null },
              output: 'The file /Users/ana/values.yaml has been updated successfully.',
            }),
          ],
        });

      const editing = (oldString: string, newString: string, replaceAll = false, path?: string) =>
        waiting(call({ name: 'Edit', actionKind: 'Edit', action: editAction(oldString, newString, replaceAll, path) }));
      const editRequest = () => screen.getByRole('group', { name: 'File edit awaiting approval' });
      const marks = () => [...editRequest().querySelectorAll('pre mark')].map((m) => m.textContent);

      // The change is the thing approved: the path, then the text that goes and
      // the text that replaces it, spelled like a write's content.
      it('draws the path and both strings as text, with two buttons', async () => {
        draw([editing('replicas: 1', 'replicas: 3', false, '/Users/ana/val\u{202E}ues.yaml')]);
        expect(editRequest()).toHaveTextContent('Edit this file?');
        expect(editRequest().querySelector('p.font-mono mark')).toHaveTextContent('\\u{202E}');
        const pres = editRequest().querySelectorAll('pre');
        expect(pres[0]).toHaveTextContent('replicas: 1');
        expect(pres[1]).toHaveTextContent('replicas: 3');
        expect(editRequest()).toHaveTextContent('Replace');
        expect(editRequest()).toHaveTextContent('With');
        expect(editRequest()).not.toHaveTextContent('Every occurrence in the file.');
        expect(approve()).toBeEnabled();

        await act(async () => {
          fireEvent.click(approve());
        });
        expect(sendMock).toHaveBeenCalledWith({ id: 'ap-1', decision: 'Once' });
      });

      // Each string is a piece of a file: every \r, and each space or tab that
      // ends a line, is spelled.
      it('spells each \\r and the whitespace ending a line', () => {
        draw([editing('a: 1  \r\nb', 'a: 2\r\nb')]);
        expect(marks()).toEqual(['\\u{20}', '\\u{20}', '\\u{D}', '\\u{D}']);
      });

      it('folds each long string and holds Approve until both are shown', async () => {
        draw([editing(`${'a: 1\n'.repeat(30)}old: x`, `${'a: 2\n'.repeat(30)}new: y`)]);
        expect(editRequest()).not.toHaveTextContent('old: x');
        expect(editRequest()).not.toHaveTextContent('new: y');
        expect(approve()).toBeDisabled();
        expect(deny()).toBeEnabled();

        await act(async () => {
          fireEvent.click(screen.getAllByRole('button', { name: /^Show the rest/ })[0]);
        });
        expect(editRequest()).toHaveTextContent('old: x');
        expect(approve()).toBeDisabled();

        await act(async () => {
          fireEvent.click(screen.getByRole('button', { name: /^Show the rest/ }));
        });
        expect(editRequest()).toHaveTextContent('new: y');
        expect(approve()).toBeEnabled();
      });

      // A newline at either end of a string decides whether the edit joins or
      // splits lines, and a <pre> draws neither: each string says how it ends.
      it('names the newlines at the edges of each string', () => {
        const { unmount } = draw([editing('x: 1\n', 'x: 2')]);
        const lines = () => [...editRequest().querySelectorAll('p')].map((p) => p.textContent);
        expect(lines()).toContain('Ends with a newline.');
        expect(lines()).toContain('No newline at either end.');
        unmount();

        draw([editing('\nx: 1\n', '\nx: 2')]);
        expect(lines()).toContain('Starts and ends with a newline.');
        expect(lines()).toContain('Starts with a newline.');
      });

      it('names a CRLF at the edges of each string', () => {
        draw([editing('\r\nx: 1\r\n', '\r\nx: 2')]);
        const lines = [...editRequest().querySelectorAll('p')].map((p) => p.textContent);
        expect(lines).toContain('Starts and ends with a newline.');
        expect(lines).toContain('Starts with a newline.');
      });

      it('says so when the text will be deleted', () => {
        draw([editing('x: 1\n', '')]);
        expect(editRequest()).toHaveTextContent('The text will be deleted.');
        expect(editRequest().querySelectorAll('pre')).toHaveLength(1);
        expect(approve()).toBeEnabled();
      });

      it('says when every occurrence is replaced', () => {
        draw([editing('app:1', 'app:2', true)]);
        expect(editRequest()).toHaveTextContent('Every occurrence in the file.');
      });

      // A tab against spaces draws as the same text, so a change in whitespace
      // alone is said, and only then.
      it('names a change in whitespace alone', () => {
        const { unmount } = draw([editing('\tx: 1', '    x: 1')]);
        expect(editRequest()).toHaveTextContent('The two differ only in whitespace.');
        unmount();

        draw([editing('\tx: 1', '    x: 2')]);
        expect(editRequest()).not.toHaveTextContent('The two differ only in whitespace.');
      });

      // A settled edit reads as its path, and its body is the two strings as on
      // the request, then what the model read.
      it('summarises a settled edit by its path', () => {
        draw([settled('replicas: 1', 'replicas: 3')]);
        const details = document.querySelector('details details')!;
        expect(details.querySelector('summary')!.textContent).toBe('Edit/Users/ana/values.yaml');
        const pres = details.querySelectorAll('pre');
        expect(pres[0]).toHaveTextContent('replicas: 1');
        expect(pres[1]).toHaveTextContent('replicas: 3');
        expect(pres[2]).toHaveTextContent('has been updated successfully');
        expect(details).toHaveTextContent('No newline at either end.');
      });

      // An edit nobody was asked about is in the chat's workspace: its two
      // strings are drawn open under its summary, each folded on its own.
      it('draws an unasked edit open, and not again inside the disclosure', async () => {
        draw([
          msg({
            toolCalls: [
              call({
                name: 'Edit',
                actionKind: 'Edit',
                status: 'Succeeded',
                action: editAction(`${'a: 1\n'.repeat(30)}old: x`, 'replicas: 3'),
                approval: null,
                output: 'The file /Users/ana/values.yaml has been updated successfully.',
              }),
            ],
          }),
        ]);
        const details = document.querySelector('details')!;
        const outside = () => [...document.querySelectorAll('pre')].filter((p) => !p.closest('details'));
        expect(outside()).toHaveLength(2);
        expect(outside()[0]).not.toHaveTextContent('old: x');
        expect(outside()[1]).toHaveTextContent('replicas: 3');
        expect(details.querySelectorAll('pre')).toHaveLength(1);

        await act(async () => {
          fireEvent.click(screen.getByRole('button', { name: /^Show the rest/ }));
        });
        expect(outside()[0]).toHaveTextContent('old: x');
      });
    });

    // The sidecar goes on waiting when the decision never reached it, so the
    // request offers the buttons again rather than sit disabled.
    it('re-enables the buttons and says so when the decision could not be sent', async () => {
      sendMock.mockResolvedValueOnce({ error: { networkError: new Error('sidecar unreachable'), graphQLErrors: [] } });
      draw([waiting()]);
      await act(async () => {
        fireEvent.click(approve());
      });
      expect(approve()).toBeEnabled();
      expect(deny()).toBeEnabled();
      expect(request()).toHaveTextContent('The decision did not reach the sidecar. Try again.');

      await act(async () => {
        fireEvent.click(deny());
      });
      expect(sendMock).toHaveBeenLastCalledWith({ id: 'ap-1', decision: 'Deny' });
      expect(deny()).toBeDisabled();
      expect(screen.queryByText('The decision did not reach the sidecar. Try again.')).toBeNull();
    });

    // false is no turn waiting: a cancel or a settle is already on its way
    // through the watch, and it is what takes the request down.
    it('leaves the buttons down when nothing was waiting', async () => {
      sendMock.mockResolvedValueOnce({ data: { approvalDecide: false } });
      draw([waiting()]);
      await act(async () => {
        fireEvent.click(approve());
      });
      expect(approve()).toBeDisabled();
      expect(deny()).toBeDisabled();
      expect(screen.queryByText('The decision did not reach the sidecar. Try again.')).toBeNull();
    });

    // The row can still say AwaitingApproval a moment after the decision commits,
    // and a window that mounts then must not offer a choice that no longer exists.
    it('holds the buttons down on a decided approval', () => {
      const { unmount } = draw([
        waiting(
          call({
            action: {
              description: '',
              command: {
                text: 'wc -l ~/.kube/config',
                cwd: '/Users/ana',
                background: false,
                sandboxed: false,
                network: false,
                env: [],
              },
              read: null,
              write: null,
              edit: null,
              search: null,
              fetch: null,
              memory: null,
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
            approval: { id: 'ap-1', status: 'Approved', duration: null },
          }),
        ),
      ]);
      expect(approve()).toBeDisabled();
      expect(deny()).toBeDisabled();
      unmount();

      draw([msg({ status: 'Cancelled', content: [], toolCalls: [call()] })]);
      expect(approve()).toBeDisabled();
      expect(deny()).toBeDisabled();
    });

    // The next command's request must not inherit the last one's pressed state.
    it('keys the approval request on its approval', async () => {
      const { rerender } = draw([waiting()]);
      await act(async () => {
        fireEvent.click(approve());
      });
      expect(approve()).toBeDisabled();

      rerender(
        wrap(
          <ChatTranscript
            messages={[
              msg({
                status: 'WaitingApproval',
                content: [],
                toolCalls: [
                  decided({ status: 'Succeeded' }),
                  call({
                    id: 'tc2',
                    action: {
                      description: '',
                      command: {
                        text: 'date',
                        cwd: '/Users/ana',
                        background: false,
                        sandboxed: false,
                        network: false,
                        env: [],
                      },
                      read: null,
                      write: null,
                      edit: null,
                      search: null,
                      fetch: null,
                      memory: null,
                      delegate: null,
                      kubeQuery: null,
                      logsView: null,
                      logsRead: null,
                      logsWatch: null,
                    },
                    approval: { id: 'ap-2', status: 'Pending', duration: null },
                  }),
                ],
              }),
            ]}
            phase="live"
            mode="chat"
            chatID="c1"
            clusterID="1"
            approveArmMs={0}
          />,
        ),
      );
      expect(request().querySelector('pre')).toHaveTextContent('date');
      expect(approve()).toBeEnabled();
      expect(deny()).toBeEnabled();
    });

    // The request is the gate, so a character the eye would not see — a
    // bidirectional override, a zero-width space — is spelled out in a mark,
    // and the same six characters written literally are not.
    it('spells an invisible character in the command out', () => {
      draw([
        waiting(
          call({
            action: {
              description: '',
              command: {
                text: "echo '\\u{202E}' \u{202E}rm -rf ~\u{200B}",
                cwd: '/Users/ana',
                background: false,
                sandboxed: false,
                network: false,
                env: [],
              },
              read: null,
              write: null,
              edit: null,
              search: null,
              fetch: null,
              memory: null,
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
            approval: { id: 'ap-1', status: 'Pending', duration: null },
          }),
        ),
      ]);
      const marks = [...request().querySelectorAll('mark')].map((m) => m.textContent);
      expect(marks).toEqual(['\\u{202E}', '\\u{200B}']);
      expect(request().querySelector('pre')).toHaveTextContent("echo '\\u{202E}' \\u{202E}rm -rf ~\\u{200B}");
    });

    // A command past the fold cannot be approved on what is shown: the rest is
    // one click away, and Approve waits on it.
    it('folds a long command and holds Approve until the rest is shown', async () => {
      const long = `${'\n'.repeat(30)}rm -rf ~`;
      draw([
        waiting(
          call({
            action: {
              description: '',
              command: { text: long, cwd: '/Users/ana', background: false, sandboxed: false, network: false, env: [] },
              read: null,
              write: null,
              edit: null,
              search: null,
              fetch: null,
              memory: null,
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
            approval: { id: 'ap-1', status: 'Pending', duration: null },
          }),
        ),
      ]);
      expect(request().querySelector('pre')).not.toHaveTextContent('rm -rf ~');
      expect(approve()).toBeDisabled();
      const show = screen.getByRole('button', { name: 'Show the rest \u{2014} 15 more characters, 7 more lines' });

      await act(async () => {
        fireEvent.click(show);
      });
      expect(request().querySelector('pre')).toHaveTextContent('rm -rf ~');
      expect(screen.queryByRole('button', { name: /Show the rest/ })).toBeNull();
      expect(approve()).toBeEnabled();
    });

    // Refusing needs no reading: Deny is never held behind the fold.
    it('keeps Deny enabled behind the fold', async () => {
      draw([
        waiting(
          call({
            action: {
              description: '',
              command: {
                text: 'x'.repeat(2001),
                cwd: '/Users/ana',
                background: false,
                sandboxed: false,
                network: false,
                env: [],
              },
              read: null,
              write: null,
              edit: null,
              search: null,
              fetch: null,
              memory: null,
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
            approval: { id: 'ap-1', status: 'Pending', duration: null },
          }),
        ),
      ]);
      expect(screen.getByRole('button', { name: 'Show the rest \u{2014} 1 more characters' })).toBeInTheDocument();
      expect(approve()).toBeDisabled();
      expect(deny()).toBeEnabled();
      await act(async () => {
        fireEvent.click(deny());
      });
      expect(sendMock).toHaveBeenCalledWith({ id: 'ap-1', decision: 'Deny' });
    });

    it('draws a short command whole with no fold', () => {
      draw([waiting()]);
      expect(screen.queryByRole('button', { name: /Show the rest/ })).toBeNull();
      expect(approve()).toBeEnabled();
    });

    // The line naming where the command runs: a muted `in`, then the directory.
    const dirLine = () => screen.getByText('/Users/ana').closest('p')!;
    const follows = (a: Node, b: Node) => a.compareDocumentPosition(b) === Node.DOCUMENT_POSITION_FOLLOWING;

    it('draws the directory under the command, above the buttons', () => {
      draw([waiting()]);
      expect(request()).toContainElement(dirLine());
      expect(dirLine()).toHaveTextContent(/^in\s*\/Users\/ana$/);
      expect(screen.getByText('in')).toHaveClass('text-muted-foreground');
      expect(screen.getByText('/Users/ana')).toHaveClass('font-mono', 'break-all');
      expect(follows(request().querySelector('pre')!, dirLine())).toBe(true);
      expect(follows(dirLine(), approve())).toBe(true);
    });

    // The fold button stays against the command it unfolds; once the rest is
    // shown, the line sits directly under the whole command.
    it('draws the directory after Show the rest on a folded command', async () => {
      const long = `${'\n'.repeat(30)}rm -rf ~`;
      draw([
        waiting(
          call({
            action: {
              description: '',
              command: { text: long, cwd: '/Users/ana', background: false, sandboxed: false, network: false, env: [] },
              read: null,
              write: null,
              edit: null,
              search: null,
              fetch: null,
              memory: null,
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
            approval: { id: 'ap-1', status: 'Pending', duration: null },
          }),
        ),
      ]);
      const show = screen.getByRole('button', { name: /Show the rest/ });
      expect(follows(request().querySelector('pre')!, show)).toBe(true);
      expect(follows(show, dirLine())).toBe(true);

      await act(async () => {
        fireEvent.click(show);
      });
      expect(request().querySelector('pre')!.nextElementSibling).toBe(dirLine());
    });

    it('spells an invisible character in the directory out', () => {
      draw([
        waiting(
          call({
            action: {
              description: '',
              command: {
                text: 'ls',
                cwd: '/tmp/\u{202E}x',
                background: false,
                sandboxed: false,
                network: false,
                env: [],
              },
              read: null,
              write: null,
              edit: null,
              search: null,
              fetch: null,
              memory: null,
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
            approval: { id: 'ap-1', status: 'Pending', duration: null },
          }),
        ),
      ]);
      const line = screen.getByText('in').closest('p')!;
      expect(line.textContent).not.toContain('\u{202E}');
      expect([...line.querySelectorAll('mark')].map((m) => m.textContent)).toEqual(['\\u{202E}']);
    });

    it('lists every variable a command outside the sandbox gets, each spelled out and none folded', () => {
      const env = ['HELM_NAMESPACE=apps', 'NOTE=a\u{202E}b', 'GIT_SSH_COMMAND=ssh\n  rm x\t\n'];
      draw([
        waiting(
          call({
            action: {
              description: '',
              command: { text: 'helm list', cwd: '/srv/app', background: false, sandboxed: false, network: false, env },
              read: null,
              write: null,
              edit: null,
              search: null,
              fetch: null,
              memory: null,
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
            approval: { id: 'ap-1', status: 'Pending', duration: null },
          }),
        ),
      ]);
      const lines = screen.getAllByText('with').map((label) => label.closest('p')!);
      expect(lines.map((l) => l.textContent)).toEqual([
        'withHELM_NAMESPACE=apps',
        'withNOTE=a\\u{202E}b',
        'withGIT_SSH_COMMAND=ssh\n  rm x\\u{9}\\u{A}',
      ]);
      expect(lines[1].querySelector('mark')).toHaveTextContent('\\u{202E}');
      // The newline and indent inside the value are drawn, not collapsed.
      expect(lines[2].querySelector('.whitespace-pre-wrap')).not.toBeNull();
      expect([...lines[2].querySelectorAll('mark')].map((m) => m.textContent)).toEqual(['\\u{9}', '\\u{A}']);
    });

    it('draws no directory line for a call that runs nowhere', () => {
      draw([
        waiting(
          call({
            action: {
              description: '',
              command: { text: 'ls', cwd: '', background: false, sandboxed: false, network: false, env: [] },
              read: null,
              write: null,
              edit: null,
              search: null,
              fetch: null,
              memory: null,
              delegate: null,
              kubeQuery: null,
              logsView: null,
              logsRead: null,
              logsWatch: null,
            },
            approval: { id: 'ap-1', status: 'Pending', duration: null },
          }),
        ),
      ]);
      expect(screen.queryByText('in')).toBeNull();
      expect(screen.queryByText('with')).toBeNull();
    });

    const described = (description: string, text = 'wc -l ~/.kube/config') =>
      waiting(
        call({
          action: {
            description,
            command: { text, cwd: '/Users/ana', background: false, sandboxed: false, network: false, env: [] },
            read: null,
            write: null,
            edit: null,
            search: null,
            fetch: null,
            memory: null,
            delegate: null,
            kubeQuery: null,
            logsView: null,
            logsRead: null,
            logsWatch: null,
          },
          approval: { id: 'ap-1', status: 'Pending', duration: null },
        }),
      );

    // The description is the model's claim, never the app's: labelled, italic,
    // one line whose closing quote survives the ellipsis, and no tooltip, since
    // a native one draws the text unspelled.
    it("draws the model's description above the command, labelled and quoted", () => {
      draw([described('Count the lines\nof the kubeconfig')]);
      expect(request()).toHaveTextContent('The model says:');
      const line = screen.getByText('Count the lines…');
      expect(line).toHaveClass('truncate', 'min-w-0');
      expect(line.previousSibling).toHaveTextContent('“');
      expect(line.nextSibling).toHaveTextContent('”');
      expect(line.closest('.italic')).not.toBeNull();
      expect(request().querySelector('[title]')).toBeNull();
      const pre = request().querySelector('pre')!;
      expect(line.compareDocumentPosition(pre)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    });

    it('spells an invisible character in the description out', () => {
      draw([described('List files\u{202E}gnp.exe')]);
      const marks = [...request().querySelectorAll('mark')].map((m) => m.textContent);
      expect(marks).toEqual(['\\u{202E}']);
      expect(screen.getByText(/List files/)).toHaveTextContent('List files\\u{202E}gnp.exe');
    });

    it('draws no description line for a description with nothing to draw', () => {
      draw([described(' \n ')]);
      expect(request()).not.toHaveTextContent('The model says:');
      expect(request().querySelector('.italic')).toBeNull();
    });

    // The description sits above the command and never folds it away: the
    // command is drawn and held exactly as it is without one.
    it('folds a long command under a description as it does without one', async () => {
      draw([described('Count lines', `${'\n'.repeat(30)}rm -rf ~`)]);
      expect(request().querySelector('pre')).not.toHaveTextContent('rm -rf ~');
      expect(approve()).toBeDisabled();
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /Show the rest/ }));
      });
      expect(request().querySelector('pre')).toHaveTextContent('rm -rf ~');
      expect(approve()).toBeEnabled();
    });

    // jsdom lays nothing out, so the rule is read off the classes: a command with
    // no spaces must break anywhere rather than run past a narrow panel's edge,
    // and nothing may cap its height.
    it('wraps a command with no spaces', () => {
      draw([
        msg({
          status: 'WaitingApproval',
          content: [],
          toolCalls: [
            call({
              id: 'tc0',
              status: 'Succeeded',
              action: {
                description: '',
                command: {
                  text: 'yes',
                  cwd: '/Users/ana',
                  background: false,
                  sandboxed: false,
                  network: false,
                  env: [],
                },
                read: null,
                write: null,
                edit: null,
                search: null,
                fetch: null,
                memory: null,
                delegate: null,
                kubeQuery: null,
                logsView: null,
                logsRead: null,
                logsWatch: null,
              },
              approval: { id: 'ap-0', status: 'Approved', duration: null },
              output: 'y'.repeat(500),
            }),
            call(),
          ],
        }),
      ]);
      const pres = [request().querySelector('pre')!, disclosure('yes').querySelector('pre')!];
      pres.forEach((pre) => {
        expect(pre).toHaveClass('whitespace-pre-wrap', 'break-all');
        expect(pre.className).not.toMatch(/max-h|overflow/);
      });
    });

    it('sends deny for the other button', async () => {
      draw([waiting()]);
      await act(async () => {
        fireEvent.click(deny());
      });
      expect(sendMock).toHaveBeenCalledWith({ id: 'ap-1', decision: 'Deny' });
    });

    it('draws a waiting answer with the spinner and no Stopped line', () => {
      draw([waiting()]);
      expect(screen.getByRole('status', { name: 'Answering' })).toBeInTheDocument();
      expect(screen.queryByText(/^Stopped/)).toBeNull();
    });

    it('draws a finished command as a closed disclosure holding its output', () => {
      draw([msg({ toolCalls: [decided({ status: 'Succeeded', output: '12 /Users/me/.kube/config\n' })] })]);
      const details = disclosure('wc -l ~/.kube/config');
      expect(details.open).toBe(false);
      expect(details).toHaveTextContent('12 /Users/me/.kube/config');
      expect(screen.queryByRole('button', { name: 'Approve' })).toBeNull();
    });

    // The output's first line names the exit code; the tag says only that it failed.
    it('tags a failed command as an error', () => {
      draw([msg({ toolCalls: [decided({ status: 'Failed', output: 'Exit code 1\nno such file\n' })] })]);
      expect(summary('wc -l ~/.kube/config')).toHaveTextContent(/error$/);
      expect(screen.getByText('error')).toHaveClass('text-destructive');
      expect(disclosure('wc -l ~/.kube/config').querySelector('pre')).toHaveTextContent('Exit code 1');
    });

    // The header carries the duration, so the tag reads its prefix.
    it('tags a command stopped at its timeout, keeping what it printed', () => {
      draw([
        msg({
          toolCalls: [
            decided({ status: 'Failed', output: 'Command timed out after 120s (exit code 143)\ntick\ntick\n' }),
          ],
        }),
      ]);
      expect(summary('wc -l ~/.kube/config')).toHaveTextContent(/error$/);
      expect(disclosure('wc -l ~/.kube/config')).toHaveTextContent('Command timed out after 120s');
      expect(disclosure('wc -l ~/.kube/config')).toHaveTextContent('tick');
    });

    it('never tags a successful command whose output imitates a header', () => {
      draw([msg({ toolCalls: [decided({ status: 'Succeeded', output: 'Command timed out after 120s\n' })] })]);
      expect(summary('wc -l ~/.kube/config')).toHaveTextContent(/^Commandwc -l ~\/\.kube\/config$/);
    });

    it('tags a denied command', () => {
      draw([msg({ toolCalls: [decided({ status: 'Denied', output: '{"error":"denied"}' }, 'Denied')] })]);
      expect(summary('wc -l ~/.kube/config')).toHaveTextContent('denied');
    });

    it('draws an unrun and an interrupted command under a cancelled answer, and no request', () => {
      draw([
        msg({
          status: 'Cancelled',
          content: [],
          toolCalls: [
            decided({ status: 'NotRun', output: '{"error":"cancelled"}' }),
            call({
              id: 'tc2',
              status: 'Interrupted',
              action: {
                description: '',
                command: {
                  text: 'sleep 120',
                  cwd: '/Users/ana',
                  background: false,
                  sandboxed: false,
                  network: false,
                  env: [],
                },
                read: null,
                write: null,
                edit: null,
                search: null,
                fetch: null,
                memory: null,
                delegate: null,
                kubeQuery: null,
                logsView: null,
                logsRead: null,
                logsWatch: null,
              },
              approval: { id: 'ap-2', status: 'Approved', duration: null },
              output: '{"error":"cancelled"}',
            }),
          ],
        }),
      ]);
      expect(summary('wc -l ~/.kube/config')).toHaveTextContent('not run');
      expect(summary('sleep 120')).toHaveTextContent('cancelled — may have run');
      expect(screen.queryByRole('button', { name: 'Approve' })).toBeNull();
    });

    it('draws a running command tagged so', () => {
      draw([msg({ status: 'Streaming', content: [], toolCalls: [decided({ status: 'Running' })] })]);
      expect(summary('wc -l ~/.kube/config')).toHaveTextContent('running…');
    });

    // An input bash could not read is refused before any request, so the row has no
    // approval and no action to show: its kind names it.
    it('names a call with no action by its kind', () => {
      draw([
        msg({ toolCalls: [call({ status: 'NotRun', action: null, approval: null, output: '{"error":"bad-input"}' })] }),
      ]);
      expect(summary('Command')).toHaveTextContent('not run');
      expect(summary('Command').querySelector('.italic')).toBeNull();
    });

    // TaskStop has no action to show, ever.
    it('names a stop by its kind', () => {
      draw([
        msg({
          toolCalls: [
            call({
              name: 'TaskStop',
              actionKind: 'Stop',
              status: 'Succeeded',
              action: null,
              approval: null,
              output: 'Stopped t.',
            }),
          ],
        }),
      ]);
      expect(onlySummary().textContent).toBe('Stop task');
    });

    // A sidecar search that did not run is not counted: it is a call, named by its
    // query, with its tag.
    it('summarises a search that did not run by its query', () => {
      const search = (id: string, query: string) =>
        call({
          id,
          name: 'Search',
          actionKind: 'Search',
          status: 'Denied',
          action: {
            description: '',
            command: null,
            read: null,
            write: null,
            edit: null,
            search: { query },
            fetch: null,
            memory: null,
            delegate: null,
            kubeQuery: null,
            logsView: null,
            logsRead: null,
            logsWatch: null,
          },
          approval: null,
        });
      draw([msg({ toolCalls: [search('a', 'k8s 1.34'), search('b', '')] })]);
      expect(summary('k8s 1.34')).toHaveTextContent(/^Searchk8s 1\.34denied$/);
      const [, bare] = document.querySelectorAll('details details > summary');
      expect(bare.textContent).toBe('Searchdenied');
      expect(screen.queryByText(/^Searched the web/)).toBeNull();
    });

    // A command refused before the gate still shows what the model asked for,
    // with no directory, since it never reached the gate that resolves one.
    it('summarises a command that never reached the gate', () => {
      draw([
        msg({
          toolCalls: [
            call({
              status: 'NotRun',
              action: {
                description: 'Clear the scratch',
                command: {
                  text: 'rm -rf /tmp/x',
                  cwd: '',
                  background: false,
                  sandboxed: false,
                  network: false,
                  env: [],
                },
                read: null,
                write: null,
                edit: null,
                search: null,
                fetch: null,
                memory: null,
                delegate: null,
                kubeQuery: null,
                logsView: null,
                logsRead: null,
                logsWatch: null,
              },
              approval: null,
              output: '{"error":"cancelled"}',
            }),
          ],
        }),
      ]);
      expect(summary('Clear the scratch')).toHaveTextContent('not run');
      expect(disclosure('Clear the scratch')).toHaveTextContent('$ rm -rf /tmp/x');
      expect(screen.queryByText('in')).toBeNull();
      expect(screen.queryByRole('button', { name: 'Approve' })).toBeNull();
    });

    const settled = (text: string, description: string) =>
      decided({
        status: 'Failed',
        output: 'Exit code 1\n',
        action: {
          description,
          command: { text, cwd: '/Users/ana', background: false, sandboxed: false, network: false, env: [] },
          read: null,
          write: null,
          edit: null,
          search: null,
          fetch: null,
          memory: null,
          delegate: null,
          kubeQuery: null,
          logsView: null,
          logsRead: null,
          logsWatch: null,
        },
        approval: { id: 'ap-1', status: 'Approved', duration: null },
      });
    const onlySummary = () => document.querySelector('details details > summary')!;

    // Read is ungated, so it has no approval: its summary is the path it read.
    it('summarises a Read from its action, spelled', () => {
      draw([
        msg({
          toolCalls: [
            call({
              name: 'Read',
              actionKind: 'Read',
              status: 'Succeeded',
              action: {
                description: '',
                command: null,
                read: { path: '/data/chats/c1/results/\u{202E}X.txt' },
                write: null,
                edit: null,
                search: null,
                fetch: null,
                memory: null,
                delegate: null,
                kubeQuery: null,
                logsView: null,
                logsRead: null,
                logsWatch: null,
              },
              approval: null,
              output: '     1\ta\n',
            }),
          ],
        }),
      ]);
      expect(onlySummary().textContent).toBe('Read/data/chats/c1/results/\\u{202E}X.txt');
      expect([...onlySummary().querySelectorAll('mark')].map((m) => m.textContent)).toEqual(['\\u{202E}']);
    });

    it('summarises a fetch by its URL', () => {
      draw([
        msg({
          toolCalls: [
            call({
              name: 'WebFetch',
              actionKind: 'Fetch',
              status: 'Succeeded',
              action: {
                description: '',
                command: null,
                read: null,
                write: null,
                edit: null,
                search: null,
                fetch: { url: 'https://kubernetes.io/releases/', host: 'kubernetes.io' },
                memory: null,
                delegate: null,
                kubeQuery: null,
                logsView: null,
                logsRead: null,
                logsWatch: null,
              },
              approval: { id: 'ap-1', status: 'Approved', duration: null },
              output: 'Fetched https://kubernetes.io/releases/ (text/html, 12KB)',
            }),
          ],
        }),
      ]);
      expect(onlySummary().textContent).toBe('Fetch pagehttps://kubernetes.io/releases/');
    });

    // Arguments Read refuses carry no action.
    it('names a Read with no action by its kind', () => {
      draw([
        msg({
          toolCalls: [
            call({ name: 'Read', actionKind: 'Read', status: 'Failed', action: null, approval: null, output: '' }),
          ],
        }),
      ]);
      expect(onlySummary()).toHaveTextContent(/^Read/);
      expect(onlySummary().textContent).toBe('Readerror');
    });

    // The kind is the app's word and leads the row; the model's description follows it,
    // italic, one line. The command waits in the body, so a description cannot hide it
    // from a reader who opens the call.
    it('draws a settled command by its kind and description, the command in its body', () => {
      draw([msg({ toolCalls: [settled('wc -l ~/.kube/config', 'Count lines\nmore')] })]);
      const line = screen.getByText('Count lines…');
      const head = summary('Count lines…');
      expect(head.firstChild).toHaveTextContent(/^Command$/);
      expect(head.firstChild).not.toHaveClass('italic');
      expect(line).toHaveClass('truncate');
      expect(line.closest('.italic')).not.toBeNull();
      expect(head).not.toHaveTextContent('“');
      expect(head).not.toHaveTextContent('wc -l');
      const command = screen.getByText('$ wc -l ~/.kube/config');
      expect(command).toHaveClass('text-foreground');
      expect(disclosure('Count lines…')).toContainElement(command);
      expect(line.compareDocumentPosition(command)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    });

    // The body ends with where the command ran, under what it printed.
    const ranIn = () => screen.getByText(/^Ran in /);
    it("closes a settled command's body with its directory", () => {
      draw([msg({ toolCalls: [settled('wc -l ~/.kube/config', '')] })]);
      const body = disclosure('wc -l ~/.kube/config');
      expect(body).toContainElement(ranIn());
      expect(body.querySelector('summary')).not.toContainElement(ranIn());
      expect(follows(body.querySelector('pre')!, ranIn())).toBe(true);
      expect(ranIn()).toHaveClass('text-muted-foreground/70');
    });

    it("says a sandboxed command's directory was sandboxed", () => {
      const sandboxed = settled('wc -l ~/.kube/config', '');
      sandboxed.action!.command!.sandboxed = true;
      draw([msg({ toolCalls: [sandboxed] })]);
      expect(ranIn().textContent).toBe('Ran in /Users/ana · sandboxed');
      expect(screen.queryByRole('group', { name: 'Command awaiting approval' })).toBeNull();
    });

    it("says a sandboxed command's directory had network", () => {
      const networked = settled('curl -sI https://example.com', '');
      networked.action!.command!.sandboxed = true;
      networked.network = 'Approved';
      draw([msg({ toolCalls: [networked] })]);
      expect(ranIn().textContent).toBe('Ran in /Users/ana · sandboxed, with network');
    });

    it("says nothing of a sandbox on an unconfined command's directory", () => {
      draw([msg({ toolCalls: [settled('wc -l ~/.kube/config', '')] })]);
      expect(ranIn().textContent).toBe('Ran in /Users/ana');
    });

    it('draws no directory line in the body of a call that ran nowhere', () => {
      draw([
        msg({
          toolCalls: [
            decided({
              status: 'Failed',
              action: {
                description: '',
                command: { text: 'ls', cwd: '', background: false, sandboxed: false, network: false, env: [] },
                read: null,
                write: null,
                edit: null,
                search: null,
                fetch: null,
                memory: null,
                delegate: null,
                kubeQuery: null,
                logsView: null,
                logsRead: null,
                logsWatch: null,
              },
              approval: { id: 'ap-1', status: 'Approved', duration: null },
            }),
          ],
        }),
      ]);
      expect(screen.queryByText(/^Ran in /)).toBeNull();
    });

    it('draws a settled command alone when it has no description', () => {
      draw([msg({ toolCalls: [settled('wc -l ~/.kube/config', '')] })]);
      expect(summary('wc -l ~/.kube/config').querySelector('.italic')).toBeNull();
    });

    // A native tooltip draws the raw text, so the summary carries none.
    it('gives the summary no title', () => {
      draw([msg({ toolCalls: [settled('wc -l ~/.kube/config', 'Count lines')] })]);
      expect(onlySummary().querySelector('[title]')).toBeNull();
      expect(onlySummary()).not.toHaveAttribute('title');
    });

    // A right-to-left override would reorder the tag beside it; spelled, it
    // cannot, whether the model put it in the command or the description.
    it('spells an override in the command, keeping the tag in place', () => {
      draw([msg({ toolCalls: [settled('\u{202E}rm -rf ~', '')] })]);
      expect(onlySummary().textContent).not.toContain('\u{202E}');
      expect([...onlySummary().querySelectorAll('mark')].map((m) => m.textContent)).toEqual(['\\u{202E}']);
      expect(onlySummary()).toHaveTextContent(/error$/);
    });

    it('spells an override in the description, keeping the tag in place', () => {
      draw([msg({ toolCalls: [settled('wc -l ~/.kube/config', 'Count\u{202E} lines')] })]);
      expect(onlySummary().textContent).not.toContain('\u{202E}');
      expect([...onlySummary().querySelectorAll('mark')].map((m) => m.textContent)).toEqual(['\\u{202E}']);
      expect(onlySummary()).toHaveTextContent(/error$/);
    });

    it('spells an override in the name of a call its tool could not show', () => {
      draw([msg({ toolCalls: [{ ...settled('wc -l', ''), action: null, actionKind: null, name: '\u{202E}Bash' }] })]);
      expect(onlySummary().textContent).not.toContain('\u{202E}');
      expect([...onlySummary().querySelectorAll('mark')].map((m) => m.textContent)).toEqual(['\\u{202E}']);
    });
  });

  it('heads no answer with the Kubetail mark', () => {
    draw([
      msg({ id: 'q', seq: 0, role: 'User', content: [{ type: 'text', text: 'a question' }] }),
      msg({ id: 'a', seq: 1 }),
    ]);
    expect(screen.queryByText('Kubetail')).toBeNull();
  });

  describe('the model label', () => {
    const answers = (...models: { model: string; providerID: string }[]) =>
      models.map((m, i) => msg({ id: `m${i}`, seq: i, model: m.model, provider: { id: m.providerID, label: '' } }));

    it('labels an answer whose model differs from the one before it', () => {
      draw(answers({ model: 'fake', providerID: 'fake' }, { model: 'claude-opus-5', providerID: 'anthropic' }));
      expect(screen.getByText('Opus 5')).toBeInTheDocument();
      expect(screen.queryByText('Fake model')).toBeNull();
    });

    // Nothing has changed yet, so the first answer says nothing about its model.
    it('leaves the first answer of a chat unlabelled', () => {
      draw(answers({ model: 'claude-opus-5', providerID: 'anthropic' }));
      expect(screen.queryByText('Opus 5')).toBeNull();
    });

    it('leaves a chat that stayed on one model unlabelled', () => {
      draw(answers({ model: 'fake', providerID: 'fake' }, { model: 'fake', providerID: 'fake' }));
      expect(screen.queryByText('Fake model')).toBeNull();
    });

    // The catalog is what has a key now; a chat outlives one being taken away.
    it('falls back to the stored id when the catalog no longer has the model', () => {
      catalog.current = { fetching: false, data: { models: [fake] } };
      draw(answers({ model: 'fake', providerID: 'fake' }, { model: 'claude-opus-5', providerID: 'anthropic' }));
      expect(screen.getByText('claude-opus-5')).toBeInTheDocument();
    });
  });

  it('holds its footer at the bottom of what the scroller shows', () => {
    render(
      wrap(
        <ChatTranscript messages={[msg()]} phase="live" mode="chat" chatID="c1" footer={<p>the composer</p>} />,
        'chat',
      ),
    );
    const footer = screen.getByText('the composer').parentElement!;
    expect(footer).toHaveClass('sticky', 'bottom-0');
    expect(scroller()).toContainElement(footer);
  });

  it('follows a growing answer while the reader is at the bottom', () => {
    const { rerender } = draw([msg({ status: 'Streaming' })]);
    sizeScroller(scroller());

    rerender(grown());
    expect(scroller().scrollTop).toBe(1000);
  });

  it('leaves a reader who has scrolled up where they are', () => {
    const { rerender } = draw([msg({ status: 'Streaming' })]);
    const el = scroller();
    sizeScroller(el);

    // 500 from the end: past the 32px the pin allows.
    el.scrollTop = 100;
    fireEvent.scroll(el);
    rerender(grown());
    expect(el.scrollTop).toBe(100);
  });

  it('resumes following once the reader scrolls back to the bottom', () => {
    const { rerender } = draw([msg({ status: 'Streaming' })]);
    const el = scroller();
    sizeScroller(el);
    el.scrollTop = 100;
    fireEvent.scroll(el);

    el.scrollTop = 580;
    fireEvent.scroll(el);
    rerender(grown());
    expect(el.scrollTop).toBe(1000);
  });
  describe('memory calls', () => {
    const memoryCall = (
      memory: Partial<NonNullable<NonNullable<ChatToolCall['action']>['memory']>>,
      over: Partial<ChatToolCall> = {},
    ) =>
      call({
        name: 'Memory',
        actionKind: 'Memory',
        status: 'Succeeded',
        action: {
          description: '',
          command: null,
          read: null,
          write: null,
          edit: null,
          search: null,
          fetch: null,
          memory: { op: 'save', name: 'pages', body: '', scope: 'cluster', ...memory },
          delegate: null,
          kubeQuery: null,
          logsView: null,
          logsRead: null,
          logsWatch: null,
        },
        approval: null,
        output: '{"saved":"pages"}',
        ...over,
      });
    const summary = () => document.querySelector('details details summary')!;
    const summaryText = () =>
      [...summary().querySelectorAll('.font-medium, .font-mono')].map((e) => e.textContent).join(' ');

    it.each([
      ['save', 'Save memory pages'],
      ['forget', 'Forget memory pages'],
      ['read', 'Memory pages'],
    ])('summarises a %s by the memory it names', (op, want) => {
      draw([msg({ toolCalls: [memoryCall({ op })] })]);
      expect(summaryText()).toBe(want);
    });

    it('shows what a save kept, as text', () => {
      draw([
        msg({
          toolCalls: [
            memoryCall({
              body: 'Treat its pods as <b>prod</b>.',
            }),
          ],
        }),
      ]);
      const disclosure = document.querySelector('details details')!;
      expect(disclosure).not.toHaveTextContent('For this cluster');
      expect(disclosure).not.toHaveTextContent('Type');
      expect(disclosure).not.toHaveTextContent('Description');
      expect(disclosure).toHaveTextContent('Treat its pods as <b>prod</b>.');
      expect(disclosure.querySelector('b')).toBeNull();
    });

    it('draws no body for a save that kept nothing', () => {
      draw([
        msg({
          toolCalls: [memoryCall({ body: 'the refused body' }, { status: 'Failed', output: '{"error":"secret"}' })],
        }),
      ]);
      expect(document.querySelector('details details')).not.toHaveTextContent('the refused body');
    });

    // Closed, a save for every cluster still says so, whatever came of it, so a
    // denied one does not read as a save for the cluster.
    it('names a save for every cluster in its summary', () => {
      const everywhere = { body: 'The user is Bob.', scope: 'everywhere' };
      const { unmount } = draw([msg({ toolCalls: [memoryCall(everywhere)] })]);
      expect(summary()).toHaveTextContent('for every cluster');
      expect(summaryText()).toBe('Save memory pages');
      expect(document.querySelector('details details')).toHaveTextContent('The user is Bob.');
      unmount();

      draw([
        msg({
          toolCalls: [
            memoryCall(everywhere, {
              status: 'Denied',
              approval: { id: 'ap-1', status: 'Denied', duration: null },
              output: '{"error":"denied"}',
            }),
          ],
        }),
      ]);
      expect(summary()).toHaveTextContent('for every cluster');
      expect(document.querySelector('details details')).not.toHaveTextContent('The user is Bob.');
    });

    it('names a forget for every cluster in its summary', () => {
      draw([msg({ toolCalls: [memoryCall({ op: 'forget', scope: 'everywhere' }, { output: '{"forgot":"pages"}' })] })]);
      expect(summaryText()).toBe('Forget memory pages');
      expect(summary()).toHaveTextContent('for every cluster');
    });

    it('says nothing of scope for a save for the cluster', () => {
      draw([msg({ toolCalls: [memoryCall({ body: 'b' })] })]);
      expect(summary()).not.toHaveTextContent('for every cluster');
    });
  });

  describe('cloud requests', () => {
    // A request a sandboxed command sent to a cloud platform, as the sidecar
    // serves it: waiting unless a case says otherwise.
    const cloudRequest = (
      over: Partial<ChatSandboxAction> = {},
      request: Partial<NonNullable<ChatSandboxAction['request']>> = {},
    ): ChatSandboxAction => ({
      approval: { id: 'c-1', status: 'Pending', duration: null },
      action: {
        target: 'AWS',
        summary: 'AWS: call ec2:TerminateInstances in eu-west-1',
        accessLevel: 'Write',
        principal: 'Chat',
        scope: '123456789012/eu-west-1',
        accountName: '123456789012',
        service: 'ec2',
        operation: 'TerminateInstances',
        resource: '',
        name: '',
        once: true,
        grantable: false,
        commandRule: '',
        chatRule: '',
      },
      request: {
        __typename: 'CloudRequest',
        host: 'ec2.eu-west-1.amazonaws.com',
        method: 'POST',
        path: '/?Action=TerminateInstances',
        contentType: 'application/x-www-form-urlencoded',
        body: 'InstanceId.1=i-0abc\u{202E}',
        unshown: '',
        details: [
          { label: 'Region', value: 'eu-west-1' },
          { label: 'Account', value: '1234\u{200B}' },
        ],
        ...request,
      } as ChatSandboxAction['request'],
      reason: null,
      ...over,
    });
    const waitingOn = (actions: ChatSandboxAction[]) =>
      msg({
        status: 'WaitingApproval',
        awaitingApproval: true,
        content: [],
        toolCalls: [call({ status: 'Running', approval: null, sandboxActions: actions })],
      });

    // The summary heads it, then the host, each detail, and the raw request
    // open: method, path, media type and body, every string spelled.
    it('draws the summary, host, details and request through VisibleText', () => {
      draw([waitingOn([cloudRequest()])]);
      const request = screen.getByRole('group', { name: 'AWS change awaiting approval' });
      expect(request).toHaveTextContent('AWS: call ec2:TerminateInstances in eu-west-1');
      expect(request).toHaveTextContent('ec2.eu-west-1.amazonaws.com');
      expect(request).toHaveTextContent('Region');
      expect(request).toHaveTextContent('eu-west-1');
      expect(request).toHaveTextContent('/?Action=TerminateInstances');
      expect(request).toHaveTextContent('POST application/x-www-form-urlencoded');
      expect(request).toHaveTextContent('InstanceId.1=i-0abc');
      const marks = [...request.querySelectorAll('mark')].map((m) => m.textContent);
      expect(marks).toEqual(expect.arrayContaining(['\\u{202E}', '\\u{200B}']));
      expect(
        within(request)
          .getAllByRole('button')
          .map((b) => b.textContent),
      ).toEqual(['Approve once', 'Deny']);
    });

    it('labels a data read, and names each cloud platform in words', () => {
      draw([
        waitingOn([
          cloudRequest({ action: { ...cloudRequest().action!, accessLevel: 'DataRead' } }),
          cloudRequest({
            approval: { id: 'c-2', status: 'Pending', duration: null },
            action: { ...cloudRequest().action!, target: 'GCP' },
          }),
        ]),
      ]);
      expect(screen.getByRole('group', { name: 'AWS data read awaiting approval' })).toBeInTheDocument();
      expect(screen.getByRole('group', { name: 'Google Cloud change awaiting approval' })).toBeInTheDocument();
    });

    it("draws the cloud platform's sentence in the body's place", () => {
      draw([waitingOn([cloudRequest({}, { body: '', unshown: 'A Git pack of the commits pushed.' })])]);
      const request = screen.getByRole('group', { name: 'AWS change awaiting approval' });
      expect(request).toHaveTextContent('A Git pack of the commits pushed.');
    });

    // A body behind the cloud platform's sentence is never drawn, so it holds
    // nothing back.
    it('holds Approve on no body the sentence stands for', () => {
      draw([
        waitingOn([cloudRequest({}, { body: 'line\n'.repeat(200), unshown: 'A Git pack of the commits pushed.' })]),
      ]);
      const request = screen.getByRole('group', { name: 'AWS change awaiting approval' });
      expect(request).not.toHaveTextContent('line');
      expect(within(request).getByRole('button', { name: 'Approve once' })).toBeEnabled();
    });

    // A grantable cloud platform request offers every duration, as a
    // cluster write does, each with the rule it adds through VisibleText.
    it('offers every allow answer on a grantable cloud platform request', () => {
      const action = {
        ...cloudRequest().action!,
        once: false,
        grantable: true,
        commandRule: 'Allow StartInstances of ec2 "" in AWS 123456789012 / eu-west-1 for this command',
        chatRule: 'Allow writes in AWS 123456789012 / eu-west-1\u{202E}',
      };
      draw([waitingOn([cloudRequest({ action })])]);
      const request = screen.getByRole('group', { name: 'AWS change awaiting approval' });
      expect(
        within(request)
          .getAllByRole('button')
          .map((b) => b.textContent),
      ).toEqual(['Approve once', 'Allow for this command', 'Allow for this chat', 'Always allow', 'Deny']);
      expect(within(request).getByRole('button', { name: 'Always allow' }).parentElement).toHaveTextContent(
        'Allow writes in AWS 123456789012 / eu-west-1\\u{202E}',
      );
    });

    // One no answer may grant offers Once and Deny alone.
    it('offers no allow answer on a cloud platform request that is not grantable', () => {
      draw([waitingOn([cloudRequest()])]);
      const request = screen.getByRole('group', { name: 'AWS change awaiting approval' });
      expect(
        within(request)
          .getAllByRole('button')
          .map((b) => b.textContent),
      ).toEqual(['Approve once', 'Deny']);
    });

    // A request it cannot draw is still headed by its target and class.
    it('heads a data read it cannot draw as a data read', () => {
      const action = { ...cloudRequest().action!, accessLevel: 'DataRead' as const };
      draw([waitingOn([cloudRequest({ action, request: null })])]);
      const request = screen.getByRole('group', { name: 'AWS data read awaiting approval' });
      expect(request).toHaveTextContent("This request can't be shown.");
    });

    // A long body folds, and Approve waits until it is shown.
    it('folds a long body and holds Approve until it is shown', async () => {
      draw([waitingOn([cloudRequest({}, { body: 'line\n'.repeat(200) })])]);
      const request = screen.getByRole('group', { name: 'AWS change awaiting approval' });
      expect(within(request).getByRole('button', { name: 'Approve once' })).toBeDisabled();
      await act(async () => {
        fireEvent.click(within(request).getByRole('button', { name: /^Show/ }));
      });
      expect(within(request).getByRole('button', { name: 'Approve once' })).toBeEnabled();
    });

    // A settled action this build cannot read draws no request, tagged as
    // the cluster's.
    it('draws a settled action it cannot read as an empty line', () => {
      draw([
        msg({
          toolCalls: [
            call({
              status: 'Succeeded',
              approval: null,
              sandboxActions: [
                {
                  approval: { id: 'c-1', status: 'Denied', duration: null },
                  action: null,
                  request: null,
                  reason: null,
                },
              ],
            }),
          ],
        }),
      ]);
      expect(screen.getByText('denied')).toBeInTheDocument();
    });

    // A settled request is one line tagged with its cloud platform and the
    // decision.
    it("tags a settled request with its cloud platform's name", () => {
      draw([
        msg({
          toolCalls: [
            call({
              status: 'Succeeded',
              approval: null,
              sandboxActions: [
                cloudRequest({ approval: { id: 'c-1', status: 'Denied', duration: null } }, { body: '' }),
              ],
            }),
          ],
        }),
      ]);
      expect(document.body).toHaveTextContent(
        'POST ec2.eu-west-1.amazonaws.com/?Action=TerminateInstancesAWS · denied',
      );
    });
  });

  describe('cluster writes', () => {
    // A write a sandboxed command sent, as the sidecar serves it: waiting unless a
    // case says otherwise.
    // Spelled flat, as a case overrides it; an empty method is an action with
    // no request.
    type FlatWrite = Omit<ChatSandboxAction, 'request'> & Omit<ChatClusterRequest, '__typename'>;
    const clusterWrite = (over: Partial<FlatWrite> = {}): ChatSandboxAction => {
      const { approval, action, reason, ...request }: FlatWrite = {
        approval: { id: 'w-1', status: 'Pending', duration: null },
        action: {
          target: 'Kubernetes',
          summary: 'Delete pods/x in web on dev',
          accessLevel: 'Write',
          principal: 'Chat',
          scope: 'c1/web',
          accountName: 'dev',
          service: '',
          operation: 'delete',
          resource: 'pods',
          name: 'x',
          once: false,
          grantable: true,
          commandRule: 'Allow delete of core pods in dev / web for this command',
          chatRule: 'Allow writes in dev / web',
        },
        method: 'DELETE',
        path: '/api/v1/namespaces/web/pods/x',
        subresource: '',
        contentType: 'application/json',
        body: '{"propagationPolicy":"Background"}',
        dryRun: false,
        diff: '',
        diffCut: false,
        diffError: '',
        reason: null,
        ...over,
      };
      return {
        approval,
        action,
        reason,
        request: request.method === '' ? null : { __typename: 'ClusterWrite', ...request },
      };
    };
    // The sandboxed command that sent the writes, running while one waits.
    const sender = (writes: ChatSandboxAction[], over: Partial<ChatToolCall> = {}) =>
      call({ status: 'Running', approval: null, sandboxActions: writes, ...over });
    const waitingOn = (writes: ChatSandboxAction[], over: Partial<ChatToolCall> = {}) =>
      msg({ status: 'WaitingApproval', awaitingApproval: true, content: [], toolCalls: [sender(writes, over)] });
    const request = () => screen.getByRole('group', { name: 'Cluster change awaiting approval' });
    const approve = () => screen.getByRole('button', { name: 'Approve once' });

    // With no diff the request is the request itself: the action's summary as
    // its heading, then the path and query as sent, and the body, then the
    // command that sent it.
    it('draws the summary, the path, the body and the command under Sent by', async () => {
      draw([waitingOn([clusterWrite({ path: '/api/v1/namespaces/web/pods/x\u{202E}' })])]);

      expect(request()).toHaveTextContent('Delete pods/x in web on dev');
      expect(request()).not.toHaveTextContent('DELETE from');
      expect(request()).not.toHaveTextContent('(dry run)');
      expect(request().querySelector('mark')).toHaveTextContent('\\u{202E}');
      const [path, method, body, command] = request().querySelectorAll('pre, p.font-mono');
      expect(path).toHaveTextContent('/api/v1/namespaces/web/pods/x');
      expect(method).toHaveTextContent('DELETE application/json');
      expect(body).toHaveTextContent('{"propagationPolicy":"Background"}');
      expect(request()).toHaveTextContent('Sent by');
      expect(command).toHaveTextContent('wc -l ~/.kube/config');
      expect(screen.queryByRole('group', { name: 'Command awaiting approval' })).toBeNull();
      expect(approve()).toBeEnabled();

      await act(async () => {
        fireEvent.click(approve());
      });
      expect(sendMock).toHaveBeenCalledWith({ id: 'w-1', decision: 'Once' });
    });

    it("ends a dry run's heading with (dry run)", () => {
      draw([
        waitingOn([
          clusterWrite({
            method: 'POST',
            path: '/apis/example.com/v1/widgets?dryRun=All',
            dryRun: true,
            action: { ...clusterWrite().action!, summary: 'Create widgets on dev', grantable: false },
          }),
        ]),
      ]);

      expect(request()).toHaveTextContent('Create widgets on dev (dry run)');
    });

    // A change to an object that exists is read as a diff, with the request
    // itself one fold away, which Approve does not wait on.
    it('draws a diff over the request, folded under Show the request', async () => {
      draw([
        waitingOn([
          clusterWrite({
            method: 'PATCH',
            diff: '@@ -1 +1 @@\n-  k: old\n+  k: new\n',
            body: `${'a: 1\n'.repeat(30)}x`,
          }),
        ]),
      ]);

      const diff = request().querySelector('pre')!;
      expect(diff.querySelector('.diff-del')?.textContent).toBe('-  k: old\n');
      expect(diff.querySelector('.diff-add')?.textContent).toBe('+  k: new\n');
      const raw = screen.getByText('Show the request').closest('details')!;
      expect(raw.open).toBe(false);
      expect(raw).toHaveTextContent('/api/v1/namespaces/web/pods/x');
      expect(approve()).toBeEnabled();
    });

    it('holds Approve until a long diff is shown', async () => {
      draw([waitingOn([clusterWrite({ method: 'PATCH', diff: '+ a\n'.repeat(40) })])]);
      expect(approve()).toBeDisabled();
      await act(async () => {
        fireEvent.click(within(request()).getAllByRole('button', { name: /^Show the rest/ })[0]);
      });
      expect(approve()).toBeEnabled();
    });

    // A diff cut short is not the whole change: the request is drawn open
    // under it, and Approve waits on both folds.
    it('draws the request open under a cut diff, and waits on both', async () => {
      draw([
        waitingOn([
          clusterWrite({
            method: 'PATCH',
            diff: `${'+ a\n'.repeat(40)}… 3 more lines not shown\n`,
            diffCut: true,
            body: `${'a: 1\n'.repeat(30)}x`,
          }),
        ]),
      ]);
      expect(screen.queryByText('Show the request')).toBeNull();
      expect(screen.getByLabelText('Method and media type')).toBeInTheDocument();
      const shows = () => within(request()).queryAllByRole('button', { name: /^Show the rest/ });
      expect(shows()).toHaveLength(2);
      await act(async () => {
        fireEvent.click(shows()[0]);
      });
      expect(approve()).toBeDisabled();
      await act(async () => {
        fireEvent.click(shows()[0]);
      });
      expect(approve()).toBeEnabled();
    });

    it('says why there is no preview, and stays approvable', () => {
      draw([waitingOn([clusterWrite({ method: 'PATCH', diffError: 'The dry run failed: denied \u200b' })])]);
      expect(request()).toHaveTextContent('No preview: The dry run failed: denied');
      expect(request().querySelector('mark')).not.toBeNull();
      expect(approve()).toBeEnabled();
    });

    // A grantable action offers five answers, each allow with the rule it adds
    // in the words Settings uses.
    it('offers five answers, with the rule under each allow', async () => {
      draw([waitingOn([clusterWrite()])]);
      const names = within(request())
        .getAllByRole('button')
        .map((b) => b.textContent);
      expect(names).toEqual(['Approve once', 'Allow for this command', 'Allow for this chat', 'Always allow', 'Deny']);
      expect(screen.getByRole('button', { name: 'Allow for this command' }).parentElement).toHaveTextContent(
        'Allow delete of core pods in dev / web for this command',
      );
      ['Allow for this chat', 'Always allow'].forEach((name) => {
        expect(screen.getByRole('button', { name }).parentElement).toHaveTextContent('Allow writes in dev / web');
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: 'Allow for this command' }));
      });
      expect(sendMock).toHaveBeenLastCalledWith({ id: 'w-1', decision: 'Command' });
    });

    // An answer that writes a rule names the rules' type, so the lists of them
    // are asked again.
    it.each([
      ['Allow for this chat', 'Chat'],
      ['Always allow', 'Always'],
    ] as const)('sends %s as %s, naming the rules type', async (name, decision) => {
      draw([waitingOn([clusterWrite()])]);
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name }));
      });
      expect(sendMock).toHaveBeenLastCalledWith({ id: 'w-1', decision }, { additionalTypenames: ['PermissionRule'] });
    });

    it('sends Deny as Deny', async () => {
      draw([waitingOn([clusterWrite()])]);
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: 'Deny' }));
      });
      expect(sendMock).toHaveBeenLastCalledWith({ id: 'w-1', decision: 'Deny' });
    });

    it('arms the four allow answers by their place, and Deny at once', () => {
      draw([waitingOn([clusterWrite()])], { approveArmMs: 500, sandboxAvailable: false });
      ['Approve once', 'Allow for this command', 'Allow for this chat', 'Always allow'].forEach((name) => {
        expect(screen.getByRole('button', { name })).toBeDisabled();
      });
      expect(screen.getByRole('button', { name: 'Deny' })).toBeEnabled();
    });

    it('offers Approve once and Deny alone for an action no rule may allow', () => {
      draw([
        waitingOn([
          clusterWrite({ action: { ...clusterWrite().action!, accessLevel: 'Destructive', grantable: false } }),
        ]),
      ]);
      const names = within(request())
        .getAllByRole('button')
        .map((b) => b.textContent);
      expect(names).toEqual(['Approve once', 'Deny']);
    });

    // A refused Always says the sidecar's reason, leaves the request waiting,
    // and still takes Approve once.
    it('says why Always was refused, and Approve once still sends', async () => {
      sendMock.mockResolvedValueOnce({
        error: {
          graphQLErrors: [
            {
              message: 'allows a destructive write, which always asks',
              extensions: { code: 'KUBETAIL_VALIDATION_ERROR' },
            },
          ],
        },
      });
      draw([waitingOn([clusterWrite()])]);
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: 'Always allow' }));
      });
      expect(request()).toHaveTextContent('allows a destructive write, which always asks');
      expect(request()).not.toHaveTextContent('The decision did not reach the sidecar.');
      await act(async () => {
        fireEvent.click(approve());
      });
      expect(sendMock).toHaveBeenLastCalledWith({ id: 'w-1', decision: 'Once' });
    });

    it("draws a rule's line through VisibleText", () => {
      draw([
        waitingOn([
          clusterWrite({
            action: { ...clusterWrite().action!, chatRule: 'Allow writes in "say \\"hi\\"\u200b"' },
          }),
        ]),
      ]);
      const line = screen.getByRole('button', { name: 'Allow for this chat' }).parentElement!;
      expect(line).toHaveTextContent('Allow writes in "say \\"hi\\"');
      expect(line.querySelector('mark')).not.toBeNull();
    });

    // An action with no request of its own and no kind this step draws is not
    // approved: what the user cannot see is not.
    it('offers Deny alone for an action with no write', () => {
      draw([waitingOn([clusterWrite({ method: '', path: '', body: '', contentType: '' })])]);
      expect(request()).toHaveTextContent("This request can't be shown.");
      expect(
        within(request())
          .getAllByRole('button')
          .map((b) => b.textContent),
      ).toEqual(['Deny']);
    });

    // A write whose stored request this build cannot read has no action and no
    // request. The command that sent it is still drawn, and nothing approves
    // what is not.
    it('offers Deny alone for a write it cannot read', () => {
      draw([waitingOn([clusterWrite({ action: null, method: '', path: '', body: '', contentType: '' })])]);
      // With no action, nothing says whose request it is.
      const unread = screen.getByRole('group', { name: 'Request awaiting approval' });
      expect(unread).toHaveTextContent("This request can't be shown.");
      expect(document.body).toHaveTextContent('wc -l ~/.kube/config');
      expect(
        within(unread)
          .getAllByRole('button')
          .map((b) => b.textContent),
      ).toEqual(['Deny']);
    });

    // The same body means different things as a merge patch and a strategic
    // one: the request says the method and the media type.
    it('draws the method and the media type under the path', () => {
      draw([
        waitingOn([
          clusterWrite({
            method: 'PUT',
            path: '/apis/example.com/v1/namespaces/web/widgets/x/status',
            subresource: 'status',
            contentType: 'application/strategic-merge-patch+json',
            body: '{}',
          }),
        ]),
      ]);
      expect(screen.getByLabelText('Method and media type')).toHaveTextContent(
        'PUT application/strategic-merge-patch+json',
      );
    });

    it('draws the method alone for a write with no body', () => {
      draw([waitingOn([clusterWrite({ body: '', contentType: '' })])]);
      expect(screen.getByLabelText('Method and media type')).toHaveTextContent(/^DELETE$/);
    });

    it('draws no body for a write that carries none', () => {
      draw([waitingOn([clusterWrite({ body: '', contentType: '' })])]);
      expect(request().querySelectorAll('pre')).toHaveLength(1);
    });

    // The command is context: Approve arms with it folded. The body is what is
    // approved: Approve waits on it.
    it('arms with the command folded, and waits on a folded body', async () => {
      const longCommand = `${'echo x\n'.repeat(30)}kubectl delete pod x`;
      const long = { ...call().action!, command: { ...call().action!.command!, text: longCommand } };
      const { unmount } = draw([waitingOn([clusterWrite()], { action: long })]);
      expect(request()).not.toHaveTextContent('kubectl delete pod x');
      expect(approve()).toBeEnabled();
      unmount();

      draw([waitingOn([clusterWrite({ method: 'PUT', body: `${'a: 1\n'.repeat(30)}secret: x` })])]);
      expect(approve()).toBeDisabled();
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /^Show the rest/ }));
      });
      expect(approve()).toBeEnabled();
    });

    it("opens a subagent's write with whose it is", () => {
      const agentCall = call({
        id: 'ag1',
        name: 'Agent',
        actionKind: 'Delegate',
        status: 'Succeeded',
        approval: null,
        action: { ...call().action!, description: 'Delete the pod', command: null },
        background: { status: 'Running', exitCode: null, report: '', matched: [], endedBecause: '', viewAround: null },
      });
      draw([
        msg({
          awaitingApproval: true,
          toolCalls: [agentCall, sender([clusterWrite()], { id: 'cc1', agentCallID: 'ag1' })],
        }),
      ]);
      expect(request()).toHaveTextContent('An agent asks:');
    });

    // A write that no longer waits is a line of its call's disclosure, tagged
    // with what became of it; one a crash stranded was not answered.
    it("lists a call's writes that no longer wait in its disclosure", () => {
      draw([
        msg({
          content: [],
          toolCalls: [
            sender(
              [
                clusterWrite({
                  approval: { id: 'w-1', status: 'Approved', duration: null },
                  path: '/api/v1/namespaces/web/pods/a',
                  body: '',
                }),
                clusterWrite({
                  approval: { id: 'w-2', status: 'Denied', duration: null },
                  method: 'PATCH',
                  path: '/api/v1/namespaces/web/pods/b',
                  body: '',
                }),
                clusterWrite({
                  approval: { id: 'w-3', status: 'Abandoned', duration: null },
                  path: '/api/v1/namespaces/web/pods/c',
                  body: '',
                }),
                clusterWrite({
                  approval: { id: 'w-4', status: 'Pending', duration: null },
                  path: '/api/v1/namespaces/web/pods/d',
                  body: '',
                }),
              ],
              { status: 'Interrupted' },
            ),
          ],
        }),
      ]);
      const lines = [...screen.getByText('wc -l ~/.kube/config').closest('details')!.querySelectorAll('li')].map(
        (li) => li.textContent,
      );
      expect(lines).toEqual([
        'DELETE /api/v1/namespaces/web/pods/aapproved',
        'PATCH /api/v1/namespaces/web/pods/bdenied',
        'DELETE /api/v1/namespaces/web/pods/cnot answered',
        'DELETE /api/v1/namespaces/web/pods/dnot answered',
      ]);
      expect(screen.queryByRole('group', { name: 'Cluster change awaiting approval' })).toBeNull();
    });

    // A read of Secret data the proxy asks about, as the sidecar serves it.
    const secretRead = (over: Partial<FlatWrite> = {}): ChatSandboxAction =>
      clusterWrite({
        action: {
          target: 'Kubernetes',
          summary: 'Show Secret db in web on dev',
          accessLevel: 'DataRead',
          principal: 'Chat',
          scope: 'c1/web',
          accountName: 'dev',
          service: '',
          operation: 'get',
          resource: 'secrets',
          name: 'db',
          once: false,
          grantable: true,
          commandRule: 'Allow get of core secrets in dev / web for this command',
          chatRule: 'Allow data reads in dev / web',
        },
        method: 'GET',
        path: '/api/v1/namespaces/web/secrets/db',
        contentType: '',
        body: '',
        ...over,
      });

    // A Secret read is drawn as a change is: its summary as the heading, the
    // path and GET, and the five answers with their rules; only its label
    // differs.
    it('draws a waiting Secret read under its own label', () => {
      draw([waitingOn([secretRead()])]);
      const read = screen.getByRole('group', { name: 'Secret read awaiting approval' });
      expect(screen.queryByRole('group', { name: 'Cluster change awaiting approval' })).toBeNull();
      expect(read).toHaveTextContent('Show Secret db in web on dev');
      const [path, method] = read.querySelectorAll('pre, p.font-mono');
      expect(path).toHaveTextContent('/api/v1/namespaces/web/secrets/db');
      expect(method.textContent).toBe('GET');
      expect(
        within(read)
          .getAllByRole('button')
          .map((b) => b.textContent),
      ).toEqual(['Approve once', 'Allow for this command', 'Allow for this chat', 'Always allow', 'Deny']);
      expect(screen.getByRole('button', { name: 'Allow for this chat' }).parentElement).toHaveTextContent(
        'Allow data reads in dev / web',
      );
    });

    it('names the context alone for a read across the cluster', () => {
      draw([
        waitingOn([
          secretRead({
            path: '/api/v1/secrets',
            action: {
              ...secretRead().action!,
              scope: 'c1',
              operation: 'list',
              chatRule: 'Allow data reads of core secrets in dev',
            },
          }),
        ]),
      ]);
      expect(screen.getByRole('button', { name: 'Always allow' }).parentElement).toHaveTextContent(
        /^Always allowAllow data reads of core secrets in dev$/,
      );
    });

    // A read that ran is on screen with what decided it, so one that showed
    // Secret data is never silent.
    it("lists a call's settled Secret reads with their tags", () => {
      draw([
        msg({
          content: [],
          toolCalls: [
            sender(
              [
                secretRead({ approval: { id: 'r-1', status: 'Approved', duration: 'Chat' } }),
                secretRead({ approval: { id: 'r-2', status: 'Denied', duration: null } }),
                secretRead({ approval: { id: 'r-3', status: 'Allowed', duration: null }, reason: 'auto mode' }),
                secretRead({
                  approval: { id: 'r-4', status: 'Refused', duration: null },
                  reason: 'a rule denies it: Deny Secret reads in dev',
                }),
              ],
              { status: 'Succeeded' },
            ),
          ],
        }),
      ]);
      const items = [...screen.getByText('wc -l ~/.kube/config').closest('details')!.querySelectorAll('li')];
      expect(items.map((li) => li.textContent)).toEqual([
        'GET /api/v1/namespaces/web/secrets/dbapproved · this chat',
        'GET /api/v1/namespaces/web/secrets/dbdenied',
        'GET /api/v1/namespaces/web/secrets/dballowedauto mode',
        'GET /api/v1/namespaces/web/secrets/dbrefuseda rule denies it: Deny Secret reads in dev',
      ]);
    });

    // An approval says how long it holds.
    it('tags an approved write with its duration', () => {
      draw([
        msg({
          content: [],
          toolCalls: [
            sender(
              (['Once', 'Command', 'Chat', 'Always'] as const).map((duration, i) =>
                clusterWrite({ approval: { id: `w-${i}`, status: 'Approved', duration }, body: '' }),
              ),
              { status: 'Succeeded' },
            ),
          ],
        }),
      ]);
      const tags = [...screen.getByText('wc -l ~/.kube/config').closest('details')!.querySelectorAll('li')].map(
        (li) => li.textContent,
      );
      expect(tags).toEqual([
        'DELETE /api/v1/namespaces/web/pods/xapproved · once',
        'DELETE /api/v1/namespaces/web/pods/xapproved · this command',
        'DELETE /api/v1/namespaces/web/pods/xapproved · this chat',
        'DELETE /api/v1/namespaces/web/pods/xapproved · always',
      ]);
    });

    // A write the engine decided with nobody asked is tagged with what it
    // decided and why; a dry run says so after its path.
    it('tags a write nobody was asked about with its reason', () => {
      draw([
        msg({
          content: [],
          toolCalls: [
            sender(
              [
                clusterWrite({
                  approval: { id: 'w-1', status: 'Allowed', duration: null },
                  path: '/api/v1/namespaces/web/pods/a',
                  body: '',
                  reason: 'auto mode',
                }),
                clusterWrite({
                  approval: { id: 'w-2', status: 'Refused', duration: null },
                  path: '/api/v1/namespaces/web/pods/b',
                  body: '',
                  reason: 'this cluster is read-only',
                }),
                clusterWrite({
                  approval: { id: 'w-3', status: 'Allowed', duration: null },
                  method: 'PATCH',
                  path: '/api/v1/namespaces/web/pods/c?dryRun=All',
                  body: '',
                  dryRun: true,
                  reason: 'it changes nothing',
                }),
              ],
              { status: 'Succeeded' },
            ),
          ],
        }),
      ]);
      const items = [...screen.getByText('wc -l ~/.kube/config').closest('details')!.querySelectorAll('li')];
      expect(items.map((li) => li.textContent)).toEqual([
        'DELETE /api/v1/namespaces/web/pods/aallowedauto mode',
        'DELETE /api/v1/namespaces/web/pods/brefusedthis cluster is read-only',
        'PATCH /api/v1/namespaces/web/pods/c?dryRun=All (dry run)allowedit changes nothing',
      ]);
      items.forEach((li) => expect(li.querySelector('[title]')).toBeNull());
    });

    it('lists no write that still waits', () => {
      draw([
        waitingOn([
          clusterWrite({ approval: { id: 'w-1', status: 'Approved', duration: null }, body: '' }),
          clusterWrite({ approval: { id: 'w-2', status: 'Pending', duration: null } }),
        ]),
      ]);
      // The command is also under Sent by, so the disclosure is found by its tag.
      const lines = screen.getByText('running…').closest('details')!.querySelectorAll('li');
      expect(lines).toHaveLength(1);
      expect(request()).toBeInTheDocument();
    });
  });

  describe('background commands', () => {
    const background = (over: Partial<ChatToolCall> = {}) =>
      call({
        action: {
          description: '',
          command: {
            text: 'make serve',
            cwd: '/Users/ana',
            background: true,
            sandboxed: false,
            network: false,
            env: [],
          },
          read: null,
          write: null,
          edit: null,
          search: null,
          fetch: null,
          memory: null,
          delegate: null,
          kubeQuery: null,
          logsView: null,
          logsRead: null,
          logsWatch: null,
        },
        approval: { id: 'ap-1', status: 'Approved', duration: null },
        status: 'Succeeded',
        output: 'Command running in background with ID: t1.',
        background: { status: 'Running', exitCode: null, report: '', matched: [], endedBecause: '', viewAround: null },
        ...over,
      });
    const summary = () => screen.getByText('make serve').closest('summary')!;
    const stop = () => screen.getByRole('button', { name: 'Stop' });

    it('says on the approval request that the command keeps running', () => {
      draw([
        msg({
          status: 'WaitingApproval',
          content: [],
          toolCalls: [
            call({
              action: {
                description: '',
                command: {
                  text: 'make serve',
                  cwd: '/Users/ana',
                  background: true,
                  sandboxed: false,
                  network: false,
                  env: [],
                },
                read: null,
                write: null,
                edit: null,
                search: null,
                fetch: null,
                memory: null,
                delegate: null,
                kubeQuery: null,
                logsView: null,
                logsRead: null,
                logsWatch: null,
              },
              approval: { id: 'ap-1', status: 'Pending', duration: null },
            }),
          ],
        }),
      ]);
      const request = screen.getByRole('group', { name: 'Command awaiting approval' });
      expect(request).toHaveTextContent('Run this command in the background?');
      expect(request).toHaveTextContent('It keeps running after this answer, until it exits or you stop it.');
      expect(request).not.toHaveTextContent('Run this command?');
    });

    it('tags a running background command and offers Stop', async () => {
      draw([msg({ toolCalls: [background()] })]);
      expect(summary()).toHaveTextContent('running in background');

      await act(async () => {
        fireEvent.click(stop());
      });
      expect(sendMock).toHaveBeenCalledWith({ id: 'tc1' });
      expect(stop()).toBeDisabled();
    });

    // false is no task running: its end is already on its way through the watch.
    it('keeps Stop down on a false answer and hands it back on an error', async () => {
      sendMock.mockResolvedValueOnce({ data: { backgroundTaskStop: false } });
      const { unmount } = draw([msg({ toolCalls: [background()] })]);
      await act(async () => {
        fireEvent.click(stop());
      });
      expect(stop()).toBeDisabled();
      unmount();

      sendMock.mockResolvedValueOnce({ error: { networkError: new Error('sidecar unreachable'), graphQLErrors: [] } });
      draw([msg({ toolCalls: [background()] })]);
      await act(async () => {
        fireEvent.click(stop());
      });
      expect(stop()).toBeEnabled();
    });

    it('tags an ended background command by how it ended, with no Stop', () => {
      (
        [
          [{ status: 'Exited', exitCode: 3, report: '' }, 'error'],
          [{ status: 'Exited', exitCode: 0, report: '' }, ''],
          [{ status: 'Stopped', exitCode: null, report: '' }, 'stopped'],
          [{ status: 'Lost', exitCode: null, report: '' }, 'lost'],
          [{ status: 'Matched', exitCode: null, report: '' }, ''],
          [{ status: 'Expired', exitCode: null, report: '' }, 'expired'],
        ] as const
      ).forEach(([task, tag]) => {
        const { unmount } = draw([
          msg({
            toolCalls: [background({ background: { ...task, matched: [], endedBecause: '', viewAround: null } })],
          }),
        ]);
        expect(summary()).toHaveTextContent(new RegExp(`^Commandmake serve${tag}$`));
        expect(screen.queryByRole('button', { name: 'Stop' })).toBeNull();
        unmount();
      });
    });

    const notice = (over: Record<string, unknown> = {}) => ({
      type: 'task_notification',
      task: {
        id: 't1',
        tool_use_id: 'call-1',
        output_file: '/r/t1.output',
        status: 'exited',
        exit_code: 0,
        command: 'make serve',
        ...over,
      },
    });
    const noticeOnly = (over: Record<string, unknown> = {}) =>
      msg({ id: 'n1', seq: 3, role: 'User', content: [notice(over)] });

    it('draws a notice-only message as its line, not a bubble', () => {
      draw([noticeOnly()]);
      const line = screen.getByText(/Background command finished · exit 0/);
      expect(line.closest('.bg-muted')).toBeNull();
      expect(screen.getByText('make serve')).toBeInTheDocument();
    });

    it("draws a notice-only message's lines and none of its context", () => {
      const context = { type: 'context', text: '## Sandbox\n\n```json\n{"commands":"outside"}\n```' };
      draw([msg({ id: 'n1', seq: 3, role: 'User', content: [context, notice()] })]);
      expect(screen.getByText(/Background command finished · exit 0/)).toBeInTheDocument();
      expect(screen.queryByText(/"commands":"outside"/)).toBeNull();
    });

    it('says so when an agent started the command', () => {
      draw([noticeOnly({ agent_description: 'Build it' })]);
      expect(screen.getByText('Background command finished · exit 0 · started by an agent —')).toBeInTheDocument();
    });

    it('names the command by its description when it has one, as the model says it', () => {
      draw([noticeOnly({ description: 'Serve the site' })]);
      expect(screen.getByText('Serve the site').closest('.italic')).not.toBeNull();
      expect(screen.queryByText('make serve')).toBeNull();
    });

    it('draws a stopped and a lost notice', () => {
      draw([
        noticeOnly({ status: 'stopped', exit_code: undefined, stopped_by: 'user' }),
        msg({ id: 'n2', seq: 4, role: 'User', content: [notice({ id: 't2', status: 'lost', exit_code: undefined })] }),
      ]);
      expect(screen.getByText(/Background command stopped/)).toBeInTheDocument();
      expect(screen.getByText(/Background command lost when Kubetail stopped/)).toBeInTheDocument();
    });

    it('spells an invisible character in the command out', () => {
      draw([noticeOnly({ command: 'make\u202eserve' })]);
      expect(document.querySelector('mark')).toHaveTextContent('\\u{202E}');
    });

    it('gives a message of notices alone no actions row', () => {
      draw([noticeOnly()]);
      expect(screen.queryByRole('button', { name: 'Copy message' })).toBeNull();
    });

    it('draws the notices a question carried above its bubble', () => {
      draw([msg({ id: 'q', seq: 3, role: 'User', content: [notice(), { type: 'text', text: 'and now?' }] })]);
      const line = screen.getByText(/Background command finished/);
      const bubble = screen.getByText('and now?');
      expect(line.compareDocumentPosition(bubble)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
      expect(line.closest('.bg-muted')).toBeNull();
    });

    it("draws an agent's notice by how it ended and the model's description, never its report", () => {
      const agent = (over: Record<string, unknown>) =>
        notice({ kind: 'agent', command: undefined, exit_code: undefined, description: 'Find pods', ...over });
      draw([
        msg({ id: 'n1', seq: 3, role: 'User', content: [agent({ id: 'a1', status: 'completed', result: 'REPORT' })] }),
        msg({ id: 'n2', seq: 4, role: 'User', content: [agent({ id: 'a2', status: 'failed', error: 'ERROR' })] }),
        msg({ id: 'n3', seq: 5, role: 'User', content: [agent({ id: 'a3', status: 'stopped', stopped_by: 'user' })] }),
        msg({
          id: 'n4',
          seq: 6,
          role: 'User',
          content: [agent({ id: 'a4', status: 'stopped', stopped_by: 'unanswered' })],
        }),
        msg({ id: 'n5', seq: 7, role: 'User', content: [agent({ id: 'a5', status: 'lost' })] }),
      ]);
      expect(screen.getByText('Agent finished —')).toBeInTheDocument();
      expect(screen.getByText('Agent failed —')).toBeInTheDocument();
      expect(screen.getByText('Agent stopped —')).toBeInTheDocument();
      expect(screen.getByText('Agent stopped: no answer in 30 minutes —')).toBeInTheDocument();
      expect(screen.getByText('Agent lost when Kubetail stopped —')).toBeInTheDocument();
      expect(screen.getAllByText('Find pods')[0].closest('.italic')).not.toBeNull();
      expect(screen.queryByText(/REPORT|ERROR/)).toBeNull();
    });

    it("draws a watch's notice by how it ended and the model's description, never its sample", () => {
      const watch = (id: string, over: Record<string, unknown>) =>
        msg({
          id,
          seq: Number(id.slice(1)) + 2,
          role: 'User',
          content: [
            notice({
              kind: 'watch',
              id,
              command: undefined,
              exit_code: undefined,
              description: 'Panics',
              result: 'SAMPLE',
              ...over,
            }),
          ],
        });
      draw([
        watch('w1', { status: 'matched' }),
        watch('w2', { status: 'expired' }),
        watch('w3', { status: 'failed' }),
        watch('w4', { status: 'stopped', stopped_by: 'user' }),
        watch('w5', { status: 'lost' }),
      ]);
      expect(screen.getByText('Log watch matched —')).toBeInTheDocument();
      expect(screen.getByText('Log watch ended: nothing matched —')).toBeInTheDocument();
      expect(screen.getByText('Log watch failed —')).toBeInTheDocument();
      expect(screen.getByText('Log watch stopped —')).toBeInTheDocument();
      expect(screen.getByText('Log watch lost when Kubetail stopped —')).toBeInTheDocument();
      expect(screen.queryByText(/SAMPLE/)).toBeNull();
    });

    it('offers no Ask again on a failed answer to a notice-only message', () => {
      draw([noticeOnly(), msg({ id: 'm4', seq: 4, status: 'Failed', content: [], error: '429 rate_limit_error' })]);
      expect(screen.queryByRole('button', { name: 'Ask again' })).toBeNull();
    });
  });

  describe('the grant offer', () => {
    // A sandboxed command that ran unasked, failed unless a case says otherwise.
    const sandboxedCall = (over: Partial<ChatToolCall> = {}) =>
      call({
        action: {
          ...call().action!,
          command: { ...call().action!.command!, text: 'cat /Users/ana/code/README.md', cwd: '/ws', sandboxed: true },
        },
        approval: null,
        status: 'Failed',
        output: 'Exit code 1\ncat: /Users/ana/code/README.md: No such file or directory',
        ...over,
      });
    const offer = () => screen.getByRole('button', { name: 'Grant a folder…' });

    it('draws the offer after a failed sandboxed call, outside its disclosure', () => {
      draw([msg({ toolCalls: [sandboxedCall()] })]);
      const disclosure = screen.getByText('cat /Users/ana/code/README.md').closest('details')!;
      expect(disclosure.nextElementSibling).toBe(offer());
    });

    it('draws none under one that succeeded or ran outside the sandbox', () => {
      draw([
        msg({
          toolCalls: [
            sandboxedCall({ status: 'Succeeded' }),
            decided({ id: 'tc2', status: 'Failed', output: 'Exit code 1' }),
          ],
        }),
      ]);
      expect(screen.queryByRole('button', { name: 'Grant a folder…' })).toBeNull();
    });

    it('opens the form in place, empty and with both durations, and Cancel closes it', () => {
      draw([msg({ toolCalls: [sandboxedCall()] })]);
      expect(offer()).toHaveAttribute('aria-expanded', 'false');
      expect(screen.queryByRole('textbox', { name: 'Folder to grant' })).toBeNull();

      fireEvent.click(offer());
      expect(offer()).toHaveAttribute('aria-expanded', 'true');
      expect(screen.getByRole('textbox', { name: 'Folder to grant' })).toHaveValue('');
      expect(screen.getByRole('tab', { name: 'For this chat', selected: true })).toBeInTheDocument();
      expect(screen.getByRole('tab', { name: 'Always' })).toBeInTheDocument();

      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
      expect(offer()).toHaveAttribute('aria-expanded', 'false');
      expect(screen.queryByRole('textbox', { name: 'Folder to grant' })).toBeNull();
    });

    it("grants the typed folder for the chat, then says Granted in the offer's place", async () => {
      sendMock.mockResolvedValue({ data: { folderGrant: { wide: [] } } });
      draw([msg({ toolCalls: [sandboxedCall()] })]);
      fireEvent.click(offer());
      fireEvent.change(screen.getByRole('textbox', { name: 'Folder to grant' }), {
        target: { value: '/Users/ana/code' },
      });
      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: 'Grant' }));
      });

      expect(sendMock).toHaveBeenCalledWith({ chatID: 'c1', path: '/Users/ana/code', write: false, duration: 'Chat' });
      expect(screen.queryByRole('textbox', { name: 'Folder to grant' })).toBeNull();
      expect(screen.queryByRole('button', { name: 'Grant a folder…' })).toBeNull();
      expect(screen.getByText('Granted')).toBeInTheDocument();
    });

    it("draws a subagent's offer inside its Agent call's disclosure", () => {
      const agent = call({
        id: 'ag1',
        name: 'Agent',
        actionKind: 'Delegate',
        status: 'Succeeded',
        approval: null,
        action: {
          ...call().action!,
          command: null,
          delegate: { prompt: 'Read the README.', agentType: 'general-purpose', model: '' },
        },
        background: {
          status: 'Completed',
          exitCode: null,
          report: '',
          matched: [],
          endedBecause: '',
          viewAround: null,
        },
      });
      draw([msg({ toolCalls: [agent, sandboxedCall({ id: 'cc1', agentCallID: 'ag1' })] })]);
      const disclosure = screen.getByText('Agent').closest('details')!;
      expect(disclosure).toContainElement(offer());
    });
  });
});
