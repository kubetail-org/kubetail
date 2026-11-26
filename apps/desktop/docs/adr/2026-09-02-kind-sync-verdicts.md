---
title: A kind sync proves itself by a frame, resumes off its cookie, and is judged at read time
date: 2026-09-02
scope: sidecar
status: Accepted
amended_by: [A quiet watch is reopened from its cookie, and reads Stale only when the reopen hangs](2026-09-27-a-quiet-watch-is-reopened-from-its-cookie.md)
---

# A kind sync proves itself by a frame, resumes off its cookie, and is judged at read time

## Context

A kind's sync is the supervisor's one worker (→ [jobs and workers](2026-08-28-jobs-and-workers.md)):
`kindSync.Run` blocks for the stream's whole life, from cold list to last delta. That ADR settles
how the supervisor treats a worker. What remained was the sync's own vocabulary: what counts as
proof that a watch works, when a start is cold versus a resume, what a run commits, and how a
reader assembles a `KindState` out of three owners.

## Decision

**The start cap is the cold-list gate.** `pass.Ready()` is called when the watch is open, never on
the first frame: bookmarks are advisory and a quiet collection may send nothing for hours, so
waiting for a frame would let the first few kinds hold every slot. `pacing.kindStartConcurrency`
therefore bounds relists in flight across every cache however many kinds are already streaming.

**How a run ends is the whole schedule.** `nil` from `applyDeltas` with the context live is the
apiserver rotating the watch: a clean exit paced by the floor, the verdict never leaving
`Watching`. A context that ended is a `Skip` (session gone, supervisor stopped it, connection
retired). Anything else is a `Fail` up the ladder. **A watch that closed having proved nothing is a
failure, not a rotation**: proof is a frame, or staying open past `staleAfter`. Without that split
a server that accepts every watch and drops it would reopen at the floor forever while reporting
`Watching`.

**The cookie decides which start this is**, not whether the cache holds rows. A cookie on disk
means a completed LIST landed, so the watch resumes from it; without one the collection is
cold-listed through `BeginReplace`/`WritePage`/`Commit`. A relist that wrote a page and died leaves
rows but no cookie and reads as cold, which is right. **An expired position relists and says
`Resyncing`.** The flag forcing it is cleared only once the relist has landed, because the cookie
survives a LIST that failed before its first page. The rows stay served throughout, which is why
this is not the `Syncing` a cold start reports.

**A resume holds its reason.** Each run seeds its verdict from `pass.Prev()` and commits only when
that moved; otherwise `RestartAll` on a 300-kind cache becomes six hundred reconciles. The one
exception is a run's first report, which always commits: until this run has spoken a reader has
only the last exit to describe the kind by. Only a resume that outlasts `staleAfter` says
`Resuming`, announced by `openWatch` from the establishing run itself, never a timer callback: one
stream's state has one writer, and `Timer.Stop` does not wait for a callback already running. The
wait for that open stays on `ctx` throughout, and what lands after the run has gone is collected
by `abandon`, since a watch nobody waits for still holds a connection.

**A bookmark is proof of life, not data.** It moves `LastLiveAt` and the cookie; only a delta moves
`LastUpdateAt`. `Stale` is `staleAfter` without either.

**`KindState` is assembled at read and stored nowhere** (`kindStateOf`), from the reason the worker
committed, the supervisor's `Attempts`, and the session's per-frame stamps. The worker's value is
the reason alone. `Live` is the one supervisor reading carried through rather than folded, because
reconstructing it means enumerating which reasons a running stream reports, and that set is this
package's to change. **A run in flight speaks for itself; a worker's last exit describes it only
while it is down.** Without that rule a kind relisting after a `410` would read `SyncFailed`
throughout the relist. The same gate is on `NextRetryAt`. A kind with nothing committed and no
exit that outranks it answers nothing, and the getter says so.

**A run lasts as long as its connection.** It ends on `Connection.Done` and the next run goes back
through the gate. Every cancelled run leaves through `kindSync.stopped`, which records nothing and
`Wake`s its own subject when the connection was what went, because the bridge's wake has already
been and found this run in flight. A kind at the gate says `NoConnection` or `IdentityMismatch`
from the `Suspend` it records, with `NextRetryAt` and `Restarts` zero.

`publishKind` wakes a record only when the answer moved, against the session's `published` map.
Every duration is a `pacing` field and production passes `defaultPacing()`.

## Alternatives considered

- **`Ready` on the first frame.** Holds slots for as long as a quiet collection stays quiet.
- **Treat every clean watch close as a rotation.** Hides a server that drops every watch.
- **Decide cold-versus-resume from row count.** A half-written relist would resume from nothing.
- **Store `KindState` on the record.** A stale duplicate of three owners, and a status write per
  frame stamp.
- **Commit the verdict every run.** A resume poke reconciles every kind twice.

## Consequences

A kind's health reads correctly through relists, rotations, and resumes, at the cost of a
read-time fold with one non-obvious rule (exit outranks verdict only while down). Any new reason a
running stream can report must be added where `Live` is derived.
