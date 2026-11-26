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
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { StrictMode, createRef, useLayoutEffect, useSyncExternalStore } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  type CoreHandle,
  type CoreRows,
  type CoreStore,
  LogViewerCore,
  type LogViewerCoreProps,
  createCoreStore,
  useCoreFields,
} from './core';
import type { Anchor, Client, Cursor, FetchQuery, TailFrame, LogLine, Page } from './types';
import { CORE_INITIAL_STATE, type CoreState } from './window';

const HEAD: Anchor = { kind: 'Head', at: null };
const TAIL: Anchor = { kind: 'Tail', at: null };
const AT: Anchor = { kind: 'At', at: '2026-10-15T00:01:00Z' };

// --- The layout stub. jsdom lays nothing out, so the scroller is 200px tall, every row is
// `rowHeight(key)` tall, `scrollHeight` is the sizer the render function draws, and
// `scrollTop` clamps as a browser's and fires `scroll`. ---

const VIEW = 200;
let viewHeight = VIEW;
let rowHeight: (key: number) => number = () => 20;
// A line's own height, by its cursor, for a test whose keys come back on other lines.
let lineHeight: ((cursor: string) => number) | null = null;

const isScroller = (el: Element) => el.classList.contains('scroller');

function sizerHeight(el: Element): number {
  const sizer = el.firstElementChild as HTMLElement | null;
  return sizer ? Number.parseFloat(sizer.style.height) || 0 : 0;
}

function maxTop(el: Element): number {
  return Math.max(0, Math.max(viewHeight, sizerHeight(el)) - viewHeight);
}

const tops = new WeakMap<Element, number>();

// A `ResizeObserver` that delivers on the next frame, as a browser's does, and when told.
const observers = new Set<StubResizeObserver>();

class StubResizeObserver {
  private readonly targets = new Set<Element>();

  constructor(private readonly callback: (entries: ResizeObserverEntry[], observer: unknown) => void) {
    observers.add(this);
  }

  observe(target: Element) {
    this.targets.add(target);
    requestAnimationFrame(() => {
      if (this.targets.has(target)) this.deliver([target]);
    });
  }

  unobserve(target: Element) {
    this.targets.delete(target);
  }

  disconnect() {
    this.targets.clear();
    observers.delete(this);
  }

  fire() {
    this.deliver([...this.targets]);
  }

  private deliver(targets: Element[]) {
    if (targets.length === 0) return;
    this.callback(
      targets.map((target) => ({ target }) as ResizeObserverEntry),
      this,
    );
  }
}

function fireResize() {
  act(() => {
    observers.forEach((observer) => observer.fire());
  });
}

const stubbed: Record<string, PropertyDescriptor | undefined> = {};

function stub(name: string, descriptor: PropertyDescriptor) {
  stubbed[name] = Object.getOwnPropertyDescriptor(HTMLElement.prototype, name);
  Object.defineProperty(HTMLElement.prototype, name, { configurable: true, ...descriptor });
}

function stubLayout() {
  stub('offsetHeight', {
    get(this: HTMLElement) {
      if (isScroller(this)) return viewHeight;
      if (this.dataset.key === undefined) return 0;
      return lineHeight && this.dataset.cursor ? lineHeight(this.dataset.cursor) : rowHeight(Number(this.dataset.key));
    },
  });
  stub('offsetWidth', { get: () => 300 });
  stub('clientWidth', { get: () => 300 });
  stub('clientHeight', {
    get(this: HTMLElement) {
      return isScroller(this) ? viewHeight : 0;
    },
  });
  stub('scrollHeight', {
    get(this: HTMLElement) {
      return isScroller(this) ? Math.max(viewHeight, sizerHeight(this)) : 0;
    },
  });
  stub('scrollTop', {
    get(this: HTMLElement) {
      return tops.get(this) ?? 0;
    },
    set(this: HTMLElement, value: number) {
      const next = Math.min(Math.max(0, Math.round(value)), maxTop(this));
      if (next === (tops.get(this) ?? 0)) return;
      tops.set(this, next);
      this.dispatchEvent(new Event('scroll'));
    },
  });
  stub('scrollTo', {
    value(this: HTMLElement, options: ScrollToOptions) {
      if (options.top !== undefined) this.scrollTop = options.top;
    },
  });
  vi.stubGlobal('ResizeObserver', StubResizeObserver);
}

function unstubLayout() {
  Object.entries(stubbed).forEach(([name, descriptor]) => {
    if (descriptor) Object.defineProperty(HTMLElement.prototype, name, descriptor);
    else delete (HTMLElement.prototype as unknown as Record<string, unknown>)[name];
  });
  observers.clear();
  vi.unstubAllGlobals();
}

// --- Lines and pages. Line `i` is stamped `i` seconds past midnight; its cursor is `c<i>`. ---

function line(i: number): LogLine {
  const second = ((i % 3600) + 3600) % 3600;
  return {
    timestamp: `2026-10-15T00:${String(Math.floor(second / 60)).padStart(2, '0')}:${String(second % 60).padStart(2, '0')}Z`,
    cursor: `c${i}`,
    message: `line ${i}`,
    truncatedBytes: 0,
    stream: { namespace: 'ns', pod: 'p', container: 'app', previous: false },
    place: { node: '', region: '', zone: '', os: '', arch: '' },
    gap: null,
  };
}

const lines = (from: number, to: number) => Array.from({ length: to - from }, (_, k) => line(from + k));

type PageOver = { edges?: Partial<Page['edges']>; anchor?: number | null; notices?: Page['notices'] };

function page(from: number, to: number, over: PageOver = {}): Page {
  return {
    lines: lines(from, to),
    edges: { prev: `before-${from}`, next: `after-${to}`, follow: null, ...over.edges },
    anchor: over.anchor ?? null,
    notices: over.notices ?? [],
  };
}

const START = { prev: null };
const END = { next: null, follow: 'live-end' };

// --- A client the test answers by hand. ---

type Fetch = { query: FetchQuery; resolve: (page: Page) => void; reject: (error: unknown) => void };
type Follow = {
  cursor: Cursor;
  seen: Cursor[];
  onFrame: (frame: TailFrame) => void;
  onFailed: (error: unknown) => void;
  closed: boolean;
};

