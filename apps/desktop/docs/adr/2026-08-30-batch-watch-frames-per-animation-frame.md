---
title: Reduce subscription frames as a batch, once per animation frame
date: 2026-08-30
scope: frontend
status: Accepted
---

# Reduce subscription frames as a batch, once per animation frame

## Context

Every delta watch sends its snapshot one frame per object, and the Tauri channel delivers each
frame as its own task. Folding per frame meant, per object: a React render, a full copy of the
accumulator map, and — in every consumer — a full array rebuild and re-sort. A 5,000-object kind
was ~12.5M map writes and 5,000 sorts before the table first painted; the on-subscribe kinds
burst had the same shape. urql's `useSubscription`, which set state per result, was the cause of
the per-frame render and offered no place to coalesce.

## Decision

`useWatchSubscription` (`src/lib/graphql/use-watch-subscription.ts`) subscribes directly
(`client.executeSubscription`) and keeps its own store. Frames are queued as they arrive and
reduced **once per `requestAnimationFrame`**, as a batch: the reducer's shape is
`(prev, frames: Data[]) => Result`, and the result is published through `useSyncExternalStore`.
A map-accumulating reducer copies its map once per batch and applies each frame in place
(`applyChange` mutates); a cheap per-frame fold is lifted with `perFrame(step)`.

The generation gate is unchanged in meaning: a frame stamped with a newer generation than the
pending batch drops the batch — a dead connection's frames never reach the reducer, since the new
connection replays everything. Exposed `data` is still masked while the published tag is stale.

The scheduler is a module-level seam (`setWatchFlushScheduler`); `flushWatchesSynchronously()`
in `@/test-utils` makes each frame visible at once for suites that push a frame and assert on the
next line.

## Alternatives considered

- **Coalesce in the exchange.** It would batch the wire frames, but urql's sink takes one
  `OperationResult` at a time and merges each into the last; the wrapper is the first place a
  batch can be expressed.
- **Keep urql's `useSubscription`, defer only publication.** Removes the sorts but not the
  renders — urql still sets state per result — and leaves the map copy per frame, since a pure
  per-frame reducer cannot know when it is safe to mutate.
- **A microtask flush.** Coalesces nothing: each IPC message is its own task, so the microtask
  runs between every two frames.
- **`setTimeout(0)`.** Timer tasks interleave with IPC tasks with no ordering guarantee, so a
  batch's size would depend on scheduling luck. `requestAnimationFrame` paces on the display,
  which is what a render batch is for.

## Consequences

- One render, one copy and one sort per flush regardless of burst size. Steady state — one frame
  per flush — costs what it did.
- A hidden window (minimized, background) does not run animation frames, so its watches
  accumulate until it is shown. Nothing is lost; nothing is painted.
- Reducers are called with a non-empty array and must handle several frames; `perFrame` exists so
  a last-value or append fold does not have to say so.
- Effects inside a reducer (chat's `setStreamed`) now run once per flush, not once per chunk.
- The accumulator lives in the hook, not in urql, so it is dropped when the watch pauses or its
  variables change. A consumer that pauses and resumes the same watch (`useCacheContents` in
  `cluster-sync-panel.tsx`) shows its loading state again on resume instead of last-known data.
  That is what the provenance guards want — a resumed watch replays its snapshot anyway.

## Revisit when

The sidecar sends a snapshot as fewer, larger frames (the paged-watch work), at which point the
batch is mostly one frame and the scheduler could flush on the frame itself.
