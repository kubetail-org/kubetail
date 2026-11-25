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

import { FakeClient, type FakeStream } from './fake-client';
import type { Cursor, FetchQuery, TailFrame, LogLine, Page } from './types';

const NOW = Date.parse('2026-10-15T12:00:00.000Z');

function stream(pod: string, weight?: number): FakeStream {
  return {
    stream: { namespace: 'default', pod, container: 'app', previous: false },
    place: { node: `node-${pod}`, region: 'r', zone: 'z', os: 'linux', arch: 'arm64' },
    weight,
  };
}

const first = (limit = 10): FetchQuery => ({ kind: 'First', limit });
const last = (limit = 10): FetchQuery => ({ kind: 'Last', limit });
const at = (moment: string, limit = 10): FetchQuery => ({ kind: 'At', at: moment, limit });
const until = (moment: string, limit = 10, seen: Cursor[] = []): FetchQuery => ({
  kind: 'Until',
  at: moment,
  seen,
  limit,
});
const since = (moment: string, limit = 10, seen: Cursor[] = []): FetchQuery => ({
  kind: 'Since',
  at: moment,
  seen,
  limit,
});
const on = (cursor: Cursor, limit = 10, seen: Cursor[] | null = null): FetchQuery => ({
  kind: 'Continue',
  cursor,
  seen,
  limit,
});

// Every fetch takes `delayMs` on a timer; this runs it out and answers the page.
async function fetchNow(fake: FakeClient, query: FetchQuery): Promise<Page> {
  const pending = fake.fetch(query);
  await vi.runOnlyPendingTimersAsync();
  return pending;
}

const indexes = (fake: FakeClient, lines: LogLine[]) =>
  lines.map((line) => Array.from({ length: fake.lineCount }, (_, i) => fake.lineAt(i).cursor).indexOf(line.cursor));

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('the log', () => {
  it('lets a line carry a hole before it', () => {
    const gap = { after: '2026-10-15T11:00:00.000Z', before: '2026-10-15T11:59:59.999Z' };
    const fake = new FakeClient({ lines: 3, streams: [stream('a')], gaps: { 1: gap } });
    expect([0, 1, 2].map((i) => fake.lineAt(i).gap)).toEqual([null, gap, null]);
  });

  it('writes every line whole but those `truncate` cuts', () => {
    const fake = new FakeClient({ lines: 3, streams: [stream('a')], truncate: { 2: 18_432 } });
    expect([0, 1, 2].map((i) => fake.lineAt(i).truncatedBytes)).toEqual([0, 0, 18_432]);
  });

  it('writes lines a millisecond apart, the last stamped with the clock', () => {
    const fake = new FakeClient({ lines: 3, streams: [stream('a')] });
    expect(fake.lineCount).toBe(3);
    expect([0, 1, 2].map((i) => fake.lineAt(i).timestamp)).toEqual([
      '2026-10-15T11:59:59.998Z',
      '2026-10-15T11:59:59.999Z',
      '2026-10-15T12:00:00.000Z',
    ]);
  });

  it('spaces its history `spacingMs` apart, the last stamped with the clock', () => {
    const fake = new FakeClient({ lines: 3, streams: [stream('a')], spacingMs: 2000 });
    expect([0, 1, 2].map((i) => fake.lineAt(i).timestamp)).toEqual([
      '2026-10-15T11:59:56.000Z',
      '2026-10-15T11:59:58.000Z',
      '2026-10-15T12:00:00.000Z',
    ]);
  });

  it('answers At at a moment inside a spaced history with lines on both sides', async () => {
    const fake = new FakeClient({ lines: 5000, streams: [stream('a')], spacingMs: 2000 });
    const page = await fetchNow(fake, at('2026-10-15T11:00:00Z', 5));
    expect(page.lines).toHaveLength(10);
    expect(page.anchor).toBe(5);
    expect(page.lines[5].timestamp).toBe('2026-10-15T11:00:00.000Z');
  });

  it('gives every line a cursor of its own', () => {
    const fake = new FakeClient({ lines: 50, streams: [stream('a'), stream('b')] });
    const cursors = new Set(Array.from({ length: 50 }, (_, i) => fake.lineAt(i).cursor));
    expect(cursors.size).toBe(50);
  });

  it('writes the streams in a fixed rotation, each in proportion to its weight', () => {
    const fake = new FakeClient({ lines: 120, streams: [stream('a'), stream('b'), stream('c')] });
    const pods = Array.from({ length: 6 }, (_, i) => fake.lineAt(i).stream.pod);
    expect(pods).toEqual(['a', 'b', 'c', 'a', 'b', 'c']);

    const weighted = new FakeClient({ lines: 120, streams: [stream('a', 10), stream('b', 10), stream('c', 1)] });
    const count = (pod: string) =>
      Array.from({ length: 105 }, (_, i) => weighted.lineAt(i).stream.pod).filter((p) => p === pod).length;
    expect([count('a'), count('b'), count('c')]).toEqual([50, 50, 5]);
    // Spread through the round, not bunched at its start.
    const round = Array.from({ length: 21 }, (_, i) => weighted.lineAt(i).stream.pod).join('');
    expect(round).not.toMatch(/aaaaa/);
  });

  it('carries each stream’s place', () => {
    const fake = new FakeClient({ lines: 2, streams: [stream('a'), stream('b')] });
    expect(fake.lineAt(1).place.node).toBe('node-b');
    expect(fake.lineAt(1).stream).toEqual(stream('b').stream);
  });

  it('writes nothing but ASCII', () => {
    const fake = new FakeClient({ lines: 30, streams: [stream('a'), stream('b')] });
    for (let i = 0; i < fake.lineCount; i += 1) {
      const { timestamp, cursor, message } = fake.lineAt(i);
      expect(`${timestamp}${cursor}${message}`).toMatch(/^[\x20-\x7e]*$/);
    }
  });
});

