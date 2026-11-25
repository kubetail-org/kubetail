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

import { act, renderHook, waitFor } from '@testing-library/react';
import { createElement } from 'react';
import type { ReactNode } from 'react';
import { Client, gql, Provider } from 'urql';
import type { Exchange, OperationContext, OperationResult } from 'urql';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { filter, pipe, tap } from 'wonka';
import type { Source } from 'wonka';

import { flushSubscriptionsSynchronously, mockTauriCore } from '@/test-utils';

// End-to-end wiring: a real urql Client running the real subscribe-exchange over
// a mocked Tauri channel, so the hook, the exchange, and the transport-status
// registry all share one module graph. (The exchange's own unit test mocks the
// registry; this test exercises the integration the mock stands in for.)
const { invokeMock, channels, liveChannel, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());
vi.mock('../error-bus', () => ({
  reportError: () => {},
  errorMessage: (err: unknown) => String(err),
}));

const { tauriSubscriptionExchange } = await import('./subscribe-exchange');
const { scheduleFlush, setSubscriptionFlushScheduler, useFoldedSubscription, subscriptionPhase } =
  await import('./use-folded-subscription');

const TICK = gql`
  subscription Tick {
    tick
  }
`;

const NEXT = (n: number) => JSON.stringify({ type: 'next', payload: { data: { tick: n } } });
const OPEN = JSON.stringify({ type: 'open' });

// Append each tick — a stand-in for a delta reducer whose accumulation must be
// thrown away and rebuilt from scratch on a reconnect.
const appendTick = (prev: number[] | undefined, frames: { tick: number }[]) => [
  ...(prev ?? []),
  ...frames.map((f) => f.tick),
];

// Drive the live channel, wrapped in act so React flushes the resulting renders.
const emit = (raw: string) => act(async () => liveChannel().onmessage!(raw));

// A fresh urql Client (running the real exchange) wrapped as a renderHook provider.
function makeWrapper() {
  const client = new Client({ url: 'tauri://graphql', exchanges: [tauriSubscriptionExchange] });
  return ({ children }: { children: ReactNode }) => createElement(Provider, { value: client }, children);
}

function renderWatch(wrapper = makeWrapper(), opts?: { unshared?: boolean }) {
  return renderHook(
    () => useFoldedSubscription<{ tick: number }, number[]>({ query: TICK, variables: {} }, appendTick, opts),
    { wrapper },
  );
}

const subscribeCount = () => invokeMock.mock.calls.filter(([cmd]) => cmd === 'graphql_subscribe').length;

// Drive one specific connection, by the order its subscription was opened.
const emitOn = (idx: number, raw: string) => act(async () => channels[idx].onmessage!(raw));

const state = (r: { result: { current: { data?: number[] | undefined; connected: boolean; generation: number } } }) =>
  r.result.current;

