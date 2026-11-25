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
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Client, LogLine, Page } from '@/lib/log-viewer';
import { FakeClient } from '@/lib/log-viewer/fake-client';
import type { LogsViewAction } from '@/lib/logs-view';

import { InlineLines, appendLine, inlinePlan, readInline, useInlineLogLines } from './inline-log-lines';

const RESOLVED = '2026-10-08T14:04:00Z';
const NEXT = '2026-10-08T14:09:00Z';

const action = (over: Partial<LogsViewAction> = {}): LogsViewAction => ({
  sources: [
    { namespace: 'prod', kind: 'Deployment', name: 'webapp', containers: [], allContainers: false, previous: false },
  ],
  filters: [],
  grep: '',
  anchor: { kind: 'Tail', at: null },
  pinToEnd: true,
  resolvedAt: RESOLVED,
  ...over,
});

describe('inlinePlan', () => {
  it('reads a live pinned tail as the newest eight, following', () => {
    expect(inlinePlan(action(), null)).toEqual({
      first: { kind: 'Last', limit: 8 },
      keep: 'all',
      end: 'tail',
      follow: true,
      until: null,
    });
  });

  it('reads a frozen pinned tail up to where it stopped being live', () => {
    expect(inlinePlan(action(), NEXT)).toEqual({
      first: { kind: 'At', at: NEXT, limit: 8 },
      keep: 'before',
      end: 'tail',
      follow: false,
      until: NEXT,
    });
  });

  it('reads an unpinned tail up to its own moment, never live though newest', () => {
    const plan = inlinePlan(action({ pinToEnd: false }), null);
    expect(plan.first).toEqual({ kind: 'At', at: RESOLVED, limit: 8 });
    expect(plan.follow).toBe(false);
    expect(plan.until).toBe(RESOLVED);
  });

  it('reads a head view as its first eight, never live though pinned and newest', () => {
    expect(inlinePlan(action({ anchor: { kind: 'Head', at: null } }), null)).toEqual({
      first: { kind: 'First', limit: 8 },
      keep: 'all',
      end: 'head',
      follow: false,
      until: null,
    });
  });

  it('reads a moment as the eight from it, never live though pinned and newest', () => {
    const at = '2026-10-08T00:00:00Z';
    expect(inlinePlan(action({ anchor: { kind: 'At', at } }), null)).toEqual({
      first: { kind: 'At', at, limit: 8 },
      keep: 'from',
      end: 'head',
      follow: false,
      until: null,
    });
  });
});

// A log of 100 lines a second apart; line i's cursor names it.
const log = () => new FakeClient({ lines: 100, spacingMs: 1000 });
const indexes = (lines: LogLine[]) => lines.map((line) => Number(line.cursor.split(':')[2]));
const range = (from: number, to: number) => Array.from({ length: to - from }, (_, i) => from + i);

describe('readInline', () => {
  it('reads the newest eight, and where a follow goes on from', async () => {
    const fake = log();
    const read = await readInline(fake, inlinePlan(action(), null));
    expect(indexes(read.lines)).toEqual(range(92, 100));
    expect(read.more).toBe(false);
    expect(read.after).not.toBeNull();
  });

  it('reads the eight before until', async () => {
    const fake = log();
    const read = await readInline(fake, inlinePlan(action(), fake.lineAt(50).timestamp));
    expect(indexes(read.lines)).toEqual(range(42, 50));
  });

  it('keeps every line at until out, however many there are', async () => {
    // The clock alone stands still, so the appended lines share the newest line's stamp.
    vi.useFakeTimers({ toFake: ['Date'] });
    const fake = log();
    // Ten more lines at the clock's time, the newest line's: eleven at until, more than a page.
    fake.append(10);
    const read = await readInline(fake, inlinePlan(action(), fake.lineAt(99).timestamp));
    expect(indexes(read.lines)).toEqual(range(91, 99));
    vi.useRealTimers();
  });

  it('reads the eight from the moment', async () => {
    const fake = log();
    const at = fake.lineAt(50).timestamp;
    const read = await readInline(fake, inlinePlan(action({ anchor: { kind: 'At', at } }), null));
    expect(indexes(read.lines)).toEqual(range(50, 58));
  });

  it('reads the first eight', async () => {
    const fake = log();
    const read = await readInline(fake, inlinePlan(action({ anchor: { kind: 'Head', at: null } }), null));
    expect(indexes(read.lines)).toEqual(range(0, 8));
  });
});