function scripted() {
  const fetches: Fetch[] = [];
  const follows: Follow[] = [];
  const state = { refuseFollow: false };
  const client: Client = {
    fetch: (query) =>
      new Promise<Page>((resolve, reject) => {
        fetches.push({ query, resolve, reject });
      }),
    tail: (cursor, seen, onFrame, onFailed) => {
      if (state.refuseFollow) {
        state.refuseFollow = false;
        throw new Error('refused');
      }
      const follow: Follow = { cursor, seen, onFrame, onFailed, closed: false };
      follows.push(follow);
      return () => {
        follow.closed = true;
      };
    },
  };
  const open = () => follows.filter((f) => !f.closed);
  return {
    client,
    fetches,
    follows,
    open,
    refuseNextFollow: () => {
      state.refuseFollow = true;
    },
    last: () => fetches.at(-1)!,
  };
}

// --- Mounting. ---

function draw(rows: CoreRows) {
  return (
    <div style={{ height: rows.totalSize, position: 'relative' }}>
      {rows.loading && <span data-testid="loading" />}
      {rows.failed && <span data-testid="failed" />}
      {rows.rows.map((row) => (
        <div
          key={row.key}
          data-index={row.index}
          data-key={row.key}
          data-cursor={row.line.cursor}
          data-start={row.start}
          ref={rows.measureElement}
          style={{ position: 'absolute', top: row.start }}
        >
          {row.line.message}
        </div>
      ))}
      <button type="button" data-testid="more-before" onClick={() => rows.loadMore('before')}>
        more before
      </button>
      <button type="button" data-testid="more-after" onClick={() => rows.loadMore('after')}>
        more after
      </button>
    </div>
  );
}

type MountProps = Partial<Omit<LogViewerCoreProps, 'children' | 'ref'>> & { client: Client };

function mount(props: MountProps) {
  const ref = createRef<CoreHandle>();
  const store = props.store ?? createCoreStore();
  const element = (p: MountProps) => (
    <StrictMode>
      <LogViewerCore
        ref={ref}
        anchor={TAIL}
        pinToEnd
        store={store}
        estimateRowHeight={() => 20}
        className="scroller"
        {...p}
      >
        {draw}
      </LogViewerCore>
    </StrictMode>
  );
  const view = render(element(props));
  const scroller = () => view.container.querySelector('.scroller') as HTMLElement;
  return {
    ref,
    store,
    view,
    scroller,
    state: () => store.getSnapshot(),
    rerender: (next: Partial<MountProps>) => view.rerender(element({ ...props, ...next })),
    keys: () => [...view.container.querySelectorAll<HTMLElement>('[data-key]')].map((el) => Number(el.dataset.key)),
    startOf: (key: number) => Number(view.container.querySelector<HTMLElement>(`[data-key="${key}"]`)?.dataset.start),
  };
}

/** Lets settled promises run and React commit what they caused. */
async function flush() {
  await act(async () => {
    await Promise.resolve();
  });
}

/** One animation frame, in its own act so React commits before the next. */
async function frame(count = 1) {
  for (let i = 0; i < count; i += 1) {
    // oxlint-disable-next-line no-await-in-loop -- each frame is its own act
    await act(async () => {
      await vi.advanceTimersByTimeAsync(16);
    });
  }
}

async function answer(fetch: Fetch, p: Page) {
  await act(async () => {
    fetch.resolve(p);
  });
}

beforeEach(() => {
  vi.useFakeTimers({
    toFake: [
      'setTimeout',
      'clearTimeout',
      'setInterval',
      'clearInterval',
      'requestAnimationFrame',
      'cancelAnimationFrame',
      'Date',
    ],
  });
  rowHeight = () => 20;
  lineHeight = null;
  viewHeight = VIEW;
  stubLayout();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  unstubLayout();
  vi.useRealTimers();
});

describe('the store', () => {
  it('starts at the window’s first snapshot and notifies on a change alone', () => {
    const store = createCoreStore();
    expect(store.getSnapshot()).toBe(CORE_INITIAL_STATE);
    const listener = vi.fn();
    const unsubscribe = store.subscribe(listener);
    const next: CoreState = { ...CORE_INITIAL_STATE, loading: false };
    store.set(next);
    store.set(next);
    expect(listener).toHaveBeenCalledTimes(1);
    expect(store.getSnapshot()).toBe(next);
    unsubscribe();
    store.set(CORE_INITIAL_STATE);
    expect(listener).toHaveBeenCalledTimes(1);
  });
});

const LOADING: readonly 'loading'[] = ['loading'];

describe('useCoreFields', () => {
  it('re-renders its reader only when a field it reads moves', () => {
    const store = createCoreStore();
    let renders = 0;
    function FieldsReader() {
      useCoreFields(store, LOADING);
      useLayoutEffect(() => {
        renders += 1;
      });
      return null;
    }
    render(<FieldsReader />);
    const mounted = renders;

    act(() => store.set({ ...store.getSnapshot(), visible: 5, range: { from: 'a', to: 'b' } }));
    expect(renders).toBe(mounted);
    act(() => store.set({ ...store.getSnapshot(), loading: false }));
    expect(renders).toBe(mounted + 1);
  });
});

describe('the scroller', () => {
  it('is a focusable div with its own overflow, handing the render function its rows', () => {
    const { client } = scripted();
    const { scroller, view } = mount({ client });
    const el = scroller();
    expect(el.tagName).toBe('DIV');
    expect(el.tabIndex).toBe(0);
    expect(el.style.overflow).toBe('auto');
    expect(el.style.getPropertyValue('overflow-anchor')).toBe('none');
    expect(view.getByTestId('loading')).toBeInTheDocument();
  });
});

describe('the landing', () => {
  it('asks once under StrictMode, draws the page at keys from 0, and ends loading a frame later', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: HEAD, firstPageSize: 30 });
    await flush();
    expect(s.fetches.map((f) => f.query)).toEqual([{ kind: 'First', limit: 30 }]);
    await answer(s.fetches[0], page(0, 30, { edges: START }));
    expect(m.keys().slice(0, 10)).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8, 9]);
    expect(m.view.queryByText('line 0')).toBeInTheDocument();
    expect(m.state().loading).toBe(true);
    expect(m.view.getByTestId('loading')).toBeInTheDocument();
    await frame();
    expect(m.state().loading).toBe(false);
    expect(m.view.queryByTestId('loading')).toBeNull();
  });

  it('ends loading on the hidden timer when no frame comes', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: HEAD });
    await flush();
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation(() => 0);
    await answer(s.fetches[0], page(0, 30, { edges: START }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(249);
    });
    expect(m.state().loading).toBe(true);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(m.state().loading).toBe(false);
  });
});

