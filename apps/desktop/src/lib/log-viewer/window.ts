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

import { Lines, compareTimestamps, containerKey } from './lines';
import type { Anchor, Cursor, FetchQuery, TailFrame, LogLine, Notice, Page } from './types';

// A window of a log, with no React in it. It holds the lines, both edges, the pin, the pause,
// where the after edge resumes and the newest line of each container; it takes every event —
// a landing, a page, a follow's frames, the user's scroll, a pause — and answers what the
// scroller must do next. The adapter performs the answers and never decides anything itself.

export type Edge = 'before' | 'after';

export type EdgeState = {
  /** Whether there are more lines that way. */
  more: boolean;
  /** Whether the last page that way held no line at all but had a page on: a budget spent, read on by hand. */
  empty: boolean;
  /** Whether a page that way is in flight. Only an edge with more has one. */
  loading: boolean;
};

/** What the last page that way said; `loading` comes from the tickets. */
type PageSaid = Omit<EdgeState, 'loading'>;

export type Effect =
  | { kind: 'none' }
  /** Keep the first visible row where it is on screen across this change. */
  | { kind: 'hold' }
  | { kind: 'toEnd' }
  | { kind: 'toKey'; key: number };

export type CoreState = {
  loading: boolean;
  /** The landing failed. The window holds nothing and pages nothing; `jumpTo` lands again. */
  failed: boolean;
  /** Whether the viewport keeps the end in view. The user's hand changes it. */
  pinnedToEnd: boolean;
  /** Whether the follow is open: landed, pinned, not paused, not ended. */
  following: boolean;
  /** Why the follow ended, in the client's words; null while it has not. A landing clears it. */
  ended: string | null;
  /** Whether the newest line is on screen: following, with the last line in the visible range. */
  atEnd: boolean;
  /** The earliest and latest timestamps among the visible lines; null before any is on screen. */
  range: { from: string; to: string } | null;
  /** How many lines are on screen. */
  visible: number;
  /** How many lines the window holds. With `firstKey` it moves whenever the lines held change, so a render reading the snapshot sees every change. */
  length: number;
  /** The key of the first line held, or the key the next line appended gets. A prepend lowers it and a drop at the before edge raises it. */
  firstKey: number;
  /** The edges, as `edges()` answers them. */
  before: EdgeState;
  after: EdgeState;
  /** What this landing's pages and follow could not do, once per code and message, first seen first. */
  notices: Notice[];
};

export type LogWindowOptions = {
  /** Whether a `Tail` landing keeps the end in view. */
  pinToEnd: boolean;
  /** The landing's limit. */
  firstPageSize: number;
  /** Every later page's limit. */
  pageSize: number;
  /** The lines the window settles at: past it, a page or a batch ends with a drop at one edge. */
  maxLines: number;
  /** The core's paging threshold. A drop keeps this many lines and one more past the visible range, so it never pages the edge it dropped. */
  loadMoreThreshold: number;
};

const NO_MORE: EdgeState = { more: false, empty: false, loading: false };

export const CORE_INITIAL_STATE: CoreState = {
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
  before: NO_MORE,
  after: NO_MORE,
  notices: [],
};

const NONE: Effect = { kind: 'none' };

// The page on that way. After, `next` alone: a poll of the live end through `follow` spends no
// budget and pages on to nothing.
const onward = (page: Page, edge: Edge): Cursor | null => (edge === 'before' ? page.edges.prev : page.edges.next);

// An edge's state off a page: empty when the page held no line and had a page on that way.
function edgeOf(page: Page, edge: Edge): PageSaid {
  const more = onward(page, edge) !== null;
  return { more, empty: more && page.lines.length === 0 };
}

/** Where a page's after edge goes on: its `next`, else its `follow`, the live end. */
export const resumeOf = (page: Page): Cursor | null => page.edges.next ?? page.edges.follow;

export class LogWindow {
  private readonly lines = new Lines();

  private loading = true;

  private isLanded = false;

  private failed = false;

  private pinned = false;

  private paused = false;

  private ended: string | null = null;

