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

import { Channel, invoke } from '@tauri-apps/api/core';
import { subscriptionExchange } from 'urql';

import { errorMessage, reportError } from '../error-bus';
import { clearStatus, markConnected, markDisconnected } from './transport-status';

// The Rust bridge's channel envelopes. `open` fires once per established SSE
// connection, before any `next`. `complete` is the server's graceful end;
// `closed` is host-synthesized for an ungraceful one. Both reconnect, but only
// `closed` reports.
type SubPayload = { data?: unknown; errors?: unknown; extensions?: Record<string, unknown> };

type SubMessage =
  | { type: 'open' }
  | { type: 'next'; payload: SubPayload }
  | { type: 'error'; payload: unknown }
  | { type: 'complete' }
  | { type: 'closed' };

// The sidecar's marker for "this watch died, here is why" — set by its
// SubscriptionFailureExtension; keep in step with sidecar/graph/subscription_failure.go.
const SUBSCRIPTION_FAILED = 'subscriptionFailed';

// The message from a terminal-failure frame — the sidecar's reason a watch died —
// or null if payload isn't one. Keyed on the marker the sidecar's
// SubscriptionFailureExtension sets, never on shape: a non-null field erroring nulls its
// parent, so an ordinary frame carrying a field error is shape-identical.
function terminalError(payload: SubPayload): string | null {
  if (payload.extensions?.[SUBSCRIPTION_FAILED] !== true) return null;
  const first = Array.isArray(payload.errors) ? (payload.errors[0] as { message?: unknown }) : undefined;
  return typeof first?.message === 'string' ? first.message : 'the server ended the subscription';
}

const BASE_DELAY_MS = 1_000;
const MAX_DELAY_MS = 30_000;

// No jitter: one client per subscription, so no thundering herd.
const backoffDelay = (attempt: number) => Math.min(MAX_DELAY_MS, BASE_DELAY_MS * 2 ** attempt);

// Fire-and-forget: the op may already be gone, so rejection is expected.
const dropOp = (id: number) => {
  invoke('graphql_unsubscribe', { id }).catch(() => undefined);
};

