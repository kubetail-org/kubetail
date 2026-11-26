---
title: Report a dead watch as a terminal GraphQL error, not a graceful end
date: 2026-08-14
scope: sidecar, frontend
status: Accepted
amended_by:
  - [Cached-data watches ping, re-read and diff, and a cleared cache ends one cleanly](2026-08-26-cached-data-read-loop.md)
  - [Cache health is a read-side fold over the records, with paused kinds resolved first](2026-09-02-cache-health-fold.md)
  - [Name every data access for its family, kind and operation](2026-11-25-name-every-data-access-for-its-family-kind-and-operation.md)
---

# Report a dead watch as a terminal GraphQL error, not a graceful end

## Context

A cluster watch that dies — RBAC revoked on a CRD, a resource version past retention, the cache
file gone — closed its channel and spent the reason on a `slog.Warn`. Nothing above the service
could read it, so the resolver saw one closed channel for both "the ctx you passed ended" and
"the upstream broke".

The recovery path already worked: gqlgen completes the subscription, `subscribe-exchange` treats
`complete` as never-legitimate and reconnects, the host's `open` frame bumps the transport
generation, `useWatchSubscription` resets its accumulator, and a fresh snapshot arrives. What was
missing is the *reason*. A **permanently** broken watch therefore became an invisible reconnect
loop: a spinner or a stale table for the user, one `Warn` per cycle in the sidecar log.

Two constraints shaped the design.

**gqlgen cannot emit a mid-stream error from a subscription resolver.** The generated dispatch
builds each frame as `&graphql.Response{Data: …}` and returns `nil` the moment the resolver's
channel closes, ending the SSE loop. The only live response context mid-stream is the per-frame
one, which the watch goroutine does not hold. So the reason has to outlive the resolver and be
picked up on the way out.

**urql merges each subscription frame into the previous result** (`@urql/core`'s
`mergeResultPatch`). An errors-only frame pushed to the sink re-delivers the *last* frame's data,
folding it a second time — harmless for our id-keyed reducers, but only by luck.

## Decision

The service's failable watches return `*cluster.Stream[T]` — `Frames` plus `Err()` — instead of a
bare channel. `Frames` closes on every exit, so `Err` is the only thing separating a failure from
an ordinary teardown; the reason is recorded *before* the close, which makes "Frames closed" a safe
cue to read it.

At the boundary, `graph.WatchFailureExtension` carries it to the wire in two halves, because the
resolver and the frame that would carry the reason never share a response context:

- `InterceptOperation` hangs a claim-once slot on the operation ctx, which gqlgen threads into both
  the resolvers and every later frame.
- `watchStream` files `Stream.Err()` into that slot as a watch's frames run out.
- `InterceptResponse` claims it once the stream is spent and emits one errors-only
  `graphql.Response` ahead of the transport's completion. It goes through `AddError`, so the
  existing error presenter logs it.

The terminal frame is **marked** — `extensions: {"watchFailed": true}` — not merely shaped. A client
cannot infer stream death from "errors and no data": a non-null field that errors nulls its parent,
so a live frame whose `stats` failed is byte-identical. On the client, `subscribe-exchange` keys on
that marker and treats the frame as a drop that can explain itself: report, mark disconnected,
reconnect. It never reaches the sink, which sidesteps the merge behavior above. Every other frame is
live state, so its errors stay ordinary field errors bound for `errorReportExchange`.

## Alternatives rejected

**A `Failed` frame type.** The obvious shape, and the one beehive v0.23.0 had just removed: a
terminal frame carries no entity, so every reducer downstream must recognize it *before* folding or
read it as a deletion. At the GraphQL boundary it would reintroduce exactly the null-entity hazard
the `Bookmark` already forces us to guard (→ [ADR: delta-watch
protocol](2026-08-09-delta-watch-protocol.md)).

**Beehive's full stream shape.** v0.23.0 replaced `(snapshot, channel, error)` with a stream value
carrying `Objects`/`ResourceVersion`/`Changes`/`Err()`. Adopting all of it here would re-split what
the delta-watch pump (`deltaWatch`, `stream.go`) exists to *join*: our watches fold the snapshot into the
frame channel as `Added…` + `Bookmark`, which is what the delta protocol and the webview's reducer
want. Only `Err()` was missing, so only `Err()` was added.

**A failure sink smuggled on the ctx.** Zero signature changes, but the error path becomes
invisible: a reader of the pump cannot see where the reason goes without knowing the
extension exists. Go idiom prefers the explicit return.

**Converting every watch.** The cached-data watches (`CachedData()`) read the local cache and
their read loop retried forever, so they have no terminal reason to carry; an `Err()` that is
always nil is ceremony. They stay plain channels.

The line is drawn at the **source, not the shape**. "Gauges are current-on-subscribe, so they can't
fail" is the wrong rule and was briefly the stated one: `Caches().WatchHealth` is a gauge whose
fold reads beehive records of its own, failing for exactly the reasons its sibling delta
watches do — and it is the always-mounted stream, so it was the worst one to miss. It returns a
`Stream` too, and a read the fold cannot make ends it with that error.

## Consequences

- The failure is visible: a banner via the error bus, and a logged reason naming the kind.
- Recovery is unchanged — the reconnect still runs, and last-known data is still held through it,
  so the terminal frame costs no rows.
- `NewStream` is exported. Any fake implementing the family interfaces has to build one, so this is
  API surface rather than a test seam.
- `watchStream` is load-bearing: a resolver over a `*Stream` that used `ptrStream` would silently
  drop the reason and report the failure as a graceful end.
