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

import { renderHook } from '@testing-library/react';
import { print } from 'graphql';
import type { DocumentNode } from 'graphql';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { ChatCitation, ChatSandboxAction, ChatToolCall } from './chats';

// Same seam as the cached-data hooks' suites: stand in for useFoldedSubscription,
// capture the reducer it was handed, and fold frames through it directly.
const { useFoldedSubscriptionMock } = vi.hoisted(() => ({ useFoldedSubscriptionMock: vi.fn() }));
vi.mock('@/lib/graphql/use-folded-subscription', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/graphql/use-folded-subscription')>()),
  useFoldedSubscription: useFoldedSubscriptionMock,
}));

const {
  actionKindLabel,
  chatModeOf,
  chatTitleOf,
  chatTitleHasLineBreak,
  chatTitleTooLong,
  normalizeChatTitle,
  grantOffered,
  noticesOf,
  queryResultOf,
  ranSearch,
  searchesOf,
  sourcesOf,
  textOf,
  useChatMessages,
  useChats,
  waitingRequestsOf,
} = await import('./chats');

let acc: unknown;
let lastReducer: ((prev: unknown, frames: unknown[]) => unknown) | undefined;
let lastArgs: { query?: DocumentNode; variables?: { chatID?: string } } | undefined;
let lastOptions: { unshared?: boolean } | undefined;

function push(wrapper: string, ...frames: unknown[]) {
  acc = lastReducer!(
    acc,
    frames.map((f) => ({ [wrapper]: f })),
  );
}

function render<T>(hook: () => T, connected = true) {
  useFoldedSubscriptionMock.mockImplementation((args, reduce, options) => {
    lastArgs = args;
    lastReducer = reduce;
    lastOptions = options;
    return { data: acc, connected };
  });
  return renderHook(hook);
}

beforeEach(() => {
  acc = undefined;
  lastReducer = undefined;
  vi.clearAllMocks();
});

