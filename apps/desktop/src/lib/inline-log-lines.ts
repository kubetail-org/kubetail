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

// The lines a `LogsView` card draws inline: a fixed window of eight, with no scroller. The
// chat's newest pinned `Tail` view runs live; every other card reads one page and stops.

import { useEffect, useState, useSyncExternalStore } from 'react';

import { compareTimestamps, resumeOf } from '@/lib/log-viewer';
import type { Client, Cursor, FetchQuery, TailFrame, LogLine, Page } from '@/lib/log-viewer';
import type { LogsViewAction } from '@/lib/logs-view';

/** The lines the window holds. One constant, so the card's height is fixed. */
export const INLINE_LINES = 8;

/** The pages a short read goes on for past the first, before it says more may match. */
export const READ_ON_PAGES = 4;

export type InlinePlan = {
  /** The first read. */
  first: FetchQuery;
  /** Of an `At` page, the lines before its anchor or from it on; every line of the others. */
  keep: 'before' | 'from' | 'all';
  /** The end the window keeps: a tail window its newest lines, the others their first. */
  end: 'tail' | 'head';
  /** Whether the window follows on from its first page. */
  follow: boolean;
  /** Where a frozen tail window ends, RFC 3339. Null for the others. */
  until: string | null;
};

/**
 * What a card reads. `until` is the next view's `resolvedAt`, where this one stopped being
 * live; null while it is the chat's newest view, which alone may run live.
 */
export function inlinePlan(action: LogsViewAction, until: string | null): InlinePlan {
  const { anchor, pinToEnd, resolvedAt } = action;
  const limit = INLINE_LINES;
  if (anchor.kind === 'Head') {
    return { first: { kind: 'First', limit }, keep: 'all', end: 'head', follow: false, until: null };
  }
  if (anchor.kind === 'At') {
    return {
      first: { kind: 'At', at: anchor.at as string, limit },
      keep: 'from',
      end: 'head',
      follow: false,
      until: null,
    };
  }
  // A pinned tail runs live while it is the newest and ends where it stopped being live; an
  // unpinned one never ran and ends at its own moment. The lines before an `At` page's anchor
  // all precede the moment, so a frozen tail holds none at it.
  const moment = pinToEnd ? until : resolvedAt;
  if (moment === null) {
    return { first: { kind: 'Last', limit }, keep: 'all', end: 'tail', follow: true, until: null };
  }
  return { first: { kind: 'At', at: moment, limit }, keep: 'before', end: 'tail', follow: false, until: moment };
}

export type InlineRead = {
  /** Oldest first, at most `INLINE_LINES`. */
  lines: LogLine[];
  /** Whether the read stopped short of eight lines with more that way. */
  more: boolean;
  /** Where a follow goes on from: the first page's `resumeOf`. */
  after: Cursor | null;
};

// The lines of the first page the plan keeps.
function kept(page: Page, keep: InlinePlan['keep']): LogLine[] {
  if (keep === 'all' || page.anchor === null) return page.lines;
  return keep === 'before' ? page.lines.slice(0, page.anchor) : page.lines.slice(page.anchor);
}

/**
 * Reads a window's lines as the plan says. A page short of eight lines with a page on spent its
 * budget, as a selective grep does, so the read goes on that way: back from `prev` for a tail
 * window, on from `next` for the others.
 */
export async function readInline(client: Client, plan: InlinePlan): Promise<InlineRead> {
  const tail = plan.end === 'tail';
  const page = await client.fetch(plan.first);
  const onward = ({ edges }: Page) => (tail ? edges.prev : edges.next);
  let lines = kept(page, plan.keep);
  let cursor = onward(page);
  for (let i = 0; i < READ_ON_PAGES && lines.length < INLINE_LINES && cursor !== null; i += 1) {
    // oxlint-disable-next-line no-await-in-loop -- each page starts at the last one's edge
    const next = await client.fetch({ kind: 'Continue', cursor, seen: null, limit: INLINE_LINES });
    // A page can repeat a line the last one sent; its cursor names it.
    const held = new Set(lines.map((line) => line.cursor));
    const fresh = next.lines.filter((line) => !held.has(line.cursor));
    lines = tail ? [...fresh, ...lines] : [...lines, ...fresh];
    cursor = onward(next);
  }
  return {
    lines: tail ? lines.slice(-INLINE_LINES) : lines.slice(0, INLINE_LINES),
    more: lines.length < INLINE_LINES && cursor !== null,
    after: resumeOf(page),
  };
}