describe('where a landing puts the scroller', () => {
  it('lands a Tail at the end, pinned', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: TAIL });
    await flush();
    expect(s.fetches[0].query).toEqual({ kind: 'Last', limit: 150 });
    await answer(s.fetches[0], page(0, 150, { edges: END }));
    expect(m.scroller().scrollTop).toBe(150 * 20 - VIEW);
    expect(m.keys()).toContain(0);
    expect(m.startOf(0)).toBe(149 * 20);
    expect(m.state().pinnedToEnd).toBe(true);
  });

  it('lands an At with key 0 at the top', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: AT });
    await flush();
    expect(s.fetches[0].query).toEqual({ kind: 'At', at: AT.at, limit: 150 });
    await answer(s.fetches[0], page(0, 150, { anchor: 60 }));
    await frame(3);
    expect(m.startOf(0)).toBe(60 * 20);
    expect(m.scroller().scrollTop).toBe(60 * 20);
    expect(m.state().pinnedToEnd).toBe(false);
  });

  it('marks a failed landing and reports it once', async () => {
    const s = scripted();
    const onError = vi.fn();
    const m = mount({ client: s.client, anchor: HEAD, onError });
    await flush();
    const error = new Error('no');
    await act(async () => {
      s.fetches[0].reject(error);
    });
    expect(onError).toHaveBeenCalledExactlyOnceWith(error);
    expect(m.state().failed).toBe(true);
    expect(m.view.getByTestId('failed')).toBeInTheDocument();
    await frame();
    expect(m.state().loading).toBe(false);
  });
});

describe('landing again', () => {
  it('reports nothing from a landing a jump replaced while it was in flight', async () => {
    const s = scripted();
    const onError = vi.fn();
    const m = mount({ client: s.client, anchor: HEAD, onError });
    await flush();
    act(() => m.ref.current!.jumpTo(TAIL));
    await flush();
    expect(s.fetches.map((f) => f.query.kind)).toEqual(['First', 'Last']);
    await act(async () => {
      s.fetches[0].reject(new Error('late'));
    });
    expect(onError).not.toHaveBeenCalled();
    await answer(s.fetches[1], page(0, 150, { edges: END }));
    expect(m.state().pinnedToEnd).toBe(true);
  });

  it('jumps to each kind, and a jump to Tail pins whatever pinToEnd said', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: HEAD, pinToEnd: false });
    await flush();
    await answer(s.fetches[0], page(0, 150, { edges: START }));
    act(() => m.ref.current!.jumpTo(TAIL));
    await flush();
    await answer(s.last(), page(0, 150, { edges: END }));
    expect(s.last().query.kind).toBe('Last');
    expect(m.state().pinnedToEnd).toBe(true);
    act(() => m.ref.current!.jumpTo(AT));
    await flush();
    expect(s.last().query).toEqual({ kind: 'At', at: AT.at, limit: 150 });
    await answer(s.last(), page(0, 150, { anchor: 10 }));
    expect(m.state().pinnedToEnd).toBe(false);
    act(() => m.ref.current!.jumpTo(HEAD));
    await flush();
    expect(s.last().query.kind).toBe('First');
  });

  it('hands each landing’s scroller to scrollerRef', async () => {
    const s = scripted();
    const seen: (HTMLDivElement | null)[] = [];
    const m = mount({ client: s.client, anchor: HEAD, scrollerRef: (el: HTMLDivElement | null) => seen.push(el) });
    await flush();
    const first = m.scroller();
    expect(seen.at(-1)).toBe(first);

    act(() => m.ref.current!.jumpTo(TAIL));
    await flush();
    expect(m.scroller()).not.toBe(first);
    expect(seen.at(-1)).toBe(m.scroller());
  });

  it('retries at the mount’s anchor, pinned as pinToEnd says', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: TAIL, pinToEnd: false });
    await flush();
    act(() => m.ref.current!.jumpTo(HEAD));
    await flush();
    await act(async () => {
      s.last().reject(new Error('no'));
    });
    act(() => m.ref.current!.retry());
    await flush();
    expect(s.last().query.kind).toBe('Last');
    await answer(s.last(), page(0, 150, { edges: END }));
    expect(m.state().pinnedToEnd).toBe(false);
    expect(m.scroller().scrollTop).toBe(150 * 20 - VIEW);
  });

  it('lands a new client where the last jump did, and asks the old one nothing more', async () => {
    const a = scripted();
    const b = scripted();
    const m = mount({ client: a.client, anchor: TAIL });
    await flush();
    await answer(a.fetches[0], page(0, 150, { edges: END }));
    act(() => m.ref.current!.jumpTo(AT));
    await flush();
    await answer(a.last(), page(0, 150, { anchor: 10 }));
    const asked = a.fetches.length;
    m.rerender({ client: b.client });
    await flush();
    expect(b.fetches.map((f) => f.query)).toEqual([{ kind: 'At', at: AT.at, limit: 150 }]);
    await answer(b.fetches[0], page(0, 150, { anchor: 10 }));
    await frame(3);
    expect(a.fetches.length).toBe(asked);
    expect(a.open()).toEqual([]);
  });
});

// What a parent reading the store last committed.
function Reader({ store, onCommit }: { store: CoreStore; onCommit: (state: CoreState) => void }) {
  const state = useSyncExternalStore(store.subscribe, store.getSnapshot);
  useLayoutEffect(() => {
    onCommit(state);
  }, [state, onCommit]);
  return null;
}

// Mounts with each landing and notes what the parent had committed once the landing's first
// commit task is over: a microtask runs before the browser paints, and after any re-render a
// layout effect forced.
function AtFirstPaint({ box, painted }: { box: { last: CoreState | null }; painted: (CoreState | null)[] }) {
  useLayoutEffect(() => {
    queueMicrotask(() => painted.push(box.last));
  }, [box, painted]);
  return null;
}

// React's scheduler runs its tasks on `setImmediate` under Node, which the fake timers leave real.
const { setImmediate: nextTask } = globalThis as unknown as { setImmediate: (fn: () => void) => void };

/** Runs `fn` on React's own scheduler, outside act, and lets its tasks run. */
async function unbatched(fn: () => void) {
  const env = globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean };
  env.IS_REACT_ACT_ENVIRONMENT = false;
  try {
    fn();
    for (let i = 0; i < 5; i += 1) {
      // oxlint-disable-next-line no-await-in-loop -- one scheduler task each
      await new Promise<void>((resolve) => {
        nextTask(resolve);
      });
    }
  } finally {
    env.IS_REACT_ACT_ENVIRONMENT = true;
  }
}