describe('useChats', () => {
  const chat = (id: string, updatedAt: string, mode = 'Chat') => ({
    id,
    title: `chat ${id}`,
    mode,
    createdAt: '2026-09-01T00:00:00Z',
    updatedAt,
  });
  const pushChats = (...frames: { type: string; chat: unknown }[]) => push('chatCollectionWatch', ...frames);
  const renderChats = () => render(() => useChats());

  // The composer's sandbox button reads the chat's switch off the list, so every
  // window sees a switch another one made.
  it("selects each chat's switches", () => {
    renderChats();
    expect(print(lastArgs!.query!)).toMatch(
      /chat \{\s+id\s+title\s+mode\s+clusterID\s+createdAt\s+updatedAt\s+awaitingApproval\s+sandboxDisabled\s+networkEnabled\s+\}/,
    );
  });

  it('has no chats before the first frame', () => {
    const { result } = renderChats();
    expect(result.current.chats).toEqual([]);
    expect(result.current.phase).toBe('connecting');
  });

  it('is connecting until the Bookmark closes the snapshot, then live', () => {
    const { result, rerender } = renderChats();
    pushChats({ type: 'Added', chat: chat('a', '2026-09-01T10:00:00Z') });
    rerender();
    expect(result.current.phase).toBe('connecting');

    pushChats({ type: 'Bookmark', chat: null });
    rerender();
    expect(result.current.phase).toBe('live');
    expect(result.current.chats.map((c) => c.id)).toEqual(['a']);
  });

  // A reconnect masks the old connection's list until the replay's Bookmark.
  it('holds the last synced list through a replay, taking each record as it lands', () => {
    const { result, rerender } = renderChats();
    pushChats(
      { type: 'Added', chat: chat('a', '2026-09-01T10:00:00Z') },
      { type: 'Added', chat: chat('b', '2026-09-01T09:00:00Z') },
      { type: 'Bookmark', chat: null },
    );
    rerender();

    acc = undefined;
    rerender();
    expect(result.current.phase).toBe('connecting');
    expect(result.current.chats.map((c) => c.title)).toEqual(['chat a', 'chat b']);

    pushChats({ type: 'Added', chat: { ...chat('b', '2026-09-01T11:00:00Z'), title: 'renamed' } });
    rerender();
    expect(result.current.chats.map((c) => c.title)).toEqual(['renamed', 'chat a']);

    // The Bookmark says what the replay left out is gone.
    pushChats({ type: 'Bookmark', chat: null });
    rerender();
    expect(result.current.phase).toBe('live');
    expect(result.current.chats.map((c) => c.title)).toEqual(['renamed']);
  });

  it('sorts by updatedAt newest first, parsing the ISO wire value', () => {
    const { result, rerender } = renderChats();
    pushChats(
      { type: 'Added', chat: chat('old', '2026-09-01T09:00:00Z') },
      { type: 'Added', chat: chat('new', '2026-09-01T11:30:00Z') },
      { type: 'Added', chat: chat('mid', '2026-09-01T10:00:00Z') },
      { type: 'Bookmark', chat: null },
    );
    rerender();
    expect(result.current.chats.map((c) => c.id)).toEqual(['new', 'mid', 'old']);
  });

  // The nav filters the list on every draw; a list that moved each time would
  // redraw every row.
  it('hands back the same list until a frame changes it', () => {
    const { result, rerender } = renderChats();
    pushChats({ type: 'Added', chat: chat('a', '2026-09-01T10:00:00Z') }, { type: 'Bookmark', chat: null });
    rerender();
    const first = result.current.chats;
    rerender();
    expect(result.current.chats).toBe(first);
  });

  it('breaks a shared updatedAt by id, descending', () => {
    const { result, rerender } = renderChats();
    const at = '2026-09-01T10:00:00Z';
    pushChats(
      { type: 'Added', chat: chat('01A', at) },
      { type: 'Added', chat: chat('01C', at) },
      { type: 'Added', chat: chat('01B', at) },
      { type: 'Bookmark', chat: null },
    );
    rerender();
    expect(result.current.chats.map((c) => c.id)).toEqual(['01C', '01B', '01A']);
  });

  it('replaces a chat on Modified and drops it on Deleted', () => {
    const { result, rerender } = renderChats();
    pushChats({ type: 'Added', chat: chat('a', '2026-09-01T10:00:00Z') }, { type: 'Bookmark', chat: null });
    pushChats({ type: 'Modified', chat: { ...chat('a', '2026-09-01T12:00:00Z'), title: 'renamed' } });
    rerender();
    expect(result.current.chats).toHaveLength(1);
    expect(result.current.chats[0].title).toBe('renamed');

    pushChats({ type: 'Deleted', chat: chat('a', '2026-09-01T12:00:00Z') });
    rerender();
    expect(result.current.chats).toEqual([]);
  });

  it('drops a change carrying no chat instead of reading it as the snapshot boundary', () => {
    const { result, rerender } = renderChats();
    pushChats({ type: 'Added', chat: null });
    rerender();
    expect(result.current.phase).toBe('connecting');
    expect(result.current.chats).toEqual([]);
  });

  // The list is unscoped and the mode rides on every row; filtering is the caller's.
  it('carries a chat mode through the fold', () => {
    const { result, rerender } = renderChats();
    pushChats(
      { type: 'Added', chat: chat('a', '2026-09-01T10:00:00Z', 'Dashboard') },
      { type: 'Bookmark', chat: null },
    );
    rerender();
    expect(result.current.chats.map((c) => c.mode)).toEqual(['Dashboard']);
  });

  it('takes a connection of its own, since the sidebar consumer can mount late', () => {
    renderChats();
    expect(lastOptions).toEqual({ unshared: true });
  });
});

