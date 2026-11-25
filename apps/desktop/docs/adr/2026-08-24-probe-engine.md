---
title: Extract the probe scheduler into a Kubernetes-free engine
date: 2026-08-24
scope: sidecar
status: Accepted
amended_by:
  - [The probe engine is a supervisor, and a probe is a reconciler](2026-08-28-supervisor-vocabulary.md)
  - [The supervisor runs two kinds of thing — jobs and workers](2026-08-28-jobs-and-workers.md)
  - [A supervisor run may bring its next run forward, never push it back](2026-09-30-a-run-may-bring-its-next-run-forward.md)
---

# Extract the probe scheduler into a Kubernetes-free engine

## Context

`kubeconn` hands out leases on kube-contexts and reports what probing the server behind one
found. Half of it was Kubernetes — what to ask a server, and what the answers mean — and half was
a scheduler: a work queue, a level-triggered pass, an in-flight guard, per-probe cadences, and a
timer per context.

The scheduler half was not finished. It still owed panic recovery in the worker, a per-run
deadline, a backoff ladder, a worker count, and logs. Written where they were, each would have
landed as generic machinery threaded through Kubernetes-specific code — and the seam was already
being crossed in the other direction: the scheduling function interleaved generic rules (succeeded
→ interval, failed → retry) with `ReasonContextNotFound`, `ReasonUnsupported`, and `PhasePending`.

Two neighbours might have absorbed it and could not. `beehive` is the control plane *above*
`services/cluster` — durable, multi-kind, with conditions and a GC cascade — and this is a handful of
in-memory observations *below* it. Kubernetes' `controller-runtime` needs an apiserver: its
Manager, its informers, and its `Reconcile` contract all assume one, and there is no server here
to watch.

## Decision

The scheduler is `sidecar/internal/probe`, an engine that runs periodic observations over a set
of named subjects and knows nothing about Kubernetes. `kubeconn` keeps what is asked and what
the answers mean: `probe.go` is `registerProbes` plus the five probe structs, and `service.go`
is leases and publishing.

**The engine owns the observables.** A probe is a struct implementing `Probe[T]` — beehive's
controller shape, with `T` its observable's value type, inferred at
`Register(e, name, p, opts...)`; a run hands back its result plus its next value (nil keeps the
previous one), and the engine records the pair: one value beside one `Attempts` per probe,
committed under the engine's lock. `Read` and `OnPass` hand the set back as a `Snapshot`, and the
typed `Handle[T]` registration returned projects one probe's `Observation[T]` out of it. This
replaced a caller-defined subject snapshot handed back beside `[]Attempts`: the engine stamped
`LastSeen` — bookkeeping about a value — for a value it never held, every consumer had to rebuild
the value/attempts pairing itself (`kubeconn` kept an ID struct and a zip function for exactly
that), and "a probe writes only the observation it owns" was discipline where it is now
structure. The engine is not generic; heterogeneous value types coexist because the handle is
the one door in or out of the erased storage. What was lost is a place for cross-probe
invariants maintained in one commit, which nothing used; a consumer that needs one can fold its
own struct in `OnPass`.

**A run's own `Result` is its schedule.** `Succeeded` is due again after the probe's interval,
`Fail` climbs the backoff ladder, `Suspend` records why and waits for a `Wake`, and `Skip` records
nothing and waits for a `Wake`. This is the decision that makes the extraction possible: every
domain rule went back to the probe that owns it, so the engine never learns what
`ContextNotFound` means, and the scheduler has no vocabulary left to be Kubernetes-shaped in.

The engine derives, never arms. Every pass recomputes each probe's next run from recorded state,
so no path can leave a run un-armed by forgetting it. `Wake(subject, ids...)` is the one input a
derivation cannot produce — an external fact that a recorded answer went stale — and it goes
through the run queue, whose held/dirty machinery redelivers a key that was mid-run when its
commit lands. `Needs(ids...)` declares that a probe can only run over another's success, taking
IDs `Register` already returned, so the graph is acyclic by construction.

## Alternatives considered

**Finish the machinery inside `kubeconn`.** The cheapest change, and the one the gap list argued
against: five of the six remaining items are engine concerns, so the package would have accreted
library features around business logic with no boundary to test either against.

**A third-party reconciler.** Every Go reconciler library we found (`reconciler.io/runtime`,
`angelokurtis/reconciler`, `kubermatic/reconciler`) wraps `controller-runtime` and therefore needs
an apiserver. The health-check libraries (`go-sundheit`, `InVision/go-health`,
`alexliesenfeld/health`) are single-subject: they check *this* process's dependencies, with no
notion of a fleet of subjects each carrying its own schedule. Nothing in between exists.

**`client-go`'s `TypedRateLimitingInterface` for the ladder.** Its schedule lives in a private map,
and `ScheduledAt` is rendered by the frontend as a countdown — the next run has to be readable
data. The same requirement rules out jitter inside the schedule, which would move the rendered
countdown on every pass, and any stateful limiter, since the pass re-derives from scratch.

**A `RequeueAfter` analogue**, letting a probe name its own next run time. The four results cover
every schedule `kubeconn` needs, and a probe choosing a time would put scheduling policy back in
the bodies the extraction just took it out of.

## Consequences

`kubeconn`'s public surface is unchanged — `Lease`, `State`, `Observation`, `Reason`, `Phase` —
and `services/cluster` needed no edit. Two behaviour changes rode the move deliberately: a departed
context's failure streak now clears, since `Suspend` parks a question rather than failing at one,
and a context that resolves suspends with `ReasonResolved` (scaffolding, deleted when dialing
lands).

The invariants someone could break without noticing, each pinned by a test in `internal/probe`:
a run in flight is left alone, because `NextAttempt` *is* that run; a finished run ends and
reschedules in one critical section, or a reader between the two sees a probe that quietly
stopped; a subject removed and re-added is a different subject, and a run dispatched against the
old one commits nothing; and a body that panics or returns the zero `Result` is still recorded
and still gives its key back — otherwise it wedges twice over, reading as in flight forever with
its key held in the queue.

Three things the engine deliberately does not have. There is **no clock seam**: tests pace by
option — `WithInterval`, `WithBackoff`, `WithTimeout` shrunk — which is the repo's rule for
timing-dependent units, and a clock would be a second mechanism for the same job. There are **no
metrics**: `Attempts` already carries verdict, latency, failure count, and `FailingSince`, and
`OnPass` publishes them on every pass, so the observability surface is the data itself. And
there is **no injected logger** — the engine logs through `slog` like the rest of the sidecar, and
only where nothing else can report: a body that panicked or handed back nothing.

## Revisit when

A second subject type needs the engine — that is what would prove the shape and justify promoting
it out of `internal/`. Or when a probe genuinely needs to name its own next run time, which is
when the four-result vocabulary has stopped being enough.
