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

import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { StrictMode } from 'react';
import { Client as UrqlClient, Provider as UrqlProvider } from 'urql';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Client, FetchQuery, TailFrame, LogLine, Page } from '@/lib/log-viewer';
import {
  EMPTY_LOG_VIEWPORT,
  type LogViewport,
  type LogViewportHandle,
  createLogViewportHandle,
} from '@/lib/log-viewport';
import type { LogsViewAction } from '@/lib/logs-view';

// The seam: the client the viewer builds is the test's.
const { built } = vi.hoisted(() => ({ built: { current: null as Client | null, calls: [] as unknown[][] } }));
vi.mock('@/lib/logs-client', () => ({
  createLogsClient: (...args: unknown[]) => {
    built.calls.push(args);
    return built.current;
  },
}));

const { reportError } = vi.hoisted(() => ({ reportError: vi.fn() }));
const onGrepApplied = vi.fn();
vi.mock('@/lib/error-bus', () => ({ reportError }));

const { LogViewer } = await import('./log-viewer');
const { LOG_ROW_PX } = await import('./row');

// --- Layout. jsdom lays nothing out: the scroller is 200px tall, its content the sizer the
// viewer draws, and `scrollTop` clamps and fires `scroll` as a browser's does. ---

const VIEW = 200;
// The core's scroller: focusable, with its own overflow. A popover trigger is focusable too.
const SCROLLER = '[tabindex="0"][style*="overflow"]';
const isScroller = (el: HTMLElement) => el.matches(SCROLLER);
const sizerHeight = (el: HTMLElement) =>
  Number.parseFloat((el.firstElementChild as HTMLElement | null)?.style.height ?? '') || 0;
const tops = new WeakMap<HTMLElement, number>();
const stubbed: [string, PropertyDescriptor | undefined][] = [];

function stub(name: string, descriptor: PropertyDescriptor) {
  stubbed.push([name, Object.getOwnPropertyDescriptor(HTMLElement.prototype, name)]);
  Object.defineProperty(HTMLElement.prototype, name, { configurable: true, ...descriptor });
}

function stubLayout() {
  const height = (el: HTMLElement) => (isScroller(el) ? VIEW : 0);
  stub('offsetHeight', {
    get: function get(this: HTMLElement) {
      return height(this);
    },
  });
  stub('clientHeight', {
    get: function get(this: HTMLElement) {
      return height(this);
    },
  });
  stub('offsetWidth', { get: () => 300 });
  stub('scrollHeight', {
    get: function get(this: HTMLElement) {
      return isScroller(this) ? Math.max(VIEW, sizerHeight(this)) : 0;
    },
  });
  stub('scrollTop', {
    get: function get(this: HTMLElement) {
      return tops.get(this) ?? 0;
    },
    set: function set(this: HTMLElement, value: number) {
      const next = Math.min(Math.max(0, Math.round(value)), Math.max(0, sizerHeight(this) - VIEW));
      if (next === (tops.get(this) ?? 0)) return;
      tops.set(this, next);
      this.dispatchEvent(new Event('scroll'));
    },
  });
  stub('scrollTo', {
    value: function scrollTo(this: HTMLElement, options: ScrollToOptions) {
      if (options.top !== undefined) this.scrollTop = options.top;
    },
  });
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}

      unobserve() {}

      disconnect() {}
    },
  );
}

function unstubLayout() {
  stubbed.toReversed().forEach(([name, descriptor]) => {
    if (descriptor) Object.defineProperty(HTMLElement.prototype, name, descriptor);
    else delete (HTMLElement.prototype as unknown as Record<string, unknown>)[name];
  });
  stubbed.length = 0;
}

// --- Lines, pages and a client the test answers by hand. Line `i` is stamped `i` seconds
// past 09:00 local, today. ---