describe('useChatMessages', () => {
  const msg = (id: string, seq: number, over: Record<string, unknown> = {}) => ({
    id,
    chatID: 'chat-1',
    seq,
    role: 'Assistant',
    content: [{ type: 'text', text: 'hi' }],
    status: 'Complete',
    error: '',
    ...over,
  });
  const pushMessages = (...frames: { type: string; message: unknown }[]) =>
    push('chatMessageCollectionWatch', ...frames);
  const renderMessages = () => render(() => useChatMessages('chat-1'));

  it('watches the chat it was given, on a connection of its own', () => {
    renderMessages();
    expect(lastArgs?.variables).toEqual({ chatID: 'chat-1' });
    expect(lastOptions).toEqual({ unshared: true });
  });

  it('is connecting until the Bookmark, then live', () => {
    const { result, rerender } = renderMessages();
    pushMessages({ type: 'Added', message: msg('m1', 1) });
    rerender();
    expect(result.current.phase).toBe('connecting');

    pushMessages({ type: 'Bookmark', message: null });
    rerender();
    expect(result.current.phase).toBe('live');
  });

  it('orders by seq, not arrival', () => {
    const { result, rerender } = renderMessages();
    pushMessages(
      { type: 'Added', message: msg('m2', 2) },
      { type: 'Added', message: msg('m1', 1) },
      { type: 'Added', message: msg('m3', 3) },
      { type: 'Bookmark', message: null },
    );
    rerender();
    expect(result.current.messages.map((m) => m.id)).toEqual(['m1', 'm2', 'm3']);
  });

  it('replaces a streaming message with each whole answer so far', () => {
    const { result, rerender } = renderMessages();
    pushMessages(
      { type: 'Added', message: msg('m1', 1, { status: 'Streaming', content: [{ type: 'text', text: 'Hel' }] }) },
      { type: 'Bookmark', message: null },
    );
    pushMessages({
      type: 'Modified',
      message: msg('m1', 1, { status: 'Complete', content: [{ type: 'text', text: 'Hello' }] }),
    });
    rerender();
    expect(result.current.messages).toHaveLength(1);
    expect(result.current.messages[0].content).toEqual([{ type: 'text', text: 'Hello' }]);
  });

  // The transcript's memoized rows rely on it: a message the frame left alone
  // keeps its object, so its row skips the render.
  it('keeps every message a Modified left alone as the same object', () => {
    const { result, rerender } = renderMessages();
    pushMessages(
      { type: 'Added', message: msg('m1', 1) },
      { type: 'Added', message: msg('m2', 2) },
      { type: 'Added', message: msg('m3', 3, { status: 'Streaming' }) },
      { type: 'Bookmark', message: null },
    );
    rerender();
    const [m1, m2] = result.current.messages;

    pushMessages({ type: 'Modified', message: msg('m3', 3, { status: 'Streaming', content: [] }) });
    rerender();
    expect(result.current.messages[0]).toBe(m1);
    expect(result.current.messages[1]).toBe(m2);
  });

  // A tool-call change arrives as a Modified frame of its message, so the fold
  // needs nothing new: the selection is what brings the calls in.
  it("carries a message's tool calls off the frame", () => {
    const { result, rerender } = renderMessages();
    expect(print(lastArgs!.query!)).toMatch(
      /toolCalls \{\s+id\s+name\s+actionKind\s+status\s+runsOn\s+action \{\s+description\s+command \{\s+text\s+cwd\s+background\s+sandboxed\s+network\s+env\s+\}\s+read \{\s+path\s+\}\s+write \{\s+path\s+content\s+\}\s+edit \{\s+path\s+oldString\s+newString\s+replaceAll\s+\}\s+search \{\s+query\s+\}\s+fetch \{\s+url\s+host\s+\}\s+memory \{\s+op\s+name\s+body\s+scope\s+\}\s+delegate \{\s+prompt\s+agentType\s+model\s+\}\s+kubeQuery \{\s+sql\s+limit\s+\}\s+logsView \{\s+sources \{\s+namespace\s+kind\s+name\s+containers\s+allContainers\s+previous\s+\}\s+filters \{\s+field\s+values\s+\}\s+grep\s+anchor \{\s+kind\s+at\s+\}\s+pinToEnd\s+resolvedAt\s+\}\s+logsRead \{\s+sources \{\s+namespace\s+kind\s+name\s+containers\s+allContainers\s+previous\s+\}\s+filters \{\s+field\s+values\s+\}\s+grep\s+since\s+until\s+before \{\s+lines\s+span\s+\}\s+after \{\s+lines\s+span\s+\}\s+pageFrom\s+resolvedAt\s+lines \{\s+timestamp\s+namespace\s+pod\s+container\s+previous\s+message\s+gap \{\s+after\s+before\s+\}\s+truncatedBytes\s+\}\s+olderMore\s+newerMore\s+notices \{\s+code\s+message\s+\}\s+viewAround \{\s+sources \{\s+namespace\s+kind\s+name\s+containers\s+allContainers\s+previous\s+\}\s+filters \{\s+field\s+values\s+\}\s+grep\s+anchor \{\s+kind\s+at\s+\}\s+pinToEnd\s+resolvedAt\s+\}\s+\}\s+logsWatch \{\s+sources \{\s+namespace\s+kind\s+name\s+containers\s+allContainers\s+previous\s+\}\s+filters \{\s+field\s+values\s+\}\s+grep\s+since\s+limit\s+maxBytes\s+maxMessageBytes\s+resolvedAt\s+until\s+\}\s+\}\s+agentCallID\s+approval \{\s+id\s+status\s+duration\s+\}\s+network\s+sandboxActions \{\s+approval \{\s+id\s+status\s+duration\s+\}\s+action \{\s+target\s+summary\s+accessLevel\s+principal\s+scope\s+accountName\s+service\s+operation\s+resource\s+name\s+once\s+grantable\s+commandRule\s+chatRule\s+\}\s+request \{\s+__typename\s+\.\.\. on ClusterWrite \{\s+method\s+path\s+subresource\s+contentType\s+body\s+dryRun\s+diff\s+diffCut\s+diffError\s+\}\s+\.\.\. on CloudRequest \{\s+host\s+method\s+path\s+contentType\s+body\s+unshown\s+details \{\s+label\s+value\s+\}\s+\}\s+\}\s+reason\s+\}\s+output\s+background \{\s+status\s+exitCode\s+report\s+matched \{\s+timestamp\s+namespace\s+pod\s+container\s+previous\s+message\s+gap \{\s+after\s+before\s+\}\s+truncatedBytes\s+\}\s+endedBecause\s+viewAround \{\s+sources \{\s+namespace\s+kind\s+name\s+containers\s+allContainers\s+previous\s+\}\s+filters \{\s+field\s+values\s+\}\s+grep\s+anchor \{\s+kind\s+at\s+\}\s+pinToEnd\s+resolvedAt\s+\}\s+\}\s+\}/,
    );
    expect(print(lastArgs!.query!)).toMatch(/citations \{\s+type\s+url\s+title\s+citedText\s+\}/);
    const call = {
      id: 'tc1',
      name: 'bash',
      actionKind: 'Command',
      status: 'AwaitingApproval',
      action: {
        description: 'List files',
        command: { text: 'ls', cwd: '/Users/ana', background: false, sandboxed: false, network: false },
        read: null,
        write: null,
        edit: null,
        search: null,
        fetch: null,
        memory: null,
      },
      approval: { id: 'ap1', status: 'Pending', duration: null },
      output: '',
    };
    pushMessages(
      { type: 'Added', message: msg('m1', 1, { status: 'WaitingApproval', toolCalls: [call] }) },
      { type: 'Bookmark', message: null },
    );
    rerender();
    expect(result.current.messages[0].toolCalls).toEqual([call]);
  });

  it('drops a message on Deleted and a change carrying none at all', () => {
    const { result, rerender } = renderMessages();
    pushMessages({ type: 'Added', message: msg('m1', 1) }, { type: 'Bookmark', message: null });
    pushMessages({ type: 'Modified', message: null }, { type: 'Deleted', message: msg('m1', 1) });
    rerender();
    expect(result.current.messages).toEqual([]);
    expect(result.current.phase).toBe('live');
  });
});