describe('the store under a parent', () => {
  function mountWithReader(client: Client) {
    const store = createCoreStore();
    const ref = createRef<CoreHandle>();
    const box: { last: CoreState | null } = { last: null };
    const painted: (CoreState | null)[] = [];
    const element = (c: Client) => (
      <StrictMode>
        <Reader
          store={store}
          onCommit={(state) => {
            box.last = state;
          }}
        />
        <LogViewerCore
          ref={ref}
          client={c}
          anchor={TAIL}
          pinToEnd
          store={store}
          estimateRowHeight={() => 20}
          className="scroller"
        >
          {(rows) => (
            <>
              {draw(rows)}
              <AtFirstPaint box={box} painted={painted} />
            </>
          )}
        </LogViewerCore>
      </StrictMode>
    );
    const view = render(element(client));
    return { store, ref, box, painted, view, rerender: (c: Client) => view.rerender(element(c)) };
  }

  async function landPinned(s: ReturnType<typeof scripted>, box: { last: CoreState | null }) {
    await flush();
    await answer(s.last(), page(0, 150, { edges: END }));
    await frame();
    expect(box.last).toMatchObject({ loading: false, pinnedToEnd: true, following: true });
  }

  it('shows each change, and loading with no pin from the first paint after a jump, a retry or a new client', async () => {
    const s = scripted();
    const m = mountWithReader(s.client);
    await landPinned(s, m.box);

    let from = m.painted.length;
    await unbatched(() => m.ref.current!.jumpTo(TAIL));
    expect(m.painted[from]).toMatchObject({ loading: true, pinnedToEnd: false, following: false });
    await landPinned(s, m.box);

    from = m.painted.length;
    await unbatched(() => m.ref.current!.retry());
    expect(m.painted[from]).toMatchObject({ loading: true, pinnedToEnd: false, following: false });
    await landPinned(s, m.box);

    const next = scripted();
    from = m.painted.length;
    await unbatched(() => m.rerender(next.client));
    expect(m.painted[from]).toMatchObject({ loading: true, pinnedToEnd: false, following: false });
    await landPinned(next, m.box);
  });
});

const lineFrame = (i: number): TailFrame => ({ kind: 'Line', line: line(i) });

async function send(follow: Follow, frames: TailFrame[]) {
  await act(async () => {
    frames.forEach((f) => follow.onFrame(f));
  });
}

async function landTail(s: ReturnType<typeof scripted>, over: Partial<Page> = {}) {
  await flush();
  await answer(s.last(), page(0, 150, { ...over, edges: { ...END, ...over.edges } }));
  await frame();
}

describe('the follow', () => {
  it('opens once the window follows, at its resume cursor, and stays open alone', async () => {
    const s = scripted();
    const m = mount({ client: s.client });
    await flush();
    expect(s.follows).toEqual([]);
    await landTail(s);
    expect(s.open().map((f) => f.cursor)).toEqual(['live-end']);
    expect(m.state().following).toBe(true);
  });

  it('hands frames over once per frame, and keeps the end in view', async () => {
    const s = scripted();
    const m = mount({ client: s.client });
    await landTail(s);
    const [follow] = s.open();
    await send(follow, [lineFrame(150), lineFrame(151), { kind: 'Notice', notice: { code: 'n', message: 'n' } }]);
    expect(m.view.queryByText('line 151')).toBeNull();
    await frame();
    expect(m.view.queryByText('line 151')).toBeInTheDocument();
    expect(m.scroller().scrollTop).toBe(152 * 20 - VIEW);
  });

  it('hands frames over on the hidden timer when no frame comes', async () => {
    const s = scripted();
    const m = mount({ client: s.client });
    await landTail(s);
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation(() => 0);
    await send(s.open()[0], [lineFrame(150)]);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(250);
    });
    expect(m.view.queryByText('line 150')).toBeInTheDocument();
  });

  it('goes to followFailed and onError once when the client refuses it', async () => {
    const s = scripted();
    const onError = vi.fn();
    s.refuseNextFollow();
    const m = mount({ client: s.client, onError });
    await landTail(s);
    expect(onError).toHaveBeenCalledOnce();
    expect(m.state()).toMatchObject({ pinnedToEnd: false, following: false });
    expect(s.open()).toEqual([]);
  });

  it('goes to followFailed and onError once, and closes the follow, when it fails after opening', async () => {
    const s = scripted();
    const onError = vi.fn();
    const m = mount({ client: s.client, onError });
    await landTail(s);
    const [follow] = s.open();
    await act(async () => {
      follow.onFailed(new Error('cache gone'));
    });
    expect(onError).toHaveBeenCalledOnce();
    expect(m.state()).toMatchObject({ pinnedToEnd: false, following: false });
    expect(follow.closed).toBe(true);

    // A failure that lands after the follow closed changes nothing.
    follow.onFailed(new Error('late'));
    expect(onError).toHaveBeenCalledOnce();
  });
});

describe('the pause', () => {
  it('closes the follow, drops the batch still waiting, and opens it again on play when pinned', async () => {
    const s = scripted();
    const m = mount({ client: s.client });
    await landTail(s);
    const [follow] = s.open();
    await send(follow, [lineFrame(150)]);
    m.rerender({ paused: true });
    await flush();
    expect(follow.closed).toBe(true);
    await frame(2);
    expect(m.view.queryByText('line 150')).toBeNull();
    expect(m.state().following).toBe(false);
    m.rerender({ paused: false });
    await flush();
    expect(s.open().map((f) => f.cursor)).toEqual(['live-end']);
  });

  it('lands a jump paused, opening no follow and paging nothing after', async () => {
    const s = scripted();
    const m = mount({ client: s.client, paused: true });
    await landTail(s);
    expect(m.state()).toMatchObject({ pinnedToEnd: true, following: false });
    act(() => m.ref.current!.jumpTo(TAIL));
    await landTail(s);
    const asked = s.fetches.length;
    act(() => m.view.getByTestId('more-after').click());
    await flush();
    expect(s.fetches.length).toBe(asked);
    expect(s.follows).toEqual([]);
  });
});

/** A scroll with no input behind it, as the core's own or the browser's. */
async function scrollTo(el: HTMLElement, top: number) {
  await act(async () => {
    el.scrollTo({ top });
  });
}

