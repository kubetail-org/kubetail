---
title: A supervisor run may bring its next run forward, never push it back
date: 2026-09-30
scope: sidecar
status: Accepted
---

# A supervisor run may bring its next run forward, never push it back

## Context

[The probe engine](2026-08-24-probe-engine.md) rejected a `RequeueAfter` analogue: the four
results covered every schedule, and a probe choosing its own time would put scheduling policy
back in the bodies. Three days later a job needed one. The kind-catalog sweep stood behind a
watch that only woke it, with the sweep's interval as the backstop. On a healthy watch the
backstop could be long. With the watch down it had to be short. One registration could not be
both. That sweep has since been removed with the package that held it; the result it asked for
survived into `internal/lib/supervisor`. This records the reversal, made on 2026-08-27 and written
down on 2026-09-30.

## Decision

**`Result.RequeueAfter(d)` can only bring a run forward** (`supervisor/result.go`). The supervisor
takes it when it is positive and shorter than the registered interval, and ignores it otherwise
(`due`, `supervisor.go`). The registration stays the bound on requests made against someone
else's cluster, so no return path can push a subject past it, and forgetting the ask makes a
subject slower, never wrong.

It is read on a succeeded result alone: `Fail` owns the backoff ladder and `Suspend` schedules
nothing. A zero is no ask rather than "immediately", which would be a hot loop. The ask is kept on
the recorded `Attempt`, because every pass re-derives the schedule from recorded state.

It is spelled as beehive spells it, since a reader moves between the two schedulers. The contracts
differ: beehive's overrides the schedule outright, the supervisor's is clamped.

## Alternatives considered

**Two registrations, one per cadence.** Rejected: which one should run depends on what the last
run found, so the pair would need a switch between them that is the same ask by another name.

**Override semantics, as in beehive.** Rejected: a beehive resync interval is a store-side
re-level, while a supervisor registration bounds traffic to a cluster. A return path that could
lengthen it could quietly make a subject never run.

**Keep the four results and shorten the one interval.** Rejected: it pays the degraded cadence on
every healthy subject.

## Consequences

The probe engine's "a probe choosing a time" objection is answered by the clamp: a body can only
ask for sooner, inside a ceiling the registration still owns. Nothing in production calls it
today.

## Revisit when

It is still uncalled the next time the supervisor's result vocabulary changes. Then delete it.