// Bridges urql's subscriptionExchange to the host's `graphql_subscribe` /
// `graphql_unsubscribe` commands; each subscription is a host-side SSE stream
// translated into the channel envelopes above.
// See docs/adr/2026-08-09-graphql-over-tauri-ipc.md
//
// Subscriptions here are long-lived and never legitimately complete, so any
// transport end while the consumer is still subscribed reconnects with capped
// backoff — invisibly to urql (the operation and its accumulated state survive).
// The lifecycle is published out of band on transport-status.ts, keyed by
// `operation.key`: `markConnected` on `open` (bumping the generation),
// `markDisconnected` on every drop. Generation bumps on `open`, NOT the
// `graphql_subscribe` ack — the host acks before it dials, so an ack-driven
// reset would wrongly clear last-known data on every failed retry.
// See docs/adr/2026-08-09-transport-status-generation.md
//
// An `error` frame is transport-level only (GraphQL errors arrive inside
// `next`); it reconnects rather than `sink.error` — such failures are almost
// always transient and a terminal error would kill the recovery path. Reporting
// split: `closed`/`error` report to the error bus; `complete` (graceful, can be
// legitimate — sidecar shutdown, chat's finite stream) reconnects silently.
//
// A reconnect dials with the operation's variables, unless its context carries
// `reconnectVariables`: a stream that resumes from a cursor (`logsTail`) hands
// one in, so a reconnect goes on from where the stream got to.
export const tauriSubscriptionExchange = subscriptionExchange({
  // `operation.key` is the same key useFoldedSubscription derives from
  // query+variables — transport status is stamped under it.
  forwardSubscription(request, operation) {
    return {
      subscribe(sink) {
        const { key } = operation;
        const reconnectVariables = operation.context.reconnectVariables as (() => typeof request.variables) | undefined;
        let cancelled = false;
        let opId: number | null = null;
        let attempt = 0;
        // Separate from `attempt`: backoff resets on `open` (dial ok), the
        // report gate only on `next` — else a server that 200s then drops
        // would re-report every cycle.
        let reportedOutage = false;
        // Set when a connection died of a terminal watch failure, cleared by the
        // next healthy frame. Such a connection opens and dies immediately, so
        // `open` is not evidence the watch works.
        let subscriptionFailed = false;
        let retryTimer: ReturnType<typeof setTimeout> | null = null;

        // `report: false` is the graceful `complete` — reconnect silently.
        function scheduleReconnect(message: string, cause?: unknown, report = true) {
          if (cancelled) return;
          // Generation stays untouched until the next `open`, so the hook
          // keeps last-known data through the outage.
          markDisconnected(key);
          // Usually the host already removed the entry, but the malformed-frame
          // path leaves it live; graphql_unsubscribe tolerates unknown ids.
          if (opId != null) dropOp(opId);
          opId = null;
          // First abnormal drop of an outage only; via the bus directly since
          // the result never reaches the urql sink for errorReportExchange.
          if (report && !reportedOutage) {
            reportedOutage = true;
            reportError({ source: 'subscription', message, cause });
          }
          const delay = backoffDelay(attempt);
          attempt += 1;
          retryTimer = setTimeout(() => {
            retryTimer = null;
            if (!cancelled) connect(reconnectVariables?.() ?? request.variables);
          }, delay);
        }

        function connect(variables: typeof request.variables) {
          const channel = new Channel<string>();
          let ended = false;
          // Stale = consumer tore down, or a later connect() took over — stop
          // driving the sink / scheduling reconnects.
          const isStale = () => cancelled || ended;
          const end = (message: string, cause?: unknown, report = true) => {
            if (ended) return;
            ended = true;
            scheduleReconnect(message, cause, report);
          };

          // oxlint-disable-next-line unicorn/prefer-add-event-listener -- Tauri's `Channel` is no `EventTarget`
          channel.onmessage = (raw) => {
            if (isStale()) return;
            let msg: SubMessage;
            try {
              msg = JSON.parse(raw) as SubMessage;
            } catch {
              end(`malformed subscription frame: ${raw}`, raw);
              return;
            }
            if (msg.type === 'open') {
              // Bump the generation before the snapshot folds in. Reset backoff
              // here too: an empty-snapshot recovery never sends a `next`. Not
              // after a watch failure, though — the server accepts the
              // subscription and kills it again, so resetting on each open would
              // pin the retry at the base delay forever.
              if (!subscriptionFailed) attempt = 0;
              markConnected(key);
            } else if (msg.type === 'next') {
              const reason = terminalError(msg.payload);
              if (reason !== null) {
                // The stream is over, so take the ordinary drop path: report, hold
                // last-known data, reconnect. Never the sink — urql merges each frame
                // into the previous result, so an errors-only frame would re-deliver
                // the last frame's data and fold it a second time.
                subscriptionFailed = true;
                end(reason, msg.payload);
                return;
              }
              // Healthy frame: outage over; next abnormal drop reports fresh.
              attempt = 0;
              subscriptionFailed = false;
              reportedOutage = false;
              sink.next(msg.payload as never);
            } else if (msg.type === 'error') {
              const m = typeof msg.payload === 'string' ? msg.payload : JSON.stringify(msg.payload);
              end(m, msg.payload);
            } else if (msg.type === 'complete') {
              // Graceful server end: reconnect silently.
              end('subscription completed by server', undefined, false);
            } else {
              // `closed` — and, defensively, any unknown frame type.
              end('subscription transport closed');
            }
          };

          invoke<number>('graphql_subscribe', {
            query: request.query,
            variables: variables ?? {},
            channel,
          })
            .then((id) => {
              // The ack can resolve after teardown/end — drop the orphaned op.
              if (isStale()) {
                dropOp(id);
              } else {
                opId = id;
              }
            })
            .catch((err: unknown) => end(errorMessage(err), err));
        }

        connect(request.variables);

        return {
          unsubscribe() {
            cancelled = true;
            if (retryTimer) clearTimeout(retryTimer);
            if (opId != null) dropOp(opId);
            clearStatus(key);
          },
        };
      },
    };
  },
});
