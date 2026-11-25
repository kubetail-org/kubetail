---
title: The supervisor runs two kinds of thing — jobs and workers
date: 2026-08-28
scope: sidecar
status: Accepted
---

# The supervisor runs two kinds of thing — jobs and workers

## Context

`internal/lib/supervisor` was extracted for bodies that fetch: a run requests, classifies, and returns,
and [its `Result` is its schedule](2026-08-24-probe-engine.md). A probe and a discovery sweep fit
that exactly.

A kind's sync does not. It cold-lists a collection, then holds a WATCH open for hours applying
deltas — work that has to keep running after the call returns.
*The stream is the value* made it fit anyway: a run
established the stream, committed the goroutine as its value, and returned, and the goroutine woke
the supervisor whenever anything happened.

It worked, and it cost a parallel vocabulary to say what the supervisor could not. `Provisional`
existed so establishment would not reset the ladder. `proven` and `deathRecorded` were bits on the
handle telling one run's observation from another's. `standingVerdict` was a four-case table
deciding whether a run should start anything at all. `reasonWatchRotated` was a failure the
reporting layer had to know to hide. A ten-minute interval stood in for a cadence a stream does not
have. And the stream's real state — is it up, has it proven itself — lived outside the supervisor,
in an atomic pointer and a session-side map, because the supervisor had nowhere to put it.

Every one of those is the same missing idea: the supervisor had no way to say *this thing is
running right now*.

## Decision

The supervisor runs **two kinds of thing**, and the difference is what a `Run`'s return means.

- **A job's lifetime is the call.** It runs, returns, and is quiet until it is due again. `Commit`
  is buffered and applied with the verdict, a timeout bounds the whole run, and a value it owns is
  handed back through `Discard`.
- **A worker's lifetime is the supervisor's.** `Run` blocks; returning is the worker having
  *stopped*, and the verdict says how to start it again. `Commit` is applied at once, `Ready` says
  the starting phase is over, and its value must own nothing.

`Ready` means **started**, not proven healthy — the expensive part is done and the worker is now
doing the thing. Whether what it started is working is the worker's to report through its value and
to answer for in the verdict it returns.

The rule of thumb: work with a natural end is a job; work that would need a goroutine outliving the
call is a worker.

The pass splits with them — `JobPass` and `WorkerPass` — so `Ready` on a job is not a method and a
worker handed to `RegisterJob` does not build. `Result` does not split, because a body builds both
the same way; the observation does (`JobObservation`, `WorkerObservation`), because a reader judges
them differently. **A type splits when the code holding it should see a different API, not when the
supervisor treats it differently.**

Three rules carry the worker:

**`NeverReady`.** A worker that stops before its first `Ready` is recorded a *failure* where what it
returned would otherwise leave it unpaced — it says it finished cleanly, or the startup timer ended
it. Without that, a source which accepts every start and drops it would return a clean exit each
time and restart at once, forever. With it, "you never started" is the failure and the ladder paces
it. **A run finishing cleanly is what clears the streak**, not `Ready`: starting is not proof, and
a source that accepts every start calls `Ready` on each one. This is what `Provisional`, `proven`
and `deathRecorded` were doing by hand.

A `Suspend` or a `Skip` the worker chose for itself is deliberately left alone: that is it parking
at a gate, which is how a kind sync's start ends on an unreachable cluster, and it waits for the
wake that knows why.

**A stop records nothing.** An end asked for by `Restart`, `Remove` or `Close` is not the worker's
doing, so no attempt is filed and the failure streak stands. A resume poke restarts every kind on a
cache at once; one that reset the streak would have a whole cache retry a struggling server at the
base delay. The startup timeout is deliberately *not* a stop — it cancels the same context, but the
run is recorded, so one that hangs in startup climbs the ladder and stays visible.

**Two paces.** The ladder paces failures, as it always did. A new *floor* — the worker's interval,
defaulting to the backoff base — paces clean restarts, which is what stops a watch rotation being
free. A rotation is now a plain success at the floor rather than a rung nobody was allowed to
report, so `reasonWatchRotated` goes.

`Restart` joins `Wake` as a way to ask for a run: same queue rules, plus cancelling the run in
flight and marking it stopped. Neither waits — three hundred cancels are one poke. `Remove` and
`Close` do wait, which is what makes forgetting a kind synchronous.