describe('chatModeOf', () => {
  it('spells the app mode the way the wire does', () => {
    expect(chatModeOf('chat')).toBe('Chat');
    expect(chatModeOf('dashboard')).toBe('Dashboard');
  });
});

describe('chatTitleOf', () => {
  it('names a chat with no title, and leaves any other title as it is', () => {
    expect(chatTitleOf('')).toBe('Untitled chat');
    expect(chatTitleOf(' ')).toBe(' ');
    expect(chatTitleOf('Why is checkout crashing?')).toBe('Why is checkout crashing?');
  });
});

// The sidecar's `Rename` pins the same cases (`TestRenameTrimsAndCountsAsTheWebviewDoes`,
// `TestRenameRefusesAnEmptyOverlongOrMultilineTitle`).
describe('normalizeChatTitle, chatTitleTooLong and chatTitleHasLineBreak', () => {
  it('trims what Go’s TrimSpace trims, and nothing else', () => {
    expect(normalizeChatTitle(' \t\u0085\u00a0\u2028\u3000pods\n\u205f ')).toBe('pods');
    // JavaScript's trim drops U+FEFF; Go's keeps it.
    expect(normalizeChatTitle('\ufeffpods')).toBe('\ufeffpods');
  });

  // A regex would retry from every space of the run before the `b`.
  it('reads a long paste in one pass from each end', () => {
    const run = ' '.repeat(200_000);
    expect(normalizeChatTitle(`a${run}b`)).toBe(`a${run}b`);
    expect(normalizeChatTitle(`${run}a${run}`)).toBe('a');
    expect(chatTitleTooLong(`${run}a${run}b`)).toBe(true);
    expect(chatTitleTooLong('a'.repeat(1_000_000))).toBe(true);
  });

  it('counts code points of the trimmed title against 80', () => {
    expect(chatTitleTooLong(`\u0085${'a'.repeat(80)}`)).toBe(false);
    expect(chatTitleTooLong(`\ufeff${'a'.repeat(80)}`)).toBe(true);
    expect(chatTitleTooLong('😀'.repeat(80))).toBe(false);
    expect(chatTitleTooLong('😀'.repeat(81))).toBe(true);
    expect(chatTitleTooLong('')).toBe(false);
  });

  it('finds a line break inside the trimmed title, of any of Unicode’s kinds', () => {
    for (const lineBreak of '\n\v\f\r\u0085\u2028\u2029') {
      expect(chatTitleHasLineBreak(`ops${lineBreak}west`)).toBe(true);
      expect(chatTitleHasLineBreak(`ops west${lineBreak}`)).toBe(false);
    }
    expect(chatTitleHasLineBreak('ops\u2027west')).toBe(false);
  });
});

