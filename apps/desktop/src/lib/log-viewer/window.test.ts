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
import { compareTimestamps } from './lines';
import type { Anchor, FetchQuery, TailFrame, LogLine, Page } from './types';
import { CORE_INITIAL_STATE, type Edge, type Effect, LogWindow, type LogWindowOptions } from './window';

const HEAD: Anchor = { kind: 'Head', at: null };
const TAIL: Anchor = { kind: 'Tail', at: null };
const AT: Anchor = { kind: 'At', at: '2026-10-15T00:00:30Z' };

// Line `i` is stamped `i` seconds past midnight unless told otherwise.
function line(i: number, second = i, container = 'app'): LogLine {
  return {
    timestamp: `2026-10-15T00:${String(Math.floor(second / 60)).padStart(2, '0')}:${String(second % 60).padStart(2, '0')}Z`,
    cursor: `c${i}`,
    message: `line ${i}`,
    truncatedBytes: 0,
    stream: { namespace: 'ns', pod: 'p', container, previous: false },
    place: { node: '', region: '', zone: '', os: '', arch: '' },
    gap: null,
  };
}

const range = (from: number, to: number) => Array.from({ length: to - from }, (_, k) => line(from + k));

type PageOver = { edges?: Partial<Page['edges']>; anchor?: number | null; notices?: Page['notices'] };

function page(from: number, to: number, over: PageOver = {}): Page {
  return {
    lines: range(from, to),
    edges: { prev: `before-${from}`, next: `after-${to}`, follow: null, ...over.edges },
    anchor: over.anchor ?? null,
    notices: over.notices ?? [],
  };
}

// The live end: nothing newer to page to, a follow from here.
const END = { next: null, follow: 'live-end' };
// Nothing older.
const START = { prev: null };

function make(options: Partial<LogWindowOptions> = {}): LogWindow {
  return new LogWindow({
    pinToEnd: true,
    firstPageSize: 50,
    pageSize: 20,
    maxLines: 1000,
    loadMoreThreshold: 2,
    ...options,
  });
}

function landed(anchor: Anchor, p: Page, options: Partial<LogWindowOptions> = {}): LogWindow {
  const w = make(options);
  w.landed(p, anchor);
  return w;
}

const keys = (w: LogWindow) => {
  const { length } = w.getSnapshot();
  return Array.from({ length }, (_, i) => w.keyAt(i));
};
const cursors = (w: LogWindow) => {
  const { length } = w.getSnapshot();
  return Array.from({ length }, (_, i) => w.lineAt(i).cursor);
};

const lineFrame = (i: number): TailFrame => ({ kind: 'Line', line: line(i) });

describe('before any event', () => {
  it('answers the initial snapshot', () => {
    expect(make().getSnapshot()).toBe(CORE_INITIAL_STATE);
    expect(CORE_INITIAL_STATE).toEqual({
      loading: true,
      failed: false,
      pinnedToEnd: false,
      following: false,
      ended: null,
      atEnd: false,
      range: null,
      visible: 0,
      length: 0,
      firstKey: 0,
      before: { more: false, empty: false, loading: false },
      after: { more: false, empty: false, loading: false },
      notices: [],
    });
  });
});

describe('landing', () => {
  it('asks for the first page each anchor reads, changing nothing', () => {
    const w = make({ firstPageSize: 30 });
    expect(w.landing(HEAD)).toEqual({ kind: 'First', limit: 30 });
    expect(w.landing(TAIL)).toEqual({ kind: 'Last', limit: 30 });
    expect(w.landing(AT)).toEqual({ kind: 'At', at: AT.at, limit: 30 });
    expect(w.getSnapshot()).toBe(CORE_INITIAL_STATE);
  });

  it('puts key 0 at the first line for Head', () => {
    const w = make();
    expect(w.landed(page(0, 3, { edges: START }), HEAD)).toEqual({ kind: 'none' });
    expect(keys(w)).toEqual([0, 1, 2]);
  });

  it('puts key 0 at the last line for Tail, and goes to the end', () => {
    const w = make();
    expect(w.landed(page(0, 3, { edges: END }), TAIL)).toEqual({ kind: 'toEnd' });
    expect(keys(w)).toEqual([-2, -1, 0]);
  });

  it('puts key 0 at the page’s anchor for At, and goes to it', () => {
    const w = make();
    expect(w.landed(page(0, 5, { anchor: 2 }), AT)).toEqual({ kind: 'toKey', key: 0 });
    expect(keys(w)).toEqual([-2, -1, 0, 1, 2]);
  });

  it('puts key 0 at the last line for At with no line after the moment', () => {
    const w = make();
    w.landed(page(0, 5, { anchor: 5 }), AT);
    expect(keys(w)).toEqual([-4, -3, -2, -1, 0]);
  });

  it('lands an At with no line at all and goes nowhere', () => {
    const w = make();
    expect(w.landed(page(0, 0, { anchor: 0 }), AT)).toEqual({ kind: 'none' });
    expect(w.getSnapshot().length).toBe(0);
  });

  it('reads both edges off the page, and marks each empty that held no line and said more', () => {
    const w = landed(HEAD, page(0, 3, { edges: START }), { pinToEnd: false });
    expect(w.edges()).toEqual({
      before: { more: false, empty: false, loading: false },
      after: { more: true, empty: false, loading: false },
    });

    const spent = landed(AT, page(0, 0, { anchor: 0 }), { pinToEnd: false });
    expect(spent.edges()).toEqual({
      before: { more: true, empty: true, loading: false },
      after: { more: true, empty: true, loading: false },
    });
    expect(spent.getSnapshot().before).toEqual({ more: true, empty: true, loading: false });
    expect(spent.getSnapshot().after).toEqual({ more: true, empty: true, loading: false });
  });

  it('publishes what it landed', () => {
    const gone = { code: 'stream-skipped', message: 'ns/q/app: gone' };
    const w = landed(HEAD, page(0, 3, { notices: [gone] }), { pinToEnd: false });
    expect(w.getSnapshot()).toMatchObject({ loading: true, failed: false, length: 3, notices: [gone] });
    w.loadingEnded();
    expect(w.getSnapshot().loading).toBe(false);
  });
});

describe('the pin on landing', () => {
  it('follows a Tail landing with pinToEnd', () => {
    const w = landed(TAIL, page(0, 3, { edges: END }));
    expect(w.getSnapshot()).toMatchObject({ pinnedToEnd: true, following: true });
    expect(w.following()).toBe(true);
    expect(w.followFrom().cursor).toBe('live-end');
  });

  it('pins no Tail landing without pinToEnd', () => {
    const w = landed(TAIL, page(0, 3, { edges: END }), { pinToEnd: false });
    expect(w.getSnapshot()).toMatchObject({ pinnedToEnd: false, following: false });
  });

  it('pins no Head or At landing, even when its page reached the end', () => {
    expect(landed(HEAD, page(0, 3, { edges: END })).getSnapshot().pinnedToEnd).toBe(false);
    expect(landed(AT, page(0, 3, { edges: END, anchor: 1 })).getSnapshot().pinnedToEnd).toBe(false);
  });
});

describe('landingFailed', () => {
  it('holds nothing, pages nothing and unpins', () => {
    const w = make();
    w.landingFailed();
    expect(w.getSnapshot()).toMatchObject({ failed: true, pinnedToEnd: false, length: 0 });
    expect(w.startPage('before')).toBeNull();
    expect(w.startPage('after')).toBeNull();
    expect(w.query('after')).toBeNull();
  });
});

