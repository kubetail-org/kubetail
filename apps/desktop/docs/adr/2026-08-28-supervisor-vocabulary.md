---
title: The probe engine is a supervisor, and a probe is a reconciler
date: 2026-08-28
scope: sidecar
status: Accepted
---

# The probe engine is a supervisor, and a probe is a reconciler

## Context

[The extraction](2026-08-24-probe-engine.md) pulled a scheduler out of `kubeconn` and named it for
the only bodies it then had: `internal/probe`, holding `Engine` and `Probe[T]`. Every body did in
fact probe — five of them read a server, three read discovery documents — so the name described
the package accurately enough.

What the package does is wider than that. It keeps a set of subjects each holding a standing value,
re-runs a body over one on an interval, climbs a backoff ladder when the body fails, parks it until
a dependency moves, and hands back what it stops holding. None of that is about observing. The
mismatch stayed small only while every body fetched an observation;
the kind sync (*the stream is the value*) gives a body whose value is a live stream and a
goroutine, and calling that a probe is wrong in a way a reader has to work around.

## Decision

The package is `internal/lib/supervisor`, and the body a caller writes is a `Reconciler[T]` whose
method is `Reconcile`.

| Before | After |
| --- | --- |
| package `internal/probe` | `internal/lib/supervisor` |
| `Engine` | `Supervisor` |
| `Probe[T]` | `Reconciler[T]` |
| `Probe.Run` | `Reconciler.Reconcile` |
| `ProbeOption` | `ReconcilerOption` |

Nothing else moved. `Register`, `Add`, `Remove`, `Wake`, `Read`, `OnPass`, `Result`, `Pass`,
`Snapshot`, `Observation` and `Backoff` were already named for the supervisor's job.

Three things the word "probe" still names correctly, and which keep it: `kubeconn`'s five probes,
which read a server; `kubesync`'s three discovery probes; and `testutil.Probe[T]`, the channel
tripwire tests wait on. A probe is now a reconciler whose value happens to be an observation.

**"Run" stays the noun for one call of `Reconcile`** — `runQ`, `runLoop`, "a run in flight", "a
run's `Result` is its schedule". The method is what a body implements; a run is one execution of
it, and the two words earn their keep apart.

## Alternatives considered

**Keep `Engine`, rename only `Probe`.** The smaller change, and it fixes the sharper error — the
body is what the kind sync makes wrong. But it leaves the package named `probe` while nothing in its
vocabulary is about probing, so the import path keeps lying at every call site.

**Name the package `reconcile`, for what a caller writes.** It reads well at the declaration
(`reconcile.Reconciler`) and badly everywhere else: the package is the thing that *keeps*
reconcilers running, and `reconcile.Register`, `reconcile.Add`, `reconcile.Read` are all the
supervisor's verbs, not a reconciler's. "Supervisor" names the noun that owns them.

**Take controller-runtime's spelling whole** — `Manager` and `Reconciler`. `Reconciler`/`Reconcile`
is worth borrowing: this repo's audience reads Kubernetes code, and the shape genuinely matches.
`Manager` is not — controller-runtime's manager wires caches, clients, leader election and webhooks
against an apiserver, and importing the word would promise all of it. A supervisor keeps a fixed
set of bodies running over subjects, which is a smaller and older idea.

## Consequences

Call sites read `supervisor.Register(e, name, r, opts...)`, `supervisor.Result`,
`supervisor.Pass[T]`. The fields holding one are named for the role: `kubeconn.Service.supervisor`,
`kubesync.Service.discoverySupervisor`.

`prefsync.Engine` is a different thing with the same old name and keeps it, as does `CLAUDE.md`'s
heading *The sync engine (`internal/services/cluster/internal/kubesync`)*, which names a subsystem rather
than the machinery under it. "Engine" was never reserved; it stopped being the right word only for
this package.

The rename is names alone — no behavior changed, and no test changed beyond the names it uses.
[The extraction ADR](2026-08-24-probe-engine.md) stays Accepted: the decision it records is intact,
and only what the thing is called has moved.

## Revisit when

A second body arrives whose value is neither an observation nor a stream, and `Observation` /
`Snapshot` stop covering what a subject holds. Those two words were deliberately left alone here —
they are still right for every body that exists.

## Update, 2026-08-28

The supervisor grew a second kind of thing to run, and three of the names below moved with it:
`Reconciler[T]` is `Job[T]`, `Reconcile` is `Run`, and `ReconcilerOption` is `RegistrationOption`,
beside the new `Worker[T]`. `Register` and `Pass` split into `RegisterJob`/`RegisterWorker` and
`JobPass`/`WorkerPass`, and `Get`/`Observation` into `GetJobObservation`/`GetWorkerObservation` and
`JobObservation`/`WorkerObservation`. The package name and `Supervisor` stand, which is the part
this ADR decided. → [Jobs and workers](2026-08-28-jobs-and-workers.md).
