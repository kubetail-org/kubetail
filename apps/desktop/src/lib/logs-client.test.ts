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

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { compareTimestamps } from '@/lib/log-viewer';
import type { TailFrame, Page } from '@/lib/log-viewer';
import type { LogsViewAction } from '@/lib/logs-view';
import { mockTauriCore } from '@/test-utils';

const { invokeMock, channels, channelFor, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { reportError } = vi.hoisted(() => ({ reportError: vi.fn() }));
vi.mock('@/lib/error-bus', () => ({ reportError, errorMessage: String, toError: (e: unknown) => e }));

const { createGraphqlClient } = await import('@/lib/graphql/client');
const { createLogsClient } = await import('./logs-client');

// The action as the chat's query hands it over, `__typename`s and all.
const action = {
  __typename: 'LogsViewAction',
  sources: [
    {
      __typename: 'LogsViewSource',
      namespace: 'prod',
      kind: 'Deployment',
      name: 'webapp',
      containers: ['app'],
      allContainers: false,
      previous: false,
    },
  ],
  filters: [{ __typename: 'LogsViewFilter', field: 'Node', values: ['node-a'] }],
  grep: 'error',
  anchor: { __typename: 'LogsViewAnchor', kind: 'Tail', at: null },
  pinToEnd: true,
} as unknown as LogsViewAction;

const SCOPE = {
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
  filters: [{ field: 'Node', values: ['node-a'] }],
  grep: 'error',
};

// Line 2 carries a hole before it, and line 3 was cut.
const hole = { after: '2026-10-17T11:00:00Z', before: '2026-10-17T12:00:02Z' };

const record = (i: number, container = 'app', timestamp = `2026-10-17T12:00:0${i}Z`) => ({
  __typename: 'LogRecord',
  timestamp,
  message: `line ${i}`,
  truncatedBytes: i === 3 ? 2048 : 0,
  cursor: `c${i}`,
  stream: { __typename: 'LogStream', namespace: 'prod', pod: 'webapp-1', container, previous: false },
  place: { __typename: 'LogPlace', node: 'node-a', region: 'r', zone: 'z', os: 'linux', arch: 'amd64' },
  gap: i === 2 ? { __typename: 'LogGap', ...hole } : null,
});

const line = (i: number) => ({
  timestamp: `2026-10-17T12:00:0${i}Z`,
  message: `line ${i}`,
  truncatedBytes: i === 3 ? 2048 : 0,
  cursor: `c${i}`,
  stream: { namespace: 'prod', pod: 'webapp-1', container: 'app', previous: false },
  place: { node: 'node-a', region: 'r', zone: 'z', os: 'linux', arch: 'amd64' },
  gap: i === 2 ? hole : null,
});

type Request = Record<string, unknown>;
type Answer = {
  records: unknown[];
  page: { prev: string | null; next: string | null; follow: string | null };
  notices: unknown[];
};

const answerOf = (records: unknown[], over: Partial<Answer> = {}): Answer => ({
  records,
  page: { prev: null, next: null, follow: null },
  notices: [],
  ...over,
});

// What the sidecar answers each `logsFetch` request: a page, or GraphQL errors.
let respond: (request: Request) => Answer | { errors: unknown[] } = () => answerOf([]);

const failed = vi.fn();

const asked = (): Request[] =>
  invokeMock.mock.calls
    .filter(([cmd]) => cmd === 'graphql_query')
    .map(
      ([, arg]) =>
        (JSON.parse((arg as { body: string }).body) as { variables: { request: Request } }).variables.request,
    );

const dialed = () =>
  invokeMock.mock.calls
    .filter(([cmd]) => cmd === 'graphql_subscribe')
    .map(([, arg]) => (arg as { variables: unknown }).variables);

const push = (msg: object) => channelFor('logsTail').onmessage!(JSON.stringify(msg));
const frame = (logsTail: object) => push({ type: 'next', payload: { data: { logsTail } } });

// The async `graphql_subscribe` ack, without advancing fake timers.
const flush = async () => {
  await Promise.resolve();
  await Promise.resolve();
};

beforeEach(() => {
  let id = 0;
  invokeMock.mockReset();
  channels.length = 0;
  respond = () => answerOf([]);
  invokeMock.mockImplementation(async (cmd: string, arg: { body: string }) => {
    if (cmd === 'graphql_subscribe') {
      id += 1;
      return id;
    }
    if (cmd === 'graphql_query') {
      const { request } = (JSON.parse(arg.body) as { variables: { request: Request } }).variables;
      const answer = respond(request);
      const body = 'errors' in answer ? { data: null, errors: answer.errors } : { data: { logsFetch: answer } };
      return { status: 200, body: JSON.stringify(body) };
    }
    return undefined;
  });
  reportError.mockReset();
  failed.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
});

const client = () => createLogsClient(createGraphqlClient(), 'cluster-1', action);

describe('fetch', () => {
  it('reads First from the scope, oldest first, and passes the page through', async () => {
    const notices = [{ __typename: 'LogNotice', code: 'stream-skipped', message: 'prod/db-0/app: forbidden' }];
    respond = () => answerOf([record(2), record(3)], { page: { prev: null, next: 'n1', follow: null }, notices });
    const page = await client().fetch({ kind: 'First', limit: 10 });
    expect(asked()).toEqual([{ scope: SCOPE, pageFrom: 'First', pageSize: 10 }]);
    expect(page).toEqual<Page>({
      lines: [line(2), line(3)],
      edges: { prev: null, next: 'n1', follow: null },
      anchor: null,
      notices: [{ code: 'stream-skipped', message: 'prod/db-0/app: forbidden' }],
    });
  });

  it('reads Last from the scope', async () => {
    respond = () => answerOf([record(1)], { page: { prev: 'p1', next: null, follow: 'f1' } });
    const page = await client().fetch({ kind: 'Last', limit: 5 });
    expect(asked()).toEqual([{ scope: SCOPE, pageFrom: 'Last', pageSize: 5 }]);
    expect(page.edges).toEqual({ prev: 'p1', next: null, follow: 'f1' });
  });

  it('sends a Continue as its cursor and `seen`, and nothing of the scope', async () => {
    await client().fetch({ kind: 'Continue', cursor: 'n1', seen: ['c4', 'c7'], limit: 20 });
    await client().fetch({ kind: 'Continue', cursor: 'p1', seen: null, limit: 20 });
    expect(asked()).toEqual([
      { cursor: 'n1', seen: ['c4', 'c7'], pageSize: 20 },
      { cursor: 'p1', seen: null, pageSize: 20 },
    ]);
  });

  it('reads an Until and a Since as one first page each, the bound as given, with `seen`', async () => {
    const at = '2026-10-17T12:00:05.000000001Z';
    await client().fetch({ kind: 'Until', at, seen: ['c1'], limit: 7 });
    await client().fetch({ kind: 'Since', at, seen: ['c2', 'c3'], limit: 8 });
    expect(asked()).toEqual([
      { scope: SCOPE, until: at, seen: ['c1'], pageFrom: 'Last', pageSize: 7 },
      { scope: SCOPE, since: at, seen: ['c2', 'c3'], pageFrom: 'First', pageSize: 8 },
    ]);
  });

  it('asks the sidecar again for a query it asked before, whose answer changed', async () => {
    const logs = client();
    respond = () => answerOf([record(1)], { page: { prev: null, next: null, follow: 'f' } });
    await logs.fetch({ kind: 'Continue', cursor: 'f', seen: [], limit: 10 });
    respond = () => answerOf([record(2)], { page: { prev: null, next: null, follow: 'f' } });
    const again = await logs.fetch({ kind: 'Continue', cursor: 'f', seen: [], limit: 10 });
    expect(asked()).toHaveLength(2);
    expect(again.lines).toEqual([line(2)]);
  });

  it('rejects with the sidecar’s words for a cursor it refused', async () => {
    respond = () => ({
      errors: [{ message: 'the cursor is not one this sidecar wrote', extensions: { rule: 'cursor-invalid' } }],
    });
    await expect(client().fetch({ kind: 'Continue', cursor: 'old', seen: null, limit: 10 })).rejects.toThrow(
      'the cursor is not one this sidecar wrote',
    );
  });
});

describe('fetch At', () => {
  const AT = '2026-10-17T12:00:05Z';

  it('reads both halves, the older ending the nanosecond before the moment', async () => {
    await client().fetch({ kind: 'At', at: AT, limit: 4 });
    expect(asked()).toEqual(
      expect.arrayContaining([
        { scope: SCOPE, since: AT, pageFrom: 'First', pageSize: 4 },
        { scope: SCOPE, until: '2026-10-17T12:00:04.999999999Z', pageFrom: 'Last', pageSize: 4 },
      ]),
    );
    expect(asked()).toHaveLength(2);
  });

  it('joins the older half then the newer, with the older’s `prev`, the newer’s `next` and `follow`, and the anchor between', async () => {
    respond = (request) =>
      request.pageFrom === 'Last'
        ? answerOf([record(3), record(4)], {
            page: { prev: 'p', next: 'ignored', follow: null },
            notices: [{ code: 'a', message: 'older' }],
          })
        : answerOf([record(5), record(6), record(7)], {
            page: { prev: 'ignored', next: null, follow: 'f' },
            notices: [{ code: 'b', message: 'newer' }],
          });
    const page = await client().fetch({ kind: 'At', at: AT, limit: 4 });
    expect(page.lines.map((l) => l.cursor)).toEqual(['c3', 'c4', 'c5', 'c6', 'c7']);
    expect(page.edges).toEqual({ prev: 'p', next: null, follow: 'f' });
    expect(page.anchor).toBe(2);
    expect(page.notices).toEqual([
      { code: 'a', message: 'older' },
      { code: 'b', message: 'newer' },
    ]);
  });

  it('anchors at the first line with no line before the moment', async () => {
    respond = (request) => (request.pageFrom === 'Last' ? answerOf([]) : answerOf([record(5)]));
    expect((await client().fetch({ kind: 'At', at: AT, limit: 4 })).anchor).toBe(0);
  });

  it('lands more lines at the moment than the limit in order, holding none twice', async () => {
    // A log with one line before the moment and three at it, read as the sidecar would.
    const log = [record(4), record(5, 'app', AT), record(6, 'app', AT), record(7, 'app', AT)];
    respond = (request) => {
      const size = request.pageSize as number;
      if (request.pageFrom === 'Last') {
        return answerOf(log.filter((r) => compareTimestamps(r.timestamp, request.until as string) <= 0).slice(-size));
      }
      return answerOf(log.filter((r) => compareTimestamps(r.timestamp, request.since as string) >= 0).slice(0, size));
    };
    const page = await client().fetch({ kind: 'At', at: AT, limit: 2 });
    expect(page.lines.map((l) => l.cursor)).toEqual(['c4', 'c5', 'c6']);
    expect(page.anchor).toBe(1);
  });

  it('fails when either half fails', async () => {
    respond = (request) => (request.pageFrom === 'Last' ? { errors: [{ message: 'too-many' }] } : answerOf([]));
    await expect(client().fetch({ kind: 'At', at: AT, limit: 4 })).rejects.toThrow('too-many');
  });
});

describe('follow', () => {
  it('sends the cursor and `seen` as they came, handing on lines, notices and the end', async () => {
    const logs = client();
    const frames: TailFrame[] = [];
    logs.tail('f1', ['c1'], (f) => frames.push(f), failed);
    await flush();
    expect(dialed()).toEqual([{ id: 'cluster-1', request: { cursor: 'f1', seen: ['c1'] } }]);

    push({ type: 'open' });
    frame({ __typename: 'LogsTailFrame', type: 'Line', record: record(3), notice: null, ended: null });
    frame({ type: 'Notice', record: null, notice: { code: 'stream-skipped', message: 'db: gone' }, ended: null });
    frame({ type: 'Ended', record: null, notice: null, ended: { code: 'scope-gone', message: 'the pod was deleted' } });
    expect(frames).toEqual([
      { kind: 'Line', line: line(3) },
      { kind: 'Notice', notice: { code: 'stream-skipped', message: 'db: gone' } },
      { kind: 'Ended', reason: 'the pod was deleted' },
    ]);
  });

  it('reconnects a watch that died with the `seen` given, then the newest line it handed on of each container', async () => {
    vi.useFakeTimers();
    const stop = client().tail('f1', ['s-app', 's-db'], () => {}, failed);
    await flush();
    push({ type: 'open' });
    frame({ type: 'Line', record: record(4), notice: null, ended: null });
    frame({ type: 'Line', record: record(5, 'sidecar'), notice: null, ended: null });
    frame({ type: 'Line', record: record(6), notice: null, ended: null });

    // A watch that dies once open is the exchange's to reconnect, not a failed follow.
    push({ type: 'next', payload: { errors: [{ message: 'tailer lost' }], extensions: { subscriptionFailed: true } } });
    await vi.advanceTimersByTimeAsync(1_000);
    expect(dialed().at(-1)).toEqual({
      id: 'cluster-1',
      request: { cursor: 'f1', seen: ['s-app', 's-db', 'c6', 'c5'] },
    });
    expect(failed).not.toHaveBeenCalled();
    stop();
  });

  it('never follows again after `Ended`', async () => {
    vi.useFakeTimers();
    client().tail('f1', [], () => {}, failed);
    await flush();
    push({ type: 'open' });
    frame({ type: 'Ended', record: null, notice: null, ended: { code: 'scope-gone', message: 'gone' } });
    push({ type: 'complete' });
    await vi.advanceTimersByTimeAsync(60_000);
    expect(dialed()).toHaveLength(1);
    expect(invokeMock).toHaveBeenCalledWith('graphql_unsubscribe', { id: 1 });
  });

  it('fails a follow the sidecar could not open, in its words, and never follows again', async () => {
    vi.useFakeTimers();
    const frames: TailFrame[] = [];
    client().tail('f1', [], (f) => frames.push(f), failed);
    await flush();
    push({ type: 'open' });
    push({ type: 'next', payload: { data: null, errors: [{ message: 'the cluster has no cache' }] } });
    push({ type: 'complete' });
    await vi.advanceTimersByTimeAsync(60_000);

    expect(failed).toHaveBeenCalledOnce();
    expect(failed.mock.calls[0][0]).toEqual(new Error('the cluster has no cache'));
    expect(frames).toEqual([]);
    expect(dialed()).toHaveLength(1);
  });

  it('stops the subscription when the follow is stopped', async () => {
    const frames: TailFrame[] = [];
    const stop = client().tail('f1', [], (f) => frames.push(f), failed);
    await flush();
    stop();
    expect(invokeMock).toHaveBeenCalledWith('graphql_unsubscribe', { id: 1 });
    frame({ type: 'Line', record: record(5), notice: null, ended: null });
    expect(frames).toEqual([]);
  });
});
