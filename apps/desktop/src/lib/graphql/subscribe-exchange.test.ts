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

import { Client, createRequest, gql } from 'urql';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { pipe, subscribe } from 'wonka';

import { mockTauriCore } from '@/test-utils';

// Mocks ---------------------------------------------------------------

const { invokeMock, channels, liveChannel, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const reportErrorMock = vi.fn();
vi.mock('../error-bus', () => ({
  reportError: (...args: unknown[]) => reportErrorMock(...args),
  errorMessage: (err: unknown) => String(err),
}));

// The exchange's only job re: connection status is to call this side-channel
// at the right moments; the generation/connected *semantics* it drives are
// covered by transport-status.test.ts, and the real end-to-end wiring by
// use-folded-subscription.test.tsx. So here we mock it and assert the contract.
const markConnectedMock = vi.fn();
const markDisconnectedMock = vi.fn();
const clearStatusMock = vi.fn();
vi.mock('./transport-status', () => ({
  markConnected: (...args: unknown[]) => markConnectedMock(...args),
  markDisconnected: (...args: unknown[]) => markDisconnectedMock(...args),
  clearStatus: (...args: unknown[]) => clearStatusMock(...args),
}));

const { tauriSubscriptionExchange } = await import('./subscribe-exchange');

// Helpers -------------------------------------------------------------

function makeClient() {
  return new Client({
    url: 'tauri://graphql',
    exchanges: [tauriSubscriptionExchange],
  });
}

const TICK = gql`
  subscription Tick {
    tick
  }
`;

// The op key the exchange stamps its transport status under — derived from the
// query+variables exactly like the operation the client executes, so the test
// can read the same registry entry the exchange writes.
const KEY = createRequest(TICK, {}).key;

const NEXT = (n: number) => JSON.stringify({ type: 'next', payload: { data: { tick: n } } });
// The host's "connection established" frame — emitted before the snapshot on
// every successful connection; the exchange marks the op connected on it.
const OPEN = JSON.stringify({ type: 'open' });
// Abnormal transport end (EOF/drop synthesized by the host): reconnect + report.
const CLOSED = JSON.stringify({ type: 'closed' });
// The server's own graceful end: reconnect, silently.
const COMPLETE = JSON.stringify({ type: 'complete' });

// Flush the pending microtask queue (the async invoke('graphql_subscribe')
// chain) without advancing fake timers.
const flush = async () => {
  await Promise.resolve();
  await Promise.resolve();
};

// Subscribe and collect every delivered `tick`. The reset signal rides the
// transport-status side-channel, not the sink, so `data` here is only ever real
// GraphQL data.
function start() {
  const client = makeClient();
  const seen: number[] = [];
  const sub = pipe(
    client.subscription(TICK, {}),
    subscribe((r) => {
      if (r.data) seen.push((r.data as { tick: number }).tick);
    }),
  );
  return { seen, unsubscribe: sub.unsubscribe };
}

describe('tauriSubscriptionExchange', () => {
  beforeEach(() => {
    invokeMock.mockReset();
    reportErrorMock.mockReset();
    markConnectedMock.mockReset();
    markDisconnectedMock.mockReset();
    clearStatusMock.mockReset();
    channels.length = 0;
    // graphql_subscribe hands back a unique, increasing op id per connect.
    let id = 0;
    invokeMock.mockImplementation(async (cmd: string) => {
      if (cmd === 'graphql_subscribe') {
        id += 1;
        return id;
      }
      if (cmd === 'graphql_unsubscribe') return undefined;
      throw new Error(`unexpected ${cmd}`);
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  const subscribeCalls = () => invokeMock.mock.calls.filter((c) => c[0] === 'graphql_subscribe').length;

  it('forwards `next` payloads to the urql sink and unsubscribes on teardown', async () => {
    const { seen, unsubscribe } = start();
    await flush();

    expect(invokeMock).toHaveBeenCalledWith(
      'graphql_subscribe',
      expect.objectContaining({ query: expect.stringContaining('tick'), variables: {}, channel: expect.any(Object) }),
    );

    liveChannel().onmessage!(OPEN);
    expect(markConnectedMock).toHaveBeenCalledWith(KEY);
    liveChannel().onmessage!(NEXT(1));
    liveChannel().onmessage!(NEXT(2));
    expect(seen).toEqual([1, 2]);

    unsubscribe();
    await flush();
    expect(invokeMock).toHaveBeenCalledWith('graphql_unsubscribe', { id: 1 });
    // Teardown forgets the op's transport status.
    expect(clearStatusMock).toHaveBeenCalledWith(KEY);
  });

  it('reconnects (with backoff) when the transport drops while still subscribed', async () => {
    vi.useFakeTimers();
    const { seen, unsubscribe } = start();
    await flush();
    liveChannel().onmessage!(OPEN); // first connection established
    expect(subscribeCalls()).toBe(1);
    expect(markConnectedMock).toHaveBeenCalledTimes(1);

    // A transport-level `closed` must NOT tear the urql subscription down;
    // it marks the op disconnected and triggers a backoff reconnect instead.
    liveChannel().onmessage!(CLOSED);
    expect(reportErrorMock).toHaveBeenCalledWith(expect.objectContaining({ source: 'subscription' }));
    expect(markDisconnectedMock).toHaveBeenCalledWith(KEY);
    expect(subscribeCalls()).toBe(1); // not immediate

    await vi.advanceTimersByTimeAsync(1_000); // first backoff step
    expect(subscribeCalls()).toBe(2);

    // The reconnected channel opens (a second markConnected — a new generation
    // that drives the hook's reset) and delivers data again.
    liveChannel().onmessage!(OPEN);
    liveChannel().onmessage!(NEXT(7));
    expect(markConnectedMock).toHaveBeenCalledTimes(2);
    expect(seen).toEqual([7]);

    unsubscribe();
  });

  // The sidecar ends a subscription whose watch died with one data-less frame
  // carrying the reason (SubscriptionFailureExtension). That is a drop that can explain
  // itself, so it takes the drop path — report, mark disconnected, reconnect —
  // and must NOT reach the sink: urql merges each frame into the previous result,
  // so an errors-only frame would re-deliver the last frame's data and fold it
  // twice.
  it('reports and reconnects on a terminal-failure frame, without pushing it to the sink', async () => {
    vi.useFakeTimers();
    const { seen, unsubscribe } = start();
    await flush();
    liveChannel().onmessage!(OPEN);
    liveChannel().onmessage!(NEXT(1));

    liveChannel().onmessage!(
      JSON.stringify({
        type: 'next',
        payload: {
          errors: [{ message: 'Cluster watch ended: watch too old' }],
          extensions: { subscriptionFailed: true },
        },
      }),
    );
    expect(seen).toEqual([1]); // the failure frame delivered no data of its own
    expect(reportErrorMock).toHaveBeenCalledWith(
      expect.objectContaining({ source: 'subscription', message: 'Cluster watch ended: watch too old' }),
    );
    expect(markDisconnectedMock).toHaveBeenCalledWith(KEY);

    await vi.advanceTimersByTimeAsync(1_000);
    expect(subscribeCalls()).toBe(2);

    unsubscribe();
  });

  // A frame that carries data is live state, whatever else rides with it: its
  // errors are ordinary field errors and belong to the sink and the error
  // exchange, not to the stream-death path.
  it('still forwards a frame that carries both data and errors', async () => {
    const { seen, unsubscribe } = start();
    await flush();
    liveChannel().onmessage!(OPEN);

    liveChannel().onmessage!(
      JSON.stringify({ type: 'next', payload: { data: { tick: 3 }, errors: [{ message: 'stats unavailable' }] } }),
    );
    expect(seen).toEqual([3]);
    expect(markDisconnectedMock).not.toHaveBeenCalled();

    unsubscribe();
  });

  // The reason the marker exists rather than a shape check: a non-null field that
  // errors nulls its parent, so a live frame whose `stats` failed arrives with no
  // data and an error — byte-identical to a dead watch. Tearing the subscription
  // down for it would make one bad field a reconnect loop.
  it('does not treat an unmarked data-less error frame as a dead watch', async () => {
    const { unsubscribe } = start();
    await flush();
    liveChannel().onmessage!(OPEN);

    liveChannel().onmessage!(
      JSON.stringify({ type: 'next', payload: { data: null, errors: [{ message: 'stats unavailable' }] } }),
    );
    expect(markDisconnectedMock).not.toHaveBeenCalled();
    expect(subscribeCalls()).toBe(1);

    unsubscribe();
  });

  it('reconnects silently on the server’s own `complete` (no error report)', async () => {
    vi.useFakeTimers();
    const { unsubscribe } = start();
    await flush();
    liveChannel().onmessage!(OPEN);

    // A graceful server completion is legitimate (sidecar shutdown; chat's
    // finite stream) — it must reconnect (a long-lived watch has to come back
    // after a sidecar restart) but never banner.
    liveChannel().onmessage!(COMPLETE);
    expect(reportErrorMock).not.toHaveBeenCalled();
    expect(markDisconnectedMock).toHaveBeenCalledWith(KEY);

    await vi.advanceTimersByTimeAsync(1_000);
    expect(subscribeCalls()).toBe(2);

    // If the sidecar really went away, the reconnect's failed dial produces
    // the `error` frame — and *that* reports.
    liveChannel().onmessage!(JSON.stringify({ type: 'error', payload: 'sidecar down' }));
    expect(reportErrorMock).toHaveBeenCalledTimes(1);

    unsubscribe();
  });

  it('reconnects on transport `error`, reports it, and stays silent once healthy', async () => {
    vi.useFakeTimers();
    const { seen, unsubscribe } = start();
    await flush();

    liveChannel().onmessage!(OPEN); // first connection established
    liveChannel().onmessage!(JSON.stringify({ type: 'error', payload: 'boom' }));
    expect(reportErrorMock).toHaveBeenCalledWith(expect.objectContaining({ source: 'subscription' }));
    expect(markDisconnectedMock).toHaveBeenCalledWith(KEY);

    await vi.advanceTimersByTimeAsync(1_000);
    expect(subscribeCalls()).toBe(2);

    // Healthy frame ⇒ backoff resets and no further errors are reported
    // (the banner auto-dismisses, so "recovered" == "we go quiet").
    reportErrorMock.mockClear();
    liveChannel().onmessage!(OPEN);
    liveChannel().onmessage!(NEXT(1));
    expect(seen).toEqual([1]);
    expect(markConnectedMock).toHaveBeenCalledTimes(2);
    expect(reportErrorMock).not.toHaveBeenCalled();

    // Backoff was reset by the healthy frame: the next drop reconnects
    // again after the *base* delay, not a grown one.
    liveChannel().onmessage!(CLOSED);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(subscribeCalls()).toBe(3);

    unsubscribe();
  });

  // A watch the server rejects on every subscribe opens cleanly each time and dies
  // immediately after. If `open` reset the backoff, that pins the retry at the base
  // delay forever — a permanent failure re-subscribing once a second, each `open`
  // bumping the generation and blanking the view's last-known data.
  it('keeps growing backoff when every retry dies of the same watch failure', async () => {
    vi.useFakeTimers();
    const { unsubscribe } = start();
    await flush();

    const failWatch = () =>
      liveChannel().onmessage!(
        JSON.stringify({
          type: 'next',
          payload: { errors: [{ message: 'permissions revoked' }], extensions: { subscriptionFailed: true } },
        }),
      );

    liveChannel().onmessage!(OPEN);
    failWatch();
    await vi.advanceTimersByTimeAsync(1_000);
    expect(subscribeCalls()).toBe(2);

    // Second cycle: the open must not undo the first failure's backoff step.
    liveChannel().onmessage!(OPEN);
    failWatch();
    await vi.advanceTimersByTimeAsync(1_999);
    expect(subscribeCalls()).toBe(2); // still waiting — proves it is the 2s step…
    await vi.advanceTimersByTimeAsync(1);
    expect(subscribeCalls()).toBe(3); // …not another 1s

    // A healthy frame ends the failure run, so the next drop starts over.
    liveChannel().onmessage!(OPEN);
    liveChannel().onmessage!(NEXT(1));
    liveChannel().onmessage!(CLOSED);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(subscribeCalls()).toBe(4);

    unsubscribe();
  });

  it('resets backoff on `open`, so a drop after an empty-snapshot recovery retries promptly', async () => {
    vi.useFakeTimers();
    const { unsubscribe } = start();
    await flush();
    liveChannel().onmessage!(OPEN);

    // Grow the backoff: a drop, then a failed redial.
    liveChannel().onmessage!(CLOSED); // reports (first abnormal drop)
    await vi.advanceTimersByTimeAsync(1_000);
    expect(subscribeCalls()).toBe(2);
    liveChannel().onmessage!(JSON.stringify({ type: 'error', payload: 'still down' }));
    await vi.advanceTimersByTimeAsync(2_000);
    expect(subscribeCalls()).toBe(3);

    // Recovery via an *empty* snapshot: `open` arrives but no `next` ever
    // does. The open alone must reset the backoff — the next drop is a fresh
    // outage and deserves the base delay, not the grown one.
    liveChannel().onmessage!(OPEN);
    liveChannel().onmessage!(CLOSED);
    await vi.advanceTimersByTimeAsync(999);
    expect(subscribeCalls()).toBe(3); // not yet — proves it's the base delay…
    await vi.advanceTimersByTimeAsync(1);
    expect(subscribeCalls()).toBe(4); // …not the pre-recovery 4s step

    // The report gate is NOT open-reset (only a healthy `next` clears it):
    // a flapping server that 200s then drops must not banner every cycle.
    expect(reportErrorMock).toHaveBeenCalledTimes(1);

    unsubscribe();
  });

  it('uses capped exponential backoff across repeated failures', async () => {
    vi.useFakeTimers();
    const { unsubscribe } = start();
    await flush();

    // Drop the live connection, then assert the reconnect lands exactly at
    // `delay` (not a tick before) — proving the backoff curve and its cap.
    const dropThenReconnectAt = async (delay: number, priorCalls: number) => {
      liveChannel().onmessage!(CLOSED);
      await vi.advanceTimersByTimeAsync(delay - 1);
      expect(subscribeCalls()).toBe(priorCalls); // not yet
      await vi.advanceTimersByTimeAsync(1);
      expect(subscribeCalls()).toBe(priorCalls + 1); // reconnected
    };

    // Each new connection dies without ever opening ⇒ delay doubles: 1s, 2s,
    // 4s … capped at 30s and held there. (An `open` would reset the curve —
    // covered separately.)
    await dropThenReconnectAt(1_000, 1);
    await dropThenReconnectAt(2_000, 2);
    await dropThenReconnectAt(4_000, 3);
    await dropThenReconnectAt(8_000, 4);
    await dropThenReconnectAt(16_000, 5);
    await dropThenReconnectAt(30_000, 6);
    await dropThenReconnectAt(30_000, 7);

    unsubscribe();
  });

  it('does not reconnect after the consumer unsubscribes', async () => {
    vi.useFakeTimers();
    const { unsubscribe } = start();
    await flush();
    expect(subscribeCalls()).toBe(1);

    unsubscribe();
    await flush();
    expect(invokeMock).toHaveBeenCalledWith('graphql_unsubscribe', { id: 1 });

    // A late transport `closed` on the dead channel is inert.
    liveChannel().onmessage!(CLOSED);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(subscribeCalls()).toBe(1);
  });

  it('dials a reconnect with the variables the context’s `reconnectVariables` answers', async () => {
    vi.useFakeTimers();
    const FROM = gql`
      subscription From($after: String) {
        from(after: $after)
      }
    `;
    let resumeFrom = 'c1';
    const sub = pipe(
      makeClient().subscription(FROM, { after: 'c0' }, { reconnectVariables: () => ({ after: resumeFrom }) }),
      subscribe(() => {}),
    );
    await flush();
    const dialed = () =>
      invokeMock.mock.calls
        .filter((c) => c[0] === 'graphql_subscribe')
        .map((c) => (c[1] as { variables: unknown }).variables);
    expect(dialed()).toEqual([{ after: 'c0' }]);

    liveChannel().onmessage!(OPEN);
    liveChannel().onmessage!(CLOSED);
    await vi.advanceTimersByTimeAsync(1_000);
    resumeFrom = 'c2';
    liveChannel().onmessage!(CLOSED);
    await vi.advanceTimersByTimeAsync(2_000);
    expect(dialed()).toEqual([{ after: 'c0' }, { after: 'c1' }, { after: 'c2' }]);

    sub.unsubscribe();
  });

  it('reports the drop once per outage, not on every backoff retry', async () => {
    vi.useFakeTimers();
    const { unsubscribe } = start();
    await flush();

    liveChannel().onmessage!(CLOSED);
    expect(reportErrorMock).toHaveBeenCalledTimes(1);

    // Still down: each capped retry reconnects but must NOT re-report
    // (otherwise the 5s-auto-dismiss banner flickers forever).
    await vi.advanceTimersByTimeAsync(1_000);
    liveChannel().onmessage!(CLOSED);
    await vi.advanceTimersByTimeAsync(2_000);
    liveChannel().onmessage!(CLOSED);
    expect(reportErrorMock).toHaveBeenCalledTimes(1);

    // Recover, then drop again ⇒ a fresh outage reports once more.
    await vi.advanceTimersByTimeAsync(4_000);
    liveChannel().onmessage!(NEXT(1));
    liveChannel().onmessage!(CLOSED);
    expect(reportErrorMock).toHaveBeenCalledTimes(2);

    unsubscribe();
  });

  it('reconnects on a malformed transport frame', async () => {
    vi.useFakeTimers();
    const { seen, unsubscribe } = start();
    await flush();
    liveChannel().onmessage!(OPEN); // first connection established

    liveChannel().onmessage!('}{ not json');
    expect(reportErrorMock).toHaveBeenCalledWith(
      expect.objectContaining({ source: 'subscription', message: expect.stringContaining('malformed') }),
    );

    await vi.advanceTimersByTimeAsync(1_000);
    expect(subscribeCalls()).toBe(2);
    liveChannel().onmessage!(OPEN);
    liveChannel().onmessage!(NEXT(5));
    expect(seen).toEqual([5]);

    unsubscribe();
  });

  it('marks connected on the `open` frame, not on the subscribe ack, and sends no data on open', async () => {
    const { seen, unsubscribe } = start();
    await flush();
    // The ack alone means nothing — the host acks before it dials, so keying
    // connected off it would claim a connection that hasn't opened.
    expect(markConnectedMock).not.toHaveBeenCalled();
    expect(seen).toEqual([]);

    liveChannel().onmessage!(OPEN);
    // `open` marks connected but pushes nothing onto the data channel.
    expect(markConnectedMock).toHaveBeenCalledWith(KEY);
    expect(seen).toEqual([]);

    liveChannel().onmessage!(NEXT(1));
    expect(seen).toEqual([1]);

    unsubscribe();
  });

  it('marks connected again on a reconnect open even when the replayed snapshot is empty', async () => {
    vi.useFakeTimers();
    const { seen, unsubscribe } = start();
    await flush();
    liveChannel().onmessage!(OPEN);
    expect(markConnectedMock).toHaveBeenCalledTimes(1);

    liveChannel().onmessage!(CLOSED);
    await vi.advanceTimersByTimeAsync(1_000);

    // The reconnection opens but the server has nothing to replay (everything
    // was deleted during the outage) — the `open` frame alone must mark
    // connected again (a fresh generation, so the hook resets), without any
    // `next` to ride on.
    liveChannel().onmessage!(OPEN);
    expect(markConnectedMock).toHaveBeenCalledTimes(2);
    expect(seen).toEqual([]);

    unsubscribe();
  });

  it('does not mark connected for a reconnect attempt whose dial fails', async () => {
    vi.useFakeTimers();
    const { unsubscribe } = start();
    await flush();
    liveChannel().onmessage!(OPEN);
    expect(markConnectedMock).toHaveBeenCalledTimes(1);

    // Drop, then reconnect. The host acks `graphql_subscribe` (a new channel is
    // opened) but the dial fails, so it emits an `error` frame instead of
    // `open` — modelling a sidecar that's down. No `open` ⇒ no markConnected,
    // so the hook keeps its last-known data through the outage.
    liveChannel().onmessage!(CLOSED);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(subscribeCalls()).toBe(2);
    liveChannel().onmessage!(JSON.stringify({ type: 'error', payload: 'sidecar down' }));
    expect(markConnectedMock).toHaveBeenCalledTimes(1); // still just the first open
    expect(markDisconnectedMock).toHaveBeenCalledWith(KEY);

    // The next attempt connects for real (`open`) and marks connected again.
    await vi.advanceTimersByTimeAsync(2_000);
    liveChannel().onmessage!(OPEN);
    expect(markConnectedMock).toHaveBeenCalledTimes(2);

    unsubscribe();
  });

  it('cancels a pending backoff reconnect if the consumer unsubscribes first', async () => {
    vi.useFakeTimers();
    const { unsubscribe } = start();
    await flush();

    liveChannel().onmessage!(CLOSED); // schedules reconnect
    unsubscribe(); // before the backoff elapses
    await vi.advanceTimersByTimeAsync(60_000);
    expect(subscribeCalls()).toBe(1);
  });

  it('drops an op whose subscribe ack lands after the consumer left', async () => {
    let ack!: (id: number) => void;
    invokeMock.mockImplementation(async (cmd: string) => {
      if (cmd === 'graphql_subscribe') {
        return new Promise<number>((resolve) => {
          ack = resolve;
        });
      }
      return undefined;
    });
    const { unsubscribe } = start();
    await flush();
    unsubscribe();
    expect(invokeMock).not.toHaveBeenCalledWith('graphql_unsubscribe', expect.anything());

    ack(9);
    await flush();
    expect(invokeMock).toHaveBeenCalledWith('graphql_unsubscribe', { id: 9 });
  });

  it('reports a subscribe the host refused, and dials again after backoff', async () => {
    vi.useFakeTimers();
    invokeMock.mockRejectedValueOnce(new Error('no sidecar'));
    const { unsubscribe } = start();
    await flush();

    expect(reportErrorMock).toHaveBeenCalledWith(expect.objectContaining({ source: 'subscription' }));
    expect(markDisconnectedMock).toHaveBeenCalledWith(KEY);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(subscribeCalls()).toBe(2);

    unsubscribe();
  });
});