describe('tickets', () => {
  it('answers no ticket before landing, while one is out that way, or with no more that way', () => {
    const w = make({ pinToEnd: false });
    expect(w.startPage('after')).toBeNull();

    w.landed(page(10, 20, { edges: START }), HEAD);
    expect(w.startPage('before')).toBeNull();
    const ticket = w.startPage('after');
    expect(ticket).not.toBeNull();
    expect(w.startPage('after')).toBeNull();
    expect(w.query('after')).toEqual({ kind: 'Continue', cursor: 'after-20', seen: ['c19'], limit: 20 });
  });

  it('continues from the before edge’s `prev` with no `seen`', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), { pinToEnd: false });
    expect(w.query('before')).toEqual({ kind: 'Continue', cursor: 'before-10', seen: null, limit: 20 });
  });

  it('continues from the after edge’s `follow` when its `next` is null', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), { pinToEnd: false });
    expect(w.query('after')).toMatchObject({ kind: 'Continue', cursor: 'live-end' });
  });

  it('changes nothing for a stale ticket, paged or failed', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    const ticket = w.startPage('after') as number;
    const listener = vi.fn();
    w.subscribe(listener);

    expect(w.paged(ticket + 100, page(10, 20))).toEqual({ kind: 'none' });
    w.pageFailed(ticket + 100);
    expect(w.getSnapshot().length).toBe(10);
    expect(w.startPage('after')).toBeNull();

    expect(listener).not.toHaveBeenCalled();

    w.pageFailed(ticket);
    expect(w.startPage('after')).not.toBeNull();
  });

  it('drops a page after asked before the follow opened', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    const ticket = w.startPage('after') as number;
    w.followOpened();
    expect(w.paged(ticket, page(10, 20))).toEqual({ kind: 'none' });
    expect(w.getSnapshot().length).toBe(10);
  });

  it('names its edge: a ticket before is not one after', () => {
    const w = landed(AT, page(10, 20, { anchor: 5 }), { pinToEnd: false });
    const before = w.startPage('before') as number;
    const after = w.startPage('after') as number;
    expect(before).not.toBe(after);
    w.paged(before, page(5, 10));
    expect(cursors(w)[0]).toBe('c5');
    w.paged(after, page(20, 25));
    expect(cursors(w).at(-1)).toBe('c24');
  });
});

describe('paged', () => {
  it('prepends less the held before, holds, and reads the before edge alone', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), { pinToEnd: false });
    const ticket = w.startPage('before') as number;
    const effect = w.paged(ticket, page(5, 12, { edges: { ...START, next: null, follow: 'ignored' } }));
    expect(effect).toEqual({ kind: 'hold' });
    expect(cursors(w)).toEqual(range(5, 20).map((l) => l.cursor));
    expect(w.keyAt(w.getSnapshot().length - 1)).toBe(0);
    expect(w.edges().before).toEqual({ more: false, empty: false, loading: false });
    expect(w.query('after')).toMatchObject({ cursor: 'live-end' });
  });

  it('sets empty before when a page held no line and said more', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), { pinToEnd: false });
    w.paged(w.startPage('before') as number, page(0, 0, { edges: { prev: 'older' } }));
    expect(w.edges().before).toEqual({ more: true, empty: true, loading: false });
    expect(w.getSnapshot().before).toEqual({ more: true, empty: true, loading: false });
    expect(w.query('before')).toMatchObject({ cursor: 'older' });
  });

  it('appends less the held after, and moves resume', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    const effect = w.paged(w.startPage('after') as number, page(8, 15, { edges: { prev: null } }));
    expect(effect).toEqual({ kind: 'none' });
    expect(cursors(w)).toEqual(range(0, 15).map((l) => l.cursor));
    expect(w.query('after')).toMatchObject({ cursor: 'after-15' });
    expect(w.edges().before).toEqual({ more: true, empty: false, loading: false });
  });

  it('sets empty after when a page held no line and said more', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    w.paged(w.startPage('after') as number, page(0, 0, { edges: { next: 'on' } }));
    expect(w.getSnapshot().after).toEqual({ more: true, empty: true, loading: false });
  });

  it('never sets empty after on an empty poll of the live end, which spent no budget', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    w.paged(w.startPage('after') as number, page(0, 0, { edges: END }));
    expect(w.getSnapshot().after).toEqual({ more: true, empty: false, loading: false });
  });
});

describe('loading', () => {
  it('holds an edge loading from its page’s start until it lands, publishing each change', () => {
    const w = landed(AT, page(10, 20, { anchor: 5 }), { pinToEnd: false });
    const listener = vi.fn();
    w.subscribe(listener);

    const before = w.startPage('before') as number;
    expect(w.getSnapshot().before).toEqual({ more: true, empty: false, loading: true });
    expect(w.getSnapshot().after.loading).toBe(false);
    expect(listener).toHaveBeenCalledTimes(1);

    w.paged(before, page(5, 10));
    expect(w.getSnapshot().before.loading).toBe(false);
  });

  it('clears an edge’s loading when its page fails, and publishes it', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    const ticket = w.startPage('after') as number;
    const listener = vi.fn();
    w.subscribe(listener);

    w.pageFailed(ticket);
    expect(w.getSnapshot().after).toEqual({ more: true, empty: false, loading: false });
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it('clears both edges on a landing, and the after edge on a pause', () => {
    const w = landed(AT, page(10, 20, { anchor: 5 }), { pinToEnd: false });
    w.startPage('before');
    w.startPage('after');
    w.setPaused(true);
    expect(w.getSnapshot().after.loading).toBe(false);
    expect(w.getSnapshot().before.loading).toBe(true);

    w.landed(page(10, 20, { anchor: 5 }), AT);
    expect(w.getSnapshot().before.loading).toBe(false);
  });
});

describe('readOn', () => {
  it('reads on after a page of nothing but repeats that says more, and not empty', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    w.paged(w.startPage('after') as number, page(5, 10));
    expect(w.readOn('after')).toBe(true);
    expect(w.edges().after.empty).toBe(false);

    w.paged(w.startPage('after') as number, page(10, 12));
    expect(w.readOn('after')).toBe(false);
  });

  it('does not read on after a page of repeats that says no more', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), { pinToEnd: false });
    w.paged(w.startPage('before') as number, page(15, 18, { edges: START }));
    expect(w.readOn('before')).toBe(false);
  });

  it('does not read on after a poll of the live end that brought only repeats', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), { pinToEnd: false });
    w.paged(w.startPage('after') as number, page(15, 20, { edges: END }));
    expect(w.readOn('after')).toBe(false);
  });

  it('stops reading on when the page fails', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), { pinToEnd: false });
    w.paged(w.startPage('before') as number, page(15, 18));
    expect(w.readOn('before')).toBe(true);
    w.pageFailed(w.startPage('before') as number);
    expect(w.readOn('before')).toBe(false);
  });
});

describe('the after edge', () => {
  it('has no more while following or paused, and more otherwise, whatever the page said', () => {
    const pinned = landed(TAIL, page(0, 10, { edges: END }));
    expect(pinned.edges().after).toEqual({ more: false, empty: false, loading: false });
    expect(pinned.query('after')).toBeNull();

    const behind = landed(TAIL, page(0, 10, { edges: END }), { pinToEnd: false });
    expect(behind.edges().after).toEqual({ more: true, empty: false, loading: false });
    expect(behind.getSnapshot().after).toEqual({ more: true, empty: false, loading: false });

    behind.setPaused(true);
    expect(behind.edges().after).toEqual({ more: false, empty: false, loading: false });
    expect(behind.startPage('after')).toBeNull();
  });
});