/**
 * The window with a followed line in it, the newest eight kept in time order, since a follow
 * sends each stream's lines as they come. A line whose cursor it holds is a repeat.
 */
export function appendLine(lines: LogLine[], line: LogLine): LogLine[] {
  if (lines.some((held) => held.cursor === line.cursor)) return lines;
  return [...lines, line].toSorted((a, b) => compareTimestamps(a.timestamp, b.timestamp)).slice(-INLINE_LINES);
}

export type InlineState = {
  /** Oldest first, at most `INLINE_LINES`. */
  lines: LogLine[];
  /** Whether the read stopped at its cap with more lines that way. */
  more: boolean;
  /** Until the first read lands. */
  loading: boolean;
  /** Whether a read failed: `errorReportExchange` has reported it. */
  failed: boolean;
  /** Whether the follow ended or failed, leaving the lines it held. */
  stopped: boolean;
};

/** A window whose first read has not landed. */
export const INLINE_LOADING: InlineState = { lines: [], more: false, loading: true, failed: false, stopped: false };

/**
 * One card's window, in `useSyncExternalStore`'s shape. `start` reads once; a live window
 * follows until `freeze`, `close`, or its follow ending.
 */
export class InlineLines {
  private state = INLINE_LOADING;

  private readonly listeners = new Set<() => void>();

  /** Counts the starts, so a read a later start or the close superseded changes nothing. */
  private starts = 0;

  /** Following, or about to once the first read lands. */
  private live = false;

  private stopFollow: (() => void) | null = null;

  constructor(
    private readonly client: Client,
    private readonly action: LogsViewAction,
    private until: string | null,
  ) {}

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getSnapshot = (): InlineState => this.state;

  start(): void {
    const plan = inlinePlan(this.action, this.until);
    this.live = plan.follow;
    this.starts += 1;
    const { starts } = this;
    readInline(this.client, plan).then(
      ({ lines, more, after }) => {
        if (starts !== this.starts) return;
        this.set({ ...INLINE_LOADING, loading: false, lines, more });
        if (plan.follow && after !== null) this.follow(after);
      },
      () => {
        if (starts === this.starts) this.set({ ...INLINE_LOADING, loading: false, failed: true });
      },
    );
  }

  /**
   * Stops a live window at `until`, the next view's moment, and reads the frozen page a reload
   * reads. The follow may be behind, still owing lines before the moment, or ahead, holding
   * lines at or past it; meanwhile the window keeps those before it.
   */
  freeze(until: string): void {
    if (!this.live) return;
    this.endFollow();
    this.until = until;
    const lines = this.state.lines.filter((line) => compareTimestamps(line.timestamp, until) < 0);
    this.set({ ...this.state, lines });
    this.start();
  }

  close(): void {
    this.starts += 1;
    this.endFollow();
  }

  private follow(after: Cursor): void {
    const onFrame = (frame: TailFrame) => {
      if (frame.kind === 'Line') {
        const lines = appendLine(this.state.lines, frame.line);
        // A full window has no room for what a short read may have missed.
        this.set({ ...this.state, lines, more: this.state.more && lines.length < INLINE_LINES });
      } else if (frame.kind === 'Ended') {
        this.stop();
      }
    };
    try {
      // From the page's own cursor, which already holds where each stream stopped.
      this.stopFollow = this.client.tail(after, [], onFrame, () => this.stop());
    } catch {
      this.stop();
    }
  }

  // The follow ended or failed: the window keeps what it holds.
  private stop(): void {
    this.endFollow();
    this.set({ ...this.state, stopped: true });
  }

  private endFollow(): void {
    this.live = false;
    this.stopFollow?.();
    this.stopFollow = null;
  }

  private set(state: InlineState): void {
    this.state = state;
    this.listeners.forEach((listener) => listener());
  }
}

/**
 * A card's window over `client`. It reads once, as it mounted; a later `until` freezes a live
 * window there.
 */
export function useInlineLogLines(client: Client, action: LogsViewAction, until: string | null): InlineState {
  const [lines] = useState(() => new InlineLines(client, action, until));
  useEffect(() => {
    lines.start();
    return () => lines.close();
  }, [lines]);
  useEffect(() => {
    if (until !== null) lines.freeze(until);
  }, [lines, until]);
  return useSyncExternalStore(lines.subscribe, lines.getSnapshot);
}