  /** The server's word on each edge, off the last page that way. `edges()` derives the after side's `more`, so only its `empty` is read. */
  private edge: Record<Edge, PageSaid> = { before: NO_MORE, after: NO_MORE };

  /** Where the before edge continues. */
  private beforeCursor: Cursor | null = null;

  /** Where the after edge continues, and the follow opens: the last page's `resumeOf`. */
  private resume: Cursor | null = null;

  /** Each container's newest line held, by its cursor: what a page on from `resume` reads past. */
  private newest = new Map<string, Cursor>();

  /**
   * A stamp every line dropped at an edge lies on the window's side of, null with none dropped
   * since that edge last paged: the newest dropped before, the oldest dropped after. The next page
   * that way reads from it. Set only while that edge's cursor is null.
   */
  private seam: Record<Edge, string | null> = { before: null, after: null };

  /** The user's last scroll reached the bottom of a window that is behind; the next page after that finds no more pins. */
  private wantsPin = false;

  /** Whether the last page that way held lines, all of them held already, and said more. */
  private repeats: Record<Edge, boolean> = { before: false, after: false };

  /** The page in flight toward each edge, by a number the window hands out. */
  private ticket: Record<Edge, number | null> = { before: null, after: null };

  private nextTicket = 0;

  /** The keys of the first and last line on screen, as the adapter last told it: keys stay put across a prepend and a drop. */
  private visibleKeys: { start: number; end: number } | null = null;

  private notices: Notice[] = [];

  private snapshot: CoreState = CORE_INITIAL_STATE;

  private readonly listeners = new Set<() => void>();

  constructor(private readonly options: LogWindowOptions) {}

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = (): CoreState => this.snapshot;

  lineAt(index: number): LogLine {
    return this.lines.at(index);
  }

  keyAt(index: number): number {
    return this.lines.keyAt(index);
  }

  indexOfKey(key: number): number {
    return this.lines.indexOfKey(key);
  }

  following(): boolean {
    return this.isLanded && this.pinned && !this.paused && this.ended === null;
  }

  // A window that is not following is behind the live end whatever its last page said, and a
  // paused one asks for nothing, since Pause stops lines arriving. An edge loads only while it
  // has more, so it never says both that nothing is that way and that a page is coming.
  edges(): Record<Edge, EdgeState> {
    const after = this.following() || this.paused ? NO_MORE : { more: this.isLanded, empty: this.edge.after.empty };
    const { before } = this.edge;
    return {
      before: { ...before, loading: before.more && this.ticket.before !== null },
      after: { ...after, loading: after.more && this.ticket.after !== null },
    };
  }

  // `seen` goes on every page after, followed or not: the server keeps the newer of a container's
  // line and the cursor's own, so one it already passed is harmless. An edge with a seam reads
  // from it, each container from its line held nearest that edge.
  query(edge: Edge): FetchQuery | null {
    if (!this.edges()[edge].more) return null;
    const { pageSize: limit } = this.options;
    const seam = this.seam[edge];
    if (edge === 'before') {
      if (seam !== null) return { kind: 'Until', at: seam, seen: [...this.nearest('before').values()], limit };
      return this.beforeCursor === null ? null : { kind: 'Continue', cursor: this.beforeCursor, seen: null, limit };
    }
    if (seam !== null) return { kind: 'Since', at: seam, seen: this.seen(), limit };
    return this.resume === null ? null : { kind: 'Continue', cursor: this.resume, seen: this.seen(), limit };
  }

  /** Only called while following, when `resume` is never null. */
  followFrom(): { cursor: Cursor; seen: Cursor[] } {
    return { cursor: this.resume as Cursor, seen: this.seen() };
  }

  landing(anchor: Anchor): FetchQuery {
    const limit = this.options.firstPageSize;
    if (anchor.kind === 'At') return { kind: 'At', at: anchor.at as string, limit };
    return { kind: anchor.kind === 'Head' ? 'First' : 'Last', limit };
  }