// Where a page's after edge goes on from, read off the page a `Continue` from it answers.
async function firstAfter(fake: FakeClient, cursor: Cursor | null): Promise<number> {
  const next = await fetchNow(fake, on(cursor as Cursor, 500));
  return next.lines.length ? indexes(fake, next.lines)[0] : fake.lineCount;
}

describe('fetch', () => {
  it('answers First from the start, cut by the limit, with `next` and no `prev`', async () => {
    const fake = new FakeClient({ lines: 100, repeat: 5 });
    const page = await fetchNow(fake, first(10));
    expect(indexes(fake, page.lines)).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8, 9]);
    expect(page.edges.prev).toBeNull();
    expect(page.edges.follow).toBeNull();
    expect(await firstAfter(fake, page.edges.next)).toBe(10);
    expect(page.anchor).toBeNull();
    expect(page.notices).toEqual([]);
  });

  it('answers a read that reaches the newest line with `follow` at the live end, `repeat` lines back', async () => {
    const fake = new FakeClient({ lines: 8, repeat: 3 });
    const page = await fetchNow(fake, first(10));
    expect(page.lines).toHaveLength(8);
    expect(page.edges.next).toBeNull();
    expect(await firstAfter(fake, page.edges.follow)).toBe(5);
  });

  it('never puts the live end before where the read started', async () => {
    const fake = new FakeClient({ lines: 30, repeat: 25 });
    const head = await fetchNow(fake, first(10));
    const page = await fetchNow(fake, on(head.edges.next as Cursor, 40));
    expect(indexes(fake, page.lines)[0]).toBe(10);
    expect(await firstAfter(fake, page.edges.follow)).toBe(10);

    const tail = await fetchNow(fake, last(10));
    expect(indexes(fake, tail.lines)[0]).toBe(20);
    expect(await firstAfter(fake, tail.edges.follow)).toBe(20);
  });

  it('answers Last from the end, cut by the limit, and reads back from `prev`', async () => {
    const fake = new FakeClient({ lines: 100, repeat: 4 });
    const page = await fetchNow(fake, last(10));
    expect(indexes(fake, page.lines)).toEqual([90, 91, 92, 93, 94, 95, 96, 97, 98, 99]);
    expect(page.edges.next).toBeNull();
    expect(await firstAfter(fake, page.edges.follow)).toBe(96);

    const older = await fetchNow(fake, on(page.edges.prev as Cursor));
    expect(indexes(fake, older.lines)).toEqual([80, 81, 82, 83, 84, 85, 86, 87, 88, 89]);
  });

  it('answers a read that reaches the oldest line with no `prev`', async () => {
    const fake = new FakeClient({ lines: 5 });
    const page = await fetchNow(fake, last(10));
    expect(page.lines).toHaveLength(5);
    expect(page.edges.prev).toBeNull();

    const tail = await fetchNow(fake, last(3));
    const older = await fetchNow(fake, on(tail.edges.prev as Cursor));
    expect(indexes(fake, older.lines)).toEqual([0, 1]);
    expect(older.edges.prev).toBeNull();
  });

  it('answers At with lines either side of the moment and the anchor between', async () => {
    const fake = new FakeClient({ lines: 100, repeat: 2 });
    const page = await fetchNow(fake, at(fake.lineAt(50).timestamp, 5));
    expect(indexes(fake, page.lines)).toEqual([45, 46, 47, 48, 49, 50, 51, 52, 53, 54]);
    expect(page.anchor).toBe(5);
    expect(await firstAfter(fake, page.edges.next)).toBe(55);
    const older = await fetchNow(fake, on(page.edges.prev as Cursor, 1));
    expect(indexes(fake, older.lines)).toEqual([44]);
  });

  it('answers At at a moment written with an offset', async () => {
    const fake = new FakeClient({ lines: 100 });
    const utc = new Date(fake.lineAt(50).timestamp);
    const moment = `${new Date(utc.getTime() + 2 * 3600_000).toISOString().slice(0, -1)}+02:00`;
    const page = await fetchNow(fake, at(moment, 1));
    expect(indexes(fake, page.lines)).toEqual([49, 50]);
  });

  it('answers At near either end with no `prev`, or `follow` and no `next`', async () => {
    const fake = new FakeClient({ lines: 10, repeat: 2 });
    const early = await fetchNow(fake, at(fake.lineAt(1).timestamp, 3));
    expect(indexes(fake, early.lines)).toEqual([0, 1, 2, 3]);
    expect(early.anchor).toBe(1);
    expect(early.edges.prev).toBeNull();

    const late = await fetchNow(fake, at('2030-01-01T00:00:00Z', 3));
    expect(indexes(fake, late.lines)).toEqual([7, 8, 9]);
    expect(late.anchor).toBe(3);
    expect(late.edges.next).toBeNull();
    expect(await firstAfter(fake, late.edges.follow)).toBe(8);
  });

  it('reads each container named in `seen` past its line, and the rest from the cursor', async () => {
    const fake = new FakeClient({ lines: 20, streams: [stream('a'), stream('b')] });
    const head = await fetchNow(fake, first(4));
    // Lines alternate a, b: a's line 8 is held, so a goes on from 10 and b from 4.
    const page = await fetchNow(fake, on(head.edges.next as Cursor, 6, [fake.lineAt(8).cursor]));
    expect(indexes(fake, page.lines)).toEqual([5, 7, 9, 10, 11, 12]);
  });

  it('keeps the newer of two `seen` lines of one container', async () => {
    const fake = new FakeClient({ lines: 20, streams: [stream('a')] });
    const head = await fetchNow(fake, first(4));
    const page = await fetchNow(fake, on(head.edges.next as Cursor, 3, [fake.lineAt(9).cursor, fake.lineAt(6).cursor]));
    expect(indexes(fake, page.lines)).toEqual([10, 11, 12]);
  });
});

