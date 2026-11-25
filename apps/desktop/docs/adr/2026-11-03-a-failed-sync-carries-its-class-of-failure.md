---
title: A failed kind sync carries its class of failure beside its reason
date: 2026-11-03
scope: cross-cutting
status: Accepted
---

# A failed kind sync carries its class of failure beside its reason

## Context

A kind whose run fails reads `SyncFailed`, with the error's text as its message
([kind sync verdicts](2026-09-02-kind-sync-verdicts.md)). The cache's health folds the first
offender's reason into one verdict ([cache health fold](2026-09-02-cache-health-fold.md)). So the
cluster status popup could say only that some kinds were not syncing. Whether the account was
refused, the resource had gone or the server timed out stayed in a Go error string, though each
has its own remedy. [Connected carries the probe's class of
failure](2026-11-03-connected-carries-the-probes-class-of-failure.md) solved the same problem for
the connection.

## Decision

`kubeconn.ClassifyRequest` names a failed request's class from the typed error: an
`apierrors.APIStatus` by its code (`Forbidden`, `Unauthorized`, `NotFound`, `Throttled`,
`InternalError`, `ServiceUnavailable`), or the transport under it as the probe classifies it
(`Unreachable`, `TLSInvalid`, `Timeout`). Anything else is unclassified: a watch that ended
early or a frame that would not apply names no cause.

`kindStateOf` sets `KindState.Failure` from the failed attempt's error. The health fold carries
the first offender's `Failure` beside its reason as `ClusterCacheHealth.failure`, and the webview
titles the cache's alert with it (`cacheFailureSummary`).

## Alternatives considered

- **Make the class the kind's reason**, as `Connected` does. Every consumer of a kind's verdict —
  the fold's ranking, the event grading, the Clusters dialog — keys on `SyncFailed`, and each
  would have to learn the whole vocabulary to keep treating a failure as one.
- **Reuse the probe's `classify` as it stands.** It reads any error it does not know as
  `Unreachable`, which is right for a probe's own request and wrong for a run that fails for
  reasons of its own.
- **Show the first offender's message in the webview.** Detailed, but a raw string with no
  short form, and one more subscription per open popup.

## Consequences

The class moves only when the failure does, so the health gauge stays quiet while a kind keeps
failing the same way. A new class reaches the wire without a schema change and reads as *Some
kinds aren't syncing* until the webview names it.
