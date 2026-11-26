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

// App-wide wrapper over an urql subscription: folds frames through a caller's reducer,
// exposes `connected` from the transport-status side-channel, and resets the
// accumulator across reconnects.
//
// Frames arrive one per IPC task — a snapshot of N objects is N frames — so they are
// collected as they land and reduced once per animation frame, as a batch. A reducer
// copies its map once per batch, and consumers render, rebuild and re-sort once per
// flush, not once per object.
//
// The reset is generation-gated two ways off the same counter (fold onto undefined on
// tag mismatch; exposed `data` masked while the tag is stale), so it's order-independent
// between side-channel notify and frame.
// See docs/adr/2026-08-09-transport-status-generation.md
import { useCallback, useEffect, useState, useSyncExternalStore } from 'react';
import { createRequest, useClient } from 'urql';
import type { AnyVariables, UseSubscriptionArgs } from 'urql';
import { pipe, subscribe } from 'wonka';

import { getStatus, subscribeStatus } from './transport-status';

// Reduced value tagged with the connection generation it folded under; comparing
// tags is how a reconnect's snapshot replaces prior state without a synthetic frame.
type Generational<Result> = { generation: number; result: Result };

// Runs `flush` later, once; returns a cancel.
type FlushScheduler = (flush: () => void) => () => void;

// Ceiling on how long a batch waits when the display isn't pacing us. Loose enough
// that a visible window always flushes on its frame instead.
const HIDDEN_FLUSH_MS = 250;

// Paces on the display, with a timer underneath it: a minimized or hidden window gets
// no animation frames at all, and every subscription's queue would grow until it came back.
export const scheduleFlush: FlushScheduler = (flush) => {
  // Declared before `run` so a scheduler that calls back synchronously (a stub, a
  // polyfill) reads initialized handles rather than hitting the temporal dead zone.
  let frame: number;
  let timer: ReturnType<typeof setTimeout>;
  const run = () => {
    cancelAnimationFrame(frame);
    clearTimeout(timer);
    flush();
  };
  frame = requestAnimationFrame(run);
  timer = setTimeout(run, HIDDEN_FLUSH_MS);
  return () => {
    cancelAnimationFrame(frame);
    clearTimeout(timer);
  };
};

let schedule: FlushScheduler = scheduleFlush;

// Test seam: `flushSubscriptionsSynchronously()` in `@/test-utils` makes every frame visible at once.
export function setSubscriptionFlushScheduler(next: FlushScheduler) {
  schedule = next;
}

// One hook instance's fold. `published` is the snapshot React reads; `pending` holds
// the frames received since, all from the connection `pendingGeneration` names.
type Store<Data, Result> = {
  published: Generational<Result> | undefined;
  pending: Data[];
  pendingGeneration: number;
  cancelFlush: () => void;
  listeners: Set<() => void>;
};

export type UseFoldedSubscriptionResponse<Result> = {
  data: Result | undefined;
  connected: boolean;
  /** The connection the data folded under; it moves on every reconnect, a restarted sidecar's included. */
  generation: number;
};

export type SubscriptionOptions = {
  // Give this instance its own stream instead of sharing one with every other
  // consumer of the same subscription. urql keys an operation on query + variables alone,
  // so consumers share — and one that mounts into a stream already running never
  // sees the snapshot that opened it, leaving it to fold deltas onto nothing. Costs
  // a second cold list and a second connection per extra consumer, so it is the
  // caller's call: set it where a subscription has more than one and one can mount late (a
  // popover reopening), unless a provider above them all can hold the one subscription.
  unshared?: boolean;
};

// Distinguishes one unshared instance from the next, added to the operation key so
// the key still moves with the variables. urql's own keys are hashes, so an offset
// is no more collision-prone than the hashing already is.
let nextInstance = 0;

// Folds the frames received since the last flush — never empty — onto `prev`.
export type SubscriptionReducer<Data, Result> = (prev: Result | undefined, frames: Data[]) => Result;

// Lifts a per-frame step to a batch, for folds that are cheap per frame.
export function perFrame<Data, Result>(
  step: (prev: Result | undefined, data: Data) => Result,
): SubscriptionReducer<Data, Result> {
  return (prev, frames) => frames.slice(1).reduce<Result>(step, step(prev, frames[0]));
}