describe('useFoldedSubscription', () => {
  beforeEach(() => {
    flushSubscriptionsSynchronously();
    invokeMock.mockReset();
    channels.length = 0;
    let id = 0;
    invokeMock.mockImplementation(async (cmd: string) => {
      if (cmd === 'graphql_subscribe') {
        id += 1;
        return id;
      }
      return undefined;
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('shares one connection between consumers of the same watch', async () => {
    const wrapper = makeWrapper();
    renderWatch(wrapper);
    renderWatch(wrapper);

    // urql keys on query + variables, so the second consumer rides the first's stream.
    await waitFor(() => expect(subscribeCount()).toBe(1));
  });

  it('gives an unshared consumer its own connection', async () => {
    const wrapper = makeWrapper();
    renderWatch(wrapper, { unshared: true });
    renderWatch(wrapper, { unshared: true });

    await waitFor(() => expect(subscribeCount()).toBe(2));
  });

  it('lists again for an unshared consumer that remounts beside a live one', async () => {
    // A popover closing and reopening while the page keeps the same watch: a shared
    // stream would hand the remounted consumer no snapshot, leaving it to fold later
    // deltas onto nothing.
    const wrapper = makeWrapper();
    const page = renderWatch(wrapper, { unshared: true });
    const popover = renderWatch(wrapper, { unshared: true });
    await waitFor(() => expect(channels.length).toBe(2));

    await emitOn(0, OPEN);
    await emitOn(0, NEXT(1));
    await emitOn(1, OPEN);
    await emitOn(1, NEXT(1));
    expect(state(popover).data).toEqual([1]);

    popover.unmount();
    const reopened = renderWatch(wrapper, { unshared: true });
    await waitFor(() => expect(channels.length).toBe(3));

    // Its own connection, so the server lists from cold into it.
    await emitOn(2, OPEN);
    await emitOn(2, NEXT(1));
    expect(state(reopened).data).toEqual([1]);
    expect(state(page).data).toEqual([1]);
  });

  it('is disconnected with no data before the connection opens', async () => {
    const r = renderWatch();
    await waitFor(() => expect(channels.length).toBeGreaterThan(0)); // the exchange dialed
    expect(state(r).data).toBeUndefined();
    expect(state(r).connected).toBe(false);
  });

  it('reports connected with undefined data on an empty snapshot (gap 1)', async () => {
    const r = renderWatch();
    await waitFor(() => expect(channels.length).toBeGreaterThan(0));

    await emit(OPEN); // connection up, but the snapshot replays nothing
    expect(state(r).connected).toBe(true);
    expect(state(r).data).toBeUndefined();
  });

  it('accumulates delta frames once connected', async () => {
    const r = renderWatch();
    await waitFor(() => expect(channels.length).toBeGreaterThan(0));

    await emit(OPEN);
    await emit(NEXT(1));
    await emit(NEXT(2));
    expect(state(r).data).toEqual([1, 2]);
    expect(state(r).connected).toBe(true);
  });

  it('keeps last-known data but flips connected false on a drop (gap 2)', async () => {
    vi.useFakeTimers();
    const r = renderWatch();
    await vi.waitFor(() => expect(channels.length).toBeGreaterThan(0));

    await emit(OPEN);
    await emit(NEXT(1));
    await emit(NEXT(2));

    await emit(JSON.stringify({ type: 'complete' })); // transport drops
    expect(state(r).connected).toBe(false);
    expect(state(r).data).toEqual([1, 2]); // still shown through the outage
  });

  // The sidecar ends a subscription whose watch died with one errors-only frame
  // (SubscriptionFailureExtension) before the transport completes. That is a drop with a
  // reason: the rows already sent are still the last thing that was true, so they
  // must survive it exactly as they survive a transport drop, and must not be
  // folded a second time — the reconnect that follows brings a fresh snapshot.
  it('keeps last-known data once, when a watch reports its terminal error', async () => {
    vi.useFakeTimers();
    const r = renderWatch();
    await vi.waitFor(() => expect(channels.length).toBeGreaterThan(0));

    await emit(OPEN);
    await emit(NEXT(1));
    await emit(NEXT(2));

    await emit(
      JSON.stringify({
        type: 'next',
        payload: {
          errors: [{ message: 'Cluster watch ended: watch too old' }],
          data: null,
          extensions: { subscriptionFailed: true },
        },
      }),
    );
    expect(state(r).data).toEqual([1, 2]);
    expect(state(r).connected).toBe(false);
  });

  it('rebuilds from scratch on reconnect — prior-connection state cannot linger', async () => {
    vi.useFakeTimers();
    const r = renderWatch();
    await vi.waitFor(() => expect(channels.length).toBeGreaterThan(0));

    await emit(OPEN);
    await emit(NEXT(1));
    await emit(NEXT(2));
    expect(state(r).data).toEqual([1, 2]);
    const first = state(r).generation;

    await emit(JSON.stringify({ type: 'complete' }));
    await vi.advanceTimersByTimeAsync(1_000); // backoff → reconnect dials
    await vi.waitFor(() => expect(channels.length).toBe(2));

    await emit(OPEN); // reconnect opens: masks stale data until the replay folds
    expect(state(r).connected).toBe(true);
    expect(state(r).data).toBeUndefined();
    expect(state(r).generation).toBeGreaterThan(first);

    await emit(NEXT(7)); // replayed snapshot has only object 7
    expect(state(r).data).toEqual([7]); // [1,2] are gone, not [1,2,7]
  });

  it('resets to empty on a reconnect whose replayed snapshot is empty', async () => {
    vi.useFakeTimers();
    const r = renderWatch();
    await vi.waitFor(() => expect(channels.length).toBeGreaterThan(0));

    await emit(OPEN);
    await emit(NEXT(1));
    expect(state(r).data).toEqual([1]);

    await emit(JSON.stringify({ type: 'complete' }));
    await vi.advanceTimersByTimeAsync(1_000);
    await vi.waitFor(() => expect(channels.length).toBe(2));

    await emit(OPEN); // everything was deleted during the outage
    expect(state(r).connected).toBe(true);
    expect(state(r).data).toBeUndefined();
  });

  // A variables change is a new operation: its first frame folds onto nothing, and the
  // old operation's data never shows through — e.g. chat, where each request changes
  // `variables`, must not append a later response's first chunk to the previous one.
  it("does not carry a prior operation's accumulator when variables change (P1)", async () => {
    const r = renderHook(
      ({ n }: { n: number }) =>
        useFoldedSubscription<{ tick: number }, number[]>({ query: TICK, variables: { n } }, appendTick),
      { wrapper: makeWrapper(), initialProps: { n: 1 } },
    );
    await waitFor(() => expect(channels.length).toBeGreaterThan(0));
    await emit(OPEN);
    await emit(NEXT(1));
    await emit(NEXT(2));
    expect(state(r).data).toEqual([1, 2]);

    act(() => r.rerender({ n: 2 })); // new variables ⇒ urql starts a new operation
    await waitFor(() => expect(channels.length).toBe(2));

    await emit(OPEN); // the new operation's connection opens
    expect(state(r).connected).toBe(true);
    expect(state(r).data).toBeUndefined(); // the old [1,2] must not show through

    await emit(NEXT(9));
    expect(state(r).data).toEqual([9]); // fresh — not [1, 2, 9]
  });

  // The silent case: a replacement operation that never emits. Nothing after the rerender
  // is allowed to be what clears the old rows.
  it('drops its data as soon as variables change, before the new operation emits', async () => {
    const r = renderHook(
      ({ n }: { n: number }) =>
        useFoldedSubscription<{ tick: number }, number[]>({ query: TICK, variables: { n } }, appendTick),
      { wrapper: makeWrapper(), initialProps: { n: 1 } },
    );
    await waitFor(() => expect(channels.length).toBeGreaterThan(0));
    await emit(OPEN);
    await emit(NEXT(1));
    expect(state(r).data).toEqual([1]);

    act(() => r.rerender({ n: 2 }));
    expect(state(r).data).toBeUndefined();
  });

  it('drops its data as soon as it pauses', async () => {
    const r = renderHook(
      ({ paused }: { paused: boolean }) =>
        useFoldedSubscription<{ tick: number }, number[]>({ query: TICK, variables: {}, pause: paused }, appendTick),
      { wrapper: makeWrapper(), initialProps: { paused: false } },
    );
    await waitFor(() => expect(channels.length).toBeGreaterThan(0));
    await emit(OPEN);
    await emit(NEXT(1));
    expect(state(r).data).toEqual([1]);

    act(() => r.rerender({ paused: true }));
    expect(state(r).data).toBeUndefined();
  });

  // Pausing tears the subscription down; re-executing the same op key starts clean.
  it('does not reuse a retained accumulator when the same operation pauses and reopens (P2)', async () => {
    const r = renderHook(
      ({ paused }: { paused: boolean }) =>
        useFoldedSubscription<{ tick: number }, number[]>({ query: TICK, variables: {}, pause: paused }, appendTick),
      { wrapper: makeWrapper(), initialProps: { paused: false } },
    );
    await waitFor(() => expect(channels.length).toBeGreaterThan(0));
    await emit(OPEN);
    await emit(NEXT(1));
    await emit(NEXT(2));
    expect(state(r).data).toEqual([1, 2]);

    act(() => r.rerender({ paused: true })); // teardown (urql retains the accumulator)
    act(() => r.rerender({ paused: false })); // re-execute the same op key
    await waitFor(() => expect(channels.length).toBe(2));

    await emit(OPEN);
    expect(state(r).connected).toBe(true);
    expect(state(r).data).toBeUndefined(); // the retained [1,2] must not survive

    await emit(NEXT(9));
    expect(state(r).data).toEqual([9]); // fresh — not [1, 2, 9]
  });

  // Frames arrive one per IPC task, so a snapshot of N objects is N frames. The hook folds
  // each as it lands but publishes once per flush, so consumers render (and re-sort) once.
  describe('batching', () => {
    // Capture the scheduled flush instead of running it.
    let flush: (() => void) | undefined;
    beforeEach(() => {
      flush = undefined;
      setSubscriptionFlushScheduler((fn) => {
        flush = fn;
        return () => {
          flush = undefined;
        };
      });
    });

    it('publishes the frames received since the last flush in one render', async () => {
      const renders = vi.fn();
      const r = renderHook(
        () => {
          renders();
          return useFoldedSubscription<{ tick: number }, number[]>({ query: TICK, variables: {} }, appendTick);
        },
        { wrapper: makeWrapper() },
      );
      await waitFor(() => expect(channels.length).toBeGreaterThan(0));
      await emit(OPEN);
      renders.mockClear();

      await emit(NEXT(1));
      await emit(NEXT(2));
      await emit(NEXT(3));
      expect(renders).not.toHaveBeenCalled();
      expect(state(r).data).toBeUndefined();

      act(() => flush!());
      expect(renders).toHaveBeenCalledTimes(1);
      expect(state(r).data).toEqual([1, 2, 3]);
    });

    // One fold per flush, so a reducer copies its map once per batch, not once per frame.
    it('reduces the batch in one call', async () => {
      const reduce = vi.fn(appendTick);
      const r = renderHook(
        () => useFoldedSubscription<{ tick: number }, number[]>({ query: TICK, variables: {} }, reduce),
        {
          wrapper: makeWrapper(),
        },
      );
      await waitFor(() => expect(channels.length).toBeGreaterThan(0));
      await emit(OPEN);
      await emit(NEXT(1));
      await emit(NEXT(2));
      expect(reduce).not.toHaveBeenCalled();

      act(() => flush!());
      expect(reduce).toHaveBeenCalledTimes(1);
      expect(reduce).toHaveBeenLastCalledWith(undefined, [{ tick: 1 }, { tick: 2 }]);
      expect(state(r).data).toEqual([1, 2]);
    });

    // A dead connection's frames never reach the reducer: the new connection replays everything.
    it('drops frames received before a reconnect that lands in the same batch', async () => {
      vi.useFakeTimers();
      const r = renderWatch();
      await vi.waitFor(() => expect(channels.length).toBeGreaterThan(0));
      await emit(OPEN);
      await emit(NEXT(1));
      act(() => flush!());
      expect(state(r).data).toEqual([1]);

      await emit(NEXT(2)); // pending when the transport drops
      await emit(JSON.stringify({ type: 'complete' }));
      await vi.advanceTimersByTimeAsync(1_000);
      await vi.waitFor(() => expect(channels.length).toBe(2));
      await emit(OPEN);
      await emit(NEXT(7));
      act(() => flush!());
      expect(state(r).data).toEqual([7]); // not [1, 2, 7], not [2, 7]
    });

    // The dead connection's flush is cancelled with its frames. Left scheduled, it would run
    // after the reconnect's own flush had emptied the queue and fold an empty batch — which
    // `perFrame` reads as `frames[0]` being undefined.
    it('cancels the pending flush of the connection it drops', async () => {
      vi.useFakeTimers();
      const queued: (() => void)[] = [];
      setSubscriptionFlushScheduler((fn) => {
        queued.push(fn);
        return () => {
          const i = queued.indexOf(fn);
          if (i >= 0) queued.splice(i, 1);
        };
      });
      const reduce = vi.fn(appendTick);
      const r = renderHook(
        () => useFoldedSubscription<{ tick: number }, number[]>({ query: TICK, variables: {} }, reduce),
        { wrapper: makeWrapper() },
      );
      await vi.waitFor(() => expect(channels.length).toBeGreaterThan(0));
      await emit(OPEN);
      await emit(NEXT(1)); // schedules a flush that never runs

      await emit(JSON.stringify({ type: 'complete' }));
      await vi.advanceTimersByTimeAsync(1_000);
      await vi.waitFor(() => expect(channels.length).toBe(2));
      await emit(OPEN);
      await emit(NEXT(7));

      expect(queued).toHaveLength(1);
      act(() => queued.splice(0).forEach((fn) => fn()));
      expect(reduce).toHaveBeenCalledTimes(1);
      expect(reduce).toHaveBeenLastCalledWith(undefined, [{ tick: 7 }]);
      expect(state(r).data).toEqual([7]);
    });
  });
});

// `context` is part of UseSubscriptionArgs, so it has to reach the exchanges — a wrapper
// that accepted it and dropped it would fail silently.
describe('operation context', () => {
  // Records what each subscription operation carried, and answers nothing.
  const captureExchange =
    (seen: OperationContext[]): Exchange =>
    () =>
    (ops$) =>
      pipe(
        ops$,
        tap((op) => {
          if (op.kind === 'subscription') seen.push(op.context);
        }),
        filter(() => false),
      ) as unknown as Source<OperationResult>;

  function renderWithContext(seen: OperationContext[], initial: Partial<OperationContext>) {
    const client = new Client({ url: 'tauri://graphql', exchanges: [captureExchange(seen)] });
    return renderHook(
      ({ ctx }: { ctx: Partial<OperationContext> }) =>
        useFoldedSubscription<{ tick: number }, number[]>({ query: TICK, variables: {}, context: ctx }, appendTick),
      {
        wrapper: ({ children }: { children: ReactNode }) => createElement(Provider, { value: client }, children),
        initialProps: { ctx: initial },
      },
    );
  }

  it('forwards it to the exchanges', async () => {
    const seen: OperationContext[] = [];
    renderWithContext(seen, { requestPolicy: 'network-only' });
    await waitFor(() => expect(seen).toHaveLength(1));
    expect(seen[0].requestPolicy).toBe('network-only');
  });

  it('re-executes when it changes', async () => {
    const seen: OperationContext[] = [];
    const r = renderWithContext(seen, { requestPolicy: 'network-only' });
    await waitFor(() => expect(seen).toHaveLength(1));

    act(() => r.rerender({ ctx: { requestPolicy: 'cache-and-network' } }));
    await waitFor(() => expect(seen).toHaveLength(2));
    expect(seen[1].requestPolicy).toBe('cache-and-network');
  });
});

// The production scheduler. A minimized window gets no animation frames, so the frames
// every watch is queueing have to reach the reducer some other way.
describe('scheduleFlush', () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('flushes on the animation frame when the window is drawing', () => {
    vi.useFakeTimers();
    const frames: (() => void)[] = [];
    vi.stubGlobal('requestAnimationFrame', (fn: () => void) => frames.push(fn));
    vi.stubGlobal('cancelAnimationFrame', () => {});
    const flush = vi.fn();

    scheduleFlush(flush);
    frames[0]();
    expect(flush).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(10_000); // the fallback was cancelled by the frame that ran
    expect(flush).toHaveBeenCalledTimes(1);
  });

  it('flushes on the fallback timer when no animation frame arrives', () => {
    vi.useFakeTimers();
    vi.stubGlobal('requestAnimationFrame', () => 0); // suspended: never calls back
    vi.stubGlobal('cancelAnimationFrame', () => {});
    const flush = vi.fn();

    scheduleFlush(flush);
    expect(flush).not.toHaveBeenCalled();

    vi.advanceTimersByTime(1_000);
    expect(flush).toHaveBeenCalledTimes(1);
  });

  it('cancels both paths', () => {
    vi.useFakeTimers();
    let frame: (() => void) | undefined;
    vi.stubGlobal('requestAnimationFrame', (fn: () => void) => {
      frame = fn;
      return 1;
    });
    vi.stubGlobal('cancelAnimationFrame', () => {
      frame = undefined;
    });
    const flush = vi.fn();

    scheduleFlush(flush)();
    frame?.();
    vi.advanceTimersByTime(10_000);
    expect(flush).not.toHaveBeenCalled();
  });
});

describe('subscriptionPhase', () => {
  it('reads no-data-yet as connecting when the transport is down', () => {
    expect(subscriptionPhase(false, false)).toBe('connecting');
  });

  it('reads an incomplete snapshot as connecting even with the transport up', () => {
    // The case the Bookmark exists for: reporting this as an empty collection is
    // what showed "nothing here" over a set the server was still listing.
    expect(subscriptionPhase(false, true)).toBe('connecting');
  });

  it('reads a synced watch through an outage as reconnecting', () => {
    expect(subscriptionPhase(true, false)).toBe('reconnecting');
  });

  it('reads a synced watch on a live connection as live', () => {
    expect(subscriptionPhase(true, true)).toBe('live');
  });
});