describe('the one-sided kinds', () => {
  it('answers Until with the newest lines at or before the moment, the moment’s own included', async () => {
    const fake = new FakeClient({ lines: 20, repeat: 2 });
    const page = await fetchNow(fake, until(fake.lineAt(10).timestamp, 3));
    expect(indexes(fake, page.lines)).toEqual([8, 9, 10]);
    const older = await fetchNow(fake, on(page.edges.prev as Cursor, 2));
    expect(indexes(fake, older.lines)).toEqual([6, 7]);
  });

  it('answers Since with the oldest lines at or after the moment, the moment’s own included', async () => {
    const fake = new FakeClient({ lines: 20, repeat: 2 });
    const page = await fetchNow(fake, since(fake.lineAt(10).timestamp, 3));
    expect(indexes(fake, page.lines)).toEqual([10, 11, 12]);
    expect(await firstAfter(fake, page.edges.next)).toBe(13);
  });

  it('reads each container named in a Since’s `seen` past its line, as a Continue does', async () => {
    const fake = new FakeClient({ lines: 20, streams: [stream('a'), stream('b')] });
    // Lines alternate a, b: a's line 8 is held, so a goes on from 10 and b from the moment.
    const page = await fetchNow(fake, since(fake.lineAt(4).timestamp, 6, [fake.lineAt(8).cursor]));
    expect(indexes(fake, page.lines)).toEqual([5, 7, 9, 10, 11, 12]);
  });

  it('reads each container named in an Until’s `seen` before its line, on every page back', async () => {
    const fake = new FakeClient({ lines: 20, streams: [stream('a'), stream('b')] });
    // a's oldest held is line 6: a reads before it, b back from the moment.
    const page = await fetchNow(fake, until(fake.lineAt(15).timestamp, 4, [fake.lineAt(6).cursor]));
    expect(indexes(fake, page.lines)).toEqual([9, 11, 13, 15]);
    const older = await fetchNow(fake, on(page.edges.prev as Cursor, 3));
    expect(indexes(fake, older.lines)).toEqual([4, 5, 7]);
  });
});