describe('paging', () => {
  it('pages after as the visible range nears the end of a window behind', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: HEAD });
    await flush();
    await answer(s.fetches[0], page(0, 150, { edges: START }));
    await frame();
    expect(s.fetches.length).toBe(1);
    await scrollTo(m.scroller(), 2000);
    await flush();
    expect(s.fetches.map((f) => f.query)).toEqual([
      { kind: 'First', limit: 150 },
      expect.objectContaining({ kind: 'Continue', cursor: 'after-150', limit: 100 }),
    ]);
  });

  it('pages before as the visible range nears the start', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: AT });
    await flush();
    await answer(s.fetches[0], page(0, 150, { anchor: 75 }));
    await frame(3);
    expect(s.fetches.length).toBe(1);
    await scrollTo(m.scroller(), 400);
    await flush();
    expect(s.last().query).toEqual({ kind: 'Continue', cursor: 'before-0', seen: null, limit: 100 });
  });

  it('asks no page before on a Tail landing until the user scrolls near it', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: TAIL });
    await landTail(s);
    await frame(3);
    expect(s.fetches.length).toBe(1);
    await scrollTo(m.scroller(), 400);
    await flush();
    expect(s.last().query).toEqual({ kind: 'Continue', cursor: 'before-0', seen: null, limit: 100 });
  });

  it('reads the next page toward an edge when the render function asks', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: HEAD });
    await flush();
    await answer(s.fetches[0], page(0, 150, { edges: START }));
    await frame();
    act(() => m.view.getByTestId('more-after').click());
    await flush();
    expect(s.last().query).toMatchObject({ kind: 'Continue', cursor: 'after-150', limit: 100 });
  });
});

describe('keeping the reader’s place across pages', () => {
  async function landAt(s: ReturnType<typeof scripted>) {
    await flush();
    await answer(s.fetches[0], page(0, 150, { anchor: 75 }));
    await frame(3);
  }

  it('keeps the first visible row where it was after a page before, and asks no second page off the range it moved', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: AT });
    await landAt(s);
    await scrollTo(m.scroller(), 407);
    await flush();
    expect(s.fetches.length).toBe(2);
    await answer(s.last(), page(-100, 0, { edges: { next: 'x' } }));
    expect(m.startOf(-55)).toBe(120 * 20);
    expect(m.scroller().scrollTop).toBe(120 * 20 + 7);
    await frame(2);
    expect(s.fetches.length).toBe(2);
  });

  it('pages again on the next frame after a page of repeats, with no scroll', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: HEAD });
    await flush();
    await answer(s.fetches[0], page(0, 150, { edges: START }));
    await frame();
    await scrollTo(m.scroller(), 2000);
    await flush();
    await answer(s.last(), page(140, 150, { edges: { next: 'after-repeats' } }));
    expect(s.fetches.length).toBe(2);
    await frame();
    expect(s.last().query).toMatchObject({ kind: 'Continue', cursor: 'after-repeats', limit: 100 });
  });

  it('leaves the edge after an empty page, and pages again only on the next scroll or loadMore', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: HEAD });
    await flush();
    await answer(s.fetches[0], page(0, 150, { edges: START }));
    await frame();
    await scrollTo(m.scroller(), 2000);
    await flush();
    await answer(s.last(), page(0, 0, { edges: { next: 'after-empty' } }));
    await frame(3);
    expect(s.fetches.length).toBe(2);
    expect(m.state().after).toEqual({ more: true, empty: true, loading: false });
    await scrollTo(m.scroller(), 2020);
    await flush();
    expect(s.fetches.length).toBe(3);
    expect(s.last().query).toMatchObject({ cursor: 'after-empty' });
    await answer(s.last(), page(0, 0, { edges: { next: 'after-empty-2' } }));
    await frame(3);
    act(() => m.view.getByTestId('more-after').click());
    await flush();
    expect(s.last().query).toMatchObject({ cursor: 'after-empty-2' });
  });

  it('leaves the scroller where the user put it after a page of repeats before', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: AT });
    await landAt(s);
    await scrollTo(m.scroller(), 400);
    await flush();
    await answer(s.last(), page(0, 10, { edges: { prev: 'before-r' } }));
    await frame(2);
    await scrollTo(m.scroller(), 900);
    await frame(3);
    expect(m.scroller().scrollTop).toBe(900);
  });
});

describe('a page that fails', () => {
  it('keeps the rows and reports once per run of failures, again after a success', async () => {
    const s = scripted();
    const onError = vi.fn();
    const m = mount({ client: s.client, anchor: HEAD, onError });
    await flush();
    await answer(s.fetches[0], page(0, 150, { edges: START }));
    await frame();
    const fail = async () => {
      act(() => m.view.getByTestId('more-after').click());
      await flush();
      await act(async () => {
        s.last().reject(new Error('page'));
      });
    };
    await fail();
    await fail();
    await fail();
    expect(onError).toHaveBeenCalledOnce();
    expect(m.view.queryByText('line 0')).toBeInTheDocument();
    act(() => m.view.getByTestId('more-after').click());
    await flush();
    await answer(s.last(), page(150, 160));
    await frame();
    await fail();
    expect(onError).toHaveBeenCalledTimes(2);
  });
});

describe('keeping the end in view while following', () => {
  it('keeps it when a row measures taller', async () => {
    const s = scripted();
    const m = mount({ client: s.client });
    await landTail(s);
    rowHeight = (key) => (key === 0 ? 50 : 20);
    fireResize();
    await frame(2);
    expect(m.scroller().scrollTop).toBe(149 * 20 + 50 - VIEW);
  });

  it('keeps it when the scroller resizes', async () => {
    const s = scripted();
    const m = mount({ client: s.client });
    await landTail(s);
    viewHeight = 100;
    fireResize();
    await frame(2);
    expect(m.scroller().scrollTop).toBe(150 * 20 - 100);
  });
});

/** An input on the scroller, then the scroll it causes, as a browser sends them. */
async function input(el: HTMLElement, event: () => void, top?: number) {
  await act(async () => {
    event();
    if (top !== undefined) el.scrollTo({ top });
  });
}

const BOTTOM = 150 * 20 - VIEW;