describe('reaching the bottom', () => {
  it('answers false and wants nothing while following or paused', () => {
    expect(landed(TAIL, page(0, 10, { edges: END })).reachedBottom()).toBe(false);
    const paused = landed(HEAD, page(0, 10), { pinToEnd: false });
    paused.setPaused(true);
    expect(paused.reachedBottom()).toBe(false);
  });

  it('pins when the page after finds no more, whether or not it added lines', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    expect(w.reachedBottom()).toBe(true);
    expect(w.paged(w.startPage('after') as number, page(10, 12, { edges: END }))).toEqual({ kind: 'toEnd' });
    expect(w.getSnapshot()).toMatchObject({ pinnedToEnd: true, following: true });

    const none = landed(HEAD, page(0, 10, { edges: END }), { pinToEnd: false });
    none.reachedBottom();
    expect(none.paged(none.startPage('after') as number, page(8, 10, { edges: END }))).toEqual({ kind: 'toEnd' });
    expect(none.getSnapshot().pinnedToEnd).toBe(true);
  });

  it('gives up the wish on a page that adds lines and finds more', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    w.reachedBottom();
    expect(w.paged(w.startPage('after') as number, page(10, 30))).toEqual({ kind: 'none' });
    w.paged(w.startPage('after') as number, page(30, 32, { edges: END }));
    expect(w.getSnapshot().pinnedToEnd).toBe(false);
  });

  it('keeps the wish through a page of repeats that finds more', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    w.reachedBottom();
    w.paged(w.startPage('after') as number, page(5, 10));
    expect(w.readOn('after')).toBe(true);
    expect(w.paged(w.startPage('after') as number, page(10, 11, { edges: END }))).toEqual({ kind: 'toEnd' });
    expect(w.getSnapshot().pinnedToEnd).toBe(true);
  });

  it('pins nothing without the wish', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    w.paged(w.startPage('after') as number, page(10, 12, { edges: END }));
    expect(w.getSnapshot().pinnedToEnd).toBe(false);
  });

  it('reads on from `follow` once unpinned by a scroll, and pins on a page with no `next`', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }));
    w.scrolledUp();
    expect(w.reachedBottom()).toBe(true);
    expect(w.query('after')).toMatchObject({ cursor: 'live-end' });
    const effect = w.paged(w.startPage('after') as number, page(10, 12, { edges: { next: null, follow: 'later' } }));
    expect(effect).toEqual({ kind: 'toEnd' });
    expect(w.getSnapshot().following).toBe(true);
    expect(w.followFrom().cursor).toBe('later');
  });

  it('unpins and drops the wish on a scroll up', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }));
    w.scrolledUp();
    expect(w.getSnapshot()).toMatchObject({ pinnedToEnd: false, following: false });

    w.reachedBottom();
    w.scrolledUp();
    w.paged(w.startPage('after') as number, page(10, 12, { edges: END }));
    expect(w.getSnapshot().pinnedToEnd).toBe(false);
  });
});

describe('notices', () => {
  const notice = (code: string, message = code) => ({ code, message });

  it('gathers each page’s notices once per code and message, first seen first, and a landing starts over', () => {
    const w = landed(HEAD, page(0, 10, { notices: [notice('a')] }), { pinToEnd: false });
    w.paged(w.startPage('after') as number, page(10, 12, { notices: [notice('b'), notice('a')] }));
    w.paged(w.startPage('before') as number, page(0, 0, { notices: [notice('b', 'other'), notice('c'), notice('c')] }));
    expect(w.getSnapshot().notices).toEqual([notice('a'), notice('b'), notice('b', 'other'), notice('c')]);

    const before = w.getSnapshot().notices;
    w.paged(w.startPage('after') as number, page(12, 14, { notices: [notice('c')] }));
    expect(w.getSnapshot().notices).toBe(before);

    w.landed(page(0, 1), HEAD);
    expect(w.getSnapshot().notices).toEqual([]);
  });

  it('takes a follow’s notice as a page’s, held once', () => {
    const w = landed(TAIL, page(0, 10, { edges: END, notices: [notice('a')] }));
    w.followed([
      { kind: 'Notice', notice: notice('b') },
      { kind: 'Notice', notice: notice('a') },
    ]);
    expect(w.getSnapshot().notices).toEqual([notice('a'), notice('b')]);
  });
});

describe('seen', () => {
  it('sends the newest line held of each container on every page after, and none before', () => {
    const w = landed(
      AT,
      { ...page(0, 0, { anchor: 2 }), lines: [line(0, 0, 'a'), line(1, 1, 'b'), line(2, 2, 'a')] },
      {
        pinToEnd: false,
      },
    );
    expect(w.query('after')).toMatchObject({ seen: ['c2', 'c1'] });
    expect(w.query('before')).toMatchObject({ seen: null });

    w.paged(w.startPage('after') as number, { ...page(3, 3), lines: [line(3, 3, 'b')] });
    expect(w.query('after')).toMatchObject({ seen: ['c2', 'c3'] });

    // An older page leaves the newest where it was.
    w.paged(w.startPage('before') as number, { ...page(0, 0), lines: [line(-1, 0, 'c')] });
    expect(w.query('after')).toMatchObject({ seen: ['c2', 'c3'] });
  });

  it('keys a container’s current and previous runs as one', () => {
    const previous = { ...line(1), stream: { ...line(1).stream, previous: true } };
    const w = landed(HEAD, { ...page(0, 0), lines: [line(0), previous] }, { pinToEnd: false });
    expect(w.query('after')).toMatchObject({ seen: ['c1'] });
  });

  it('follows from the page’s cursor with the newest line of each container, and moves on each frame', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }));
    expect(w.followFrom()).toEqual({ cursor: 'live-end', seen: ['c9'] });
    w.followed([lineFrame(10), { kind: 'Line', line: line(11, 11, 'b') }]);
    expect(w.followFrom()).toEqual({ cursor: 'live-end', seen: ['c10', 'c11'] });
  });

  it('reads on after a follow from the page that pinned it, holding no line twice', async () => {
    vi.useFakeTimers();
    const streams = ['a', 'b'].map((container) => ({
      stream: { namespace: 'ns', pod: 'p', container, previous: false },
      place: { node: '', region: '', zone: '', os: '', arch: '' },
    }));
    const fake = new FakeClient({ lines: 40, streams, repeat: 6 });
    const w = make({ firstPageSize: 10, pageSize: 10 });
    const fetched = async (query: FetchQuery) => {
      const pending = fake.fetch(query);
      await vi.runOnlyPendingTimersAsync();
      return pending;
    };
    w.landed(await fetched(w.landing(TAIL)), TAIL);

    const frames: TailFrame[] = [];
    const { cursor, seen } = w.followFrom();
    const cancel = fake.tail(cursor, seen, (frame) => frames.push(frame));
    await vi.runOnlyPendingTimersAsync();
    fake.append(5);
    w.followed(frames);
    cancel();
    w.scrolledUp();
    fake.append(5);

    w.paged(w.startPage('after') as number, await fetched(w.query('after') as FetchQuery));
    const held = cursors(w);
    expect(new Set(held).size).toBe(held.length);
    expect(held).toHaveLength(50 - 30);
    vi.useRealTimers();
  });
});

describe('followed', () => {
  it('appends less the held and goes to the end', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }));
    expect(w.followed([lineFrame(8), lineFrame(9), lineFrame(10), lineFrame(11)])).toEqual({ kind: 'toEnd' });
    expect(cursors(w).slice(-3)).toEqual(['c9', 'c10', 'c11']);
    expect(w.getSnapshot().length).toBe(12);
  });

  it('never moves resume on a frame', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }));
    w.followed([lineFrame(10), { kind: 'Notice', notice: { code: 'n', message: 'n' } }]);
    expect(w.followFrom().cursor).toBe('live-end');
  });

  it('tells no one about notices or lines it holds', () => {
    const w = landed(TAIL, page(0, 10, { edges: END, notices: [{ code: 'n', message: 'n' }] }));
    const listener = vi.fn();
    w.subscribe(listener);
    w.followed([{ kind: 'Notice', notice: { code: 'n', message: 'n' } }, lineFrame(9)]);
    expect(listener).not.toHaveBeenCalled();
    w.followed([lineFrame(10)]);
    expect(listener).toHaveBeenCalledOnce();
  });

  it('changes nothing while not following: unpinned, paused, ended or not landed', () => {
    const unpinned = landed(TAIL, page(0, 10, { edges: END }), { pinToEnd: false });
    const paused = landed(TAIL, page(0, 10, { edges: END }));
    paused.setPaused(true);
    const ended = landed(TAIL, page(0, 10, { edges: END }));
    ended.followed([{ kind: 'Ended', reason: 'gone' }]);
    const notLanded = make();

    [unpinned, paused, ended, notLanded].forEach((w) => {
      const before = w.getSnapshot();
      expect(w.followed([lineFrame(10), { kind: 'Notice', notice: { code: 'n', message: 'n' } }])).toEqual({
        kind: 'none',
      });
      expect(w.getSnapshot()).toBe(before);
    });
    expect(unpinned.query('after')).toMatchObject({ cursor: 'live-end' });
  });
});

describe('an Ended frame', () => {
  it('closes the follow, ignores what comes after it, and opens the after edge', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }));
    w.followed([lineFrame(10), { kind: 'Ended', reason: 'the pod went away' }, lineFrame(11)]);
    expect(w.getSnapshot()).toMatchObject({
      ended: 'the pod went away',
      following: false,
      pinnedToEnd: true,
      length: 11,
    });
    expect(w.edges().after.more).toBe(true);

    w.landed(page(0, 3, { edges: END }), TAIL);
    expect(w.getSnapshot()).toMatchObject({ ended: null, following: true });
  });
});