describe('what the fake refuses', () => {
  it('rejects a Continue from a cursor it did not write, or from a line’s', async () => {
    const fake = new FakeClient({ lines: 10 });
    const foreign = ['line-3', 'fake:next:x', 'fake:next:11', 'fake:line:3', ''];
    const answers = await Promise.allSettled(foreign.map((cursor) => fake.fetch(on(cursor))));
    expect(answers.map((a) => a.status)).toEqual(foreign.map(() => 'rejected'));
  });

  it('rejects `seen` beside `prev`, a `seen` it did not write, and a limit under one', async () => {
    const fake = new FakeClient({ lines: 10 });
    const tail = await fetchNow(fake, last(3));
    await expect(fake.fetch(on(tail.edges.prev as Cursor, 3, []))).rejects.toThrow(Error);
    await expect(fake.fetch(on(tail.edges.follow as Cursor, 3, ['fake:line:99']))).rejects.toThrow(Error);
    await expect(fake.fetch(first(0))).rejects.toThrow(Error);
  });

  it('throws from follow with a cursor that is not a `next` or `follow` it wrote', async () => {
    const fake = new FakeClient({ lines: 10 });
    const tail = await fetchNow(fake, last(3));
    expect(() => fake.tail('elsewhere', [], () => {})).toThrow(Error);
    expect(() => fake.tail(tail.edges.prev as Cursor, [], () => {})).toThrow(Error);
  });
});

describe('time', () => {
  it('answers a fetch once `delayMs` has passed', async () => {
    const fake = new FakeClient({ lines: 10, delayMs: 100 });
    let page: Page | undefined;
    fake.fetch(first()).then(
      (p) => {
        page = p;
      },
      () => {},
    );
    await vi.advanceTimersByTimeAsync(99);
    expect(page).toBeUndefined();
    await vi.advanceTimersByTimeAsync(1);
    expect(page?.lines).toHaveLength(10);
  });

  it('stamps appended lines with the clock, never earlier than the line before', () => {
    const fake = new FakeClient({ lines: 2 });
    vi.advanceTimersByTime(5);
    fake.append(2);
    expect(fake.lineCount).toBe(4);
    expect(fake.lineAt(3).timestamp).toBe('2026-10-15T12:00:00.005Z');

    vi.setSystemTime(NOW - 1000);
    fake.append(1);
    expect(fake.lineAt(4).timestamp).toBe('2026-10-15T12:00:00.005Z');
  });

  it('spaces one append’s lines `spacingMs` apart, the last at the clock, never earlier than the line before', () => {
    const fake = new FakeClient({ lines: 1 });
    vi.advanceTimersByTime(10);
    fake.append(3, 4);
    expect([1, 2, 3].map((i) => fake.lineAt(i).timestamp)).toEqual([
      '2026-10-15T12:00:00.002Z',
      '2026-10-15T12:00:00.006Z',
      '2026-10-15T12:00:00.010Z',
    ]);
    fake.append(2, 100);
    expect([4, 5].map((i) => fake.lineAt(i).timestamp)).toEqual([
      '2026-10-15T12:00:00.010Z',
      '2026-10-15T12:00:00.010Z',
    ]);
  });

  it('grows at `rate` lines a second until closed', () => {
    const fake = new FakeClient({ lines: 0, rate: 10 });
    vi.advanceTimersByTime(1000);
    expect(fake.lineCount).toBe(10);
    fake.close();
    vi.advanceTimersByTime(1000);
    expect(fake.lineCount).toBe(10);
  });
});