describe('the user’s intent', () => {
  async function following() {
    const s = scripted();
    const m = mount({ client: s.client });
    await landTail(s);
    expect(m.state().following).toBe(true);
    return { s, m, el: m.scroller() };
  }

  it('unpins and closes the follow on a wheel up', async () => {
    const { s, m, el } = await following();
    const [follow] = s.open();
    await input(el, () => fireEvent.wheel(el, { deltaY: -100 }), BOTTOM - 100);
    await flush();
    expect(m.state()).toMatchObject({ pinnedToEnd: false, following: false });
    expect(follow.closed).toBe(true);
    await frame(3);
    expect(el.scrollTop).toBe(BOTTOM - 100);
  });

  it('stays where a wheel up put it between two batches, and when its scroll lands with a batch', async () => {
    const { s, m, el } = await following();
    const [follow] = s.open();
    await send(follow, [lineFrame(150)]);
    await frame();
    const bottom = 151 * 20 - VIEW;
    expect(el.scrollTop).toBe(bottom);
    await send(follow, [lineFrame(151)]);
    await input(el, () => fireEvent.wheel(el, { deltaY: -100 }), bottom - 100);
    await send(follow, [lineFrame(152)]);
    await frame(3);
    expect(el.scrollTop).toBe(bottom - 100);
    expect(m.view.queryByText('line 151')).toBeNull();
    expect(m.state().following).toBe(false);
  });

  it.each([
    ['ArrowUp', {}],
    ['PageUp', {}],
    [' ', { shiftKey: true }],
  ])('unpins on %j with %j', async (key, mods) => {
    const { m, el } = await following();
    await input(el, () => fireEvent.keyDown(el, { key, ...mods }), BOTTOM - 40);
    expect(m.state().pinnedToEnd).toBe(false);
  });

  it('unpins on Home even when the scroll it causes moves nothing', async () => {
    const { m, el } = await following();
    await input(el, () => fireEvent.keyDown(el, { key: 'Home' }));
    expect(m.state().pinnedToEnd).toBe(false);
  });

  it('unpins on a pointer down, before any scroll', async () => {
    const { s, m, el } = await following();
    const [follow] = s.open();
    await input(el, () => fireEvent.pointerDown(el));
    expect(m.state()).toMatchObject({ pinnedToEnd: false, following: false });
    expect(follow.closed).toBe(true);
  });

  it('reads a pointer drag as the user’s for as long as the pointer is down, with an overlay scrollbar', async () => {
    const { s, m, el } = await following();
    expect(el.clientWidth).toBe(300);
    await input(el, () => fireEvent.pointerDown(el));
    const asked = s.fetches.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    // Back to the bottom under the pointer is the user's scroll, so the page it asks for pins.
    await input(el, () => {}, BOTTOM - 300);
    await input(el, () => {}, BOTTOM);
    expect(s.fetches.length).toBe(asked + 1);
    await answer(s.last(), page(150, 160, { edges: END }));
    expect(m.state().pinnedToEnd).toBe(true);
  });

  it.each(['pointercancel', 'blur'])('ends pointer intent on %s with no pointerup', async (end) => {
    const { s, m, el } = await following();
    await input(el, () => fireEvent.pointerDown(el));
    const asked = s.fetches.length;
    await act(async () => {
      window.dispatchEvent(new Event(end));
      await vi.advanceTimersByTimeAsync(200);
    });
    // The window behind pages off the visible range, but a scroll that is not the user's asks
    // the page for no pin.
    await scrollTo(el, BOTTOM - 100);
    await scrollTo(el, BOTTOM);
    expect(s.fetches.length).toBe(asked + 1);
    await answer(s.last(), page(150, 160, { edges: END }));
    expect(m.state().pinnedToEnd).toBe(false);
  });

  it('reads a touch drag as the user’s until it ends', async () => {
    const { m, el } = await following();
    await input(el, () => fireEvent.touchStart(el));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    await input(el, () => fireEvent.touchMove(el), BOTTOM - 100);
    expect(m.state().pinnedToEnd).toBe(false);
  });

  it.each(['touchEnd', 'touchCancel'] as const)('ends touch intent on %s', async (end) => {
    const { m, el } = await following();
    await input(el, () => fireEvent.touchStart(el));
    await act(async () => {
      fireEvent[end](el);
      await vi.advanceTimersByTimeAsync(200);
    });
    // The same move that unpins mid-drag is not the user's once the finger is off.
    await scrollTo(el, BOTTOM - 100);
    expect(m.state().pinnedToEnd).toBe(true);
  });

  it('changes nothing on a scroll with no input behind it, or long after one', async () => {
    const { m, el } = await following();
    await scrollTo(el, BOTTOM - 100);
    expect(m.state().pinnedToEnd).toBe(true);
    await input(el, () => fireEvent.wheel(el, { deltaY: 100 }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(200);
    });
    await scrollTo(el, BOTTOM - 100);
    expect(m.state().pinnedToEnd).toBe(true);
  });

  it('changes nothing on the clamp as content shrinks', async () => {
    const { m, el } = await following();
    rowHeight = () => 19;
    fireResize();
    await frame(2);
    expect(el.scrollTop).toBeLessThan(BOTTOM);
    expect(el.scrollTop).toBe(el.scrollHeight - VIEW);
    expect(m.state().pinnedToEnd).toBe(true);
  });

  it('changes nothing on the clamp as content shrinks during a wheel down', async () => {
    const { m, el } = await following();
    await input(el, () => fireEvent.wheel(el, { deltaY: 100 }));
    rowHeight = () => 19;
    fireResize();
    await frame(2);
    expect(el.scrollTop).toBeLessThan(BOTTOM);
    expect(m.state().pinnedToEnd).toBe(true);
  });

  it('unpins on an up key when the content overflows by less than the tolerance', async () => {
    viewHeight = 150 * 20 - 5;
    const { m, el } = await following();
    expect(el.scrollTop).toBe(5);
    await input(el, () => fireEvent.keyDown(el, { key: 'ArrowUp' }), 0);
    expect(m.state().pinnedToEnd).toBe(false);
  });

  it('unpins on a wheel up whose scroll stays within the tolerance', async () => {
    const { m, el } = await following();
    await input(el, () => fireEvent.wheel(el, { deltaY: -5 }), BOTTOM - 5);
    expect(m.state().pinnedToEnd).toBe(false);
  });

  describe('reaching the bottom of a window behind', () => {
    async function behind() {
      const s = scripted();
      const m = mount({ client: s.client, anchor: HEAD, loadMoreThreshold: 0 });
      await flush();
      await answer(s.fetches[0], page(0, 150, { edges: START }));
      await frame();
      return { s, m, el: m.scroller() };
    }

    it('asks for a page on a wheel down to the bottom and pins when it finds no more', async () => {
      const { s, m, el } = await behind();
      await input(el, () => fireEvent.wheel(el, { deltaY: 100 }), BOTTOM);
      await flush();
      expect(s.last().query).toMatchObject({ kind: 'Continue', cursor: 'after-150', limit: 100 });
      await answer(s.last(), page(150, 160, { edges: END }));
      expect(m.state()).toMatchObject({ pinnedToEnd: true, following: true });
      expect(el.scrollTop).toBe(160 * 20 - VIEW);
    });

    it('does the same on a wheel down while already at the bottom', async () => {
      const { s, m, el } = await behind();
      await scrollTo(el, BOTTOM);
      await flush();
      expect(s.fetches.length).toBe(2);
      await input(el, () => fireEvent.wheel(el, { deltaY: 100 }));
      // The wheel's layout read waits for the frame.
      await frame();
      await answer(s.last(), page(150, 160, { edges: END }));
      expect(m.state().pinnedToEnd).toBe(true);
    });

    it('reads no layout per wheel event, and reaches the bottom on the next frame', async () => {
      const { s, m, el } = await behind();
      await scrollTo(el, BOTTOM);
      await flush();
      const reads = vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get');
      await input(el, () => {
        for (let i = 0; i < 5; i += 1) fireEvent.wheel(el, { deltaY: 20 });
      });
      expect(reads).not.toHaveBeenCalled();
      await frame();
      await answer(s.last(), page(150, 160, { edges: END }));
      expect(m.state().pinnedToEnd).toBe(true);
    });

    it('does not pin when the bottom was reached with no input behind it', async () => {
      const { s, m, el } = await behind();
      await scrollTo(el, BOTTOM);
      await flush();
      await answer(s.last(), page(150, 160, { edges: END }));
      expect(m.state().pinnedToEnd).toBe(false);
    });

    it('does the same on End, and still pages on End in the frame after a page after landed', async () => {
      const { s, m, el } = await behind();
      await scrollTo(el, BOTTOM);
      await flush();
      await answer(s.last(), page(150, 160));
      const asked = s.fetches.length;
      await input(el, () => fireEvent.keyDown(el, { key: 'End' }), 160 * 20 - VIEW);
      await flush();
      expect(s.fetches.length).toBe(asked + 1);
      await answer(s.last(), page(160, 170, { edges: END }));
      expect(m.state().pinnedToEnd).toBe(true);
    });

    it('pins again after a refused follow when the user scrolls to the bottom', async () => {
      const s = scripted();
      s.refuseNextFollow();
      const m = mount({ client: s.client, onError: () => {} });
      await landTail(s);
      expect(m.state().pinnedToEnd).toBe(false);
      const el = m.scroller();
      await input(el, () => fireEvent.wheel(el, { deltaY: 100 }));
      await frame();
      expect(s.last().query).toMatchObject({ kind: 'Continue', cursor: 'live-end', limit: 100 });
      await answer(s.last(), page(150, 150, { edges: END }));
      expect(m.state().following).toBe(true);
      expect(s.open().map((f) => f.cursor)).toEqual(['live-end']);
    });
  });
});