describe('setPaused', () => {
  it('closes the follow, asks for nothing after and drops the wish to pin', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }));
    w.setPaused(true);
    expect(w.getSnapshot()).toMatchObject({ following: false, pinnedToEnd: true });
    expect(w.query('after')).toBeNull();

    w.setPaused(false);
    expect(w.getSnapshot().following).toBe(true);
  });

  it('drops a page after asked before it', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    w.reachedBottom();
    const ticket = w.startPage('after') as number;
    w.setPaused(true);
    expect(w.paged(ticket, page(10, 12, { edges: END }))).toEqual({ kind: 'none' });
    expect(w.getSnapshot()).toMatchObject({ length: 10, pinnedToEnd: false });

    w.setPaused(false);
    w.paged(w.startPage('after') as number, page(10, 12, { edges: END }));
    expect(w.getSnapshot().pinnedToEnd).toBe(false);
  });
});

describe('followFailed', () => {
  it('unpins, so the next scroll to the bottom pages, pins and follows', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }));
    w.followFailed();
    expect(w.getSnapshot()).toMatchObject({ pinnedToEnd: false, following: false });

    expect(w.reachedBottom()).toBe(true);
    expect(w.query('after')).toMatchObject({ cursor: 'live-end' });
    expect(w.paged(w.startPage('after') as number, page(10, 11, { edges: END }))).toEqual({ kind: 'toEnd' });
    expect(w.getSnapshot()).toMatchObject({ pinnedToEnd: true, following: true });
  });
});

describe('publishing', () => {
  it('answers the same snapshot until a field changes', () => {
    const w = landed(HEAD, page(0, 10), { pinToEnd: false });
    const first = w.getSnapshot();
    w.setPaused(false);
    w.scrolledUp();
    expect(w.getSnapshot()).toBe(first);

    // A page of repeats changes nothing once it lands; its start and end are what publish.
    w.paged(w.startPage('after') as number, page(5, 10));
    expect(w.getSnapshot()).toEqual(first);
  });

  it('moves length and tells its listeners on every page and batch that adds a line', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }));
    const ticket = w.startPage('before') as number;
    const listener = vi.fn();
    w.subscribe(listener);
    w.paged(ticket, page(5, 10));
    expect(w.getSnapshot().length).toBe(15);
    w.followed([lineFrame(20)]);
    expect(w.getSnapshot().length).toBe(16);
    expect(listener).toHaveBeenCalledTimes(2);
  });

  it('stops telling a listener that unsubscribed', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }));
    const listener = vi.fn();
    w.subscribe(listener)();
    w.followed([lineFrame(10)]);
    expect(listener).not.toHaveBeenCalled();
  });
});

describe('setVisibleRange', () => {
  it('publishes how many lines are on screen and whether the newest is', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }));
    w.setVisibleRange(5, 9);
    expect(w.getSnapshot()).toMatchObject({ visible: 5, atEnd: true });
    w.setVisibleRange(3, 7);
    expect(w.getSnapshot()).toMatchObject({ visible: 5, atEnd: false });
  });

  it('is never at the end while not following', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }), { pinToEnd: false });
    w.setVisibleRange(5, 9);
    expect(w.getSnapshot().atEnd).toBe(false);
  });

  it('spans the earliest and latest visible timestamps when a follow’s lines arrived out of order', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }));
    w.followed([
      { kind: 'Line', line: line(10, 50) },
      { kind: 'Line', line: line(11, 12) },
      { kind: 'Line', line: line(12, 40) },
    ]);
    w.setVisibleRange(9, 12);
    expect(w.getSnapshot().range).toEqual({ from: '2026-10-15T00:00:09Z', to: '2026-10-15T00:00:50Z' });
  });

  it('notifies when atEnd changes though the range moved its end alone', () => {
    const w = make();
    w.landed({ ...page(0, 0, { edges: END }), lines: [line(0, 1), line(1, 1), line(2, 1)] }, TAIL);
    w.setVisibleRange(0, 1);
    const listener = vi.fn();
    w.subscribe(listener);
    w.setVisibleRange(1, 2);
    expect(w.getSnapshot().atEnd).toBe(true);
    expect(listener).toHaveBeenCalledOnce();
    w.setVisibleRange(1, 2);
    expect(listener).toHaveBeenCalledOnce();
  });

  it('publishes nothing on screen before any range, or past what it holds', () => {
    const w = landed(HEAD, page(0, 3), { pinToEnd: false });
    expect(w.getSnapshot()).toMatchObject({ visible: 0, range: null });
    w.setVisibleRange(5, 9);
    expect(w.getSnapshot()).toMatchObject({ visible: 0, range: null });
  });
});

const linesBefore = (frames: TailFrame[]): LogLine[] => {
  const ended = frames.findIndex((f) => f.kind === 'Ended');
  return (ended === -1 ? frames : frames.slice(0, ended)).flatMap((f) => (f.kind === 'Line' ? [f.line] : []));
};

// The walk: a seeded random walk drives a window over the fake's log, modelling the core
// around it, and checks the window's rules after every step.

// Runs `count` async steps one after another.
function inTurn(count: number, run: (n: number) => Promise<void>): Promise<void> {
  return Array.from({ length: count }, (_, n) => n).reduce((prev, n) => prev.then(() => run(n)), Promise.resolve());
}

// A linear congruential generator, so a failing seed replays exactly.
function lcg(seed: number) {
  // The product stays below 2^53, so plain arithmetic is exact.
  let state = seed % 2 ** 32;
  const next = () => {
    state = (state * 1664525 + 1013904223) % 2 ** 32;
    return state / 2 ** 32;
  };
  return {
    chance: (p: number) => next() < p,
    int: (n: number) => Math.floor(next() * n),
  };
}

const VIEW = 5;
const PAGE = 8;
const FIRST_PAGE = 12;

const pod = (name: string, weight: number): FakeStream => ({
  stream: { namespace: 'walk', pod: name, container: 'app', previous: false },
  place: { node: `node-${name}`, region: '', zone: '', os: 'linux', arch: 'amd64' },
  weight,
});

type Pending = {
  window: number;
  /** The landing, or the edge a page went toward with its ticket, and whether it read from a seam. */
  for: { kind: 'landing'; anchor: Anchor } | { kind: 'page'; edge: Edge; ticket: number; seam: boolean };
  /** Spans this fetch's read passed over, counted once its answer is taken. */
  spans: { from: number; to: number }[];
  answer: { page: Page } | { error: unknown } | null;
};

