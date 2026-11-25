---
title: The discovery sweep writes only on a changed fingerprint, drops a fixed list, and never prunes a partial answer
date: 2026-09-02
scope: sidecar
status: Accepted
---

# The discovery sweep writes only on a changed fingerprint, drops a fixed list, and never prunes a partial answer

## Context

Discovery runs on `internal/lib/supervisor` as three jobs per cache: `apiVersions` reads `/api`,
`apiGroups` reads `/apis`, and `resources` fans out over both on a data edge. Its answer is the
`kind_catalog` table, which the cache controller's pass turns into kind records
(→ [kind records mirror the catalog](2026-09-02-kind-records-mirror-the-catalog.md)). The table
is written through the single writer every kind's deltas queue behind, an aggregated API can be
broken while the rest of the cluster is fine, and some kinds a cluster serves answer no question a
user would ask of a mirror.

## Decision

- **A sweep is a probe whose collection cannot be watched**: plain GETs, no resourceVersion, so it
  is a cold list re-run on the supervisor's cadence. `SyncKinds` reconciles by fingerprint and
  prune, as a relist does by mark and sweep.
- **The answer goes to disk and nowhere else.** The sweep starts no kind and stops none; it
  publishes news and the kind records' passes do the rest.
- **The write is skipped when the stored fingerprint matches.** The fingerprint is read off the
  table rather than remembered, so a restart and a cleared cache each write once. The prune flag is
  part of the fingerprint: a partial answer and a complete one over identical rows are different
  writes.
- **Four filters, none optional**: preferred version only, `list` and `watch` in the verbs, no `/`
  in the plural, and not in `notMirrored`. `notMirrored` is an explicit (group, plural) table of
  kinds dropped on purpose: `events.k8s.io/events` (v1/events is the synced spelling),
  `v1/endpoints` (EndpointSlice carries the same state at less churn), and
  `coordination.k8s.io/leases` (renewed every few seconds, diagnostic of nothing). Sensitivity does
  not earn a place there; a Secret is mirrored and redacted on the way in. A dropped kind is
  invisible, since no read path reaches a cluster object except the mirror, so the bar is "answers
  no question", not "is expensive".
- **A group that will not answer is `Partial`, and blocks the prune.** Its kinds report their own
  verdicts, so a broken aggregated API shows up twice and correctly. `Partial` is the one verdict a
  `supervisor.Result` cannot carry, so it rides two fields on the session.
- **`IsCRD` comes from a CRD list matched by (group, plural)**, no version, since one definition
  serves several versions. Best-effort and outside the verdict: a refusal leaves every kind reading
  as built-in. **The same list yields the printer columns**, matched by (group, version, plural),
  because `additionalPrinterColumns` sits inside each `spec.versions[]` entry. They are stored as a
  JSON string on `kind_catalog.printer_columns` and kept as a string on `KindRow`, because the row
  is the kinds watch's `comparable` diff value and the string is what makes an edited CRD arrive as
  `Modified`. Descriptors only; the sidecar computes no cell values.
- **Two loops wake a sweep the supervisor cannot schedule.**
  `wakeOnConnectionChange` (which, since the kind supervisor, wakes the session's kinds with the
  sweep) carries both directions: a suspended run schedules
  nothing, so only a wake brings it back once a connection vouches for the cache; and a settled run
  is scheduled, so a connection that stopped dialing would read `Discovered` until the interval
  came round. It is level-triggered against the facts, since `WatchState` is latest-value, and the
  invalid direction wakes only what the verdict does not already say, because that feed publishes
  every pass. `connectionReason` is the single mapping shared with the run.
  `wakeDiscoverySweepOnCatalogChange` subscribes on the CRD and APIService object keys and wakes
  the whole sweep, not the fan-out alone, because a CRD for a new group adds that group to `/apis`.
- **A sweep prunes against the last group list it read, and that is safe.** The rows on disk were
  written from a list no newer than that one, and a group-version that stopped serving fails its
  own document read, which makes the sweep partial and prunes nothing.

## Alternatives considered

- **Write the catalog every sweep and let the upsert no-op.** A delete plus an upsert per row on
  the writer every few minutes, per cache.
- **Filter kinds by cost or sensitivity.** Cost is the user's to pay if they want the answer;
  sensitivity is handled by redaction at write time.
- **Prune on a partial answer, dropping the broken group's kinds.** Deletes records and rows for
  kinds that still exist, and hides the broken API behind an empty nav.
- **Compute printer cell values in the sidecar.** The native body ships to the client, which
  evaluates the jsonPath itself.

## Consequences

A settled cluster costs one fingerprint read per sweep. Adding a kind to `notMirrored` needs a
sentence beside it saying what question it fails to answer. A `Partial` cache keeps stale kinds
until the group answers; that is the intended trade.