describe('measure', () => {
  async function landAt(props: Partial<MountProps> = {}) {
    const s = scripted();
    const m = mount({ client: s.client, anchor: AT, ...props });
    await flush();
    await answer(s.fetches[0], page(0, 150, { anchor: 75 }));
    await frame(3);
    return { s, m, el: m.scroller() };
  }

  it('keeps the first visible row where it was when every row changes height', async () => {
    const { m, el } = await landAt();
    await scrollTo(el, 75 * 20 + 7);
    await frame(12);
    rowHeight = () => 30;
    m.rerender({ estimateRowHeight: () => 30 });
    act(() => m.ref.current!.measure());
    fireResize();
    await frame(12);
    expect(m.startOf(0)).toBe(75 * 30);
    expect(el.scrollTop).toBe(75 * 30 + 7);
  });

  it('keeps the end while following', async () => {
    const s = scripted();
    const m = mount({ client: s.client });
    await landTail(s);
    rowHeight = () => 30;
    m.rerender({ estimateRowHeight: () => 30 });
    act(() => m.ref.current!.measure());
    fireResize();
    await frame(2);
    expect(m.scroller().scrollTop).toBe(150 * 30 - VIEW);
  });

  it('keeps the row when a page before and a re-measure land in one commit', async () => {
    const { s, m, el } = await landAt();
    await scrollTo(el, 407);
    await flush();
    rowHeight = () => 30;
    m.rerender({ estimateRowHeight: () => 30 });
    await act(async () => {
      s.last().resolve(page(-100, 0, { edges: { next: 'x' } }));
      await Promise.resolve();
      m.ref.current!.measure();
    });
    fireResize();
    await frame(12);
    expect(m.startOf(-55)).toBe(120 * 30);
    expect(el.scrollTop).toBe(120 * 30 + 7);
  });

  it('pages nothing by the range while in flight, and pages once it is over', async () => {
    const { s, m, el } = await landAt();
    act(() => m.ref.current!.measure());
    await scrollTo(el, 400);
    await flush();
    expect(s.fetches.length).toBe(1);
    await frame();
    expect(s.last().query).toMatchObject({ kind: 'Continue', seen: null });
  });

  it('does nothing on a second call while one is in flight', async () => {
    const estimate = vi.fn(() => 20);
    const { m } = await landAt({ estimateRowHeight: estimate });
    act(() => m.ref.current!.measure());
    await flush();
    expect(estimate).toHaveBeenCalled();
    estimate.mockClear();
    act(() => m.ref.current!.measure());
    await flush();
    expect(estimate).not.toHaveBeenCalled();
    await frame();
    act(() => m.ref.current!.measure());
    await flush();
    expect(estimate).toHaveBeenCalled();
  });
});

describe('landing at a row', () => {
  it('puts the row at the top after the rows above it measured taller than estimated', async () => {
    rowHeight = (key) => (key < 0 ? 30 : 20);
    const s = scripted();
    const m = mount({ client: s.client, anchor: AT });
    await flush();
    await answer(s.fetches[0], page(0, 150, { anchor: 75 }));
    fireResize();
    await frame(6);
    expect(m.startOf(0)).toBeGreaterThan(75 * 20);
    expect(m.scroller().scrollTop).toBe(m.startOf(0));
  });
});