function record(fake: FakeClient, cursor: Cursor, seen: Cursor[] = []) {
  const frames: TailFrame[] = [];
  const cancel = fake.tail(cursor, seen, (frame) => frames.push(frame));
  const lines = () =>
    indexes(
      fake,
      frames.flatMap((f) => (f.kind === 'Line' ? [f.line] : [])),
    );
  return { frames, cancel, lines };
}

// The live end of a fake of `count` lines with no repeat: a `follow` past every line.
async function liveEnd(fake: FakeClient): Promise<Cursor> {
  return (await fetchNow(fake, last(1))).edges.follow as Cursor;
}

describe('follow', () => {
  it('replays the lines past the cursor once `delayMs` has passed, with what was appended meanwhile', async () => {
    const fake = new FakeClient({ lines: 10, delayMs: 50, repeat: 0 });
    const head = await fetchNow(fake, first(7));
    const follow = record(fake, head.edges.next as Cursor);
    fake.append(1);
    expect(follow.frames).toEqual([]);
    await vi.advanceTimersByTimeAsync(50);
    expect(follow.lines()).toEqual([7, 8, 9, 10]);
    expect(follow.frames.every((f) => f.kind === 'Line')).toBe(true);
  });

  it('sends nothing on a quiet follow', async () => {
    const fake = new FakeClient({ lines: 10, repeat: 0 });
    const follow = record(fake, await liveEnd(fake));
    await vi.advanceTimersByTimeAsync(0);
    expect(follow.frames).toEqual([]);
  });

  it('sends each append’s lines inside the append, one stream’s batch after another', async () => {
    const fake = new FakeClient({ lines: 0, streams: [stream('a'), stream('b')], repeat: 0 });
    const follow = record(fake, await liveEnd(fake));
    await vi.advanceTimersByTimeAsync(0);

    fake.append(4);
    expect(follow.frames.map((f) => (f.kind === 'Line' ? f.line.stream.pod : f.kind))).toEqual(['a', 'a', 'b', 'b']);
    expect(follow.lines()).toEqual([0, 2, 1, 3]);

    fake.append(1);
    expect(follow.lines()).toEqual([0, 2, 1, 3, 4]);
  });

  it('reads each container named in `seen` past its line', async () => {
    const fake = new FakeClient({ lines: 10, streams: [stream('a'), stream('b')] });
    const head = await fetchNow(fake, first(4));
    const follow = record(fake, head.edges.next as Cursor, [fake.lineAt(6).cursor]);
    await vi.advanceTimersByTimeAsync(0);
    // Lines alternate a, b: a goes on past 6, b from 4.
    expect(follow.lines()).toEqual([8, 5, 7, 9]);
  });

  it('delivers nothing once cancelled, before its replay or after', async () => {
    const fake = new FakeClient({ lines: 10, repeat: 0 });
    const head = await fetchNow(fake, first(1));
    const early = record(fake, head.edges.next as Cursor);
    early.cancel();
    await vi.advanceTimersByTimeAsync(0);
    expect(early.frames).toEqual([]);

    const late = record(fake, await liveEnd(fake));
    await vi.advanceTimersByTimeAsync(0);
    late.cancel();
    fake.append(3);
    expect(late.frames).toEqual([]);
  });

  it('repeats `repeat` lines from the live end, and no more', async () => {
    const fake = new FakeClient({ lines: 100, repeat: 7 });
    const tail = await fetchNow(fake, last(50));
    const follow = record(fake, tail.edges.follow as Cursor);
    await vi.advanceTimersByTimeAsync(0);
    expect(follow.lines()).toEqual([93, 94, 95, 96, 97, 98, 99]);
  });
});