describe('readInline reading on', () => {
  it('reads a short tail page on backward, and follows from the first page', async () => {
    const fake = log();
    const fetch = vi.fn((query: Parameters<Client['fetch']>[0]) => fake.fetch(query));
    fake.emptyNext();
    const read = await readInline({ fetch, tail: fake.tail.bind(fake) }, inlinePlan(action(), null));
    expect(indexes(read.lines)).toEqual(range(84, 92));
    const first = (await fetch.mock.results[0].value) as Page;
    expect(fetch.mock.calls[1][0]).toEqual({ kind: 'Continue', cursor: first.edges.prev, seen: null, limit: 8 });
    expect(read.after).toBe(first.edges.follow);
  });

  it('reads a short head page on forward', async () => {
    const fake = log();
    const fetch = vi.fn((query: Parameters<Client['fetch']>[0]) => fake.fetch(query));
    fake.emptyNext();
    const read = await readInline(
      { fetch, tail: fake.tail.bind(fake) },
      inlinePlan(action({ anchor: { kind: 'Head', at: null } }), null),
    );
    expect(indexes(read.lines)).toEqual(range(8, 16));
    const first = (await fetch.mock.results[0].value) as Page;
    expect(fetch.mock.calls[1][0]).toEqual({ kind: 'Continue', cursor: first.edges.next, seen: null, limit: 8 });
  });

  it('stops where the edge says no more', async () => {
    const fake = new FakeClient({ lines: 5 });
    const read = await readInline(fake, inlinePlan(action({ anchor: { kind: 'Head', at: null } }), null));
    expect(indexes(read.lines)).toEqual(range(0, 5));
    expect(read.more).toBe(false);
  });

  it('stops four pages past the first, saying there may be more', async () => {
    const fake = log();
    const fetch = vi.fn(async (query: Parameters<Client['fetch']>[0]) => ({ ...(await fake.fetch(query)), lines: [] }));
    const read = await readInline({ fetch, tail: fake.tail.bind(fake) }, inlinePlan(action(), null));
    expect(fetch).toHaveBeenCalledTimes(5);
    expect(read.lines).toEqual([]);
    expect(read.more).toBe(true);
  });
});

describe('appendLine', () => {
  const fake = log();
  const lines = (from: number, to: number) => range(from, to).map((i) => fake.lineAt(i));

  it('appends a newer line and keeps the newest eight', () => {
    expect(indexes(appendLine(lines(10, 18), fake.lineAt(18)))).toEqual(range(11, 19));
  });

  it('drops a line whose cursor it holds', () => {
    const held = lines(10, 18);
    expect(appendLine(held, fake.lineAt(15))).toBe(held);
  });

  it('sorts a line from a slower stream into place, and drops one older than all eight', () => {
    expect(indexes(appendLine([...lines(10, 14), ...lines(15, 19)], fake.lineAt(14)))).toEqual(range(11, 19));
    expect(indexes(appendLine(lines(10, 18), fake.lineAt(3)))).toEqual(range(10, 18));
  });
});