function line(i: number, over: Partial<LogLine['stream']> = {}): LogLine {
  return {
    timestamp: new Date(2026, 9, 15, 9, 0, i).toISOString(),
    cursor: `c${i}`,
    message: `line ${i}`,
    truncatedBytes: 0,
    stream: { namespace: 'prod', pod: 'webapp', container: 'app', previous: false, ...over },
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

type Fetch = { query: FetchQuery; resolve: (p: Page) => void; reject: (error: unknown) => void };
type Follow = { onFrame: (frame: TailFrame) => void; onFailed: (error: unknown) => void; closed: boolean };

function scripted() {
  const fetches: Fetch[] = [];
  const follows: Follow[] = [];
  const refusal = { next: null as Error | null };
  const client: Client = {
    fetch: (query) =>
      new Promise<Page>((resolve, reject) => {
        fetches.push({ query, resolve, reject });
      }),
    tail: (_cursor, _seen, onFrame, onFailed) => {
      if (refusal.next) {
        const error = refusal.next;
        refusal.next = null;
        throw error;
      }
      const follow: Follow = { onFrame, onFailed, closed: false };
      follows.push(follow);
      return () => {
        follow.closed = true;
      };
    },
  };
  built.current = client;
  return { fetches, follows, refusal, last: () => fetches.at(-1)! };
}

// --- Mounting. ---

const source = (over: Partial<LogsViewAction['sources'][number]> = {}): LogsViewAction['sources'][number] => ({
  namespace: 'prod',
  kind: 'Deployment',
  name: 'webapp',
  containers: [],
  allContainers: false,
  previous: false,
  ...over,
});

const action = (over: Partial<LogsViewAction> = {}): LogsViewAction => ({
  sources: [source()],
  filters: [],
  grep: '',
  anchor: { kind: 'Tail', at: null },
  pinToEnd: true,
  resolvedAt: '2026-10-08T14:04:00Z',
  ...over,
});

// The viewer hands the app's urql client to `createLogsClient`, which the test replaces.
const urql = new UrqlClient({ url: 'tauri://graphql', exchanges: [] });

function tree(query: LogsViewAction, viewport: LogViewportHandle) {
  return (
    <StrictMode>
      <UrqlProvider value={urql}>
        <LogViewer
          clusterId="cluster-1"
          query={query}
          openAt={query.anchor}
          pinToEnd={query.pinToEnd}
          viewport={viewport}
          onGrepApplied={onGrepApplied}
        />
      </UrqlProvider>
    </StrictMode>
  );
}

function mount(query: LogsViewAction = action()) {
  const viewport = createLogViewportHandle();
  const view = render(tree(query, viewport));
  return { ...view, viewport };
}

const rowTexts = () => screen.queryAllByTestId('log-row').map((row) => row.textContent ?? '');

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
  await frame(2);
}

async function refuse(fetch: Fetch) {
  await act(async () => {
    fetch.reject(new Error('fetch refused'));
  });
  await frame(2);
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
  vi.setSystemTime(new Date(2026, 9, 15, 12, 0, 0));
  built.calls = [];
  reportError.mockReset();
  onGrepApplied.mockReset();
  stubLayout();
});