describe('textOf', () => {
  it('concatenates text blocks in order', () => {
    expect(
      textOf([
        { type: 'text', text: 'Hello ' },
        { type: 'text', text: 'world' },
      ]),
    ).toBe('Hello world');
  });

  it('skips thinking and tool blocks', () => {
    expect(
      textOf([
        { type: 'thinking', thinking: 'hmm' },
        { type: 'tool_use', id: 't1', name: 'grep', input: {} },
        { type: 'text', text: 'the answer' },
        { type: 'tool_result', tool_use_id: 't1', content: 'rows' },
      ]),
    ).toBe('the answer');
  });

  // The two text blocks were written at different moments — before and after a
  // tool round — so they read as two paragraphs, the way llm.Text joins them.
  it('joins text blocks across a tool round with a blank line', () => {
    expect(
      textOf([
        { type: 'text', text: 'Let me check.' },
        { type: 'tool_use', id: 't1', name: 'list_objects', input: {} },
        { type: 'tool_result', id: 't1', text: '{"count":2}' },
        { type: 'text', text: 'There are 2.' },
      ]),
    ).toBe('Let me check.\n\nThere are 2.');
    expect(
      textOf([
        { type: 'tool_use', id: 't1', name: 'list_objects', input: {} },
        { type: 'tool_result', id: 't1', text: '{"count":2}' },
        { type: 'text', text: 'There are 2.' },
      ]),
    ).toBe('There are 2.');
  });

  it('leaves a context block out of the question', () => {
    expect(
      textOf([
        { type: 'context', text: '<context>\n## Cluster\n\n```json\n{"context":"prod"}\n```\n</context>' },
        { type: 'text', text: 'why?' },
      ]),
    ).toBe('why?');
  });

  it('answers empty for content that is not an array of blocks', () => {
    expect(textOf(undefined)).toBe('');
    expect(textOf(null)).toBe('');
    expect(textOf('plain string')).toBe('');
    expect(textOf({ type: 'text', text: 'not an array' })).toBe('');
  });

  it('ignores a block that is not an object or whose text is not a string', () => {
    expect(textOf([null, 'text', 42, { type: 'text' }, { type: 'text', text: 7 }, { type: 'text', text: 'ok' }])).toBe(
      'ok',
    );
  });
});

describe('noticesOf', () => {
  const exited = {
    type: 'task_notification',
    task: {
      id: 't1',
      tool_use_id: 'call-1',
      output_file: '/r/t1.output',
      status: 'exited',
      exit_code: 0,
      description: 'Serve the site',
      command: 'make serve',
    },
  };

  it('reads each notice a message carries, in order', () => {
    const stopped = {
      type: 'task_notification',
      task: { ...exited.task, id: 't2', status: 'stopped', exit_code: undefined, stopped_by: 'user' },
    };
    expect(noticesOf([exited, stopped, { type: 'text', text: 'and?' }])).toEqual([
      {
        id: 't1',
        kind: 'command',
        status: 'exited',
        stoppedBy: '',
        exitCode: 0,
        description: 'Serve the site',
        command: 'make serve',
        agentDescription: '',
      },
      {
        id: 't2',
        kind: 'command',
        status: 'stopped',
        stoppedBy: 'user',
        exitCode: null,
        description: 'Serve the site',
        command: 'make serve',
        agentDescription: '',
      },
    ]);
  });

  it('reads an absent kind, description, code, command or agent as none', () => {
    expect(noticesOf([{ type: 'task_notification', task: { id: 't', status: 'lost' } }])).toEqual([
      {
        id: 't',
        kind: 'command',
        status: 'lost',
        stoppedBy: '',
        exitCode: null,
        description: '',
        command: '',
        agentDescription: '',
      },
    ]);
  });

  it("reads an agent's notice, and never its report or error, which are the model's copy", () => {
    const agent = (status: string, extra: object = {}) => ({
      type: 'task_notification',
      task: { kind: 'agent', id: 't', status, description: 'Find pods', result: 'Two pods.', error: 'e', ...extra },
    });
    const notices = noticesOf([agent('completed'), agent('failed'), agent('stopped', { stopped_by: 'unanswered' })]);
    expect(notices.map((n) => [n.kind, n.status, n.stoppedBy])).toEqual([
      ['agent', 'completed', ''],
      ['agent', 'failed', ''],
      ['agent', 'stopped', 'unanswered'],
    ]);
    expect(Object.keys(notices[0])).not.toContain('result');
    expect(Object.keys(notices[0])).not.toContain('error');
  });

  it("reads a watch's notice, and never its sample, which is the model's copy", () => {
    const watch = (status: string, extra: object = {}) => ({
      type: 'task_notification',
      task: { kind: 'watch', id: 't', status, description: 'Panics', result: 'Watched 1 line.', matched: 1, ...extra },
    });
    const notices = noticesOf([
      watch('matched'),
      watch('expired'),
      watch('failed'),
      watch('stopped', { stopped_by: 'user' }),
    ]);
    expect(notices.map((n) => [n.kind, n.status, n.stoppedBy])).toEqual([
      ['watch', 'matched', ''],
      ['watch', 'expired', ''],
      ['watch', 'failed', ''],
      ['watch', 'stopped', 'user'],
    ]);
    expect(Object.keys(notices[0])).not.toContain('result');
  });

  it('reads a status its kind never ends in as lost', () => {
    const task = (kind: string | undefined, status: string) => ({
      type: 'task_notification',
      task: { kind, id: `${kind}-${status}`, status },
    });
    const notices = noticesOf([
      task(undefined, 'completed'),
      task('command', 'matched'),
      task('agent', 'exited'),
      task('agent', 'expired'),
      task('watch', 'exited'),
      task('watch', 'completed'),
    ]);
    expect(notices.map((n) => [n.kind, n.status])).toEqual([
      ['command', 'lost'],
      ['command', 'lost'],
      ['agent', 'lost'],
      ['agent', 'lost'],
      ['watch', 'lost'],
      ['watch', 'lost'],
    ]);
  });

  it('reads the agent whose call started the command', () => {
    expect(
      noticesOf([{ ...exited, task: { ...exited.task, agent_description: 'Build it' } }])[0].agentDescription,
    ).toBe('Build it');
  });

  it('drops a block that is not a notice', () => {
    expect(
      noticesOf([
        { type: 'task_notification' },
        { type: 'task_notification', task: { id: 't', status: 7 } },
        { type: 'task_notification', task: { status: 'exited' } },
      ]),
    ).toEqual([]);
    expect(noticesOf(undefined)).toEqual([]);
  });

  it('draws no text of its own', () => {
    expect(textOf([exited, { type: 'text', text: 'and?' }])).toBe('and?');
    expect(textOf([exited])).toBe('');
  });
});