  landed(page: Page, anchor: Anchor): Effect {
    const count = page.lines.length;
    let zeroAt = 0;
    if (anchor.kind === 'Tail') zeroAt = count - 1;
    else if (anchor.kind === 'At') zeroAt = page.anchor ?? 0;
    this.lines.reset(page.lines, zeroAt);
    this.edge = { before: edgeOf(page, 'before'), after: edgeOf(page, 'after') };
    this.beforeCursor = page.edges.prev;
    this.resume = resumeOf(page);
    this.seam = { before: null, after: null };
    this.newest = new Map();
    this.noteNewest(page.lines);
    this.notices = [];
    this.joinNotices(page.notices);
    this.repeats = { before: false, after: false };
    this.ticket = { before: null, after: null };
    this.isLanded = true;
    this.ended = null;
    this.wantsPin = false;
    // A Tail landing shows the end, so it follows from there; the others show their anchor.
    this.pinned = this.options.pinToEnd && anchor.kind === 'Tail';
    this.emit();
    if (anchor.kind === 'Tail') return { kind: 'toEnd' };
    if (anchor.kind === 'At' && count > 0) return { kind: 'toKey', key: 0 };
    return NONE;
  }

  landingFailed(): void {
    this.failed = true;
    this.pinned = false;
    this.emit();
  }

  startPage(edge: Edge): number | null {
    if (!this.isLanded || this.ticket[edge] !== null || this.query(edge) === null) return null;
    this.nextTicket += 1;
    this.ticket[edge] = this.nextTicket;
    this.emit();
    return this.nextTicket;
  }

  paged(ticket: number, page: Page): Effect {
    const edge = this.edgeOfTicket(ticket);
    if (edge === null) return NONE;
    this.ticket[edge] = null;
    // A drop that way clears its ticket, so this page read from the seam when one was set.
    this.seam[edge] = null;
    // Only the edge paged toward reads the page's edges; the other side is ignored.
    this.edge[edge] = edgeOf(page, edge);
    const added = edge === 'before' ? this.lines.prepend(page.lines) : this.lines.append(page.lines);
    this.repeats[edge] = page.lines.length > 0 && added === 0 && onward(page, edge) !== null;
    this.joinNotices(page.notices);
    let effect: Effect = NONE;
    if (edge === 'before') {
      this.beforeCursor = page.edges.prev;
      // Keys are stable across a prepend, so the adapter holds the first visible row by its key.
      effect = { kind: 'hold' };
      // While following the reader is at the end, so the lines just read go again; otherwise
      // the drop takes the edge the page did not add to.
      this.drop(this.following() ? 'before' : 'after');
    } else {
      this.noteNewest(page.lines);
      this.resume = resumeOf(page);
      if (this.wantsPin && page.edges.next === null) {
        // The newest lines: a follow would have brought them the same way.
        this.pinned = true;
        this.wantsPin = false;
        effect = { kind: 'toEnd' };
      } else if (this.wantsPin && added > 0) {
        // Rows to read before the bottom; reaching it again asks again.
        this.wantsPin = false;
      }
      if (this.drop('before') > 0 && effect.kind === 'none') effect = { kind: 'hold' };
    }
    this.emit();
    return effect;
  }

  /** A failure waits for the user's next scroll. The adapter reports the error. */
  pageFailed(ticket: number): void {
    const edge = this.edgeOfTicket(ticket);
    if (edge === null) return;
    this.ticket[edge] = null;
    this.repeats[edge] = false;
    this.emit();
  }

  /** The adapter pages that way again at once: a page of repeats moved the edge but brought no row to scroll to. */
  readOn(edge: Edge): boolean {
    return this.repeats[edge] && this.edges()[edge].more;
  }

  /** A page after asked before the follow opened is dropped when it lands. */
  followOpened(): void {
    this.ticket.after = null;
  }