describe('the knobs', () => {
  it('fails the next fetch, and only it', async () => {
    const fake = new FakeClient({ lines: 10 });
    fake.failNext('fetch');
    const failed = fake.fetch(first());
    const caught = failed.catch((e: unknown) => e);
    await vi.runOnlyPendingTimersAsync();
    expect(await caught).toBeInstanceOf(Error);
    expect((await fetchNow(fake, first())).lines).toHaveLength(10);
  });

  it('throws from the next follow, and only it', async () => {
    const fake = new FakeClient({ lines: 10 });
    const end = await liveEnd(fake);
    fake.failNext('follow');
    expect(() => fake.tail(end, [], () => {})).toThrow(Error);
    expect(() => fake.tail(end, [], () => {})).not.toThrow();
  });

  it('keeps a skipped stream out of every page and follow, and names it in a notice, until unskipped', async () => {
    const a = stream('a');
    const b = stream('b');
    const fake = new FakeClient({ lines: 10, streams: [a, b], repeat: 0 });
    fake.skip(b.stream);

    const page = await fetchNow(fake, first(10));
    expect(page.lines.map((l) => l.stream.pod)).toEqual(['a', 'a', 'a', 'a', 'a']);
    expect(page.notices).toHaveLength(1);
    expect(page.notices[0].code).toBe('stream-skipped');
    expect(page.notices[0].message).toContain('default/b/app');

    const around = await fetchNow(fake, at(fake.lineAt(5).timestamp, 4));
    expect(indexes(fake, around.lines)).toEqual([2, 4, 6, 8]);
    expect(around.anchor).toBe(2);

    const follow = record(fake, await liveEnd(fake));
    await vi.advanceTimersByTimeAsync(0);
    fake.append(2);
    expect(follow.lines()).toEqual([10]);

    fake.unskip(b.stream);
    expect((await fetchNow(fake, first(20))).notices).toEqual([]);
    fake.append(2);
    expect(follow.lines()).toEqual([10, 12, 13]);
  });

  it('answers the next fetch with no line, its edges where the read would have left them', async () => {
    const fake = new FakeClient({ lines: 100, repeat: 2 });

    fake.emptyNext();
    const head = await fetchNow(fake, first(10));
    expect(head.lines).toEqual([]);
    expect(head.edges.prev).toBeNull();
    expect(await firstAfter(fake, head.edges.next)).toBe(10);

    const middle = await fetchNow(fake, at(fake.lineAt(50).timestamp, 1));
    fake.emptyNext();
    const back = await fetchNow(fake, on(middle.edges.prev as Cursor, 10));
    expect(back.lines).toEqual([]);
    expect(back.edges.prev).not.toBeNull();

    fake.emptyNext();
    const around = await fetchNow(fake, at(fake.lineAt(70).timestamp, 5));
    expect(around.lines).toEqual([]);
    expect(around.anchor).toBe(0);
    expect(await firstAfter(fake, around.edges.next)).toBe(75);

    const near = await fetchNow(fake, at(fake.lineAt(95).timestamp, 1));
    fake.emptyNext();
    const end = await fetchNow(fake, on(near.edges.next as Cursor, 10));
    expect(end.edges.next).toBeNull();
    expect(await firstAfter(fake, end.edges.follow)).toBe(98);

    fake.emptyNext();
    const landing = await fetchNow(fake, last(10));
    expect(landing.edges.next).toBeNull();
    expect(await firstAfter(fake, landing.edges.follow)).toBe(100);

    expect(fake.passedOver()).toEqual([
      { from: 0, to: 10 },
      { from: 39, to: 49 },
      { from: 65, to: 75 },
      { from: 96, to: 98 },
      { from: 90, to: 100 },
    ]);
    const older = await fetchNow(fake, on(back.edges.prev as Cursor, 1));
    expect(indexes(fake, older.lines)).toEqual([38]);
  });

  it('ends every open follow, and sends nothing after', async () => {
    const fake = new FakeClient({ lines: 10, delayMs: 10, repeat: 0 });
    const end = await liveEnd(fake);
    const head = await fetchNow(fake, first(1));
    const live = record(fake, end);
    await vi.advanceTimersByTimeAsync(10);
    const waiting = record(fake, head.edges.next as Cursor);

    fake.endFollows('the pod went away');
    expect(live.frames).toEqual([{ kind: 'Ended', reason: 'the pod went away' }]);
    expect(waiting.frames).toEqual([{ kind: 'Ended', reason: 'the pod went away' }]);

    fake.append(3);
    await vi.advanceTimersByTimeAsync(10);
    expect(live.frames).toHaveLength(1);
    expect(waiting.frames).toHaveLength(1);
  });
});