async function walk(seed: number) {
  const rand = lcg(seed);
  const repeat = rand.chance(0.3) ? PAGE + 1 + rand.int(10) : rand.int(PAGE);
  const fake = new FakeClient({
    lines: 120 + rand.int(200),
    streams: [pod('a', 10), pod('b', 10), pod('c', 1)],
    repeat,
  });
  // Most walks bound the window tightly, so drops happen at both edges.
  const maxLines = rand.chance(0.7) ? 12 + rand.int(20) : 1000;

  // Each line's index in the log, by its cursor.
  const indexOf = new Map<string, number>();
  const index = (cursor: string) => {
    for (let i = indexOf.size; i < fake.lineCount; i += 1) indexOf.set(fake.lineAt(i).cursor, i);
    return indexOf.get(cursor) as number;
  };

  let windowId = 0;
  let w = make();
  let landedOk = false;
  let paused = false;
  let top = 0;
  let lastRange = '';
  let outstanding: Record<Edge, number | null> = { before: null, after: null };
  let passed: { from: number; to: number }[] = [];
  let byFollow: Set<string> = new Set();
  let bottomWish = false;
  let sawEnded = false;
  let pinned = false;
  const pending: Pending[] = [];
  // Answers the walk holds until a later step lets them through, in any order.
  const heldBack = new Set<Pending>();
  let cancel: (() => void) | null = null;
  let frames: TailFrame[] = [];
  // What the step did, for the check that nothing else moves the view.
  let moved = false;
  // Every line this landing held and let go, and whether it dropped any line, one it had just
  // added included.
  let dropped = new Set<string>();
  let anyDrop = false;
  // The sweep at the end reads every page as asked.
  let calm = false;

  const length = () => w.getSnapshot().length;
  const maxTop = () => Math.max(length() - VIEW, 0);
  const topKey = () => (length() > 0 ? w.keyAt(Math.min(top, length() - 1)) : null);
  const context = `seed ${seed}`;

  // Pinned turns on only where `allowed` says it may.
  function checkPin(allowed: boolean) {
    const now = w.getSnapshot().pinnedToEnd;
    if (now && !pinned) expect(allowed, `${context}: pinned without cause`).toBe(true);
    pinned = now;
  }

  function apply(effect: Effect, heldKey: number | null) {
    if (effect.kind === 'hold' && heldKey !== null) top = w.indexOfKey(heldKey);
    if (effect.kind === 'toEnd') top = maxTop();
    if (effect.kind === 'toKey') top = w.indexOfKey(effect.key);
    top = Math.min(Math.max(top, 0), maxTop());
  }

  function fetch(query: FetchQuery, of: Pending['for']) {
    const decide = calm ? -1 : rand.int(10);
    if (decide === 0) fake.failNext('fetch');
    if (decide === 1) fake.emptyNext();
    const before = fake.passedOver().length;
    const entry: Pending = { window: windowId, for: of, spans: [], answer: null };
    if (!calm && rand.chance(0.3)) heldBack.add(entry);
    fake.fetch(query).then(
      (p) => {
        entry.answer = { page: p };
      },
      (error: unknown) => {
        entry.answer = { error };
      },
    );
    entry.spans = fake.passedOver().slice(before);
    pending.push(entry);
  }

  function pageToward(edge: Edge) {
    const ticket = w.startPage(edge);
    if (ticket === null) return;
    outstanding[edge] = ticket;
    const query = w.query(edge) as FetchQuery;
    fetch(query, { kind: 'page', edge, ticket, seam: query.kind === 'Until' || query.kind === 'Since' });
  }

  function closeFollow() {
    cancel?.();
    cancel = null;
  }

  function land(next: Anchor, pinToEnd: boolean) {
    closeFollow();
    windowId += 1;
    w = make({ pinToEnd, firstPageSize: FIRST_PAGE, pageSize: PAGE, maxLines, loadMoreThreshold: 2 });
    landedOk = false;
    paused = false;
    top = 0;
    lastRange = '';
    outstanding = { before: null, after: null };
    passed = [];
    byFollow = new Set();
    bottomWish = false;
    sawEnded = false;
    pinned = false;
    dropped = new Set();
    anyDrop = false;
    fetch(w.landing(next), { kind: 'landing', anchor: next });
  }

  function deliver(entry: Pending) {
    pending.splice(pending.indexOf(entry), 1);
    if (entry.window !== windowId || entry.answer === null) return;
    const { answer } = entry;
    if (entry.for.kind === 'landing') {
      if ('page' in answer) {
        apply(w.landed(answer.page, entry.for.anchor), null);
        landedOk = true;
        passed.push(...entry.spans);
        moved = true;
        checkPin(entry.for.anchor.kind === 'Tail' && w.getSnapshot().pinnedToEnd);
      } else {
        w.landingFailed();
        checkPin(false);
      }
      w.loadingEnded();
      return;
    }
    const { edge, ticket } = entry.for;
    const accepted = outstanding[edge] === ticket;
    if (accepted) outstanding[edge] = null;
    if ('error' in answer) {
      w.pageFailed(ticket);
      // A failed fetch leaves its edge to be asked again.
      if (accepted && w.query(edge) !== null) pageToward(edge);
      return;
    }
    const lengthBefore = length();
    const heldCursors = heldNow();
    const heldIndexes = [...heldCursors].map(index);
    const heldKey = topKey();
    const snapBefore = w.getSnapshot();
    apply(
      tracking(() => w.paged(ticket, answer.page), accepted ? answer.page.lines : []),
      heldKey,
    );
    // A page extends the stretch on its own side: its new lines are past every line held. A
    // seam page, or any page once the window has dropped, reads per container instead.
    const fresh = [...heldNow()].filter((c) => !heldCursors.has(c)).map(index);
    if (heldIndexes.length > 0 && fresh.length > 0 && !entry.for.seam && !anyDrop) {
      if (edge === 'before')
        expect(Math.max(...fresh), `${context}: a page before inside the stretch`).toBeLessThan(
          Math.min(...heldIndexes),
        );
      else
        expect(Math.min(...fresh), `${context}: a page after inside the stretch`).toBeGreaterThan(
          Math.max(...heldIndexes),
        );
    }
    if (!accepted) {
      expect(w.getSnapshot(), `${context}: a stale page changed the window`).toBe(snapBefore);
      return;
    }
    if (paused && edge === 'after') expect(length(), `${context}: a page added lines while paused`).toBe(lengthBefore);
    passed.push(...entry.spans);
    moved = true;
    checkPin(edge === 'after' && answer.page.edges.next === null && bottomWish);
    if (w.readOn(edge)) pageToward(edge);
  }

  function flush() {
    if (frames.length === 0) return;
    const batch = frames;
    frames = [];
    const wasFollowing = w.following();
    const heldCursors = heldNow();
    const effect = tracking(
      () => w.followed(batch),
      // The window reads no frame past an `Ended`.
      wasFollowing ? linesBefore(batch) : [],
    );
    const fresh = [...heldNow()].filter((c) => !heldCursors.has(c));
    fresh.forEach((c) => byFollow.add(c));
    if (fresh.length > 0) expect(wasFollowing, `${context}: a closed follow added lines`).toBe(true);
    if (effect.kind !== 'none') moved = true;
    apply(effect, null);
    if (w.getSnapshot().ended !== null) sawEnded = true;
    checkPin(false);
  }

  function syncFollow() {
    if (w.following() && cancel === null) {
      w.followOpened();
      outstanding.after = null;
      try {
        const { cursor, seen } = w.followFrom();
        cancel = fake.tail(cursor, seen, (frame) => frames.push(frame));
      } catch {
        w.followFailed();
        expect(w.getSnapshot().pinnedToEnd, `${context}: a refused follow left it pinned`).toBe(false);
        checkPin(false);
      }
    } else if (!w.following() && cancel !== null) {
      // The unsubscribe lags the close: frames already queued still reach `followed`.
      closeFollow();
    }
  }

  function syncRange() {
    if (length() === 0) return;
    const start = top;
    const end = Math.min(top + VIEW - 1, length() - 1);
    // A drop the range brings keeps the rows on screen by their keys.
    const keyAtTop = w.keyAt(start);
    tracking(() => w.setVisibleRange(start, end));
    top = Math.min(Math.max(w.indexOfKey(keyAtTop), 0), maxTop());
    const key = `${w.keyAt(start)}:${w.keyAt(end)}`;
    if (key === lastRange) return;
    lastRange = key;
    if (start <= 2) pageToward('before');
    if (end >= length() - 3) pageToward('after');
  }

  // The adapter's reactions, until nothing more happens.
  async function settle(round = 0): Promise<void> {
    syncFollow();
    await vi.advanceTimersByTimeAsync(0);
    const ready = pending.filter((entry) => entry.answer !== null && !heldBack.has(entry));
    ready.forEach(deliver);
    const hadFrames = frames.length > 0;
    flush();
    syncFollow();
    syncRange();
    const quiet = ready.length === 0 && !hadFrames && !pending.some((e) => e.answer === null);
    if (!quiet && round < 40) await settle(round + 1);
  }

  function scrollTo(row: number) {
    const target = Math.min(Math.max(row, 0), maxTop());
    if (target < top) {
      w.scrolledUp();
      bottomWish = false;
      top = target;
      // A scroll up that ended off the bottom leaves it unpinned.
      expect(w.getSnapshot().pinnedToEnd, `${context}: pinned after a scroll up`).toBe(false);
      pinned = false;
    } else {
      top = target;
    }
    moved = true;
    if (top === maxTop() && length() > 0 && w.reachedBottom()) {
      bottomWish = true;
      pageToward('after');
    }
  }

  // Runs a window call that brings `incoming` and notes what it dropped, a line just brought
  // included; a drop clears the ticket at its edge, as the walk's record must.
  function tracking<T>(call: () => T, incoming: readonly LogLine[] = []): T {
    const before = Array.from({ length: length() }, (_, i) => w.lineAt(i).cursor);
    const result = call();
    const now = heldNow();
    if (incoming.some((l) => !now.has(l.cursor))) anyDrop = true;
    const firstKept = before.findIndex((c) => now.has(c));
    before.forEach((c, i) => {
      if (now.has(c)) return;
      dropped.add(c);
      anyDrop = true;
      outstanding[firstKept === -1 || i < firstKept ? 'before' : 'after'] = null;
    });
    return result;
  }

  function heldNow(): Set<string> {
    return new Set(Array.from({ length: length() }, (_, i) => w.lineAt(i).cursor));
  }

  const inPassed = (i: number) => passed.some(({ from, to }) => from <= i && i < to);

  // `check` runs after every step, so each rule over the held lines finds its first breach and
  // asserts once: an `expect` per line is most of the walk's time under coverage.
  function check() {
    const snap = w.getSnapshot();
    const held = Array.from({ length: snap.length }, (_, i) => w.lineAt(i).cursor);
    expect(new Set(held).size, `${context}: a line held twice`).toBe(held.length);
    const stranger = held.findIndex((c, i) => w.lineAt(i) !== fake.lineAt(index(c)));
    expect(stranger, `${context}: a line not the fake's`).toBe(-1);
    if (snap.length > 0) expect(snap.firstKey, `${context}: firstKey disagrees`).toBe(w.keyAt(0));

    // Each container's held lines are one run of its log, but for lines an empty read passed over.
    const byContainer = new Map<string, number[]>();
    held.map(index).forEach((i) => {
      const key = fake.lineAt(i).stream.pod;
      byContainer.set(key, [...(byContainer.get(key) ?? []), i]);
    });
    let broken: string | null = null;
    byContainer.forEach((indexes, name) => {
      const sorted = indexes.toSorted((a, b) => a - b);
      for (let k = 1; k < sorted.length; k += 1) {
        for (let i = sorted[k - 1] + 1; i < sorted[k]; i += 1) {
          if (fake.lineAt(i).stream.pod === name && !inPassed(i)) broken ??= name;
        }
      }
    });
    expect(broken, `${context}: ${broken}'s held lines are not one run`).toBeNull();

    // Held lines and passed-over spans are one stretch of the log, until the window drops.
    const covered = new Set(held.map(index));
    passed.forEach(({ from, to }) => {
      for (let i = from; i < to; i += 1) covered.add(i);
    });
    if (covered.size > 0 && !anyDrop) {
      const all = [...covered];
      const low = Math.min(...all);
      const high = Math.max(...all);
      expect(high - low + 1, `${context}: the lines held are not one stretch`).toBe(covered.size);
    }

    // What pages brought is in log order; a follow's lines keep their arrival order.
    if (!anyDrop) {
      const paged = held.filter((c) => !byFollow.has(c)).map(index);
      const outOfOrder = paged.findIndex((value, i) => i > 0 && value <= paged[i - 1]);
      expect(outOfOrder, `${context}: paged lines out of log order`).toBe(-1);
    }

    expect(snap.following, `${context}: following disagrees`).toBe(
      landedOk && snap.pinnedToEnd && !paused && snap.ended === null,
    );
    if (sawEnded) expect(snap.following, `${context}: an ended follow reopened`).toBe(false);
    (['before', 'after'] as const).forEach((edge) => {
      expect(snap[edge].loading, `${context}: ${edge} loading disagrees`).toBe(
        snap[edge].more && outstanding[edge] !== null,
      );
    });
    if (snap.following && cancel !== null) {
      const last = fake.lineCount - 1;
      // A drop by arrival order can take a stream's newest lines; the before seam then reaches them.
      const query = w.query('before');
      const behindSeam = query?.kind === 'Until' && compareTimestamps(query.at, fake.lineAt(last).timestamp) >= 0;
      const holdsLast =
        held.includes(fake.lineAt(last).cursor) ||
        passed.some(({ from, to }) => from <= last && last < to) ||
        behindSeam;
      expect(holdsLast, `${context}: following without the newest line`).toBe(true);
    }
  }

  const randomAnchor = (): Anchor => {
    const kind = (['Head', 'Tail', 'At'] as const)[rand.int(3)];
    return kind === 'At' ? { kind, at: fake.lineAt(rand.int(fake.lineCount)).timestamp } : { kind, at: null };
  };

  const first = randomAnchor();
  land(first, rand.chance(0.5));
  await settle();
  check();

  async function step(n: number) {
    moved = false;
    const keyBefore = topKey();
    const windowBefore = windowId;
    const actions = 1 + rand.int(3);
    for (let a = 0; a < actions; a += 1) {
      switch (rand.int(14)) {
        case 0:
          scrollTo(rand.int(maxTop() + 1));
          break;
        case 1:
          scrollTo(maxTop());
          break;
        case 2:
          scrollTo(top - 1);
          if (top === 0) {
            // A wheel up at the top still moves up as far as the user means.
            w.scrolledUp();
            bottomWish = false;
            pinned = w.getSnapshot().pinnedToEnd;
          }
          break;
        case 3:
          paused = !paused;
          w.setPaused(paused);
          if (paused) {
            outstanding.after = null;
            bottomWish = false;
          }
          checkPin(false);
          break;
        case 4:
          // Spaced lines a follow sends stream by stream, out of time order.
          fake.append(1 + rand.int(15), rand.int(3));
          break;
        case 5: {
          const next = randomAnchor();
          land(next, next.kind === 'Tail');
          break;
        }
        case 6: {
          const edge: Edge = rand.chance(0.5) ? 'before' : 'after';
          if (w.edges()[edge].empty) pageToward(edge);
          break;
        }
        case 7: {
          const answered = pending.filter((entry) => entry.answer !== null);
          if (answered.length > 0) deliver(answered[rand.int(answered.length)]);
          break;
        }
        case 8:
          fake.append(1);
          break;
        case 9:
          fake.endFollows('ended by the walk');
          break;
        case 10:
          if (rand.chance(0.25)) fake.failNext('follow');
          break;
        case 11:
          vi.advanceTimersByTime(rand.int(1000));
          break;
        case 12:
          // A spaced burst a follow sends out of time order, then small batches whose drops
          // straddle its out-of-order lines.
          if (w.following()) {
            fake.append(PAGE + rand.int(8), 1 + rand.int(2));
            // oxlint-disable-next-line no-await-in-loop -- each action settles before the next
            await settle();
            // oxlint-disable-next-line no-await-in-loop -- each action settles before the next
            await inTurn(4, async () => {
              vi.advanceTimersByTime(1);
              fake.append(1 + rand.int(3));
              await settle();
            });
          }
          break;
        default:
          heldBack.clear();
      }
    }
    await settle();
    check();
    if (!moved && windowId === windowBefore && keyBefore !== null) {
      expect(topKey(), `${context}: the view moved on its own at step ${n}`).toBe(keyBefore);
    }
  }

  await inTurn(60, (n) => step(n));

  // Scrolling back to the start and forward to the end brings every line the window dropped.
  if (landedOk && !w.getSnapshot().failed) {
    calm = true;
    heldBack.clear();
    if (paused) {
      paused = false;
      w.setPaused(false);
    }
    await settle();
    check();
    const swept = new Set(heldNow());
    const sweep = async (edge: Edge, done: () => boolean) => {
      for (let n = 0; n < 300 && !done(); n += 1) {
        scrollTo(edge === 'before' ? 0 : maxTop());
        pageToward(edge);
        // oxlint-disable-next-line no-await-in-loop -- each page settles before the next is asked
        await settle();
        check();
        heldNow().forEach((c) => swept.add(c));
      }
      // oxlint-disable-next-line vitest/no-conditional-expect -- the seed decides whether the sweep runs
      expect(done(), `${context}: the sweep ${edge} never ended`).toBe(true);
    };
    await sweep('before', () => !w.edges().before.more);
    // An ended follow leaves the after edge open, so the newest line held ends the sweep too.
    await sweep('after', () => !w.edges().after.more || heldNow().has(fake.lineAt(fake.lineCount - 1).cursor));
    const lost = [...dropped].find((c) => !inPassed(index(c)) && !swept.has(c));
    // oxlint-disable-next-line vitest/no-conditional-expect -- the seed decides whether the sweep runs
    expect(lost, `${context}: ${lost} never came back`).toBeUndefined();
  }
  closeFollow();
}