afterEach(() => {
  cleanup();
  localStorage.clear();
  unstubLayout();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('LogViewer', () => {
  it('opens at the tail with the last page drawn as rows', async () => {
    const s = scripted();
    mount();
    expect(built.calls[0]).toEqual([urql, 'cluster-1', action()]);
    expect(s.fetches[0].query).toEqual({ kind: 'Last', limit: 300 });

    await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
    expect(rowTexts().at(-1)).toContain('line 19');
    expect(rowTexts().at(-1)).toContain('09:00:19.000');
  });

  it('draws Loading over the header and the rows until the landing ends', async () => {
    const s = scripted();
    mount();
    expect(screen.getByText('Loading')).toBeInTheDocument();
    await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
    expect(screen.queryByText('Loading')).not.toBeInTheDocument();
  });

  it('lands around a time picked from the toolbar, unpinned', async () => {
    const s = scripted();
    const { viewport } = mount();
    await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
    fireEvent.click(screen.getByRole('button', { name: 'Jump to a time' }));
    fireEvent.change(screen.getByPlaceholderText('Timestamp'), { target: { value: line(5).timestamp } });
    fireEvent.keyDown(screen.getByPlaceholderText('Timestamp'), { key: 'Enter' });
    await frame();
    expect(s.last().query).toEqual({ kind: 'At', at: line(5).timestamp, limit: 300 });
    await answer(s.last(), page(0, 20, { anchor: 5 }));
    expect(viewport.get().pinnedToEnd).toBe(false);
  });

  it('opens at a moment with that line marked as where it landed', async () => {
    const s = scripted();
    const at = line(30).timestamp;
    mount(action({ anchor: { kind: 'At', at }, pinToEnd: false }));
    expect(s.fetches[0].query).toEqual({ kind: 'At', at, limit: 300 });

    await answer(s.fetches[0], page(20, 40, { anchor: 10 }));
    const landed = screen.getAllByTestId('log-row').find((row) => row.textContent?.includes('line 30'))!;
    expect(landed.querySelector('[data-col="Pos"]')).toHaveTextContent(/^0$/);
    expect(landed.querySelector('[data-col="Pos"]')).toHaveClass('border-l-2');
  });

  it('adds a row for a followed line and keeps the bottom while pinned', async () => {
    const s = scripted();
    const { container } = mount();
    await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
    expect(s.follows.filter((f) => !f.closed)).toHaveLength(1);

    await act(async () => {
      s.follows.find((f) => !f.closed)!.onFrame({ kind: 'Line', line: line(20) });
    });
    await frame(2);
    expect(rowTexts().at(-1)).toContain('line 20');
    const scroller = container.querySelector<HTMLElement>(SCROLLER)!;
    expect(scroller.scrollTop).toBe(scroller.scrollHeight - VIEW);
  });

  it('writes what is on screen into the viewport, with nothing selected', async () => {
    const s = scripted();
    const { viewport } = mount();
    await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));

    // Pinned to the end of 20 lines, the view shows the last it has room for.
    const shown = Math.ceil(VIEW / LOG_ROW_PX);
    expect(viewport.get()).toEqual({
      range: { from: line(20 - shown).timestamp, to: line(19).timestamp },
      atEnd: true,
      pinnedToEnd: true,
      paused: false,
      visible: shown,
      grep: '',
      selection: [],
      selectedCount: 0,
    });
  });

  it('empties the viewport on unmount', async () => {
    const s = scripted();
    const { viewport, unmount } = mount();
    await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));

    unmount();
    expect(viewport.get()).toEqual({
      range: null,
      atEnd: false,
      pinnedToEnd: false,
      paused: false,
      visible: 0,
      grep: null,
      selection: [],
      selectedCount: 0,
    });
  });

  describe('errors', () => {
    it('keeps the rows when a page fails and says so, until a page lands', async () => {
      const s = scripted();
      mount(action({ pinToEnd: false }));
      await answer(s.fetches[0], page(100, 120, { edges: END }));
      // Unpinned, it pages both ways at once.
      expect(s.fetches).toHaveLength(3);
      const [, before, after] = s.fetches;

      await refuse(before);
      expect(screen.getByText('Could not load more lines.')).toBeInTheDocument();
      expect(rowTexts().length).toBeGreaterThan(0);
      // The operation's error reached the bus through `errorReportExchange`, not the viewer.
      expect(reportError).not.toHaveBeenCalled();

      await answer(after, page(120, 120, { edges: { next: null, follow: 'on' } }));
      expect(screen.queryByText('Could not load more lines.')).not.toBeInTheDocument();
    });

    it('draws a refused follow’s reason, spelled out', async () => {
      const s = scripted();
      s.refusal.next = new Error('follow refused: pod\u202Egone');
      mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      const reason = screen.getByText(/follow refused: pod/);
      expect(reason.querySelector('mark')).not.toBeNull();
    });

    it('draws why a follow failed after it opened, and stops following', async () => {
      const s = scripted();
      const { viewport } = mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      const follow = s.follows.find((f) => !f.closed)!;
      await act(async () => {
        follow.onFailed(new Error('the cluster cache is gone'));
      });
      await frame(2);
      expect(screen.getByText(/the cluster cache is gone/)).toBeInTheDocument();
      expect(follow.closed).toBe(true);
      expect(viewport.get().pinnedToEnd).toBe(false);
    });

    it('draws why the follow ended', async () => {
      const s = scripted();
      mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      await act(async () => {
        s.follows.at(-1)!.onFrame({ kind: 'Ended', reason: 'the pod was deleted' });
      });
      await frame(2);
      expect(screen.getByText(/No new lines:/)).toHaveTextContent('No new lines: the pod was deleted');
    });

    it('offers Try again after a failed landing, which lands at the view’s anchor unpinned as it said', async () => {
      const s = scripted();
      const { viewport } = mount(action({ pinToEnd: false }));
      await refuse(s.fetches[0]);
      expect(screen.getByText('Could not load lines.')).toBeInTheDocument();
      expect(screen.queryByText('Could not load more lines.')).not.toBeInTheDocument();

      fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
      await frame();
      expect(s.last().query).toEqual({ kind: 'Last', limit: 300 });
      expect(screen.queryByText('Could not load more lines.')).not.toBeInTheDocument();
      expect(screen.queryByText('Could not load lines.')).not.toBeInTheDocument();

      await answer(s.last(), page(0, 20, { edges: { ...START, ...END } }));
      expect(rowTexts().length).toBeGreaterThan(0);
      expect(viewport.get().pinnedToEnd).toBe(false);
      expect(screen.queryByText('Could not load more lines.')).not.toBeInTheDocument();
    });

    it('says when a view has no lines', async () => {
      const s = scripted();
      mount();
      await answer(s.fetches[0], page(0, 0, { edges: { ...START, ...END } }));
      expect(screen.getByText('No lines.')).toBeInTheDocument();
    });
  });

  describe('edge rows', () => {
    it('say Loading… only while a page that way is in flight', async () => {
      const s = scripted();
      mount(action({ pinToEnd: false }));
      await answer(s.fetches[0], page(100, 120, { edges: END }));
      expect(screen.getAllByText('Loading…')).toHaveLength(2);

      // Behind, the after edge still has more, and says nothing with no page asked.
      await answer(
        s.fetches.find((f) => f.query.kind === 'Continue' && f.query.seen !== null)!,
        page(120, 120, { edges: { next: null, follow: 'on' } }),
      );
      expect(screen.getAllByText('Loading…')).toHaveLength(1);
    });

    it('offer Keep looking after an empty page, which pages that way', async () => {
      const s = scripted();
      mount(action({ pinToEnd: false }));
      await answer(s.fetches[0], page(100, 120, { edges: END }));
      await answer(
        s.fetches.find((f) => f.query.kind === 'Continue' && f.query.seen === null)!,
        page(100, 100),
      );
      expect(screen.getByText('No more in the last read')).toBeInTheDocument();

      const asked = s.fetches.length;
      fireEvent.click(screen.getByRole('button', { name: 'Keep looking' }));
      expect(s.fetches.length).toBe(asked + 1);
      expect(s.last().query).toMatchObject({ kind: 'Continue', cursor: 'before-100', seen: null });
    });
  });

  it('draws what a page could not do, its code in a title', async () => {
    const s = scripted();
    mount();
    const notices = [{ code: 'stream-skipped', message: 'prod/db-0/app: forbidden' }];
    await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END }, notices }));
    expect(screen.getByText('prod/db-0/app: forbidden').closest('[title]')).toHaveAttribute('title', 'stream-skipped');
  });

  it('draws the pod and the container as separate cells once both are picked', async () => {
    localStorage.setItem('kubetail:log-viewer-columns', '["Pod","Container"]');
    const s = scripted();
    mount(action({ sources: [source({ allContainers: true })] }));
    await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
    const last = screen.getAllByTestId('log-row').at(-1)!;
    expect(last.querySelector('[data-col="Pod"]')).toHaveTextContent(/^webapp$/);
    expect(last.querySelector('[data-col="Container"]')).toHaveTextContent(/^app$/);
  });

  describe('the toolbar', () => {
    const press = (name: string) => fireEvent.click(screen.getByRole('button', { name }));
    const open = (s: ReturnType<typeof scripted>) => s.follows.filter((f) => !f.closed);

    it('is disabled while the view loads', () => {
      scripted();
      mount();
      expect(screen.getByRole('button', { name: 'Jump to the start' })).toBeDisabled();
    });

    it('jumps to the start, and to the end, which pins', async () => {
      const s = scripted();
      const { viewport } = mount(action({ pinToEnd: false }));
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));

      press('Jump to the start');
      await frame();
      expect(s.last().query).toEqual({ kind: 'First', limit: 300 });
      await answer(s.last(), page(0, 20, { edges: { ...START, ...END } }));
      expect(viewport.get().pinnedToEnd).toBe(false);

      press('Jump to the end');
      await frame();
      expect(s.last().query).toEqual({ kind: 'Last', limit: 300 });
      await answer(s.last(), page(0, 20, { edges: { ...START, ...END } }));
      expect(viewport.get().pinnedToEnd).toBe(true);
    });

    it('pauses at the bottom with no lines arriving, and plays again from there', async () => {
      const s = scripted();
      const { viewport } = mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      expect(open(s)).toHaveLength(1);

      press('Pause');
      await frame();
      expect(open(s)).toHaveLength(0);
      expect(rowTexts().at(-1)).toContain('line 19');
      expect(viewport.get().paused).toBe(true);

      press('Play');
      await frame();
      expect(open(s)).toHaveLength(1);
      expect(viewport.get().paused).toBe(false);
      await act(async () => {
        open(s)[0].onFrame({ kind: 'Line', line: line(20) });
      });
      await frame(2);
      expect(rowTexts().at(-1)).toContain('line 20');
    });

    it('keeps the columns and wrap the user picks for the next window', async () => {
      const s = scripted();
      mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      press('View');
      await frame(2);
      fireEvent.click(screen.getByRole('checkbox', { name: 'Pod' }));
      fireEvent.click(screen.getByRole('checkbox', { name: 'Wrap' }));
      await frame();
      expect(screen.getAllByTestId('log-row').at(-1)!.querySelector('[data-col="Pod"]')).not.toBeNull();
      expect(localStorage.getItem('kubetail:log-viewer-columns')).toBe('["Timestamp","Dot","Pod"]');
      expect(localStorage.getItem('kubetail:log-viewer-wrap')).toBe('true');
    });
  });

  describe('the grep box', () => {
    const grepFor = async (pattern: string) => {
      const box = screen.getByRole('textbox', { name: 'Grep' });
      fireEvent.change(box, { target: { value: pattern } });
      fireEvent.keyDown(box, { key: 'Enter' });
      await frame();
    };
    const open = (s: ReturnType<typeof scripted>) => s.follows.filter((f) => !f.closed);

    it('checks a new grep with a one-line read, then lands at the end through it, following', async () => {
      const s = scripted();
      const { viewport } = mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      expect(open(s)).toHaveLength(1);

      await grepFor('line 1.');
      expect(built.calls.at(-1)).toEqual([urql, 'cluster-1', action({ grep: 'line 1.' })]);
      expect(s.last().query).toEqual({ kind: 'Last', limit: 1 });
      // The last good view stays until the grep is accepted.
      expect(rowTexts().at(-1)).toContain('line 19');

      expect(onGrepApplied).not.toHaveBeenCalled();
      expect(viewport.get().grep).toBe('');

      await answer(s.last(), page(19, 20, { edges: END }));
      expect(onGrepApplied).toHaveBeenCalledWith('line 1.');
      expect(viewport.get().grep).toBe('line 1.');
      expect(s.last().query).toEqual({ kind: 'Last', limit: 300 });
      await answer(s.last(), page(10, 20, { edges: { ...START, ...END } }));
      expect(open(s)).toHaveLength(1);
      await act(async () => {
        open(s)[0].onFrame({ kind: 'Line', line: line(20) });
      });
      await frame(2);
      expect(rowTexts().at(-1)).toContain('line 20');
    });

    it('lands at the first line on screen when not pinned, unpinned', async () => {
      const s = scripted();
      const { container, viewport } = mount(
        action({ anchor: { kind: 'At', at: line(30).timestamp }, pinToEnd: false }),
      );
      await answer(s.fetches[0], page(20, 40, { anchor: 10, edges: START }));
      // The user scrolls away from where the view landed.
      const scroller = container.querySelector<HTMLElement>(SCROLLER)!;
      fireEvent.wheel(scroller);
      act(() => {
        scroller.scrollTop -= 5 * LOG_ROW_PX;
      });
      await frame();
      const { from } = viewport.get().range!;
      expect(from).not.toBe(line(30).timestamp);

      await grepFor('line');
      await answer(s.last(), page(39, 40));
      expect(s.last().query).toEqual({ kind: 'At', at: from, limit: 300 });
      await answer(s.last(), page(20, 40, { anchor: 0, edges: START }));
      expect(viewport.get().pinnedToEnd).toBe(false);
    });

    it('starts with the view’s grep, and an empty box clears it', async () => {
      const s = scripted();
      mount(action({ grep: 'line 1.' }));
      await answer(s.fetches[0], page(18, 20, { edges: { ...START, ...END } }));
      expect(screen.getByRole('textbox', { name: 'Grep' })).toHaveValue('line 1.');

      await grepFor('');
      expect(built.calls.at(-1)).toEqual([urql, 'cluster-1', action({ grep: '' })]);
      await answer(s.last(), page(19, 20, { edges: END }));
      expect(s.last().query).toEqual({ kind: 'Last', limit: 300 });
      await answer(s.last(), page(15, 20, { edges: { ...START, ...END } }));
      expect(rowTexts()[0]).toContain('line 15');
    });

    it('says why a grep was refused at the box and keeps the last good view, following', async () => {
      const s = scripted();
      mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      const follow = open(s)[0];

      await grepFor('(a');
      const asked = s.fetches.length;
      await act(async () => {
        s.last().reject(new Error('error parsing regexp: missing closing ): `(a`'));
      });
      await frame(2);
      const box = screen.getByRole('textbox', { name: 'Grep' });
      expect(box).toHaveAttribute('aria-invalid', 'true');
      expect(box).toHaveAccessibleDescription('error parsing regexp: missing closing ): `(a`');
      expect(box).toHaveValue('(a');
      expect(screen.queryByText('Could not load lines.')).not.toBeInTheDocument();
      expect(s.fetches).toHaveLength(asked);
      expect(rowTexts().at(-1)).toContain('line 19');
      expect(follow.closed).toBe(false);
      expect(onGrepApplied).not.toHaveBeenCalled();

      await grepFor('(a)');
      await answer(s.last(), page(19, 20, { edges: END }));
      expect(box).toHaveAttribute('aria-invalid', 'false');
    });

    it('drops a grep answered after the viewer closed, leaving the viewport empty', async () => {
      const s = scripted();
      const { viewport, unmount } = mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      await grepFor('line 1.');
      const probe = s.last();

      unmount();
      await act(async () => {
        probe.resolve(page(19, 20, { edges: END }));
      });
      expect(viewport.get()).toEqual(EMPTY_LOG_VIEWPORT);
      expect(onGrepApplied).not.toHaveBeenCalled();
    });

    it('neither reads nor lands for the grep in effect, and clears a refusal and a pending grep', async () => {
      const s = scripted();
      mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      const box = screen.getByRole('textbox', { name: 'Grep' });
      const asked = s.fetches.length;
      await grepFor('');
      expect(s.fetches).toHaveLength(asked);

      await grepFor('(a');
      await refuse(s.last());
      expect(box).toHaveAttribute('aria-invalid', 'true');
      await grepFor('b');
      const pending = s.last();
      await grepFor('');
      expect(box).not.toHaveAttribute('aria-invalid', 'true');

      await answer(pending, page(19, 20, { edges: END }));
      expect(s.last()).toBe(pending);
      expect(onGrepApplied).not.toHaveBeenCalled();
    });

    it('never writes the old lines or selection into the viewport under the new grep', async () => {
      const s = scripted();
      const { viewport } = mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      const pos = screen.getAllByTestId('log-row').at(-1)!.querySelector<HTMLElement>('[data-col="Pos"]')!;
      fireEvent.mouseDown(pos);
      fireEvent.mouseUp(document);
      expect(viewport.get().selection).toHaveLength(1);

      const written: LogViewport[] = [];
      viewport.subscribe(() => written.push(viewport.get()));
      await grepFor('line 1..');
      await answer(s.last(), page(119, 120, { edges: END }));
      await answer(s.last(), page(100, 120, { edges: { ...START, ...END } }));

      const underNew = written.filter((w) => w.grep === 'line 1..');
      expect(underNew.length).toBeGreaterThan(0);
      underNew.forEach((w) => {
        expect(w.selection).toEqual([]);
        expect(!w.range || w.range.from >= line(100).timestamp).toBe(true);
      });
    });

    it('keeps a pending grep across a new viewport handle', async () => {
      const s = scripted();
      const { rerender } = mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      await grepFor('line 1.');
      const probe = s.last();

      const next = createLogViewportHandle();
      rerender(tree(action(), next));
      await answer(probe, page(19, 20, { edges: END }));
      expect(onGrepApplied).toHaveBeenCalledWith('line 1.');
      expect(next.get().grep).toBe('line 1.');
    });

    it('acts on the last grep submitted alone', async () => {
      const s = scripted();
      mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      await grepFor('a');
      const a = s.last();
      await grepFor('b');
      const b = s.last();
      await grepFor('(c');
      const c = s.last();
      const box = screen.getByRole('textbox', { name: 'Grep' });

      await answer(a, page(19, 20, { edges: END }));
      expect(s.last()).toBe(c);
      await refuse(b);
      expect(box).toHaveAttribute('aria-invalid', 'false');
      await act(async () => {
        c.reject(new Error('missing closing )'));
      });
      expect(box).toHaveAccessibleDescription('missing closing )');
    });
  });

  describe('wrap', () => {
    // A wrapped row measures by its box; the long one is three lines tall here.
    const rowHeight = (el: Element) => (el.textContent?.includes('long') ? 60 : 20);

    beforeEach(() => {
      stub('getBoundingClientRect', {
        value: function rect(this: HTMLElement) {
          return { height: this.dataset.testid === 'log-row' ? rowHeight(this) : 0, width: 0 } as DOMRect;
        },
      });
      // A browser's observer reports a size a frame after it starts watching, which is when
      // the virtualizer takes a row it skipped while its own scroll settled.
      vi.stubGlobal(
        'ResizeObserver',
        class {
          constructor(private readonly callback: ResizeObserverCallback) {}

          observe(target: Element) {
            requestAnimationFrame(() => this.callback([{ target } as ResizeObserverEntry], this));
          }

          unobserve() {}

          disconnect() {}
        },
      );
    });

    const withLong = () => ({
      ...page(0, 20, { edges: { ...START, ...END } }),
      lines: lines(0, 20).map((l, i) => (i === 18 ? { ...l, message: `long ${'x'.repeat(300)}` } : l)),
    });
    const topOf = (text: string) =>
      screen.getAllByTestId('log-row').find((row) => row.textContent?.includes(text))!.style.transform;

    it('lets a long message’s row grow, and the next row starts below it', async () => {
      localStorage.setItem('kubetail:log-viewer-wrap', 'true');
      const s = scripted();
      mount();
      await answer(s.fetches[0], withLong());
      const start = Number(/translateY\((\d+)px\)/.exec(topOf('long'))![1]);
      expect(topOf('line 19')).toBe(`translateY(${start + 60}px)`);
      expect(screen.getAllByTestId('log-row')[0].querySelector('[data-col="Message"]')).toHaveClass(
        'whitespace-pre-wrap',
      );
    });

    it('keeps every row one line tall with wrap off', async () => {
      const s = scripted();
      mount();
      await answer(s.fetches[0], withLong());
      const start = Number(/translateY\((\d+)px\)/.exec(topOf('long'))![1]);
      expect(topOf('line 19')).toBe(`translateY(${start + LOG_ROW_PX}px)`);
    });
  });

  it('names the columns over the rows', async () => {
    const s = scripted();
    mount();
    await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
    expect(screen.getByTestId('log-header')).toHaveTextContent('PositionTimestampSourceMessage');
  });

  it('draws the default columns, with no pod', async () => {
    const s = scripted();
    mount();
    await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
    const last = screen.getAllByTestId('log-row').at(-1)!;
    expect([...last.querySelectorAll<HTMLElement>('[data-col]')].map((el) => el.dataset.col)).toEqual([
      'Pos',
      'Timestamp',
      'Dot',
      'Message',
    ]);
  });

  describe('selection', () => {
    const rowOf = (text: string, root: ParentNode = document) =>
      [...root.querySelectorAll<HTMLElement>('[data-testid="log-row"]')].find((row) =>
        row.textContent?.includes(text),
      )!;
    const posOf = (text: string, root?: ParentNode) =>
      rowOf(text, root).querySelector<HTMLElement>('[data-col="Pos"]')!;
    const cellOf = (text: string, col: string) => rowOf(text).querySelector<HTMLElement>(`[data-col="${col}"]`)!;
    const scrollerIn = (root: ParentNode) => root.querySelector<HTMLElement>(SCROLLER)!;
    const bands = () => document.querySelectorAll<HTMLElement>('[data-selection-band]');

    beforeEach(() => {
      Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    });

    async function landed() {
      const s = scripted();
      const view = mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      return { s, ...view };
    }

    function selectRows(from: string, to: string) {
      fireEvent.mouseDown(posOf(from));
      fireEvent.mouseUp(document);
      fireEvent.mouseDown(posOf(to), { shiftKey: true });
    }

    it('paints one band behind a run of selected rows, and outlines its ends', async () => {
      await landed();
      selectRows('line 17', 'line 19');
      expect(bands()).toHaveLength(1);
      expect(bands()[0].style.height).toBe(`${3 * LOG_ROW_PX}px`);
      expect(rowOf('line 17')).toHaveAttribute('aria-selected', 'true');
      expect(rowOf('line 17').style.boxShadow).toBe('inset 0 1px 0 0 var(--ring)');
      expect(rowOf('line 19').style.boxShadow).toBe('inset 0 -1px 0 0 var(--ring)');
    });

    it('rings a selected cell', async () => {
      await landed();
      fireEvent.mouseDown(cellOf('line 18', 'Message'));
      fireEvent.mouseUp(document);
      expect(rowOf('line 18').querySelector('[data-overlay-col="Message"]')).not.toBeNull();
    });

    it("rings a run of cells under a hole's notice, not through it", async () => {
      const s = scripted();
      mount();
      const gap = { after: line(10).timestamp, before: line(18).timestamp };
      const landing = page(0, 20, { edges: { ...START, ...END } });
      landing.lines[18] = { ...landing.lines[18], gap };
      await answer(s.fetches[0], landing);
      fireEvent.mouseDown(cellOf('line 17', 'Message'));
      fireEvent.mouseUp(document);
      fireEvent.mouseDown(cellOf('line 19', 'Message'), { shiftKey: true });
      const ring = (msg: string) => rowOf(msg).querySelector<HTMLElement>('[data-overlay-col="Message"]')!;
      expect(ring('line 17').style.borderBottomWidth).toBe('1px');
      expect(ring('line 18').style.borderTopWidth).toBe('1px');
      expect(ring('line 18').parentElement!.style.top).toBe(`${LOG_ROW_PX}px`);
      expect(ring('line 18').style.borderBottomWidth).toBe('0px');
      expect(ring('line 19').style.borderTopWidth).toBe('0px');
    });

    it('copies the selected rows on Cmd+C as tab-separated lines', async () => {
      const { container } = await landed();
      selectRows('line 18', 'line 19');
      fireEvent.keyDown(scrollerIn(container), { key: 'c', metaKey: true });
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('09:00:18.000\tline 18\n09:00:19.000\tline 19');
    });

    it('reports a refused copy to the error bus', async () => {
      vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error('denied'));
      const { container } = await landed();
      selectRows('line 19', 'line 19');
      fireEvent.keyDown(scrollerIn(container), { key: 'c', metaKey: true });
      await act(async () => {});
      expect(reportError).toHaveBeenCalledWith(expect.objectContaining({ message: 'clipboard: write: denied' }));
    });

    it('puts the selected lines in the viewport, in order, and empties it on Escape', async () => {
      const { container, viewport } = await landed();
      fireEvent.mouseDown(posOf('line 19'));
      fireEvent.mouseUp(document);
      fireEvent.mouseDown(posOf('line 16'), { metaKey: true });
      expect(viewport.get().selection).toEqual([line(16), line(19)]);
      expect(viewport.get().pinnedToEnd).toBe(true);

      fireEvent.keyDown(scrollerIn(container), { key: 'Escape' });
      expect(viewport.get().selection).toEqual([]);
    });

    it("puts a block of cells' lines in the viewport", async () => {
      const { viewport } = await landed();
      fireEvent.mouseDown(cellOf('line 17', 'Message'));
      fireEvent.mouseUp(document);
      fireEvent.mouseDown(cellOf('line 18', 'Timestamp'), { shiftKey: true });
      expect(viewport.get().selection).toEqual([line(17), line(18)]);
    });

    it('clears the selection with a jump, and the viewport with it', async () => {
      const { s, viewport } = await landed();
      selectRows('line 18', 'line 19');
      expect(viewport.get().selection).toHaveLength(2);

      fireEvent.click(screen.getByRole('button', { name: 'Jump to the end' }));
      await frame();
      await answer(s.last(), page(0, 20, { edges: { ...START, ...END } }));
      expect(bands()).toHaveLength(0);
      expect(viewport.get().selection).toEqual([]);
    });

    it('copies only the viewer the key reached', async () => {
      const s = scripted();
      const a = mount();
      const b = mount();
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      await answer(s.fetches[1], page(0, 20, { edges: { ...START, ...END } }));

      fireEvent.mouseDown(posOf('line 19', a.container));
      fireEvent.mouseUp(document);
      fireEvent.mouseDown(posOf('line 18', b.container));
      fireEvent.mouseUp(document);

      fireEvent.keyDown(scrollerIn(a.container), { key: 'c', metaKey: true });
      expect(navigator.clipboard.writeText).toHaveBeenCalledTimes(1);
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('09:00:19.000\tline 19');
      expect(b.viewport.get().selection).toEqual([line(18)]);
    });

    it('keeps the cursor in view as ↑ moves it above the screen, and stops following', async () => {
      const { container, viewport } = await landed();
      const scroller = scrollerIn(container);
      // Pinned to the end of 20 rows, the screen starts partway into line 11.
      expect(scroller.scrollTop).toBe(20 * LOG_ROW_PX - VIEW);
      fireEvent.mouseDown(cellOf('line 12', 'Message'));
      fireEvent.mouseUp(document);

      fireEvent.keyDown(scroller, { key: 'ArrowUp' });
      await frame();
      expect(scroller.scrollTop).toBe(11 * LOG_ROW_PX);
      expect(viewport.get().pinnedToEnd).toBe(false);
      fireEvent.keyDown(scroller, { key: 'ArrowUp' });
      await frame();
      expect(scroller.scrollTop).toBe(10 * LOG_ROW_PX);
      expect(viewport.get().selection).toEqual([line(10)]);
    });

    it('keeps the cursor in view as ↓ moves it below the screen, and leaves a row in view alone', async () => {
      const s = scripted();
      const { container } = mount(action({ anchor: { kind: 'Head', at: null }, pinToEnd: false }));
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      const scroller = scrollerIn(container);
      expect(scroller.scrollTop).toBe(0);
      fireEvent.mouseDown(cellOf('line 6', 'Message'));
      fireEvent.mouseUp(document);

      // Line 7 ends 8px above the screen's bottom.
      fireEvent.keyDown(scroller, { key: 'ArrowDown' });
      await frame();
      expect(scroller.scrollTop).toBe(0);
      // Line 8 runs past it, so the screen moves just enough to show it whole.
      fireEvent.keyDown(scroller, { key: 'ArrowDown' });
      await frame();
      expect(scroller.scrollTop).toBe(9 * LOG_ROW_PX - VIEW);
    });

    it('keeps taking arrow keys after the clicked cell scrolls out of the rendered rows', async () => {
      const s = scripted();
      const { viewport } = mount(action({ anchor: { kind: 'Head', at: null }, pinToEnd: false }));
      await answer(s.fetches[0], page(0, 20, { edges: { ...START, ...END } }));
      const cell = cellOf('line 6', 'Message');
      fireEvent.mouseDown(cell);
      fireEvent.mouseUp(document);
      cell.focus();

      // The keys go where the browser sends them: the focused element.
      for (let i = 0; i < 13; i += 1) {
        fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' });
        // oxlint-disable-next-line no-await-in-loop -- each key scrolls in its own frame
        await frame();
      }
      expect(cell.isConnected).toBe(false);
      expect(viewport.get().selection).toEqual([line(19)]);
      fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' });
      await frame();
      expect(viewport.get().selection).toEqual([line(18)]);
    });

    it('selects with Space on a position cell without reading it as a scroll to the bottom', async () => {
      const s = scripted();
      const { viewport } = mount(action({ anchor: { kind: 'Head', at: null }, pinToEnd: false }));
      await answer(s.fetches[0], page(0, 5, { edges: { ...START, ...END } }));
      // A short Head landing sits at the bottom, unpinned, paging after.
      expect(s.last().query).toMatchObject({ kind: 'Continue', cursor: 'live-end' });

      fireEvent.keyDown(posOf('line 4'), { key: ' ' });
      await answer(s.last(), page(5, 5, { edges: { prev: 'live-end', ...END } }));
      expect(viewport.get().selection).toEqual([line(4)]);
      expect(viewport.get().pinnedToEnd).toBe(false);
    });
  });
});