The mechanics change to match: **one dispatcher, a slot semaphore, and a goroutine per run.**
`WithWorkers` becomes `WithStartConcurrency`, because what it bounds is *starts* — a job is starting
for its whole run, a worker only until `Ready`. So eight is eight cold lists however many streams
are already up, which a supervisor of hundreds of workers needs and a cap on runs in flight could
not give.

## Consequences

`kubesync/kinds.go` loses `kindStream`, `kindRun`, `enterKindRun`/`leaveKindRun`, `cancelKindRun`,
`committedStream`, `pendingStream`, `standingVerdict`, `proven`, `deathRecorded`,
`reasonWatchRotated`, `kindSyncInterval`, `Discard`, and the verdict half of `commitKind`. A kind's
sync is one function that runs for as long as the stream does.

Two things become wire-visible, and both are the better answer. A watch that closes before its first
frame now reads `SyncFailed` with the server's message, where `reasonWatchRotated` hid it. And
`KindState.Restarts` changes meaning — from the retry streak while a kind is down to how many times
the stream has come back inside the current healthy stretch, which is what its doc always said it
was for, and which is the flapping question the streak could never answer. The streak stays readable
as a non-zero `NextRetryAt`.

`Attempts` gains `HealthySince` and `Restarts` — `FailingSince`'s mirror — and `Attempt` gains
`ReadyAt`. One `Attempts` serves both kinds: it is the one scheduler's ledger, and the members a job
never uses are zero there and say so.

The cost is that the supervisor now holds a goroutine per running worker, where before it held one
per slot. That is what a supervisor of streams is; the semaphore is what keeps the *starts* bounded,
which is the resource that was ever scarce.

**What a reader sees while a worker runs needed one more rule than the spec anticipated.** A
worker's last exit describes it only while it is down: a run in flight speaks for itself as soon as
it is `Live` or has committed anything since that exit. Without it a kind relisting after a `410`
would read `SyncFailed` throughout the relist, and one whose connection came back would read
`IdentityMismatch` for as long as it streamed. The same rule gates `NextRetryAt`, so a live stream
never serves the countdown of the failure it recovered from. This is `WorkerObservation.Live()`'s
reason for existing, applied at the seam.

**A timed-out start is `NeverReady` whatever the body returned**, which took one more rule than
"only a clean exit converts". A worker reading its cancelled context reports that cancel — the kind
sync answers it with `Skip` — and taken at its word that parks the worker on a wake nobody owes it,
or records a `Suspend` that never retries. So the supervisor marks the start it cut short and reads
that at the exit, alongside the clean-exit rule. What a worker chose for ITSELF is untouched: a
`Suspend` or a `Skip` before the first frame is it parking at a gate, which is the ordinary way a
kind sync's start ends on an unreachable cluster, and converting those would have every kind climb
the ladder under a reason nothing observed.

**Tying a worker's `Ready` to its first frame does not survive contact with Kubernetes**, and this
is the correction that cost the most. The spec put it there so a watch the server accepts and drops
would read as never-ready; but bookmarks are advisory and a quiet collection may send nothing for
hours, so eight quiet kinds would hold every start slot indefinitely and the rest of a cache would
never list a row. `Ready` is therefore "started" — for a stream, the watch being open, which is
also what makes `Live()` honest for a healthy idle collection. Two rules put back what moving it
gave up: `Ready` no longer clears the failure streak, so a source that accepts every start cannot
flatten the ladder; and the kind sync reports a watch that closed having proved nothing — no frame,
and not open long enough to go stale — as a failure rather than a rotation.

Moving `Ready` to establishment paid for itself twice: `Live()` became true as soon as the watch
opened, which is also what a re-established stream needs to speak for itself — so the kind sync's
value collapsed to the **reason alone**. It commits exactly when that moves, so the supervisor's
`ChangedAt` is "watching since 10:02" and a struct carrying its own stamp was a second copy to
keep in step.

One smaller consequence fell out of building it. A connection retired under
a stream asks for its own next run: the pool publishes the replacement *before* the stream can
notice the connection under it died, so the session's bridge has already fired and no other wake is
owed.

Superseded: *The stream is the value*.