  /**
   * A batch the adapter flushes after the follow closed — the user scrolled up or paused before
   * the unsubscribe ran — must neither add a line behind the reader nor pull the scroller to
   * the end; the page after that comes next reads those lines again from `resume`.
   */
  followed(frames: readonly TailFrame[]): Effect {
    if (!this.following()) return NONE;
    // An `Ended` is the last frame read; what follows it is ignored.
    const ended = frames.findIndex((frame) => frame.kind === 'Ended');
    const taken = ended === -1 ? frames : frames.slice(0, ended + 1);
    const lines: LogLine[] = [];
    taken.forEach((frame) => {
      if (frame.kind === 'Line') lines.push(frame.line);
      else if (frame.kind === 'Notice') this.joinNotices([frame.notice]);
      else this.ended = frame.reason;
    });
    this.lines.append(lines);
    this.noteNewest(lines);
    this.drop('before');
    this.emit();
    return { kind: 'toEnd' };
  }

  /** The client refused to open the follow, so the window is behind until a page pins it again. The adapter reports the error. */
  followFailed(): void {
    this.pinned = false;
    this.wantsPin = false;
    this.emit();
  }

  scrolledUp(): void {
    this.wantsPin = false;
    this.pinned = false;
    this.emit();
  }

  /**
   * The user's scroll reached the bottom of a window that is behind. On true the adapter pages
   * after at once, since a scroller already at the bottom moves no range to page from; the page
   * pins when it finds no more.
   */
  reachedBottom(): boolean {
    if (this.following() || this.paused) return false;
    this.wantsPin = true;
    return true;
  }

  setPaused(paused: boolean): void {
    this.paused = paused;
    if (paused) {
      this.ticket.after = null;
      this.wantsPin = false;
    }
    this.emit();
  }

  setVisibleRange(start: number, end: number): void {
    const keys = { start: this.lines.keyAt(start), end: this.lines.keyAt(end) };
    if (this.visibleKeys?.start === keys.start && this.visibleKeys.end === keys.end) return;
    this.visibleKeys = keys;
    this.emit();
  }

  /** The adapter calls it a frame after the landing settled, landed or failed. */
  loadingEnded(): void {
    this.loading = false;
    this.emit();
  }

  /**
   * Brings the window back toward `maxLines` by dropping lines at `edge`, never the visible range
   * or the margin past it on that side, which keeps the dropped edge out of the core's paging
   * threshold. While following the range is the end, where the core keeps the view. Answers how
   * many it dropped.
   */
  private drop(edge: Edge): number {
    const count = Math.min(this.lines.length - this.options.maxLines, this.room(edge));
    if (count <= 0) return 0;
    const dropped = edge === 'before' ? this.lines.dropFirst(count) : this.lines.dropLast(count);
    this.foldSeam(edge, dropped);
    // A page in flight that way would land beside the hole the drop opened.
    this.ticket[edge] = null;
    this.repeats[edge] = false;
    if (edge === 'before') {
      this.beforeCursor = null;
      this.edge.before = { more: true, empty: false };
    } else {
      // The newest lines are gone, so the window is behind and pages on from the seam.
      this.resume = null;
      this.edge.after = { more: false, empty: false };
      this.pinned = false;
      this.wantsPin = false;
      this.newest = this.nearest('after');
    }
    return count;
  }

  /**
   * The lines a drop at `edge` may take: everything beyond the visible range and its margin, or
   * everything with no range yet. A range whose lines a drop took since the adapter last reported
   * it says nothing about the screen, so nothing is taken until the next report.
   */
  private room(edge: Edge): number {
    const range = this.following() ? this.endRange() : this.visibleRange();
    if (range === null) return this.lines.length;
    if (range.start < 0 || range.end >= this.lines.length) return 0;
    const margin = this.options.loadMoreThreshold + 1;
    const room = edge === 'before' ? range.start - margin : this.lines.length - 1 - range.end - margin;
    return Math.max(room, 0);
  }

  // Lines are held in arrival order, so a later drop's farthest stamp may lie short of an
  // earlier one's: the seam keeps the farther, the newest before and the oldest after.
  private foldSeam(edge: Edge, dropped: readonly LogLine[]): void {
    const sign = edge === 'before' ? 1 : -1;
    dropped.forEach(({ timestamp }) => {
      const seam = this.seam[edge];
      if (seam === null || sign * compareTimestamps(timestamp, seam) > 0) this.seam[edge] = timestamp;
    });
  }