type SearchFields = Pick<ChatToolCall, 'actionKind' | 'runsOn' | 'status' | 'action' | 'agentCallID'>;

/** A call the provider ran: a search for query. */
function providerSearch(query: string): SearchFields {
  return {
    actionKind: 'Search',
    runsOn: 'Provider',
    status: null,
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
    agentCallID: null,
  };
}

/** A search the sidecar ran, succeeded unless a case says otherwise. */
function sidecarSearch(query: string): SearchFields {
  return { ...providerSearch(query), runsOn: 'Sidecar', status: 'Succeeded' };
}

describe('actionKindLabel', () => {
  it('spells every kind', () => {
    expect(actionKindLabel('Command')).toBe('Command');
    expect(actionKindLabel('Read')).toBe('Read');
    expect(actionKindLabel('Write')).toBe('Write');
    expect(actionKindLabel('Edit')).toBe('Edit');
    expect(actionKindLabel('Search')).toBe('Search');
    expect(actionKindLabel('Fetch')).toBe('Fetch page');
    expect(actionKindLabel('Stop')).toBe('Stop task');
    expect(actionKindLabel('Delegate')).toBe('Agent');
    expect(actionKindLabel('KubeQuery')).toBe('Query');
    expect(actionKindLabel('LogsRead')).toBe('Read logs');
    expect(actionKindLabel('LogsWatch')).toBe('Watch logs');
  });
});

describe('ranSearch', () => {
  // A sidecar search is drawn as a call until it has succeeded; the provider's
  // has no status and has always run.
  it('passes a provider search and a succeeded sidecar search', () => {
    expect(ranSearch(providerSearch('q'))).toBe(true);
    expect(ranSearch({ ...sidecarSearch('q'), status: 'Succeeded' })).toBe(true);
  });

  it('refuses a sidecar search of every other status', () => {
    (['NotRun', 'AwaitingApproval', 'Denied', 'Running', 'Failed', 'Interrupted'] as const).forEach((status) => {
      expect(ranSearch({ ...sidecarSearch('q'), status }), status).toBe(false);
    });
  });

  it('refuses a call of any other kind, or of none', () => {
    expect(ranSearch({ ...providerSearch('q'), actionKind: 'Command' })).toBe(false);
    expect(ranSearch({ ...providerSearch('q'), actionKind: null })).toBe(false);
  });
});