describe('InlineLines', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  // Lets the fake's reads and follow replays land.
  const settle = () => vi.advanceTimersByTimeAsync(0);
  // Writes one line a second after the last.
  const write = (fake: FakeClient, count: number) => {
    for (let i = 0; i < count; i += 1) {
      vi.advanceTimersByTime(1000);
      fake.append(1);
    }
  };
  const started = async (fake: Client, until: string | null = null) => {
    const lines = new InlineLines(fake, action(), until);
    lines.start();
    await settle();
    return lines;
  };

  it('follows a live window from its first page, keeping the newest eight', async () => {
    const fake = log();
    const lines = await started(fake);
    expect(indexes(lines.getSnapshot().lines)).toEqual(range(92, 100));
    write(fake, 3);
    await settle();
    expect(lines.getSnapshot()).toMatchObject({ loading: false, failed: false, stopped: false });
    expect(indexes(lines.getSnapshot().lines)).toEqual(range(95, 103));
  });

  it('keeps the newest eight in time order across streams', async () => {
    const stream = (pod: string) => ({
      stream: { namespace: 'prod', pod, container: 'app', previous: false },
      place: { node: 'n', region: '', zone: '', os: 'linux', arch: 'amd64' },
    });
    const fake = new FakeClient({ lines: 100, spacingMs: 1000, delayMs: 10, streams: [stream('a'), stream('b')] });
    const lines = new InlineLines(fake, action(), null);
    lines.start();
    await vi.advanceTimersByTimeAsync(10);
    // Written a second apart before the follow's replay, which sends them a stream at a time.
    for (let i = 0; i < 6; i += 1) {
      vi.setSystemTime(Date.now() + 1000);
      fake.append(1);
    }
    await vi.advanceTimersByTimeAsync(10);
    expect(indexes(lines.getSnapshot().lines)).toEqual(range(98, 106));
  });

  it('a frozen window reads its page and never follows', async () => {
    const fake = log();
    const until = fake.lineAt(50).timestamp;
    const lines = await started(fake, until);
    expect(indexes(lines.getSnapshot().lines)).toEqual(range(42, 50));
    write(fake, 3);
    await settle();
    expect(indexes(lines.getSnapshot().lines)).toEqual(range(42, 50));
  });

  it('freezing drops the lines at or past until and ends on the page a reload reads', async () => {
    const fake = log();
    const lines = await started(fake);
    write(fake, 3);
    await settle();
    lines.freeze(fake.lineAt(101).timestamp);
    await settle();
    write(fake, 3);
    await settle();
    expect(indexes(lines.getSnapshot().lines)).toEqual(range(93, 101));
    const reload = await started(fake, fake.lineAt(101).timestamp);
    expect(reload.getSnapshot().lines).toEqual(lines.getSnapshot().lines);
  });

  it('freezing reads the frozen page even when every held line precedes until', async () => {
    const fake = log();
    // A follow that has delivered nothing yet: lines before until are still on their way.
    const lines = await started({ fetch: fake.fetch.bind(fake), tail: () => () => {} });
    write(fake, 3);
    lines.freeze(new Date(Date.now() + 1).toISOString());
    await settle();
    expect(indexes(lines.getSnapshot().lines)).toEqual(range(95, 103));
  });

  it('drops the short-read notice once the follow fills the window', async () => {
    // A follow that sends only lines written after it opened.
    const fake = new FakeClient({ lines: 100, spacingMs: 1000, repeat: 0 });
    // Every page comes back empty, as a selective grep's can, so the read stops at its cap.
    const fetch = async (query: Parameters<Client['fetch']>[0]) => ({ ...(await fake.fetch(query)), lines: [] });
    const lines = await started({ fetch, tail: fake.tail.bind(fake) });
    // The read is five pages, one after another.
    await vi.runAllTimersAsync();
    expect(lines.getSnapshot()).toMatchObject({ lines: [], more: true });
    write(fake, 3);
    await settle();
    expect(lines.getSnapshot().more).toBe(true);
    write(fake, 5);
    await settle();
    expect(lines.getSnapshot().more).toBe(false);
  });

  it('an ended follow keeps its lines and stops', async () => {
    const fake = log();
    const lines = await started(fake);
    fake.endFollows('the pod is gone');
    write(fake, 3);
    await settle();
    expect(lines.getSnapshot().stopped).toBe(true);
    expect(indexes(lines.getSnapshot().lines)).toEqual(range(92, 100));
  });

  it('a refused follow keeps its lines and stops', async () => {
    const fake = log();
    fake.failNext('follow');
    const lines = await started(fake);
    expect(lines.getSnapshot().stopped).toBe(true);
    expect(indexes(lines.getSnapshot().lines)).toEqual(range(92, 100));
  });

  it('a failed first read answers the failure', async () => {
    const fake = log();
    fake.failNext('fetch');
    const lines = await started(fake);
    expect(lines.getSnapshot()).toMatchObject({ loading: false, failed: true, lines: [] });
  });

  it('loads until the first read lands, and a closed window takes no read', async () => {
    const fake = new FakeClient({ lines: 100, delayMs: 50 });
    const lines = new InlineLines(fake, action(), null);
    const seen = vi.fn();
    lines.subscribe(seen);
    lines.start();
    expect(lines.getSnapshot().loading).toBe(true);
    lines.close();
    await vi.advanceTimersByTimeAsync(100);
    expect(seen).not.toHaveBeenCalled();
    expect(lines.getSnapshot().loading).toBe(true);
  });
});

describe('useInlineLogLines', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const settle = () => act(() => vi.advanceTimersByTimeAsync(0));
  const write = (fake: FakeClient) =>
    act(() => {
      vi.advanceTimersByTime(1000);
      fake.append(1);
    });

  it('follows while live, and freezes at until when a newer view arrives', async () => {
    const fake = log();
    const view = action();
    const { result, rerender } = renderHook(
      ({ until }: { until: string | null }) => useInlineLogLines(fake, view, until),
      { initialProps: { until: null as string | null } },
    );
    expect(result.current.loading).toBe(true);
    await settle();
    write(fake);
    await settle();
    expect(indexes(result.current.lines)).toEqual(range(93, 101));

    rerender({ until: fake.lineAt(100).timestamp });
    await settle();
    write(fake);
    await settle();
    expect(indexes(result.current.lines)).toEqual(range(92, 100));
  });

  it('reads once, with what it was mounted with', async () => {
    const fake = log();
    const fetch = vi.fn((query: Parameters<Client['fetch']>[0]) => fake.fetch(query));
    const view = action({ anchor: { kind: 'Head', at: null } });
    const { rerender } = renderHook(
      ({ until }: { until: string | null }) => useInlineLogLines({ fetch, tail: fake.tail.bind(fake) }, view, until),
      { initialProps: { until: null as string | null } },
    );
    await settle();
    rerender({ until: fake.lineAt(50).timestamp });
    await settle();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('closes its tail when it unmounts', async () => {
    const fake = log();
    const stop = vi.fn();
    const tail = vi.fn(() => stop);
    const { unmount } = renderHook(() => useInlineLogLines({ fetch: fake.fetch.bind(fake), tail }, action(), null));
    await settle();
    expect(tail).toHaveBeenCalledTimes(1);
    expect(tail).toHaveBeenCalledWith(expect.any(String), [], expect.any(Function), expect.any(Function));
    unmount();
    expect(stop).toHaveBeenCalled();
  });
});