describe('firstKey', () => {
  it('is the first held line’s key, moving with a prepend, and 0 with none', () => {
    const w = landed(TAIL, page(10, 13, { edges: END }));
    expect(w.getSnapshot().firstKey).toBe(-2);
    const ticket = w.startPage('before') as number;
    w.paged(ticket, page(8, 10));
    expect(w.getSnapshot().firstKey).toBe(-4);
    expect(landed(HEAD, page(0, 0, { edges: { ...START, ...END } })).getSnapshot().firstKey).toBe(0);
  });
});

// A window of at most 10 lines, a margin of 3 past the visible range.
const BOUND = { maxLines: 10, loadMoreThreshold: 2 };
const frames = (from: number, to: number) => range(from, to).map((l): TailFrame => ({ kind: 'Line', line: l }));

describe('a follow past maxLines', () => {
  it('drops at the before edge, keeping the kept keys and moving firstKey, and goes to the end', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }), BOUND);
    expect(w.followed(frames(10, 15))).toEqual({ kind: 'toEnd' });
    expect(cursors(w)).toEqual(range(5, 15).map((l) => l.cursor));
    expect(keys(w)).toEqual(Array.from({ length: 10 }, (_, k) => k - 4));
    const snap = w.getSnapshot();
    expect(snap.length).toBe(10);
    expect(snap.firstKey).toBe(-4);
    expect(snap.before).toEqual({ more: true, empty: false, loading: false });
    expect(snap.following).toBe(true);
  });

  it('drops nothing while at or under maxLines', () => {
    const w = landed(TAIL, page(0, 5, { edges: END }), BOUND);
    w.followed(frames(5, 10));
    expect(w.getSnapshot()).toMatchObject({ length: 10, firstKey: -4, before: { more: true } });
    expect(w.query('before')).toEqual({ kind: 'Continue', cursor: 'before-0', seen: null, limit: 20 });
  });
});