  /** The first and last index on screen, null before the adapter told any. */
  private visibleRange(): { start: number; end: number } | null {
    if (this.visibleKeys === null) return null;
    return { start: this.lines.indexOfKey(this.visibleKeys.start), end: this.lines.indexOfKey(this.visibleKeys.end) };
  }

  /** As many lines as are on screen, at the end. */
  private endRange(): { start: number; end: number } | null {
    if (this.visibleKeys === null) return null;
    const last = this.lines.length - 1;
    return { start: last - (this.visibleKeys.end - this.visibleKeys.start), end: last };
  }

  private edgeOfTicket(ticket: number): Edge | null {
    if (this.ticket.before === ticket) return 'before';
    if (this.ticket.after === ticket) return 'after';
    return null;
  }

  // A follow sends a notice once per container generation and a page may repeat one, so a
  // notice is held once per code and message.
  private joinNotices(notices: readonly Notice[]): void {
    const held = new Set(this.notices.map(noticeKey));
    const fresh = notices.filter((notice) => {
      const key = noticeKey(notice);
      if (held.has(key)) return false;
      held.add(key);
      return true;
    });
    if (fresh.length > 0) this.notices = [...this.notices, ...fresh];
  }

  // Lines on the after side come in each container's log order, so the last of one is its newest.
  private noteNewest(lines: readonly LogLine[]): void {
    lines.forEach((line) => this.newest.set(containerKey(line.stream), line.cursor));
  }

  private seen(): Cursor[] {
    return [...this.newest.values()];
  }

  // Each container's line held nearest `edge`, by its cursor. A container's held lines are one
  // run of its log, so before it is the container's oldest line and after its newest.
  private nearest(edge: Edge): Map<string, Cursor> {
    const nearest = new Map<string, Cursor>();
    this.lines.forEach((line) => {
      const key = containerKey(line.stream);
      if (edge === 'after' || !nearest.has(key)) nearest.set(key, line.cursor);
    });
    return nearest;
  }

  // A follow's lines keep their arrival order, so the first and last visible need not be the bounds.
  private onScreen(): Pick<CoreState, 'visible' | 'range' | 'atEnd'> {
    const last = this.lines.length - 1;
    const visibleRange = this.visibleRange();
    if (visibleRange === null) return { visible: 0, range: null, atEnd: false };
    const start = Math.max(visibleRange.start, 0);
    const end = Math.min(visibleRange.end, last);
    if (end < start) return { visible: 0, range: null, atEnd: false };
    let from = this.lines.at(start).timestamp;
    let to = from;
    for (let i = start + 1; i <= end; i += 1) {
      const { timestamp } = this.lines.at(i);
      if (compareTimestamps(timestamp, from) < 0) from = timestamp;
      if (compareTimestamps(timestamp, to) > 0) to = timestamp;
    }
    return { visible: end - start + 1, range: { from, to }, atEnd: this.following() && visibleRange.end === last };
  }

  // Publishes a new snapshot when a field changed. An unchanged object keeps its identity, so
  // a render reading one does not loop.
  private emit(): void {
    const prev = this.snapshot;
    const { before, after } = this.edges();
    const { visible, range, atEnd } = this.onScreen();
    const next: CoreState = {
      loading: this.loading,
      failed: this.failed,
      pinnedToEnd: this.pinned,
      following: this.following(),
      ended: this.ended,
      atEnd,
      range: same(prev.range, range) ? prev.range : range,
      visible,
      length: this.lines.length,
      firstKey: this.lines.keyAt(0),
      before: same(prev.before, before) ? prev.before : before,
      after: same(prev.after, after) ? prev.after : after,
      notices: this.notices,
    };
    if (same(prev, next)) return;
    this.snapshot = next;
    this.listeners.forEach((listener) => listener());
  }
}

const noticeKey = ({ code, message }: Notice) => JSON.stringify([code, message]);

// Whether two objects hold the same values field for field.
function same(a: object | null, b: object | null): boolean {
  if (a === null || b === null) return a === b;
  const fields = Object.entries(a);
  return (
    fields.length === Object.keys(b).length &&
    fields.every(([key, value]) => Object.is(value, b[key as keyof typeof b]))
  );
}