describe('searchesOf', () => {
  it('lists the queries of the searches that ran, in order, skipping empty ones', () => {
    expect(searchesOf([providerSearch('a'), providerSearch(''), providerSearch('b')])).toEqual({
      count: 3,
      queries: ['a', 'b'],
    });
    expect(searchesOf([])).toEqual({ count: 0, queries: [] });
  });

  // The kind is the tool's, so a search whose arguments did not parse is still
  // counted, and a call of another kind is not, whatever its action holds.
  it('counts by kind, whoever ran it', () => {
    const calls: SearchFields[] = [
      sidecarSearch('mine'),
      { ...providerSearch('ignored'), action: null },
      { ...sidecarSearch('/x'), actionKind: 'Read' },
      { ...sidecarSearch('denied'), status: 'Denied' },
      providerSearch('theirs'),
    ];
    expect(searchesOf(calls)).toEqual({ count: 3, queries: ['mine', 'theirs'] });
  });

  // A subagent's search is drawn inside its Agent call, with its other calls.
  it("counts the answer's own searches alone", () => {
    expect(searchesOf([providerSearch('mine'), { ...providerSearch('the agent'), agentCallID: 'a1' }])).toEqual({
      count: 1,
      queries: ['mine'],
    });
  });
});

/** A citation of a web page, as the sidecar serves it. */
function cited(url: string, title: string): ChatCitation {
  return { type: 'web_search_result_location', url, title, citedText: '' };
}

describe('sourcesOf', () => {
  it('dedupes by url, the first title winning', () => {
    expect(sourcesOf([cited('https://a', 'A'), cited('https://b', 'B'), cited('https://a', 'A again')])).toEqual([
      { url: 'https://a', title: 'A' },
      { url: 'https://b', title: 'B' },
    ]);
    expect(sourcesOf([])).toEqual([]);
  });

  // A document the model was given has no address, so it is not a source to list.
  it('skips a citation with no url', () => {
    expect(
      sourcesOf([{ type: 'page_location', url: '', title: 'Runbook', citedText: 'p. 4' }, cited('https://a', '')]),
    ).toEqual([{ url: 'https://a', title: '' }]);
  });
});

describe('textOf across a server call', () => {
  // The text before a provider's call and the text after it were written at
  // different moments, so they read as two paragraphs, whatever the call holds.
  it('joins text blocks across a server call with a blank line', () => {
    expect(
      textOf([
        { type: 'text', text: 'Let me look.' },
        { type: 'server_use', id: 'srv_1', name: 'anthropic_web_search_20260318', input: { query: 'q' } },
        { type: 'text', text: '1.34 removed it.' },
      ]),
    ).toBe('Let me look.\n\n1.34 removed it.');
    expect(
      textOf([
        { type: 'server_use', id: 'srv_1', name: 'anthropic_web_search_20260318', input: {} },
        { type: 'text', text: '1.34 removed it.' },
      ]),
    ).toBe('1.34 removed it.');
  });
});

/** A cluster write of approval id and status, as the sidecar serves it. */
function clusterWrite(id: string, status: ChatSandboxAction['approval']['status']): ChatSandboxAction {
  return {
    approval: { id, status, duration: null },
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
      commandRule: '',
      chatRule: '',
    },
    request: {
      __typename: 'ClusterWrite',
      method: 'DELETE',
      path: '/api/v1/namespaces/web/pods/x',
      subresource: '',
      contentType: '',
      body: '',
      dryRun: false,
      diff: '',
      diffCut: false,
      diffError: '',
    },
    reason: null,
  };
}

/** A cloud platform request of approval id and status, as the sidecar serves it. */
function cloudRequest(id: string, status: ChatSandboxAction['approval']['status']): ChatSandboxAction {
  return {
    approval: { id, status, duration: null },
    action: {
      target: 'AWS',
      summary: 'AWS: call ec2:TerminateInstances',
      accessLevel: 'Write',
      principal: 'Chat',
      scope: '123456789012/us-east-1',
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
      host: 'ec2.amazonaws.com',
      method: 'POST',
      path: '/',
      contentType: 'text/plain',
      body: 'Action=TerminateInstances',
      unshown: '',
      details: [],
    },
    reason: null,
  };
}

/** A call of status carrying writes, and its own approval when asked about. */
function callWith(
  id: string,
  status: ChatToolCall['status'],
  writes: ChatSandboxAction[],
  approval: ChatToolCall['approval'] = null,
): ChatToolCall {
  return {
    id,
    name: 'Bash',
    actionKind: 'Command',
    status,
    runsOn: 'Sidecar',
    action: null,
    agentCallID: null,
    approval,
    network: null,
    output: '',
    background: null,
    sandboxActions: writes,
  };
}