describe('the bound', () => {
  // Answers a page toward either edge off a log of line `i` at `i` seconds, ten lines a page.
  function pageFor(query: FetchQuery): Page {
    const oldestHeld = (seen: Cursor[]) => Math.min(...seen.map((c) => Number(c.slice(1))));
    const newestHeld = (seen: Cursor[]) => Math.max(...seen.map((c) => Number(c.slice(1))));
    let from: number;
    if (query.kind === 'Until') from = oldestHeld(query.seen) - query.limit;
    else if (query.kind === 'Since') from = newestHeld(query.seen) + 1;
    else if (query.kind === 'Continue' && query.cursor.startsWith('before-'))
      from = Number(query.cursor.slice(7)) - query.limit;
    else if (query.kind === 'Continue') from = Number(query.cursor.slice(6));
    else throw new Error(`no page for ${query.kind}`);
    return page(from, from + query.limit);
  }

  // Answers every page asked, a round at a time, until the core asks no more.
  async function answerAll(s: ReturnType<typeof scripted>, answered: Set<Fetch>, round = 0): Promise<void> {
    const waiting = s.fetches.filter((f) => !answered.has(f));
    if (waiting.length === 0 || round === 30) return;
    await waiting.reduce(
      (prev, f) =>
        prev.then(() => {
          answered.add(f);
          return answer(f, pageFor(f.query));
        }),
      Promise.resolve(),
    );
    await frame(2);
    await answerAll(s, answered, round + 1);
  }

  it('keeps a follow run past maxLines at maxLines, with the end in view', async () => {
    const s = scripted();
    const m = mount({ client: s.client, maxLines: 200, loadMoreThreshold: 10 });
    await landTail(s);
    await send(
      s.open()[0],
      lines(150, 250).map((l): TailFrame => ({ kind: 'Line', line: l })),
    );
    await frame(3);
    expect(m.state()).toMatchObject({ length: 200, firstKey: -99, following: true, atEnd: true });
    expect(m.scroller().scrollTop).toBe(200 * 20 - VIEW);
  });

  it('ends a batch larger than the room above the view at the edge bound once its end is in view', async () => {
    const s = scripted();
    const m = mount({ client: s.client, maxLines: 20, loadMoreThreshold: 2 });
    await landTail(s);
    await send(
      s.open()[0],
      lines(150, 250).map((l): TailFrame => ({ kind: 'Line', line: l })),
    );
    await frame(4);
    expect(m.state()).toMatchObject({ length: 20, following: true, atEnd: true });
    expect(m.scroller().scrollTop).toBe(20 * 20 - VIEW);
    expect(s.fetches).toHaveLength(1);
  });

  it('pages the dropped lines in again on a scroll up, the first visible row where it was, and pins again at the bottom', async () => {
    const s = scripted();
    const m = mount({ client: s.client, maxLines: 200, loadMoreThreshold: 10 });
    await landTail(s);
    await send(
      s.open()[0],
      lines(150, 250).map((l): TailFrame => ({ kind: 'Line', line: l })),
    );
    await frame(3);

    await input(m.scroller(), () => fireEvent.wheel(m.scroller(), { deltaY: -100 }), 100);
    await flush();
    expect(s.last().query).toEqual({ kind: 'Until', at: line(49).timestamp, seen: ['c50'], limit: 100 });
    await answer(s.last(), page(0, 50, { edges: START }));
    // Row -94 sat at the top; fifty rows went in above it.
    expect(m.scroller().scrollTop).toBe(55 * 20);
    expect(m.state()).toMatchObject({ length: 200, firstKey: -149, pinnedToEnd: false });

    await frame(2);
    await input(m.scroller(), () => fireEvent.keyDown(m.scroller(), { key: 'End' }), 200 * 20 - VIEW);
    await flush();
    expect(s.last().query).toEqual({ kind: 'Since', at: line(200).timestamp, seen: ['c199'], limit: 100 });
    await answer(s.last(), page(200, 250, { edges: END }));
    await frame(3);
    expect(m.state()).toMatchObject({ length: 200, following: true, firstKey: -99 });
  });

  it('settles a still reader between the edges under the between bound, paging stopped, every visible key held', async () => {
    const s = scripted();
    const m = mount({
      client: s.client,
      anchor: AT,
      maxLines: 20,
      loadMoreThreshold: 2,
      firstPageSize: 10,
      pageSize: 10,
    });
    await flush();
    await answer(s.fetches[0], page(55, 65, { anchor: 5 }));
    await frame(3);
    const answered = new Set([s.fetches[0]]);
    await answerAll(s, answered);
    await frame(3);
    expect(s.fetches.filter((f) => !answered.has(f))).toEqual([]);
    // max(maxLines, visible + 2 · threshold + pageSize + 1)
    expect(m.state().length).toBeLessThanOrEqual(10 + 2 * 2 + 10 + 1);
    const held = new Set(Array.from({ length: m.state().length }, (_, i) => m.state().firstKey + i));
    expect(m.keys().every((key) => held.has(key))).toBe(true);
  });

  it('settles rows read back under reused keys at their own measured heights', async () => {
    lineHeight = (cursor) => (Number(cursor.slice(1)) % 2 === 0 ? 20 : 40);
    const s = scripted();
    const m = mount({ client: s.client, maxLines: 200, loadMoreThreshold: 10 });
    await landTail(s);
    await send(
      s.open()[0],
      lines(150, 250).map((l): TailFrame => ({ kind: 'Line', line: l })),
    );
    await frame(3);
    await input(m.scroller(), () => fireEvent.wheel(m.scroller(), { deltaY: -100 }), 100);
    await flush();
    await answer(s.last(), page(0, 50, { edges: START }));
    await frame(3);

    const rows = [...m.view.container.querySelectorAll<HTMLElement>('[data-key]')].toSorted(
      (a, b) => Number(a.dataset.start) - Number(b.dataset.start),
    );
    rows.slice(1).forEach((row, k) => {
      const above = rows[k];
      expect(Number(row.dataset.start) - Number(above.dataset.start)).toBe(lineHeight!(above.dataset.cursor!));
    });
  });

  it('changes nothing for a page made stale by a drop, and holds its edge for the frame it lands in', async () => {
    const s = scripted();
    const m = mount({ client: s.client, anchor: AT, maxLines: 150, loadMoreThreshold: 10 });
    await flush();
    await answer(s.fetches[0], page(0, 150, { anchor: 75 }));
    await frame(3);
    await scrollTo(m.scroller(), 0);
    await flush();
    const before = s.last();
    expect(before.query).toMatchObject({ kind: 'Continue', cursor: 'before-0' });
    await scrollTo(m.scroller(), 150 * 20 - VIEW);
    await flush();
    await answer(s.last(), page(150, 200));
    expect(m.state()).toMatchObject({ length: 150, firstKey: -25, before: { more: true, loading: false } });

    const asked = s.fetches.length;
    await answer(before, page(-100, 0));
    expect(m.state()).toMatchObject({ length: 150, firstKey: -25 });
    await frame(2);
    expect(s.fetches).toHaveLength(asked);
    await scrollTo(m.scroller(), 0);
    await flush();
    expect(s.last().query).toMatchObject({ kind: 'Until', at: line(49).timestamp, seen: ['c50'] });
  });
});
