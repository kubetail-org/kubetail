---
title: A cache stopped at its size ceiling is held there by its own record
date: 2026-09-03
scope: sidecar
status: Accepted
---

# A cache stopped at its size ceiling is held there by its own record

## Context

The ceiling stops a cache by the switch that was already there: `armSync` calls
`ForgetDiscovery` instead of `TrackDiscovery` (→ [bound the cache by total
size](2026-09-03-bound-the-cache-by-total-size.md)).

Stopping the sync tears the session down, which releases the store claim. The last claim closes the
file and ends its janitor. So the act of stopping destroys the evidence it was based on:
`Stats.OverSizeLimit` reads the janitor's memo off an open file, and there is no longer one. A
controller pass that read the verdict alone would find `false` on the very next pass of the cache it
had just stopped, restart it, reopen the oversized file, sweep, and stop it again — writing more
into a full cache each time round.

## Decision

**The janitor's verdict is the only way into the stop; the record's `Synced` condition is what keeps
the cache there.** The pass writes `Synced=False` / `ReasonSizeLimit` on the way in, and while that
condition stands it decides by measuring bytes against `Stats.SizeLimitBytes` rather than by reading
the verdict.

Two comparisons against one ceiling, because the two moments differ. Entering, the file is being
filled and its WAL may hold pages the database already counts — only the janitor, which checkpoints
before it judges, is right about that file. Leaving, nothing is writing the cache and a cleanly
closed SQLite file has no WAL at all, so the bytes are unambiguous. Neither side holds a limit of
its own; both read the one `Stats` reports.

Two further consequences are part of the decision:

- **The health gauge reads the stored condition**, above every other arm of its fold. The gauge is
  otherwise a projection of live per-kind state, but a stopped cache arms no kinds, so nothing live
  can answer for it — and a cache that is both at its ceiling and paused kind by kind must report
  the ceiling, or the two reasons stop being distinguishable.
- **`clusterCacheClear` requeues the cache record**, with the retry backoff reset. A stopped cache
  holds no claim, so `Manager.Clear` reopens nothing: no file, no janitor, no verdict, no wake. The
  clear is the user's remedy, so it carries the wake itself.

## Alternatives considered

**Keep the pause in memory, in the controller.** It would survive the closed file but not a restart,
and the cache the user restarts the app to fix is exactly the cache that is full. A verdict a
restart forgets is one the user can trip over twice.

**Hold the file open while the cache is stopped, so the janitor keeps judging it.** It keeps the
verdict alive at the price of the thing the stop is for: an open file has a WAL, a janitor, and a
writer's worth of machinery pointed at a cache we have decided not to write to.

**Recompute the verdict in `Stats` for a closed file.** That is a second implementation of the
ceiling in the place whose doc comment promises it never recomputes, and it disagrees with the
janitor's over a WAL mid-checkpoint.

## Consequences

A stopped cache never restarts itself, because nothing evicts: the file only shrinks when the user
clears it, and the limit only rises across a restart. That is the ADR above's bargain, made concrete
— "stops" means "stays stopped until the user acts".

The record now carries a decision, not just observations, which is a small widening of what a
condition means here. `Synced` has one writer and one reason, so the ambiguity is bounded; a second
writer would need to keep the presence check honest, since presence — not status — is what holds the
pause. A liveness condition an earlier process wrote reads as `Unknown` until the pass re-confirms
it, and reading that as "not stopped" would release every stopped cache on restart.

The clear's requeue is a latency hint rather than a guarantee. `ClusterCache` has no periodic pass
of its own, so a requeue that never lands leaves the cache stopped until the next start. It is
in-process, and its only failures are a record already gone or a process already stopping, so the
gap it leaves is one a restart closes.