// Lines alternating containers a and b, line `i` stamped `i` seconds past midnight.
const mixed = (from: number, to: number) =>
  Array.from({ length: to - from }, (_, k) => line(from + k, from + k, (from + k) % 2 === 0 ? 'a' : 'b'));
const pageOf = (lines: LogLine[], edges: Partial<Page['edges']> = {}): Page => ({
  lines,
  edges: { prev: 'before-p', next: 'after-p', follow: null, ...edges },
  anchor: null,
  notices: [],
});
const at = (i: number) => line(i).timestamp;

describe('a page past maxLines', () => {
  it('drops a page before at the before edge while following, and keeps following', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), BOUND);
    const ticket = w.startPage('before') as number;
    expect(w.paged(ticket, page(5, 10))).toEqual({ kind: 'hold' });
    expect(cursors(w)).toEqual(range(10, 20).map((l) => l.cursor));
    expect(w.getSnapshot()).toMatchObject({ following: true, pinnedToEnd: true, firstKey: -9 });
    expect(w.query('before')).toEqual({ kind: 'Until', at: at(9), seen: ['c10'], limit: 20 });
  });

  it('drops a page after at the before edge, and holds', () => {
    const w = landed(HEAD, page(0, 10, { edges: START }), BOUND);
    const ticket = w.startPage('after') as number;
    expect(w.paged(ticket, page(10, 15))).toEqual({ kind: 'hold' });
    expect(cursors(w)).toEqual(range(5, 15).map((l) => l.cursor));
    expect(w.getSnapshot()).toMatchObject({ firstKey: 5, before: { more: true, empty: false } });
    expect(w.query('before')).toEqual({ kind: 'Until', at: at(4), seen: ['c5'], limit: 20 });
    expect(w.query('after')).toEqual({ kind: 'Continue', cursor: 'after-15', seen: ['c14'], limit: 20 });
  });

  it('drops a page before at the after edge, unpins, and reads on after from the seam past the newest kept', () => {
    const w = landed(TAIL, pageOf(mixed(10, 20), END), BOUND);
    w.setPaused(true);
    const ticket = w.startPage('before') as number;
    expect(w.paged(ticket, pageOf(mixed(5, 10)))).toEqual({ kind: 'hold' });
    expect(cursors(w)).toEqual(mixed(5, 15).map((l) => l.cursor));
    expect(w.getSnapshot()).toMatchObject({ pinnedToEnd: false, firstKey: -14 });
    expect(w.query('after')).toBeNull();

    w.setPaused(false);
    expect(w.getSnapshot().following).toBe(false);
    expect(w.query('after')).toEqual({ kind: 'Since', at: at(15), seen: ['c13', 'c14'], limit: 20 });
  });
});

describe('the drop’s cap', () => {
  it('never takes the visible range or the margin past it, capping the drop short', () => {
    const w = landed(HEAD, page(0, 10, { edges: START }), BOUND);
    w.setVisibleRange(2, 5);
    expect(w.paged(w.startPage('after') as number, page(10, 15))).toEqual({ kind: 'none' });
    expect(w.getSnapshot()).toMatchObject({ length: 15, firstKey: 0, before: { more: false } });

    w.setVisibleRange(6, 9);
    expect(w.paged(w.startPage('after') as number, page(15, 20))).toEqual({ kind: 'hold' });
    expect(cursors(w)).toEqual(range(3, 20).map((l) => l.cursor));
    expect(w.getSnapshot()).toMatchObject({ firstKey: 3, visible: 4, range: { from: at(6), to: at(9) } });
  });

  it('reads the range below the lines a page just put before it', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), BOUND);
    w.setVisibleRange(5, 9);
    w.paged(w.startPage('before') as number, page(5, 10));
    expect(cursors(w)).toEqual(range(10, 20).map((l) => l.cursor));
    expect(w.getSnapshot()).toMatchObject({ visible: 5, range: { from: at(15), to: at(19) }, atEnd: true });
  });

  it('keeps the margin after the range when it drops at the after edge', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), { ...BOUND, pinToEnd: false });
    w.setVisibleRange(0, 4);
    w.paged(w.startPage('before') as number, page(5, 10));
    // The range is 5..9 once the page is in; it and three lines past it stay.
    expect(cursors(w)).toEqual(range(5, 18).map((l) => l.cursor));
  });
});

describe('a drop of nothing', () => {
  it('leaves every edge as it was when a page ends at maxLines', () => {
    const w = landed(HEAD, page(0, 5, { edges: START }), BOUND);
    w.paged(w.startPage('after') as number, page(5, 10));
    expect(w.getSnapshot()).toMatchObject({ length: 10, firstKey: 0, before: { more: false } });
    expect(w.query('before')).toBeNull();
  });

  it('sets no seam, clears no ticket or cursor and keeps the pin when the cap leaves nothing to drop', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), { ...BOUND, pinToEnd: false });
    w.setVisibleRange(5, 9);
    const after = w.startPage('after');
    w.paged(w.startPage('before') as number, page(5, 10));
    expect(w.getSnapshot()).toMatchObject({ length: 15, after: { more: true, loading: true } });
    expect(w.query('after')).toEqual({ kind: 'Continue', cursor: 'live-end', seen: ['c19'], limit: 20 });
    expect(after).not.toBeNull();
  });
});

