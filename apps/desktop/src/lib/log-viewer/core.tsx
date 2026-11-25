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

import { type Virtualizer, useVirtualizer } from '@tanstack/react-virtual';
import {
  type ReactNode,
  type Ref,
  type RefObject,
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react';

import type { Anchor, Client, TailFrame, LogLine } from './types';
import { CORE_INITIAL_STATE, type CoreState, type Edge, type EdgeState, type Effect, LogWindow } from './window';

// The window behind a scroller. Every rule about lines, edges and the pin is the window's; this
// file tells it what the DOM and the client did, and performs what it answers.

/** The numbers the core runs on. Each has a default. */
type Settings = {
  overscan: number;
  firstPageSize: number;
  pageSize: number;
  /** Pages toward an edge once at most this many rows lie between the visible range and it. */
  loadMoreThreshold: number;
  /** The lines the window settles at; past it a page or a batch drops lines at one edge. */
  maxLines: number;
  /** How far from the bottom, in pixels, still counts as reaching it. */
  pinTolerance: number;
  /** The height of an edge row while that edge has more; the core lays rows out below it. */
  edgeRowHeight: number;
};

export type LogViewerCoreProps = Partial<Settings> & {
  ref?: Ref<CoreHandle>;
  client: Client;
  /** Where the core lands on mount. Read once: a new landing is `jumpTo`, `retry`, or a new mount. A new `client` lands again as the last landing did. */
  anchor: Anchor;
  /** Whether the mount's `Tail` landing starts pinned to the end. Read once, like `anchor`. */
  pinToEnd: boolean;
  /** The user's Pause. While set, no lines arrive, across landings. */
  paused?: boolean;
  /** Where the core publishes its state. The core makes its own when none is given. */
  store?: CoreStore;
  /** Each landing's scroller, for a caller that reads its size or its sideways scroll. A landing mounts a new one. */
  scrollerRef?: (el: HTMLDivElement | null) => void;
  onError?: (error: unknown) => void;
  estimateRowHeight: (line: LogLine) => number;
  measureElement?: (
    element: Element,
    entry: ResizeObserverEntry | undefined,
    instance: Virtualizer<HTMLDivElement, Element>,
  ) => number;
  className?: string;
  children: (rows: CoreRows) => ReactNode;
};

export type CoreHandle = {
  /** Lands again at `anchor`. A jump to `Tail` pins to the end; any other unpins. */
  jumpTo: (anchor: Anchor) => void;
  /** Lands again as the mount did: at the `anchor` prop, pinned as `pinToEnd` says. */
  retry: () => void;
  /** Re-measures every row, keeping the reader's place, or the end while following. */
  measure: () => void;
};

/** The core's published state, in `useSyncExternalStore`'s shape. It outlives landings. */
export type CoreStore = {
  subscribe: (listener: () => void) => () => void;
  getSnapshot: () => CoreState;
  /** The core's alone. */
  set: (state: CoreState) => void;
};

/** `start` is from the top of the sizer, the before edge row included. */
export type CoreRow = { key: number; index: number; size: number; start: number; line: LogLine };

export type CoreRows = {
  loading: boolean;
  failed: boolean;
  before: EdgeState;
  after: EdgeState;
  edgeRowHeight: number;
  /** The rows and the edge rows together, for the sizer. */
  totalSize: number;
  rows: CoreRow[];
  count: number;
  /** The key of the first line held; the keys held run from it for `count`. */
  firstKey: number;
  lineOfKey: (key: number) => LogLine | undefined;
  keyAt: (index: number) => number | undefined;
  /** -1 for a key not held. */
  indexOfKey: (key: number) => number;
  measureElement: (node: Element | null) => void;
  /** Scrolls the row at `index` into view by the least movement; a row fully in view stays put. */
  scrollIntoView: (index: number) => void;
  /** Reads the next page toward `edge` now, for an edge row's button: with no row drawn after an empty page, there is no scroll to read on at. */
  loadMore: (edge: Edge) => void;
};

/**
 * The fields of a store's state a component draws from, re-rendering only when one of them
 * moves: the visible range moves the state on every row scrolled past, and a component that
 * draws nothing from it should not render for it. `keys` must be stable.
 */
export function useCoreFields<K extends keyof CoreState>(
  store: Pick<CoreStore, 'subscribe' | 'getSnapshot'>,
  keys: readonly K[],
): Pick<CoreState, K> {
  const picked = useRef<Pick<CoreState, K> | null>(null);
  const getPicked = useCallback(() => {
    const state = store.getSnapshot();
    const last = picked.current;
    if (last !== null && keys.every((key) => Object.is(last[key], state[key]))) return last;
    const next = Object.fromEntries(keys.map((key) => [key, state[key]])) as Pick<CoreState, K>;
    picked.current = next;
    return next;
  }, [store, keys]);
  return useSyncExternalStore(store.subscribe, getPicked);
}

const LANDING_FIELDS = ['length', 'firstKey', 'loading', 'following', 'failed', 'before', 'after'] as const;

export function createCoreStore(): CoreStore {
  let state = CORE_INITIAL_STATE;
  const listeners = new Set<() => void>();
  return {
    subscribe: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    getSnapshot: () => state,
    set: (next) => {
      if (next === state) return;
      state = next;
      listeners.forEach((listener) => listener());
    },
  };
}

/** How long a frame may take to come before a timer stands in for it: a minimized window gets none. */
const HIDDEN_FLUSH_MS = 250;

/** Calls `fn` on the next animation frame, or after `HIDDEN_FLUSH_MS` when none comes. Answers a cancel. */
function onNextFrame(fn: () => void): () => void {
  let done = false;
  const run = () => {
    if (done) return;
    done = true;
    cancelAnimationFrame(frame);
    clearTimeout(timer);
    fn();
  };
  const frame = requestAnimationFrame(run);
  const timer = setTimeout(run, HIDDEN_FLUSH_MS);
  return () => {
    done = true;
    cancelAnimationFrame(frame);
    clearTimeout(timer);
  };
}

export function LogViewerCore({
  ref,
  client,
  anchor,
  pinToEnd,
  paused = false,
  store,
  scrollerRef,
  onError,
  estimateRowHeight,
  measureElement,
  overscan = 3,
  firstPageSize = 150,
  pageSize = 100,
  loadMoreThreshold = 50,
  maxLines = 5000,
  pinTolerance = 10,
  edgeRowHeight = 0,
  className,
  children,
}: LogViewerCoreProps) {
  const [ownStore] = useState(createCoreStore);
  const [mounted] = useState(() => ({ anchor, pinToEnd }));
  const [landing, setLanding] = useState({ id: 0, ...mounted });
  const landingRef = useRef<LandingHandle>(null);

  // A new client lands again as the last landing did. The id moves during render so the old
  // landing never renders with a client whose cursors it does not hold.
  const [lastClient, setLastClient] = useState(client);
  if (client !== lastClient) {
    setLastClient(client);
    setLanding((last) => ({ ...last, id: last.id + 1 }));
  }

  useImperativeHandle(
    ref,
    () => ({
      jumpTo: (next) => setLanding((last) => ({ id: last.id + 1, anchor: next, pinToEnd: next.kind === 'Tail' })),
      retry: () => setLanding((last) => ({ id: last.id + 1, ...mounted })),
      measure: () => landingRef.current?.measure(),
    }),
    [mounted],
  );

  return (
    <Landing
      key={landing.id}
      ref={landingRef}
      client={client}
      anchor={landing.anchor}
      pinToEnd={landing.pinToEnd}
      paused={paused}
      store={store ?? ownStore}
      scrollerRef={scrollerRef}
      onError={onError}
      estimateRowHeight={estimateRowHeight}
      measureElement={measureElement}
      overscan={overscan}
      firstPageSize={firstPageSize}
      pageSize={pageSize}
      loadMoreThreshold={loadMoreThreshold}
      maxLines={maxLines}
      pinTolerance={pinTolerance}
      edgeRowHeight={edgeRowHeight}
      className={className}
    >
      {children}
    </Landing>
  );
}

type LandingHandle = Pick<CoreHandle, 'measure'>;

type LandingProps = Settings & {
  ref: Ref<LandingHandle>;
  client: Client;
  anchor: Anchor;
  pinToEnd: boolean;
  paused: boolean;
  store: CoreStore;
  scrollerRef?: (el: HTMLDivElement | null) => void;
  onError?: (error: unknown) => void;
  estimateRowHeight: (line: LogLine) => number;
  measureElement?: LogViewerCoreProps['measureElement'];
  className?: string;
  children: (rows: CoreRows) => ReactNode;
};

// One landing: a window, a scroller and a virtualizer, made fresh each time `LogViewerCore`
// moves the key. Below, `logWindow` is the landing's window; `window` is the browser's.
function Landing({
  ref,
  client,
  anchor,
  pinToEnd,
  paused,
  store,
  scrollerRef,
  onError,
  estimateRowHeight,
  measureElement,
  overscan,
  firstPageSize,
  pageSize,
  loadMoreThreshold,
  maxLines,
  pinTolerance,
  edgeRowHeight,
  className,
  children,
}: LandingProps) {
  const [logWindow] = useState(() => new LogWindow({ pinToEnd, firstPageSize, pageSize, maxLines, loadMoreThreshold }));
  const state = useCoreFields(logWindow, LANDING_FIELDS);
  const scrollRef = useRef<HTMLDivElement>(null);
  const setScroller = useCallback(
    (el: HTMLDivElement | null) => {
      scrollRef.current = el;
      scrollerRef?.(el);
    },
    [scrollerRef],
  );

  // Published from the first layout effect, before paint, so no frame shows the last landing's
  // state under this one.
  useLayoutEffect(() => {
    store.set(logWindow.getSnapshot());
    return logWindow.subscribe(() => store.set(logWindow.getSnapshot()));
  }, [logWindow, store]);

  // Keys are consecutive, so the first names them all. Read from the snapshot with the length, so
  // the virtualizer sees the keys and the count of one commit together.
  const { length, firstKey, loading, following } = state;
  const getItemKey = useCallback((index: number) => firstKey + index, [firstKey]);
  const estimateSize = useCallback(
    (index: number) => estimateRowHeight(logWindow.lineAt(index)),
    [estimateRowHeight, logWindow],
  );
  const beforeRow = state.before.more ? edgeRowHeight : 0;
  const afterRow = state.after.more ? edgeRowHeight : 0;
  // oxlint-disable-next-line react/incompatible-library -- the virtualizer mutates in place; this component reads it in render
  const virtualizer = useVirtualizer({
    count: length,
    getScrollElement: () => scrollRef.current,
    getItemKey,
    estimateSize,
    scrollMargin: beforeRow,
    overscan,
    useFlushSync: false,
    // The end of a scroll from the browser, not a debounce reset on every scroll event.
    useScrollendEvent: true,
    ...(measureElement ? { measureElement } : {}),
  });

  const totalSize = virtualizer.getTotalSize() + beforeRow + afterRow;
  const perform = useAnswers(scrollRef, virtualizer, logWindow, totalSize, following);
  const { loadMore, held } = useClient({ client, logWindow, anchor, paused, following, perform, onError });
  const { measuring, measure } = useMeasure(virtualizer, perform);
  useImperativeHandle(ref, () => ({ measure }), [measure]);
  useIntent(scrollRef, logWindow, pinTolerance, loadMore);

  // The visible range without overscan, handed to the window after every commit; an unchanged
  // range emits nothing, so it settles.
  const range = virtualizer.calculateRange();
  const startIndex = range?.startIndex ?? -1;
  const endIndex = range?.endIndex ?? -1;
  useLayoutEffect(() => {
    if (startIndex >= 0) logWindow.setVisibleRange(startIndex, endIndex);
  });

  // Paging driven by the range waits while the range is not the reader's: until loading has
  // ended, since a landing's first commit draws its rows with the scroller still at the top,
  // and while a re-measure is in flight. The user's reach of the bottom and a page of repeats
  // page through `loadMore` directly, with no hold.
  useEffect(() => {
    if (loading || measuring || startIndex < 0) return;
    if (startIndex <= loadMoreThreshold && !held.current.before) loadMore('before');
    if (endIndex >= length - 1 - loadMoreThreshold && !held.current.after) loadMore('after');
  }, [startIndex, endIndex, length, loading, measuring, loadMoreThreshold, loadMore, held]);

  const rows: CoreRows = {
    loading,
    failed: state.failed,
    before: state.before,
    after: state.after,
    edgeRowHeight,
    totalSize,
    rows: virtualizer.getVirtualItems().map((item) => ({
      key: item.key as number,
      index: item.index,
      size: item.size,
      start: item.start,
      line: logWindow.lineAt(item.index),
    })),
    count: length,
    firstKey,
    lineOfKey: (key) => {
      const index = logWindow.indexOfKey(key);
      return index >= 0 && index < length ? logWindow.lineAt(index) : undefined;
    },
    keyAt: (index) => (index >= 0 && index < length ? logWindow.keyAt(index) : undefined),
    indexOfKey: (key) => {
      const index = logWindow.indexOfKey(key);
      return index >= 0 && index < length ? index : -1;
    },
    measureElement: virtualizer.measureElement,
    scrollIntoView: (index) => virtualizer.scrollToIndex(index, { align: 'auto' }),
    loadMore,
  };

  return (
    <div
      ref={setScroller}
      className={className}
      // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- focusable so the keys reach the scroller, and its intent with them
      tabIndex={0}
      style={{ overflow: 'auto', overflowAnchor: 'none' }}
    >
      {children(rows)}
    </div>
  );
}

type ClientOptions = {
  client: Client;
  logWindow: LogWindow;
  anchor: Anchor;
  paused: boolean;
  following: boolean;
  perform: (effect: Effect) => void;
  onError?: (error: unknown) => void;
};

// Everything the landing asks of the client — the landing itself, each page, and the follow —
// and what it does with the answers. Answers `loadMore`, which pages toward an edge, and
// `held`, the edges paging driven by the range must leave alone this frame.
function useClient({ client, logWindow, anchor, paused, following, perform, onError }: ClientOptions) {
  const report = useRef(onError);
  useEffect(() => {
    report.current = onError;
  });

  // Whether this landing is still mounted, for what the client answers after it is gone.
  const gone = useRef(false);
  // An edge a page just landed on, held from paging driven by the range until the next frame,
  // so the range the page moves does not ask twice.
  const held = useRef<Record<Edge, boolean>>({ before: false, after: false });
  // A page's error is reported once per run of failures: momentum at the bottom asks many times
  // a second, and against a client that keeps failing each ask would be a report.
  const failing = useRef(false);

  const loadMore = useCallback(
    (edge: Edge) => {
      const ask = () => {
        const ticket = logWindow.startPage(edge);
        if (ticket === null) return;
        client.fetch(logWindow.query(edge)!).then(
          (page) => {
            if (gone.current) return;
            failing.current = false;
            held.current[edge] = true;
            perform(logWindow.paged(ticket, page));
            onNextFrame(() => {
              if (gone.current) return;
              held.current[edge] = false;
              // A page of repeats moved the edge and not the range, so nothing else would ask.
              if (logWindow.readOn(edge)) ask();
            });
          },
          (error: unknown) => {
            if (gone.current) return;
            logWindow.pageFailed(ticket);
            if (failing.current) return;
            failing.current = true;
            report.current?.(error);
          },
        );
      };
      ask();
    },
    [client, logWindow, perform],
  );

  // Declared before the landing, so a landing made while paused lands paused.
  useEffect(() => {
    logWindow.setPaused(paused);
  }, [logWindow, paused]);

  // The landing, once: StrictMode runs the effect twice, so a ref says whether it started.
  const started = useRef(false);
  useEffect(() => {
    gone.current = false;
    if (!started.current) {
      started.current = true;
      const settled = () => {
        if (!gone.current) onNextFrame(() => logWindow.loadingEnded());
      };
      client.fetch(logWindow.landing(anchor)).then(
        (page) => {
          if (gone.current) return;
          perform(logWindow.landed(page, anchor));
          settled();
        },
        (error: unknown) => {
          if (gone.current) return;
          logWindow.landingFailed();
          report.current?.(error);
          settled();
        },
      );
    }
    return () => {
      gone.current = true;
    };
  }, [client, logWindow, anchor, perform]);

  // The follow, open exactly while the window follows. Its frames go to the window once per
  // frame; a batch still waiting when it closes is dropped.
  useEffect(() => {
    // StrictMode runs this twice; a refused follow has unpinned the window by the second run.
    if (!following || !logWindow.following()) return undefined;
    logWindow.followOpened();
    let batch: TailFrame[] = [];
    let cancelFlush: (() => void) | null = null;
    const flush = () => {
      const frames = batch;
      batch = [];
      cancelFlush = null;
      perform(logWindow.followed(frames));
    };
    let closed = false;
    const failed = (error: unknown) => {
      logWindow.followFailed();
      report.current?.(error);
    };
    let unsubscribe: () => void;
    try {
      const { cursor, seen } = logWindow.followFrom();
      unsubscribe = client.tail(
        cursor,
        seen,
        (frame) => {
          batch.push(frame);
          cancelFlush ??= onNextFrame(flush);
        },
        // Unpinning closes the follow through this effect's cleanup.
        (error) => {
          if (!closed) failed(error);
        },
      );
    } catch (error) {
      failed(error);
      return undefined;
    }
    return () => {
      closed = true;
      unsubscribe();
      cancelFlush?.();
      batch = [];
    };
  }, [following, client, logWindow, perform]);

  return { loadMore, held };
}

// A re-measure is in flight from the call until a frame after its hold is performed: it resets
// every row to its estimate before the hold puts the reader back, so the range meanwhile is not
// the reader's. A second call while one is in flight does nothing.
function useMeasure(virtualizer: Virtualizer<HTMLDivElement, Element>, perform: (effect: Effect) => void) {
  const [measuring, setMeasuring] = useState(false);
  const inFlight = useRef(false);

  const measure = useCallback(() => {
    if (inFlight.current) return;
    inFlight.current = true;
    setMeasuring(true);
    perform({ kind: 'hold' });
    virtualizer.measure();
  }, [perform, virtualizer]);

  // Runs in the commit that performs the hold, since the two were set together.
  useLayoutEffect(() => {
    if (!measuring) return undefined;
    return onNextFrame(() => {
      inFlight.current = false;
      setMeasuring(false);
    });
  }, [measuring]);

  return { measuring, measure };
}

// An answer the scroller performs after the commit it brings. A hold carries the first visible
// row's key and how far into it the scroll sat, read off the layout still on screen.
type Answer =
  | { kind: 'hold'; at: { key: number; offset: number } | null }
  | { kind: 'toEnd' }
  | { kind: 'toKey'; key: number };

// Keeps each answer in state, so it brings a commit of its own and is never left for an
// unrelated one, and performs it in the layout effect after that commit.
function useAnswers(
  scrollRef: RefObject<HTMLDivElement | null>,
  virtualizer: Virtualizer<HTMLDivElement, Element>,
  logWindow: LogWindow,
  totalSize: number,
  following: boolean,
): (effect: Effect) => void {
  const [answer, setAnswer] = useState<Answer | null>(null);
  const pending = useRef<Answer | null>(null);

  const perform = useCallback(
    (effect: Effect) => {
      if (effect.kind === 'none') return;
      let next: Answer;
      if (effect.kind === 'hold') {
        // Only the first hold before a commit counts: a second is read against lines the
        // screen does not show yet.
        if (pending.current?.kind === 'hold') return;
        next = { kind: 'hold', at: firstVisible(scrollRef.current, virtualizer) };
      } else {
        next = { ...effect };
      }
      pending.current = next;
      setAnswer(next);
    },
    [scrollRef, virtualizer],
  );

  useLayoutEffect(() => {
    const el = scrollRef.current;
    pending.current = null;
    if (answer === null || el === null) return;
    if (answer.kind === 'hold') {
      if (answer.at === null) return;
      // `getTotalSize` brings `measurementsCache` up to date with the rows measured this commit.
      virtualizer.getTotalSize();
      const item = virtualizer.measurementsCache[logWindow.indexOfKey(answer.at.key)];
      if (item) el.scrollTop = item.start + answer.at.offset;
    } else if (answer.kind === 'toEnd') {
      el.scrollTop = el.scrollHeight;
    } else {
      virtualizer.scrollToIndex(logWindow.indexOfKey(answer.key), { align: 'start' });
    }
  }, [answer, scrollRef, virtualizer, logWindow]);

  // While following, a commit that changes the sizer keeps the end in view, whatever the
  // answer, and so does a resize: a scrollbar appearing or the panel resizing must not hide
  // the last row. A commit that leaves the sizer alone reads no layout for it.
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (el !== null && following) el.scrollTop = el.scrollHeight;
  }, [scrollRef, totalSize, following]);
  useEffect(() => {
    const el = scrollRef.current;
    if (el === null) return undefined;
    const observer = new ResizeObserver(() => {
      if (logWindow.following()) el.scrollTop = el.scrollHeight;
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [scrollRef, logWindow]);

  return perform;
}

function firstVisible(el: HTMLDivElement | null, virtualizer: Virtualizer<HTMLDivElement, Element>) {
  if (el === null) return null;
  const item = virtualizer.getVirtualItemForOffset(el.scrollTop);
  return item ? { key: item.key as number, offset: el.scrollTop - item.start } : null;
}

/** How long after an input on the scroller a scroll event still reads as the user's. */
const INTENT_MS = 150;

const UP_KEYS = new Set(['ArrowUp', 'PageUp', 'Home']);
const DOWN_KEYS = new Set(['ArrowDown', 'PageDown', 'End']);

// Tells the user's scrolls from the core's by the input events that precede them, and hands
// the window the two facts it reads: the user scrolled up, or reached the bottom.
function useIntent(
  scrollRef: RefObject<HTMLDivElement | null>,
  logWindow: LogWindow,
  pinTolerance: number,
  loadMore: (edge: Edge) => void,
) {
  useEffect(() => {
    const el = scrollRef.current;
    if (el === null) return undefined;
    let inputAt = -Infinity;
    let pointerDown = false;
    let touching = false;
    let lastTop = el.scrollTop;

    const atBottom = () => el.scrollTop + el.clientHeight >= el.scrollHeight - pinTolerance;
    const reachedBottom = () => {
      // A scroller already at the bottom moves no range to page from.
      if (logWindow.reachedBottom()) loadMore('after');
    };
    const mark = () => {
      inputAt = Date.now();
    };

    const onScroll = () => {
      const top = el.scrollTop;
      const moved = top - lastTop;
      lastTop = top;
      if (!pointerDown && !touching && Date.now() - inputAt > INTENT_MS) return;
      // The bottom is read first: a clamp or a re-measure can move the scroll up during an
      // input, a trackpad's momentum in particular, and leaves it at the bottom.
      if (atBottom()) {
        if (moved > 0) reachedBottom();
      } else if (moved < 0) {
        logWindow.scrolledUp();
      }
    };
    // A wheel up unpins directly, since its scroll may stay within the tolerance. A scroller at
    // its bottom sends no scroll event for a move down, so a move down there reaches the bottom
    // directly. A trackpad sends wheel events faster than frames, so the layout is read once a
    // frame for all of them.
    let wheelFrame: number | null = null;
    const onWheel = (event: WheelEvent) => {
      mark();
      if (event.deltaY < 0) logWindow.scrolledUp();
      if (event.deltaY <= 0 || wheelFrame !== null) return;
      wheelFrame = requestAnimationFrame(() => {
        wheelFrame = null;
        if (atBottom()) reachedBottom();
      });
    };
    const onKeyDown = (event: KeyboardEvent) => {
      const up = UP_KEYS.has(event.key) || (event.key === ' ' && event.shiftKey);
      const down = DOWN_KEYS.has(event.key) || (event.key === ' ' && !event.shiftKey);
      if (!up && !down) return;
      mark();
      // An up key unpins directly: its scroll may be clamped to no move, or stay within the
      // tolerance. The scroll End causes may be clamped too.
      if (up) logWindow.scrolledUp();
      else if (event.key === 'End' || atBottom()) reachedBottom();
    };
    // A pointer down on the lines is the user's hand on them, which the pin yields to at once:
    // a row clicked while pinned would otherwise scroll away as lines arrive.
    const onPointerDown = () => {
      pointerDown = true;
      logWindow.scrolledUp();
    };
    const onPointerEnd = () => {
      pointerDown = false;
    };
    const onTouch = () => {
      touching = true;
    };
    const onTouchEnd = () => {
      touching = false;
    };

    el.addEventListener('scroll', onScroll, { passive: true });
    el.addEventListener('wheel', onWheel, { passive: true });
    el.addEventListener('keydown', onKeyDown);
    el.addEventListener('pointerdown', onPointerDown);
    el.addEventListener('touchstart', onTouch, { passive: true });
    el.addEventListener('touchmove', onTouch, { passive: true });
    el.addEventListener('touchend', onTouchEnd);
    el.addEventListener('touchcancel', onTouchEnd);
    // An engine can drop the pointerup after a scrollbar drag or a release outside the window.
    window.addEventListener('pointerup', onPointerEnd);
    window.addEventListener('pointercancel', onPointerEnd);
    window.addEventListener('blur', onPointerEnd);
    return () => {
      if (wheelFrame !== null) cancelAnimationFrame(wheelFrame);
      el.removeEventListener('scroll', onScroll);
      el.removeEventListener('wheel', onWheel);
      el.removeEventListener('keydown', onKeyDown);
      el.removeEventListener('pointerdown', onPointerDown);
      el.removeEventListener('touchstart', onTouch);
      el.removeEventListener('touchmove', onTouch);
      el.removeEventListener('touchend', onTouchEnd);
      el.removeEventListener('touchcancel', onTouchEnd);
      window.removeEventListener('pointerup', onPointerEnd);
      window.removeEventListener('pointercancel', onPointerEnd);
      window.removeEventListener('blur', onPointerEnd);
    };
  }, [scrollRef, logWindow, pinTolerance, loadMore]);
}
