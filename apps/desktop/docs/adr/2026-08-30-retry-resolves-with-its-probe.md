---
title: Resolve a connection retry when its own probe finishes
date: 2026-08-30
scope: sidecar
status: Accepted
---

# Resolve a connection retry when its own probe finishes

## Context

`clusterConnectionRetry` woke the five probes and resolved at once. The webview's Retry button
therefore had nothing to bind to, and showed a fixed acknowledgement instead — long enough that it
routinely outlived the row's own "checking…" spinner, which follows the real run. Two spinners for
one probe, disagreeing.

The button cannot work the disagreement out for itself. It needs two facts that never reach it:

- **Which run satisfies the ask.** A `Wake` landing while a run is in flight finds the supervisor
  holding the key, so the ask is redelivered on `Done` and the *next* run satisfies it — never the
  one already out.
- **The edges.** `probing` rides `kubeconn`'s state hub, which keeps only the latest value per
  context. A run ending and the requested one starting arrive as one unbroken `probing`, so the
  falling edge meaning "yours finished" is indistinguishable from the one meaning "yours has not
  started".

Two rounds of client-side inference each shipped a defect: a click during an in-flight probe
disarmed the button before the requested run began, and a coalesced pair left it spinning after
the requested run ended.

## Decision

`kubeconn.Service.RetryAndWait(ctx, contextName) error` replaces `Retry`. It claims the context,
subscribes to the state feed, stamps `askedAt`, wakes all five probes, and returns once a
committed `Connection.LastAttempt` began at or after the ask. `cluster.RetryConnection` passes
its request context through and returns the error, so the GraphQL mutation is held open for the
probe's round trip. The webview binds the button to urql's `fetching` — `ConnectionDetail` owns
the `useMutation`, since a panel-level hook would spin every open row's button.

**`Attempts.LastRunAt` against a baseline is what makes this readable.** The supervisor stamps it
with the start of every run that finishes, whatever the run concluded, so one comparison answers
both halves of the question: a run that began at or after the ask has ended. It is a monotonic
level, so coalescing — which drops intermediate values but never regresses one — cannot defeat it
the way it defeats edge detection. And it encodes the supervisor's queue rule for free: a run
already in flight started before the ask, so it correctly does not answer.

**`LastAttempt` will not do**, which is the trap. A `Skip` finishes a run and records no attempt —
the connection probe skips on an unread kubeconfig, and on a cancellation — so a wait keyed on a
committed attempt hangs until the ceiling and fails a retry whose probe already ran. That is
reachable from an ordinary Retry click.

**The queue wait carries no deadline of ours.** The supervisor takes a fleet-wide start slot
before dispatching, and the run queue is ordered, so the requested run's *start* is delayed by
however long the probes queued ahead of it take — bounded, since every job carries a timeout, but
by no number this call can name. The caller's context ends that wait; a ceiling run from the ask
would report a failure for a probe nobody had tried yet.

`retryCeiling` therefore starts when the run *begins*, which the state feed publishes, and is
`connectionTimeout` plus slack — sized off the probe's own deadline rather than a second literal,
so a change there cannot silently undersize it. It ends a run that never ends; resolving `true`
would tell the button a probe finished when none had.

## Alternatives considered

- **A caller-supplied request token**, correlated back on the record. It answers the same question
  and costs a schema field, a wire round trip per click, and a token to garbage-collect — for one
  reader who is already blocked on the call. `StartedAt` is a level the sidecar already publishes.
- **Publishing the edges** — a signal hub firing per run rather than a level hub. It would make
  the client's edge detection sound, but leaves the harder half (which run satisfies the ask)
  unanswered, and every reader would then pay for a fleet's worth of per-run events.
- **Tuning the acknowledgement.** What shipped twice. Both defects were timing guesses about facts
  the webview does not have; a third guess is a third defect.

## Consequences

One GraphQL request is held open per click, for the probe's round trip plus its queue wait — one
goroutine, one state subscription, one claim. Bounded by the ceiling and by the button being
disabled while it is out.

A retry can now fail, where it was infallible in practice. `errorReportExchange` puts the
operation error on the error bus, and `fetching` clears either way, so the button re-arms.

Two obligations. **No transport timeout may sit below the ceiling** — neither `invokeFetch` nor
the `graphql_query` command sets one today. And urql retries `networkError` up to three times, so
a socket dropped mid-probe re-fires the mutation and the sidecar sees a second wake; that is
acceptable (a wake buys at most one extra dial), but the retry policy must not be widened to cover
this mutation's new duration.

**Nothing cancels the run.** The wait's context is the request's; the run is the supervisor's, and
a `Wake` is not a `Restart`. A caller that goes away leaves the probe running.

## Revisit when

The retry's verdict has to reach a window other than the one that asked, or land on the record
itself. A token stops being overhead for one reader at that point and becomes the row's own data.