export function useFoldedSubscription<Data, Result, Variables extends AnyVariables = AnyVariables>(
  args: UseSubscriptionArgs<Variables, Data>,
  reduce: SubscriptionReducer<Data, Result>,
  { unshared = false }: SubscriptionOptions = {},
): UseFoldedSubscriptionResponse<Result> {
  const client = useClient();

  // Fixed for this instance's lifetime, so its key still moves with the variables.
  const [instance] = useState(() => {
    if (!unshared) return 0;
    nextInstance += 1;
    return nextInstance;
  });

  // Same op key ⇒ same request object, so a caller passing a fresh `variables` literal
  // each render (chat) does not resubscribe.
  const shared = createRequest(args.query, args.variables);
  const next = instance ? { ...shared, key: shared.key + instance } : shared;
  const [request, setRequest] = useState(next);
  if (request.key !== next.key) setRequest(next);
  const { key } = request;

  const status = useSyncExternalStore(
    useCallback((onChange) => subscribeStatus(key, onChange), [key]),
    () => getStatus(key),
  );

  const [store] = useState<Store<Data, Result>>(() => ({
    published: undefined,
    pending: [],
    pendingGeneration: 0,
    cancelFlush: () => {},
    listeners: new Set(),
  }));

  // The fold always uses the latest reducer (its closure may read props).
  const [reduceRef] = useState(() => ({ current: reduce }));
  useEffect(() => {
    reduceRef.current = reduce;
  });

  // `context` rides along to the exchanges. Like urql's own hook, it is compared by
  // identity: a caller passing a fresh literal each render resubscribes each render.
  const { pause, context } = args;
  useEffect(() => {
    if (pause) return undefined;
    const flush = () => {
      const frames = store.pending;
      store.pending = [];
      const { published, pendingGeneration: generation } = store;
      // Frames from a newer connection than the accumulator ⇒ fold onto a clean slate.
      const base = published && published.generation === generation ? published.result : undefined;
      store.published = { generation, result: reduceRef.current(base, frames) };
      store.listeners.forEach((notify) => notify());
    };
    const { unsubscribe } = pipe(
      client.executeSubscription(request, context),
      subscribe((res) => {
        if (!res.data) return;
        const { generation } = getStatus(key);
        // A reconnect mid-batch: the dead connection's frames are dropped, since the new
        // one replays everything. Its flush goes with them — left scheduled, it would run
        // after this frame's own flush and hand the reducer an empty batch.
        if (store.pendingGeneration !== generation) {
          store.cancelFlush();
          store.pending = [];
        }
        store.pendingGeneration = generation;
        store.pending.push(res.data);
        // Test the queue, not the cancel: a synchronous scheduler flushes before returning.
        if (store.pending.length === 1) store.cancelFlush = schedule(flush);
      }),
    );
    return () => {
      unsubscribe();
      store.cancelFlush();
      store.pending = [];
      store.published = undefined;
    };
  }, [client, request, key, pause, context, store, reduceRef]);

  const published = useSyncExternalStore(
    useCallback(
      (onChange) => {
        store.listeners.add(onChange);
        return () => store.listeners.delete(onChange);
      },
      [store],
    ),
    () => store.published,
  );

  // Mask a prior-connection accumulator (snapshot not yet folded, or empty and
  // never will be) back to "no data yet".
  const data = published && published.generation === status.generation ? published.result : undefined;

  return { data, connected: status.connected, generation: status.generation };
}

// A subscription's rendering state. `connecting` covers everything before the first
// delivery is complete — transport still dialing, or its snapshot still arriving — so a
// consumer renders an empty state only from `live`/`reconnecting`, where empty
// means empty. `reconnecting` is a drop with last-known data held.
export type SubscriptionPhase = 'connecting' | 'reconnecting' | 'live';

// `snapshotComplete` means the first delivery has arrived: a collection watch's or an
// event feed's Bookmark. Deriving it from "any data yet" instead would report a populated
// collection as empty for the whole time the server spends listing it — `connected` flips
// on the transport's open frame, which precedes the first row.
export function subscriptionPhase(snapshotComplete: boolean, connected: boolean): SubscriptionPhase {
  if (!snapshotComplete) return 'connecting';
  return connected ? 'live' : 'reconnecting';
}