describe('waitingRequestsOf', () => {
  // One command's cluster and cloud platform requests are one list, in the
  // order asked.
  it("orders a call's cluster and cloud requests by approval id", () => {
    const running = callWith('c1', 'Running', [
      clusterWrite('a1', 'Pending'),
      cloudRequest('a2', 'Pending'),
      clusterWrite('a3', 'Pending'),
    ]);

    const got = waitingRequestsOf([running]);

    expect(got.map((r) => r.approval.id)).toEqual(['a1', 'a2', 'a3']);
    expect(got[1].change?.request?.__typename).toBe('CloudRequest');
  });

  // A write waits while it is pending and its call runs; ids are UUIDv7, so a
  // call's request and a write's sort together in the order asked.
  it("orders a call's and a write's requests together by approval id", () => {
    const running = callWith('c1', 'Running', [clusterWrite('a3', 'Approved'), clusterWrite('a4', 'Pending')]);
    const asking = callWith('c2', 'AwaitingApproval', [], { id: 'a2', status: 'Pending', duration: null });

    const got = waitingRequestsOf([running, asking]);

    expect(got.map((r) => r.approval.id)).toEqual(['a2', 'a4']);
    expect(got[0].change).toBeNull();
    expect(got[1].change?.request?.path).toBe('/api/v1/namespaces/web/pods/x');
    expect(got[1].call).toBe(running);
  });

  it('takes neither an abandoned write nor a pending one on a call no longer running', () => {
    const abandoned = callWith('c1', 'Running', [clusterWrite('a1', 'Abandoned')]);
    const stranded = callWith('c2', 'Interrupted', [clusterWrite('a2', 'Pending')]);

    expect(waitingRequestsOf([abandoned, stranded])).toEqual([]);
  });
});

/** A sandboxed bash call of status. */
function command(status: ChatToolCall['status']): ChatToolCall {
  return {
    ...callWith('c1', status, []),
    action: {
      description: '',
      command: { text: 'cat ~/code/README.md', cwd: '/ws', background: false, sandboxed: true, network: false },
    } as ChatToolCall['action'],
  };
}

describe('grantOffered', () => {
  it('offers a grant under a sandboxed command that failed', () => {
    expect(grantOffered(command('Failed'))).toBe(true);
  });

  it('offers none under one that has not failed, one outside the sandbox, or another tool', () => {
    (['Running', 'Succeeded', 'AwaitingApproval', 'Denied', 'NotRun'] as const).forEach((status) => {
      expect(grantOffered(command(status)), status).toBe(false);
    });
    const outside = command('Failed');
    outside.action!.command!.sandboxed = false;
    expect(grantOffered(outside)).toBe(false);
    expect(
      grantOffered({ ...command('Failed'), action: { description: '', read: { path: '/etc/x' } } } as ChatToolCall),
    ).toBe(false);
    expect(grantOffered({ ...command('Failed'), action: null })).toBe(false);
  });

  it('offers one under a background command that exited non-zero or with no code', () => {
    const exited = (exitCode: number | null): ChatToolCall => ({
      ...command('Succeeded'),
      background: { status: 'Exited', exitCode, report: '', matched: [], endedBecause: '', viewAround: null },
    });
    expect(grantOffered(exited(1))).toBe(true);
    expect(grantOffered(exited(null))).toBe(true);
    expect(grantOffered(exited(0))).toBe(false);
    (['Running', 'Stopped', 'Lost'] as const).forEach((status) => {
      const task = {
        ...command('Succeeded'),
        background: { status, exitCode: null, report: '', matched: [], endedBecause: '', viewAround: null },
      };
      expect(grantOffered(task), status).toBe(false);
    });
  });
});

describe('queryResultOf', () => {
  const rows = {
    cluster: 'prod',
    freshness: { status: 'watching', reason: '' },
    columns: ['name', 'n'],
    more: true,
    rows: [
      ['a', 1],
      ['b', null],
    ],
  };

  it('reads an answer of rows', () => {
    expect(queryResultOf(JSON.stringify(rows))).toEqual({
      cluster: 'prod',
      columns: ['name', 'n'],
      rows: [
        ['a', 1],
        ['b', null],
      ],
      more: true,
    });
  });

  it('reads a missing cluster or more as empty', () => {
    expect(queryResultOf('{"columns":[],"rows":[]}')).toEqual({
      cluster: '',
      columns: [],
      rows: [],
      more: false,
    });
  });

  it.each([
    ['an error', '{"error":"sql","message":"no such column"}'],
    ['a withheld answer', '{"cluster":"prod","freshness":{"status":"unknown"}}'],
    ['a saved result', '<persisted-output>\nOutput too large\n</persisted-output>'],
    ['JSON that is not an object', '[1]'],
    ['JSON null', 'null'],
    ['a column that is not a name', '{"columns":[1],"rows":[]}'],
    ['a row of another width', '{"columns":["a"],"rows":[["x","y"]]}'],
    ['a row that is not an array', '{"columns":["a"],"rows":["x"]}'],
  ])('answers null for %s', (_, output) => {
    expect(queryResultOf(output)).toBeNull();
  });
});