describe('the seam', () => {
  // A window of 5..14 whose before edge dropped 0..4.
  const droppedBefore = () => {
    const w = landed(HEAD, page(0, 10, { edges: START }), BOUND);
    w.paged(w.startPage('after') as number, page(10, 15));
    return w;
  };

  it('is replaced by the seam page’s `prev`, which pages on as a Continue', () => {
    const w = droppedBefore();
    w.paged(w.startPage('before') as number, page(2, 5, { edges: { prev: 'before-2' } }));
    expect(w.query('before')).toEqual({ kind: 'Continue', cursor: 'before-2', seen: null, limit: 20 });
    // Three in at the before edge, three out at the after edge.
    expect(cursors(w)).toEqual(range(2, 12).map((l) => l.cursor));
    expect(w.query('after')).toEqual({ kind: 'Since', at: at(12), seen: ['c11'], limit: 20 });
  });

  it('reads on after a seam page of repeats alone that says more', () => {
    const w = droppedBefore();
    w.paged(w.startPage('before') as number, page(5, 8, { edges: { prev: 'before-5' } }));
    expect(w.readOn('before')).toBe(true);
    expect(w.query('before')).toEqual({ kind: 'Continue', cursor: 'before-5', seen: null, limit: 20 });
  });

  it('is replaced after by the seam page’s `resumeOf`, pinning on a wish when it finds no more', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), { ...BOUND, pinToEnd: false });
    w.paged(w.startPage('before') as number, page(5, 10));
    expect(w.reachedBottom()).toBe(true);
    expect(w.paged(w.startPage('after') as number, page(15, 20, { edges: END }))).toEqual({ kind: 'toEnd' });
    expect(w.getSnapshot()).toMatchObject({ following: true });
    expect(w.followFrom()).toEqual({ cursor: 'live-end', seen: ['c19'] });
  });

  it('keeps the farthest stamp of every drop at the before edge, though a later drop’s lies short of it', () => {
    const w = landed(TAIL, page(0, 2, { edges: END }), { ...BOUND, maxLines: 4 });
    w.followed([line(2, 40), line(3, 3)].map((l): TailFrame => ({ kind: 'Line', line: l })));
    [line(4, 41), line(5, 42), line(6, 43), line(7, 44)].forEach((l) => w.followed([{ kind: 'Line', line: l }]));
    // Dropped c0 at 0s, c1 at 1s, c2 at 40s and c3 at 3s.
    expect(cursors(w)).toEqual(['c4', 'c5', 'c6', 'c7']);
    expect(w.query('before')).toEqual({ kind: 'Until', at: at(40), seen: ['c4'], limit: 20 });
  });

  it('keeps the farthest stamp of every drop at the after edge, though a later drop’s lies short of it', () => {
    const w = landed(HEAD, pageOf([line(10), line(11), line(12), line(13, 5)]), { ...BOUND, maxLines: 4 });
    w.paged(w.startPage('before') as number, pageOf([line(9)]));
    w.paged(w.startPage('before') as number, pageOf([line(8)]));
    expect(cursors(w)).toEqual(['c8', 'c9', 'c10', 'c11']);
    expect(w.query('after')).toEqual({ kind: 'Since', at: at(5), seen: ['c11'], limit: 20 });
  });

  it('clears the ticket at its edge: the page in flight that way changes nothing when it lands', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), { ...BOUND, pinToEnd: false });
    const before = w.startPage('before') as number;
    w.paged(w.startPage('after') as number, page(20, 25));
    expect(w.getSnapshot().before.loading).toBe(false);
    const snap = w.getSnapshot();
    expect(w.paged(before, page(5, 10))).toEqual({ kind: 'none' });
    expect(w.getSnapshot()).toBe(snap);
    expect(w.query('before')).toEqual({ kind: 'Until', at: at(14), seen: ['c15'], limit: 20 });
  });

  it('is cleared by a landing', () => {
    const w = droppedBefore();
    w.landed(page(30, 40, { edges: { prev: 'before-30' } }), HEAD);
    expect(w.query('before')).toEqual({ kind: 'Continue', cursor: 'before-30', seen: null, limit: 20 });
    expect(w.query('after')).toEqual({ kind: 'Continue', cursor: 'after-40', seen: ['c39'], limit: 20 });
  });
});

describe('a drop while following', () => {
  it('protects the end it goes to, not the range before the batch, so a batch larger than the room ends at maxLines', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }), BOUND);
    w.setVisibleRange(5, 9);
    w.followed(frames(10, 20));
    expect(w.getSnapshot()).toMatchObject({ length: 10, firstKey: 1 });

    w.setVisibleRange(5, 9);
    expect(w.getSnapshot()).toMatchObject({ visible: 5, atEnd: true });
  });

  it('keeps the margin above the end it goes to', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }), { ...BOUND, maxLines: 4 });
    w.setVisibleRange(5, 9);
    w.followed(frames(10, 12));
    // Five lines on screen at the end and three above them stay.
    expect(w.getSnapshot().length).toBe(8);
  });

  it('drops nothing on a range whose lines a drop took, until the adapter reports again', () => {
    const w = landed(TAIL, page(0, 10, { edges: END }), BOUND);
    w.setVisibleRange(5, 9);
    // A batch far larger than the window takes the lines the range named.
    w.followed(frames(10, 40));
    expect(w.getSnapshot().length).toBe(10);
    w.scrolledUp();

    w.paged(w.startPage('before') as number, pageOf(range(100, 105)));
    expect(w.getSnapshot().length).toBe(15);
    expect(w.lineAt(14).cursor).toBe('c39');

    // Reported again, the range and its margin are what the next drop keeps.
    w.setVisibleRange(5, 9);
    w.paged(w.startPage('before') as number, pageOf(range(105, 110)));
    expect(w.getSnapshot().length).toBe(18);
  });

  it('leaves setVisibleRange to record the range alone, while not following', () => {
    const w = landed(HEAD, page(0, 10, { edges: START }), BOUND);
    w.setVisibleRange(5, 9);
    w.paged(w.startPage('after') as number, page(10, 20));
    w.setVisibleRange(15, 19);
    expect(w.getSnapshot().length).toBe(18);
  });
});

describe('reading a dropped edge back', () => {
  // Pages `edge` once through the fake and answers what the window answered.
  async function pageThrough(w: LogWindow, fake: FakeClient, edge: Edge): Promise<Effect> {
    const ticket = w.startPage(edge) as number;
    return w.paged(ticket, await fake.fetch(w.query(edge) as FetchQuery));
  }

  it('sends each container’s oldest held line in the Until, so none reads past its held run', async () => {
    const fake = new FakeClient({ lines: 40, streams: [pod('a', 1), pod('b', 1)] });
    const w = make({ ...BOUND, pinToEnd: false, firstPageSize: 10, pageSize: 10 });
    w.landed(await fake.fetch(w.landing(HEAD)), HEAD);
    await pageThrough(w, fake, 'after');
    const until = w.query('before');
    expect(until).toEqual({
      kind: 'Until',
      at: fake.lineAt(9).timestamp,
      seen: ['fake:line:10', 'fake:line:11'],
      limit: 10,
    });

    await pageThrough(w, fake, 'before');
    expect(cursors(w)).toEqual(Array.from({ length: 10 }, (_, i) => `fake:line:${i}`));
    expect(w.query('before')).toBeNull();
  });

  it('brings a line past a hole back with its `gap`, as the sidecar serves it', async () => {
    const gap = { after: '2026-10-15T11:00:00.000Z', before: '2026-10-15T11:30:00.000Z' };
    const fake = new FakeClient({ lines: 40, gaps: { 5: gap } });
    const w = make({ ...BOUND, pinToEnd: false, firstPageSize: 10, pageSize: 10 });
    w.landed(await fake.fetch(w.landing(HEAD)), HEAD);
    await pageThrough(w, fake, 'after');
    expect(cursors(w)).not.toContain('fake:line:5');

    await pageThrough(w, fake, 'before');
    expect(w.lineAt(5)).toMatchObject({ cursor: 'fake:line:5', gap });
  });

  it('keeps every visible key when a page before lands with the range at the top', () => {
    const w = landed(TAIL, page(10, 20, { edges: END }), { ...BOUND, pinToEnd: false });
    w.setVisibleRange(0, 4);
    const visible = keys(w).slice(0, 5);
    w.paged(w.startPage('before') as number, page(5, 10));
    expect(keys(w)).toEqual(expect.arrayContaining(visible));
  });
});

describe('the walk', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.parse('2026-10-15T12:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // 300 seeds of 60 steps, in batches of 50 so no one test nears its timeout on a busy runner.
  const batches = Array.from({ length: 6 }, (_, b) => ({ from: b * 50 + 1, to: b * 50 + 50 }));
  // oxlint-disable-next-line vitest/expect-expect -- `walk` checks the rules after every step
  it.each(batches)(
    'keeps every rule through seeds $from to $to, 60 steps each',
    async ({ from, to }) => {
      await inTurn(to - from + 1, (n) => walk(from + n));
    },
    60_000,
  );
});
